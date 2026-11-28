package poigrid

import(
 "context"
 "crypto/sha256"
 "encoding/binary"
 "encoding/hex"
 "encoding/json"
 "errors"
 "os"
 "path/filepath"
 "strings"
 "sync"
 "testing"
)
func put(t *testing.T,dir,name string,b []byte)(string,int64){t.Helper();if e:=os.WriteFile(filepath.Join(dir,name),b,0600);e!=nil{t.Fatal(e)};h:=sha256.Sum256(b);return hex.EncodeToString(h[:]),int64(len(b))}
func fixture(t *testing.T)(string,Manifest){
 t.Helper();dir:=t.TempDir();var payload,prov,index strings.Builder
 // IDs are intentionally opposite to cell order.
 for j,id:=range []string{"poi-ovt-z","poi-ovt-a","poi-ovt-m"}{
  lon:=-122.09+float64(j)*.05
  w:=Waypoint{id,37.2,lon,"Cafe","coffee",baseNote,Created};raw,_:=json.Marshal(w)
  v:=entry{w.Lat,w.Lon,uint64(payload.Len()),uint32(len(raw)),0}
  if e:=binary.Write(&index,binary.LittleEndian,v);e!=nil{t.Fatal(e)};payload.Write(raw)
  p,_:=json.Marshal(provenance{id,json.RawMessage(`[{"dataset":"Overture","license":"CDLA-Permissive-2.0"}]`),.8,"open","cafe","cafe"});prov.Write(p);prov.WriteByte('\n')
 }
 const prefix="https://overturemaps-us-west-2.s3.us-west-2.amazonaws.com/release/"+Release+"/theme=places/type=place/"
 sha:=strings.Repeat("a",64)
 m:=Manifest{Schema:1,Release:Release,Mapping:Mapping,Created:Created,Rings:1,Edges:4,Stats:Stats{Rows:3,Features:3,Bands:1,Categories:map[string]int{"coffee":3},Licenses:map[string]int{"CDLA-Permissive-2.0":3}},Sources:[]Source{{Release:Release,URL:prefix+"part-00002-5783d80c-d44a-5845-9057-3a39013540b0-c000.zstd.parquet",SHA256:sha,Bytes:836166912},{Release:Release,URL:prefix+"part-00001-43e455e5-5fd3-50ee-8b56-2de40da4223e-c000.zstd.parquet",SHA256:sha,Bytes:823024945},{Release:"TIGER2026",URL:"https://www2.census.gov/geo/tiger/TIGER2026/STATE/tl_2026_us_state.zip",SHA256:sha,Bytes:1}}}
 m.PayloadSHA256,m.PayloadBytes=put(t,dir,"payload.bin",[]byte(payload.String()))
 m.IndexSHA256,m.IndexBytes=put(t,dir,"index.bin",[]byte(index.String()))
 m.ProvenanceSHA256,m.ProvenanceBytes=put(t,dir,"provenance.jsonl",[]byte(prov.String()))
 manifest(t,dir,m);return dir,m
}
func manifest(t *testing.T,d string,m Manifest){raw,e:=json.Marshal(m);if e!=nil{t.Fatal(e)};put(t,d,"manifest.json",raw)}
func TestReader(t *testing.T){
 dir,_:=fixture(t);r,_,ev,e:=Open(dir);if e!=nil{t.Fatal(e)};defer r.Close();if ev.ProvenanceRows!=3{t.Fatal(ev)}
 q:=Query{-122.2,37.1,-121.9,37.3,[]string{"coffee"},2,MaxArrayBytes}
 a,e:=r.Search(context.Background(),q);if e!=nil||len(a.Records)!=2||!a.Truncated||!strings.Contains(string(a.Records[0]),"poi-ovt-a")||!strings.Contains(string(a.Records[1]),"poi-ovt-m"){t.Fatal(a,e)}
 raw,_:=json.Marshal(a.Records);if len(raw)!=a.ArrayBytes{t.Fatal("byte accounting")}
 q.ByteLimit=2;a,e=r.Search(context.Background(),q);if e!=nil||len(a.Records)!=0||!a.Truncated||a.ArrayBytes!=2{t.Fatal(a,e)}
 q.ByteLimit=MaxArrayBytes;ctx,cancel:=context.WithCancel(context.Background());cancel();if _,e=r.Search(ctx,q);!errors.Is(e,context.Canceled){t.Fatal(e)}
 for i:=0;i<4;i++{r.slots<-struct{}{}}
 if _,e=r.Search(context.Background(),q);!errors.Is(e,ErrBusy){t.Fatal(e)}
 for i:=0;i<4;i++{<-r.slots}
 var wg sync.WaitGroup;for i:=0;i<4;i++{wg.Add(1);go func(){defer wg.Done();for j:=0;j<20;j++{a,e:=r.Search(context.Background(),q);if e!=nil||len(a.Records)!=2{t.Errorf("parallel %v %v",a,e)}}}()};wg.Wait()
 q.Categories=[]string{"coffee","coffee"};if _,e=r.Search(context.Background(),q);!errors.Is(e,ErrQuery){t.Fatal(e)}
 q.Categories=[]string{"coffee"};q.West=-180;if _,e=r.Search(context.Background(),q);!errors.Is(e,ErrQuery){t.Fatal(e)}
 q.West=-122.2;if e=r.Close();e!=nil{t.Fatal(e)};if _,e=r.Search(context.Background(),q);!errors.Is(e,ErrClosed){t.Fatal(e)}
}
func TestCorruption(t *testing.T){
 for _,kind:=range []string{"hash","index_bounds","provenance_id","provenance_license","missing_provenance","duplicate_json_key","license_count"}{t.Run(kind,func(t *testing.T){
  dir,m:=fixture(t);read:=func(n string)[]byte{p,e:=os.ReadFile(filepath.Join(dir,n));if e!=nil{t.Fatal(e)};return p}
  switch kind{
  case "hash":p:=read("payload.bin");p[0]='[';put(t,dir,"payload.bin",p)
  case "index_bounds":p:=read("index.bin");binary.LittleEndian.PutUint64(p[16:24],^uint64(0));m.IndexSHA256,m.IndexBytes=put(t,dir,"index.bin",p)
  case "provenance_id":p:=strings.Replace(string(read("provenance.jsonl")),"poi-ovt-z","poi-ovt-x",1);m.ProvenanceSHA256,m.ProvenanceBytes=put(t,dir,"provenance.jsonl",[]byte(p))
  case "provenance_license":p:=strings.Replace(string(read("provenance.jsonl")),"CDLA-Permissive-2.0","unknown",1);m.ProvenanceSHA256,m.ProvenanceBytes=put(t,dir,"provenance.jsonl",[]byte(p))
  case "missing_provenance":p:=read("provenance.jsonl");m.ProvenanceSHA256,m.ProvenanceBytes=put(t,dir,"provenance.jsonl",p[strings.IndexByte(string(p),'\n')+1:])
  case "duplicate_json_key":p:=strings.Replace(string(read("payload.bin")),`"name":"Cafe"`,`"id":"Cafe"`,1);m.PayloadSHA256,m.PayloadBytes=put(t,dir,"payload.bin",[]byte(p))
  case "license_count":m.Stats.Licenses["CDLA-Permissive-2.0"]=2
  };manifest(t,dir,m);r,_,_,e:=Open(dir);if e==nil{r.Close();t.Fatal("accepted corruption")}
 })}
}
