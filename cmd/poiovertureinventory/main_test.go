package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestScan(t *testing.T){
	p:="https://stac.overturemaps.org/"+release+"/places/"
	a:="https://overturemaps-us-west-2.s3.us-west-2.amazonaws.com/release/"+release+"/theme=places/type=place/a.parquet"
	nodes:=map[string]node{
		root:{Type:"Catalog",Links:[]link{{"child","places/catalog.json"},{"child","buildings/catalog.json"}}},
		p+"catalog.json":{Type:"Catalog",Links:[]link{{"child","place/collection.json"}}},
		p+"place/collection.json":{Type:"Collection",Links:[]link{{"item","a.json"},{"item","b.json"}}},
		p+"place/a.json":{Type:"Feature",ID:"a",Collection:"place",BBox:[]float64{-123,37,-121,39},Assets:map[string]asset{"aws":{a,123}}},
		p+"place/b.json":{Type:"Feature",ID:"b",Collection:"place",BBox:[]float64{0,0,1,1}},
	}
	got,e:=scan(context.Background(),func(_ context.Context,u string)(node,error){return nodes[u],nil})
	if e!=nil||got.ItemsInspected!=2||got.FullFileBytes!=123||len(got.Candidates)!=1||len(got.Candidates[0].Samples)!=2{t.Fatalf("%+v %v",got,e)}
}
func TestLink(t *testing.T){
	for _,u:=range []string{"https://example.com/a.json","../catalog.json","https://stac.overturemaps.org/other/catalog.json","places/a.json?x=1"}{
		if _,e:=catalogURL(root,u);e==nil{t.Fatalf("accepted %s",u)}
	}
}

func TestReadMetadata(t *testing.T) {
	for _, tc := range []struct {
		name, body, encoding string
		status int
		length, remaining int64
		wantBytes int
		wantError bool
	}{
		{"streamed", "{}", "", 200, -1, 10, 2, false},
		{"known", "{}", "", 200, 2, 2, 2, false},
		{"identity", "{}", "identity", 200, -1, 10, 2, false},
		{"stream exact cap", "{}", "", 200, -1, 2, 2, true},
		{"stream exceeds cap", "abcdef", "", 200, -1, 3, 3, true},
		{"declared exceeds cap", "abcdef", "", 200, 6, 3, 0, true},
		{"truncated", "{}", "", 200, 3, 10, 2, true},
		{"gzip", "{}", "gzip", 200, -1, 10, 0, true},
		{"http error", "{}", "", 503, -1, 10, 0, true},
		{"budget spent", "{}", "", 200, -1, 0, 0, true},
	}{
		t.Run(tc.name,func(t *testing.T){
			body := strings.NewReader(tc.body)
			resp := &http.Response{StatusCode:tc.status,ContentLength:tc.length,Header:make(http.Header),Body:io.NopCloser(body)}
			resp.Header.Set("Content-Encoding",tc.encoding)
			raw,err:=readMetadata(resp,tc.remaining)
			if (err!=nil)!=tc.wantError || len(raw)!=tc.wantBytes || len(tc.body)-body.Len()!=tc.wantBytes {t.Fatalf("got %d bytes, error %v, remaining source %d",len(raw),err,body.Len())}
		})
	}
}

func TestCaliforniaInventory(t *testing.T){
 p:="https://stac.overturemaps.org/"+release+"/places/"
 prefix:="s3://overturemaps-us-west-2/release/"+release+"/theme=places/type=place/"
 nodes:=map[string]node{
  root:{Type:"Catalog",Links:[]link{{"child","places/catalog.json"}}},
  p+"catalog.json":{Type:"Collection",Links:[]link{{"item","north.json"},{"item","south.json"},{"item","other.json"}}},
  p+"north.json":{Type:"Feature",ID:"north",Collection:"place",BBox:[]float64{-125,34,-110,45},Assets:map[string]asset{"aws":{prefix+"north.parquet",123}}},
  p+"south.json":{Type:"Feature",ID:"south",Collection:"place",BBox:[]float64{-120,30,-110,34},Assets:map[string]asset{"aws":{prefix+"south.parquet",456}}},
  p+"other.json":{Type:"Feature",ID:"other",Collection:"place",BBox:[]float64{-100,30,-90,40}},
 }
 get:=func(_ context.Context,u string)(node,error){return nodes[u],nil}
 pilot,e:=scanRegion(context.Background(),get,false);if e!=nil||pilot.FullFileBytes!=123||len(pilot.Candidates)!=1{t.Fatalf("%+v %v",pilot,e)}
 ca,e:=scanRegion(context.Background(),get,true);if e!=nil||ca.FullFileBytes!=579||len(ca.Candidates)!=2||ca.ItemsInspected!=3{t.Fatalf("%+v %v",ca,e)}
 if len(ca.Candidates[1].Samples)!=1||ca.Candidates[1].Samples[0]!="california_envelope"{t.Fatal(ca)}
}
