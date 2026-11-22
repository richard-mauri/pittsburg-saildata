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

// The mock XYZ tree is an ordinary disk dataset; the HTTP server never
// generates missing images, even when legacy mock environment flags are set.
func TestMockNauticalXYZCacheToBrowser(t *testing.T){
 root:=filepath.Join(t.TempDir(),"fixture-only")
 cmd:=exec.Command("go","run","./cmd/mocknauticaltiles","-dir",root)
 output,err:=cmd.CombinedOutput()
 if err!=nil{t.Fatalf("generate lower-zoom fixtures: %v: %s",err,output)}
 if !strings.Contains(string(output),"Created 756 MOCK TEST DATA XYZ PNG tiles"){t.Fatalf("unexpected count: %s",output)}
 if !mockchart.HasMarker(root){t.Fatal("mock marker missing")}
 var upstream atomic.Int32
 sentinel:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  upstream.Add(1);http.Error(w,"forbidden NOAA request",500)
 }))
 defer sentinel.Close()
 h:=nauticalTileHandlerWithUpstream(root,sentinel.URL,sentinel.Client())
 t.Setenv("SAILING_NAUTICAL_MOCK_ON_DEMAND","1")
 for _,point:=range []struct{site string;z,x,y int}{
  {"SF-z9",9,81,197},{"Monterey-z10",10,165,399},
  {"SF-z11",11,327,791},{"Monterey-z12",12,661,1599},
 }{
  uri:=nauticalTilePrefix+strconv.Itoa(point.z)+"/"+strconv.Itoa(point.x)+"/"+strconv.Itoa(point.y)+".png"
  w:=httptest.NewRecorder();h.ServeHTTP(w,httptest.NewRequest("GET",uri,nil))
  if w.Code!=200||w.Header().Get("X-Sailing-Chart-Result")!="cache-hit"{
   t.Fatalf("%s: code=%d source=%s",point.site,w.Code,w.Header().Get("X-Sailing-Chart-Result"))
  }
  if !validNauticalPNG(w.Body.Bytes()){t.Fatalf("%s invalid PNG",point.site)}
  if _,err:=png.Decode(bytes.NewReader(w.Body.Bytes()));err!=nil{t.Fatal(err)}
 }
 uri:=nauticalTilePrefix+"14/2620/6334.png"
 w:=httptest.NewRecorder();h.ServeHTTP(w,httptest.NewRequest("GET",uri,nil))
 if w.Code!=404||w.Header().Get("X-Sailing-Chart-Result")!="tile-not-cached"{
  t.Fatal("mock flag must never synthesize high zoom cache misses")
 }
 if _,err:=os.Stat(filepath.Join(root,"16"));!os.IsNotExist(err){t.Fatal("read-only server created high-zoom fixture directories")}
 if upstream.Load()!=0{t.Fatal("mock cache lookup unexpectedly contacted NOAA")}

 // The standalone manager is the ONLY author of additional native tiles.
 high:=filepath.Join(t.TempDir(),"managed-sf-z14")
 cmd=exec.Command("go","run","./cmd/nauticaltilemanager",
  "-west","-122.42","-south","37.75","-east","-122.41","-north","37.76",
  "-min-zoom","14","-max-zoom","14","-max-tiles","20",
  "-source","mock","-download","-dir",high)
 output,err=cmd.CombinedOutput()
 if err!=nil{t.Fatalf("manager z14 fake fixture: %v: %s",err,output)}
 managed:=nauticalTileHandler(high)
 for _,name:=range []string{nauticalTilePrefix+"16/10482/25336.png",
  nauticalTilePrefix+"16/10482/25336.png"}{
  w:=httptest.NewRecorder();managed.ServeHTTP(w,httptest.NewRequest("GET",name,nil))
  if strings.Contains(name,"/14/")&&w.Code!=200{t.Fatalf("missing z14 disk tile %s: %d",name,w.Code)}
  if strings.Contains(name,"/16/")&&w.Code!=404{t.Fatalf("undownloaded z16 must be a cache miss, got %d",w.Code)}
 }
 if upstream.Load()!=0{t.Fatal("managed disk fixture contacted NOAA")}
}
