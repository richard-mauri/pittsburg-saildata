package main

import (
 "bytes"
 "image/png"
 "net/http"
 "net/http/httptest"
 "os"
 "os/exec"
 "path/filepath"
 "strconv"
 "strings"
 "sync/atomic"
 "testing"
)

// Stage A exercises the actual chart handler with Go-generated PNG files.
// All NOAA traffic is redirected to a local sentinel server that must never
// be called for cache hits. The isolated fixture directory is deleted by Go.
func TestMockNauticalXYZCacheToBrowser(t *testing.T) {
 root:=filepath.Join(t.TempDir(),"mock-test-only")
 cmd:=exec.Command("go","run","./cmd/mocknauticaltiles","-dir",root)
 output,err:=cmd.CombinedOutput()
 if err!=nil{t.Fatalf("generate mock XYZ fixtures: %v: %s",err,output)}
 if !bytes.Contains(output,[]byte("MOCK TEST DATA")){t.Fatalf("fixture not labeled: %s",output)}

 var upstreamCalls atomic.Int32
 upstream:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  upstreamCalls.Add(1)
  http.Error(w,"offline sentinel: NOAA must not be contacted on cache hit",http.StatusBadGateway)
 }))
 defer upstream.Close()
 handler:=nauticalTileHandlerWithUpstream(root,upstream.URL,upstream.Client())

 // Independently known Leaflet/Web Mercator tile coordinates, not an MBTiles
 // TMS row. z10 Pittsburgh around x165/y394, z12 Delta around x664/y1578.
 centers:=[]struct{site string;z,x,y int}{
  {"Pittsburg",10,165,394},
  {"Delta",12,664,1578},
 }
 for _,center:=range centers {
  t.Run(center.site,func(t *testing.T){
   path:=nauticalTilePrefix+strconv.Itoa(center.z)+"/"+strconv.Itoa(center.x)+"/"+strconv.Itoa(center.y)+".png"
   disk:=filepath.Join(root,strconv.Itoa(center.z),strconv.Itoa(center.x),strconv.Itoa(center.y)+".png")
   expected,err:=os.ReadFile(disk);if err!=nil{t.Fatalf("fixture absent at %s: %v",disk,err)}
   if !validNauticalPNG(expected){t.Fatal("fixture is not a complete 256x256 PNG")}
   img,err:=png.Decode(bytes.NewReader(expected));if err!=nil{t.Fatal(err)}
   if img.Bounds().Dx()!=256||img.Bounds().Dy()!=256{t.Fatal("wrong fixture dimensions")}
   // Opaque visible mock image, not a transparent NOAA-looking placeholder.
   _,_,_,a:=img.At(30,30).RGBA()
   if a!=0xffff {t.Fatal("mock fixture pixel is transparent")}
   for i:=0;i<2;i++{
    w:=httptest.NewRecorder()
    handler.ServeHTTP(w,httptest.NewRequest("GET",path,nil))
    if w.Code!=http.StatusOK{t.Fatalf("GET HTTP %d: %s",w.Code,w.Body.String())}
    if got:=w.Header().Get("X-Sailing-Chart-Source");got!="cache"{t.Fatalf("source=%q",got)}
    if got:=w.Header().Get("X-Sailing-Chart-Result");got!="cache-hit"{t.Fatalf("result=%q",got)}
    if got:=w.Header().Get("Content-Type");got!="image/png"{t.Fatalf("mime=%q",got)}
    if !bytes.Equal(w.Body.Bytes(),expected){t.Fatal("browser response differs from disk PNG")}
   }
   head:=httptest.NewRecorder()
   handler.ServeHTTP(head,httptest.NewRequest("HEAD",path,nil))
   if head.Code!=http.StatusOK||head.Body.Len()!=0||head.Header().Get("X-Sailing-Chart-Result")!="cache-hit"{
    t.Fatalf("HEAD did not use cache: status=%d bytes=%d",head.Code,head.Body.Len())
   }
  })
 }
 if n:=upstreamCalls.Load();n!=0{t.Fatalf("cache-hit made %d upstream calls",n)}

 // Reusing an existing output directory is forbidden (protects a real cache).
 cmd=exec.Command("go","run","./cmd/mocknauticaltiles","-dir",root)
 output,err=cmd.CombinedOutput()
 if err==nil||!strings.Contains(string(output),"already exists"){
  t.Fatalf("generator failed to reject existing directory: err=%v, output=%s",err,output)
 }
}
