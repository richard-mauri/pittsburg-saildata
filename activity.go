package main

import (
 "bytes"
 "os"
 _ "embed"
 "context"
 "crypto/rand"
 "encoding/hex"
 "encoding/json"
 "errors"
 "io"
 "net/http"
 "net/url"
 "strings"
 "strconv"
 "sync"
 "time"
)

//go:embed web/static/activity.js
var sailingActivityJS string

// Reporters are created only for HTTP server mode, never command-line reports.
var sailingActivity *ActivityReporter

func sailingActivityConfigured() bool {
  reporter,err:=NewActivityReporter(os.Getenv("SAILING_ACTIVITY_URL"),os.Getenv("HOMEPAGE_ACTIVITY_KEY_SAILING"))
  return err==nil && reporter!=nil
}

// ActivityReporter is an optional, best-effort, server-only adapter to the
// Homepage collector. It never sends URLs, account tokens, or visitor IDs.
type ActivityReporter struct {
 mu sync.Mutex
 now func() time.Time
 client *http.Client
 endpoint string
 key string
 presence map[string]activityPresence
 inFlight int
 lastInteraction time.Time
}
type activityPresence struct {
 seen time.Time
 interacted time.Time
 visible bool
}
type activityPing struct {
 ID string `json:"id"`
 Visible bool `json:"visible"`
 Interacted bool `json:"interacted"`
}
type activityPayload struct {
 Active2 int `json:"active2"`
 Active5 int `json:"active5"`
 Active15 int `json:"active15"`
 Active60 int `json:"active60"`
 Foreground int `json:"foreground"`
 Background int `json:"background"`
 Anonymous int `json:"anonymous"`
 Authenticated int `json:"authenticated"`
 InFlight int `json:"inFlight"`
 LastInteraction string `json:"lastInteraction,omitempty"`
}

// NewActivityReporter activates telemetry only with an explicit URL and a
// private >=32-character service key. Limit plain HTTP to loopback.
func NewActivityReporter(endpoint,key string) (*ActivityReporter,error) {
 if endpoint=="" && key=="" {return nil,nil}
 if endpoint=="" || len(key)<32 {return nil,errors.New("activity reporter requires a collector URL and a server-only key of at least 32 characters")}
 u,err:=url.Parse(endpoint)
 if err!=nil || u.User!=nil || u.RawQuery!="" || u.Fragment!="" || u.Path!="/api/admin/activity/ingest" || u.Host=="" {
  return nil,errors.New("invalid Homepage activity collector URL")
 }
 if u.Scheme!="https" && !(u.Scheme=="http" && (u.Hostname()=="127.0.0.1" || u.Hostname()=="localhost" || u.Hostname()=="::1")) {
  return nil,errors.New("activity collector must use HTTPS or loopback HTTP")
 }
 return &ActivityReporter{
  now:time.Now,client:&http.Client{Timeout:3*time.Second,CheckRedirect:func(*http.Request,[]*http.Request)error{return http.ErrUseLastResponse}},
  endpoint:endpoint,key:key,presence:make(map[string]activityPresence),
 },nil
}

func (a *ActivityReporter) ping(w http.ResponseWriter,r *http.Request) {
 w.Header().Set("Cache-Control","no-store")
 w.Header().Set("X-Content-Type-Options","nosniff")
 if r.Method!=http.MethodPost {w.Header().Set("Allow","POST");http.Error(w,"method not allowed",405);return}
 if r.Header.Get("Origin")!="http://"+r.Host && r.Header.Get("Origin")!="https://"+r.Host {
  http.Error(w,"origin required",403);return
 }
 if r.Header.Get("Content-Type")!="application/json" {http.Error(w,"JSON required",415);return}
 r.Body=http.MaxBytesReader(w,r.Body,256)
 var p activityPing
 dec:=json.NewDecoder(r.Body);dec.DisallowUnknownFields()
 if err:=dec.Decode(&p);err!=nil{http.Error(w,"invalid activity ping",400);return}
 var extra interface{}
 if dec.Decode(&extra)!=io.EOF || len(p.ID)!=32 || strings.Trim(p.ID,"0123456789abcdef")!=""{
  http.Error(w,"invalid activity ID",400);return
 }
 now:=a.now()
 a.mu.Lock()
 defer a.mu.Unlock()
 for id,past:=range a.presence {if now.Sub(past.seen)>time.Hour{delete(a.presence,id)}}
 past,exists:=a.presence[p.ID]
 if !exists && len(a.presence)>=5000{http.Error(w,"activity capacity reached",503);return}
 if exists && now.Sub(past.seen)<15*time.Second {w.WriteHeader(204);return}
 past.seen=now
 past.visible=p.Visible
 if p.Interacted && p.Visible {past.interacted=now;a.lastInteraction=now}
 a.presence[p.ID]=past
 w.WriteHeader(204)
}

func (a *ActivityReporter) working() func(){
 a.mu.Lock();a.inFlight++;a.mu.Unlock()
 return func(){a.mu.Lock();a.inFlight--;a.mu.Unlock()}
}

func (a *ActivityReporter) snapshot() activityPayload {
 a.mu.Lock();defer a.mu.Unlock()
 now:=a.now()
 p:=activityPayload{InFlight:a.inFlight}
 for id,v:=range a.presence {
  age:=now.Sub(v.seen)
  if age>time.Hour {delete(a.presence,id);continue}
  if age<0 {continue}
  if age<=time.Hour {p.Active60++}
  if age<=15*time.Minute {p.Active15++}
  if age<=5*time.Minute {p.Active5++}
  if age<=2*time.Minute {
   p.Active2++
   if v.visible {p.Foreground++}else{p.Background++}
   // A SailingGo tab is anonymous/unverified until its Homepage identity is
   // verified by the server. Never trust browser-provided role flags.
   p.Anonymous++
  }
 }
 if !a.lastInteraction.IsZero(){p.LastInteraction=a.lastInteraction.UTC().Format(time.RFC3339)}
 return p
}

func (a *ActivityReporter) report(ctx context.Context) error {
 p:=a.snapshot()
 body,err:=json.Marshal(p);if err!=nil{return err}
 rawNonce:=make([]byte,16)
 if _,err=rand.Read(rawNonce);err!=nil{return err}
 request,err:=http.NewRequestWithContext(ctx,http.MethodPost,a.endpoint,bytes.NewReader(body))
 if err!=nil{return err}
 request.Header.Set("Content-Type","application/json")
 request.Header.Set("Authorization","Bearer "+a.key)
 request.Header.Set("X-Activity-Service","sailing")
 request.Header.Set("X-Activity-Timestamp",strconvFormatUnix(time.Now().Unix()))
 request.Header.Set("X-Activity-Nonce",hex.EncodeToString(rawNonce))
 response,err:=a.client.Do(request);if err!=nil{return err}
 defer response.Body.Close()
 io.Copy(io.Discard,io.LimitReader(response.Body,1024))
 if response.StatusCode!=http.StatusNoContent{return errors.New("activity collector rejected report")}
 return nil
}
func strconvFormatUnix(ts int64) string {return strconv.FormatInt(ts,10)}

// Run emits periodic independent heartbeat snapshots, including zeros when
// visitors leave, and exits when SailingGo shuts down. Failures never block feeds.
func (a *ActivityReporter) Run(ctx context.Context) {
 if a==nil{return}
 ticker:=time.NewTicker(60*time.Second)
 defer ticker.Stop()
 for {
  select {
  case <-ctx.Done():return
  case <-ticker.C:
   _=a.report(ctx)
  }
 }
}
