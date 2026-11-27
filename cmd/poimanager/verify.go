package main

import(
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "math"
 "path/filepath"
 "runtime"
 "sort"
 "sync"
 "time"

 "pittsburg-saildata/internal/poigrid"
)
type benchmark struct{
 Evidence poigrid.Evidence
 Queries,ConcurrentQueries,Truncated int
 GridMS,ScanMS,ConcurrentWallMS,GridP95MS,ConcurrentP95MS float64
 PayloadReturnedBytes int64
 GoHeapBytes,GoSysBytes uint64
 Parity string
}
func same(a,b poigrid.Result)bool{if a.Truncated!=b.Truncated||a.ArrayBytes!=b.ArrayBytes||len(a.Records)!=len(b.Records){return false};for i:=range a.Records{if !bytes.Equal(a.Records[i],b.Records[i]){return false}};return true}
func p95(v []float64)float64{sort.Float64s(v);return v[int(math.Ceil(.95*float64(len(v))))-1]}
func checkGeneration(dir string,measure bool)(generation,benchmark,error){
 var m generation;var b benchmark
 r,_,ev,e:=poigrid.Open(dir);if e!=nil{return m,b,e};defer r.Close()
 if e=readJSON(filepath.Join(dir,"manifest.json"),1<<20,&m);e!=nil{return m,b,e}
 if !measure{return m,b,nil}
 runtime.GC();var mem runtime.MemStats;runtime.ReadMemStats(&mem)
 b=benchmark{Evidence:ev,GoHeapBytes:mem.HeapAlloc,GoSysBytes:mem.Sys,Parity:"pass"}
 q:=func(w,s,e,n float64,c ...string)poigrid.Query{return poigrid.Query{West:w,South:s,East:e,North:n,Categories:c,Limit:500,ByteLimit:poigrid.MaxArrayBytes}}
 qs:=[]poigrid.Query{
 q(-122.43,37.77,-122.39,37.81,"restaurant","coffee","grocery","parking"),
 q(-118.30,34.02,-118.20,34.10,"restaurant","coffee","grocery","parking"),
 q(-121.94,38,-121.84,38.06,"restaurant","coffee","grocery","fuel"),
 q(-124.2,41.7,-123.9,42.05,"fuel","lodging","restaurant"),
 q(-114.8,32.6,-114.4,32.9,"fuel","lodging"),
 q(-122.43,37.77,-122.39,37.81,"fuel"),
 q(-118.30,34.02,-118.20,34.10,"coffee"),
 q(-120.58,38.16,-120.49,38.24,"fuel","grocery"),
 q(-120.08,38.93,-119.94,39.02,"restaurant","lodging"),
 q(-117.23,32.66,-117.10,32.76,"parking","ev_charging"),
 q(-121.6,36.55,-121.4,36.75,"restaurant","lodging"),
 q(-122.45,37.75,-122.35,37.85,"restaurant"),
 q(-118.5,34,-118.1,34.3,"restaurant","coffee"),
 q(-122.5,37.7,-122.1,38,"grocery","fuel"),
 q(-120,36,-119.7,36.3,"ev_charging"),
 q(-110,35,-109.9,35.1,"fuel"),
 }
 // Explicit small byte budget exercises whole-record truncation on dense data.
 limited:=qs[0];limited.ByteLimit=1024;qs=append(qs,limited)
 ctx:=context.Background();expected:=make([]poigrid.Result,len(qs));var timings []float64
 for i,q:=range qs{
  start:=time.Now();a,e:=r.Search(ctx,q);ms:=float64(time.Since(start))/float64(time.Millisecond);if e!=nil{return m,b,e};b.GridMS+=ms;timings=append(timings,ms)
  start=time.Now();z,e:=r.ScanForVerification(ctx,q);b.ScanMS+=float64(time.Since(start))/float64(time.Millisecond);if e!=nil{return m,b,e}
  if !same(a,z){return m,b,errors.New("indexed/scan parity failure")};raw,e:=json.Marshal(a.Records);if e!=nil||len(raw)!=a.ArrayBytes||len(raw)>q.ByteLimit{return m,b,errors.New("encoded byte bound failure")}
  expected[i]=a;b.PayloadReturnedBytes+=int64(a.ArrayBytes);if a.Truncated{b.Truncated++};b.Queries++
 }
 b.GridP95MS=p95(timings)
 // Four workers each execute the same query set; no unbounded work queue.
 type workerResult struct{times []float64;err error}
 results:=make(chan workerResult,4);var wg sync.WaitGroup
 start:=time.Now()
 for j:=0;j<4;j++{wg.Add(1);go func(){defer wg.Done();v:=workerResult{};for i,q:=range qs{t:=time.Now();a,e:=r.Search(ctx,q);v.times=append(v.times,float64(time.Since(t))/float64(time.Millisecond));if e!=nil{v.err=e;break};if !same(a,expected[i]){v.err=errors.New("concurrent parity failure");break}};results<-v}()}
 wg.Wait();close(results);b.ConcurrentWallMS=float64(time.Since(start))/float64(time.Millisecond)
 timings=nil;for v:=range results{if v.err!=nil{return m,b,v.err};timings=append(timings,v.times...)}
 b.ConcurrentQueries=len(timings);b.ConcurrentP95MS=p95(timings)
 return m,b,nil
}
func verify()error{if e:=realDir("private");e!=nil{return e};if e:=realDir(study);e!=nil{return e};m,b,e:=checkGeneration(filepath.Join(study,"generation"),true);if e!=nil{return e};return printJSON(struct{Manifest generation;Benchmark benchmark;Note string}{m,b,"Read-only full provenance reconciliation; deterministic stable-ID JSON array parity, 500 records / 496 KiB array cap; 4-worker warm query parity. No cold-cache/HTTP/runtime RSS or publication approval."})}
