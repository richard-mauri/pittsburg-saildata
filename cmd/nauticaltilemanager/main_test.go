package main

import (
 "bytes"
 "context"
 "encoding/json"
 "image"
 "image/png"
 "io"
 "net/http"
 "net/http/httptest"
 "os"
 "strconv"
 "path/filepath"
 "strings"
 "sync/atomic"
 "testing"
 "time"

 "pittsburg-saildata/internal/mockchart"
)

func localPlan()options{
 return options{West:-122.42,South:37.75,East:-122.41,North:37.76,
  MinZoom:13,MaxZoom:13,MaxTiles:20,Source:"mock",Timeout:30*time.Second,Retries:2}
}
func TestBoundsAndLimits(t *testing.T){
 o:=localPlan()
 p,err:=buildPlan(o);if err!=nil{t.Fatal(err)}
 if p.Total<1||p.Total>int64(o.MaxTiles){t.Fatalf("unexpected total %d",p.Total)}
 o.MaxTiles=0
 if _,err:=buildPlan(o);err==nil{t.Fatal("missing hard cap accepted")}
 o=localPlan();o.MaxZoom=18
 if _,err:=buildPlan(o);err==nil{t.Fatal("z18 must be display overzoom only")}
 o=localPlan();o.East=-123
 if _,err:=buildPlan(o);err==nil{t.Fatal("backward bounds accepted")}
 o=localPlan();o.West=-125
 if _,err:=buildPlan(o);err==nil{t.Fatal("mock outside region accepted")}
 o=localPlan();o.MaxTiles=1;o.MinZoom=13;o.MaxZoom=14
 if _,err:=buildPlan(o);err==nil||!strings.Contains(err.Error(),"exceeds"){
  t.Fatalf("oversized plan accepted: %v",err)
 }
}
func TestNativeZoom12And15ManagedPlanning(t *testing.T) {
 for _, z := range []int{12, 15} {
  o := localPlan()
  o.Source = "noaa"
  o.MinZoom, o.MaxZoom = z, z
  o.MaxTiles = 100
  p, err := buildPlan(o)
  if err != nil || p.Total < 1 { t.Fatalf("z%d plan: %+v %v", z, p, err) }
 }
 o := localPlan()
 o.Source = "noaa"
 o.MinZoom, o.MaxZoom = 11, 11
 if _, err := buildPlan(o); err == nil { t.Fatal("z11 accepted") }
 o.MinZoom, o.MaxZoom = 16, 16
 if _, err := buildPlan(o); err == nil { t.Fatal("z16 accepted") }
}

