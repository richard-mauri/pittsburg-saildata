package main

import (
 "context"
 "errors"
 "fmt"
 "net/http"
 "os"
 "path/filepath"
 "strconv"
 "strings"
 "time"
)

// writeZoomSamples is an explicitly requested NOAA WMS diagnostic. Unlike
// managed acquisition, it can inspect zooms 12 and 15, but only by writing
// isolated, uniquely named PNGs under a fresh, non-managed directory.
// It never changes the accepted managed z13..14 download policy.
func writeZoomSamples(ctx context.Context,out,endpoint string,client *http.Client,timeout time.Duration,retries int) error {
 if strings.TrimSpace(out)=="" {return errors.New("-zoom-samples-dir is required")}
 if timeout<=0||retries<0||retries>5{return errors.New("invalid -timeout or -retries")}
 abs,err:=filepath.Abs(out);if err!=nil{return err}
 if abs==string(filepath.Separator){return errors.New("refusing filesystem root as sample directory")}
 // Restrict test imagery to a fresh output directory, outside every marked
 // managed NOAA/mock cache. The marker check is read-only, before any writes.
 for parent:=filepath.Dir(abs);;parent=filepath.Dir(parent){
  if _,err:=os.Lstat(filepath.Join(parent,markerFile));err==nil{
   return errors.New("zoom sample output cannot be inside a managed nautical tile directory")
  }else if !os.IsNotExist(err){return err}
  if parent==filepath.Dir(parent){break}
 }
 if _,err:=os.Lstat(abs);err==nil{return errors.New("zoom sample directory already exists: use a new path")}else if !os.IsNotExist(err){return err}
 if client==nil{client=&http.Client{Timeout:timeout}}
 samples:=[]struct{name string;lon,lat float64}{
  {"pittsburg",-121.88,38.03},
  {"suisun-bay",-122.06,38.07},
  {"delta-junction",-121.65,38.06},
 }
 const cleanLayers="0,1,2,3,4,5,6,7"
 if err:=os.Mkdir(abs,0700);err!=nil{return err}
 fmt.Printf("Clean NOAA zoom study: 12 PNGs, 3 geographic samples, native zooms 12,13,14,15, WMS layers 0..7; output=%s\n",abs)
 for _,sample:=range samples{
  for _,z:=range []int{12,13,14,15}{
   select{case <-ctx.Done():return ctx.Err();default:}
   x,y:=xyz(sample.lon,sample.lat,z)
   b,err:=comparisonRequest(ctx,endpoint,client,z,x,y,cleanLayers,retries)
   if err!=nil{return fmt.Errorf("sample %s z%d: %w (managed chart cache unchanged)",sample.name,z,err)}
   if !validPNG(b){return fmt.Errorf("sample %s z%d returned invalid PNG",sample.name,z)}
   file:=sample.name+"-z"+strconv.Itoa(z)+"-layers-0-7.png"
   if err:=atomicPNG(filepath.Join(abs,file),b);err!=nil{return err}
   fmt.Printf("  %s: %d PNG bytes [XYZ %d/%d/%d]\n",file,len(b),z,x,y)
  }
 }
 fmt.Println("Zoom study complete: inspect z12/z15 beside accepted z13/z14; managed chart cache unchanged")
 return nil
}
