// Command mocknauticaltiles pre-generates lower-zoom test PNGs for the full
// San Francisco Bay through Monterey region at z9..12. Use the standalone
// nauticaltilemanager for additional local mock XYZ paths at native z13..16.
// These are fake images, never for navigation.
package main

import (
 "flag"
 "fmt"
 "math"
 "os"
 "path/filepath"
 "strconv"

 "pittsburg-saildata/internal/mockchart"
)

type tile struct{z,x,y int}
func xyz(lon,lat float64,z int) tile {
 lat=math.Max(-85.05112878,math.Min(85.05112878,lat))
 n:=math.Exp2(float64(z))
 x:=int(math.Floor((lon+180)/360*n))
 latRad:=lat*math.Pi/180
 y:=int(math.Floor((1-math.Asinh(math.Tan(latRad))/math.Pi)/2*n))
 limit:=(1<<uint(z))-1
 if x<0{x=0};if x>limit{x=limit}
 if y<0{y=0};if y>limit{y=limit}
 return tile{z,x,y}
}

// Pre-generating z9..12 keeps disk usage and owner Mac build costs bounded.
// Missing z13..16 tiles must be created with the separate Tile Manager.
func run(directory string) error {
 if directory=="" {return fmt.Errorf("-dir is required")}
 abs,err:=filepath.Abs(directory);if err!=nil{return err}
 if _,err=os.Lstat(abs);err==nil{return fmt.Errorf("fixture output already exists: %s",abs)}
 if !os.IsNotExist(err){return err}
 tiles:=map[tile]struct{}{}
 for z:=9;z<=12;z++ {
  nw,se:=xyz(mockchart.West,mockchart.North,z),xyz(mockchart.East,mockchart.South,z)
  for x:=nw.x;x<=se.x;x++ {for y:=nw.y;y<=se.y;y++ {
   tiles[tile{z,x,y}]=struct{}{}
   if len(tiles)>1200{return fmt.Errorf("pre-generated mock tile ceiling exceeded")}
  }}
 }
 if err=os.MkdirAll(abs,0700);err!=nil{return err}
 for t:=range tiles {
  pngBytes,err:=mockchart.Render(t.z,t.x,t.y);if err!=nil{return err}
  dir:=filepath.Join(abs,strconv.Itoa(t.z),strconv.Itoa(t.x))
  if err=os.MkdirAll(dir,0700);err!=nil{return err}
  if err=os.WriteFile(filepath.Join(dir,fmt.Sprintf("%d.png",t.y)),pngBytes,0600);err!=nil{return err}
 }
 // Strict marker enables on-demand render only when an expressly opted-in
 // loopback SailingGo test process is used (never on the public hostname).
 if err=os.WriteFile(filepath.Join(abs,mockchart.MarkerName),[]byte(mockchart.MarkerText),0600);err!=nil{return err}
 fmt.Printf("Created %d MOCK TEST DATA XYZ PNG tiles at %s (never for navigation); region z9-12 only; use nauticaltilemanager for z13-16\n",len(tiles),abs)
 return nil
}
func main(){
 directory:=flag.String("dir","","new, nonexistent test-only tile directory outside Git/live cache")
 flag.Parse()
 if flag.NArg()!=0 {fmt.Fprintln(os.Stderr,"unexpected arguments");os.Exit(2)}
 if err:=run(*directory);err!=nil{fmt.Fprintln(os.Stderr,err);os.Exit(1)}
}