func TestDryRunWritesNothingAndNoNetwork(t *testing.T){
 o:=localPlan();o.Dir=filepath.Join(t.TempDir(),"must-not-exist")
 var requests atomic.Int32
 upstream:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  requests.Add(1);http.Error(w,"unexpected",500)
 }));defer upstream.Close()
 if _,err:=execute(context.Background(),o,upstream.URL,upstream.Client());err!=nil{t.Fatal(err)}
 if _,err:=os.Stat(o.Dir);!os.IsNotExist(err){t.Fatal("dry run wrote to disk")}
 if requests.Load()!=0{t.Fatal("dry run used network")}
}
func TestDownloadMockCacheAndResume(t *testing.T){
 o:=localPlan();o.Dir=filepath.Join(t.TempDir(),"managed")
 o.Download=true
 var calls atomic.Int32
 upstream:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  calls.Add(1);http.Error(w,"unexpected",500)
 }));defer upstream.Close()
 first,err:=execute(context.Background(),o,upstream.URL,upstream.Client())
 if err!=nil{t.Fatal(err)}
 if first.Created<=0||first.Skipped!=0{t.Fatalf("first run: %+v",first)}
 again,err:=execute(context.Background(),o,upstream.URL,upstream.Client())
 if err!=nil{t.Fatal(err)}
 if again.Created!=0||again.Skipped!=first.Created{t.Fatalf("resume not a disk cache hit: %+v",again)}
 if calls.Load()!=0{t.Fatal("mock path contacted network")}
 o.MaxBytes=1
 if _,err:=execute(context.Background(),o,upstream.URL,upstream.Client());err==nil{t.Fatal("byte cap not enforced")}
 o=localPlan();o.Dir=filepath.Join(t.TempDir(),"bad");o.Download=true
 if err:=os.MkdirAll(o.Dir,0700);err!=nil{t.Fatal(err)}
 if _,err:=execute(context.Background(),o,upstream.URL,upstream.Client());err==nil{t.Fatal("unmarked directory accepted")}
}
func TestNOAAWMSAndAtomicOutput(t *testing.T){
 o:=localPlan();o.Source="noaa";o.Download=true;o.Dir=filepath.Join(t.TempDir(),"noaa")
 png,err:=mockchart.Render(9,81,197);if err!=nil{t.Fatal(err)}
 var calls atomic.Int32
 upstream:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  calls.Add(1)
  q:=r.URL.Query()
  if q.Get("REQUEST")!="GetMap"||q.Get("CRS")!="EPSG:3857"||
    q.Get("WIDTH")!="256"||q.Get("TRANSPARENT")!="TRUE"||q.Get("LAYERS")!="0,1,2,3,4,5,6,7"{
   t.Errorf("wrong NOAA WMS query: %v",q)
  }
  w.Header().Set("Content-Type","image/png")
  _,_=w.Write(png)
 }));defer upstream.Close()
 first,err:=execute(context.Background(),o,upstream.URL,upstream.Client())
 if err!=nil{t.Fatal(err)}
 marker,err:=os.ReadFile(filepath.Join(o.Dir,markerFile));if err!=nil{t.Fatal(err)}
 if !strings.HasPrefix(string(marker),"SailingGo-managed-XYZ-v2\n") ||
  !strings.Contains(string(marker),"\nlayers=0,1,2,3,4,5,6,7\n"){
  t.Fatalf("clean NOAA dataset marker does not record layer provenance: %q",marker)
 }
 if calls.Load()!=int32(first.Created){t.Fatalf("upstream calls %d != created %d",calls.Load(),first.Created)}
 second,err:=execute(context.Background(),o,upstream.URL,upstream.Client())
 if err!=nil{t.Fatal(err)}
 if second.Created!=0||second.Skipped!=first.Created||calls.Load()!=int32(first.Created){
  t.Fatalf("resume requested NOAA unexpectedly: %+v",second)
 }
 p,_:=buildPlan(o);r:=p.Rows[0]
 path:=filepath.Join(o.Dir,"13",itoa(r.X0),itoa(r.Y0)+".png")
 b,err:=os.ReadFile(path);if err!=nil||!bytes.Equal(b,png){t.Fatal("wrong saved NOAA bytes",err)}
}
func itoa(n int)string{return strings.TrimSpace(strconv.Itoa(n))}

func TestTransientNOAARetry(t *testing.T){
 o:=localPlan();o.Source="noaa";o.Dir=filepath.Join(t.TempDir(),"retries");o.Download=true
 data,err:=mockchart.Render(9,81,197);if err!=nil{t.Fatal(err)}
 var calls atomic.Int32
 upstream:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if calls.Add(1)==1 {http.Error(w,"temporarily unavailable",503);return}
  w.Header().Set("Content-Type","image/png");_,_=w.Write(data)
 }));defer upstream.Close()
 total,err:=execute(context.Background(),o,upstream.URL,upstream.Client())
 if err!=nil{t.Fatal(err)}
 if total.Created<1||calls.Load()!=int32(total.Created+1){t.Fatalf("expected one transient NOAA retry, got %+v calls=%d",total,calls.Load())}
}

