package main

import (
 "context"
 "os"
 "path/filepath"
 "strconv"
 "testing"
 "time"

 "pittsburg-saildata/internal/mockchart"
)

// Recreate legacy v1-style downloaded NOAA tile paths without making any
// remote request. The existing operator dataset also uses the v1 marker.
func legacyNOAARoot(t *testing.T) string {
 t.Helper()
 root:=filepath.Join(t.TempDir(),"managed-noaa")
 if err:=os.Mkdir(root,0700);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(filepath.Join(root,markerFile),[]byte("SailingGo-managed-XYZ-v1\nsource=noaa\nbounds=-123.00000000,36.50000000,-121.45000000,38.45000000\nzoom=9..12\n"),0600);err!=nil{t.Fatal(err)}
 for _,z:=range []int{9,10,11,12,13,14,15,16}{
  x,y:=xyz(-121.9,38.0,z)
  pngData,err:=mockchart.Render(z,x,y);if err!=nil{t.Fatal(err)}
  dir:=filepath.Join(root,strconv.Itoa(z),strconv.Itoa(x))
  if err:=os.MkdirAll(dir,0700);err!=nil{t.Fatal(err)}
  if err:=os.WriteFile(filepath.Join(dir,strconv.Itoa(y)+".png"),pngData,0600);err!=nil{t.Fatal(err)}
 }
 return root
}
func TestPruneDryRunAndExplicitApply(t *testing.T){
 root:=legacyNOAARoot(t)
 o:=options{Source:"noaa",Dir:root,MaxTiles:1,MaxBytes:1,Timeout:time.Second}
 _,before,err:=inspectManaged(options{Source:"noaa",Dir:root,MaxTiles:100,AllowLegacyNOAA:true},plan{})
 if err!=nil||before.Count!=8{t.Fatalf("bad fixture inventory %+v err=%v",before,err)}
 if err:=pruneManaged(o,false);err!=nil{t.Fatal(err)}
 _,afterDry,err:=inspectManaged(options{Source:"noaa",Dir:root,MaxTiles:100,AllowLegacyNOAA:true},plan{})
 if err!=nil||afterDry!=before{t.Fatalf("dry run mutated dataset: %+v versus %+v",afterDry,before)}
 if err:=pruneManaged(o,true);err!=nil{t.Fatal(err)}
 _,after,err:=inspectManaged(options{Source:"noaa",Dir:root,MaxTiles:100,AllowLegacyNOAA:true},plan{})
 if err!=nil||after.Count!=2||after.Bytes>=before.Bytes{t.Fatalf("prune didn't retain z13/14 only: %+v %v",after,err)}
 for _,z:=range []int{9,10,11,12,15,16} {
  if _,err:=os.Stat(filepath.Join(root,strconv.Itoa(z)));!os.IsNotExist(err){t.Fatalf("zoom %d not removed: %v",z,err)}
 }
 for _,z:=range []int{13,14}{
  if _,err:=os.Stat(filepath.Join(root,strconv.Itoa(z)));err!=nil{t.Fatalf("zoom %d was removed: %v",z,err)}
 }
 if _,err:=os.Stat(filepath.Join(root,markerFile));err!=nil{t.Fatal("marker removed")}
 if err:=pruneManaged(o,true);err!=nil{t.Fatalf("prune is not idempotent: %v",err)}
 // Legacy NOAA PNGs were made with layers 0-12 and must NEVER be extended
 // with cleaner 0-7 tiles, even after old zooms are pruned.
 ext:=options{Source:"noaa",Dir:root,West:-121.9,East:-121.89,South:37.99,North:38.0,MinZoom:13,MaxZoom:14,MaxTiles:100,Timeout:time.Second}
 if _,err:=execute(context.Background(),ext,"",nil);err==nil{t.Fatal("legacy all-layer NOAA cache was accepted for clean-layer acquisition")}
}
func TestPruneRefusesUnknownOrMixedDataset(t *testing.T){
 root:=legacyNOAARoot(t)
 o:=options{Source:"mock",Dir:root}
 if err:=pruneManaged(o,true);err==nil{t.Fatal("mock source pruned NOAA dataset")}
 o.Source="noaa"
 extra:=filepath.Join(root,"unmanaged.txt")
 if err:=os.WriteFile(extra,[]byte("keep me"),0600);err!=nil{t.Fatal(err)}
 if err:=pruneManaged(o,true);err==nil{t.Fatal("unsafe extra file not rejected")}
 if _,err:=os.Stat(filepath.Join(root,"9"));err!=nil{t.Fatal("prune deleted tiles despite unsafe extra file")}
 if _,err:=os.Stat(extra);err!=nil{t.Fatal("unexpected file removed")}
}
func TestRestrictNOAAAcquisitionToNative13And14(t *testing.T){
 for _,pair:=range [][2]int{{9,12},{12,13},{14,15},{15,16},{13,14}} {
  o:=localPlan();o.Source="noaa";o.MinZoom=pair[0];o.MaxZoom=pair[1]
  _,err:=buildPlan(o)
  if pair==[2]int{13,14} {
   if err!=nil{t.Fatalf("valid 13-14 plan rejected: %v",err)}
  } else if err==nil{t.Fatalf("bad NOAA range %v accepted",pair)}
 }
}
