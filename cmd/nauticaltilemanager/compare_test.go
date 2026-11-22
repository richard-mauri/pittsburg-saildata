package main

import (
 "bytes"
 "context"
 "image"
 "image/color"
 "image/png"
 "net/http"
 "net/http/httptest"
 "os"
 "path/filepath"
 "strings"
 "sync/atomic"
 "testing"
 "time"
)

func TestComparisonCreatesOnlyIsolatedPairedSamples(t *testing.T){
 var count atomic.Int32
 upstream:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  count.Add(1)
  q:=r.URL.Query()
  if q.Get("REQUEST")!="GetMap"||q.Get("CRS")!="EPSG:3857"||
   q.Get("WIDTH")!="256"||q.Get("HEIGHT")!="256"||q.Get("BBOX")==""{
   t.Errorf("bad WMS request: %s",r.URL.RawQuery)
  }
  layers:=q.Get("LAYERS")
  if layers!="0,1,2,3,4,5,6,7"&&layers!="0,1,2,3,4,5,6,7,8,9,10,11,12"{
   t.Errorf("unexpected layer selection %q",layers)
  }
  img:=image.NewRGBA(image.Rect(0,0,256,256))
  if strings.HasSuffix(layers,",12"){img.Set(0,0,color.RGBA{R:255,A:255})}else{img.Set(0,0,color.RGBA{B:255,A:255})}
  w.Header().Set("Content-Type","image/png")
  if err:=png.Encode(w,img);err!=nil{t.Error(err)}
 }))
 defer upstream.Close()
 output:=filepath.Join(t.TempDir(),"wms-compare")
 if err:=compareNOAALayers(context.Background(),output,upstream.URL,upstream.Client(),time.Second,0);err!=nil{t.Fatal(err)}
 if count.Load()!=8{t.Fatalf("wanted 8 exact WMS requests, got %d",count.Load())}
 ents,err:=os.ReadDir(output);if err!=nil{t.Fatal(err)}
 if len(ents)!=8{t.Fatalf("expected 8 saved PNGs, got %d",len(ents))}
 for _,place:=range []string{"pittsburg","suisun-bay"}{
  for _,z:=range []string{"z13","z14"}{
   left:=filepath.Join(output,place+"-"+z+"-all-0-12.png")
   right:=filepath.Join(output,place+"-"+z+"-visible-0-7.png")
   a,err:=os.ReadFile(left);if err!=nil{t.Fatal(err)}
   b,err:=os.ReadFile(right);if err!=nil{t.Fatal(err)}
   if !validPNG(a)||!validPNG(b)||bytes.Equal(a,b){
    t.Fatalf("WMS layer images not individually requested for %s %s",place,z)
   }
  }
 }
 if err:=compareNOAALayers(context.Background(),output,upstream.URL,upstream.Client(),time.Second,0);err==nil{
  t.Fatal("existing comparison directory should be refused to avoid overwrites")
 }
 if count.Load()!=8{t.Fatal("existing directory rejection contacted NOAA")}
}
func TestComparisonRejectsMissingDirAndInvalidPNG(t *testing.T){
 var calls atomic.Int32
 upstream:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  calls.Add(1);w.Header().Set("Content-Type","image/png");_,_=w.Write([]byte("not a png"))
 }))
 defer upstream.Close()
 if err:=compareNOAALayers(context.Background(),"",upstream.URL,upstream.Client(),time.Second,0);err==nil{
  t.Fatal("missing comparison directory accepted")
 }
 if calls.Load()!=0{t.Fatal("missing dir contacted upstream")}
 dir:=filepath.Join(t.TempDir(),"invalid")
 if err:=compareNOAALayers(context.Background(),dir,upstream.URL,upstream.Client(),time.Second,0);err==nil{
  t.Fatal("bad PNG accepted")
 }
 if calls.Load()!=1{t.Fatalf("invalid PNG should fail fast, calls=%d",calls.Load())}
 entries,err:=os.ReadDir(dir);if err!=nil{t.Fatal(err)}
 if len(entries)!=0{t.Fatal("invalid PNG written to disk")}
}

func TestComparisonRefusesManagedDirectoryDescendant(t *testing.T){
 root:=filepath.Join(t.TempDir(),"managed")
 if err:=os.Mkdir(root,0700);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(filepath.Join(root,markerFile),[]byte("SailingGo-managed-XYZ-v1\nsource=noaa\n"),0600);err!=nil{t.Fatal(err)}
 child:=filepath.Join(root,"compare","new-run")
 if err:=compareNOAALayers(context.Background(),child,"http://127.0.0.1:1",nil,time.Second,0);err==nil{
  t.Fatal("comparison was allowed inside managed tile cache")
 }
 if _,err:=os.Stat(filepath.Join(root,"compare"));!os.IsNotExist(err){t.Fatal("comparison created a directory inside the managed cache")}
}