func TestExtendManagedDatasetWithNewZoomAndOverlappingPlan(t *testing.T){
 o:=localPlan();o.Dir=filepath.Join(t.TempDir(),"managed");o.Download=true
 initial,err:=execute(context.Background(),o,"",nil)
 if err!=nil{t.Fatal(err)}
 if initial.Created<1{t.Fatal("no original tiles")}
 markerBefore,err:=os.ReadFile(filepath.Join(o.Dir,markerFile));if err!=nil{t.Fatal(err)}
 z14:=o;z14.MinZoom=14;z14.MaxZoom=14
 p,err:=buildPlan(z14);if err!=nil{t.Fatal(err)}
 _,inv,err:=inspectManaged(z14,p);if err!=nil{t.Fatal(err)}
 if inv.Count!=initial.Created||inv.New!=p.Total||inv.Overlap!=0{t.Fatalf("unexpected initial extension inventory: %+v",inv)}
 // Preflight the cumulative file count before downloading anything.
 reject:=z14;reject.MaxTiles=int(initial.Created+p.Total-1)
 if _,err:=execute(context.Background(),reject,"",nil);err==nil||!strings.Contains(err.Error(),"cumulative"){
  t.Fatalf("cumulative tile cap not enforced: %v",err)
 }
 if _,err:=os.Stat(filepath.Join(o.Dir,"14"));!os.IsNotExist(err){t.Fatal("rejected plan created zoom-14 directory")}
 extended,err:=execute(context.Background(),z14,"",nil)
 if err!=nil{t.Fatal(err)}
 if extended.Created!=p.Total||extended.Skipped!=0{t.Fatalf("extension failed: %+v",extended)}
 markerAfter,err:=os.ReadFile(filepath.Join(o.Dir,markerFile))
 if err!=nil||!bytes.Equal(markerAfter,markerBefore){t.Fatal("extension modified original marker/provenance",err)}
 // Re-running original bounding box must recognize all original paths as
 // overlaps; neither the original nor the newly added z14 paths are replaced.
 again,err:=execute(context.Background(),o,"",nil)
 if err!=nil{t.Fatal(err)}
 if again.Created!=0||again.Skipped!=initial.Created{t.Fatalf("original plan did not resume: %+v",again)}
 _,inv,err=inspectManaged(o,mustPlan(t,o))
 if err!=nil||inv.Count!=initial.Created+extended.Created||inv.New!=0{
  t.Fatalf("wrong total after extension: %+v err=%v",inv,err)
 }
 // A full directory byte cap cannot be bypassed by requesting a different
 // zoom or bounding box.
 tooSmall:=z14;tooSmall.MaxBytes=inv.Bytes-1
 if _,err:=execute(context.Background(),tooSmall,"",nil);err==nil||!strings.Contains(err.Error(),"max-bytes"){
  t.Fatalf("cumulative byte cap bypassed: %v",err)
 }
 // Never append mock imagery into a NOAA managed root or vice versa.
 wrongSource:=z14;wrongSource.Source="noaa"
 if _,err:=execute(context.Background(),wrongSource,"",nil);err==nil||!strings.Contains(err.Error(),"different source"){
  t.Fatalf("mixed chart sources accepted: %v",err)
 }
}
func mustPlan(t *testing.T,o options)plan{
 t.Helper();p,err:=buildPlan(o);if err!=nil{t.Fatal(err)};return p
}
func TestExtendRejectsUnexpectedEntriesAndSymlinks(t *testing.T){
 o:=localPlan();o.Dir=filepath.Join(t.TempDir(),"managed");o.Download=true
 if _,err:=execute(context.Background(),o,"",nil);err!=nil{t.Fatal(err)}
 bad:=filepath.Join(o.Dir,"unexpected.txt")
 if err:=os.WriteFile(bad,[]byte("do not touch"),0600);err!=nil{t.Fatal(err)}
 if _,err:=execute(context.Background(),o,"",nil);err==nil{
  t.Fatal("unmanaged extra file accepted")
 }
 if err:=os.Remove(bad);err!=nil{t.Fatal(err)}
 if err:=os.Symlink(filepath.Join(o.Dir,"13"),filepath.Join(o.Dir,"14"));err!=nil{t.Skipf("symlink test unavailable: %v",err)}
 z14:=o;z14.MinZoom=14;z14.MaxZoom=14
 if _,err:=execute(context.Background(),z14,"",nil);err==nil{
  t.Fatal("managed tile tree symlink accepted")
 }
}
func TestExtendDryRunDoesNotModifyExisting(t *testing.T){
 o:=localPlan();o.Dir=filepath.Join(t.TempDir(),"managed");o.Download=true
 first,err:=execute(context.Background(),o,"",nil)
 if err!=nil{t.Fatal(err)}
 o.MinZoom=14;o.MaxZoom=14;o.Download=false
 if _,err:=execute(context.Background(),o,"",nil);err!=nil{t.Fatal(err)}
 if _,err:=os.Stat(filepath.Join(o.Dir,"14"));!os.IsNotExist(err){
  t.Fatal("dry-run extension created new zoom dir")
 }
 _,inv,err:=inspectManaged(o,mustPlan(t,o))
 if err!=nil||inv.Count!=first.Created||inv.New==0{t.Fatalf("dry-run inventory wrong: %+v err=%v",inv,err)}
}

