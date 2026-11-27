package main

import(
 "bytes"
 "encoding/binary"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "math"
 "os"
 "path/filepath"
 "runtime"
 "sort"
 "time"
)
type cellKey struct{X,Y int}
type span struct{Start,Count int}
type grid struct{index,payload *os.File;cells map[cellKey]span;rows int}
func(g *grid)close(){g.index.Close();g.payload.Close()}
func(g *grid)at(i int)(entry,error){var v entry;var raw [32]byte;_,e:=g.index.ReadAt(raw[:],int64(i)*32);if e!=nil{return v,e};e=binary.Read(bytes.NewReader(raw[:]),binary.LittleEndian,&v);return v,e}
func(g *grid)payloadAt(v entry)([]byte,error){if v.Length==0||v.Length>8192{return nil,errors.New("invalid payload length")};p:=make([]byte,v.Length);_,e:=g.payload.ReadAt(p,int64(v.Offset));return p,e}
func openGrid(dir string,g generation)(*grid,error){
 i,e:=os.Open(filepath.Join(dir,"index.bin"));if e!=nil{return nil,e};p,e:=os.Open(filepath.Join(dir,"payload.bin"));if e!=nil{i.Close();return nil,e}
 q:=&grid{i,p,map[cellKey]span{},g.Stats.Rows};ok:=false;defer func(){if !ok{q.close()}}()
 var prev entry;offsets:=make([]entry,0,q.rows);counts:=map[string]int{}
 ids:=map[string]bool{}
 for j:=0;j<q.rows;j++{v,e:=q.at(j);if e!=nil{return nil,e}
  if v.Cat>=uint32(len(cats))||!inEnvelope(v.Lon,v.Lat)||v.Offset>uint64(g.PayloadBytes)||uint64(v.Length)>uint64(g.PayloadBytes)-v.Offset||(j>0&&!less(prev,v)){return nil,errors.New("invalid index entry/order")}
  prev=v;raw,e:=q.payloadAt(v);if e!=nil{return nil,e};var w waypoint;var fields map[string]json.RawMessage
  if json.Unmarshal(raw,&w)!=nil||json.Unmarshal(raw,&fields)!=nil||len(fields)!=7||w.Lat!=v.Lat||w.Lon!=v.Lon||w.Category!=cats[v.Cat]||!nameOK(w.Name)||len(w.ID)<9||w.ID[:8]!="poi-ovt-"||!validID.MatchString(w.ID[8:])||w.Created!=created||w.Notes==""||len(w.Notes)>1000||ids[w.ID]{return nil,errors.New("invalid waypoint payload")}
  ids[w.ID]=true;counts[w.Category]++;offsets=append(offsets,v)
  x,y:=cell(v);k:=cellKey{x,y};s,exists:=q.cells[k];if !exists{s.Start=j};s.Count++;q.cells[k]=s
 }
 sort.Slice(offsets,func(i,j int)bool{return offsets[i].Offset<offsets[j].Offset});var off uint64
 for _,v:=range offsets{if v.Offset!=off{return nil,errors.New("payload offset gap/overlap")};off+=uint64(v.Length)}
 if off!=uint64(g.PayloadBytes)||len(counts)!=len(g.Stats.Categories){return nil,errors.New("payload/count mismatch")};for k,n:=range counts{if g.Stats.Categories[k]!=n{return nil,errors.New("category count mismatch")}}
 ok=true;return q,nil
}
type query struct{W,S,E,N float64;Cat int}
func matches(v entry,q query)bool{return v.Lon>=q.W&&v.Lon<=q.E&&v.Lat>=q.S&&v.Lat<=q.N&&(q.Cat<0||uint32(q.Cat)==v.Cat)}
func(g *grid)search(q query,scan bool)([][]byte,bool,error){
 out:=make([][]byte,0,500)
 consume:=func(start,n int)(bool,error){for i:=start;i<start+n;i++{v,e:=g.at(i);if e!=nil{return false,e};if !matches(v,q){continue};if len(out)==500{return true,nil};p,e:=g.payloadAt(v);if e!=nil{return false,e};out=append(out,p)};return false,nil}
 if scan{t,e:=consume(0,g.rows);return out,t,e}
 for x:=int(math.Floor(q.W/.05));x<=int(math.Floor(q.E/.05));x++{for y:=int(math.Floor(q.S/.05));y<=int(math.Floor(q.N/.05));y++{s:=g.cells[cellKey{x,y}];t,e:=consume(s.Start,s.Count);if t||e!=nil{return out,t,e}}}
 return out,false,nil
}
type benchmark struct{Queries,Rows,Cells,Truncated int;GridMS,ScanMS,GridP95MS float64;PayloadReturnedBytes int64;GoHeapBytes,GoSysBytes uint64;Parity string}
func checkGeneration(dir string,measure bool)(generation,benchmark,error){
 var m generation;var b benchmark
 if e:=realDir(dir);e!=nil{return m,b,e}
 if e:=readJSON(filepath.Join(dir,"manifest.json"),1<<20,&m);e!=nil{return m,b,e}
 if m.Schema!=1||m.Release!=release||m.Mapping!=mapping||m.Created!=created||m.Stats.Rows<1||m.Stats.Rows>maxRows||m.IndexBytes!=int64(m.Stats.Rows)*32||len(m.Sources)!=3{return m,b,errors.New("invalid generation manifest")}
 for _,v:=range []struct{name,sha string;n,cap int64}{{"payload.bin",m.PayloadSHA256,m.PayloadBytes,fileCap},{"index.bin",m.IndexSHA256,m.IndexBytes,32*maxRows},{"provenance.jsonl",m.ProvenanceSHA256,m.ProvenanceBytes,fileCap}}{h,n,e:=digest(filepath.Join(dir,v.name),v.cap);if e!=nil||h!=v.sha||n!=v.n{return m,b,fmt.Errorf("generation integrity failure: %s",v.name)}}
 g,e:=openGrid(dir,m);if e!=nil{return m,b,e};defer g.close()
 if !measure{return m,b,nil}
 runtime.GC();var mem runtime.MemStats;runtime.ReadMemStats(&mem)
 b=benchmark{Rows:g.rows,Cells:len(g.cells),GoHeapBytes:mem.HeapAlloc,GoSysBytes:mem.Sys,Parity:"pass"}
 qs:=[]query{{-122.43,37.77,-122.39,37.81,-1},{-118.30,34.02,-118.20,34.10,-1},{-121.94,38,-121.84,38.06,-1},{-124.2,41.7,-123.7,42.1,-1},{-114.8,32.6,-114.4,33.2,-1},{-122.43,37.77,-122.39,37.81,2},{-118.30,34.02,-118.20,34.10,0},{-120,36,-119,37,-1}}
 // 24 deterministic data-anchored queries, including category-specific cases.
 for i:=0;i<24;i++{v,e:=g.at(i*(g.rows-1)/23);if e!=nil{return m,b,e};cat:=-1;if i%2==0{cat=int(v.Cat)};qs=append(qs,query{v.Lon-.025,v.Lat-.025,v.Lon+.025,v.Lat+.025,cat})}
 var times []float64
 for _,q:=range qs{
  start:=time.Now();a,t,e:=g.search(q,false);elapsed:=float64(time.Since(start))/float64(time.Millisecond);if e!=nil{return m,b,e};b.GridMS+=elapsed;times=append(times,elapsed)
  start=time.Now();z,u,e:=g.search(q,true);b.ScanMS+=float64(time.Since(start))/float64(time.Millisecond);if e!=nil{return m,b,e}
  if t!=u||len(a)!=len(z){return m,b,errors.New("query parity failure")};for i:=range a{if !bytes.Equal(a[i],z[i]){return m,b,errors.New("payload parity failure")};b.PayloadReturnedBytes+=int64(len(a[i]))}
  if t{b.Truncated++};b.Queries++
 }
 sort.Float64s(times);b.GridP95MS=times[int(math.Ceil(.95*float64(len(times))))-1];return m,b,nil
}
func verify()error{if e:=realDir("private");e!=nil{return e};if e:=realDir(study);e!=nil{return e};m,b,e:=checkGeneration(filepath.Join(study,"generation"),true);if e!=nil{return e};return printJSON(struct{Manifest generation;Benchmark benchmark;Note string}{m,b,"Full JSON payload parity, 500-result cap, warm sequential file-read benchmark; no cold cache or concurrent workload. Go heap/sys not peak RSS; use /usr/bin/time -l on Mac."})}
var _=io.EOF
