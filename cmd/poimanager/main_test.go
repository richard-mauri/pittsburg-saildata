package main

import(
 "archive/zip"
 "bytes"
 "context"
 "fmt"
 "os/exec"
 "time"
 "encoding/binary"
 "encoding/csv"
 "encoding/hex"
 "math"
 "os"
 "path/filepath"
 "testing"
 "pittsburg-saildata/internal/poigrid"
)
func square(w,s,e,n float64)[]xy{return []xy{{w,s},{e,s},{e,n},{w,n},{w,s}}}
func TestBoundary(t *testing.T){b,e:=newBoundary([][]xy{square(-123,36,-120,39),square(-122,37,-121,38),square(-119,34,-118,35)});if e!=nil{t.Fatal(e)}
 for _,v:=range []struct{x,y float64;inside,near bool}{{-122.5,37,true,false},{-121.5,37.5,false,false},{-118.5,34.5,true,false},{-117,34,false,false},{-123,37,true,true}}{i,n:=b.contains(v.x,v.y);if i!=v.inside||n!=v.near{t.Fatalf("%+v got %v %v",v,i,n)}}}
func wkb(x,y float64)string{b:=make([]byte,21);b[0]=1;binary.LittleEndian.PutUint32(b[1:],1);binary.LittleEndian.PutUint64(b[5:],math.Float64bits(x));binary.LittleEndian.PutUint64(b[13:],math.Float64bits(y));return hex.EncodeToString(b)}
func TestBuildAndGrid(t *testing.T){
 boundary,e:=newBoundary([][]xy{square(-123,36,-120,39)});if e!=nil{t.Fatal(e)}
 var csvbuf,p,v bytes.Buffer;w:=csv.NewWriter(&csvbuf);_=w.Write([]string{"id","name","basic_category","primary_category","hierarchy","operating_status","confidence","wkb","sources"})
 for _,r:=range [][]string{{"a","Cafe","cafe","coffee_shop","NULL","open","0.8",wkb(-122,37.2),`[{"dataset":"Overture","license":"CDLA-Permissive-2.0"}]`},{"b","Eatery","casual_eatery","casual_eatery","NULL","NULL","0.5",wkb(-122,37.3),`[{"dataset":"Overture","license":"CDLA-Permissive-2.0"}]`},{"c","Cafe","cafe","internet_cafe","NULL","open","0.9",wkb(-122,37.2),"NULL"},{"d","Cafe","cafe","cafe","NULL","open","0.1",wkb(-122,37.2),"NULL"},{"e","Cafe","cafe","cafe","NULL","open","0.9",wkb(-122,37.2),"NULL"}}{_=w.Write(r)};w.Flush()
 b:=newBuilder(boundary,&p,&v);if e=b.consume(&csvbuf,37,38);e!=nil{t.Fatal(e)};_=b.payload.Flush();_=b.prov.Flush()
 if b.stats.Rows!=2||b.stats.Rejected["taxonomy"]!=1||b.stats.Rejected["confidence"]!=1||b.stats.Rejected["missing_or_unknown_license"]!=1{t.Fatalf("%+v",b.stats)}
 dir:=t.TempDir();if e=os.WriteFile(filepath.Join(dir,"payload.bin"),p.Bytes(),0600);e!=nil{t.Fatal(e)};if e=writeIndex(filepath.Join(dir,"index.bin"),b.entries);e!=nil{t.Fatal(e)}
 if e=os.WriteFile(filepath.Join(dir,"provenance.jsonl"),v.Bytes(),0600);e!=nil{t.Fatal(e)}
 m:=generation{Schema:1,Release:release,Mapping:mapping,Created:created,Rings:1,Edges:4,Stats:b.stats,Sources:[]source{{Release:release,URL:northURL,SHA256:northHash,Bytes:northBytes},{Release:release,URL:southURL,SHA256:northHash,Bytes:southBytes},{Release:"TIGER2026",URL:boundaryURL,SHA256:northHash,Bytes:1}}}
 m.PayloadSHA256,m.PayloadBytes,e=digest(filepath.Join(dir,"payload.bin"),fileCap);if e!=nil{t.Fatal(e)}
 m.IndexSHA256,m.IndexBytes,e=digest(filepath.Join(dir,"index.bin"),fileCap);if e!=nil{t.Fatal(e)}
 m.ProvenanceSHA256,m.ProvenanceBytes,e=digest(filepath.Join(dir,"provenance.jsonl"),fileCap);if e!=nil{t.Fatal(e)}
 if e=saveJSON(filepath.Join(dir,"manifest.json"),m);e!=nil{t.Fatal(e)}
 g,_,ev,e:=poigrid.Open(dir);if e!=nil{t.Fatal(e)};defer g.Close()
 q:=poigrid.Query{West:-122.1,South:37.1,East:-121.9,North:37.4,Categories:[]string{"coffee","restaurant"},Limit:500,ByteLimit:poigrid.MaxArrayBytes}
 a,e:=g.Search(context.Background(),q);z,ee:=g.ScanForVerification(context.Background(),q)
 if e!=nil||ee!=nil||a.Truncated||z.Truncated||len(a.Records)!=2||ev.ProvenanceRows!=2||!same(a,z){t.Fatal("parity",e,ee)}
}

