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
 profileEndpoint string
 key string
 presence map[string]activityPresence
 inFlight int
 lastInteraction time.Time
}
type activityPresence struct {
 seen time.Time
 interacted time.Time
 visible bool
 sessionKey string
 userKey string
 username string
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
 UniqueUsers int `json:"uniqueUsers,omitempty"`
 Users []activityUserSummary `json:"users,omitempty"`
 InFlight int `json:"inFlight"`
 LastInteraction string `json:"lastInteraction,omitempty"`
}
type activityUserSummary struct {
 User string `json:"user"`
 Sessions int `json:"sessions"`
 LastSeen string `json:"lastSeen"`
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
  endpoint:endpoint,profileEndpoint:strings.TrimSuffix(endpoint,"/api/admin/activity/ingest")+"/api/session/profile",key:key,presence:make(map[string]activityPresence),
 },nil
}

func (a *ActivityReporter) verifyHandoff(ctx context.Context,bearer string) (string,string,string,bool) {
 if len(bearer)!=64 || strings.Trim(bearer,"0123456789abcdef")!="" {return "","","",true}
 req,err:=http.NewRequestWithContext(ctx,http.MethodGet,a.profileEndpoint,nil)
 if err!=nil{return "","","",false}
 req.Header.Set("Origin","https://weather.themauris.org")
 req.Header.Set("Authorization","Bearer "+bearer)
 resp,err:=a.client.Do(req)
 if err!=nil{return "","","",false}
 defer resp.Body.Close()
 if resp.StatusCode==http.StatusUnauthorized || resp.StatusCode==http.StatusForbidden {return "","","",true}
 if resp.StatusCode!=http.StatusOK{return "","","",false}
 var profile struct {User string `json:"user"`; ActivitySession string `json:"activitySession"`; ActivityUser string `json:"activityUser"`}
 if json.NewDecoder(io.LimitReader(resp.Body,2048)).Decode(&profile)!=nil{return "","","",false}
 if len(profile.ActivitySession)!=64 || len(profile.ActivityUser)!=64 || profile.User=="" || len(profile.User)>128 ||
 strings.Trim(profile.ActivitySession,"0123456789abcdef")!="" || strings.Trim(profile.ActivityUser,"0123456789abcdef")!="" {return "","","",false}
 return profile.ActivitySession,profile.ActivityUser,profile.User,true
}

func (a *ActivityReporter) identity(w http.ResponseWriter,r *http.Request) {
 w.Header().Set("Cache-Control","no-store")
 if r.Method!=http.MethodGet {w.Header().Set("Allow","GET");http.Error(w,"method not allowed",405);return}
 if origin:=r.Header.Get("Origin");origin!="" && origin!="http://"+r.Host && origin!="https://"+r.Host {http.Error(w,"invalid origin",403);return}
 state,user:="guest",""
 if authorization:=r.Header.Get("Authorization");strings.HasPrefix(authorization,"Bearer ") {
  bearer:=strings.TrimPrefix(authorization,"Bearer ")
  session,_,name,available:=a.verifyHandoff(r.Context(),bearer)
  if !available {http.Error(w,"identity validation unavailable",503);return}
  if session!="" {state,user="signed-in",name}
 }
 w.Header().Set("Content-Type","application/json")
 _=json.NewEncoder(w).Encode(map[string]string{"state":state,"user":user})
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
 sessionKey,userKey,username:="","",""
 if authorization:=r.Header.Get("Authorization");strings.HasPrefix(authorization,"Bearer ") {
  bearer:=strings.TrimPrefix(authorization,"Bearer ")
  var available bool
  sessionKey,userKey,username,available=a.verifyHandoff(r.Context(),bearer)
  if !available {http.Error(w,"Homepage identity validation unavailable",503);return}
  if sessionKey=="" {a.mu.Lock();delete(a.presence,p.ID);a.mu.Unlock();http.Error(w,"Homepage session unauthorized",401);return}
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
 past.sessionKey=sessionKey
 past.userKey=userKey
 past.username=username
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
 identities:=make(map[string]activityPresence,len(a.presence))
 for id,v:=range a.presence {
  age:=now.Sub(v.seen)
  if age>time.Hour {delete(a.presence,id);continue}
  if age<0 {continue}
  key:="guest:"+id
  if v.sessionKey!="" {key="session:"+v.sessionKey}
  if previous,exists:=identities[key];!exists || v.seen.After(previous.seen) {identities[key]=v}
 }
 users:=make(map[string]bool)
 summaries:=make(map[string]activityUserSummary)
 for _,v:=range identities {
  age:=now.Sub(v.seen)
  p.Active60++
  if v.sessionKey!="" && v.username!="" {
   summary:=summaries[v.username];summary.User=v.username
   seen:=v.seen.UTC().Format(time.RFC3339)
   if seen>summary.LastSeen {summary.LastSeen=seen}
   if age<=2*time.Minute {summary.Sessions++}
   summaries[v.username]=summary
  }
  if age<=15*time.Minute {p.Active15++}
  if age<=5*time.Minute {p.Active5++}
  if age<=2*time.Minute {
   p.Active2++
   if v.visible {p.Foreground++}else{p.Background++}
   if v.sessionKey!="" {p.Authenticated++;users[v.userKey]=true}else{p.Anonymous++}
  }
 }
 p.UniqueUsers=len(users)
 for _,summary:=range summaries {p.Users=append(p.Users,summary)}
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
