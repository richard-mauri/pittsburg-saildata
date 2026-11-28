package main

import (
 "bytes"
 "context"
 "encoding/json"
 "fmt"
 "io"
 "net"
 "net/http"
 "os"
 "os/exec"
 "path/filepath"
 "runtime"
 "sort"
 "strconv"
 "strings"
 "sync"
 "testing"
 "time"
)

// Explicit owner-only test of the actual compiled server, never the live
// listener. No production environment is inherited and no tunnel is started.
type poiLoopback struct {
 base string
 client *http.Client
 pid int
 stop func()
 ready time.Duration
}
type poiWireResult struct {
 code int
 header http.Header
 body []byte
 elapsed time.Duration
}
func (s *poiLoopback) request(method,path string, headers map[string]string)(poiWireResult,error){
 var out poiWireResult
 req,e:=http.NewRequest(method,s.base+path,nil);if e!=nil{return out,e}
 for k,v:=range headers{req.Header.Set(k,v)}
 start:=time.Now()
 resp,e:=s.client.Do(req);if e!=nil{return out,e}
 defer resp.Body.Close()
 out.body,e=io.ReadAll(io.LimitReader(resp.Body,poiMaxBody+1))
 out.elapsed=time.Since(start);out.code=resp.StatusCode;out.header=resp.Header
 if e!=nil{return out,e};if len(out.body)>poiMaxBody{return out,fmt.Errorf("response body exceeded cap")}
 return out,nil
}
func (s *poiLoopback) check(t *testing.T,method,path string,headers map[string]string,want int)poiWireResult{
 t.Helper();v,e:=s.request(method,path,headers)
 if e!=nil||v.code!=want{t.Fatalf("%s %s: status=%d expected=%d error=%v",method,strings.SplitN(path,"?",2)[0],v.code,want,e)}
 return v
}
func poiChildRSS(t *testing.T,pid int)int64{
 t.Helper();ctx,cancel:=context.WithTimeout(context.Background(),3*time.Second);defer cancel()
 raw,e:=exec.CommandContext(ctx,"/bin/ps","-o","rss=","-p",strconv.Itoa(pid)).Output()
 if e!=nil{t.Fatal("child RSS unavailable",e)}
 n,e:=strconv.ParseInt(strings.TrimSpace(string(raw)),10,64);if e!=nil||n<=0{t.Fatal("invalid child RSS")}
 return n
}
func startPOILoopback(t *testing.T,binary,generation,lsof string)*poiLoopback{
 t.Helper()
 socket,e:=net.Listen("tcp4","127.0.0.1:0");if e!=nil{t.Fatal(e)}
 port:=strconv.Itoa(socket.Addr().(*net.TCPAddr).Port)
 if e=socket.Close();e!=nil{t.Fatal(e)}
 addr:="127.0.0.1:"+port
 scratch:=t.TempDir()
 logFile,e:=os.OpenFile(filepath.Join(scratch,"child.log"),os.O_CREATE|os.O_EXCL|os.O_WRONLY,0600);if e!=nil{t.Fatal(e)}
 defer logFile.Close()
 ctx,cancel:=context.WithTimeout(context.Background(),90*time.Second)
 t.Cleanup(cancel)
 cmd:=exec.CommandContext(ctx,binary,"-server","-port",port)
 cmd.Dir=scratch
 cmd.Env=[]string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin","HOME="+scratch,"TMPDIR="+scratch,
  "SAILING_ACTIVITY_TEST_LISTEN="+addr,"SAILING_ACTIVITY_URL=","HOMEPAGE_ACTIVITY_KEY_SAILING=",
  "SAILING_STREET_TILE_UPSTREAM=disabled-for-loopback-test","SAILING_NAUTICAL_TILE_DIR="+filepath.Join(scratch,"no-chart-cache"),
  "SAILING_POI_GENERATION="+generation}
 cmd.Stdout=logFile;cmd.Stderr=logFile
 start:=time.Now();if e=cmd.Start();e!=nil{t.Fatal(e)}
 done:=make(chan struct{})
 go func(){_ = cmd.Wait();close(done)}()
 transport:=&http.Transport{Proxy:nil,DisableCompression:true,MaxConnsPerHost:4,MaxIdleConnsPerHost:4,ResponseHeaderTimeout:3*time.Second}
 s:=&poiLoopback{base:"http://"+addr,pid:cmd.Process.Pid,client:&http.Client{Transport:transport,Timeout:4*time.Second,CheckRedirect:func(*http.Request,[]*http.Request)error{return http.ErrUseLastResponse}}}
 var once sync.Once
 s.stop=func(){once.Do(func(){transport.CloseIdleConnections();cancel();<-done})}
 t.Cleanup(s.stop)
 // Closing the temporary port reservation creates a small race. Never
 // send a request until lsof confirms this exact child owns the listener.
 deadline:=time.Now().Add(15*time.Second)
 for time.Now().Before(deadline){
  select{case <-done:t.Fatal("disposable server exited before readiness");default:}
  lc,cc:=context.WithTimeout(ctx,2*time.Second)
  raw,err:=exec.CommandContext(lc,lsof,"-nP","-a","-p",strconv.Itoa(s.pid),"-iTCP:"+port,"-sTCP:LISTEN","-Fn").Output();cc()
  if err==nil{
   for _,line:=range strings.Split(string(raw),"\n"){if line=="n"+addr{
    select{case <-done:t.Fatal("disposable server exited before health check");default:}
    v,err:=s.request("GET","/health",nil)
    if err==nil&&v.code==200&&strings.TrimSpace(string(v.body))=="OK"{s.ready=time.Since(start);return s}
   }}
  }
  time.Sleep(100*time.Millisecond)
 }
 t.Fatal("disposable loopback listener readiness/ownership not established")
 return nil
}
func poiLoopbackStatus(t *testing.T,v poiWireResult,enabled bool)poiStatus{
 t.Helper();var out poiStatus
 if json.Unmarshal(v.body,&out)!=nil||out.Version!=1||out.Available!=enabled{t.Fatal("unexpected dataset status")}
 if enabled{
  n:=0;for _,v:=range out.CategoryCounts{n+=v}
  if out.DatasetVersion==nil||n!=199475||len(out.CategoryCounts)!=7{t.Fatal("reviewed dataset count mismatch")}
 }else if out.DatasetVersion!=nil||v.header.Get("Cache-Control")!="no-store"{t.Fatal("disabled status exposed data or cache")}
 return out
}
func poiLoopbackCategories(t *testing.T,v poiWireResult,enabled bool){
 t.Helper();var c poiCategoriesResponse
 if json.Unmarshal(v.body,&c)!=nil||len(c.Categories)!=11{t.Fatal("invalid category registry")}
 for _,k:=range []string{"coffee","ev_charging","fuel","grocery","lodging","parking","restaurant"}{if c.Categories[k].Available!=enabled||c.Categories[k].NativeCategory!=k{t.Fatal("terrestrial availability mismatch",k)}}
 for _,k:=range []string{"marina","marine_fuel","pumpout","boat_ramp"}{if c.Categories[k].Available{t.Fatal("marine category exposed",k)}}
}
func poiLoopbackRecords(t *testing.T,v poiWireResult,empty bool)poiBBoxResponse{
 t.Helper();var out poiBBoxResponse
 if json.Unmarshal(v.body,&out)!=nil||out.Version!=1||out.DatasetVersion==nil||out.Records==nil||len(out.Records)>500{t.Fatal("invalid bbox response")}
 if empty!=(len(out.Records)==0){t.Fatal("unexpected empty/nonempty bbox")}
 prev:=""
 for _,r:=range out.Records{
  var w map[string]json.RawMessage;var wp struct{ID,Category string}
  if json.Unmarshal(r.Waypoint,&w)!=nil||len(w)!=7||json.Unmarshal(r.Waypoint,&wp)!=nil||wp.ID<=prev||len(r.ProviderCategories)!=1||r.ProviderCategories[0]!=wp.Category{t.Fatal("invalid waypoint identity/category/order")}
  for _,key:=range []string{"id","lat","lon","name","category","notes","created"}{if _,ok:=w[key];!ok{t.Fatal("missing waypoint key",key)}}
  prev=wp.ID
 }
 return out
}
// Only run after owner builds the verified branch. Ordinary tests and CI
// compile this test but skip actual child processes and private data.
func TestPOILoopbackOwner(t *testing.T){
 generation:=os.Getenv("SAILING_POI_TEST_GENERATION")
 if os.Getenv("SAILING_POI_LOOPBACK_TEST")!="1"{t.Skip("explicit owner loopback opt-in required")}
 if runtime.GOOS!="darwin"||generation==""{t.Fatal("requires owner Mac and generation path")}
 binary,e:=filepath.Abs("sailing-go");if e!=nil{t.Fatal(e)}
 st,e:=os.Lstat(binary);if e!=nil||!st.Mode().IsRegular()||st.Mode().Perm()&0111==0{t.Fatal("build the verified branch with devbuild.sh first")}
 generation,e=filepath.Abs(generation);if e!=nil{t.Fatal(e)}
 st,e=os.Lstat(generation);if e!=nil||!st.IsDir(){t.Fatal("generation unavailable")}
 lsof,e:=exec.LookPath("lsof");if e!=nil{lsof="/usr/sbin/lsof";if _,e=os.Stat(lsof);e!=nil{t.Fatal("lsof required to verify listener ownership")}}
 disabled:=startPOILoopback(t,binary,"",lsof)
 disabled.check(t,"GET","/health",nil,200)
 poiLoopbackStatus(t,disabled.check(t,"GET",poiStatusPath,nil,200),false)
 poiLoopbackCategories(t,disabled.check(t,"GET",poiCategoriesPath,nil,200),false)
 v:=disabled.check(t,"GET",poiTestBBox,map[string]string{"If-None-Match":"*"},503)
 if v.header.Get("Cache-Control")!="no-store"||v.header.Get("ETag")!=""{t.Fatal("disabled bbox cache mismatch")}
 baseline:=poiChildRSS(t,disabled.pid)
 disabled.stop()
 enabled:=startPOILoopback(t,binary,generation,lsof)
 enabled.check(t,"GET","/health",nil,200)
 poiLoopbackStatus(t,enabled.check(t,"GET",poiStatusPath,nil,200),true)
 poiLoopbackCategories(t,enabled.check(t,"GET",poiCategoriesPath,nil,200),true)
 initial:=poiChildRSS(t,enabled.pid)
 t.Logf("POI loopback startup (includes readiness probe): disabled_ms=%.3f enabled_ms=%.3f disabled_rss_kib=%d enabled_initial_rss_kib=%d delta_kib=%d",float64(disabled.ready)/float64(time.Millisecond),float64(enabled.ready)/float64(time.Millisecond),baseline,initial,initial-baseline)
 cors:=map[string]string{"Origin":poiOrigin}
 v=enabled.check(t,"GET",poiTestBBox,cors,200);poiLoopbackRecords(t,v,false)
 if v.header.Get("Access-Control-Allow-Origin")!=poiOrigin||v.header.Get("Access-Control-Allow-Credentials")!=""||v.header.Get("ETag")==""||v.header.Get("Cache-Control")!="public, max-age=60"||!strings.Contains(strings.Join(v.header.Values("Vary"),","),"Origin"){t.Fatal("CORS/cache headers")}
 tag:=v.header.Get("ETag")
 v=enabled.check(t,"GET",poiTestBBox,map[string]string{"Origin":poiOrigin,"If-None-Match":tag},304)
 if len(v.body)!=0||v.header.Get("ETag")!=tag{t.Fatal("conditional body/validator")}
 v=enabled.check(t,"OPTIONS",poiTestBBox,map[string]string{"Origin":poiOrigin,"Access-Control-Request-Method":"GET","Access-Control-Request-Headers":"If-None-Match"},204)
 if v.header.Get("Access-Control-Allow-Origin")!=poiOrigin||v.header.Get("Access-Control-Allow-Credentials")!=""{t.Fatal("preflight headers")}
 v=enabled.check(t,"GET",poiStatusPath,map[string]string{"Origin":"https://denied.example"},403);if v.header.Get("Access-Control-Allow-Origin")!=""{t.Fatal("denied CORS")}
 enabled.check(t,"OPTIONS",poiStatusPath,map[string]string{"Origin":poiOrigin,"Access-Control-Request-Method":"GET","Access-Control-Request-Headers":"Authorization"},403)
 enabled.check(t,"GET",poiBBoxPath+"?bbox=-125,32,-114,42&category=coffee",nil,400)
 v=enabled.check(t,"GET",poiAttributionPath,cors,200)
 if !bytes.Equal(v.body,poiAttribution){t.Fatal("attribution text mismatch")}
 for _,path:=range []string{"/api/poi/unknown","/api/poi/status/","/api/poi/%73tatus","/api/charts/tiles/3/2/4.png","/assets/marine_places.json","/point-weather","/api/activity/identity"}{enabled.check(t,"GET",path,nil,401)}
 enabled.check(t,"POST",poiStatusPath,nil,401)
 enabled.check(t,"HEAD",poiStatusPath,nil,401)
 // Street remains public, but the child uses an invalid upstream setting
 // and must fail closed locally without a real tile fetch.
 enabled.check(t,"GET","/api/maps/street/3/2/4.png",nil,503)
 time.Sleep(2100*time.Millisecond)
 paths:=[]string{poiTestBBox,
 poiBBoxPath+"?bbox=-122.43,37.77,-122.39,37.81&category=restaurant&category=coffee&category=grocery&category=parking&limit=500",
 poiBBoxPath+"?bbox=-118.5,34,-118.1,34.3&category=restaurant&category=coffee&limit=500",
 poiBBoxPath+"?bbox=-121.94,38,-121.84,38.06&category=fuel&category=restaurant",
 poiBBoxPath+"?bbox=-120.58,38.16,-120.49,38.24&category=grocery&category=fuel",
 poiBBoxPath+"?bbox=-117.23,32.66,-117.10,32.76&category=parking&category=ev_charging",
 poiBBoxPath+"?bbox=-110,35,-109.9,35.1&category=coffee",
 poiBBoxPath+"?bbox=-122.43,37.77,-122.39,37.81&category=marina"}
 expected:=make([][]byte,len(paths));var seq,con []float64;total:=0
 for i,path:=range paths{
  v:=enabled.check(t,"GET",path,cors,200)
  // Sparse Sierra may legitimately be empty; verify actual dense and
  // explicitly outside/unavailable examples separately.
  empty:=i>=6
  if i==4{var out poiBBoxResponse;if json.Unmarshal(v.body,&out)!=nil{t.Fatal("sparse response")};empty=len(out.Records)==0}
  out:=poiLoopbackRecords(t,v,empty)
  if i==7&&(len(out.UnavailableCategories)!=1||out.UnavailableCategories[0]!="marina"){t.Fatal("marine availability")}
  expected[i]=v.body;seq=append(seq,float64(v.elapsed)/float64(time.Millisecond));total+=len(v.body)
 }
 time.Sleep(2100*time.Millisecond)
 for i,path:=range paths{
  var wg sync.WaitGroup;results:=make(chan poiWireResult,4)
  for j:=0;j<4;j++{wg.Add(1);go func(){defer wg.Done();v,e:=enabled.request("GET",path,cors);if e!=nil||v.code!=200||!bytes.Equal(v.body,expected[i]){t.Errorf("concurrent query %d status=%d error=%v",i,v.code,e)};results<-v}()}
  wg.Wait();close(results)
  for v:=range results{con=append(con,float64(v.elapsed)/float64(time.Millisecond))}
  // Let the actual token bucket refill; do not reset server state.
  time.Sleep(550*time.Millisecond)
 }
 sort.Float64s(seq);sort.Float64s(con)
 after:=poiChildRSS(t,enabled.pid)
 t.Logf("POI loopback HTTP: sequential=%d p95_ms=%.3f concurrent=%d p95_ms=%.3f bytes=%d enabled_after_queries_rss_kib=%d delta_kib=%d",len(seq),seq[len(seq)-1],len(con),con[30],total,after,after-baseline)
 time.Sleep(2100*time.Millisecond)
 successes,limited:=0,0
 for i:=0;i<32;i++{
  v,e:=enabled.request("GET",poiStatusPath,nil);if e!=nil{t.Fatal(e)}
  switch v.code{case 200:successes++;case 429:limited++;if v.header.Get("Retry-After")!="1"||v.header.Get("Cache-Control")!="no-store"{t.Fatal("rate-limit headers")};default:t.Fatal("unexpected rate-test status",v.code)}
 }
 if limited==0{t.Fatal("bounded loopback burst did not exercise rate limiting")}
 t.Logf("POI loopback burst: ok=%d limited=%d",successes,limited)
 if t.Failed(){t.Fatal("loopback parity failed")}
 enabled.stop()
 t.Log("POI loopback acceptance PASS; both disposable children stopped; no live service changes")
}
