// Package poiweb handles an optional reviewed website enrichment, separate from native waypoints.
package poiweb

import (
 "bytes"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "errors"
 "io"
 "net/url"
 "os"
 "regexp"
 "strings"
)

const Filename = "websites-v1.json"
const MaxBytes = 32 << 20
const MaxURL = 1024
const BasePayload = "c7f24b89ac06cac0ba3eec98e3a61a1090a5fcdad432ef6962e04218aa24188b"
const BaseIndex = "322411080c4b40a181bf2215e3e924003cf75b9f53dcebd707d7f879dbc998f6"
const BaseProvenance = "ddc65e65c393ddcf2481500b9fd7379ae758418d49cdfeb53a1e83113ecd36d2"
const BaseRows = 199475
var id = regexp.MustCompile(`^poi-ovt-[A-Za-z0-9_-]{1,72}$`)
var hash = regexp.MustCompile(`^[a-f0-9]{64}$`)
var label = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)
var alpha = regexp.MustCompile(`[A-Za-z]`)
type Entry struct { ID string `json:"id"`; Website string `json:"website"` }
type Index struct { Version int `json:"version"`; Payload string `json:"payload_sha256"`; Rows int `json:"base_rows"`; Entries []Entry `json:"entries"` }

// Restrict to absolute HTTP(S) DNS names, without credentials, IP literals,
// local names, nonstandard ports, whitespace or control characters. No fetch.
func Valid(v string) bool {
 if len(v)==0 || len(v)>MaxURL || !(strings.HasPrefix(v,"https://")||strings.HasPrefix(v,"http://")) {return false}
 for _,r:=range v {if r<=32||r>=127||r=='\\'||r=='<'||r=='>'||r=='"' {return false}}
 lower:=strings.ToLower(v)
 if strings.Contains(lower,"%0a")||strings.Contains(lower,"%0d")||strings.Contains(lower,"%00"){return false}
 u,e:=url.Parse(v);if e!=nil||u.User!=nil||u.Host==""||u.Opaque!=""||strings.Contains(u.Host,":"){return false}
 host:=strings.ToLower(u.Hostname());parts:=strings.Split(host,".")
 if len(host)>253||len(parts)<2||!alpha.MatchString(parts[len(parts)-1]){return false}
 for _,p:=range parts {if !label.MatchString(p){return false}}
 for _,suffix:=range []string{".local",".localhost",".internal",".test",".invalid",".example",".onion",".lan",".home",".arpa"}{if strings.HasSuffix(host,suffix){return false}}
 return true
}
func Select(values []string) string {
 // Prefer HTTPS; within each scheme preserve the publisher's order.
 for _,prefix:=range []string{"https://","http://"}{for _,v:=range values{if strings.HasPrefix(v,prefix)&&Valid(v){return v}}}
 return ""
}
func Decode(raw []byte, expected string)(map[string]string,error){
 if len(raw)>MaxBytes||!hash.MatchString(expected){return nil,errors.New("invalid website index size or digest")}
 sum:=sha256.Sum256(raw);if hex.EncodeToString(sum[:])!=expected{return nil,errors.New("website digest mismatch")}
 d:=json.NewDecoder(bytes.NewReader(raw));d.DisallowUnknownFields();var x Index
 if d.Decode(&x)!=nil||d.Decode(new(any))!=io.EOF||x.Version!=1||x.Payload!=BasePayload||x.Rows!=BaseRows||x.Entries==nil||len(x.Entries)>BaseRows{return nil,errors.New("invalid website index")}
 out:=make(map[string]string,len(x.Entries));previous:=""
 for _,v:=range x.Entries{if !id.MatchString(v.ID)||v.ID<=previous||!Valid(v.Website){return nil,errors.New("invalid website entry")};out[v.ID]=v.Website;previous=v.ID}
 // Canonical compact JSON plus newline is required; rejects duplicate keys
 // and ambiguous encodings, and binds exact reviewed bytes.
 canonical,e:=json.Marshal(x);if e!=nil||!bytes.Equal(raw,append(canonical,'\n')){return nil,errors.New("noncanonical website index")}
 return out,nil
}
func Load(path,expected string)(map[string]string,error){
 st,e:=os.Lstat(path);if e!=nil||!st.Mode().IsRegular()||st.Size()<=0||st.Size()>MaxBytes{return nil,errors.New("invalid website file")}
 f,e:=os.Open(path);if e!=nil{return nil,e};defer f.Close()
 actual,e:=f.Stat();if e!=nil||!os.SameFile(st,actual){return nil,errors.New("website file changed")}
 raw,e:=io.ReadAll(io.LimitReader(f,MaxBytes+1));if e!=nil{return nil,e}
 return Decode(raw,expected)
}
