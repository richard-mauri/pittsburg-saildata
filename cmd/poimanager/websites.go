package main

import (
 "bufio"
 "context"
 "crypto/sha256"
 "encoding/csv"
 "encoding/hex"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "math"
 "os"
 "path/filepath"
 "sort"
 "strings"
 "time"

 "pittsburg-saildata/internal/poigrid"
 "pittsburg-saildata/internal/poiweb"
)

func websiteSQL(s,n float64) string {
 q:=sql(s,n)
 start,end:=strings.Index(q,"SELECT id,"),strings.Index(q," FROM read_parquet")
 return q[:start]+"SELECT id, CAST(websites AS JSON) AS websites"+q[end:]
}
func consumeWebsites(in io.Reader, accepted map[string]bool, seen map[string]string) error {
 c:=csv.NewReader(io.LimitReader(in,rawCap+1));hdr,e:=c.Read()
 if e!=nil||strings.Join(hdr,",")!="id,websites"{return errors.New("website column unavailable or unexpected CSV schema")}
 count:=0
 for {f,e:=c.Read();if e==io.EOF{break};if e!=nil{return e};count++
  if count>maxBandRows||c.InputOffset()>rawCap||len(f[0])>72||len(f[1])>1<<20{return errors.New("website extraction cap exceeded")}
  id:="poi-ovt-"+f[0];if !accepted[id]{continue}
  var values []string
  if f[1]!=""&&f[1]!="NULL"&&json.Unmarshal([]byte(f[1]),&values)!=nil{return errors.New("invalid website list")}
  v:=poiweb.Select(values)
  if old,ok:=seen[id];ok&&old!=v{return errors.New("conflicting website for source ID")}
  seen[id]=v
 }
 return nil
}
// Read only the exact previously reviewed base and source files; publish a
// separate local candidate index, never mutate the generation or download.
func websites() error {
 if e:=realDir("private");e!=nil{return e};if e:=realDir(study);e!=nil{return e}
 dest:=filepath.Join(study,poiweb.Filename)
 if _,e:=os.Lstat(dest);!os.IsNotExist(e){return errors.New("website candidate already exists")}
 dir:=filepath.Join(study,"generation")
 reader,m,_,e:=poigrid.Open(dir);if e!=nil{return e};reader.Close()
 if m.PayloadSHA256!=poiweb.BasePayload||m.IndexSHA256!=poiweb.BaseIndex||m.ProvenanceSHA256!=poiweb.BaseProvenance||m.Stats.Rows!=poiweb.BaseRows{return errors.New("website enrichment requires reviewed base")}
 if _,e=north();e!=nil{return e}
 if _,e=checkSource(study,"south.parquet",southURL,release,southBytes,southBytes+1,"0679fbe44ee8642000a73ead77d196945f17b3d12a44a5bc17bd5db95b746fc3");e!=nil{return e}
 f,e:=os.Open(filepath.Join(dir,"provenance.jsonl"));if e!=nil{return e}
 scan:=bufio.NewScanner(f);scan.Buffer(make([]byte,64<<10),2<<20)
 accepted:=make(map[string]bool,poiweb.BaseRows)
 for scan.Scan(){var p struct{ID string};if json.Unmarshal(scan.Bytes(),&p)!=nil||accepted[p.ID]||!strings.HasPrefix(p.ID,"poi-ovt-"){f.Close();return errors.New("invalid base identity")};accepted[p.ID]=true}
 e=scan.Err();f.Close();if e!=nil||len(accepted)!=poiweb.BaseRows{return errors.New("base identity mismatch")}
 ctx,cancel:=context.WithTimeout(context.Background(),2*time.Hour);defer cancel()
 image,_,e:=localImage(ctx);if e!=nil{return e}
 stage,e:=os.MkdirTemp(study,"website-stage-");if e!=nil{return e}
 fmt.Fprintln(os.Stderr,"Website candidate staging:",stage)
 seen:=make(map[string]string,poiweb.BaseRows);var total int64
 for _,src:=range []struct{path string;lo,hi int}{{filepath.Join(study,"south.parquet"),32,35},{filepath.Join(northDir,"places.parquet"),34,43}}{
  input,e:=filepath.Abs(src.path);if e!=nil{return e}
  for j:=src.lo;j<src.hi;j++{
   n:=math.Min(float64(j+1),42.1);if float64(j)>=n{continue}
   tmp,e:=os.CreateTemp(stage,"band-");if e!=nil{return e}
   container:="sailinggo-web-"+filepath.Base(stage)+"-"+filepath.Base(tmp.Name())
   e=extractQuery(ctx,input,container,image,websiteSQL(float64(j),n),tmp)
   if e==nil{var st os.FileInfo;st,e=tmp.Stat();if e==nil{total+=st.Size();if total>8<<30{e=errors.New("website extraction total cap")}}}
   if e==nil{_,e=tmp.Seek(0,io.SeekStart)};if e==nil{e=consumeWebsites(tmp,accepted,seen)}
   ce:=tmp.Close();if e==nil{e=ce};if e!=nil{return fmt.Errorf("website extraction failed; staging retained: %w",e)}
   if e=os.Remove(tmp.Name());e!=nil{return e}
  }
 }
 if len(seen)!=len(accepted){return errors.New("website source IDs did not cover reviewed generation")}
 x:=poiweb.Index{Version:1,Payload:poiweb.BasePayload,Rows:poiweb.BaseRows,Entries:[]poiweb.Entry{}}
 for id,v:=range seen{if v!=""{x.Entries=append(x.Entries,poiweb.Entry{ID:id,Website:v})}}
 sort.Slice(x.Entries,func(i,j int)bool{return x.Entries[i].ID<x.Entries[j].ID})
 raw,e:=json.Marshal(x);if e!=nil{return e};raw=append(raw,'\n')
 sum:=sha256.Sum256(raw);digest:=hex.EncodeToString(sum[:])
 if _,e=poiweb.Decode(raw,digest);e!=nil{return e}
 candidate:=filepath.Join(stage,poiweb.Filename)
 out,e:=os.OpenFile(candidate,os.O_CREATE|os.O_EXCL|os.O_WRONLY,0600);if e!=nil{return e}
 _,e=out.Write(raw);if e==nil{e=out.Sync()};ce:=out.Close();if e==nil{e=ce};if e!=nil{return e}
 // Link cannot overwrite even if a competing process creates dest.
 if e=os.Link(candidate,dest);e!=nil{return e}
 if e=os.Remove(candidate);e!=nil{return e};if e=os.Remove(stage);e!=nil{return e}
 return printJSON(map[string]any{"base_rows":len(accepted),"matched_rows":len(seen),"website_rows":len(x.Entries),"missing_or_invalid":len(accepted)-len(x.Entries),"bytes":len(raw),"sha256":digest,"candidate":dest,"publication":"local candidate only; review required"})
}
