package main

import (
 "bytes"
 "context"
 "errors"
 "fmt"
 "io"
 "net"
 "net/http"
 "os"
 "path/filepath"
 "strconv"
 "strings"
 "time"
)

// compareNOAALayers is an isolated diagnostic only. It never uses the
// Tile Manager's managed XYZ cache, marker, download path or prune path.
// Every run requires a NEW output directory to prevent silent overwrites.
func compareNOAALayers(ctx context.Context,out,endpoint string,client *http.Client,timeout time.Duration,retries int)error{
 if strings.TrimSpace(out)==""{return errors.New("-compare-dir is required")}
 if timeout<=0||retries<0||retries>5{return errors.New("invalid -timeout or -retries")}
 abs,err:=filepath.Abs(out);if err!=nil{return err}
 if abs==string(filepath.Separator){return errors.New("refusing filesystem root as comparison directory")}
 // Refuse a child of ANY managed NOAA/mock dataset, irrespective of the
 // caller's shell variable. Avoid writing an unmanaged file into an XYZ cache.
 for parent:=filepath.Dir(abs);;parent=filepath.Dir(parent){
  marker:=filepath.Join(parent,markerFile)
  if _,err:=os.Lstat(marker);err==nil{return errors.New("comparison output cannot live inside a managed nautical tile directory")}
  if parent==filepath.Dir(parent){break}
 }
 if _,err:=os.Lstat(abs);err==nil{return errors.New("comparison directory already exists: use a new directory (no overwrites)")}else if !os.IsNotExist(err){return err}
 if client==nil{client=&http.Client{Timeout:timeout}}
 // Exact same XYZ tiles and bounding boxes for both layer configurations,
 // sampled at two locations and each accepted native zoom.
 samples:=[]struct{
  label string
  lon,lat float64
 }{
  {"pittsburg",-121.88,38.03},
  {"suisun-bay",-122.06,38.07},
 }
 var tasks []struct{file string;z,x,y int;layers string}
 for _,sample:=range samples{
  for _,z:=range []int{13,14}{
   x,y:=xyz(sample.lon,sample.lat,z)
   for _,variant:=range []struct{name,layers string}{
    {"all-0-12","0,1,2,3,4,5,6,7,8,9,10,11,12"},
    {"visible-0-7","0,1,2,3,4,5,6,7"},
   }{
    tasks=append(tasks,struct{file string;z,x,y int;layers string}{
     sample.label+"-z"+strconv.Itoa(z)+"-"+variant.name+".png",z,x,y,variant.layers,
    })
   }
  }
 }
 // No network occurs until the new directory is securely created.
 if err:=os.Mkdir(abs,0700);err!=nil{return err}
 fmt.Printf("NOAA WMS comparison: 8 requests, 4 identical XYZ pairs, z13 and z14, Pittsburg and Suisun Bay\n")
 for _,task:=range tasks{
  b,err:=comparisonRequest(ctx,endpoint,client,task.z,task.x,task.y,task.layers,retries)
  if err!=nil{return fmt.Errorf("comparison %s: %w (no managed cache touched)",task.file,err)}
  if !validPNG(b){return fmt.Errorf("invalid 256px PNG for %s",task.file)}
  // The directory is new and each filename unique. Use the ordinary atomic
  // write helper but never write anywhere inside the managed NOAA cache.
  if err:=atomicPNG(filepath.Join(abs,task.file),b);err!=nil{return err}
  fmt.Printf("  %s: %d bytes [XYZ %d/%d/%d]\n",task.file,len(b),task.z,task.x,task.y)
 }
 for i:=0;i<len(tasks);i+=2 {
  left,err:=os.ReadFile(filepath.Join(abs,tasks[i].file));if err!=nil{return err}
  right,err:=os.ReadFile(filepath.Join(abs,tasks[i+1].file));if err!=nil{return err}
  fmt.Printf("  Pair %s: byte-identical=%t\n",tasks[i].file[:strings.LastIndex(tasks[i].file,"-all-0-12.png")],bytes.Equal(left,right))
 }
 fmt.Printf("Comparison complete: %s (inspect both images in each pair; managed NOAA cache unchanged)\n",abs)
 return nil
}

func comparisonRequest(ctx context.Context,endpoint string,client *http.Client,z,x,y int,layers string,retries int)([]byte,error){
 target,err:=requestURLWithLayers(endpoint,z,x,y,layers);if err!=nil{return nil,err}
 for attempt:=0;attempt<=retries;attempt++{
  req,err:=http.NewRequestWithContext(ctx,http.MethodGet,target,nil);if err!=nil{return nil,err}
  req.Header.Set("Accept","image/png")
  req.Header.Set("User-Agent","SailingGo nautical WMS layer comparison")
  resp,err:=client.Do(req)
  retryable:=false
  if err==nil{
   if resp.StatusCode==http.StatusOK{
    b,readErr:=io.ReadAll(io.LimitReader(resp.Body,maxPNGBytes+1))
    resp.Body.Close()
    if readErr!=nil{return nil,readErr}
    if !validPNG(b){return nil,errors.New("NOAA returned invalid/non-256px PNG")}
    return b,nil
   }
   status:=resp.StatusCode;resp.Body.Close()
   err=fmt.Errorf("NOAA HTTP %d",status)
   retryable=status==429||status>=500
  }else{
   retryable=errors.Is(err,context.DeadlineExceeded)
   var ne net.Error
   if errors.As(err,&ne)&&(ne.Timeout()||ne.Temporary()){retryable=true}
  }
  if ctx.Err()!=nil{return nil,ctx.Err()}
  if !retryable||attempt==retries{return nil,fmt.Errorf("after %d attempt(s): %w",attempt+1,err)}
  delay:=time.Duration(1<<uint(attempt))*time.Second
  fmt.Fprintf(os.Stderr,"WMS comparison tile %d/%d/%d layers %s attempt %d/%d failed: %v; retrying in %s\n",z,x,y,layers,attempt+1,retries+1,err,delay)
  select{case <-ctx.Done():return nil,ctx.Err();case <-time.After(delay):}
 }
 return nil,errors.New("unreachable retry state")
}
