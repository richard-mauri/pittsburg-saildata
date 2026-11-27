package main

import (
 "bytes"
 "context"
 "encoding/binary"
 "encoding/csv"
 "encoding/hex"
 "fmt"
 "math"
 "os"
 "os/exec"
 "path/filepath"
 "strings"
 "testing"
 "time"
)
func wkb(lat,lon float64)string{
 b:=make([]byte,21);b[0]=1;binary.LittleEndian.PutUint32(b[1:5],1);binary.LittleEndian.PutUint64(b[5:13],math.Float64bits(lon));binary.LittleEndian.PutUint64(b[13:21],math.Float64bits(lat));return hex.EncodeToString(b)
}
func TestTransfer(t *testing.T){
 for _,s:=range []string{"ab","abcd"}{if _,e:=transfer(ioDiscard{},strings.NewReader(s),3,10);e==nil{t.Fatal("bad length accepted")}}
 if h,e:=transfer(ioDiscard{},strings.NewReader("abc"),3,10);e!=nil||h!="ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"{t.Fatalf("%s %v",h,e)}
}
type ioDiscard struct{}
func(ioDiscard)Write(p []byte)(int,error){return len(p),nil}
func TestNormalize(t *testing.T){
 var b bytes.Buffer;w:=csv.NewWriter(&b);_ = w.Write(strings.Split("id,name,basic_category,primary_category,hierarchy,operating_status,confidence,wkb,sources",","))
 valid:=[]string{"a","Cafe","cafe","cafe",`["restaurant","cafe"]`,"open","0.9",wkb(38.02,-121.9),`[{"dataset":"test","license":"test-license"}]`}
 _=w.Write(valid);_=w.Write(valid)
 for _,pair:=range [][2]string{{"b","permanently_closed"},{"c","temporarily_closed"}}{v:=append([]string(nil),valid...);v[0],v[5]=pair[0],pair[1];_=w.Write(v)}
 v:=append([]string(nil),valid...);v[0],v[7]="d",wkb(39,-121.9);_=w.Write(v)
 v=append([]string(nil),valid...);v[0],v[1]="e","";_=w.Write(v)
 w.Flush();rows,r,e:=normalize(&b,"delta")
 if e!=nil||len(rows)!=1||r.Features!=6||r.AcceptedCategories["coffee"]!=1||r.Rejected["duplicate"]!=1||r.Rejected["outside_sample"]!=1||r.Rejected["permanently_closed"]!=1||r.Rejected["temporarily_closed"]!=1||r.Rejected["unnamed_or_invalid_name"]!=1||r.SourceDatasets["test"]!=1{t.Fatalf("%+v %v",r,e)}
 if rows[0].ID!="poi-ovt-a"{t.Fatal(rows)}
}
func TestGeometry(t *testing.T){
 lat,lon,ok:=point(wkb(38,-122));if !ok||lat!=38||lon!=-122{t.Fatal(lat,lon,ok)}
 for _,s:=range []string{"","00",wkb(math.NaN(),0),wkb(91,0)}{if _,_,ok:=point(s);ok{t.Fatal("accepted bad geometry")}}
}
func TestDockerFixture(t *testing.T){
 if os.Getenv("POI_DUCKDB_TEST")!="1"{t.Skip("requires explicitly pulled prebuilt Docker image")}
 ctx,cancel:=context.WithTimeout(context.Background(),2*time.Minute);defer cancel()
 id,_,e:=localImage(ctx);if e!=nil{t.Fatal(e)}
 dir:=t.TempDir()
 fixture:=fmt.Sprintf("COPY (SELECT 'fixture-id' AS id, {'primary':'Example Cafe'} AS names, 'cafe' AS basic_category, {'primary':'cafe','hierarchy':['restaurant','cafe']} AS taxonomy, 'open' AS operating_status, 0.9 AS confidence, unhex('%s') AS geometry, [{'dataset':'fixture','license':'test'}] AS sources, {'xmin':-121.9,'xmax':-121.9,'ymin':38.02,'ymax':38.02} AS bbox) TO '/fixture/places.parquet' (FORMAT PARQUET);",wkb(38.02,-121.9))
 cmd:=exec.CommandContext(ctx,"docker","run","--rm","--pull=never","--network","none","--read-only","--cap-drop","ALL","--security-opt","no-new-privileges","--memory","4g","--cpus","2","--user",fmt.Sprintf("%d:%d",os.Getuid(),os.Getgid()),"--mount","type=bind,source="+dir+",target=/fixture","--entrypoint","/duckdb",id,"-batch","-bail","-c",fixture)
 if o,e:=cmd.CombinedOutput();e!=nil{t.Fatalf("%s %v",o,e)}
 var raw bytes.Buffer
 if e=extract(ctx,filepath.Join(dir,"places.parquet"),"sailinggo-ovt-ci-"+filepath.Base(dir),id,boxes["delta"],&raw);e!=nil{t.Fatal(e)}
 rows,r,e:=normalize(&raw,"delta");if e!=nil||len(rows)!=1||r.AcceptedCategories["coffee"]!=1{t.Fatalf("%+v %v",r,e)}
}
