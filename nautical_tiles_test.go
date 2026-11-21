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

func TestNauticalUpstreamFailureCooldownAndCachedHit(t *testing.T){
 root:=t.TempDir()
 var hits atomic.Int32
 svc:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  hits.Add(1)
  time.Sleep(150*time.Millisecond)
  w.WriteHeader(http.StatusBadGateway)
 }))
 defer svc.Close()
 client:=&http.Client{Timeout:40*time.Millisecond}
 h:=nauticalTileHandlerWithUpstream(root,svc.URL,client)
 path:="/api/charts/tiles/10/123/456.png"
 first:=httptest.NewRecorder()
 h.ServeHTTP(first,httptest.NewRequest("GET",path,nil))
 if first.Code!=503 {t.Fatalf("timeout should be unavailable: %d",first.Code)}
 second:=httptest.NewRecorder()
 h.ServeHTTP(second,httptest.NewRequest("GET",path,nil))
 if second.Code!=503 || hits.Load()!=1 {t.Fatalf("circuit breaker did not suppress retried request: %d hits=%d",second.Code,hits.Load())}
 // The upstream cooldown should not block already cached valid tiles.
 pngData:=testNauticalPNG(t)
 dir:=filepath.Join(root,"10","124")
 if err:=os.MkdirAll(dir,0700);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(filepath.Join(dir,"456.png"),pngData,0600);err!=nil{t.Fatal(err)}
 w:=httptest.NewRecorder()
 h.ServeHTTP(w,httptest.NewRequest("GET","/api/charts/tiles/10/124/456.png",nil))
 if w.Code!=200 || hits.Load()!=1 {t.Fatal("cache must work during upstream outage")}
}

func TestNauticalUpstreamConcurrencyLimit(t *testing.T){
 root:=t.TempDir()
 pngData:=testNauticalPNG(t)
 var active atomic.Int32
 var highest atomic.Int32
 var hits atomic.Int32
 release:=make(chan struct{})
 started:=make(chan struct{},4)
 srv:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  hits.Add(1)
  current:=active.Add(1)
  defer active.Add(-1)
  for {
   top:=highest.Load()
   if top>=current || highest.CompareAndSwap(top,current){break}
  }
  started<-struct{}{}
  <-release
  _,_=w.Write(pngData)
 }))
 defer srv.Close()
 h:=nauticalTileHandlerWithUpstream(root,srv.URL,srv.Client())
 const count=8
 results:=make(chan int,count)
 var wg sync.WaitGroup
 for i:=0;i<count;i++{
  wg.Add(1)
  go func(i int){
   defer wg.Done()
   path:=fmt.Sprintf("/api/charts/tiles/11/%d/792.png",200+i)
   w:=httptest.NewRecorder()
   h.ServeHTTP(w,httptest.NewRequest("GET",path,nil))
   results<-w.Code
  }(i)
 }
 for i:=0;i<4;i++{
  select{
  case <-started:
  case <-time.After(2*time.Second):
   close(release)
   t.Fatal("upstream did not reach four concurrent requests")
  }
 }
 time.Sleep(50*time.Millisecond)
 if got:=hits.Load();got>4 {
  close(release)
  t.Fatalf("unexpected %d concurrent upstream requests before release",got)
 }
 close(release)
 wg.Wait()
 close(results)
 for code:=range results{
  if code!=200 {t.Errorf("limited fetch resulted in %d",code)}
 }
 if highest.Load()>4 {t.Fatalf("more than four upstream calls concurrent: %d",highest.Load())}
}

func TestNauticalDiagnosticHeaders(t *testing.T) {
 const path="/api/charts/tiles/11/327/792.png"
 check:=func(w *httptest.ResponseRecorder,source,result string) {
  t.Helper()
  if got:=w.Header().Get("X-Sailing-Chart-Source");got!=source {t.Errorf("source: got %q want %q",got,source)}
  if got:=w.Header().Get("X-Sailing-Chart-Result");got!=result {t.Errorf("result: got %q want %q",got,result)}
 }
 w:=httptest.NewRecorder()
 nauticalTileHandlerWithUpstream("","http://127.0.0.1:1",nil).ServeHTTP(w,httptest.NewRequest("GET",path,nil))
 check(w,"unavailable","cache-unavailable")

 pngData:=testNauticalPNG(t)
 root:=t.TempDir()
 hits:=0
 service:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request) {
  hits++
  w.Header().Set("Content-Type","image/png")
  _,_=w.Write(pngData)
 }))
 h:=nauticalTileHandlerWithUpstream(root,service.URL,service.Client())
 w=httptest.NewRecorder()
 h.ServeHTTP(w,httptest.NewRequest("GET",path,nil))
 check(w,"noaa","noaa-fetched")
 if hits!=1 || w.Code!=200 {t.Fatal("expected real upstream fixture fetch")}
 service.Close()
 w=httptest.NewRecorder()
 h.ServeHTTP(w,httptest.NewRequest("GET",path,nil))
 check(w,"cache","cache-hit")
 if hits!=1 {t.Fatal("cache hit contacted upstream")}

 failing:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request) {
  w.WriteHeader(http.StatusBadGateway)
 }))
 defer failing.Close()
 fh:=nauticalTileHandlerWithUpstream(t.TempDir(),failing.URL,failing.Client())
 w=httptest.NewRecorder()
 fh.ServeHTTP(w,httptest.NewRequest("GET",path,nil))
 check(w,"unavailable","noaa-http-error")
 if got:=w.Header().Get("X-Sailing-Chart-Upstream-Status");got!="502" {t.Fatalf("upstream HTTP status: %q",got)}
 w=httptest.NewRecorder()
 fh.ServeHTTP(w,httptest.NewRequest("GET",path,nil))
 check(w,"unavailable","cooldown")
 if w.Header().Get("X-Sailing-Chart-Upstream-Status")!="" {t.Fatal("cooldown must not assert an upstream HTTP response")}

 timed:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request) {
  time.Sleep(130*time.Millisecond)
  w.WriteHeader(200)
 }))
 defer timed.Close()
 th:=nauticalTileHandlerWithUpstream(t.TempDir(),timed.URL,&http.Client{Timeout:25*time.Millisecond})
 w=httptest.NewRecorder()
 th.ServeHTTP(w,httptest.NewRequest("GET",path,nil))
 check(w,"unavailable","noaa-timeout")
 if w.Header().Get("X-Sailing-Chart-Upstream-Status")!="" {t.Fatal("timeout must not invent HTTP status")}

 corrupted:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request) {
  w.Header().Set("Content-Type","image/png")
  _,_=w.Write([]byte("not really PNG"))
 }))
 defer corrupted.Close()
 ch:=nauticalTileHandlerWithUpstream(t.TempDir(),corrupted.URL,corrupted.Client())
 w=httptest.NewRecorder()
 ch.ServeHTTP(w,httptest.NewRequest("GET",path,nil))
 check(w,"unavailable","noaa-invalid-png")
}
