package main

import(
 "archive/zip"
 "encoding/binary"
 "errors"
 "fmt"
 "io"
 "math"
 "strings"
)
type xy struct{X,Y float64}
type edge struct{A,B xy}
type stateBoundary struct{bins map[int][]edge;Rings,Edges int}
const binSize=0.05
const datumMargin=0.0001
func finite(x float64)bool{return !math.IsNaN(x)&&!math.IsInf(x,0)}
func inEnvelope(x,y float64)bool{return finite(x)&&finite(y)&&x>=-125&&x<=-114&&y>=32&&y<=42.1}
func newBoundary(rings [][]xy)(*stateBoundary,error){
 b:=&stateBoundary{bins:map[int][]edge{},Rings:len(rings)}
 if len(rings)==0{return nil,errors.New("empty boundary")}
 refs:=0
 for _,r:=range rings{
  if len(r)<4||r[0]!=r[len(r)-1]{return nil,errors.New("invalid closed ring")}
  for i:=1;i<len(r);i++{a,z:=r[i-1],r[i];if !inEnvelope(a.X,a.Y)||!inEnvelope(z.X,z.Y){return nil,errors.New("boundary outside expected envelope")}
   if a==z{continue};b.Edges++;if b.Edges>2000000{return nil,errors.New("edge cap")}
   lo:=int(math.Floor((math.Min(a.Y,z.Y)-datumMargin)/binSize));hi:=int(math.Floor((math.Max(a.Y,z.Y)+datumMargin)/binSize))
   for k:=lo;k<=hi;k++{refs++;if refs>4000000{return nil,errors.New("boundary bin cap")};b.bins[k]=append(b.bins[k],edge{a,z})}
  }
 }
 return b,nil
}
// Even-odd crossing across all polygon rings (including holes). A conservative
// 5m near-edge quarantine avoids treating NAD83 coordinates as exact WGS84.
func(b *stateBoundary)contains(x,y float64)(inside,near bool){
 if !inEnvelope(x,y){return false,false}
 for _,e:=range b.bins[int(math.Floor(y/binSize))]{
  a,z:=e.A,e.B
  if (a.Y>y)!=(z.Y>y)&&x<a.X+(z.X-a.X)*(y-a.Y)/(z.Y-a.Y){inside=!inside}
  if x<math.Min(a.X,z.X)-datumMargin||x>math.Max(a.X,z.X)+datumMargin||y<math.Min(a.Y,z.Y)-datumMargin||y>math.Max(a.Y,z.Y)+datumMargin{continue}
  scale:=111195*math.Cos(y*math.Pi/180)
  ax,ay:=(a.X-x)*scale,(a.Y-y)*111195
  bx,by:=(z.X-x)*scale,(z.Y-y)*111195
  dx,dy:=bx-ax,by-ay;den:=dx*dx+dy*dy
  t:=0.0;if den>0{t=math.Max(0,math.Min(1,-(ax*dx+ay*dy)/den))}
  if math.Hypot(ax+t*dx,ay+t*dy)<=5{near=true}
 }
 return
}
func u32(b []byte)int{return int(binary.LittleEndian.Uint32(b))}
func be32(b []byte)int{return int(binary.BigEndian.Uint32(b))}
func f64(b []byte)float64{return math.Float64frombits(binary.LittleEndian.Uint64(b))}
func dbfCalifornia(raw []byte)(int,int,error){
 if len(raw)<33||raw[0]!=3{return 0,0,errors.New("unsupported DBF")}
 n:=u32(raw[4:8]);header:=int(binary.LittleEndian.Uint16(raw[8:10]));width:=int(binary.LittleEndian.Uint16(raw[10:12]))
 if n<1||n>1000||header<33||header>len(raw)||width<2||width>4096||int64(header)+int64(n)*int64(width)>int64(len(raw))||raw[header-1]!=13||(header-33)%32!=0{return 0,0,errors.New("bad DBF layout")}
 pos,field:=1,-1
 for i:=32;i+32<=header-1;i+=32{d:=raw[i:i+32];name:=strings.TrimRight(string(d[:11]),"\x00 ");l:=int(d[16]);if l<1||pos+l>width{return 0,0,errors.New("bad DBF field")};if name=="STATEFP"{if d[11]!='C'||l!=2||field!=-1{return 0,0,errors.New("bad STATEFP")};field=pos};pos+=l}
 if field<0||pos!=width{return 0,0,errors.New("missing STATEFP")}
 found:=-1
 for i:=0;i<n;i++{r:=raw[header+i*width:header+(i+1)*width];if r[0]!=' '&&r[0]!='*'{return 0,0,errors.New("bad DBF row")};if r[0]==' '&&string(r[field:field+2])=="06"{if found!=-1{return 0,0,errors.New("duplicate California")};found=i}}
 if found<0{return 0,0,errors.New("California missing")}
 return found,n,nil
}
func shapeCalifornia(raw []byte,index,total int)([][]xy,error){
 if len(raw)<100||be32(raw[:4])!=9994||be32(raw[24:28])*2!=len(raw)||u32(raw[28:32])!=1000||u32(raw[32:36])!=5{return nil,errors.New("unsupported polygon SHP")}
 var selected [][]xy;count:=0
 for off:=100;off<len(raw);{
  if len(raw)-off<8{return nil,errors.New("truncated shape")}
  size:=be32(raw[off+4:off+8])*2
  if size<4||size>len(raw)-off-8||be32(raw[off:off+4])!=count+1{return nil,errors.New("bad shape record")}
  p:=raw[off+8:off+8+size];off+=8+size
  if count==index{
   if len(p)<44||u32(p[:4])!=5{return nil,errors.New("California shape is not polygon")}
   parts,points:=u32(p[36:40]),u32(p[40:44])
   if parts<1||parts>100000||points<4||points>2000000||44+4*parts+16*points!=len(p){return nil,errors.New("bad polygon layout")}
   start:=44+4*parts
   prev:=-1
   for j:=0;j<parts;j++{a:=u32(p[44+4*j:]);z:=points;if j+1<parts{z=u32(p[48+4*j:])};if a<=prev||(j==0&&a!=0)||z-a<4||z>points{return nil,errors.New("bad polygon part")};prev=a
    ring:=make([]xy,z-a);for k:=a;k<z;k++{v:=p[start+16*k:];ring[k-a]=xy{f64(v[:8]),f64(v[8:16])}};selected=append(selected,ring)
   }
  }
  count++
 }
 if count!=total||len(selected)==0{return nil,errors.New("DBF/SHP record count mismatch")}
 return selected,nil
}
func loadBoundary(p string)(*stateBoundary,error){
 z,e:=zip.OpenReader(p);if e!=nil{return nil,e};defer z.Close()
 if len(z.File)>32{return nil,errors.New("archive member cap")}
 wanted:=map[string][]byte{};var expanded uint64
 for _,f:=range z.File{
  if strings.ContainsAny(f.Name,"/\\")||f.Name=="."||f.Name==".."||!f.Mode().IsRegular(){return nil,errors.New("unsafe archive member")}
  if f.UncompressedSize64>256<<20||expanded>256<<20-f.UncompressedSize64{return nil,errors.New("expanded archive cap")};expanded+=f.UncompressedSize64
  switch f.Name{case "tl_2026_us_state.shp","tl_2026_us_state.dbf","tl_2026_us_state.prj":
   if _,ok:=wanted[f.Name];ok{return nil,errors.New("duplicate archive member")}
   if strings.HasSuffix(f.Name,".prj")&&f.UncompressedSize64>8192{return nil,errors.New("CRS cap")}
   r,e:=f.Open();if e!=nil{return nil,e};raw,e:=io.ReadAll(io.LimitReader(r,int64(f.UncompressedSize64)+1));r.Close();if e!=nil||uint64(len(raw))!=f.UncompressedSize64{return nil,errors.New("archive length/CRC failure")};wanted[f.Name]=raw
  }
 }
 prj:=strings.ToUpper(strings.TrimSpace(string(wanted["tl_2026_us_state.prj"])))
 if !strings.HasPrefix(prj,"GEOGCS[")||!strings.Contains(prj,"NORTH_AMERICAN_1983")||!strings.Contains(prj,"GRS_1980")||!strings.Contains(prj,"GREENWICH")||!strings.Contains(prj,"DEGREE")||strings.Contains(prj,"PROJCS"){return nil,fmt.Errorf("unsupported state CRS %q",prj)}
 idx,n,e:=dbfCalifornia(wanted["tl_2026_us_state.dbf"]);if e!=nil{return nil,e}
 rings,e:=shapeCalifornia(wanted["tl_2026_us_state.shp"],idx,n);if e!=nil{return nil,e};return newBoundary(rings)
}
