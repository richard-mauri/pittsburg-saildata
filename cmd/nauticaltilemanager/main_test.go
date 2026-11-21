package main

import (
 "bytes"
 "context"
 "net/http"
 "net/http/httptest"
 "os"
 "strconv"
 "path/filepath"
 "strings"
 "sync/atomic"
 "testing"
 "time"

 "pittsburg-saildata/internal/mockchart"
)

func localPlan()options{
 return options{West:-122.42,South:37.75,East:-122.41,North:37.76,
  MinZoom:9,MaxZoom:9,MaxTiles:20,Source:"mock",Timeout:30*time.Second,Retries:2}
}
func TestBoundsAndLimits(t *testing.T){
 o:=localPlan()
 p,err:=buildPlan(o);if err!=nil{t.Fatal(err)}
 if p.Total<1||p.Total>int64(o.MaxTiles){t.Fatalf("unexpected total %d",p.Total)}
 o.MaxTiles=0
 if _,err:=buildPlan(o);err==nil{t.Fatal("missing hard cap accepted")}
 o=localPlan();o.MaxZoom=18
 if _,err:=buildPlan(o);err==nil{t.Fatal("z18 must be display overzoom only")}
 o=localPlan();o.East=-123
 if _,err:=buildPlan(o);err==nil{t.Fatal("backward bounds accepted")}
 o=localPlan();o.West=-125
 if _,err:=buildPlan(o);err==nil{t.Fatal("mock outside region accepted")}
 o=localPlan();o.MaxTiles=1;o.MinZoom=9;o.MaxZoom=16
 if _,err:=buildPlan(o);err==nil||!strings.Contains(err.Error(),"exceeds"){
  t.Fatalf("oversized plan accepted: %v",err)
 }
}
func TestDryRunWritesNothingAndNoNetwork(t *testing.T){
 o:=localPlan();o.Dir=filepath.Join(t.TempDir(),"must-not-exist")
 var requests atomic.Int32
 upstream:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  requests.Add(1);http.Error(w,"unexpected",500)
 }));defer upstream.Close()
 if _,err:=execute(context.Background(),o,upstream.URL,upstream.Client());err!=nil{t.Fatal(err)}
 if _,err:=os.Stat(o.Dir);!os.IsNotExist(err){t.Fatal("dry run wrote to disk")}
 if requests.Load()!=0{t.Fatal("dry run used network")}
}
func TestDownloadMockCacheAndResume(t *testing.T){
 o:=localPlan();o.Dir=filepath.Join(t.TempDir(),"managed")
 o.Download=true
 var calls atomic.Int32
 upstream:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  calls.Add(1);http.Error(w,"unexpected",500)
 }));defer upstream.Close()
 first,err:=execute(context.Background(),o,upstream.URL,upstream.Client())
 if err!=nil{t.Fatal(err)}
 if first.Created<=0||first.Skipped!=0{t.Fatalf("first run: %+v",first)}
 again,err:=execute(context.Background(),o,upstream.URL,upstream.Client())
 if err!=nil{t.Fatal(err)}
 if again.Created!=0||again.Skipped!=first.Created{t.Fatalf("resume not a disk cache hit: %+v",again)}
 if calls.Load()!=0{t.Fatal("mock path contacted network")}
 o.MaxBytes=1
 if _,err:=execute(context.Background(),o,upstream.URL,upstream.Client());err==nil{t.Fatal("byte cap not enforced")}
 o=localPlan();o.Dir=filepath.Join(t.TempDir(),"bad");o.Download=true
 if err:=os.MkdirAll(o.Dir,0700);err!=nil{t.Fatal(err)}
 if _,err:=execute(context.Background(),o,upstream.URL,upstream.Client());err==nil{t.Fatal("unmarked directory accepted")}
}
func TestNOAAWMSAndAtomicOutput(t *testing.T){
 o:=localPlan();o.Source="noaa";o.Download=true;o.Dir=filepath.Join(t.TempDir(),"noaa")
 png,err:=mockchart.Render(9,81,197);if err!=nil{t.Fatal(err)}
 var calls atomic.Int32
 upstream:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  calls.Add(1)
  q:=r.URL.Query()
  if q.Get("REQUEST")!="GetMap"||q.Get("CRS")!="EPSG:3857"||
    q.Get("WIDTH")!="256"||q.Get("TRANSPARENT")!="TRUE"{
   t.Errorf("wrong NOAA WMS query: %v",q)
  }
  w.Header().Set("Content-Type","image/png")
  _,_=w.Write(png)
 }));defer upstream.Close()
 first,err:=execute(context.Background(),o,upstream.URL,upstream.Client())
 if err!=nil{t.Fatal(err)}
 if calls.Load()!=int32(first.Created){t.Fatalf("upstream calls %d != created %d",calls.Load(),first.Created)}
 second,err:=execute(context.Background(),o,upstream.URL,upstream.Client())
 if err!=nil{t.Fatal(err)}
 if second.Created!=0||second.Skipped!=first.Created||calls.Load()!=int32(first.Created){
  t.Fatalf("resume requested NOAA unexpectedly: %+v",second)
 }
 p,_:=buildPlan(o);r:=p.Rows[0]
 path:=filepath.Join(o.Dir,"9",itoa(r.X0),itoa(r.Y0)+".png")
 b,err:=os.ReadFile(path);if err!=nil||!bytes.Equal(b,png){t.Fatal("wrong saved NOAA bytes",err)}
}
func itoa(n int)string{return strings.TrimSpace(strconv.Itoa(n))}

func TestTransientNOAARetry(t *testing.T){
 o:=localPlan();o.Source="noaa";o.Dir=filepath.Join(t.TempDir(),"retries");o.Download=true
 data,err:=mockchart.Render(9,81,197);if err!=nil{t.Fatal(err)}
 var calls atomic.Int32
 upstream:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if calls.Add(1)==1 {http.Error(w,"temporarily unavailable",503);return}
  w.Header().Set("Content-Type","image/png");_,_=w.Write(data)
 }));defer upstream.Close()
 total,err:=execute(context.Background(),o,upstream.URL,upstream.Client())
 if err!=nil{t.Fatal(err)}
 if total.Created<1||calls.Load()!=int32(total.Created+1){t.Fatalf("expected one transient NOAA retry, got %+v calls=%d",total,calls.Load())}
}
