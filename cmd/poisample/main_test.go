package main
import("bytes";"crypto/md5";"crypto/sha256";"fmt";"strings";"testing")
func TestChecksumAndCap(t *testing.T){
	data:="fixture";m:=fmt.Sprintf("%x",md5.Sum([]byte(data)))
	s,err:=parseMD5(m+"  "+sourceFile+"\n");if err!=nil||s!=m{t.Fatal(s,err)}
	if _,err:=parseMD5(m+" other");err==nil{t.Fatal("wrong filename")}
	var b bytes.Buffer
	got,err:=copyChecked(&b,strings.NewReader(data),int64(len(data)),20,m)
	if err!=nil||got!=fmt.Sprintf("%x",sha256.Sum256([]byte(data)))||b.String()!=data{t.Fatal(got,err)}
	for _,n:=range []int64{-1,0,21,8}{if _,err:=copyChecked(&b,strings.NewReader(data),n,20,m);err==nil{t.Fatal("accepted",n)}}
	if _,err:=copyChecked(&b,strings.NewReader("changed"),7,20,m);err==nil{t.Fatal("wrong digest")}
}
func featureLine(id int,typ,coords,tags string)string{
	return fmt.Sprintf("\x1e{\"type\":\"Feature\",\"geometry\":{\"type\":%q,\"coordinates\":%s},\"properties\":{\"@type\":\"node\",\"@id\":%d,%s}}\n",typ,coords,id,tags)
}
func TestNormalize(t *testing.T){
	f:=featureLine(1,"Point","[-121.9,38.02]","\"amenity\":\"fuel\",\"name\":\"Fuel\"")
	data:=f+f+featureLine(2,"Point","[-121.84,38.06]","\"leisure\":\"marina\",\"name\":\"Marina\"")+featureLine(3,"Polygon","[]","\"amenity\":\"parking\",\"name\":\"Parking\"")+featureLine(4,"Point","[-121.8,38.02]","\"amenity\":\"restaurant\",\"name\":\"Outside\"")+featureLine(5,"Point","[-121.9,38.02]","\"amenity\":\"cafe\"")+featureLine(6,"Point","[-121.9,38.02]","\"amenity\":\"fuel\",\"waterway\":\"fuel\",\"name\":\"Dock\"")+featureLine(7,"Point","[-121.9,38.02]","\"amenity\":\"fuel\",\"disused\":\"yes\",\"name\":\"Closed\"")
	rows,r,err:=normalize(strings.NewReader(data),"delta")
	if err!=nil||len(rows)!=3||r.RawCategories["fuel"]!=3||r.Rejected["duplicate"]!=1||r.Rejected["non_point_geometry"]!=1||r.Rejected["outside_sample"]!=1||r.Rejected["unnamed_or_invalid_name"]!=1||r.Rejected["closed_or_disused"]!=1||r.AcceptedCategories["marine_fuel"]!=1{t.Fatal(rows,r,err)}
	if rows[0].ID!="poi-osm-n-1"||rows[1].ID!="poi-osm-n-2"{t.Fatal(rows)}
	if _,_,err:=normalize(strings.NewReader(f+featureLine(1,"Point","[-121.9,38.02]","\"amenity\":\"fuel\",\"name\":\"Changed\"")),"delta");err==nil{t.Fatal("conflicting ID")}
	if _,_,err:=normalize(strings.NewReader("not JSON"),"delta");err==nil{t.Fatal("invalid JSON")}
	if validName(strings.Repeat("😀",41))||validName("a\nb"){t.Fatal("invalid text accepted")}
}
