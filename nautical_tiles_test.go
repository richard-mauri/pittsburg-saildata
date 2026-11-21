package main

import (
 "bytes"
 "fmt"
 "image"
 "image/color"
 "image/png"
 "math"
 "net/http"
 "net/http/httptest"
 "os"
 "path/filepath"
 "strconv"
 "strings"
 "sync"
 "sync/atomic"
 "testing"
 "time"
)

func testNauticalPNG(t *testing.T) []byte {
 t.Helper()
 img:=image.NewRGBA(image.Rect(0,0,256,256))
 img.Set(0,0,color.RGBA{R:23,G:42,B:80,A:255})
 var buf bytes.Buffer
 if err:=png.Encode(&buf,img);err!=nil{t.Fatal(err)}
 return buf.Bytes()
}

func TestParseNauticalTilePath(t *testing.T) {
 valid:=[]string{"/api/charts/tiles/0/0/0.png","/api/charts/tiles/11/327/792.png","/api/charts/tiles/16/10672/25741.png"}
 for _,p:=range valid {_,_,_,ok:=parseNauticalTilePath(p);if !ok {t.Errorf("valid: %s",p)}}
 invalid:=[]string{"/api/charts/tiles/11/2048/0.png","/api/charts/tiles/11/0/2048.png","/api/charts/tiles/19/0/0.png","/api/charts/tiles/11/-1/0.png","/api/charts/tiles/11/1/../2.png","/api/charts/tiles/11/1/2.jpg","/api/charts/tiles/11/1/2.png/extra","/api/charts/tiles/11/1/2.png?x=1"}
 for _,p:=range invalid {_,_,_,ok:=parseNauticalTilePath(p);if ok {t.Errorf("invalid: %s",p)}}
}

func TestNauticalTileHandlerUnavailableAndLocal(t *testing.T) {
 path:="/api/charts/tiles/11/327/792.png"
 h:=nauticalTileHandlerWithUpstream("","http://127.0.0.1:1",nil)
 w:=httptest.NewRecorder()
 h.ServeHTTP(w,httptest.NewRequest("GET",path,nil))
 if w.Code!=503 || w.Header().Get("Cache-Control")!="no-store" {t.Fatalf("unconfigured cache %d",w.Code)}
 root:=t.TempDir()
 // Offline mock responds with a failure: no local tile is a clean 503.
 srv:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){w.WriteHeader(503)}))
 defer srv.Close()
 h=nauticalTileHandlerWithUpstream(root,srv.URL,srv.Client())
 w=httptest.NewRecorder()
 h.ServeHTTP(w,httptest.NewRequest("GET",path,nil))
 if w.Code!=503 {t.Fatalf("upstream unavailable: %d",w.Code)}
 dir:=filepath.Join(root,"11","327")
 if err:=os.MkdirAll(dir,0700);err!=nil{t.Fatal(err)}
 data:=testNauticalPNG(t)
 if err:=os.WriteFile(filepath.Join(dir,"792.png"),data,0600);err!=nil{t.Fatal(err)}
 w=httptest.NewRecorder()
 h.ServeHTTP(w,httptest.NewRequest("GET",path,nil))
 if w.Code!=200 || !bytes.Equal(w.Body.Bytes(),data){t.Fatalf("cached tile: %d",w.Code)}
 if w.Header().Get("Content-Type")!="image/png" || w.Header().Get("Cache-Control")!="private, max-age=3600" {t.Fatal("PNG headers missing")}
 w=httptest.NewRecorder()
 h.ServeHTTP(w,httptest.NewRequest("HEAD",path,nil))
 if w.Code!=200 || w.Body.Len()!=0 {t.Fatal("HEAD must not serve data")}
 w=httptest.NewRecorder()
 h.ServeHTTP(w,httptest.NewRequest("POST",path,nil))
 if w.Code!=405 {t.Fatal("POST must be rejected")}
 w=httptest.NewRecorder()
 h.ServeHTTP(w,httptest.NewRequest("GET","/api/charts/tiles/11/2048/0.png",nil))
 if w.Code!=400 {t.Fatal("bad coordinates must be rejected")}
 if err:=os.WriteFile(filepath.Join(dir,"792.png"),[]byte("invalid"),0600);err!=nil{t.Fatal(err)}
 w=httptest.NewRecorder()
 h.ServeHTTP(w,httptest.NewRequest("GET",path,nil))
 if w.Code!=503 {t.Fatal("invalid cached PNG must not be served")}
}

