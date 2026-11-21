package main

import (
 "fmt"
 "net/http"
 "net/http/httptest"
 "os"
 "strings"
 "testing"
 "time"
)

func TestSailingHTTPAuthLiveGuardAndBrowserHandoff(t *testing.T) {
 token:="Bearer "+strings.Repeat("b",64)
 role:="guest"
 identity:="guest-test"
 status:=http.StatusOK
 expired:=false
 calls:=0
 authority:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  calls++
  if r.Method!=http.MethodGet||r.URL.Path!="/api/session/profile"||
     r.Header.Get("Authorization")!=token||r.Header.Get("Origin")!=sailingSuiteOrigin{
   t.Errorf("invalid Homepage verifier request: %v %s",r.Method,r.URL.Path)
  }
  if status!=200{w.WriteHeader(status);return}
  expires:=time.Now().Add(time.Hour)
  if expired{expires=time.Now().Add(-time.Minute)}
  fmt.Fprintf(w,`{"user":%q,"role":%q,"expiresAt":%q}`,identity,role,expires.UTC().Format(time.RFC3339))
 }))
 defer authority.Close()
 gate:=&SailingSuiteGate{ProfileURL:authority.URL+"/api/session/profile",Client:authority.Client()}
 session,err:=NewSailingHTTPAuth(gate)
 if err!=nil{t.Fatal(err)}
 routeCalls:=0
 handler:=session.ProtectWeather(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  routeCalls++
  w.Header().Set("Content-Type","application/json")
  fmt.Fprintf(w,`{"path":%q,"data":"protected-weather"}`,r.URL.Path)
 }))
 do:=func(method,path,header string,cookie *http.Cookie,origin string)*httptest.ResponseRecorder{
  t.Helper()
  req:=httptest.NewRequest(method,"https://weather.themauris.org"+path,nil)
  req.Header.Set("X-User","admin")
  req.Header.Set("X-Role","admin")
  if header!=""{req.Header.Set("Authorization",header)}
  if cookie!=nil{req.AddCookie(cookie)}
  if origin!=""{req.Header.Set("Origin",origin)}
  rec:=httptest.NewRecorder()
  handler.ServeHTTP(rec,req)
  return rec
 }
 // Only the small sign-in bootstrap is returned without authentication, not
 // the weather HTML (which embeds live reports and map/NOAA information).
 for _,path:=range []string{"/","/planning","/report?format=html"}{
  r:=do("GET",path,"",nil,"")
  if r.Code!=200||!strings.Contains(r.Body.String(),"/suite/session")||
     strings.Contains(r.Body.String(),"protected-weather")||r.Header().Get("Cache-Control")!="no-store"{
   t.Errorf("%s anonymous bootstrap incorrect: %d %s",path,r.Code,r.Body.String())
  }
 }
 for _,path:=range []string{
  "/report?format=json","/report?format=text","/voice",
  "/wind-readings","/wind-stations","/point-weather","/marine-forecast",
  "/smoke-overlay","/fire-perimeters","/swell-forecast",
  "/assets/marine_places.json","/api/future","/api/activity/identity",
 }{
  r:=do("GET",path,"",nil,"")
  if r.Code!=401{t.Errorf("unprotected %s: got %d want 401",path,r.Code)}
 }
 // AJAX chart refresh must receive 401, never a login HTML page that
 // its DOM parser might mistake for a successful weather report.
 ajax:=httptest.NewRequest("GET","https://weather.themauris.org/report?format=html",nil)
 ajax.Header.Set("X-Requested-With","current-date-controls")
 ajaxResult:=httptest.NewRecorder()
 handler.ServeHTTP(ajaxResult,ajax)
 if ajaxResult.Code!=401 {t.Fatalf("anonymous AJAX chart status %d",ajaxResult.Code)}
 if routeCalls!=0||calls!=0{t.Fatal("anonymous weather data reached handler or authority")}
 for _,path:=range []string{"/health","/welcome","/assets/tanner.jpg","/assets/hero.jpg","/activity.js"}{
  if r:=do("GET",path,"",nil,"");r.Code!=200{t.Errorf("public asset %s blocked: %d",path,r.Code)}
 }
 if routeCalls!=5 {t.Fatalf("public route count %d",routeCalls)}
 if r:=do("POST","/suite/session",token,nil,"https://attacker.example");r.Code!=403{
  t.Fatalf("cross-site session creation status %d",r.Code)
 }
 if r:=do("GET","/suite/session",token,nil,"");r.Code!=405{t.Fatalf("GET handoff status %d",r.Code)}
 if r:=do("POST","/suite/session","Bearer broken",nil,sailingSuiteOrigin);r.Code!=401{
  t.Fatalf("malformed bearer status %d",r.Code)
 }
 loggedIn:=do("POST","/suite/session",token,nil,sailingSuiteOrigin)
 if loggedIn.Code!=204{t.Fatalf("browser handoff status %d %s",loggedIn.Code,loggedIn.Body.String())}
 cookies:=loggedIn.Result().Cookies()
 if len(cookies)!=1{t.Fatalf("expected one sealed cookie, got %d",len(cookies))}
 cookie:=cookies[0]
 if cookie.Name!=sailingSessionCookie||cookie.Value==""||!cookie.Secure||!cookie.HttpOnly||
    cookie.SameSite!=http.SameSiteLaxMode||cookie.Path!="/"{
  t.Fatalf("invalid HttpOnly cookie attributes: %+v",cookie)
 }
 if strings.Contains(cookie.Value,strings.Repeat("b",64)){t.Fatal("bearer leaked in visible cookie value")}
 for _,role=range []string{"user","admin","guest"}{
  for _,path:=range []string{"/report?format=json","/report?format=html","/planning","/wind-readings","/api/future","/assets/marine_places.json"}{
   r:=do("GET",path,"",cookie,"")
   if r.Code!=200||!strings.Contains(r.Body.String(),"protected-weather"){
    t.Errorf("verified %s blocked %s: %d",role,path,r.Code)
   }
  }
 }
 // Cross-origin cookie writes must be rejected even after login.
 req:=httptest.NewRequest("POST","https://weather.themauris.org/api/activity/identity",nil)
 req.AddCookie(cookie)
 req.Header.Set("Origin","https://attacker.example")
 denied:=httptest.NewRecorder()
 handler.ServeHTTP(denied,req)
 if denied.Code!=403{t.Fatalf("cross-origin cookie write status %d",denied.Code)}
 req=httptest.NewRequest("POST","https://weather.themauris.org/api/activity/identity",nil)
 req.AddCookie(cookie)
 req.Header.Set("Origin",sailingSuiteOrigin)
 accepted:=httptest.NewRecorder()
 handler.ServeHTTP(accepted,req)
 if accepted.Code!=200{t.Fatalf("same-origin cookie write status %d",accepted.Code)}
 // A CLI bearer is verified per-request, too, without a browser cookie.
 if r:=do("GET","/wind-readings",token,nil,"");r.Code!=200{t.Fatalf("direct API bearer %d",r.Code)}
 tampered:=*cookie
 tampered.Value=tampered.Value+"invalid"
 if r:=do("GET","/wind-readings","",&tampered,"");r.Code!=401{t.Fatalf("tampered cookie status %d",r.Code)}
 // Verification must not trust earlier identity: revoked/expired/invalid
 // credentials block ALL routes even with an existing encrypted cookie.
 for _,state:=range []struct{code int;role string;expired bool;want int}{
  {401,"guest",false,401},{503,"guest",false,503},{200,"unknown",false,401},{200,"guest",true,401},
 }{
  status,role,expired=state.code,state.role,state.expired
  if r:=do("GET","/wind-readings","",cookie,"");r.Code!=state.want{
   t.Errorf("revoked/expired identity status %d want %d",r.Code,state.want)
  }
 }
 if calls<20{t.Fatalf("too few independent Homepage verifications: %d",calls)}
}

