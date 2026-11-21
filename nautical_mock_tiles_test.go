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

 "pittsburg-saildata/internal/mockchart"
)

// All real NOAA calls use a local sentinel. No network or external NOAA data
// is required. The generated region is fake and explicitly non-navigational.
func TestMockNauticalXYZCacheToBrowser(t *testing.T) {
 root:=filepath.Join(t.TempDir(),"mock-test-only")
 cmd:=exec.Command("go","run","./cmd/mocknauticaltiles","-dir",root)
 output,err:=cmd.CombinedOutput()
 if err!=nil{t.Fatalf("generate Bay-to-Monterey fixtures: %v: %s",err,output)}
 if !strings.Contains(string(output),"Created 756 MOCK TEST DATA XYZ PNG tiles"){
  t.Fatalf("unexpected bounded lower-zoom tile count: %s",output)
 }
 if !mockchart.HasMarker(root){t.Fatal("missing safe test-only region marker")}

 var upstreamCalls atomic.Int32
 upstream:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  upstreamCalls.Add(1)
  http.Error(w,"NOAA sentinel must not be queried in local mock mode",http.StatusBadGateway)
 }))
 defer upstream.Close()
 handler:=nauticalTileHandlerWithUpstream(root,upstream.URL,upstream.Client())
 t.Setenv("SAILING_NAUTICAL_MOCK_ON_DEMAND","1")

 // Two distinct coastal endpoints at EVERY displayed nautical zoom level.
 // z9..12 must exist on disk; z13..18 are bounded and rendered on demand.
 sites:=[]struct{site string;coords [10][2]int}{
  {"SanFrancisco",[10][2]int{{81,197},{163,395},{327,791},{655,1583},{1310,3167},{2620,6334},{5241,12668},{10482,25336},{20964,50673},{41928,101347}}},
  {"Monterey",[10][2]int{{82,199},{165,399},{330,799},{661,1599},{1322,3199},{2644,6399},{5289,12799},{10578,25599},{21157,51199},{42314,102398}}},
 }
 for _,site:=range sites {
  for idx,coords:=range site.coords {
   z,x,y:=idx+9,coords[0],coords[1]
   t.Run(site.site+"-z"+strconv.Itoa(z),func(t *testing.T){
    if !mockchart.Covers(z,x,y){t.Fatalf("tile %d/%d/%d outside coverage",z,x,y)}
    uri:=nauticalTilePrefix+strconv.Itoa(z)+"/"+strconv.Itoa(x)+"/"+strconv.Itoa(y)+".png"
    req:=httptest.NewRequest("GET",uri,nil)
    req.Host="127.0.0.1:8765"
    req.RemoteAddr="127.0.0.1:54321"
    w:=httptest.NewRecorder()
    handler.ServeHTTP(w,req)
    if w.Code!=http.StatusOK{t.Fatalf("GET %d: %s",w.Code,w.Body.String())}
    if !validNauticalPNG(w.Body.Bytes()){t.Fatal("not a complete 256x256 PNG")}
    if _,err:=png.Decode(bytes.NewReader(w.Body.Bytes()));err!=nil{t.Fatal(err)}
    disk:=filepath.Join(root,strconv.Itoa(z),strconv.Itoa(x),strconv.Itoa(y)+".png")
    if z<=12 {
     data,err:=os.ReadFile(disk);if err!=nil{t.Fatal(err)}
     if !bytes.Equal(data,w.Body.Bytes()){t.Fatal("disk fixture bytes mismatch")}
     if w.Header().Get("X-Sailing-Chart-Result")!="cache-hit"{t.Fatal("lower zoom must be a cache hit")}
    } else {
     if _,err:=os.Stat(disk);!os.IsNotExist(err){t.Fatal("high zoom must be lazy, not on disk")}
     if w.Header().Get("X-Sailing-Chart-Result")!="mock-generated"{t.Fatal("high zoom must be lazily generated")}
    }
    head:=httptest.NewRecorder()
    req.Method="HEAD"
    handler.ServeHTTP(head,req)
    if head.Code!=http.StatusOK || head.Body.Len()!=0 {t.Fatalf("HEAD %d bytes=%d",head.Code,head.Body.Len())}
   })
  }
 }
 if upstreamCalls.Load()!=0{t.Fatal("mock region accessed NOAA sentinel")}

 outside:=httptest.NewRequest("GET",nauticalTilePrefix+"12/1/1.png",nil)
 outside.Host="localhost:8765";outside.RemoteAddr="127.0.0.1:55555"
 w:=httptest.NewRecorder();handler.ServeHTTP(w,outside)
 if w.Code!=http.StatusNotFound||w.Header().Get("X-Sailing-Chart-Result")!="outside-mock-region"{
  t.Fatalf("outside region returned %d, result %q",w.Code,w.Header().Get("X-Sailing-Chart-Result"))
 }
 if upstreamCalls.Load()!=0{t.Fatal("mock out-of-region request contacted NOAA")}
 
 // A public Host cannot cause dynamic fake imagery, even if the owner
 // explicitly enabled the local mode on the server.
 public:=httptest.NewRequest("GET",nauticalTilePrefix+"18/41928/101347.png",nil)
 public.Host="weather.themauris.org";public.RemoteAddr="127.0.0.1:55555"
 w=httptest.NewRecorder();handler.ServeHTTP(w,public)
 if w.Code==http.StatusOK||w.Header().Get("X-Sailing-Chart-Result")=="mock-generated"{
  t.Fatal("public hostname must never render on-demand fake chart imagery")
 }
 if upstreamCalls.Load()==0{t.Fatal("public request bypassed upstream sentinel unexpectedly")}

 // A generated output directory is never overwritten.
 cmd=exec.Command("go","run","./cmd/mocknauticaltiles","-dir",root)
 output,err=cmd.CombinedOutput()
 if err==nil||!strings.Contains(string(output),"already exists"){
  t.Fatalf("generator did not reject existing directory: %v: %s",err,output)
 }
}
