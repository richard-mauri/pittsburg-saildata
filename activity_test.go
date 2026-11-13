package main

import (
 "context"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "os"
 "strings"
 "testing"
 "time"
)

func TestSailingReporterConfigurationAndUI(t *testing.T) {
 if r,err:=NewActivityReporter("","");err!=nil || r!=nil {t.Fatalf("disabled reporter: %v",err)}
 for _,bad:=range []string{"http://example.org/api/admin/activity/ingest","http://127.0.0.1:8788/incorrect","https://u:p@example.org/api/admin/activity/ingest"}{
  if _,err:=NewActivityReporter(bad,strings.Repeat("k",40));err==nil {t.Errorf("unsafe collector accepted %s",bad)}
 }
 if _,err:=NewActivityReporter("http://127.0.0.1:8788/api/admin/activity/ingest","weak");err==nil{t.Fatal("weak secret accepted")}
 t.Setenv("SAILING_ACTIVITY_URL","")
 t.Setenv("HOMEPAGE_ACTIVITY_KEY_SAILING","")
 disabled:=readSailingUITemplate("web/templates/sailing.html")
 if strings.Contains(disabled,"/activity.js"){t.Fatal("browser heartbeat leaked without server configuration")}
 t.Setenv("SAILING_ACTIVITY_URL","http://127.0.0.1:8788/api/admin/activity/ingest")
 t.Setenv("HOMEPAGE_ACTIVITY_KEY_SAILING",strings.Repeat("k",40))
 for _,name:=range []string{"welcome.html","sailing.html","details.html","stations.html"} {
  page:=readSailingUITemplate("web/templates/"+name)
  if !strings.Contains(page,"/activity.js"){t.Errorf("missing heartbeat in %s",name)}
 }
 if strings.Contains(sailingActivityJS,"HOMEPAGE_ACTIVITY_KEY_SAILING"){t.Fatal("private key in browser code")}
}

func TestSailingPresenceRollingWindowsAndInFlight(t *testing.T){
 a,_:=NewActivityReporter("http://127.0.0.1:8788/api/admin/activity/ingest",strings.Repeat("k",40))
 now:=time.Now().UTC().Truncate(time.Second)
 a.now=func()time.Time{return now}
 ping:=func(origin string)int{
  r:=httptest.NewRequest("POST","http://127.0.0.1:8087/api/activity/ping",strings.NewReader(`{"id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","visible":true,"interacted":true}`))
  r.Header.Set("Origin",origin);r.Header.Set("Content-Type","application/json")
  w:=httptest.NewRecorder();a.ping(w,r);return w.Code
 }
 if got:=ping("https://evil.example");got!=403{t.Errorf("cross origin status %d",got)}
 if got:=ping("http://127.0.0.1:8087");got!=204{t.Fatalf("same origin status %d",got)}
 p:=a.snapshot()
 if p.Active2!=1||p.Active5!=1||p.Active15!=1||p.Active60!=1||p.Anonymous!=1||p.Foreground!=1||p.LastInteraction==""{t.Fatalf("unexpected rolling snapshot %+v",p)}
 end:=a.working()
 if p=a.snapshot();p.InFlight!=1{t.Fatalf("background or request work missing %+v",p)}
 end()
 if p=a.snapshot();p.InFlight!=0{t.Fatalf("work not released %+v",p)}
 now=now.Add(3*time.Minute)
 if p=a.snapshot();p.Active2!=0||p.Active5!=1{t.Fatalf("expired window wrong %+v",p)}
}

func TestSailingReporterHomepageWireProtocol(t *testing.T){
 secret:=strings.Repeat("z",40)
 var got activityPayload
 srv:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if r.Header.Get("X-Activity-Service")!="sailing" || r.Header.Get("Authorization")!="Bearer "+secret{t.Error("incorrect server-only authentication")}
  if r.Header.Get("X-Activity-Nonce")=="" || r.Header.Get("X-Activity-Timestamp")==""{t.Error("replay protection headers missing")}
  if err:=json.NewDecoder(r.Body).Decode(&got);err!=nil{t.Error(err)}
  w.WriteHeader(204)
 }))
 defer srv.Close()
 a,err:=NewActivityReporter(srv.URL+"/api/admin/activity/ingest",secret)
 if err!=nil{t.Fatal(err)}
 a.mu.Lock();a.presence[strings.Repeat("b",32)]=activityPresence{seen:time.Now(),visible:true};a.mu.Unlock()
 if err:=a.report(context.Background());err!=nil{t.Fatal(err)}
 if got.Active2!=1||got.Anonymous!=1||got.Active60!=1{t.Fatalf("aggregate mismatch %+v",got)}
}

func TestSailingReporterDoesNotMutateWeatherContracts(t *testing.T){
 if appVersion!="1.16.0"{t.Fatal("semantic app version changed")}
 if buildVersion!="v402"{t.Fatal("expected next development build lineage")}
 if os.Getenv("SAILING_ACTIVITY_URL")=="" && sailingActivityConfigured(){t.Fatal("unexpected reporter configuration")}
}