func TestSailingHTTPAuthLocalCookieAndFailClosed(t *testing.T) {
 session,err:=NewSailingHTTPAuth(&SailingSuiteGate{ProfileURL:"http://127.0.0.1:1/api/session/profile",Client:&http.Client{Timeout:time.Millisecond*200}})
 if err!=nil{t.Fatal(err)}
 handler:=session.ProtectWeather(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){t.Fatal("unverified request reached report")}))
 r:=httptest.NewRequest("GET","http://127.0.0.1:9321/report?format=json",nil)
 r.Header.Set("Authorization","Bearer "+strings.Repeat("f",64))
 w:=httptest.NewRecorder()
 handler.ServeHTTP(w,r)
 if w.Code!=503{t.Fatalf("Homepage outage status %d",w.Code)}
 if sailingCookieName(r)!=sailingLocalSessionCookie{t.Fatal("loopback cookie name incorrect")}
 if !sailingBootstrapRoute(httptest.NewRequest("GET","http://127.0.0.1:9321/report?format=html",nil)){t.Fatal("browser HTML bootstrap missing")}
 if sailingBootstrapRoute(httptest.NewRequest("GET","http://127.0.0.1:9321/report?format=json",nil)){t.Fatal("JSON must not get login bootstrap")}
}

func TestSailingProductionListenerInstallsHTTPAuthorization(t *testing.T) {
 raw,err:=os.ReadFile("main.go")
 if err!=nil{t.Fatal(err)}
 script:=string(raw)
 for _,required:=range []string{
  "NewSailingHTTPAuth(NewSailingSuiteGate())",
  "handler=authGate.ProtectWeather(handler)",
  "http.ListenAndServe(addr, handler)",
 }{
  if !strings.Contains(script,required){t.Errorf("production route guard absent: %s",required)}
 }
 if strings.Index(script,"handler=authGate.ProtectWeather(handler)")>strings.Index(script,"http.ListenAndServe(addr, handler)"){
  t.Fatal("production listener starts before guard installation")
 }
}
