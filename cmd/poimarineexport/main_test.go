package main

import (
	"encoding/json"
	"strings"
	"testing"
)

const sample = `{"version":"14","generator_version":"v32","updated":"2026-09-25","region":"test","places":[{"name":"A","category":"marinas","lat":38,"lon":-122,"source":"OpenStreetMap","source_id":"osm:n/1","coord_source":"osm_geometry","coord_status":"source_provided"},{"name":"Antioch Marina Launch Ramp","category":"launch_ramps","lat":38.01,"lon":-121.8,"source":"local override","coord_source":"local_override","coord_status":"explicit_override"},{"name":"Ignored","category":"boatyards"}]}`

func TestAdapt(t *testing.T){
	r,rows,e:=adapt([]byte(sample))
	if e!=nil||r.Accepted!=2||r.Skipped!=1||r.Counts["marina"]!=1||r.Counts["boat_ramp"]!=1{t.Fatalf("%+v %v",r,e)}
	_,again,e:=adapt([]byte(sample))
	if e!=nil||again[0].Waypoint.ID!=rows[0].Waypoint.ID{t.Fatal("unstable ID")}
	for _,row:=range rows{
		b,_:=json.Marshal(row.Waypoint)
		var fields map[string]any
		if json.Unmarshal(b,&fields)!=nil||len(fields)!=7||len(row.Waypoint.ID)>80||row.Waypoint.Category!="marina"{t.Fatalf("bad waypoint %s",b)}
	}
}

func TestReject(t *testing.T){
	for _,s:=range []string{
		strings.Replace(sample,`"lat":38,`, "",1),
		strings.Replace(sample,`"lat":38`,`"lat":91`,1),
		strings.Replace(sample,`"source_id":"osm:n/1",`,"",1),
		strings.Replace(sample,`"name":"A"`,`"name":"`+strings.Repeat("😀",41)+`"`,1),
		strings.Replace(sample,`"name":"Antioch Marina Launch Ramp"`,`"name":"Unknown override"`,1),
	}{
		if _,_,e:=adapt([]byte(s));e==nil{t.Fatal("accepted invalid scoped row")}
	}
}
