package main

import (
 "bytes"
 "image"
 "image/color"
 "image/png"
 "net/http"
 "net/http/httptest"
 "os"
 "path/filepath"
 "strconv"
 "sync/atomic"
 "testing"
)

func testNauticalPNG(t *testing.T) []byte {
 t.Helper()
 img:=image.NewRGBA(image.Rect(0,0,256,256))
 img.Set(0,0,color.RGBA{23,42,80,255})
 var b bytes.Buffer
 if err:=png.Encode(&b,img);err!=nil{t.Fatal(err)}
 return b.Bytes()
}
func TestParseNauticalTilePath(t *testing.T){
 for _,p:=range []string{"/api/charts/tiles/9/82/197.png","/api/charts/tiles/11/327/792.png","/api/charts/tiles/16/10672/25741.png"}{
  if _,_,_,ok:=parseNauticalTilePath(p);!ok{t.Errorf("valid native tile rejected: %s",p)}
 }
 for _,p:=range []string{"/api/charts/tiles/0/0/0.png","/api/charts/tiles/17/1/1.png",
  "/api/charts/tiles/18/1/1.png","/api/charts/tiles/11/2048/0.png",
  "/api/charts/tiles/11/0/2048.png","/api/charts/tiles/11/-1/0.png",
  "/api/charts/tiles/11/1/../2.png","/api/charts/tiles/11/1/2.jpg",
  "/api/charts/tiles/11/1/2.png/extra","/api/charts/tiles/11/1/2.png?x=1"} {
  if _,_,_,ok:=parseNauticalTilePath(p);ok{t.Errorf("invalid tile accepted: %s",p)}
 }
}
func TestReadOnlyNauticalTileHandler(t *testing.T) {
 const uri="/api/charts/tiles/11/327/792.png"
 root:=t.TempDir()
 var network atomic.Int32
 external:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  network.Add(1);http.Error(w,"unexpected upstream",500)
 }))
 defer external.Close()
 // The upstream injection is intentionally ignored by the read-only handler.
 h:=nauticalTileHandlerWithUpstream(root,external.URL,external.Client())
 get:=func(path string)*httptest.ResponseRecorder {
  w:=httptest.NewRecorder();h.ServeHTTP(w,httptest.NewRequest("GET",path,nil));return w
 }
 w:=get(uri)
 if w.Code!=404||w.Header().Get("X-Sailing-Chart-Result")!="tile-not-cached"{
  t.Fatalf("missing tile should be transparent 404: %d, result=%s",w.Code,w.Header().Get("X-Sailing-Chart-Result"))
 }
 if network.Load()!=0{t.Fatal("browser tile request called NOAA")}
 if _,err:=os.Stat(filepath.Join(root,"11"));!os.IsNotExist(err){t.Fatal("cache miss created directories")}
 dir:=filepath.Join(root,"11","327")
 if err:=os.MkdirAll(dir,0700);err!=nil{t.Fatal(err)}
 payload:=testNauticalPNG(t)
 path:=filepath.Join(dir,"792.png")
 if err:=os.WriteFile(path,payload,0600);err!=nil{t.Fatal(err)}
 w=get(uri)
 if w.Code!=200||!bytes.Equal(w.Body.Bytes(),payload)||
  w.Header().Get("X-Sailing-Chart-Result")!="cache-hit"{
  t.Fatalf("disk cache hit invalid: code=%d",w.Code)
 }
 if w.Header().Get("Content-Type")!="image/png"||w.Header().Get("Cache-Control")!="private, max-age=3600"{t.Fatal("missing PNG/cache headers")}
 r:=httptest.NewRequest("HEAD",uri,nil);w=httptest.NewRecorder();h.ServeHTTP(w,r)
 if w.Code!=200||w.Body.Len()!=0{t.Fatal("HEAD must read cache without returning body")}
 for _,method:=range []string{"POST","PUT","DELETE"}{
  w=httptest.NewRecorder();h.ServeHTTP(w,httptest.NewRequest(method,uri,nil))
  if w.Code!=405{t.Fatalf("%s accepted",method)}
 }
 w=get("/api/charts/tiles/17/1/1.png")
 if w.Code!=400{t.Fatal("z17 is display overzoom, not native cache path")}
 if err:=os.WriteFile(path,[]byte("broken"),0600);err!=nil{t.Fatal(err)}
 w=get(uri)
 if w.Code!=503||w.Header().Get("X-Sailing-Chart-Result")!="cache-invalid"{t.Fatal("corrupted PNG served")}
 if network.Load()!=0{t.Fatal("corrupt cache caused NOAA fetch")}
}
func TestUnconfiguredAndPartialCoverageReadOnly(t *testing.T) {
 uri:="/api/charts/tiles/10/163/395.png"
 w:=httptest.NewRecorder()
 nauticalTileHandler("").ServeHTTP(w,httptest.NewRequest("GET",uri,nil))
 if w.Code!=503{t.Fatal("missing cache directory must not be treated as available")}
 root:=t.TempDir()
 h:=nauticalTileHandler(root)
 // Only one geographic tile exists. Neighboring cells must not change
 // map selection; their 404s let the Street underlay show through.
 dir:=filepath.Join(root,"10","163")
 if err:=os.MkdirAll(dir,0700);err!=nil{t.Fatal(err)}
 data:=testNauticalPNG(t)
 if err:=os.WriteFile(filepath.Join(dir,"395.png"),data,0600);err!=nil{t.Fatal(err)}
 for _,tc:=range []struct{x,y,status int}{{163,395,200},{164,395,404},{163,396,404}}{
  uri:=nauticalTilePrefix+"10/"+strconv.Itoa(tc.x)+"/"+strconv.Itoa(tc.y)+".png"
  w:=httptest.NewRecorder();h.ServeHTTP(w,httptest.NewRequest("GET",uri,nil))
  if w.Code!=tc.status{t.Fatalf("tile %s status=%d want=%d",uri,w.Code,tc.status)}
 }
}
