// Local staged statewide study; never installs or serves a dataset.
package main

import (
 "archive/zip"
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "errors"
 "flag"
 "fmt"
 "io"
 "net/http"
 "os"
 "os/exec"
 "path/filepath"
 "regexp"
 "strings"
 "time"
)

const release="2026-11-18.0"
const imageTag="duckdb/duckdb:1.4.2"
const study="private/poi-california-2026-11-18.0"
const northDir="private/poi-overture-2026-11-18.0"
const northBytes int64=836166912
const northHash="be8157302e8d94b648f884ae65b10f6073960b439dd0aceda2e145c75bfe5441"
const baseURL="https://overturemaps-us-west-2.s3.us-west-2.amazonaws.com/release/"+release+"/theme=places/type=place/"
const northURL=baseURL+"part-00002-5783d80c-d44a-5845-9057-3a39013540b0-c000.zstd.parquet"
const southURL=baseURL+"part-00001-43e455e5-5fd3-50ee-8b56-2de40da4223e-c000.zstd.parquet"
const southBytes int64=823024945
const boundaryURL="https://www2.census.gov/geo/tiger/TIGER2026/STATE/tl_2026_us_state.zip"
const zipCap int64=32<<20
const transferCap int64=1000000000
const rawCap int64=256<<20
const maxRows=1000000
const maxBandRows=300000
const fileCap int64=1<<30
const mapping="ca-terrestrial-v1"
type source struct { Release,URL,SHA256,ETag,LastModified,DownloadedAt string; Bytes int64; ElapsedSeconds float64 }
func realDir(p string)error{st,e:=os.Lstat(p);if e!=nil{return e};if !st.IsDir()||st.Mode()&os.ModeSymlink!=0{return errors.New("not a real directory")};return nil}
func regular(p string,cap int64)(int64,error){st,e:=os.Lstat(p);if e!=nil{return 0,e};if !st.Mode().IsRegular()||st.Size()<=0||st.Size()>cap{return 0,fmt.Errorf("invalid file: %s",p)};return st.Size(),nil}
func readJSON(p string,cap int64,v any)error{if _,e:=regular(p,cap);e!=nil{return e};b,e:=os.ReadFile(p);if e!=nil{return e};return json.Unmarshal(b,v)}
func printJSON(v any)error{e:=json.NewEncoder(os.Stdout);e.SetIndent("","  ");return e.Encode(v)}
func saveJSON(p string,v any)error{f,e:=os.OpenFile(p,os.O_CREATE|os.O_EXCL|os.O_WRONLY,0600);if e!=nil{return e};enc:=json.NewEncoder(f);enc.SetIndent("","  ");e=enc.Encode(v);if e==nil{e=f.Sync()};if x:=f.Close();e==nil{e=x};return e}
func digest(p string,cap int64)(string,int64,error){n,e:=regular(p,cap);if e!=nil{return "",0,e};f,e:=os.Open(p);if e!=nil{return "",0,e};defer f.Close();h:=sha256.New();got,e:=io.Copy(h,io.LimitReader(f,cap+1));if e!=nil||got!=n{return "",0,errors.New("file hash read failed")};return hex.EncodeToString(h.Sum(nil)),n,nil}
func magic(p string,n int64)error{f,e:=os.Open(p);if e!=nil{return e};defer f.Close();var a,z [4]byte;if _,e=f.ReadAt(a[:],0);e!=nil{return e};if _,e=f.ReadAt(z[:],n-4);e!=nil{return e};if string(a[:])!="PAR1"||string(z[:])!="PAR1"{return errors.New("invalid Parquet magic")};return nil}
func checkSource(dir,name,url,rel string,expected,cap int64,knownHash string)(source,error){
 var m source;if e:=realDir(dir);e!=nil{return m,e}
 if e:=readJSON(filepath.Join(dir,name+".json"),8192,&m);e!=nil{return m,e}
 if m.URL!=url||m.Release!=rel||m.Bytes<=0||m.Bytes>cap||(expected>0&&m.Bytes!=expected)||(knownHash!=""&&m.SHA256!=knownHash){return m,errors.New("source manifest mismatch")}
 p:=filepath.Join(dir,name);h,n,e:=digest(p,cap);if e!=nil||n!=m.Bytes||h!=m.SHA256{return m,errors.New("source hash/size mismatch")}
 if strings.HasSuffix(name,".parquet"){if e=magic(p,n);e!=nil{return m,e}}
 return m,nil
}
func north()(source,error){
 var m source;if e:=realDir(northDir);e!=nil{return m,e};if e:=readJSON(filepath.Join(northDir,"source.json"),8192,&m);e!=nil{return m,e}
 if m.Release!=release||m.URL!=northURL||m.Bytes!=northBytes||m.SHA256!=northHash{return m,errors.New("north manifest mismatch")}
 p:=filepath.Join(northDir,"places.parquet");h,n,e:=digest(p,northBytes);if e!=nil||n!=northBytes||h!=northHash{return m,errors.New("north source mismatch")};return m,magic(p,n)
}
type progress struct{w io.Writer;n,total int64;last time.Time}
func(p *progress)Write(b []byte)(int,error){n,e:=p.w.Write(b);p.n+=int64(n);if time.Since(p.last)>=time.Second||p.n==p.total{fmt.Fprintf(os.Stderr,"\r%.1f / %.1f MiB (%.1f%%)",float64(p.n)/(1<<20),float64(p.total)/(1<<20),100*float64(p.n)/float64(p.total));p.last=time.Now()};return n,e}
func download(ctx context.Context,name,url,rel string,expected,cap int64)(source,error){
 var m source
 if _,e:=os.Lstat(filepath.Join(study,name));e==nil{return checkSource(study,name,url,rel,expected,cap,"")}else if !os.IsNotExist(e){return m,e}
 if _,e:=os.Lstat(filepath.Join(study,name+".json"));!os.IsNotExist(e){return m,errors.New("manifest exists without source")}
 ctx,cancel:=context.WithTimeout(ctx,15*time.Minute);defer cancel()
 tr:=http.DefaultTransport.(*http.Transport).Clone();tr.Proxy=nil;tr.DisableCompression=true;defer tr.CloseIdleConnections()
 c:=&http.Client{Transport:tr,CheckRedirect:func(*http.Request,[]*http.Request)error{return errors.New("redirect refused")}}
 req,e:=http.NewRequestWithContext(ctx,"GET",url,nil);if e!=nil{return m,e};req.Header.Set("Accept-Encoding","identity")
 start:=time.Now();res,e:=c.Do(req);if e!=nil{return m,e};defer res.Body.Close()
 if res.StatusCode!=200||res.ContentLength>cap||(expected>0&&res.ContentLength!=-1&&res.ContentLength!=expected)||(res.Header.Get("Content-Encoding")!=""&&!strings.EqualFold(res.Header.Get("Content-Encoding"),"identity")){return m,fmt.Errorf("unexpected source response %d length %d",res.StatusCode,res.ContentLength)}
 part:=filepath.Join(study,name+".partial");f,e:=os.OpenFile(part,os.O_CREATE|os.O_EXCL|os.O_WRONLY,0600);if e!=nil{return m,e}
 fmt.Fprintln(os.Stderr,"Acquiring",name)
 hash:=sha256.New();total:=res.ContentLength;if total<=0{total=cap};p:=&progress{w:io.MultiWriter(f,hash),total:total}
 n,e:=io.Copy(p,io.LimitReader(res.Body,cap));fmt.Fprintln(os.Stderr)
 if e==nil&&(n==cap&&(expected==0||n!=expected)){e=errors.New("body cap reached before EOF")}
 if e==nil&&expected>0&&n!=expected{e=errors.New("unexpected source length")}
 if e==nil&&res.ContentLength>=0&&n!=res.ContentLength{e=errors.New("incomplete response")}
 // Known exact-length Parquet is read with cap > expected; EOF is observed.
 if e==nil&&n<=0{e=errors.New("empty source")}
 if e==nil{e=f.Sync()};if x:=f.Close();e==nil{e=x};if e!=nil{return m,e}
 if strings.HasSuffix(name,".parquet"){if e=magic(part,n);e!=nil{return m,e}}else{z,e:=zip.OpenReader(part);if e!=nil{return m,e};z.Close()}
 m=source{rel,url,hex.EncodeToString(hash.Sum(nil)),res.Header.Get("ETag"),res.Header.Get("Last-Modified"),time.Now().UTC().Format(time.RFC3339Nano),n,time.Since(start).Seconds()}
 if e=os.Rename(part,filepath.Join(study,name));e!=nil{return m,e};if e=saveJSON(filepath.Join(study,name+".json"),m);e!=nil{return m,e};return m,nil
}
func acquire()error{
 if e:=realDir("private");e!=nil{return e};n,e:=north();if e!=nil{return e}
 if e=os.Mkdir(study,0700);e!=nil&&!os.IsExist(e){return e};if e=realDir(study);e!=nil{return e}
 if southBytes+1+zipCap>transferCap{return errors.New("acquisition budget")}
 ctx,cancel:=context.WithTimeout(context.Background(),30*time.Minute);defer cancel()
 s,e:=download(ctx,"south.parquet",southURL,release,southBytes,southBytes+1);if e!=nil{return e}
 z,e:=download(ctx,"state.zip",boundaryURL,"TIGER2026",0,zipCap);if e!=nil{return e}
 return printJSON([]source{n,s,z})
}
func plan()error{return printJSON(map[string]any{"Study":study,"Release":release,"ReuseNorthBytes":northBytes,"NorthSHA256":northHash,"SouthURL":southURL,"SouthBytes":southBytes,"BoundaryURL":boundaryURL,"BoundaryCap":zipCap,"NewBodyCap":transferCap,"WorkingAllowanceGiB":8,"Mapping":mapping,"ConfidenceMinimum":0.5,"Publication":"local staged study only; no marine or runtime install","Commands":"acquire -apply; build; verify"})}
var imageID=regexp.MustCompile("^sha256:[0-9a-f]{64}$")
func localImage(ctx context.Context)(string,string,error){raw,e:=exec.CommandContext(ctx,"docker","image","inspect","--format","{{.Id}} {{.Architecture}}",imageTag).Output();if e!=nil{return "","",e};v:=strings.Fields(string(raw));if len(v)!=2||!imageID.MatchString(v[0])||(v[1]!="arm64"&&v[1]!="amd64"){return "","",errors.New("invalid image")};return v[0],v[1],nil}
type capped struct{w io.Writer;left int64}
func(c *capped)Write(p []byte)(int,error){if int64(len(p))>c.left{return 0,errors.New("output cap exceeded")};n,e:=c.w.Write(p);c.left-=int64(n);return n,e}
func run(args []string)error{
 if len(args)==0{return plan()};fs:=flag.NewFlagSet(args[0],flag.ContinueOnError)
 switch args[0]{case "plan","build","verify":if e:=fs.Parse(args[1:]);e!=nil{return e};if fs.NArg()!=0{return errors.New("unexpected argument")};switch args[0]{case "plan":return plan();case "build":return build();default:return verify()}
 case "acquire":a:=fs.Bool("apply",false,"perform bounded acquisition");if e:=fs.Parse(args[1:]);e!=nil{return e};if fs.NArg()!=0{return errors.New("unexpected argument")};if !*a{return plan()};return acquire()}
 return errors.New("use plan, acquire -apply, build, verify")
}
func main(){if e:=run(os.Args[1:]);e!=nil{fmt.Fprintln(os.Stderr,e);os.Exit(1)}}
