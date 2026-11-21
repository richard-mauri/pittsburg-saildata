package main

import (
 "fmt"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
 "time"
)

func TestSailingSuiteGateFailClosed(t *testing.T){
 validBearer:="Bearer "+strings.Repeat("a",64)
 code:=http.StatusOK
 role:="guest"
 user:="guest-example"
 expired:=false
 hits:=0
 profile:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  hits++
  if r.URL.Path!="/api/session/profile"||r.Method!=http.MethodGet||
     r.Header.Get("Origin")!=sailingSuiteOrigin||
     r.Header.Get("Authorization")!=validBearer{
   t.Errorf("invalid Homepage profile request: %s",r.URL.Path)
  }
  if code!=http.StatusOK{w.WriteHeader(code);return}
  expires:=time.Now().Add(time.Hour)
  if expired{expires=time.Now().Add(-time.Hour)}
  fmt.Fprintf(w,`{"user":%q,"role":%q,"expiresAt":%q}`,user,role,expires.UTC().Format(time.RFC3339))
 }))
 defer profile.Close()
 gate:=&SailingSuiteGate{ProfileURL:profile.URL+"/api/session/profile",Client:profile.Client()}
 reached:=0
 handler:=gate.ProtectAPI(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){reached++;w.WriteHeader(http.StatusNoContent)}))
 check:=func(path,auth string,want int){
  t.Helper()
  req:=httptest.NewRequest(http.MethodGet,"https://weather.themauris.org"+path,nil)
  req.Header.Set("X-SailingGo-Role","admin")
  if auth!=""{req.Header.Set("Authorization",auth)}
  rec:=httptest.NewRecorder()
  handler.ServeHTTP(rec,req)
  if rec.Code!=want{t.Errorf("%q %q: got %d want %d",path,auth!="" ,rec.Code,want)}
  if (path=="/api"||strings.HasPrefix(path,"/api/"))&&rec.Header().Get("Cache-Control")!="no-store"{
   t.Errorf("protected API %s missing no-store",path)
  }
 }
 for _,path:=range []string{"/api","/api/activity/ping","/api/activity/identity","/api/future-weather-data"}{
  check(path,"",401)
  check(path,"Bearer broken",401)
  check(path,validBearer,204)
 }
 for _,role=range []string{"user","admin","guest"}{check("/api/future-weather-data",validBearer,204)}
 role="fake-admin";check("/api/future-weather-data",validBearer,401)
 role="user";user="";check("/api/future-weather-data",validBearer,401)
 user="reader";expired=true;check("/api/future-weather-data",validBearer,401)
 expired=false;code=401;check("/api/future-weather-data",validBearer,401)
 code=503;check("/api/future-weather-data",validBearer,503)
 code=200
 for _,path:=range []string{"/","/welcome","/assets/tanner.jpg","/health"}{check(path,"",204)}
 if hits==0||reached==0{t.Fatal("gate did not exercise both upstream verification and downstream handler")}
}

func TestSailingSuiteGateNetworkFailureAndDefault(t *testing.T){
 gate:=&SailingSuiteGate{ProfileURL:"http://127.0.0.1:1/api/session/profile",Client:&http.Client{Timeout:time.Second}}
 handler:=gate.ProtectAPI(http.HandlerFunc(func(http.ResponseWriter,*http.Request){t.Fatal("unverified request reached handler")}))
 req:=httptest.NewRequest(http.MethodGet,"https://weather.themauris.org/api/weather",nil)
 req.Header.Set("Authorization","Bearer "+strings.Repeat("b",64))
 w:=httptest.NewRecorder()
 handler.ServeHTTP(w,req)
 if w.Code!=503{t.Fatalf("network failure status %d",w.Code)}
 standard:=NewSailingSuiteGate()
 if standard.ProfileURL!=sailingHomepageProfileURL{t.Fatal("default profile URL changed")}
 if standard.Client.Timeout!=4*time.Second{t.Fatal("verification timeout changed")}
}
