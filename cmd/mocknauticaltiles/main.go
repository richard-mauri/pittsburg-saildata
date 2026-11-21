// Command mocknauticaltiles creates small, unmistakably fake XYZ chart tiles
// for offline, owner-controlled testing. Never point it at a live NOAA cache.
package main

import (
 "bytes"
 "flag"
 "fmt"
 "image"
 "image/color"
 "image/draw"
 "image/png"
 "math"
 "os"
 "path/filepath"
 "strconv"
 "strings"
)

type location struct{name string; lon,lat float64}
var sites=[]location{
 {"PITTSBURG",-121.885,38.028},
 {"DELTA",-121.590,38.095},
}
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

// This 5x7 block font deliberately avoids external dependencies/network.
var glyphs=map[rune][7]string{
 'A':{"01110","10001","10001","11111","10001","10001","10001"},
 'B':{"11110","10001","10001","11110","10001","10001","11110"},
 'C':{"01111","10000","10000","10000","10000","10000","01111"},
 'D':{"11110","10001","10001","10001","10001","10001","11110"},
 'E':{"11111","10000","10000","11110","10000","10000","11111"},
 'F':{"11111","10000","10000","11110","10000","10000","10000"},
 'G':{"01111","10000","10000","10111","10001","10001","01111"},
 'H':{"10001","10001","10001","11111","10001","10001","10001"},
 'I':{"11111","00100","00100","00100","00100","00100","11111"},
 'K':{"10001","10010","10100","11000","10100","10010","10001"},
 'L':{"10000","10000","10000","10000","10000","10000","11111"},
 'M':{"10001","11011","10101","10101","10001","10001","10001"},
 'N':{"10001","11001","10101","10011","10001","10001","10001"},
 'O':{"01110","10001","10001","10001","10001","10001","01110"},
 'P':{"11110","10001","10001","11110","10000","10000","10000"},
 'R':{"11110","10001","10001","11110","10100","10010","10001"},
 'S':{"01111","10000","10000","01110","00001","00001","11110"},
 'T':{"11111","00100","00100","00100","00100","00100","00100"},
 'U':{"10001","10001","10001","10001","10001","10001","01110"},
 'V':{"10001","10001","10001","10001","10001","01010","00100"},
 'X':{"10001","10001","01010","00100","01010","10001","10001"},
 'Y':{"10001","10001","01010","00100","00100","00100","00100"},
 '0':{"01110","10001","10011","10101","11001","10001","01110"},
 '1':{"00100","01100","00100","00100","00100","00100","01110"},
 '2':{"01110","10001","00001","00010","00100","01000","11111"},
 '3':{"11110","00001","00001","01110","00001","00001","11110"},
 '4':{"00010","00110","01010","10010","11111","00010","00010"},
 '5':{"11111","10000","10000","11110","00001","00001","11110"},
 '6':{"01110","10000","10000","11110","10001","10001","01110"},
 '7':{"11111","00001","00010","00100","01000","01000","01000"},
 '8':{"01110","10001","10001","01110","10001","10001","01110"},
 '9':{"01110","10001","10001","01111","00001","00001","01110"},
 '/':{"00001","00001","00010","00100","01000","10000","10000"},
 '-':{"00000","00000","00000","11111","00000","00000","00000"},
 ':':{"00000","00100","00100","00000","00100","00100","00000"},
 ' ':{"00000","00000","00000","00000","00000","00000","00000"},
}

func printLine(img *image.RGBA,x,y,scale int,s string,c color.RGBA) {
 for _,letter:=range strings.ToUpper(s){
  pattern,ok:=glyphs[letter];if !ok {panic("missing mock glyph")}
  for dy,row:=range pattern {
   for dx,px:=range row {
    if px=='1' {
     draw.Draw(img,image.Rect(x+dx*scale,y+dy*scale,x+(dx+1)*scale,y+(dy+1)*scale),&image.Uniform{C:c},image.Point{},draw.Src)
    }
   }
  }
  x+=6*scale
 }
}
func render(t tile) ([]byte,error) {
 img:=image.NewRGBA(image.Rect(0,0,256,256))
 draw.Draw(img,img.Bounds(),&image.Uniform{C:color.RGBA{20,39,65,255}},image.Point{},draw.Src)
 // Grid and diagonals make tile orientation and seams unmistakable.
 grid:=color.RGBA{65,124,151,255}
 for v:=0;v<256;v++ {img.Set(v,0,grid);img.Set(0,v,grid);img.Set(v,v,grid);img.Set(255-v,v,grid)}
 orange:=color.RGBA{255,180,65,255}
 white:=color.RGBA{255,255,255,255}
 printLine(img,13,30,2,"MOCK TEST DATA",orange)
 printLine(img,13,100,2,"NOT FOR",white)
 printLine(img,13,120,2,"NAVIGATION",white)
 printLine(img,13,180,2,fmt.Sprintf("%d/%d/%d",t.z,t.x,t.y),orange)
 var b bytes.Buffer
 err:=png.Encode(&b,img)
 return b.Bytes(),err
}
func run(directory string,zooms []int) error {
 if directory=="" {return fmt.Errorf("-dir is required")}
 abs,err:=filepath.Abs(directory);if err!=nil{return err}
 // Avoid accidental writes to any existing chart cache. Fresh directory only.
 if _,err=os.Lstat(abs);err==nil {return fmt.Errorf("fixture output already exists: %s",abs)}
 if !os.IsNotExist(err){return err}
 tiles:=map[tile]struct{}{}
 for _,z:=range zooms {
  if z<10||z>12{return fmt.Errorf("zoom %d outside approved test range 10..12",z)}
  n:=1<<uint(z)
  for _,site:=range sites {
   center:=xyz(site.lon,site.lat,z)
   for dx:=-1;dx<=1;dx++ {for dy:=-1;dy<=1;dy++ {
    x,y:=center.x+dx,center.y+dy
    if x>=0&&x<n&&y>=0&&y<n {tiles[tile{z,x,y}]=struct{}{}}
   }}
  }
 }
 if err=os.MkdirAll(abs,0700);err!=nil{return err}
 for t:=range tiles{
  pngBytes,err:=render(t);if err!=nil{return err}
  dir:=filepath.Join(abs,strconv.Itoa(t.z),strconv.Itoa(t.x))
  if err=os.MkdirAll(dir,0700);err!=nil{return err}
  if err=os.WriteFile(filepath.Join(dir,fmt.Sprintf("%d.png",t.y)),pngBytes,0600);err!=nil{return err}
 }
 fmt.Printf("Created %d MOCK TEST DATA XYZ PNG tiles at %s (never for navigation)\n",len(tiles),abs)
 return nil
}
func main(){
 directory:=flag.String("dir","","new, nonexistent test-only tile directory outside Git/live cache")
 flag.Parse()
 if flag.NArg()!=0 {fmt.Fprintln(os.Stderr,"unexpected arguments");os.Exit(2)}
 if err:=run(*directory,[]int{10,11,12});err!=nil{fmt.Fprintln(os.Stderr,err);os.Exit(1)}
}
