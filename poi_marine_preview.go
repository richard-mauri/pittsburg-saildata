package main

import (
 "context"
 "crypto/sha256"
 "crypto/subtle"
 "encoding/hex"
 "encoding/json"
 "errors"
 "io"
 "math"
 "net"
 "net/http"
 "os"
 "sort"
 "strings"
 "unicode"
 "unicode/utf16"
 "unicode/utf8"

 "pittsburg-saildata/internal/poigrid"
 "pittsburg-saildata/internal/poiweb"
)

const marinePreviewDigest = "830225558ed19e950e58d577db6fad41a46521c2f3023ee98b4d1c6c31f26fc3"
const marinePreviewHeader = "X-POI-Local-Preview"
const marinePreviewNotice = "Regional marine study, September 2026; local preview only. Pumpout coverage unavailable."
type marineRow struct {
 waypoint poigrid.Waypoint
 website string
}
type marineLayer struct {
 rows []marineRow
 counts map[string]int
 addr, token string
}
func marineCategory(k string)bool{return k=="marina"||k=="boat_ramp"||k=="marine_fuel"||k=="pumpout"}
func marineClean(s string,n int)bool{return utf8.ValidString(s)&&strings.TrimSpace(s)!=""&&len(utf16.Encode([]rune(s)))<=n&&strings.IndexFunc(s,unicode.IsControl)<0}
func marinePreviewConfig(addr, token string)bool{
 host,port,e:=net.SplitHostPort(addr)
 if e!=nil||host!="127.0.0.1"||port==""||port=="0"||len(token)!=64{return false}
 raw,e:=hex.DecodeString(token);return e==nil&&len(raw)==32
}
func(m *marineLayer) allowed(r *http.Request)bool{
 host,_,e:=net.SplitHostPort(r.RemoteAddr)
 return e==nil&&host=="127.0.0.1"&&r.Host==m.addr&&r.Header.Get("Origin")==""&&
 len(r.Header.Values(marinePreviewHeader))==1&&subtle.ConstantTimeCompare([]byte(r.Header.Get(marinePreviewHeader)),[]byte(m.token))==1
}
func loadMarinePreview(path,addr,token string)(*marineLayer,error){
 if !marinePreviewConfig(addr,token){return nil,errors.New("marine preview requires isolated loopback and capability")}
 st,e:=os.Lstat(path);if e!=nil||!st.Mode().IsRegular()||st.Size()<=0||st.Size()>8<<20{return nil,errors.New("invalid marine asset")}
 f,e:=os.Open(path);if e!=nil{return nil,e};defer f.Close()
 actual,e:=f.Stat();if e!=nil||!os.SameFile(st,actual){return nil,errors.New("marine asset changed")}
 raw,e:=io.ReadAll(io.LimitReader(f,(8<<20)+1));if e!=nil{return nil,e}
 return decodeMarinePreview(raw,addr,token)
}
func decodeMarinePreview(raw []byte,addr,token string)(*marineLayer,error){
 sum:=sha256.Sum256(raw);if len(raw)>8<<20||hex.EncodeToString(sum[:])!=marinePreviewDigest||!marinePreviewConfig(addr,token){return nil,errors.New("unreviewed marine asset or preview config")}
 var a struct{
 Version string `json:"version"`
 Generator string `json:"generator_version"`
 Updated string `json:"updated"`
 Places []struct{
 Name string `json:"name"`
 Category string `json:"category"`
 Lat *float64 `json:"lat"`
 Lon *float64 `json:"lon"`
 Website string `json:"website"`
 Source string `json:"source"`
 SourceID string `json:"source_id"`
 CoordSource string `json:"coord_source"`
 CoordStatus string `json:"coord_status"`
 } `json:"places"`
 }
 if json.Unmarshal(raw,&a)!=nil||a.Version!="14"||a.Generator!="v32"||a.Updated!="2026-09-25"||len(a.Places)!=774{return nil,errors.New("marine asset metadata mismatch")}
 m:=&marineLayer{counts:map[string]int{},addr:addr,token:token}
 mapping:=map[string]string{"marinas":"marina","launch_ramps":"boat_ramp","fuel_docks":"marine_fuel"}
 seen:=map[string]bool{}
 for _,p:=range a.Places{
  c:=mapping[p.Category];if c==""{continue}
  if p.Lat==nil||p.Lon==nil||math.IsNaN(*p.Lat)||math.IsNaN(*p.Lon)||math.Abs(*p.Lat)>90||math.Abs(*p.Lon)>180||!marineClean(p.Name,80)||!marineClean(p.Source,100)||!marineClean(p.CoordSource,100)||!marineClean(p.CoordStatus,100){return nil,errors.New("invalid scoped marine record")}
  identity:=p.SourceID
  if identity==""{if p.Source!="local override"||p.Name!="Antioch Marina Launch Ramp"||c!="boat_ramp"||p.CoordStatus!="explicit_override"{return nil,errors.New("unmapped marine identity")};identity="local-antioch-marina-launch-ramp"}
  if !marineClean(identity,200){return nil,errors.New("invalid marine identity")}
  d:=sha256.Sum256([]byte(c+"\x00"+p.Source+"\x00"+identity));id:="poi-marine-"+hex.EncodeToString(d[:])
  if seen[id]{return nil,errors.New("duplicate marine identity")};seen[id]=true
  w:=poigrid.Waypoint{ID:id,Lat:*p.Lat,Lon:*p.Lon,Name:p.Name,Category:c,Notes:"Source: "+p.Source+".",Created:"2026-09-25T00:00:00Z"}
  link:="";if poiweb.Valid(p.Website){link=p.Website}
  m.rows=append(m.rows,marineRow{w,link});m.counts[c]++
 }
 if len(m.rows)!=465||m.counts["marina"]!=234||m.counts["boat_ramp"]!=151||m.counts["marine_fuel"]!=80{return nil,errors.New("marine counts mismatch")}
 sort.Slice(m.rows,func(i,j int)bool{return m.rows[i].waypoint.ID<m.rows[j].waypoint.ID})
 return m,nil
}
func(m *marineLayer) search(ctx context.Context,q poigrid.Query)([]poiRecord,error){
 out:=[]poiRecord{}
 if m==nil{return out,nil}
 for _,r:=range m.rows{
  if e:=ctx.Err();e!=nil{return nil,e}
  w:=r.waypoint
  if !containsPOICategory(q.Categories,w.Category)||w.Lon<q.West||w.Lon>q.East||w.Lat<q.South||w.Lat>q.North{continue}
  raw,e:=json.Marshal(w);if e!=nil{return nil,e}
  out=append(out,poiRecord{ProviderCategories:[]string{w.Category},Waypoint:raw,Website:r.website})
 }
 return out,nil
}
func marinePreviewSources()[]poiSource{return []poiSource{
 {"OpenStreetMap contributors","2026-09-25","ODbL-1.0","© OpenStreetMap contributors; regional curated marine study","https://www.openstreetmap.org/copyright"},
 {"California State Parks/DBW","2026-09-25","Redistribution not cleared","California State Parks Division of Boating and Waterways; local study only","https://dbw.parks.ca.gov/ConditionsOfUse"},
 {"Curated marine overrides","2026-09-25","Local study only","Curated coordinate override; no public distribution",""},
}}
