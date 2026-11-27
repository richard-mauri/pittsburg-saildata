// poioverturesample is a local, isolated source evaluation tool.
package main

import (
 "context"
 "crypto/sha256"
 "encoding/binary"
 "encoding/csv"
 "encoding/hex"
 "encoding/json"
 "errors"
 "flag"
 "fmt"
 "io"
 "math"
 "net/http"
 "os"
 "os/exec"
 "path/filepath"
 "regexp"
 "sort"
 "strconv"
 "strings"
 "time"
 "unicode"
 "unicode/utf16"
 "unicode/utf8"
)

const release = "2026-11-18.0"
const sourceURL = "https://overturemaps-us-west-2.s3.us-west-2.amazonaws.com/release/2026-11-18.0/theme=places/type=place/part-00002-5783d80c-d44a-5845-9057-3a39013540b0-c000.zstd.parquet"
const expectedBytes int64 = 836166912
const bodyCap int64 = 1000000000
const rawCap int64 = 64 << 20
const maxRows = 100000
const studyPath = "private/poi-overture-2026-11-18.0"
const imageTag = "duckdb/duckdb:1.4.2"
type bbox struct { W,S,E,N float64 }
var boxes = map[string]bbox{"delta":{-121.94,38,-121.84,38.06},"sf":{-122.43,37.77,-122.39,37.81}}
type manifest struct {
 Release,URL,SHA256,ETag,LastModified,DownloadedAt string
 Bytes int64
 ElapsedSeconds float64
}
type progress struct { w io.Writer; n,total int64; last time.Time; display io.Writer }
func (p *progress) Write(b []byte)(int,error){
 n,e:=p.w.Write(b);p.n+=int64(n)
 if p.n==p.total || time.Since(p.last)>=time.Second {
  fmt.Fprintf(p.display,"\rOverture: %.1f / %.1f MiB (%.1f%%)",float64(p.n)/(1<<20),float64(p.total)/(1<<20),100*float64(p.n)/float64(p.total));p.last=time.Now()
 }
 return n,e
}
func saveJSON(path string,v any)error{
 f,e:=os.OpenFile(path,os.O_CREATE|os.O_EXCL|os.O_WRONLY,0600);if e!=nil{return e}
 enc:=json.NewEncoder(f);enc.SetIndent("","  ");e=enc.Encode(v);if x:=f.Close();e==nil{e=x};return e
}
func realDir(p string)error{st,e:=os.Lstat(p);if e!=nil{return e};if !st.IsDir()||st.Mode()&os.ModeSymlink!=0{return errors.New("expected real directory")};return nil}
func regular(p string,n int64)error{st,e:=os.Lstat(p);if e!=nil{return e};if !st.Mode().IsRegular()||st.Size()!=n{return errors.New("unexpected file type or length")};return nil}
func transfer(w io.Writer,r io.Reader,n,cap int64)(string,error){
 if n<=0||n>=cap{return "",errors.New("invalid expected size or cap")}
 hash:=sha256.New();got,e:=io.CopyN(io.MultiWriter(w,hash),r,n);if e!=nil||got!=n{return "",errors.New("incomplete source body")}
 var extra [1]byte
 if k,e:=io.ReadFull(r,extra[:]);k!=0||e!=io.EOF{return "",errors.New("source body exceeds expected size or EOF unverified")}
 return hex.EncodeToString(hash.Sum(nil)),nil
}
func magic(p string,n int64)error{
 f,e:=os.Open(p);if e!=nil{return e};defer f.Close()
 var a,z [4]byte
 if _,e=f.ReadAt(a[:],0);e!=nil{return e};if _,e=f.ReadAt(z[:],n-4);e!=nil{return e}
 if string(a[:])!="PAR1"||string(z[:])!="PAR1"{return errors.New("invalid Parquet magic")};return nil
}
func download(apply bool)error{
 if !apply{fmt.Printf("DRY RUN: %s\nExpected %d bytes; cap %d; 15 minutes; new %s only\n",sourceURL,expectedBytes,bodyCap,studyPath);return nil}
 if e:=os.MkdirAll("private",0700);e!=nil{return e};if e:=realDir("private");e!=nil{return e}
 if e:=os.Mkdir(studyPath,0700);e!=nil{return fmt.Errorf("study must not exist: %w",e)}
 fmt.Fprintln(os.Stderr,"New study:",studyPath)
 ctx,cancel:=context.WithTimeout(context.Background(),15*time.Minute);defer cancel()
 tr:=http.DefaultTransport.(*http.Transport).Clone();tr.Proxy=nil;tr.DisableCompression=true;defer tr.CloseIdleConnections()
 c:=&http.Client{Transport:tr,CheckRedirect:func(*http.Request,[]*http.Request)error{return errors.New("redirect refused")}}
 req,e:=http.NewRequestWithContext(ctx,http.MethodGet,sourceURL,nil);if e!=nil{return e};req.Header.Set("Accept-Encoding","identity")
 start:=time.Now();res,e:=c.Do(req);if e!=nil{return e};defer res.Body.Close()
 if res.StatusCode!=200||(res.ContentLength!=-1&&res.ContentLength!=expectedBytes)||(res.Header.Get("Content-Encoding")!=""&&!strings.EqualFold(res.Header.Get("Content-Encoding"),"identity")){return fmt.Errorf("unexpected source response: status=%d length=%d encoding=%q",res.StatusCode,res.ContentLength,res.Header.Get("Content-Encoding"))}
 part:=filepath.Join(studyPath,"places.parquet.partial");out,e:=os.OpenFile(part,os.O_CREATE|os.O_EXCL|os.O_WRONLY,0600);if e!=nil{return e}
 p:=&progress{w:out,total:expectedBytes,display:os.Stderr};digest,e:=transfer(p,res.Body,expectedBytes,bodyCap);fmt.Fprintln(os.Stderr)
 if e==nil{e=out.Sync()};if x:=out.Close();e==nil{e=x};if e!=nil{return e}
 if e=magic(part,expectedBytes);e!=nil{return e}
 if e=os.Rename(part,filepath.Join(studyPath,"places.parquet"));e!=nil{return e}
 m:=manifest{release,sourceURL,digest,res.Header.Get("ETag"),res.Header.Get("Last-Modified"),time.Now().UTC().Format(time.RFC3339Nano),expectedBytes,time.Since(start).Seconds()}
 if e=saveJSON(filepath.Join(studyPath,"source.json"),m);e!=nil{return e};return printJSON(m)
}
func verifySource()error{
 if e:=realDir("private");e!=nil{return e};if e:=realDir(studyPath);e!=nil{return e}
 mp:=filepath.Join(studyPath,"source.json");st,e:=os.Lstat(mp);if e!=nil||!st.Mode().IsRegular()||st.Size()>8192{return errors.New("invalid source manifest")}
 raw,e:=os.ReadFile(mp);if e!=nil{return e};var m manifest
 if json.Unmarshal(raw,&m)!=nil||m.URL!=sourceURL||m.Release!=release||m.Bytes!=expectedBytes{return errors.New("unexpected source manifest")}
 p:=filepath.Join(studyPath,"places.parquet");if e=regular(p,expectedBytes);e!=nil{return e};if e=magic(p,expectedBytes);e!=nil{return e}
 f,e:=os.Open(p);if e!=nil{return e};defer f.Close();sha:=sha256.New();n,e:=io.Copy(sha,io.LimitReader(f,expectedBytes+1));if e!=nil||n!=expectedBytes||hex.EncodeToString(sha.Sum(nil))!=m.SHA256{return errors.New("source SHA256 mismatch")};return nil
}
func sql(b bbox)string{
 // GeoParquet bounds may be float32 rounded outward; filter on overlap, then decode exact WKB in Go.
 return fmt.Sprintf("SET autoinstall_known_extensions=false; SET autoload_known_extensions=false; SET enable_geoparquet_conversion=false; SET threads=2; SET memory_limit='2GB'; SET temp_directory='/tmp/duckdb'; SET max_temp_directory_size='512MB'; SELECT id, names.primary AS name, basic_category, taxonomy.primary AS primary_category, CAST(taxonomy.hierarchy AS JSON) AS hierarchy, operating_status, confidence, hex(geometry) AS wkb, CAST(sources AS JSON) AS sources FROM read_parquet('/input/places.parquet') WHERE bbox.xmin <= %.8f AND bbox.xmax >= %.8f AND bbox.ymin <= %.8f AND bbox.ymax >= %.8f LIMIT %d;",b.E,b.W,b.N,b.S,maxRows+1)
}
type capped struct{ w io.Writer; left int64 }
func(c *capped) Write(p []byte)(int,error){if int64(len(p))>c.left{return 0,errors.New("output exceeds cap")};n,e:=c.w.Write(p);c.left-=int64(n);return n,e}
var imageID=regexp.MustCompile("^sha256:[0-9a-f]{64}$")
func localImage(ctx context.Context)(string,string,error){
 cmd:=exec.CommandContext(ctx,"docker","image","inspect","--format","{{.Id}} {{.Architecture}}",imageTag)
 raw,e:=cmd.Output();if e!=nil{return "","",fmt.Errorf("local %s required (pull explicitly first): %w",imageTag,e)}
 v:=strings.Fields(string(raw));if len(v)!=2||!imageID.MatchString(v[0])||(v[1]!="arm64"&&v[1]!="amd64"){return "","",errors.New("unexpected image identity")};return v[0],v[1],nil
}
func extract(ctx context.Context,input,container,id string,b bbox,out io.Writer)error{
 cmd:=exec.CommandContext(ctx,"docker","run","--rm","--name",container,"--pull=never","--network","none","--read-only","--cap-drop","ALL","--security-opt","no-new-privileges","--memory","4g","--cpus","2","--pids-limit","128","--tmpfs","/tmp:rw,noexec,nosuid,size=1g","--user",fmt.Sprintf("%d:%d",os.Getuid(),os.Getgid()),"--mount","type=bind,source="+input+",target=/input/places.parquet,readonly","--entrypoint","/duckdb",id,"-batch","-bail","-csv","-header","-c",sql(b))
 cmd.Stdout=&capped{out,rawCap};cmd.Stderr=&capped{os.Stderr,1<<20}
 e:=cmd.Run()
 if e!=nil{ // Only stop this uniquely named study container, if still running.
  cleanup,cancel:=context.WithTimeout(context.Background(),15*time.Second);defer cancel()
  _=exec.CommandContext(cleanup,"docker","rm","-f",container).Run()
  return fmt.Errorf("offline DuckDB extraction failed: %w",e)
 };return nil
}
type row struct{ ID,Name,Category string; Lat,Lon float64 }
type report struct{
 Sample,Release,Note,ImageID,Architecture string
 Bounds bbox
 Features,Rows int
 RawCategories,AcceptedCategories,Rejected,BasicCategories,PrimaryCategories,OperatingStatus,Confidence,SourceDatasets,SourceLicenses map[string]int
 ExtractSeconds,NormalizeSeconds float64
 RawBytes int64
}
var validID=regexp.MustCompile("^[A-Za-z0-9_-]{1,72}$")
func finite(n float64)bool{return !math.IsNaN(n)&&!math.IsInf(n,0)}
func validName(s string)bool{
 if strings.TrimSpace(s)==""||!utf8.ValidString(s)||len(utf16.Encode([]rune(s)))>80{return false}
 for _,r:=range s{if unicode.IsControl(r)||r==utf8.RuneError||r==0xfffe||r==0xffff{return false}};return true
}
func point(s string)(float64,float64,bool){
 raw,e:=hex.DecodeString(s);if e!=nil||len(raw)!=21{return 0,0,false}
 var order binary.ByteOrder=binary.LittleEndian
 if raw[0]==0{order=binary.BigEndian}else if raw[0]!=1{return 0,0,false}
 if order.Uint32(raw[1:5])!=1{return 0,0,false}
 lon,lat:=math.Float64frombits(order.Uint64(raw[5:13])),math.Float64frombits(order.Uint64(raw[13:21]))
 return lat,lon,finite(lat)&&finite(lon)&&math.Abs(lat)<=90&&math.Abs(lon)<=180
}
// Provisional exact token allowlist, primary path only (not alternates). Review
// observed release taxonomy before final selection. More specific categories win.
func category(basic,primary string,hierarchy []string)string{
 has:=func(keys ...string)bool{for _,k:=range keys{if basic==k||primary==k{return true};for _,v:=range hierarchy{if v==k{return true}}};return false}
 switch{
 case has("coffee_shop","cafe","coffeehouse"):return "coffee"
 case has("gas_station","fuel_station"):return "fuel"
 case has("electric_vehicle_charging_station","ev_charging_station","electric_vehicle_charging","charging_station"):return "ev_charging"
 case has("grocery_store","supermarket"):return "grocery"
 case has("parking_lot","parking_garage","parking"):return "parking"
 case has("lodging","hotel","motel","hostel","guest_house"):return "lodging"
 case has("restaurant"):return "restaurant"
 };return ""
}
func counter(s string)string{if s==""{return "(missing)"};if len(s)>128||!utf8.ValidString(s){return "(invalid)"};for _,r:=range s{if unicode.IsControl(r){return "(invalid)"}};return s}
func normalize(in io.Reader,sample string)([]row,report,error){
 b,ok:=boxes[sample];r:=report{Sample:sample,Release:release,Bounds:b,Note:"Provisional exact taxonomy mapping; named point-only terrestrial pilot. Bbox overlap candidates, exact WKB point filter; missing bbox excluded at source. No access/hours/coverage guarantee. Raw source metadata retained locally.",RawCategories:map[string]int{},AcceptedCategories:map[string]int{},Rejected:map[string]int{},BasicCategories:map[string]int{},PrimaryCategories:map[string]int{},OperatingStatus:map[string]int{},Confidence:map[string]int{},SourceDatasets:map[string]int{},SourceLicenses:map[string]int{}}
 if !ok{return nil,r,errors.New("unknown sample")}
 c:=csv.NewReader(io.LimitReader(in,rawCap+1));header,e:=c.Read();if e!=nil{return nil,r,e}
 if strings.Join(header,",")!="id,name,basic_category,primary_category,hierarchy,operating_status,confidence,wkb,sources"{return nil,r,errors.New("unexpected DuckDB CSV schema")}
 seen:=map[string]row{}
 for{
  f,e:=c.Read();if e==io.EOF{break};if e!=nil{return nil,r,e};r.Features++
  if r.Features>maxRows||c.InputOffset()>rawCap{return nil,r,errors.New("sample cap exceeded")}
  for _,v:=range f{if !utf8.ValidString(v)||len(v)>1<<20{return nil,r,errors.New("invalid or oversized CSV field")}}
  // DuckDB CLI CSV uses uppercase NULL for SQL null. Normalize only the
  // nullable fields; ambiguous literal NULL names/IDs are rejected, not invented.
  for _,i:=range []int{1,2,3,4,5,6,8}{if f[i]=="NULL"{f[i]=""}}
  if f[0]=="NULL"{r.Rejected["invalid_id"]++;continue}
  lat,lon,ok:=point(f[7]);if !ok{r.Rejected["invalid_point"]++;continue}
  if lon<b.W||lon>b.E||lat<b.S||lat>b.N{r.Rejected["outside_sample"]++;continue}
  r.BasicCategories[counter(f[2])]++;r.PrimaryCategories[counter(f[3])]++;r.OperatingStatus[counter(f[5])]++
  var hier []string
  if f[4]!=""&&json.Unmarshal([]byte(f[4]),&hier)!=nil{return nil,r,errors.New("invalid hierarchy JSON")}
  cat:=category(f[2],f[3],hier);if cat==""{r.Rejected["unclassified"]++;continue};r.RawCategories[cat]++
  if f[6]==""{r.Confidence["missing"]++}else{
   v,e:=strconv.ParseFloat(f[6],64);if e!=nil||!finite(v)||v<0||v>1{r.Rejected["invalid_confidence"]++;continue}
   if v==0{r.Confidence["zero"]++}else if v<0.5{r.Confidence["below_0.5"]++}else if v<0.8{r.Confidence["0.5_to_0.8"]++}else{r.Confidence["0.8_or_above"]++}
   if v==0{r.Rejected["zero_confidence"]++;continue}
  }
  if f[5]=="permanently_closed"{r.Rejected["permanently_closed"]++;continue}
  if f[5]=="temporarily_closed"{r.Rejected["temporarily_closed"]++;continue}
  if f[5]!=""&&f[5]!="open"{r.Rejected["unknown_status_value"]++;continue}
  name:=strings.TrimSpace(f[1]);if !validName(name){r.Rejected["unnamed_or_invalid_name"]++;continue}
  if !validID.MatchString(f[0]){r.Rejected["invalid_id"]++;continue}
  v:=row{"poi-ovt-"+f[0],name,cat,lat,lon}
  if old,ok:=seen[v.ID];ok{if old!=v{return nil,r,errors.New("conflicting duplicate source identity")};r.Rejected["duplicate"]++;continue}
  var sources []struct{Dataset,License string}
  if f[8]!=""&&json.Unmarshal([]byte(f[8]),&sources)!=nil{return nil,r,errors.New("invalid source metadata JSON")}
  ds,ls:=map[string]bool{},map[string]bool{}
  for _,s:=range sources{ds[counter(s.Dataset)]=true;ls[counter(s.License)]=true}
  if len(sources)==0{ds["(missing)"]=true;ls["(missing)"]=true}
  for k:=range ds{r.SourceDatasets[k]++};for k:=range ls{r.SourceLicenses[k]++}
  seen[v.ID]=v;r.AcceptedCategories[cat]++
 }
 out:=make([]row,0,len(seen));for _,v:=range seen{out=append(out,v)};sort.Slice(out,func(i,j int)bool{return out[i].ID<out[j].ID});r.Rows=len(out);return out,r,nil
}
func writeCSV(p string,rows []row)error{
 f,e:=os.OpenFile(p,os.O_CREATE|os.O_EXCL|os.O_WRONLY,0600);if e!=nil{return e}
 w:=csv.NewWriter(&capped{f,rawCap});e=w.Write([]string{"id","lat","lon","category","name"})
 for _,r:=range rows{if e!=nil{break};e=w.Write([]string{r.ID,strconv.FormatFloat(r.Lat,'f',-1,64),strconv.FormatFloat(r.Lon,'f',-1,64),r.Category,r.Name})}
 w.Flush();if e==nil{e=w.Error()};if x:=f.Close();e==nil{e=x};return e
}
func sample(name string)error{
 b,ok:=boxes[name];if !ok{return errors.New("sample must be delta or sf")}
 if e:=verifySource();e!=nil{return e}
 ctx,cancel:=context.WithTimeout(context.Background(),15*time.Minute);defer cancel()
 id,arch,e:=localImage(ctx);if e!=nil{return e}
 dir:=filepath.Join(studyPath,name);if e=os.Mkdir(dir,0700);e!=nil{return fmt.Errorf("sample must not exist: %w",e)}
 p,e:=filepath.Abs(filepath.Join(studyPath,"places.parquet"));if e!=nil{return e}
 // A unique token prevents cleanup from targeting any pre-existing container.
 token,e:=os.MkdirTemp(dir,"run-");if e!=nil{return e}
 container:="sailinggo-ovt-"+name+"-"+filepath.Base(token)
 raw:=filepath.Join(dir,"features.csv");out,e:=os.OpenFile(raw,os.O_CREATE|os.O_EXCL|os.O_WRONLY,0600);if e!=nil{return e}
 fmt.Fprintf(os.Stderr,"Offline DuckDB extraction %s (%s %s)\n",name,id,arch)
 start:=time.Now();e=extract(ctx,p,container,id,b,out);elapsed:=time.Since(start).Seconds();if x:=out.Close();e==nil{e=x};if e!=nil{return e}
 info,e:=os.Stat(raw);if e!=nil{return e}
 in,e:=os.Open(raw);if e!=nil{return e};defer in.Close();start=time.Now()
 rows,rep,e:=normalize(in,name);if e!=nil{return e}
 rep.ImageID,rep.Architecture,rep.ExtractSeconds,rep.NormalizeSeconds,rep.RawBytes=id,arch,elapsed,time.Since(start).Seconds(),info.Size()
 if e=writeCSV(filepath.Join(dir,"normalized.csv"),rows);e!=nil{return e};if e=saveJSON(filepath.Join(dir,"report.json"),rep);e!=nil{return e};return printJSON(rep)
}
// resumeNormalizing reuses the owner's completed extraction after a parser
// failure. It cannot independently certify that an older CSV was complete.
func resumeNormalizing(name string)error{
 if _,ok:=boxes[name];!ok{return errors.New("sample must be delta or sf")}
 if e:=verifySource();e!=nil{return e}
 dir:=filepath.Join(studyPath,name);if e:=realDir(dir);e!=nil{return e}
 raw:=filepath.Join(dir,"features.csv");st,e:=os.Lstat(raw)
 if e!=nil||!st.Mode().IsRegular()||st.Size()<=0||st.Size()>rawCap{return errors.New("invalid saved CSV")}
 for _,p:=range []string{"normalized.csv","report.json"}{if _,e:=os.Lstat(filepath.Join(dir,p));!os.IsNotExist(e){return errors.New("normalization outputs must not exist; preserve existing files")}}
 in,e:=os.Open(raw);if e!=nil{return e};defer in.Close()
 start:=time.Now();rows,rep,e:=normalize(in,name);if e!=nil{return e}
 rep.RawBytes=st.Size();rep.NormalizeSeconds=time.Since(start).Seconds()
 rep.Note+=" Reused owner-confirmed completed extraction; original image/time not measured by this command, blank image/zero extraction seconds are unavailable, not new extraction measurements."
 if e=writeCSV(filepath.Join(dir,"normalized.csv"),rows);e!=nil{return e}
 if e=saveJSON(filepath.Join(dir,"report.json"),rep);e!=nil{return e}
 return printJSON(rep)
}
func printJSON(v any)error{e:=json.NewEncoder(os.Stdout);e.SetIndent("","  ");return e.Encode(v)}
func run(args []string)error{
 if len(args)==0{return errors.New("use download [-apply], sample|normalize|audit -sample delta|sf")}
 fs:=flag.NewFlagSet(args[0],flag.ContinueOnError)
 switch args[0]{
 case "download":a:=fs.Bool("apply",false,"perform bounded download");if e:=fs.Parse(args[1:]);e!=nil{return e};if fs.NArg()!=0{return errors.New("unexpected argument")};return download(*a)
 case "audit":n:=fs.String("sample","","delta or sf");review:=fs.Bool("review",false,"include deterministic local-only record examples; do not share raw output");if e:=fs.Parse(args[1:]);e!=nil{return e};if fs.NArg()!=0{return errors.New("unexpected argument")};return runAudit(*n,*review)
 case "sample","normalize":n:=fs.String("sample","","delta or sf");if e:=fs.Parse(args[1:]);e!=nil{return e};if fs.NArg()!=0{return errors.New("unexpected argument")};if args[0]=="normalize"{return resumeNormalizing(*n)};return sample(*n)
 };return errors.New("unknown subcommand")
}
func main(){if e:=run(os.Args[1:]);e!=nil{fmt.Fprintln(os.Stderr,e);os.Exit(1)}}
