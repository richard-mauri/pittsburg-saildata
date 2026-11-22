package main

import (
 "context"
 "image"
 "image/png"
 "net/http"
 "net/http/httptest"
 "os"
 "path/filepath"
 "strings"
 "sync"
 "testing"
 "time"
)

func TestDisplayStudyIsolatedAndSameFootprints(t *testing.T){
 var mu sync.Mutex
 seen:=map[string]map[string]bool{}
 server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  q:=r.URL.Query()
  if q.Get("LAYERS")!="0,1,2,3,4,5,6,7"||q.Get("BBOX")==""{t.Errorf("unexpected request %s",r.URL.RawQuery)}
  variant:="default"
  if q.Has("Display_params"){variant="scamin-off";if !strings.Contains(q.Get("Display_params"),"HonorScamin"){t.Errorf("missing SCAMIN parameter")}}
  mu.Lock()
  if seen[q.Get("BBOX")]==nil{seen[q.Get("BBOX")]=map[string]bool{}}
  seen[q.Get("BBOX")][variant]=true
  mu.Unlock()
  w.Header().Set("Content-Type","image/png")
  if err:=png.Encode(w,image.NewRGBA(image.Rect(0,0,256,256)));err!=nil{t.Error(err)}
 }))
 defer server.Close()
 parent:=t.TempDir()
 out:=filepath.Join(parent,"display-study")
 if err:=writeDisplayStudy(context.Background(),out,server.URL,server.Client(),time.Second,0);err!=nil{t.Fatal(err)}
 for _,z:=range []string{"13","14","15"}{
  for _,variant:=range []string{"default","scamin-off"}{
   file:=filepath.Join(out,"pittsburg-z"+z+"-"+variant+".png")
   if b,err:=os.ReadFile(file);err!=nil||!validPNG(b){t.Fatalf("missing/invalid %s: %v",file,err)}
   if _,err:=os.Stat(file+".request.txt");err!=nil{t.Fatal(err)}
  }
 }
 if len(seen)!=3{t.Fatalf("expected three distinct footprints, got %d",len(seen))}
 for bbox,v:=range seen{if !v["default"]||!v["scamin-off"]{t.Fatalf("unpaired footprint %s: %v",bbox,v)}}
 if err:=writeDisplayStudy(context.Background(),out,server.URL,server.Client(),time.Second,0);err==nil{t.Fatal("allowed output overwrite")}
}
func TestDisplayStudyRejectsManagedCache(t *testing.T){
 root:=filepath.Join(t.TempDir(),"cache")
 if err:=os.Mkdir(root,0700);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(filepath.Join(root,markerFile),[]byte("marker"),0600);err!=nil{t.Fatal(err)}
 out:=filepath.Join(root,"study")
 if err:=writeDisplayStudy(context.Background(),out,"http://127.0.0.1:1",nil,time.Second,0);err==nil{t.Fatal("allowed managed cache write")}
 if _,err:=os.Stat(out);!os.IsNotExist(err){t.Fatal("created output inside cache")}
}
