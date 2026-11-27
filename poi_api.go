package main

import (
 "context"
 "crypto/sha256"
 _ "embed"
 "encoding/hex"
 "encoding/json"
 "errors"
 "log"
 "math"
 "net/http"
 "net/url"
 "regexp"
 "sort"
 "strconv"
 "strings"
 "sync"
 "sync/atomic"
 "time"

 "pittsburg-saildata/internal/poigrid"
)

const (
 poiOrigin = "https://gps.themauris.org"
 poiStatusPath = "/api/poi/status"
 poiCategoriesPath = "/api/poi/categories"
 poiBBoxPath = "/api/poi/bbox"
 poiAttributionPath = "/api/poi/attribution"
 poiRegistry = "ca-poi-v1"
 poiMaxBody = 512 << 10
 poiNotice = "Informational mapped locations only. Existence, access, hours and navigational suitability are not verified. Marine coverage is unavailable."
)
var poiPaths = []string{poiStatusPath,poiCategoriesPath,poiBBoxPath,poiAttributionPath}

// Only literal paths and read methods bypass authentication. No wildcard,
// decoded aliases, or normalization-based authorization.
func poiPublicRequest(r *http.Request) bool {
 if r.Method!=http.MethodGet && r.Method!=http.MethodOptions {return false}
 if r.URL.RawPath!="" || r.URL.EscapedPath()!=r.URL.Path {return false}
 for _,p:=range poiPaths {if r.URL.Path==p{return true}}
 return false
}
type poiCategory struct {
 Label string `json:"label"`
 Icon string `json:"icon"`
 Color string `json:"color"`
 Available bool `json:"available"`
 NativeCategory string `json:"native_category"`
}
func poiRegistryEntries(counts map[string]int, available bool) map[string]poiCategory {
 out:=map[string]poiCategory{}
 for _,v:=range []struct{key,label,icon,color string}{
 {"coffee","Coffee shop","☕","#92400e"},
 {"ev_charging","EV charging","🔌","#166534"},
 {"fuel","Gas station","⛽","#c2410c"},
 {"grocery","Grocery store","🛒","#15803d"},
 {"lodging","Lodging","🏨","#4f46e5"},
 {"parking","Parking","🅿️","#475569"},
 {"restaurant","Restaurant","🍴","#9333ea"},
 {"marina","Marina","M","#6d28d9"},
 {"boat_ramp","Boat ramp","🚤","#0891b2"},
 {"marine_fuel","Marine fuel","⛽","#0369a1"},
 {"pumpout","Pumpout","🚰","#0d9488"},
 } {out[v.key]=poiCategory{v.label,v.icon,v.color,available&&counts[v.key]>0,v.key}}
 return out
}
type poiSource struct {
 Name string `json:"name"`
 Release string `json:"release"`
 License string `json:"license"`
 Attribution string `json:"attribution"`
 URL string `json:"url"`
}
func poiSources() []poiSource {
 return []poiSource{
 {"Overture Maps Places",poigrid.Release,"CDLA-Permissive-2.0","Overture Maps Foundation and contributing sources","https://docs.overturemaps.org/attribution/"},
 {"Foursquare",poigrid.Release,"Apache-2.0","Foursquare Labs, Inc.; modified 2026-11-27","https://opensource.foursquare.com/places-notice-txt/"},
 {"AllThePlaces",poigrid.Release,"CC0-1.0","AllThePlaces contributors","https://www.alltheplaces.xyz/"},
 }
}

