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
 "net"
 "net/http"
 "net/url"
 "os"
 "path/filepath"
 "strconv"
 "strings"
 "sync"
 "time"

 "pittsburg-saildata/internal/mockchart"
)

const noaaURL = "https://gis.charttools.noaa.gov/arcgis/rest/services/MCS/NOAAChartDisplay/MapServer/exts/MaritimeChartService/WMSServer"
const maxPNGBytes = 4 << 20
const markerFile = ".sailing-nautical-tile-manager.json"

type options struct {
 West, South, East, North float64
 MinZoom, MaxZoom, MaxTiles, Workers int
 MaxBytes int64
 Dir, Source string
 AllowLegacyNOAA bool // Internal maintenance only; never exposed as a CLI flag.
 DisableSCAMIN bool // NOAA only; isolated managed dataset with separate provenance.
 Download bool
 Timeout time.Duration
 Retries int
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
 if o.MinZoom<12 || o.MaxZoom>15 || o.MinZoom>o.MaxZoom {
  return p,errors.New("managed NOAA tile acquisition is restricted to native zooms 12..15")
 }
 if o.Workers<0||o.Workers>8{return p,errors.New("-workers must be within 1..8")}
 if o.MaxTiles<=0 {return p,errors.New("-max-tiles must be positive")}
 if o.MaxBytes<0 {return p,errors.New("-max-bytes must be nonnegative")}
 if o.Timeout<=0 {return p,errors.New("-timeout must be positive")}
 if o.Retries<0||o.Retries>5{return p,errors.New("-retries must be within 0..5")}
 if o.Source!="noaa" && o.Source!="mock" {return p,errors.New("-source must be noaa or mock")}
 if o.DisableSCAMIN && o.Source!="noaa" {return p,errors.New("-disable-scamin requires -source noaa")}
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
 // Original v1 marker retained as source/provenance identity across additions.\n // Caps are enforced cumulatively by the managed-directory inventory.
 if o.Source=="noaa" && o.DisableSCAMIN {return fmt.Sprintf("SailingGo-managed-XYZ-v3\nsource=noaa\nlayers=0,1,2,3,4,5,6,7\nHonorScamin=1\nbounds=%.8f,%.8f,%.8f,%.8f\nzoom=%d..%d\n",o.West,o.South,o.East,o.North,o.MinZoom,o.MaxZoom)}
 if o.Source=="noaa" {return fmt.Sprintf("SailingGo-managed-XYZ-v2\nsource=noaa\nlayers=0,1,2,3,4,5,6,7\nbounds=%.8f,%.8f,%.8f,%.8f\nzoom=%d..%d\n",o.West,o.South,o.East,o.North,o.MinZoom,o.MaxZoom)}
 return fmt.Sprintf("SailingGo-managed-XYZ-v1\nsource=%s\nbounds=%.8f,%.8f,%.8f,%.8f\nzoom=%d..%d\n",
  o.Source,o.West,o.South,o.East,o.North,o.MinZoom,o.MaxZoom)
}
func validPNG(b []byte)bool{
 if len(b)<8||len(b)>maxPNGBytes||!bytes.Equal(b[:8],[]byte{137,80,78,71,13,10,26,10}){return false}
 cfg,err:=png.DecodeConfig(bytes.NewReader(b));if err!=nil||cfg.Width!=256||cfg.Height!=256{return false}
 _,err=png.Decode(bytes.NewReader(b));return err==nil
}
func requestURL(base string,z,x,y int)(string,error){
 return requestURLWithLayers(base,z,x,y,"0,1,2,3,4,5,6,7")
}
func requestURLWithLayers(base string,z,x,y int,layers string)(string,error){
 u,err:=url.Parse(base);if err!=nil||u.Scheme==""||u.Host==""{return "",errors.New("invalid NOAA URL")}
 const half=20037508.342789244
 size:=2*half/math.Exp2(float64(z))
 left:= -half+float64(x)*size
 top:=half-float64(y)*size
 q:=u.Query()
 q.Set("SERVICE","WMS");q.Set("VERSION","1.3.0");q.Set("REQUEST","GetMap")
 q.Set("LAYERS",layers);q.Set("STYLES","")
 q.Set("CRS","EPSG:3857")
 q.Set("BBOX",fmt.Sprintf("%.6f,%.6f,%.6f,%.6f",left,top-size,left+size,top))
 q.Set("WIDTH","256");q.Set("HEIGHT","256");q.Set("FORMAT","image/png");q.Set("TRANSPARENT","TRUE")
 u.RawQuery=q.Encode()
 return u.String(),nil
}
func withSCAMINDisabled(target string)(string,error){
 u,err:=url.Parse(target);if err!=nil{return "",err}
 q:=u.Query();q.Set("Display_params",`{"ECDISParameters":{"DynamicParameters":{"Parameter":[{"name":"HonorScamin","value":1}]}}}`);u.RawQuery=q.Encode()
 return u.String(),nil
}
func getTile(ctx context.Context,o options,base string,client *http.Client,z,x,y int)([]byte,error){
 if o.Source=="mock" {return mockchart.Render(z,x,y)}
 target,err:=requestURL(base,z,x,y);if err!=nil{return nil,err}
 if o.DisableSCAMIN {target,err=withSCAMINDisabled(target);if err!=nil{return nil,err}}
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

// downloadWithRetry retries only transport timeouts and retryable upstream
// HTTP statuses. Invalid PNGs, authentication failures and malformed requests
// stop immediately so a bad request does not hammer NOAA repeatedly.
func downloadWithRetry(ctx context.Context,o options,base string,client *http.Client,z,x,y int)([]byte,error){
 for attempt:=0;attempt<=o.Retries;attempt++{
  b,err:=getTile(ctx,o,base,client,z,x,y)
  if err==nil{return b,nil}
  if ctx.Err()!=nil{return nil,ctx.Err()}
  if o.Source=="mock"{return nil,err}
  retryable:=errors.Is(err,context.DeadlineExceeded)
  var netErr net.Error
  if errors.As(err,&netErr)&&(netErr.Timeout()||netErr.Temporary()){retryable=true}
  // NOAA WMS may return transient 429/5xx responses during service load.
  msg:=err.Error()
  if strings.HasPrefix(msg,"NOAA HTTP 429")||strings.HasPrefix(msg,"NOAA HTTP 5"){retryable=true}
  if !retryable||attempt==o.Retries{
   return nil,fmt.Errorf("after %d attempt(s): %w",attempt+1,err)
  }
  delay:=time.Duration(1<<uint(attempt))*time.Second
  fmt.Fprintf(os.Stderr,"NOAA tile %d/%d/%d attempt %d/%d failed: %v; retrying in %s\\n",
   z,x,y,attempt+1,o.Retries+1,err,delay)
  select{
  case <-ctx.Done():return nil,ctx.Err()
  case <-time.After(delay):
  }
 }
 return nil,errors.New("unreachable retry state")
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
 dir,inv,err:=inspectManaged(o,p);if err!=nil{return t,err}
 o.print(p)
 if o.Dir!="" {fmt.Printf("Managed dataset: existing=%d tiles (%d PNG bytes), overlap=%d, new=%d, projected-total=%d / max-tiles=%d\n",inv.Count,inv.Bytes,inv.Overlap,inv.New,inv.Count+inv.New,o.MaxTiles)}
 if !o.Download{return t,nil}
 if err:=createManagedRoot(dir,o);err!=nil{return t,err}
 t.Bytes=inv.Bytes
 if o.MaxBytes>0&&t.Bytes>o.MaxBytes{return t,fmt.Errorf("existing cache exceeds -max-bytes=%d",o.MaxBytes)}
 workers:=o.Workers
 if workers==0{workers=1} // Backward-compatible default for direct callers.
 if client==nil{client=&http.Client{Timeout:o.Timeout}}
 ctx,cancel:=context.WithCancel(ctx);defer cancel()
 type tileJob struct{z,x,y int}
 type tileResult struct{job tileJob;b []byte;existing bool;err error}
 jobs:=make(chan tileJob)
 results:=make(chan tileResult,workers)
 var wg sync.WaitGroup
 for i:=0;i<workers;i++{
  wg.Add(1)
  go func(){
   defer wg.Done()
   for j:=range jobs{
    path:=filepath.Join(dir,strconv.Itoa(j.z),strconv.Itoa(j.x),strconv.Itoa(j.y)+".png")
    var result tileResult
    result.job=j
    if b,e:=os.ReadFile(path);e==nil{
     if !validPNG(b){result.err=fmt.Errorf("invalid existing tile, refusing overwrite: %s",path)}else{result.existing=true}
    }else if !os.IsNotExist(e){result.err=e
    }else{
     result.b,result.err=downloadWithRetry(ctx,o,base,client,j.z,j.x,j.y)
    }
    results<-result
   }
  }()
 }
 go func(){
  defer close(jobs)
  for _,row:=range p.Rows{
   for x:=row.X0;x<=row.X1;x++{for y:=row.Y0;y<=row.Y1;y++{
    select{case <-ctx.Done():return;case jobs<-tileJob{row.Z,x,y}:}
   }}
  }
 }()
 go func(){wg.Wait();close(results)}()
 started:=time.Now()
 var processed int64
 var firstErr error
 fmt.Printf("Acquiring %d planned tiles (new=%d, existing=%d); source=%s, SCAMIN disabled=%t, workers=%d\n",p.Total,inv.New,inv.Overlap,o.Source,o.DisableSCAMIN,workers)
 for r:=range results{
  if firstErr!=nil{continue} // Drain workers safely after cancellation.
  j:=r.job
  if r.err!=nil{firstErr=fmt.Errorf("tile %d/%d/%d: %w",j.z,j.x,j.y,r.err);cancel();continue}
  action:="existing"
  if r.existing{t.Skipped++}else{
   if !validPNG(r.b){firstErr=fmt.Errorf("tile %d/%d/%d: invalid PNG",j.z,j.x,j.y);cancel();continue}
   if o.MaxBytes>0&&t.Bytes+int64(len(r.b))>o.MaxBytes{
    firstErr=fmt.Errorf("next tile exceeds -max-bytes=%d; resume with larger budget only if intended",o.MaxBytes);cancel();continue
   }
   path:=filepath.Join(dir,strconv.Itoa(j.z),strconv.Itoa(j.x),strconv.Itoa(j.y)+".png")
   if err:=atomicPNG(path,r.b);err!=nil{firstErr=err;cancel();continue}
   t.Created++;t.Bytes+=int64(len(r.b))
   action=fmt.Sprintf("saved (%d bytes)",len(r.b))
  }
  processed++
  elapsed:=time.Since(started)
  eta:="calculating"
  if processed>0{eta=time.Duration(float64(elapsed)*float64(p.Total-processed)/float64(processed)).Round(time.Second).String()}
  fmt.Printf("[%d/%d] %.1f%% z%d/%d/%d %s | saved=%d skipped=%d | elapsed=%s ETA=%s\n",processed,p.Total,100*float64(processed)/float64(p.Total),j.z,j.x,j.y,action,t.Created,t.Skipped,elapsed.Round(time.Second),eta)
 }
 if firstErr!=nil{return t,firstErr}
 if err:=ctx.Err();err!=nil{return t,err}
 fmt.Printf("Saved %d new tiles, verified %d existing tiles, PNG bytes=%d (acquisition restricted to native z12..15)\n",t.Created,t.Skipped,t.Bytes)
 return t,nil
}
func main(){
 var o options
 var prune,applyPrune,compareLayers,zoomSamples,displayStudy bool
 var compareDir,zoomSamplesDir,displayStudyDir string
 flag.Float64Var(&o.West,"west",math.NaN(),"western longitude")
 flag.Float64Var(&o.South,"south",math.NaN(),"southern latitude")
 flag.Float64Var(&o.East,"east",math.NaN(),"eastern longitude")
 flag.Float64Var(&o.North,"north",math.NaN(),"northern latitude")
 flag.IntVar(&o.MinZoom,"min-zoom",13,"first native nautical zoom (12..15)")
 flag.IntVar(&o.MaxZoom,"max-zoom",14,"last native nautical zoom (12..15)")
 flag.IntVar(&o.Workers,"workers",4,"concurrent tile requests (1..8; default 4)")
 flag.IntVar(&o.MaxTiles,"max-tiles",0,"required hard ceiling on cumulative XYZ files in the managed directory")
 flag.Int64Var(&o.MaxBytes,"max-bytes",0,"optional total cached PNG bytes ceiling (0 = not configured)")
 flag.DurationVar(&o.Timeout,"timeout",30*time.Second,"NOAA request timeout for each attempt")
 flag.IntVar(&o.Retries,"retries",2,"extra attempts after a timeout or HTTP 429/5xx (0..5)")
 flag.StringVar(&o.Dir,"dir","","existing managed directory to extend or new managed directory (required for -download)")
 flag.StringVar(&o.Source,"source","noaa","noaa or mock")
 flag.BoolVar(&o.Download,"download",false,"explicitly enable file creation and NOAA downloads")
 flag.BoolVar(&o.DisableSCAMIN,"disable-scamin",false,"NOAA only: render all SCAMIN-filtered features; requires separate managed -dir")
 flag.BoolVar(&compareLayers,"compare-layers",false,"save a small separate NOAA WMS 0-12 vs 0-7 diagnostic sample; never touch managed cache")
 flag.StringVar(&compareDir,"compare-dir","","NEW output directory for WMS layer comparison PNG samples")
 flag.BoolVar(&zoomSamples,"zoom-samples",false,"fetch 12 isolated NOAA WMS z12..15 PNGs for visual study; never modify managed cache")
 flag.StringVar(&zoomSamplesDir,"zoom-samples-dir","","NEW output directory for zoom 12..15 study PNGs")
 flag.BoolVar(&displayStudy,"display-study",false,"save isolated NOAA default vs experimental SCAMIN comparison; never modify managed cache")
 flag.StringVar(&displayStudyDir,"display-study-dir","","NEW output directory for NOAA display study")
 flag.BoolVar(&prune,"prune",false,"plan removal of NOAA cached tiles outside native zoom 13..14 (dry-run default)")
 flag.BoolVar(&applyPrune,"apply-prune",false,"with -prune, explicitly delete zooms 9..12 and 15..16 from managed NOAA dataset")
 flag.Parse()
 if flag.NArg()!=0 {fmt.Fprintln(os.Stderr,"unexpected positional arguments");os.Exit(2)}
 if applyPrune&&!prune {fmt.Fprintln(os.Stderr,"-apply-prune requires -prune");os.Exit(2)}
 if displayStudy {
  if prune||applyPrune||compareLayers||zoomSamples||compareDir!=""||zoomSamplesDir!=""||o.Download||o.Dir!=""||o.Source!="noaa"||o.DisableSCAMIN {fmt.Fprintln(os.Stderr,"-display-study is isolated from acquisition and other diagnostics");os.Exit(2)}
  if err:=writeDisplayStudy(context.Background(),displayStudyDir,noaaURL,&http.Client{Timeout:o.Timeout},o.Timeout,o.Retries);err!=nil{fmt.Fprintln(os.Stderr,err);os.Exit(1)}
  return
 }
 if displayStudyDir!=""{fmt.Fprintln(os.Stderr,"-display-study-dir requires -display-study");os.Exit(2)}
 if zoomSamples {
  if prune||applyPrune||compareLayers||compareDir!=""||o.Download||o.Dir!=""||o.Source!="noaa"{
   fmt.Fprintln(os.Stderr,"-zoom-samples is isolated: cannot combine with -download, -prune, -compare-layers, -dir or -source mock");os.Exit(2)
  }
  if err:=writeZoomSamples(context.Background(),zoomSamplesDir,noaaURL,&http.Client{Timeout:o.Timeout},o.Timeout,o.Retries);err!=nil{
   fmt.Fprintln(os.Stderr,err);os.Exit(1)
  }
  return
 }
 if zoomSamplesDir!=""{fmt.Fprintln(os.Stderr,"-zoom-samples-dir requires -zoom-samples");os.Exit(2)}
 if compareLayers {
  if prune||applyPrune||o.Download||o.Dir!=""||o.Source!="noaa"{
   fmt.Fprintln(os.Stderr,"-compare-layers is isolated: cannot combine with -download, -prune, -dir or -source mock");os.Exit(2)
  }
  if err:=compareNOAALayers(context.Background(),compareDir,noaaURL,&http.Client{Timeout:o.Timeout},o.Timeout,o.Retries);err!=nil{
   fmt.Fprintln(os.Stderr,err);os.Exit(1)
  }
  return
 }
 if prune {
  if o.Download{fmt.Fprintln(os.Stderr,"-prune and -download are mutually exclusive");os.Exit(2)}
  if err:=pruneManaged(o,applyPrune);err!=nil{fmt.Fprintln(os.Stderr,err);os.Exit(1)}
  return
 }
 _,err:=execute(context.Background(),o,noaaURL,&http.Client{Timeout:o.Timeout})
 if err!=nil{fmt.Fprintln(os.Stderr,err);os.Exit(1)}
}
