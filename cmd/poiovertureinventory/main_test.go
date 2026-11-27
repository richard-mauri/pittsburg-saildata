package main

import (
	"context"
	"testing"
)

func TestScan(t *testing.T){
	p:="https://stac.overturemaps.org/"+release+"/places/"
	a:="s3://overturemaps-us-west-2/release/"+release+"/theme=places/type=place/a.parquet"
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