//go:embed docs/poi-licenses/CDLA-Permissive-2.0.txt
var poiCDLA string
//go:embed docs/poi-licenses/Apache-2.0.txt
var poiApache string
//go:embed docs/poi-licenses/CC0-1.0.txt
var poiCC0 string
//go:embed docs/poi-licenses/Foursquare-NOTICE.txt
var poiFoursquare string
var poiAttribution = []byte("SailingGo POI API — data attribution and full license texts\n"+
 "Overture Maps Foundation, Places release 2026-11-18.0. https://docs.overturemaps.org/attribution/\n"+
 "CDLA Permissive 2.0 sources in this generation: Meta, Microsoft, PinMeTo, RenderSEO, DAC, BrightQuery, Birdeye, Uberall, OpenTravel; Overture and Overture-signals carry CDLA metadata. Foursquare: Apache 2.0. AllThePlaces: CC0 1.0.\n"+
 "Foursquare copyright 2024 Foursquare Labs, Inc. All rights reserved (Overture attribution; Overture schema changes 2026-03-18). See full publisher NOTICE below.\n"+
 "SailingGo modifications dated 2026-11-27: selected California points using 2026 Census TIGER/Line state boundary, filtered confidence/status/name/license, mapped categories and encoded waypoint records and index. Source coordinates and identity retained. No endorsement or access/hours/navigation guarantee. Marine data not included.\n"+
 "Boundary reference: https://www.census.gov/geographies/mapping-files/time-series/geo/tiger-line-file.html\n\n"+
 "FOURSQUARE NOTICE\n"+poiFoursquare+"\n\nCDLA PERMISSIVE 2.0\n"+poiCDLA+"\n\nAPACHE 2.0\n"+poiApache+"\n\nCC0 1.0\n"+poiCC0)

