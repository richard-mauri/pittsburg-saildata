// Command nauticaltilemanager plans and acquires a bounded XYZ chart dataset.
// Dry-run is the default and never contacts NOAA or writes to disk.
package main

import (
 "bytes"
 "context"
 "errors"
 "flag"
 "fmt"
 "image/png"
 "io"
 "math"
 "net/http"
 "net/url"
 "os"
 "path/filepath"
 "strconv"
 "strings"
 "time"

 "pittsburg-saildata/internal/mockchart"
)

const noaaURL = "https://gis.charttools.noaa.gov/arcgis/rest/services/MCS/NOAAChartDisplay/MapServer/exts/MaritimeChartService/WMSServer"
const maxPNGBytes = 4 << 20
const markerFile = ".sailing-nautical-tile-manager.json"

type options struct {
 West, South, East, North float64
 MinZoom, MaxZoom, MaxTiles int
 MaxBytes int64
 Dir, Source string
 Download bool
}
type rectangle struct {Z,X0,Y0,X1,Y1 int}
func (r rectangle) count() int64 {return int64(r.X1-r.X0+1)*int64(r.Y1-r.Y0+1)}
type plan struct {Rows []rectangle; Total int64}

func xyz(lon,lat float64,z int) (x,y int) {
 n:=math.Exp2(float64(z))
 x=int(math.Floor((lon+180)/360*n))
 y=int(math.Floor((1-math.Asinh(math.Tan(lat*math.Pi/180))/math.Pi)/2*n))
 return
}
func buildPlan(o options) (plan,error) {
 var p plan
 if math.IsNaN(o.West)||math.IsNaN(o.East)||math.IsNaN(o.South)||math.IsNaN(o.North)||
    math.IsInf(o.West,0)||math.IsInf(o.East,0)||math.IsInf(o.South,0)||math.IsInf(o.North,0)||
    o.West < -180 || o.East > 180 || o.West>=o.East ||
    o.South < -85 || o.North > 85 || o.South>=o.North {
  return p,errors.New("invalid geographic bounds: use west < east, south < north, lon [-180,180] and lat [-85,85]")
 }
 if o.MinZoom<9 || o.MaxZoom>16 || o.MinZoom>o.MaxZoom {
  return p,errors.New("native NOAA tile zoom must be within 9..16 (display z17/18 overzooms z16)")
 }
 if o.MaxTiles<=0 {return p,errors.New("-max-tiles must be positive")}
 if o.MaxBytes<0 {return p,errors.New("-max-bytes must be nonnegative")}
 if o.Source!="noaa" && o.Source!="mock" {return p,errors.New("-source must be noaa or mock")}
 if o.Source=="mock" && (o.West<mockchart.West||o.East>mockchart.East||
   o.South<mockchart.South||o.North>mockchart.North) {
  return p,errors.New("mock bounds must be inside the test-only Bay-to-Monterey rectangle")
 }
 for z:=o.MinZoom;z<=o.MaxZoom;z++ {
  x0,y0:=xyz(o.West,o.North,z)
  x1,y1:=xyz(o.East,o.South,z)
  // The bounding box is conservatively inclusive of boundary tiles.
  r:=rectangle{z,x0,y0,x1,y1}
  p.Total+=r.count()
  if p.Total>int64(o.MaxTiles) {
   return p,fmt.Errorf("planned %d tiles exceeds -max-tiles=%d (through zoom %d); reduce bounds/zoom or raise cap explicitly",p.Total,o.MaxTiles,z)
  }
  p.Rows=append(p.Rows,r)
 }
 return p,nil
}
func (o options) print(p plan) {
 fmt.Printf("Nautical tile plan: source=%s bounds=%.5f,%.5f,%.5f,%.5f zoom=%d..%d max-tiles=%d total=%d\n",
  o.Source,o.West,o.South,o.East,o.North,o.MinZoom,o.MaxZoom,o.MaxTiles,p.Total)
 for _,r:=range p.Rows {fmt.Printf("  z%d: %d tiles (x=%d..%d y=%d..%d)\n",r.Z,r.count(),r.X0,r.X1,r.Y0,r.Y1)}
 if !o.Download {fmt.Println("DRY RUN: no network or disk writes; pass -download to fetch/save tiles")}
}
func (o options) signature() string {
 // Deliberately excludes caps and -download so a stricter resume is safe.
 return fmt.Sprintf("SailingGo-managed-XYZ-v1\nsource=%s\nbounds=%.8f,%.8f,%.8f,%.8f\nzoom=%d..%d\n",
  o.Source,o.West,o.South,o.East,o.North,o.MinZoom,o.MaxZoom)
}
func safeOutput(o options) (string,error) {
 if strings.TrimSpace(o.Dir)=="" {return "",errors.New("-dir is required with -download")}
 abs,err:=filepath.Abs(o.Dir);if err!=nil{return "",err}
 if abs==string(filepath.Separator) {return "",errors.New("refusing filesystem root as output")}
 info,err:=os.Lstat(abs)
 if os.IsNotExist(err){
  if err=os.MkdirAll(abs,0700);err!=nil{return "",err}
  if err=os.WriteFile(filepath.Join(abs,markerFile),[]byte(o.signature()),0600);err!=nil{return "",err}
  return abs,nil
 }
 if err!=nil{return "",err}
 if !info.IsDir()||info.Mode()&os.ModeSymlink!=0{return "",errors.New("output must be a real directory")}
 b,err:=os.ReadFile(filepath.Join(abs,markerFile))
 if err!=nil||string(b)!=o.signature(){return "",errors.New("existing directory has no matching Tile Manager marker; refusing to overwrite or mix caches")}
 return abs,nil
}
func validPNG(b []byte)bool{
 if len(b)<8||len(b)>maxPNGBytes||!bytes.Equal(b[:8],[]byte{137,80,78,71,13,10,26,10}){return false}
 cfg,err:=png.DecodeConfig(bytes.NewReader(b));if err!=nil||cfg.Width!=256||cfg.Height!=256{return false}
 _,err=png.Decode(bytes.NewReader(b));return err==nil
}
func requestURL(base string,z,x,y int)(string,error){
 u,err:=url.Parse(base);if err!=nil||u.Scheme==""||u.Host==""{return "",errors.New("invalid NOAA URL")}
 const half=20037508.342789244
 size:=2*half/math.Exp2(float64(z))
 left:= -half+float64(x)*size
 top:=half-float64(y)*size
 q:=u.Query()
 q.Set("SERVICE","WMS");q.Set("VERSION","1.3.0");q.Set("REQUEST","GetMap")
 q.Set("LAYERS","0,1,2,3,4,5,6,7,8,9,10,11,12");q.Set("STYLES","")
 q.Set("CRS","EPSG:3857")
 q.Set("BBOX",fmt.Sprintf("%.6f,%.6f,%.6f,%.6f",left,top-size,left+size,top))
 q.Set("WIDTH","256");q.Set("HEIGHT","256");q.Set("FORMAT","image/png");q.Set("TRANSPARENT","TRUE")
 u.RawQuery=q.Encode()
 return u.String(),nil
}
func getTile(ctx context.Context,o options,base string,client *http.Client,z,x,y int)([]byte,error){
 if o.Source=="mock" {return mockchart.Render(z,x,y)}
 target,err:=requestURL(base,z,x,y);if err!=nil{return nil,err}
 req,err:=http.NewRequestWithContext(ctx,http.MethodGet,target,nil);if err!=nil{return nil,err}
 req.Header.Set("Accept","image/png")
 req.Header.Set("User-Agent","SailingGo nautical tile manager")
 resp,err:=client.Do(req);if err!=nil{return nil,err}
 defer resp.Body.Close()
 if resp.StatusCode!=http.StatusOK{return nil,fmt.Errorf("NOAA HTTP %d",resp.StatusCode)}
 b,err:=io.ReadAll(io.LimitReader(resp.Body,maxPNGBytes+1));if err!=nil{return nil,err}
 if !validPNG(b){return nil,errors.New("NOAA returned invalid/non-256px PNG")}
 return b,nil
}
func atomicPNG(path string,b []byte)error{
 dir:=filepath.Dir(path)
 if err:=os.MkdirAll(dir,0700);err!=nil{return err}
 f,err:=os.CreateTemp(dir,".tile-*.tmp");if err!=nil{return err}
 defer os.Remove(f.Name())
 if err=f.Chmod(0600);err!=nil{f.Close();return err}
 if _,err=f.Write(b);err!=nil{f.Close();return err}
 if err=f.Close();err!=nil{return err}
 // Existing files are never overwritten; this command is single-writer.
 if _,err=os.Lstat(path);err==nil{return errors.New("refusing to overwrite existing tile")}
 return os.Rename(f.Name(),path)
}
type totals struct {Skipped,Created int64; Bytes int64}
func execute(ctx context.Context,o options,base string,client *http.Client)(totals,error){
 var t totals
 p,err:=buildPlan(o);if err!=nil{return t,err}
 o.print(p)
 if !o.Download{return t,nil}
 dir,err:=safeOutput(o);if err!=nil{return t,err}
 if client==nil{client=&http.Client{Timeout:5*time.Second}}
 // Serial and deterministic; no unbounded concurrent NOAA downloads.
 for _,row:=range p.Rows{
  for x:=row.X0;x<=row.X1;x++{for y:=row.Y0;y<=row.Y1;y++{
   select{case <-ctx.Done():return t,ctx.Err();default:}
   path:=filepath.Join(dir,strconv.Itoa(row.Z),strconv.Itoa(x),strconv.Itoa(y)+".png")
   if b,err:=os.ReadFile(path);err==nil {
    if !validPNG(b){return t,fmt.Errorf("invalid existing tile, refusing overwrite: %s",path)}
    t.Skipped++;t.Bytes+=int64(len(b))
    if o.MaxBytes>0&&t.Bytes>o.MaxBytes{return t,fmt.Errorf("existing cache exceeds -max-bytes=%d",o.MaxBytes)}
    continue
   }else if !os.IsNotExist(err){return t,err}
   b,err:=getTile(ctx,o,base,client,row.Z,x,y)
   if err!=nil{return t,fmt.Errorf("tile %d/%d/%d: %w",row.Z,x,y,err)}
   if !validPNG(b){return t,fmt.Errorf("tile %d/%d/%d: invalid PNG",row.Z,x,y)}
   if o.MaxBytes>0&&t.Bytes+int64(len(b))>o.MaxBytes{return t,fmt.Errorf("next tile exceeds -max-bytes=%d; resume with larger budget only if intended",o.MaxBytes)}
   if err:=atomicPNG(path,b);err!=nil{return t,err}
   t.Created++;t.Bytes+=int64(len(b))
  }}
 }
 fmt.Printf("Saved %d new tiles, verified %d existing tiles, PNG bytes=%d (native z9..16 only)\n",t.Created,t.Skipped,t.Bytes)
 return t,nil
}
func main(){
 var o options
 flag.Float64Var(&o.West,"west",math.NaN(),"western longitude")
 flag.Float64Var(&o.South,"south",math.NaN(),"southern latitude")
 flag.Float64Var(&o.East,"east",math.NaN(),"eastern longitude")
 flag.Float64Var(&o.North,"north",math.NaN(),"northern latitude")
 flag.IntVar(&o.MinZoom,"min-zoom",9,"first native nautical zoom (9..16)")
 flag.IntVar(&o.MaxZoom,"max-zoom",12,"last native nautical zoom (9..16)")
 flag.IntVar(&o.MaxTiles,"max-tiles",0,"required hard ceiling on planned XYZ tile count")
 flag.Int64Var(&o.MaxBytes,"max-bytes",0,"optional total cached PNG bytes ceiling (0 = not configured)")
 flag.StringVar(&o.Dir,"dir","","isolated output directory (required for -download)")
 flag.StringVar(&o.Source,"source","noaa","noaa or mock")
 flag.BoolVar(&o.Download,"download",false,"explicitly enable file creation and NOAA downloads")
 flag.Parse()
 if flag.NArg()!=0 {fmt.Fprintln(os.Stderr,"unexpected positional arguments");os.Exit(2)}
 _,err:=execute(context.Background(),o,noaaURL,&http.Client{Timeout:5*time.Second})
 if err!=nil{fmt.Fprintln(os.Stderr,err);os.Exit(1)}
}