func TestNOAAScaminOffManagedProvenanceAndRequest(t *testing.T) {
 o:=localPlan();o.Source="noaa";o.DisableSCAMIN=true;o.Download=true;o.Dir=filepath.Join(t.TempDir(),"scamin-off")
 data,err:=mockchart.Render(9,81,197);if err!=nil{t.Fatal(err)}
 var calls atomic.Int32
 var defaultCalls atomic.Int32
 upstream:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  calls.Add(1)
  if !r.URL.Query().Has("Display_params") {defaultCalls.Add(1)} else {
  var v struct{ECDISParameters struct{DynamicParameters struct{Parameter []struct{Name string `json:"name"`;Value int `json:"value"`} `json:"Parameter"`} `json:"DynamicParameters"`} `json:"ECDISParameters"`}
  if err:=json.Unmarshal([]byte(r.URL.Query().Get("Display_params")),&v);err!=nil||len(v.ECDISParameters.DynamicParameters.Parameter)!=1||v.ECDISParameters.DynamicParameters.Parameter[0].Name!="HonorScamin"||v.ECDISParameters.DynamicParameters.Parameter[0].Value!=1{t.Errorf("wrong SCAMIN-off WMS query: %v, %v",r.URL.Query(),err)}
  }
  w.Header().Set("Content-Type","image/png");_,_=w.Write(data)
 }));defer upstream.Close()
 first,err:=execute(context.Background(),o,upstream.URL,upstream.Client());if err!=nil{t.Fatal(err)}
 if first.Created<1||calls.Load()!=int32(first.Created){t.Fatalf("unexpected acquisition %+v, calls=%d",first,calls.Load())}
 marker,err:=os.ReadFile(filepath.Join(o.Dir,markerFile));if err!=nil||!strings.HasPrefix(string(marker),"SailingGo-managed-XYZ-v3\n")||!strings.Contains(string(marker),"\nHonorScamin=1\n"){t.Fatalf("wrong SCAMIN provenance %q %v",marker,err)}
 again,err:=execute(context.Background(),o,upstream.URL,upstream.Client());if err!=nil||again.Created!=0||again.Skipped!=first.Created{t.Fatalf("SCAMIN resume failed %+v %v",again,err)}
 normal:=o;normal.DisableSCAMIN=false
 if _,err:=execute(context.Background(),normal,upstream.URL,upstream.Client());err==nil{t.Fatal("default NOAA allowed in SCAMIN-off cache")}
 normal.Dir=filepath.Join(t.TempDir(),"default")
 defaultResult,err:=execute(context.Background(),normal,upstream.URL,upstream.Client());if err!=nil{t.Fatal(err)}
 if defaultCalls.Load()!=int32(defaultResult.Created){t.Fatalf("default NOAA requests missing SCAMIN isolation: %d vs %d",defaultCalls.Load(),defaultResult.Created)}
 normal.DisableSCAMIN=true
 if _,err:=execute(context.Background(),normal,upstream.URL,upstream.Client());err==nil{t.Fatal("SCAMIN-off allowed in default cache")}
 mock:=localPlan();mock.DisableSCAMIN=true
 if _,err:=buildPlan(mock);err==nil{t.Fatal("mock accepted SCAMIN override")}
}