type poiSearcher interface {
 Search(context.Context,poigrid.Query)(poigrid.Result,error)
 Close() error
}
type poiService struct {
 reader poiSearcher
 manifest poigrid.Manifest
 unavailable atomic.Bool
 slots chan struct{}
 mu sync.Mutex
 tokens float64
 last time.Time
 now func()time.Time
}
func newPOIService(reader poiSearcher,m poigrid.Manifest)*poiService{
 return &poiService{reader:reader,manifest:m,slots:make(chan struct{},4),tokens:16,last:time.Now(),now:time.Now}
}
// The current attribution review and modification date apply ONLY to this
// owner-verified immutable generation. Refreshes need explicit review and
// updated pins; no implicit trust in an arbitrary manifest or new source.
func poiReviewedGeneration(m poigrid.Manifest,e poigrid.Evidence)bool{
 if m.Release!=poigrid.Release||m.Mapping!=poigrid.Mapping||m.Stats.Rows!=199475||
 m.PayloadSHA256!="c7f24b89ac06cac0ba3eec98e3a61a1090a5fcdad432ef6962e04218aa24188b"||
 m.IndexSHA256!="322411080c4b40a181bf2215e3e924003cf75b9f53dcebd707d7f879dbc998f6"||
 m.ProvenanceSHA256!="ddc65e65c393ddcf2481500b9fd7379ae758418d49cdfeb53a1e83113ecd36d2"||
 e.ProvenanceRows!=199475{return false}
 expected:=map[string]int{"AllThePlaces | CC0-1.0":5341,"Birdeye | CDLA-Permissive-2.0":5,"BrightQuery | CDLA-Permissive-2.0":33256,"DAC | CDLA-Permissive-2.0":3489,"Foursquare | Apache-2.0":20789,"Microsoft | CDLA-Permissive-2.0":14308,"OpenTravel | CDLA-Permissive-2.0":91,"Overture | CDLA-Permissive-2.0":199475,"Overture-signals | CDLA-Permissive-2.0":168038,"PinMeTo | CDLA-Permissive-2.0":1,"RenderSEO | CDLA-Permissive-2.0":8,"Uberall | CDLA-Permissive-2.0":1303,"meta | CDLA-Permissive-2.0":121863}
 if len(e.DatasetLicenseCounts)!=len(expected){return false}
 for k,v:=range expected{if e.DatasetLicenseCounts[k]!=v{return false}}
 expectedSources:=map[string]bool{"be8157302e8d94b648f884ae65b10f6073960b439dd0aceda2e145c75bfe5441":true,"0679fbe44ee8642000a73ead77d196945f17b3d12a44a5bc17bd5db95b746fc3":true,"2e17ea40dff3e00163eec87a26efa0b0ff7e8fe364a191bf1a7754dcb49ea326":true}
 for _,s:=range m.Sources{if !expectedSources[s.SHA256]{return false};delete(expectedSources,s.SHA256)}
 return len(expectedSources)==0
}
func openPOIService(dir string)*poiService{
 if dir==""{return newPOIService(nil,poigrid.Manifest{})}
 reader,m,evidence,err:=poigrid.Open(dir)
 if err!=nil{log.Print("Sailing POI disabled: generation validation failed");return newPOIService(nil,poigrid.Manifest{})}
 if !poiReviewedGeneration(m,evidence){_ = reader.Close();log.Print("Sailing POI disabled: generation not reviewed");return newPOIService(nil,poigrid.Manifest{})}
 log.Print("Sailing POI generation validated")
 return newPOIService(reader,m)
}
func(p *poiService)close(){if p.reader!=nil{_ = p.reader.Close()}}
func(p *poiService)available()bool{return p.reader!=nil&&!p.unavailable.Load()}
func(p *poiService)admit()bool{
 select{case p.slots<-struct{}{}:default:return false}
 p.mu.Lock()
 now:=p.now();elapsed:=now.Sub(p.last).Seconds()
 if elapsed>0{p.tokens=math.Min(16,p.tokens+elapsed*8);p.last=now}
 allowed:=p.tokens>=1
 if allowed{p.tokens--}
 p.mu.Unlock()
 if !allowed{<-p.slots}
 return allowed
}
func poiError(w http.ResponseWriter,status int,msg string){
 w.Header().Set("Content-Type","application/json; charset=utf-8")
 w.Header().Set("Cache-Control","no-store")
 w.Header().Del("ETag")
 if status==429{w.Header().Set("Retry-After","1")}
 w.WriteHeader(status)
 _=json.NewEncoder(w).Encode(struct{Error string `json:"error"`}{msg})
}
func poiCORS(w http.ResponseWriter,r *http.Request)bool{
 h:=w.Header();h.Add("Vary","Origin")
 origins:=r.Header.Values("Origin")
 if len(origins)>1||(len(origins)==1&&origins[0]!=poiOrigin){poiError(w,403,"origin not allowed");return false}
 if len(origins)==1{h.Set("Access-Control-Allow-Origin",poiOrigin);h.Set("Access-Control-Expose-Headers","ETag, Retry-After")}
 if r.Method==http.MethodOptions{
  h.Add("Vary","Access-Control-Request-Method");h.Add("Vary","Access-Control-Request-Headers")
  methods:=r.Header.Values("Access-Control-Request-Method")
  if len(origins)!=1||len(methods)!=1||methods[0]!="GET"{poiError(w,403,"preflight not allowed");return false}
  if r.Header.Get("Access-Control-Request-Private-Network")!=""{poiError(w,403,"preflight not allowed");return false}
  for _,line:=range r.Header.Values("Access-Control-Request-Headers"){for _,v:=range strings.Split(line,","){if !strings.EqualFold(strings.TrimSpace(v),"If-None-Match"){poiError(w,403,"preflight header not allowed");return false}}}
  h.Set("Access-Control-Allow-Methods","GET, OPTIONS");h.Set("Access-Control-Allow-Headers","If-None-Match");h.Set("Access-Control-Max-Age","60")
 }
 return true
}
var poiDecimal=regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)$`)
var poiInteger=regexp.MustCompile(`^[0-9]{1,3}$`)
func parsePOIQuery(raw string)(poigrid.Query,error){
 q:=poigrid.Query{Limit:200,ByteLimit:poigrid.MaxArrayBytes}
 bad:=poigrid.ErrQuery
 if raw==""||len(raw)>1024{return q,bad}
 vals,e:=url.ParseQuery(raw);if e!=nil{return q,bad}
 // ParseQuery ignores empty components; forbid ambiguous empty components.
 for _,part:=range strings.Split(raw,"&"){if part==""||!strings.Contains(part,"="){return q,bad}}
 for k:=range vals{if k!="bbox"&&k!="category"&&k!="limit"{return q,bad}}
 if len(vals["bbox"])!=1||len(vals["category"])<1||len(vals["category"])>4||len(vals["limit"])>1{return q,bad}
 parts:=strings.Split(vals.Get("bbox"),",");if len(parts)!=4{return q,bad}
 coords:=make([]float64,4)
 for i,s:=range parts{if len(s)>32||!poiDecimal.MatchString(s){return q,bad};v,e:=strconv.ParseFloat(s,64);if e!=nil||math.IsNaN(v)||math.IsInf(v,0){return q,bad};coords[i]=v}
 q.West,q.South,q.East,q.North=coords[0],coords[1],coords[2],coords[3]
 dx,dy:=q.East-q.West,q.North-q.South
 if q.West< -180||q.East>180||q.South< -90||q.North>90||dx<0||dy<0||dx>.5||dy>.5||dx*dy>.125{return q,bad}
 known:=poiRegistryEntries(nil,false);seen:=map[string]bool{}
 for _,c:=range vals["category"]{if _,ok:=known[c];!ok||seen[c]{return q,bad};seen[c]=true;q.Categories=append(q.Categories,c)}
 sort.Strings(q.Categories)
 if len(vals["limit"])==1{v:=vals.Get("limit");if !poiInteger.MatchString(v){return q,bad};n,e:=strconv.Atoi(v);if e!=nil||n<1||n>500{return q,bad};q.Limit=n}
 return q,nil
}
type poiStatus struct {
 Version int `json:"version"`
 Available bool `json:"available"`
 Coverage string `json:"coverage"`
 DatasetVersion *string `json:"dataset_version"`
 SnapshotAt *string `json:"snapshot_at"`
 RegistryRevision string `json:"registry_revision"`
 CategoryCounts map[string]int `json:"category_counts"`
 Sources []poiSource `json:"sources"`
 AttributionURL string `json:"attribution_url"`
 Notice string `json:"notice"`
}
func(p *poiService)status()poiStatus{
 v:=poiStatus{Version:1,Available:p.available(),Coverage:"California terrestrial candidate dataset",RegistryRevision:poiRegistry,CategoryCounts:map[string]int{},Sources:[]poiSource{},AttributionURL:poiAttributionPath,Notice:poiNotice}
 if v.Available{
  version:=p.manifest.Release+"-"+p.manifest.Mapping+"-"+p.manifest.PayloadSHA256
  at:=p.manifest.Created;v.DatasetVersion=&version;v.SnapshotAt=&at
  for k,n:=range p.manifest.Stats.Categories{v.CategoryCounts[k]=n}
  v.Sources=poiSources()
 }
 return v
}
type poiCategoriesResponse struct{
 Version int `json:"version"`
 RegistryRevision string `json:"registry_revision"`
 DatasetVersion *string `json:"dataset_version"`
 Categories map[string]poiCategory `json:"categories"`
 AttributionURL string `json:"attribution_url"`
}
type poiRecord struct{
 ProviderCategories []string `json:"provider_categories"`
 Waypoint json.RawMessage `json:"waypoint"`
}
type poiBBoxResponse struct{
 Version int `json:"version"`
 DatasetVersion *string `json:"dataset_version"`
 RegistryRevision string `json:"registry_revision"`
 Categories []string `json:"categories"`
 UnavailableCategories []string `json:"unavailable_categories"`
 Records []poiRecord `json:"records"`
 Truncated bool `json:"truncated"`
 Sources []poiSource `json:"sources"`
 AttributionURL string `json:"attribution_url"`
 Notice string `json:"notice"`
}
func poiConditional(r *http.Request,tag string)bool{
 for _,line:=range r.Header.Values("If-None-Match"){for _,s:=range strings.Split(line,","){s=strings.TrimSpace(s);if s=="*"||strings.TrimPrefix(s,"W/")==tag{return true}}}
 return false
}
func poiSend(w http.ResponseWriter,r *http.Request,body []byte,contentType string,cache bool){
 if len(body)>poiMaxBody{poiError(w,503,"response unavailable");return}
 h:=w.Header();h.Set("Content-Type",contentType)
 if cache{
  digest:=sha256.Sum256(body);tag:="\""+hex.EncodeToString(digest[:])+"\"";h.Set("ETag",tag);h.Set("Cache-Control","public, max-age=60")
  if poiConditional(r,tag){w.WriteHeader(304);return}
 }else{h.Set("Cache-Control","no-store")}
 _,_=w.Write(body)
}
func poiJSON(w http.ResponseWriter,r *http.Request,v any,cache bool){
 body,e:=json.Marshal(v);if e!=nil{poiError(w,503,"response unavailable");return}
 poiSend(w,r,body,"application/json; charset=utf-8",cache)
}
func(p *poiService)ServeHTTP(w http.ResponseWriter,r *http.Request){
 w.Header().Set("X-Content-Type-Options","nosniff")
 w.Header().Set("Referrer-Policy","no-referrer")
 w.Header().Set("Cache-Control","no-store")
 if r.URL.RawPath!=""||r.URL.EscapedPath()!=r.URL.Path{poiError(w,404,"not found");return}
 known:=false;for _,path:=range poiPaths{if r.URL.Path==path{known=true;break}}
 if !known{poiError(w,404,"not found");return}
 if r.Method!=http.MethodGet&&r.Method!=http.MethodOptions{w.Header().Set("Allow","GET, OPTIONS");poiError(w,405,"method not allowed");return}
 if !poiCORS(w,r){return}
 if len(r.URL.RawQuery)>1024||r.ContentLength!=0||len(r.TransferEncoding)>0{poiError(w,400,"invalid request");return}
 n:=0;for _,v:=range r.Header.Values("If-None-Match"){n+=len(v)};if n>4096{poiError(w,400,"invalid validator");return}
 var q poigrid.Query
 if r.URL.Path==poiBBoxPath&&(r.Method==http.MethodGet||r.URL.RawQuery!=""){
  var e error;q,e=parsePOIQuery(r.URL.RawQuery);if e!=nil{poiError(w,400,"invalid bbox, category or limit");return}
 }else if r.URL.RawQuery!=""||r.URL.ForceQuery{poiError(w,400,"unexpected query");return}
 if !p.admit(){poiError(w,429,"POI request limit");return}
 defer func(){<-p.slots}()
 // Bound write time for these endpoints only, including slow clients.
 controller:=http.NewResponseController(w)
 if controller.SetWriteDeadline(time.Now().Add(3*time.Second))==nil{defer controller.SetWriteDeadline(time.Time{})}
 if r.Method==http.MethodOptions{w.WriteHeader(204);return}
 if r.URL.Path==poiAttributionPath{poiSend(w,r,poiAttribution,"text/plain; charset=utf-8",true);return}
 status:=p.status()
 switch r.URL.Path{
 case poiStatusPath:poiJSON(w,r,status,status.Available)
 case poiCategoriesPath:poiJSON(w,r,poiCategoriesResponse{1,poiRegistry,status.DatasetVersion,poiRegistryEntries(status.CategoryCounts,status.Available),poiAttributionPath},status.Available)
 case poiBBoxPath:
  if !status.Available{poiError(w,503,"POI dataset unavailable");return}
  requested:=append([]string(nil),q.Categories...)
  q.Categories=nil
  unavailable:=[]string{}
  for _,c:=range requested{if status.CategoryCounts[c]>0{q.Categories=append(q.Categories,c)}else{unavailable=append(unavailable,c)}}
  out:=poiBBoxResponse{Version:1,DatasetVersion:status.DatasetVersion,RegistryRevision:poiRegistry,Categories:requested,UnavailableCategories:unavailable,Records:[]poiRecord{},Sources:status.Sources,AttributionURL:poiAttributionPath,Notice:poiNotice}
  if len(q.Categories)>0{
   ctx,cancel:=context.WithTimeout(r.Context(),time.Second);defer cancel()
   result,e:=p.reader.Search(ctx,q)
   if e!=nil{
    if errors.Is(e,poigrid.ErrBusy){poiError(w,429,"POI request limit");return}
    if !errors.Is(e,context.DeadlineExceeded)&&!errors.Is(e,context.Canceled)&&!errors.Is(e,poigrid.ErrQuery){p.unavailable.Store(true)}
    poiError(w,503,"POI query unavailable");return
   }
   if ctx.Err()!=nil{poiError(w,503,"POI query unavailable");return}
   out.Truncated=result.Truncated
   // Reader already sorts and bounds native records. Enforce the entire
   // encoded wrapper budget; a prefix of whole records only.
   empty,e:=json.Marshal(out);if e!=nil{poiError(w,503,"response unavailable");return}
   // false -> true shortens JSON by one byte. Account conservatively.
   total:=len(empty)
   for _,raw:=range result.Records{
    var wp struct{Category string `json:"category"`}
    if json.Unmarshal(raw,&wp)!=nil||!containsPOICategory(q.Categories,wp.Category){p.unavailable.Store(true);poiError(w,503,"POI query unavailable");return}
    record:=poiRecord{[]string{wp.Category},raw};encoded,e:=json.Marshal(record);if e!=nil{poiError(w,503,"response unavailable");return}
    size:=len(encoded);if len(out.Records)>0{size++}
    if len(out.Records)>=q.Limit||total+size>poiMaxBody{out.Truncated=true;break}
    out.Records=append(out.Records,record);total+=size
   }
  }
  if !p.available(){poiError(w,503,"POI dataset unavailable");return}
  poiJSON(w,r,out,true)
 }
}
func containsPOICategory(c []string,v string)bool{for _,s:=range c{if s==v{return true}};return false}
