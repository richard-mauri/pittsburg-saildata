// Package mockchart supplies unmistakably fake XYZ images for expressly
// isolated local tests. Never use its output for navigation.
package mockchart

import (
 "bytes"
 "fmt"
 "image"
 "image/color"
 "image/draw"
 "image/png"
 "math"
 "os"
 "path/filepath"
 "strings"
)

const MarkerName = ".sailing-mock-sfbay-monterey"
const MarkerText = "SAILING_MOCK_TEST_ONLY_SF_BAY_MONTEREY_Z9_16\n"
const West, East = -123.0, -121.45
const North, South = 38.45, 36.50

func xy(lon,lat float64,z int)(int,int){
 lat=math.Max(-85.05112878,math.Min(85.05112878,lat))
 n:=math.Exp2(float64(z))
 x:=int(math.Floor((lon+180)/360*n))
 y:=int(math.Floor((1-math.Asinh(math.Tan(lat*math.Pi/180))/math.Pi)/2*n))
 return x,y
}

// Covers includes any XYZ tile intersecting the bounded Bay-to-Monterey
// rectangle. Only z9..18 is approved for generated mock output.
func Covers(z,x,y int)bool{
 if z<9||z>16||x<0||y<0||x>=1<<uint(z)||y>=1<<uint(z){return false}
 x0,y0:=xy(West,North,z)
 x1,y1:=xy(East,South,z)
 return x>=x0&&x<=x1&&y>=y0&&y<=y1
}

func HasMarker(dir string)bool{
 b,err:=os.ReadFile(filepath.Join(dir,MarkerName))
 return err==nil&&string(b)==MarkerText
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
func Render(z,x,y int) ([]byte,error) {
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
 printLine(img,13,180,2,fmt.Sprintf("%d/%d/%d",z,x,y),orange)
 var b bytes.Buffer
 err:=png.Encode(&b,img)
 return b.Bytes(),err
}