func TestAcquisitionProgressReportsEachTileAndResume(t *testing.T) {
 o:=localPlan();o.Download=true;o.Dir=filepath.Join(t.TempDir(),"progress")
 capture:=func() (string,totals,error) {
  old:=os.Stdout
  reader,writer,err:=os.Pipe();if err!=nil{t.Fatal(err)}
  os.Stdout=writer
  defer func(){os.Stdout=old;_ = writer.Close();_ = reader.Close()}()
  result,runErr:=execute(context.Background(),o,"",nil)
  _=writer.Close()
  output,readErr:=io.ReadAll(reader);if readErr!=nil{t.Fatal(readErr)}
  return string(output),result,runErr
 }
 first,created,err:=capture();if err!=nil{t.Fatal(err)}
 if created.Created<1||!strings.Contains(first,"100.0%")||!strings.Contains(first,"ETA=")||!strings.Contains(first,"saved ("){t.Fatalf("missing acquisition progress: %s",first)}
 second,resumed,err:=capture();if err!=nil{t.Fatal(err)}
 if resumed.Created!=0||resumed.Skipped!=created.Created||!strings.Contains(second,"existing")||!strings.Contains(second,"100.0%"){t.Fatalf("missing resume progress: %s",second)}
}

func TestBoundedParallelAcquisitionAndResume(t *testing.T){
 o:=localPlan();o.Source="noaa";o.Workers=4;o.Download=true;o.Dir=filepath.Join(t.TempDir(),"parallel")
 data,err:=mockchart.Render(9,81,197);if err!=nil{t.Fatal(err)}
 var active,maxActive atomic.Int32
 upstream:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  n:=active.Add(1)
  for {old:=maxActive.Load();if n<=old||maxActive.CompareAndSwap(old,n){break}}
  time.Sleep(20*time.Millisecond)
  active.Add(-1)
  w.Header().Set("Content-Type","image/png");_,_=w.Write(data)
 }));defer upstream.Close()
 p,err:=buildPlan(o);if err!=nil{t.Fatal(err)}
 first,err:=execute(context.Background(),o,upstream.URL,upstream.Client());if err!=nil{t.Fatal(err)}
 if first.Created!=p.Total||maxActive.Load()<2||maxActive.Load()>4{t.Fatalf("parallel result %+v max concurrency=%d",first,maxActive.Load())}
 second,err:=execute(context.Background(),o,upstream.URL,upstream.Client());if err!=nil||second.Created!=0||second.Skipped!=p.Total{t.Fatalf("resume %+v %v",second,err)}
 o.Workers=9
 if _,err:=buildPlan(o);err==nil{t.Fatal("accepted excessive workers")}
}


func TestSeamStudySameExtentAndIsolation(t *testing.T) {
 png256,err:=mockchart.Render(13,1300,3150);if err!=nil{t.Fatal(err)}
 var seen atomic.Int32
 server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  q:=r.URL.Query()
  if q.Get("LAYERS")!="0,1,2,3,4,5,6,7"||!strings.Contains(q.Get("Display_params"),"\"HonorScamin\"") {t.Errorf("missing provenance: %v",q)}
  if q.Get("WIDTH")=="512"{
   seen.Add(1)
   im:=image.NewRGBA(image.Rect(0,0,512,512))
   var b bytes.Buffer
   if e:=png.Encode(&b,im);e!=nil{t.Error(e)}
   w.Header().Set("Content-Type","image/png");_,_=w.Write(b.Bytes());return
  }
  if q.Get("WIDTH")!="256"{t.Errorf("unexpected width: %v",q)}
  seen.Add(1);w.Header().Set("Content-Type","image/png");_,_=w.Write(png256)
 }));defer server.Close()
 out:=filepath.Join(t.TempDir(),"seam")
 if err:=writeSeamStudy(context.Background(),out,server.URL,server.Client(),-121.94,38.06);err!=nil{t.Fatal(err)}
 if seen.Load()!=5{t.Fatalf("expected 5 requests, got %d",seen.Load())}
 for _,name:=range []string{"mosaic-4x256.png","single-512.png","requests.txt"}{if _,e:=os.Stat(filepath.Join(out,name));e!=nil{t.Fatal(e)}}
 if err:=writeSeamStudy(context.Background(),out,server.URL,server.Client(),-121.94,38.06);err==nil{t.Fatal("overwriting study accepted")}
}
