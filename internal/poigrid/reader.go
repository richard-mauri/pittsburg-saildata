// Package poigrid opens immutable local California POI schema-1 generations.
// No network, data acquisition, runtime routes or publication authority.
package poigrid

import (
 "bufio"
 "bytes"
 "crypto/sha256"
 "encoding/binary"
 "encoding/hex"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "math"
 "os"
 "path/filepath"
 "regexp"
 "sort"
 "strings"
 "sync"
 "unicode"
 "unicode/utf16"
 "unicode/utf8"
)
const Release="2026-11-18.0"
const Mapping="ca-terrestrial-v1"
const Created="2026-11-18T00:00:00.000Z"
const maxRows=1000000
const fileCap int64=1<<30
const recordSize=32
const baseNote="Source: Overture Maps Places 2026-11-18. Location, access and operating hours unverified."
var categories=[...]string{"coffee","ev_charging","fuel","grocery","lodging","parking","restaurant"}
var licenseSet=map[string]bool{"CDLA-Permissive-2.0":true,"Apache-2.0":true,"CC0-1.0":true}
var idPattern=regexp.MustCompile("^poi-ovt-[A-Za-z0-9_-]{1,72}$")
var hashPattern=regexp.MustCompile("^[0-9a-f]{64}$")
type Source struct{Release,URL,SHA256,ETag,LastModified,DownloadedAt string;Bytes int64;ElapsedSeconds float64}
type Stats struct{Rows,Features,Bands int;Rejected,Categories,Licenses map[string]int;RawBytes int64}
type Manifest struct{
 Schema int
 Release,Mapping,Created,Publication,ImageID,Architecture string
 Sources []Source
 Rings,Edges int
 Stats Stats
 PayloadSHA256,IndexSHA256,ProvenanceSHA256 string
 PayloadBytes,IndexBytes,ProvenanceBytes int64
 BuildSeconds float64
 GoHeapBytes,GoSysBytes uint64
}
type Waypoint struct{ID string `json:"id"`;Lat float64 `json:"lat"`;Lon float64 `json:"lon"`;Name string `json:"name"`;Category string `json:"category"`;Notes string `json:"notes"`;Created string `json:"created"`}
type provenance struct{ID string;Sources json.RawMessage;Confidence float64;Status string;Basic,Primary string}
type entry struct{Lat,Lon float64;Offset uint64;Length,Cat uint32}
type cellKey struct{X,Y int}
type span struct{Start,Count int}
type Evidence struct{Rows,Cells,ProvenanceRows int;LicenseCounts,DatasetLicenseCounts map[string]int}
type Reader struct{
 index,payload *os.File
 cells map[cellKey]span
 rows int
 payloadBytes int64
 slots chan struct{}
 mu sync.RWMutex
 closed bool
}
func finite(x float64)bool{return !math.IsNaN(x)&&!math.IsInf(x,0)}
func inEnvelope(x,y float64)bool{return finite(x)&&finite(y)&&x>=-125&&x<=-114&&y>=32&&y<=42.1}
func key(v entry)cellKey{return cellKey{int(math.Floor(v.Lon/.05)),int(math.Floor(v.Lat/.05))}}
func less(a,b entry)bool{x,y:=key(a),key(b);if x.X!=y.X{return x.X<y.X};if x.Y!=y.Y{return x.Y<y.Y};return a.Offset<b.Offset}
func validText(s string,n int)bool{if !utf8.ValidString(s)||strings.TrimSpace(s)==""||len(utf16.Encode([]rune(s)))>n{return false};for _,r:=range s{if unicode.IsControl(r)||r==utf8.RuneError||r==0xfffe||r==0xffff{return false}};return true}
// Check exact object keys, including duplicate keys. Disallow trailing JSON.
func object(raw []byte,keys []string)error{
 if !utf8.Valid(raw){return errors.New("invalid UTF-8")}
 d:=json.NewDecoder(bytes.NewReader(raw));t,e:=d.Token();if e!=nil||t!=json.Delim('{'){return errors.New("expected JSON object")}
 want:=map[string]bool{};for _,k:=range keys{want[k]=true}
 for d.More(){t,e=d.Token();if e!=nil{return e};k,ok:=t.(string);if !ok||!want[k]{return errors.New("unexpected or duplicate JSON key")};delete(want,k);var v json.RawMessage;if e=d.Decode(&v);e!=nil{return e};if bytes.Equal(v,[]byte("null")){return errors.New("null field")}}
 if t,e=d.Token();e!=nil||t!=json.Delim('}')||len(want)!=0{return errors.New("missing JSON key")}
 if _,e=d.Token();e!=io.EOF{return errors.New("trailing JSON")};return nil
}
func decodeWaypoint(raw []byte,v entry)(Waypoint,error){
 var w Waypoint
 if e:=object(raw,[]string{"id","lat","lon","name","category","notes","created"});e!=nil{return w,e}
 if json.Unmarshal(raw,&w)!=nil||v.Cat>=uint32(len(categories))||w.Lat!=v.Lat||w.Lon!=v.Lon||w.Category!=categories[v.Cat]||!inEnvelope(w.Lon,w.Lat)||!idPattern.MatchString(w.ID)||!validText(w.Name,80)||!validText(w.Notes,1000)||w.Created!=Created||(w.Notes!=baseNote&&w.Notes!=baseNote+" Operating status unknown."){return w,errors.New("invalid waypoint")}
 return w,nil
}
func file(dir,name string,n,cap int64)(*os.File,error){
 path:=filepath.Join(dir,name);st,e:=os.Lstat(path);if e!=nil{return nil,e}
 if !st.Mode().IsRegular()||st.Size()!=n||n<=0||n>cap{return nil,fmt.Errorf("invalid file %s",name)}
 f,e:=os.Open(path);if e!=nil{return nil,e};opened,e:=f.Stat();if e!=nil||!os.SameFile(st,opened)||opened.Size()!=n{f.Close();return nil,errors.New("file changed during open")};return f,nil
}
func checked(dir,name string,n,cap int64,sha string)(*os.File,error){
 if !hashPattern.MatchString(sha){return nil,errors.New("invalid hash")}
 f,e:=file(dir,name,n,cap);if e!=nil{return nil,e}
 h:=sha256.New();got,e:=io.Copy(h,io.LimitReader(f,n+1));if e!=nil||got!=n||hex.EncodeToString(h.Sum(nil))!=sha{f.Close();return nil,fmt.Errorf("integrity failure: %s",name)}
 if _,e=f.Seek(0,io.SeekStart);e!=nil{f.Close();return nil,e};return f,nil
}
func equalCounts(a,b map[string]int)bool{if len(a)!=len(b){return false};for k,v:=range a{if v<=0||b[k]!=v{return false}};return true}
func validManifest(m Manifest)bool{
 if m.Schema!=1||m.Release!=Release||m.Mapping!=Mapping||m.Created!=Created||m.Stats.Rows<1||m.Stats.Rows>maxRows||m.IndexBytes!=int64(m.Stats.Rows)*recordSize||len(m.Sources)!=3||m.Stats.Features<m.Stats.Rows||m.Stats.Features>4000000||m.Stats.Bands<1||m.Stats.Bands>12||m.Rings<1||m.Edges<1{return false}
 n:=m.Stats.Rows;for _,v:=range m.Stats.Rejected{if v<1||v>m.Stats.Features{return false};n+=v};if n!=m.Stats.Features{return false}
 const prefix="https://overturemaps-us-west-2.s3.us-west-2.amazonaws.com/release/"+Release+"/theme=places/type=place/"
 expected:=map[string]struct{rel string;n int64}{
 prefix+"part-00002-5783d80c-d44a-5845-9057-3a39013540b0-c000.zstd.parquet":{Release,836166912},
 prefix+"part-00001-43e455e5-5fd3-50ee-8b56-2de40da4223e-c000.zstd.parquet":{Release,823024945},
 "https://www2.census.gov/geo/tiger/TIGER2026/STATE/tl_2026_us_state.zip":{"TIGER2026",0},
 }
 for _,s:=range m.Sources{v,ok:=expected[s.URL];if !ok||s.Release!=v.rel||!hashPattern.MatchString(s.SHA256)||s.Bytes<=0||(v.n!=0&&s.Bytes!=v.n)||(v.n==0&&s.Bytes>32<<20){return false};delete(expected,s.URL)}
 return true
}
func(r *Reader)at(i int)(entry,error){
 var v entry;var raw [recordSize]byte
 if i<0||i>=r.rows{return v,errors.New("index ordinal out of bounds")}
 if _,e:=r.index.ReadAt(raw[:],int64(i)*recordSize);e!=nil{return v,e}
 v=entry{math.Float64frombits(binary.LittleEndian.Uint64(raw[:8])),math.Float64frombits(binary.LittleEndian.Uint64(raw[8:16])),binary.LittleEndian.Uint64(raw[16:24]),binary.LittleEndian.Uint32(raw[24:28]),binary.LittleEndian.Uint32(raw[28:32])}
 if v.Cat>=uint32(len(categories))||!inEnvelope(v.Lon,v.Lat)||v.Length==0||v.Length>8192||v.Offset>uint64(r.payloadBytes)||uint64(v.Length)>uint64(r.payloadBytes)-v.Offset{return v,errors.New("invalid index entry")}
 return v,nil
}
func(r *Reader)record(v entry)(json.RawMessage,Waypoint,error){p:=make([]byte,v.Length);if _,e:=r.payload.ReadAt(p,int64(v.Offset));e!=nil{return nil,Waypoint{},e};w,e:=decodeWaypoint(p,v);return p,w,e}
func(r *Reader)Close()error{r.mu.Lock();defer r.mu.Unlock();if r.closed{return nil};r.closed=true;return errors.Join(r.index.Close(),r.payload.Close())}
// Open verifies all three hashes on the opened descriptors, every index/payload
// entry, and one provenance row in payload order. The directory must remain
// immutable for the reader lifetime; no mutation/reload or trust signature.
func Open(dir string)(*Reader,Manifest,Evidence,error){
 var m Manifest;var ev Evidence
 st,e:=os.Lstat(dir);if e!=nil{return nil,m,ev,e};if !st.IsDir()||st.Mode()&os.ModeSymlink!=0{return nil,m,ev,errors.New("invalid generation directory")}
 path:=filepath.Join(dir,"manifest.json");st,e=os.Lstat(path);if e!=nil||!st.Mode().IsRegular()||st.Size()<=0||st.Size()>1<<20{return nil,m,ev,errors.New("invalid manifest file")}
 mf,e:=file(dir,"manifest.json",st.Size(),1<<20);if e!=nil{return nil,m,ev,e};raw,e:=io.ReadAll(io.LimitReader(mf,1<<20+1));mf.Close();if e!=nil||json.Unmarshal(raw,&m)!=nil||!validManifest(m){return nil,m,ev,errors.New("invalid manifest")}
 i,e:=checked(dir,"index.bin",m.IndexBytes,int64(recordSize*maxRows),m.IndexSHA256);if e!=nil{return nil,m,ev,e}
 p,e:=checked(dir,"payload.bin",m.PayloadBytes,fileCap,m.PayloadSHA256);if e!=nil{i.Close();return nil,m,ev,e}
 r:=&Reader{index:i,payload:p,cells:map[cellKey]span{},rows:m.Stats.Rows,payloadBytes:m.PayloadBytes,slots:make(chan struct{},4)}
 ok:=false;defer func(){if !ok{r.Close()}}()
 prov,e:=checked(dir,"provenance.jsonl",m.ProvenanceBytes,fileCap,m.ProvenanceSHA256);if e!=nil{return nil,m,ev,e};defer prov.Close()
 offsets:=make([]entry,0,r.rows);var prev entry
 for j:=0;j<r.rows;j++{v,e:=r.at(j);if e!=nil{return nil,m,ev,e};if j>0&&!less(prev,v){return nil,m,ev,errors.New("index order invalid")};prev=v;offsets=append(offsets,v);k:=key(v);s,exists:=r.cells[k];if !exists{s.Start=j};s.Count++;r.cells[k]=s}
 sort.Slice(offsets,func(i,j int)bool{return offsets[i].Offset<offsets[j].Offset})
 scan:=bufio.NewScanner(prov);scan.Buffer(make([]byte,64<<10),2<<20)
 ids:=make(map[string]bool,r.rows);counts:=map[string]int{};ev=Evidence{Rows:r.rows,Cells:len(r.cells),LicenseCounts:map[string]int{},DatasetLicenseCounts:map[string]int{}}
 var off uint64
 for _,v:=range offsets{
  if v.Offset!=off{return nil,m,ev,errors.New("payload gap or overlap")};off+=uint64(v.Length)
  _,w,e:=r.record(v);if e!=nil{return nil,m,ev,e};if ids[w.ID]{return nil,m,ev,errors.New("duplicate waypoint identity")};ids[w.ID]=true;counts[w.Category]++
  if !scan.Scan(){return nil,m,ev,errors.New("missing provenance record")}
  raw:=scan.Bytes();var pr provenance
  if object(raw,[]string{"ID","Sources","Confidence","Status","Basic","Primary"})!=nil||json.Unmarshal(raw,&pr)!=nil||pr.ID!=w.ID||!finite(pr.Confidence)||pr.Confidence<0.5||pr.Confidence>1||(pr.Status!=""&&pr.Status!="open")||(pr.Status=="")!=(w.Notes==baseNote+" Operating status unknown."){return nil,m,ev,errors.New("provenance mismatch")}
  var sources []struct{Dataset,License string};if json.Unmarshal(pr.Sources,&sources)!=nil||len(sources)==0||len(sources)>10000{return nil,m,ev,errors.New("invalid provenance sources")}
  ls,ds:=map[string]bool{},map[string]bool{}
  for _,s:=range sources{if !validText(s.Dataset,128)||!licenseSet[s.License]{return nil,m,ev,errors.New("unknown or missing source license")};ls[s.License]=true;ds[s.Dataset+" | "+s.License]=true}
  for l:=range ls{ev.LicenseCounts[l]++};for k:=range ds{ev.DatasetLicenseCounts[k]++;if len(ev.DatasetLicenseCounts)>256{return nil,m,ev,errors.New("source inventory cap")}}
  ev.ProvenanceRows++
 }
 if scan.Scan()||scan.Err()!=nil||off!=uint64(m.PayloadBytes)||!equalCounts(counts,m.Stats.Categories)||!equalCounts(ev.LicenseCounts,m.Stats.Licenses){return nil,m,ev,errors.New("generation counts or provenance length mismatch")}
 ok=true;return r,m,ev,nil
}