func TestNauticalWMSRequestCoordinates(t *testing.T){
 target,err:=nauticalWMSRequestURL("https://example.test/wms",1,1,0)
 if err!=nil {t.Fatal(err)}
 req:=httptest.NewRequest("GET",target,nil)
 q:=req.URL.Query()
 if q.Get("CRS")!="EPSG:3857" || q.Get("REQUEST")!="GetMap" || q.Get("WIDTH")!="256" || q.Get("LAYERS")=="" {t.Fatalf("wrong WMS query: %v",q)}
 parts:=strings.Split(q.Get("BBOX"),",")
 if len(parts)!=4 {t.Fatalf("invalid bbox: %v",parts)}
 var bbox [4]float64
 for i,x:=range parts {v,e:=strconv.ParseFloat(x,64);if e!=nil{t.Fatal(e)};bbox[i]=v}
 if math.Abs(bbox[0])>0.00001 || math.Abs(bbox[1])>0.00001 || math.Abs(bbox[2]-20037508.342789244)>0.001 || math.Abs(bbox[3]-20037508.342789244)>0.001 {
  t.Fatalf("XYZ Y orientation or projection error: %v",bbox)
 }
}

func TestNauticalFetchSaveAndOfflineCacheHit(t *testing.T){
 path:="/api/charts/tiles/11/327/792.png"
 pngData:=testNauticalPNG(t)
 var hits atomic.Int32
 srv:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  hits.Add(1)
  if r.URL.Query().Get("SERVICE")!="WMS" || r.URL.Query().Get("FORMAT")!="image/png" {t.Error("invalid GetMap request")}
  w.Header().Set("Content-Type","image/png")
  _,_=w.Write(pngData)
 }))
 root:=t.TempDir()
 h:=nauticalTileHandlerWithUpstream(root,srv.URL,srv.Client())
 r:=httptest.NewRequest("GET",path,nil)
 w:=httptest.NewRecorder()
 h.ServeHTTP(w,r)
 if w.Code!=200 || !bytes.Equal(w.Body.Bytes(),pngData) {t.Fatalf("NOAA fetch: %d %s",w.Code,w.Body.String())}
 saved,err:=os.ReadFile(filepath.Join(root,"11","327","792.png"))
 if err!=nil || !bytes.Equal(saved,pngData) {t.Fatal("tile must persist to disk")}
 srv.Close()
 w=httptest.NewRecorder()
 h.ServeHTTP(w,r)
 if w.Code!=200 || hits.Load()!=1 {t.Fatalf("offline cache miss or second NOAA call: %d, %d",w.Code,hits.Load())}
}

func TestNauticalInvalidUpstreamNeverCached(t *testing.T){
 path:="/api/charts/tiles/9/2/3.png"
 root:=t.TempDir()
 srv:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  w.Header().Set("Content-Type","text/xml")
  _,_=fmt.Fprint(w,"<ServiceException>unavailable</ServiceException>")
 }))
 defer srv.Close()
 h:=nauticalTileHandlerWithUpstream(root,srv.URL,srv.Client())
 w:=httptest.NewRecorder()
 h.ServeHTTP(w,httptest.NewRequest("GET",path,nil))
 if w.Code!=503 {t.Fatalf("invalid NOAA content must be 503, got %d",w.Code)}
 if _,err:=os.Stat(filepath.Join(root,"9","2","3.png"));!os.IsNotExist(err){t.Fatalf("bad response was cached: %v",err)}
}

func TestNauticalParallelMissDeduplication(t *testing.T){
 path:="/api/charts/tiles/11/327/792.png"
 pngData:=testNauticalPNG(t)
 var hits atomic.Int32
 arrived:=make(chan struct{})
 release:=make(chan struct{})
 srv:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if hits.Add(1)==1 {close(arrived)}
  <-release
  _,_=w.Write(pngData)
 }))
 defer srv.Close()
 h:=nauticalTileHandlerWithUpstream(t.TempDir(),srv.URL,srv.Client())
 const count=6
 var wg sync.WaitGroup
 results:=make(chan int,count)
 for i:=0;i<count;i++ {
  wg.Add(1)
  go func(){defer wg.Done();w:=httptest.NewRecorder();h.ServeHTTP(w,httptest.NewRequest("GET",path,nil));results<-w.Code}()
 }
 <-arrived
 time.Sleep(80*time.Millisecond)
 close(release)
 wg.Wait()
 close(results)
 for code:=range results {if code!=200 {t.Fatalf("concurrent tile error %d",code)}}
 if hits.Load()!=1 {t.Fatalf("same tile generated %d NOAA fetches",hits.Load())}
}