func TestBoundaryArchive(t *testing.T){
 // Two DBF records, California is second; two Polygon SHP records.
 dbf:=make([]byte,65+6);dbf[0]=3;binary.LittleEndian.PutUint32(dbf[4:8],2);binary.LittleEndian.PutUint16(dbf[8:10],65);binary.LittleEndian.PutUint16(dbf[10:12],3);copy(dbf[32:43],"STATEFP");dbf[43]='C';dbf[48]=2;dbf[64]=13;copy(dbf[65:]," 32 06")
 polygon:=func(rings [][]xy)[]byte{
  count:=0;for _,r:=range rings{count+=len(r)}
  p:=make([]byte,44+4*len(rings)+16*count);binary.LittleEndian.PutUint32(p[:4],5);binary.LittleEndian.PutUint32(p[36:40],uint32(len(rings)));binary.LittleEndian.PutUint32(p[40:44],uint32(count))
  idx:=0;for j,r:=range rings{binary.LittleEndian.PutUint32(p[44+4*j:],uint32(idx));for _,v:=range r{k:=44+4*len(rings)+16*idx;binary.LittleEndian.PutUint64(p[k:],math.Float64bits(v.X));binary.LittleEndian.PutUint64(p[k+8:],math.Float64bits(v.Y));idx++}}
  return p
 }
 a:=polygon([][]xy{square(-119,36,-115,39)});b:=polygon([][]xy{square(-123,36,-120,39),square(-122,37,-121,38)})
 shp:=make([]byte,100+8+len(a)+8+len(b));binary.BigEndian.PutUint32(shp[:4],9994);binary.BigEndian.PutUint32(shp[24:28],uint32(len(shp)/2));binary.LittleEndian.PutUint32(shp[28:32],1000);binary.LittleEndian.PutUint32(shp[32:36],5)
 off:=100;for j,p:=range [][]byte{a,b}{binary.BigEndian.PutUint32(shp[off:],uint32(j+1));binary.BigEndian.PutUint32(shp[off+4:],uint32(len(p)/2));copy(shp[off+8:],p);off+=8+len(p)}
 path:=filepath.Join(t.TempDir(),"state.zip");f,e:=os.Create(path);if e!=nil{t.Fatal(e)};z:=zip.NewWriter(f)
 for n,p:=range map[string][]byte{"tl_2026_us_state.shp":shp,"tl_2026_us_state.dbf":dbf,"tl_2026_us_state.prj":[]byte(`GEOGCS["GCS_North_American_1983",DATUM["D_North_American_1983",SPHEROID["GRS_1980",6378137,298.257222101]],PRIMEM["Greenwich",0],UNIT["Degree",0.0174532925199433]]`)}{w,e:=z.Create(n);if e!=nil{t.Fatal(e)};if _,e=w.Write(p);e!=nil{t.Fatal(e)}}
 if e=z.Close();e!=nil{t.Fatal(e)};if e=f.Close();e!=nil{t.Fatal(e)}
 state,e:=loadBoundary(path);if e!=nil{t.Fatal(e)}
 if in,_:=state.contains(-122.5,37.5);!in{t.Fatal("California missing")};if in,_:=state.contains(-121.5,37.5);in{t.Fatal("hole included")};if in,_:=state.contains(-117,37.5);in{t.Fatal("other state included")}
}
func TestManagerDockerFixture(t *testing.T){
 if os.Getenv("POI_DUCKDB_TEST")!="1"{t.Skip("requires explicitly pulled image")}
 ctx,cancel:=context.WithTimeout(context.Background(),2*time.Minute);defer cancel()
 id,_,e:=localImage(ctx);if e!=nil{t.Fatal(e)}
 dir:=t.TempDir()
 fixture:=fmt.Sprintf("COPY (SELECT 'fixture-id' AS id, {'primary':'Example Eatery'} AS names, 'casual_eatery' AS basic_category, {'primary':'casual_eatery','hierarchy':['food_and_drink','casual_eatery']} AS taxonomy, 'open' AS operating_status, 0.9 AS confidence, unhex('%s') AS geometry, [{'dataset':'fixture','license':'CDLA-Permissive-2.0'}] AS sources, {'xmin':-121.9,'xmax':-121.9,'ymin':38.02,'ymax':38.02} AS bbox) TO '/fixture/places.parquet' (FORMAT PARQUET);",wkb(-121.9,38.02))
 cmd:=exec.CommandContext(ctx,"docker","run","--rm","--pull=never","--network","none","--read-only","--cap-drop","ALL","--security-opt","no-new-privileges","--memory","4g","--cpus","2","--user",fmt.Sprintf("%d:%d",os.Getuid(),os.Getgid()),"--mount","type=bind,source="+dir+",target=/fixture","--entrypoint","/duckdb",id,"-batch","-bail","-c",fixture)
 if o,e:=cmd.CombinedOutput();e!=nil{t.Fatalf("%s %v",o,e)}
 var raw,p,v bytes.Buffer
 if e=extract(ctx,filepath.Join(dir,"places.parquet"),"sailinggo-ca-ci-"+filepath.Base(dir),id,38,39,&raw);e!=nil{t.Fatal(e)}
 state,e:=newBoundary([][]xy{square(-123,37,-120,40)});if e!=nil{t.Fatal(e)}
 b:=newBuilder(state,&p,&v);if e=b.consume(&raw,38,39);e!=nil{t.Fatal(e)};if b.stats.Rows!=1||b.stats.Categories["restaurant"]!=1{t.Fatalf("%+v",b.stats)}
}
