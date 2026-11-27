package main

import(
 "bufio"
 "context"
 "crypto/sha256"
 "encoding/binary"
 "encoding/csv"
 "encoding/hex"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "math"
 "os"
 "os/exec"
 "path/filepath"
 "regexp"
 "runtime"
 "sort"
 "strconv"
 "strings"
 "time"
 "unicode"
 "unicode/utf16"
 "unicode/utf8"
)
const created="2026-11-18T00:00:00.000Z"
var cats=[]string{"coffee","ev_charging","fuel","grocery","lodging","parking","restaurant"}
var tokenGroups=[][]string{{"coffee_shop","cafe","coffeehouse"},{"electric_vehicle_charging_station","ev_charging_station","electric_vehicle_charging","charging_station"},{"gas_station"},{"grocery_store","supermarket"},{"lodging","hotel","motel","hostel","guest_house"},{"parking_lot","parking_garage","parking"},{"restaurant","fast_food_restaurant","casual_eatery"}}
var excludes=map[string]bool{"internet_cafe":true,"animal_cafe":true,"food_truck_stand":true,"campground":true,"holiday_rental_home":true,"private_lodging":true,"serviced_apartment":true,"service_apartment":true}
var licenses=map[string]bool{"CDLA-Permissive-2.0":true,"Apache-2.0":true,"CC0-1.0":true}
var validID=regexp.MustCompile("^[A-Za-z0-9_-]{1,72}$")
type waypoint struct{ID string `json:"id"`;Lat float64 `json:"lat"`;Lon float64 `json:"lon"`;Name string `json:"name"`;Category string `json:"category"`;Notes string `json:"notes"`;Created string `json:"created"`}
type provenance struct{ID string;Sources json.RawMessage;Confidence float64;Status string;Basic,Primary string}
type stats struct{Rows,Features,Bands int;Rejected,Categories,Licenses map[string]int;RawBytes int64}
type entry struct{Lat,Lon float64;Offset uint64;Length,Cat uint32}
type generation struct{
 Schema int;Release,Mapping,Created,Publication,ImageID,Architecture string
 Sources []source
 Rings,Edges int
 Stats stats
 PayloadSHA256,IndexSHA256,ProvenanceSHA256 string
 PayloadBytes,IndexBytes,ProvenanceBytes int64
 BuildSeconds float64
 GoHeapBytes,GoSysBytes uint64
}
func nameOK(s string)bool{if strings.TrimSpace(s)==""||!utf8.ValidString(s)||len(utf16.Encode([]rune(s)))>80{return false};for _,r:=range s{if unicode.IsControl(r)||r==utf8.RuneError||r==0xfffe||r==0xffff{return false}};return true}
func point(s string)(float64,float64,bool){r,e:=hex.DecodeString(s);if e!=nil||len(r)!=21{return 0,0,false};var o binary.ByteOrder=binary.LittleEndian;if r[0]==0{o=binary.BigEndian}else if r[0]!=1{return 0,0,false};if o.Uint32(r[1:5])!=1{return 0,0,false};x,y:=math.Float64frombits(o.Uint64(r[5:13])),math.Float64frombits(o.Uint64(r[13:21]));return x,y,finite(x)&&finite(y)&&math.Abs(x)<=180&&math.Abs(y)<=90}
func category(b,p string,h []string)int{
 if excludes[b]||excludes[p]{return -1}
 for _,v:=range h{if excludes[v]{return -1}}
 for i,g:=range tokenGroups{for _,k:=range g{if b==k||p==k{return i};for _,v:=range h{if v==k{return i}}}}
 return -1
}
func sql(s,n float64)string{
 var ts []string;for _,g:=range tokenGroups{for _,k:=range g{ts=append(ts,"'"+k+"'")}};tokens:=strings.Join(ts,",")
 return fmt.Sprintf("SET autoinstall_known_extensions=false; SET autoload_known_extensions=false; SET enable_geoparquet_conversion=false; SET threads=2; SET memory_limit='2GB'; SET temp_directory='/tmp/duckdb'; SET max_temp_directory_size='512MB'; SELECT id, names.primary AS name, basic_category, taxonomy.primary AS primary_category, CAST(taxonomy.hierarchy AS JSON) AS hierarchy, operating_status, confidence, hex(geometry) AS wkb, CAST(sources AS JSON) AS sources FROM read_parquet('/input/places.parquet') WHERE bbox.xmin <= -114 AND bbox.xmax >= -125 AND bbox.ymin <= %.8f AND bbox.ymax >= %.8f AND (basic_category IN (%s) OR taxonomy.primary IN (%s) OR list_has_any(taxonomy.hierarchy,[%s])) ORDER BY id LIMIT %d;",n,s,tokens,tokens,tokens,maxBandRows+1)
}
func extract(ctx context.Context,input,container,id string,s,n float64,out io.Writer)error{
 ctx,cancel:=context.WithTimeout(ctx,15*time.Minute);defer cancel()
 cmd:=exec.CommandContext(ctx,"docker","run","--rm","--name",container,"--pull=never","--network","none","--read-only","--cap-drop","ALL","--security-opt","no-new-privileges","--memory","4g","--cpus","2","--pids-limit","128","--tmpfs","/tmp:rw,noexec,nosuid,size=1g","--user",fmt.Sprintf("%d:%d",os.Getuid(),os.Getgid()),"--mount","type=bind,source="+input+",target=/input/places.parquet,readonly","--entrypoint","/duckdb",id,"-batch","-bail","-csv","-header","-c",sql(s,n))
 cmd.Stdout=&capped{out,rawCap};cmd.Stderr=&capped{os.Stderr,1<<20}
 if e:=cmd.Run();e!=nil{cleanup,cancel:=context.WithTimeout(context.Background(),15*time.Second);defer cancel();_=exec.CommandContext(cleanup,"docker","rm","-f",container).Run();return e};return nil
}
type builder struct{boundary *stateBoundary; stats stats;seen map[string][32]byte;entries []entry;payload,prov *bufio.Writer;offset,provBytes int64}
func newBuilder(b *stateBoundary,p,v io.Writer)*builder{return &builder{boundary:b,stats:stats{Rejected:map[string]int{},Categories:map[string]int{},Licenses:map[string]int{}},seen:map[string][32]byte{},payload:bufio.NewWriterSize(p,1<<20),prov:bufio.NewWriterSize(v,1<<20)}}
func(b *builder)consume(in io.Reader,s,n float64)error{
 c:=csv.NewReader(io.LimitReader(in,rawCap+1));hdr,e:=c.Read();if e!=nil{return e};if strings.Join(hdr,",")!="id,name,basic_category,primary_category,hierarchy,operating_status,confidence,wkb,sources"{return errors.New("unexpected CSV schema")}
 count:=0
 for{f,e:=c.Read();if e==io.EOF{break};if e!=nil{return e};count++;b.stats.Features++;if count>maxBandRows||c.InputOffset()>rawCap||b.stats.Features>4000000{return errors.New("candidate cap exceeded")}
  for i,v:=range f{if !utf8.ValidString(v)||len(v)>1<<20{return errors.New("invalid CSV field")};if v=="NULL"{f[i]=""}}
  x,y,ok:=point(f[7]);if !ok{b.stats.Rejected["invalid_point"]++;continue}
  if !inEnvelope(x,y)||y<s||(y>=n&&n!=42.1)||y>n{b.stats.Rejected["outside_band"]++;continue}
  var h []string;if f[4]!=""&&json.Unmarshal([]byte(f[4]),&h)!=nil{return errors.New("invalid taxonomy")}
  cat:=category(f[2],f[3],h);if cat<0{b.stats.Rejected["taxonomy"]++;continue}
  inside,near:=b.boundary.contains(x,y);if near{b.stats.Rejected["boundary_5m_quarantine"]++;continue};if !inside{b.stats.Rejected["outside_state"]++;continue}
  confidence,e:=strconv.ParseFloat(f[6],64);if e!=nil||!finite(confidence)||confidence<0.5||confidence>1{b.stats.Rejected["confidence"]++;continue}
  if f[5]!=""&&f[5]!="open"{b.stats.Rejected["operating_status"]++;continue}
  name:=strings.TrimSpace(f[1]);if !nameOK(name)||!validID.MatchString(f[0]){b.stats.Rejected["identity_or_name"]++;continue}
  var sources []struct{Dataset,License string};if f[8]!=""&&json.Unmarshal([]byte(f[8]),&sources)!=nil{return errors.New("invalid source JSON")}
  good:=len(sources)>0;ls:=map[string]bool{};for _,v:=range sources{if strings.TrimSpace(v.Dataset)==""||!licenses[v.License]{good=false};ls[v.License]=true}
  if !good{b.stats.Rejected["missing_or_unknown_license"]++;continue}
  w:=waypoint{"poi-ovt-"+f[0],y,x,name,cats[cat],"Source: Overture Maps Places 2026-11-18. Location, access and operating hours unverified.",created}
  if f[5]==""{w.Notes+=" Operating status unknown."}
  p,e:=json.Marshal(w);if e!=nil{return e}
  prov,e:=json.Marshal(provenance{w.ID,json.RawMessage(f[8]),confidence,f[5],f[2],f[3]});if e!=nil{return e}
  hash:=sha256.Sum256(append(append([]byte{},p...),prov...))
  if old,ok:=b.seen[w.ID];ok{if old!=hash{return errors.New("conflicting duplicate source ID")};b.stats.Rejected["exact_duplicate"]++;continue}
  if len(b.entries)>=maxRows||b.offset+int64(len(p))>fileCap||b.provBytes+int64(len(prov))+1>fileCap{return errors.New("generation cap exceeded")}
  if _,e=b.payload.Write(p);e!=nil{return e};if _,e=b.prov.Write(append(prov,'\n'));e!=nil{return e}
  b.entries=append(b.entries,entry{y,x,uint64(b.offset),uint32(len(p)),uint32(cat)});b.offset+=int64(len(p));b.provBytes+=int64(len(prov))+1;b.seen[w.ID]=hash;b.stats.Rows++;b.stats.Categories[w.Category]++;for l:=range ls{b.stats.Licenses[l]++}
 }
 b.stats.RawBytes+=c.InputOffset();if b.stats.RawBytes>8<<30{return errors.New("total extraction body cap")};b.stats.Bands++;return nil
}
func cell(e entry)(int,int){return int(math.Floor(e.Lon/.05)),int(math.Floor(e.Lat/.05))}
func less(a,b entry)bool{x,y:=cell(a);u,v:=cell(b);if x!=u{return x<u};if y!=v{return y<v};return a.Offset<b.Offset}
func writeIndex(p string,entries []entry)error{
 sort.Slice(entries,func(i,j int)bool{return less(entries[i],entries[j])})
 f,e:=os.OpenFile(p,os.O_CREATE|os.O_EXCL|os.O_WRONLY,0600);if e!=nil{return e};defer f.Close()
 w:=bufio.NewWriterSize(f,1<<20);for _,v:=range entries{if e=binary.Write(w,binary.LittleEndian,v);e!=nil{return e}};if e=w.Flush();e!=nil{return e};return f.Sync()
}
func build()error{
 start:=time.Now()
 if e:=realDir("private");e!=nil{return e};if e:=realDir(study);e!=nil{return e}
 if _,e:=os.Lstat(filepath.Join(study,"generation"));!os.IsNotExist(e){return errors.New("generation already exists")}
 a,e:=north();if e!=nil{return e};s,e:=checkSource(study,"south.parquet",southURL,release,southBytes,southBytes+1,"");if e!=nil{return e}
 z,e:=checkSource(study,"state.zip",boundaryURL,"TIGER2026",0,zipCap,"");if e!=nil{return e}
 boundary,e:=loadBoundary(filepath.Join(study,"state.zip"));if e!=nil{return e}
 ctx,cancel:=context.WithTimeout(context.Background(),2*time.Hour);defer cancel()
 id,arch,e:=localImage(ctx);if e!=nil{return e}
 dir,e:=os.MkdirTemp(study,"stage-");if e!=nil{return e};fmt.Fprintln(os.Stderr,"Staging",dir)
 p,e:=os.OpenFile(filepath.Join(dir,"payload.bin"),os.O_CREATE|os.O_EXCL|os.O_WRONLY,0600);if e!=nil{return e};defer p.Close()
 v,e:=os.OpenFile(filepath.Join(dir,"provenance.jsonl"),os.O_CREATE|os.O_EXCL|os.O_WRONLY,0600);if e!=nil{return e};defer v.Close()
 b:=newBuilder(boundary,p,v)
 for _,src:=range []struct{path string;lo,hi int}{{filepath.Join(study,"south.parquet"),32,35},{filepath.Join(northDir,"places.parquet"),34,43}}{
  input,e:=filepath.Abs(src.path);if e!=nil{return e}
  for j:=src.lo;j<src.hi;j++{
   n:=math.Min(float64(j+1),42.1);if float64(j)>=n{continue}
   fmt.Fprintf(os.Stderr,"Extracting %s latitude %d–%.1f; accepted %d\n",filepath.Base(input),j,n,b.stats.Rows)
   tmp,e:=os.CreateTemp(dir,"band-");if e!=nil{return e}
   container:="sailinggo-ca-"+filepath.Base(dir)+"-"+filepath.Base(tmp.Name())
   e=extract(ctx,input,container,id,float64(j),n,tmp)
   if e==nil{_,e=tmp.Seek(0,io.SeekStart)};if e==nil{e=b.consume(tmp,float64(j),n)}
   closeErr:=tmp.Close();if e==nil{e=closeErr};if e!=nil{return fmt.Errorf("band retained at %s: %w",tmp.Name(),e)}
   if e=os.Remove(tmp.Name());e!=nil{return e}
  }
 }
 if b.stats.Rows==0{return errors.New("empty generation")}
 for _,w:=range []*bufio.Writer{b.payload,b.prov}{if e=w.Flush();e!=nil{return e}}
 for _,f:=range []*os.File{p,v}{if e=f.Sync();e!=nil{return e};if e=f.Close();e!=nil{return e}}
 if e=writeIndex(filepath.Join(dir,"index.bin"),b.entries);e!=nil{return e}
 g:=generation{Schema:1,Release:release,Mapping:mapping,Created:created,Publication:"local study only; not cleared for publication",ImageID:id,Architecture:arch,Sources:[]source{a,s,z},Rings:boundary.Rings,Edges:boundary.Edges,Stats:b.stats}
 g.PayloadSHA256,g.PayloadBytes,e=digest(filepath.Join(dir,"payload.bin"),fileCap);if e!=nil{return e}
 g.IndexSHA256,g.IndexBytes,e=digest(filepath.Join(dir,"index.bin"),32*maxRows);if e!=nil{return e}
 g.ProvenanceSHA256,g.ProvenanceBytes,e=digest(filepath.Join(dir,"provenance.jsonl"),fileCap);if e!=nil{return e}
 g.BuildSeconds=time.Since(start).Seconds();var mem runtime.MemStats;runtime.ReadMemStats(&mem);g.GoHeapBytes,g.GoSysBytes=mem.HeapAlloc,mem.Sys
 if e=saveJSON(filepath.Join(dir,"manifest.json"),g);e!=nil{return e}
 if _,_,e=checkGeneration(dir,false);e!=nil{return e}
 if _,e=os.Lstat(filepath.Join(study,"generation"));!os.IsNotExist(e){return errors.New("generation destination exists")}
 if e=os.Rename(dir,filepath.Join(study,"generation"));e!=nil{return e};return printJSON(g)
}
