package main

import (
 "context"
 "image"
 "image/color"
 "image/png"
 "net/http"
 "net/http/httptest"
 "os"
 "path/filepath"
 "strconv"
 "strings"
 "sync"
 "testing"
 "time"
)

func TestZoomSamplesUseCleanLayersAndAllZoomsWithoutManagedWrites(t *testing.T){
 type reqInfo struct {z int; bbox string}
 var mu sync.Mutex
 seen:=map[string]reqInfo{}
 upstream:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  q:=r.URL.Query()
  if q.Get("LAYERS")!="0,1,2,3,4,5,6,7"||q.Get("SERVICE")!="WMS"||
   q.Get("REQUEST")!="GetMap"||q.Get("WIDTH")!="256"||q.Get("HEIGHT")!="256"||
   q.Get("CRS")!="EPSG:3857"||q.Get("TRANSPARENT")!="TRUE"||q.Get("BBOX")=="" {
   t.Errorf("incorrect clean WMS query: %s",r.URL.RawQuery)
  }
  bbox:=q.Get("BBOX")
  mu.Lock();seen[bbox]=reqInfo{bbox:bbox};mu.Unlock()
  img:=image.NewRGBA(image.Rect(0,0,256,256))
  img.Set(0,0,color.RGBA{R:240,G:120,B:20,A:255})
  w.Header().Set("Content-Type","image/png")
  if err:=png.Encode(w,img);err!=nil{t.Error(err)}
 }))
 defer upstream.Close()
 parent:=t.TempDir()
 output:=filepath.Join(parent,"zoom-study")
 if err:=writeZoomSamples(context.Background(),output,upstream.URL,upstream.Client(),time.Second,0);err!=nil{t.Fatal(err)}
 files,err:=os.ReadDir(output);if err!=nil{t.Fatal(err)}
 if len(files)!=12{t.Fatalf("want 12 files, got %d",len(files))}
 for _,site:=range []string{"pittsburg","suisun-bay","delta-junction"}{
  for _,z:=range []int{12,13,14,15}{
   filename:=site+"-z"+strconv.Itoa(z)+"-layers-0-7.png"
   b,err:=os.ReadFile(filepath.Join(output,filename))
   if err!=nil||!validPNG(b){t.Fatalf("invalid or missing sample %s: %v",filename,err)}
  }
 }
 mu.Lock();n:=len(seen);mu.Unlock()
 if n!=12{t.Fatalf("expected 12 distinct geographic XYZ BBOX requests, got %d",n)}
 if err:=writeZoomSamples(context.Background(),output,upstream.URL,upstream.Client(),time.Second,0);err==nil{
  t.Fatal("existing output path allowed")
 }
}
func TestZoomSamplesRejectManagedCacheAndInvalidNetworkPNG(t *testing.T){
 managed:=filepath.Join(t.TempDir(),"nautical_tiles")
 if err:=os.Mkdir(managed,0700);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(filepath.Join(managed,markerFile),[]byte("SailingGo-managed-XYZ-v2\nsource=noaa\nlayers=0,1,2,3,4,5,6,7\n"),0600);err!=nil{t.Fatal(err)}
 if err:=writeZoomSamples(context.Background(),filepath.Join(managed,"study"),"http://127.0.0.1:1",nil,time.Second,0);err==nil{
  t.Fatal("allowed sample output inside managed dataset")
 }
 if _,err:=os.Stat(filepath.Join(managed,"study"));!os.IsNotExist(err){t.Fatal("managed dataset modified")}
 if err:=writeZoomSamples(context.Background(),"","http://127.0.0.1:1",nil,time.Second,0);err==nil{
  t.Fatal("missing output location accepted")
 }
 var callCount int
 server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  callCount++
  w.Header().Set("Content-Type","image/png")
  _,_=w.Write([]byte("not a PNG"))
 }))
 defer server.Close()
 output:=filepath.Join(t.TempDir(),"study")
 err:=writeZoomSamples(context.Background(),output,server.URL,server.Client(),time.Second,0)
 if err==nil||!strings.Contains(err.Error(),"invalid"){t.Fatalf("invalid NOAA sample accepted: %v",err)}
 if callCount!=1{t.Fatalf("expected fast failure after one invalid PNG, got %d",callCount)}
 entries,err:=os.ReadDir(output);if err!=nil{t.Fatal(err)}
 if len(entries)!=0{t.Fatal("invalid NOAA PNG was saved")}
}

func TestManagedAcquisitionNowAcceptsZoom12And15(t *testing.T){
 for _,zoom:=range []int{12,15}{
  o:=localPlan()
  o.Source="noaa";o.MinZoom=zoom;o.MaxZoom=zoom
  if _,err:=buildPlan(o);err!=nil{t.Fatalf("normal managed acquisition rejected zoom %d: %v",zoom,err)}
 }
}
