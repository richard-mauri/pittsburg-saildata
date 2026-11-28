package main

import (
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "net/http"
 "net/http/httptest"
 "os"
 "sort"
 "strings"
 "sync"
 "testing"
 "time"

 "pittsburg-saildata/internal/poigrid"
)
type poiFake struct{fn func(context.Context,poigrid.Query)(poigrid.Result,error)}
func(f *poiFake)Search(c context.Context,q poigrid.Query)(poigrid.Result,error){return f.fn(c,q)}
func(f *poiFake)Close()error{return nil}
func poiTestService(fn func(context.Context,poigrid.Query)(poigrid.Result,error))*poiService{
 return newPOIService(&poiFake{fn},poigrid.Manifest{Release:poigrid.Release,Mapping:poigrid.Mapping,Created:poigrid.Created,PayloadSHA256:strings.Repeat("a",64),Stats:poigrid.Stats{Categories:map[string]int{"coffee":2,"restaurant":2}}})
}
func poiTestRequest(h http.Handler,method,path string,headers map[string]string)*httptest.ResponseRecorder{
 r:=httptest.NewRequest(method,"https://weather.themauris.org"+path,nil)
 for k,v:=range headers{r.Header.Set(k,v)}
 w:=httptest.NewRecorder();h.ServeHTTP(w,r);return w
}
const poiTestBBox=poiBBoxPath+"?bbox=-122.43,37.77,-122.39,37.81&category=coffee"
func poiTestRecord(cat,id string)json.RawMessage{
 b,_:=json.Marshal(poigrid.Waypoint{ID:id,Lat:37.78,Lon:-122.4,Name:"Example",Category:cat,Notes:"Source: test",Created:poigrid.Created});return b
}
func TestPOIHTTPDefaultDenyAndCORS(t *testing.T){
 p:=newPOIService(nil,poigrid.Manifest{});defer p.close()
 mux:=http.NewServeMux();for _,path:=range poiPaths{mux.Handle(path,p)}
 mux.HandleFunc("/",func(w http.ResponseWriter,r *http.Request){w.WriteHeader(204)})
 gate,e:=NewSailingHTTPAuth(NewSailingSuiteGate());if e!=nil{t.Fatal(e)}
 h:=gate.ProtectWeather(mux)
 for _,path:=range []string{poiStatusPath,poiCategoriesPath,poiAttributionPath}{w:=poiTestRequest(h,"GET",path,nil);if w.Code!=200{t.Fatalf("%s: %d",path,w.Code)}}
 for _,path:=range []string{"/api/poi","/api/poi/unknown","/api/poi/status/","/api/poi/%73tatus","/api/poi//status","/api/poi/../poi/status","/api/charts/tiles/1/2/3.png","/assets/marine_places.json","/point-weather","/api/activity/identity"}{
  if w:=poiTestRequest(h,"GET",path,nil);w.Code!=401{t.Fatalf("unexpected public %s: %d",path,w.Code)}
 }
 if w:=poiTestRequest(h,"POST",poiStatusPath,nil);w.Code!=401{t.Fatal("write exempt",w.Code)}
 if w:=poiTestRequest(h,"HEAD",poiStatusPath,nil);w.Code!=401{t.Fatal("HEAD exempt",w.Code)}
 w:=poiTestRequest(h,"GET",poiTestBBox,map[string]string{"Origin":poiOrigin})
 if w.Code!=503||w.Header().Get("Access-Control-Allow-Origin")!=poiOrigin||w.Header().Get("Cache-Control")!="no-store"{t.Fatal(w.Code,w.Header())}
 w=poiTestRequest(h,"GET",poiStatusPath,map[string]string{"Origin":"https://evil.example"})
 if w.Code!=403||w.Header().Get("Access-Control-Allow-Origin")!=""{t.Fatal(w.Code,w.Header())}
 w=poiTestRequest(h,"OPTIONS",poiTestBBox,map[string]string{"Origin":poiOrigin,"Access-Control-Request-Method":"GET","Access-Control-Request-Headers":"If-None-Match"})
 if w.Code!=204||w.Header().Get("Access-Control-Allow-Credentials")!=""||w.Header().Get("Access-Control-Allow-Origin")!=poiOrigin{t.Fatal(w.Code,w.Header())}
 w=poiTestRequest(h,"OPTIONS",poiStatusPath,map[string]string{"Origin":poiOrigin,"Access-Control-Request-Method":"GET","Access-Control-Request-Headers":"Authorization"})
 if w.Code!=403{t.Fatal(w.Code)}
 w=poiTestRequest(h,"GET",poiStatusPath,map[string]string{"Authorization":"Bearer "+strings.Repeat("a",64),"Cookie":"test=private"})
 if w.Code!=200{t.Fatal("unexpected authentication call",w.Code)}
 if w:=poiTestRequest(h,"GET","/api/maps/street/3/2/4.png",nil);w.Code!=204{t.Fatal("street route changed",w.Code)}
}
func TestPOIHTTPValidation(t *testing.T){
 p:=newPOIService(nil,poigrid.Manifest{})
 bad:=[]string{"","?bbox=-122,37,-121,38&category=coffee","?bbox=NaN,37,-122,37.1&category=coffee","?bbox=-122,37,-121.9,37.1&category=coffee&category=coffee","?bbox=-122,37,-121.9,37.1&category=unknown","?bbox=-122,37,-121.9,37.1&category=coffee&limit=0","?bbox=-122,37,-121.9,37.1&category=coffee&limit=501","?bbox=-122,37,-121.9,37.1&category=coffee&limit=2&limit=3","?bbox=-122,37,-121.9,37.1&category=coffee&token=x","?bbox=-122,37,-121.9,37.1&category=%xx","?bbox=-122,37,-121.9,37.1&category=coffee&","?bbox=-122,37,-121.9,37.1&category=coffee&bbox=1,2,3,4","?bbox=-122,37,-121.9,37.1&category=coffee&category=restaurant&category=fuel&category=lodging&category=parking","?bbox=1e2,37,100.1,37.1&category=coffee","?bbox=-122,37,-121.9,37.1&category=coffee&x="+strings.Repeat("a",1025)}
 for _,s:=range bad{if w:=poiTestRequest(p,"GET",poiBBoxPath+s,nil);w.Code!=400{t.Fatalf("invalid query status=%d for %q",w.Code,s)}}
 if w:=poiTestRequest(p,"GET",poiStatusPath+"?token=x",nil);w.Code!=400{t.Fatal(w.Code)}
 if w:=poiTestRequest(p,"GET",poiTestBBox,nil);w.Code!=503{t.Fatal(w.Code)}
}
func TestPOIHTTPResultsAndCache(t *testing.T){
 var calls int
 p:=poiTestService(func(ctx context.Context,q poigrid.Query)(poigrid.Result,error){
  calls++;if len(q.Categories)!=1||q.Categories[0]!="coffee"||q.Limit!=200{t.Errorf("query %+v",q)}
  return poigrid.Result{Records:[]json.RawMessage{poiTestRecord("coffee","poi-ovt-a")},Truncated:true},nil
 })
 path:=poiTestBBox+"&category=marina"
 w:=poiTestRequest(p,"GET",path,map[string]string{"Origin":poiOrigin})
 var out poiBBoxResponse
 if w.Code!=200||json.Unmarshal(w.Body.Bytes(),&out)!=nil||len(out.Records)!=1||!out.Truncated||len(out.UnavailableCategories)!=1||out.UnavailableCategories[0]!="marina"{t.Fatal(w.Code,w.Body.String())}
 var wp map[string]any;if json.Unmarshal(out.Records[0].Waypoint,&wp)!=nil||len(wp)!=7||wp["category"]!="coffee"{t.Fatal(wp)}
 tag:=w.Header().Get("ETag");if tag==""||w.Header().Get("Cache-Control")!="public, max-age=60"{t.Fatal(w.Header())}
 w=poiTestRequest(p,"GET",path,map[string]string{"If-None-Match":"W/"+tag})
 if w.Code!=304||w.Body.Len()!=0||calls!=2{t.Fatal(w.Code,calls)}
 w=poiTestRequest(p,"GET",strings.Replace(path,"category=coffee&category=marina","category=marina&category=coffee",1),map[string]string{"If-None-Match":tag})
 if w.Code!=304{t.Fatal("normalized query",w.Code)}
 w=poiTestRequest(p,"GET",strings.Replace(poiTestBBox,"coffee","marina",1),nil)
 if w.Code!=200||json.Unmarshal(w.Body.Bytes(),&out)!=nil||len(out.Records)!=0||len(out.UnavailableCategories)!=1{t.Fatal(w.Code,w.Body.String())}
 w=poiTestRequest(p,"GET",poiCategoriesPath,nil)
 var cats poiCategoriesResponse;if w.Code!=200||json.Unmarshal(w.Body.Bytes(),&cats)!=nil||!cats.Categories["coffee"].Available||cats.Categories["marina"].Available{t.Fatal(w.Code,w.Body.String())}
 w=poiTestRequest(p,"GET",poiAttributionPath,nil)
 if w.Code!=200||!strings.Contains(w.Body.String(),poiFoursquare)||!strings.Contains(w.Body.String(),poiApache)||!strings.Contains(w.Body.String(),poiCDLA)||!strings.Contains(w.Body.String(),poiCC0)||!strings.Contains(w.Body.String(),"2026-11-27"){t.Fatal("notice missing")}
}
func TestPOIHTTPFullBodyCapAndFailure(t *testing.T){
 p:=poiTestService(func(context.Context,poigrid.Query)(poigrid.Result,error){
  records:=make([]json.RawMessage,500)
  for i:=range records{raw,_:=json.Marshal(poigrid.Waypoint{ID:fmt.Sprintf("poi-ovt-%04d",i),Category:"coffee",Name:"Example",Notes:strings.Repeat("x",950),Created:poigrid.Created});records[i]=raw}
  return poigrid.Result{Records:records},nil
 })
 w:=poiTestRequest(p,"GET",poiTestBBox+"&limit=500",nil)
 var out poiBBoxResponse
 if w.Code!=200||w.Body.Len()>poiMaxBody||json.Unmarshal(w.Body.Bytes(),&out)!=nil||!out.Truncated||len(out.Records)==0||len(out.Records)>=500{t.Fatal(w.Code,w.Body.Len(),len(out.Records),out.Truncated)}
 bad:=poiTestService(func(context.Context,poigrid.Query)(poigrid.Result,error){return poigrid.Result{},errors.New("private path error")})
 w=poiTestRequest(bad,"GET",poiTestBBox,map[string]string{"If-None-Match":"*"})
 if w.Code!=503||strings.Contains(w.Body.String(),"private")||w.Header().Get("ETag")!=""{t.Fatal(w.Code,w.Body.String())}
 w=poiTestRequest(bad,"GET",poiStatusPath,nil);var s poiStatus
 if w.Code!=200||json.Unmarshal(w.Body.Bytes(),&s)!=nil||s.Available||s.DatasetVersion!=nil{t.Fatal(w.Code,w.Body.String())}
 missing:=openPOIService(t.TempDir()+"/absent");defer missing.close()
 if missing.available(){t.Fatal("missing dataset exposed")}
}
func TestPOIHTTPAdmission(t *testing.T){
 p:=newPOIService(nil,poigrid.Manifest{})
 now:=p.last;p.now=func()time.Time{return now}
 for i:=0;i<16;i++{if w:=poiTestRequest(p,"GET",poiStatusPath,nil);w.Code!=200{t.Fatal(i,w.Code)}}
 if w:=poiTestRequest(p,"GET",poiStatusPath,nil);w.Code!=429||w.Header().Get("Retry-After")!="1"{t.Fatal(w.Code,w.Header())}
 now=now.Add(time.Second/8)
 if w:=poiTestRequest(p,"GET",poiStatusPath,nil);w.Code!=200{t.Fatal(w.Code)}
 started:=make(chan struct{},4);release:=make(chan struct{})
 q:=poiTestService(func(ctx context.Context,_ poigrid.Query)(poigrid.Result,error){started<-struct{}{};select{case <-release:return poigrid.Result{},nil;case <-ctx.Done():return poigrid.Result{},ctx.Err()}})
 var wg sync.WaitGroup
 defer func(){close(release);wg.Wait()}()
 for i:=0;i<4;i++{wg.Add(1);go func(){defer wg.Done();w:=poiTestRequest(q,"GET",poiTestBBox,nil);if w.Code!=200{t.Errorf("admitted query %d",w.Code)}}()}
 for i:=0;i<4;i++{select{case <-started:case <-time.After(time.Second):t.Fatal("admission timeout")}}
 if w:=poiTestRequest(q,"GET",poiTestBBox,nil);w.Code!=429{t.Fatal(w.Code)}
}
func TestPOIHTTPDeadline(t *testing.T){
 p:=poiTestService(func(ctx context.Context,q poigrid.Query)(poigrid.Result,error){<-ctx.Done();return poigrid.Result{},ctx.Err()})
 w:=poiTestRequest(p,"GET",poiTestBBox,nil);if w.Code!=503||!p.available(){t.Fatal(w.Code,p.available())}
}
// Opt-in real generation acceptance, no listening socket, upstream, writes or
// runtime configuration change. Not executed by normal CI or devbuild.
func TestPOIOwnerGeneration(t *testing.T){
 dir:=os.Getenv("SAILING_POI_TEST_GENERATION");if dir==""{t.Skip("owner local generation not configured")}
 start:=time.Now();p:=openPOIService(dir);defer p.close();openMS:=float64(time.Since(start))/float64(time.Millisecond)
 if !p.available(){t.Fatal("reviewed generation unavailable")}
 gate,e:=NewSailingHTTPAuth(NewSailingSuiteGate());if e!=nil{t.Fatal(e)}
 mux:=http.NewServeMux();for _,path:=range poiPaths{mux.Handle(path,p)}
 h:=gate.ProtectWeather(mux)
 // Test rate semantics separately; for this bounded diagnostic, reset the
 // local token bucket between sequential groups, not in the runtime.
 paths:=[]string{
 poiTestBBox,
 poiBBoxPath+"?bbox=-122.43,37.77,-122.39,37.81&category=restaurant&category=coffee&category=grocery&category=parking&limit=500",
 poiBBoxPath+"?bbox=-118.5,34,-118.1,34.3&category=restaurant&category=coffee&limit=500",
 poiBBoxPath+"?bbox=-121.94,38,-121.84,38.06&category=fuel&category=restaurant",
 poiBBoxPath+"?bbox=-120.58,38.16,-120.49,38.24&category=grocery&category=fuel",
 poiBBoxPath+"?bbox=-117.23,32.66,-117.10,32.76&category=parking&category=ev_charging",
 poiBBoxPath+"?bbox=-110,35,-109.9,35.1&category=coffee",
 poiBBoxPath+"?bbox=-122.43,37.77,-122.39,37.81&category=marina",
 }
 expected:=make([]string,len(paths));times:=[]float64{};total:=0
 for i,path:=range paths{
  s:=time.Now();w:=poiTestRequest(h,"GET",path,map[string]string{"Origin":poiOrigin});times=append(times,float64(time.Since(s))/float64(time.Millisecond))
  if w.Code!=200||w.Body.Len()>poiMaxBody{t.Fatalf("query %d HTTP %d",i,w.Code)}
  var out poiBBoxResponse;if json.Unmarshal(w.Body.Bytes(),&out)!=nil||len(out.Records)>500{t.Fatal("invalid response")}
  expected[i]=w.Body.String();total+=w.Body.Len()
 }
 concurrent:=[]float64{};for i,path:=range paths{
  p.mu.Lock();p.tokens=16;p.last=p.now();p.mu.Unlock()
  var wg sync.WaitGroup;dur:=make(chan float64,4)
  for j:=0;j<4;j++{wg.Add(1);go func(){defer wg.Done();s:=time.Now();w:=poiTestRequest(h,"GET",path,map[string]string{"Origin":poiOrigin});dur<-float64(time.Since(s))/float64(time.Millisecond);if w.Code!=200||w.Body.String()!=expected[i]{t.Errorf("concurrent mismatch query %d: %d",i,w.Code)}}()}
  wg.Wait();close(dur);for v:=range dur{concurrent=append(concurrent,v)}
 }
 sort.Float64s(times);sort.Float64s(concurrent)
 t.Logf("POI HTTP in-process: open_ms=%.3f sequential=%d p95_ms=%.3f concurrent=%d p95_ms=%.3f bytes=%d parity=pass",openMS,len(times),times[len(times)-1],len(concurrent),concurrent[30],total)
}

func TestPOIOptionalWebsite(t *testing.T){
 p:=poiTestService(func(context.Context,poigrid.Query)(poigrid.Result,error){return poigrid.Result{Records:[]json.RawMessage{poiTestRecord("coffee","poi-ovt-a"),poiTestRecord("coffee","poi-ovt-b")}},nil})
 p.websites=map[string]string{"poi-ovt-a":"https://vendor.com"};p.websiteDigest=strings.Repeat("b",64)
 w:=poiTestRequest(p,"GET",poiTestBBox,nil);var out poiBBoxResponse
 if w.Code!=200||json.Unmarshal(w.Body.Bytes(),&out)!=nil||len(out.Records)!=2||out.Records[0].Website!="https://vendor.com"||out.Records[1].Website!=""||!strings.Contains(*out.DatasetVersion,"-web-"){t.Fatal(w.Code,w.Body.String())}
 var raw struct{Records []map[string]json.RawMessage};if json.Unmarshal(w.Body.Bytes(),&raw)!=nil||len(raw.Records[1])!=2{t.Fatal("missing URL must be omitted")}
}
