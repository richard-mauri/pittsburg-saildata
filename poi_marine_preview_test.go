package main
import("context";"encoding/json";"net/http";"net/http/httptest";"os";"strings";"testing";"pittsburg-saildata/internal/poigrid")
func TestMarinePreviewAsset(t *testing.T){
 token:=strings.Repeat("a",64);addr:="127.0.0.1:8766"
 m,e:=loadMarinePreview("assets/marine_places.json",addr,token)
 if e!=nil||len(m.rows)!=465{t.Fatal(e)}
 raw,e:=os.ReadFile("assets/marine_places.json");if e!=nil{t.Fatal(e)};raw[0]^=1
 if _,e=decodeMarinePreview(raw,addr,token);e==nil{t.Fatal("modified asset accepted")}
 if _,e=loadMarinePreview("assets/marine_places.json","0.0.0.0:8766",token);e==nil{t.Fatal("nonloopback accepted")}
 links:=0;for _,r:=range m.rows{if r.website!=""{links++}}
 if links==0||m.counts["pumpout"]!=0{t.Fatal("website or pumpout mismatch")}
 p:=poiTestService(func(_ context.Context,q poigrid.Query)(poigrid.Result,error){
  if len(q.Categories)!=1||q.Categories[0]!="coffee"{t.Fatal(q)}
  return poigrid.Result{Records:[]json.RawMessage{poiTestRecord("coffee","poi-ovt-a")}},nil
 })
 p.marine=m
 request:=func(path string,authorized bool)*httptest.ResponseRecorder{
  r:=httptest.NewRequest("GET","http://"+addr+path,nil);r.RemoteAddr="127.0.0.1:43210"
  if authorized{r.Header.Set(marinePreviewHeader,token)}
  w:=httptest.NewRecorder();p.ServeHTTP(w,r);return w
 }
 for _,path:=range poiPaths{if w:=request(path,false);w.Code!=403{t.Fatal(path,w.Code)}}
 w:=request(poiStatusPath,true);var s poiStatus
 if w.Code!=200||json.Unmarshal(w.Body.Bytes(),&s)!=nil||s.CategoryCounts["marina"]!=234||s.CategoryCounts["pumpout"]!=0||!strings.HasSuffix(*s.DatasetVersion,"-marine-"+marinePreviewDigest)||w.Header().Get("Cache-Control")!="no-store"{t.Fatal(w.Code,w.Body.String())}
 w=request(poiTestBBox+"&category=marina&category=pumpout",true);var out poiBBoxResponse
 if w.Code!=200||json.Unmarshal(w.Body.Bytes(),&out)!=nil||len(out.Records)<2||len(out.UnavailableCategories)!=1||out.UnavailableCategories[0]!="pumpout"{t.Fatal(w.Code,w.Body.String())}
 last:="";for _,r:=range out.Records{var wp poigrid.Waypoint;if json.Unmarshal(r.Waypoint,&wp)!=nil||wp.ID<=last||wp.Category!=r.ProviderCategories[0]{t.Fatal("order/category mismatch")};last=wp.ID}
 w=request(poiTestBBox+"&category=marina&limit=1",true);if w.Code!=200||json.Unmarshal(w.Body.Bytes(),&out)!=nil||!out.Truncated||len(out.Records)!=1{t.Fatal("global limit",w.Code)}
 w=request(poiAttributionPath,true);if w.Code!=200||!strings.Contains(w.Body.String(),"LOCAL MARINE STUDY ONLY")||w.Header().Get("Cache-Control")!="no-store"{t.Fatal("missing notice")}
 for _,host:=range []string{"weather.themauris.org","localhost:8766"}{r:=httptest.NewRequest(http.MethodGet,"http://"+host+poiStatusPath,nil);r.RemoteAddr="127.0.0.1:1234";r.Header.Set(marinePreviewHeader,token);w:=httptest.NewRecorder();p.ServeHTTP(w,r);if w.Code!=403{t.Fatal("host guard")}}
}
