package main

import(
 "bytes"
 "encoding/binary"
 "encoding/csv"
 "encoding/hex"
 "math"
 "os"
 "path/filepath"
 "testing"
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
 g,e:=openGrid(dir,generation{Stats:b.stats,PayloadBytes:int64(p.Len())});if e!=nil{t.Fatal(e)};defer g.close()
 q:=query{-123,37,-121,38,-1};a,tr,e:=g.search(q,false);z,ts,ee:=g.search(q,true);if e!=nil||ee!=nil||tr||ts||len(a)!=2||len(z)!=2||!bytes.Equal(a[0],z[0])||!bytes.Equal(a[1],z[1]){t.Fatal("parity",e,ee)}
}
