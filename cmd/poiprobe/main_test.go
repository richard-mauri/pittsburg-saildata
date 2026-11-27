package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGridParity(t *testing.T){
	ps:=synthetic(12000)
	ps=append(ps,point{12001,2,38,-122},point{12002,1,38,-122},point{12003,2,38.05,-121.95})
	g,err:=writeGrid(filepath.Join(t.TempDir(),"grid.bin"),ps);if err!=nil{t.Fatal(err)};defer g.f.Close()
	qs:=append(queries(),box{-122,38,-122,38,2},box{-122,38,-121.95,38.05,0},box{-90,0,-89,1,0})
	want:=make([][]uint32,len(qs));for i,q:=range qs{want[i]=brute(ps,q)}
	for _,workers:=range []int{1,4}{if _,err:=bench(g,qs,want,workers);err!=nil{t.Fatal(err)}}
	if err:=g.f.Truncate(0);err==nil{t.Fatal("grid file should be read-only")}
}
func TestCSV(t *testing.T){
	header:="id,lat,lon,category,name\n"
	ps,c,err:=readCSV(strings.NewReader(header+"a,38,-122,fuel,Fuel\nb,38.1,-122.1,marina,Marina\n"))
	if err!=nil||len(ps)!=2||c["fuel"]!=1||c["marina"]!=1{t.Fatal(ps,c,err)}
	for _,s:=range []string{"a,NaN,-122,fuel,Fuel\n","a,91,-122,fuel,Fuel\n","a,38,-122,fuel,Fuel\na,38,-122,fuel,Duplicate\n","a,38,-122,,Fuel\n","a,38,-122,fuel, \n"}{
		if _,_,err:=readCSV(strings.NewReader(header+s));err==nil{t.Fatalf("accepted %q",s)}
	}
}
func TestSQLiteParity(t *testing.T){
	bin,err:=exec.LookPath("sqlite3");if err!=nil{if os.Getenv("REQUIRE_POI_SQLITE")=="1"{t.Fatal(err)};t.Skip("existing sqlite3 not available")}
	ps:=synthetic(3000);ps=append(ps,point{3001,2,38,-122},point{3002,2,38.05,-121.95})
	qs:=append(queries(),box{-122,38,-122,38,2},box{-122,38,-121.95,38.05,0})
	want:=make([][]uint32,len(qs));for i,q:=range qs{want[i]=brute(ps,q)}
	s,err:=sqliteCompare(bin,filepath.Join(t.TempDir(),"probe.sqlite"),ps,qs,want);if err!=nil||s.Bytes<=0{t.Fatal(s,err)}
}
