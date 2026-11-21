package main

import (
 "bytes"
 "context"
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
 "sync"
 "time"
)

const nauticalTilePrefix = "/api/charts/tiles/"
const nauticalWMSURL = "https://gis.charttools.noaa.gov/arcgis/rest/services/MCS/NOAAChartDisplay/MapServer/exts/MaritimeChartService/WMSServer"

const nauticalMaxTileBytes = 4 << 20

// nauticalTileHandler is the same-origin, disk-first chart gateway. Browser
// code never calls NOAA. The NOAA endpoint is pinned in production.
func nauticalTileHandler(directory string) http.Handler {
 return nauticalTileHandlerWithUpstream(directory, nauticalWMSURL, &http.Client{
  Timeout: 5*time.Second,
  Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, MaxConnsPerHost: 4,
   MaxIdleConnsPerHost: 4, MaxIdleConns: 4, IdleConnTimeout: 30*time.Second,
   ResponseHeaderTimeout: 4*time.Second},
 })
}

// The upstream/client arguments make isolated httptest servers possible; the
// production mux always uses the fixed NOAA address above.
func nauticalTileHandlerWithUpstream(directory, upstream string, client *http.Client) http.Handler {
 root:=strings.TrimSpace(directory)
 if client==nil {client=&http.Client{Timeout:5*time.Second}}
 type tileFlight struct {done chan struct{}; data []byte; err error}
 var mu sync.Mutex
 flights:=map[string]*tileFlight{}
 slots:=make(chan struct{},4)
 var retryAfter time.Time

 return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  w.Header().Set("X-Content-Type-Options","nosniff")
  w.Header().Set("Cache-Control","no-store")
  if r.Method!=http.MethodGet && r.Method!=http.MethodHead {
   w.Header().Set("Allow","GET, HEAD")
   http.Error(w,"method not allowed",http.StatusMethodNotAllowed);return
  }
  z,x,y,ok:=parseNauticalTilePath(r.URL.Path)
  if !ok {http.Error(w,"invalid nautical tile coordinates",http.StatusBadRequest);return}
  if root=="" {w.Header().Set("Retry-After","60");http.Error(w,"nautical chart cache unavailable",http.StatusServiceUnavailable);return}
  filename:=filepath.Join(root,strconv.Itoa(z),strconv.Itoa(x),fmt.Sprintf("%d.png",y))
  data,err:=os.ReadFile(filename)
  if err==nil && !validNauticalPNG(data) {
   // Do not serve stale, corrupted or HTML-as-PNG files; do not overwrite
   // an existing operator file automatically.
   http.Error(w,"invalid cached nautical tile",http.StatusServiceUnavailable);return
  }
  if err!=nil && !os.IsNotExist(err) {
   http.Error(w,"nautical chart cache unreadable",http.StatusServiceUnavailable);return
  }
  if err==nil {writeNauticalTile(w,r,data);return}

  // Deduplicate simultaneous cache misses for this same tile. Waiters never
  // launch an additional NOAA request and can abandon waiting on disconnect.
  key:=fmt.Sprintf("%d/%d/%d",z,x,y)
  mu.Lock()
  if time.Now().Before(retryAfter) {
   mu.Unlock()
   w.Header().Set("Retry-After","30")
   http.Error(w,"NOAA nautical charts temporarily unavailable",http.StatusServiceUnavailable);return
  }
  if existing:=flights[key]; existing!=nil {
   mu.Unlock()
   select {
   case <-r.Context().Done(): return
   case <-existing.done:
   }
   if existing.err!=nil {
    http.Error(w,"NOAA nautical charts temporarily unavailable",http.StatusServiceUnavailable);return
   }
   writeNauticalTile(w,r,existing.data);return
  }
  flight:=&tileFlight{done:make(chan struct{})}
  flights[key]=flight
  mu.Unlock()

  // A timeout is owned by Go, independent of any Safari request timer.
  // This avoids a hung first browser request holding a cache-flight forever.
  ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second)
  defer cancel()
  select {
  case slots<-struct{}{}:
   flight.data,flight.err=fetchNauticalWMSTile(ctx,client,upstream,z,x,y)
   <-slots
  case <-ctx.Done():
   flight.err=ctx.Err()
  }
  if flight.err==nil {
   flight.err=saveNauticalTile(filename,flight.data)
   // A successfully fetched tile remains useful for this request even if
   // the disk is not writable; however, do not imply it is cached.
   if flight.err!=nil {
    // Treat cache write failures as explicit errors, not silent memory-only success.
    flight.data=nil
   }
  }
  mu.Lock()
  if flight.err!=nil {retryAfter=time.Now().Add(30*time.Second)}
  delete(flights,key)
  close(flight.done)
  mu.Unlock()
  if flight.err!=nil {
   w.Header().Set("Retry-After","30")
   http.Error(w,"NOAA nautical charts temporarily unavailable",http.StatusServiceUnavailable);return
  }
  writeNauticalTile(w,r,flight.data)
 })
}

func writeNauticalTile(w http.ResponseWriter,r *http.Request,data []byte){
 w.Header().Set("Content-Type","image/png")
 w.Header().Set("Cache-Control","private, max-age=3600")
 w.Header().Set("Content-Length",strconv.Itoa(len(data)))
 if r.Method==http.MethodGet {_,_=w.Write(data)}
}

func validNauticalPNG(data []byte) bool {
 if len(data)<8 || len(data)>nauticalMaxTileBytes ||
  !bytes.Equal(data[:8],[]byte{137,80,78,71,13,10,26,10}) {return false}
 config,err:=png.DecodeConfig(bytes.NewReader(data))
 return err==nil && config.Width==256 && config.Height==256
}

func saveNauticalTile(filename string,data []byte) error {
 if !validNauticalPNG(data) {return fmt.Errorf("invalid NOAA tile PNG")}
 dir:=filepath.Dir(filename)
 if err:=os.MkdirAll(dir,0700);err!=nil{return err}
 tmp,err:=os.CreateTemp(dir,".nautical-*.tmp")
 if err!=nil{return err}
 name:=tmp.Name()
 defer os.Remove(name)
 if err:=tmp.Chmod(0600);err!=nil{tmp.Close();return err}
 if _,err=tmp.Write(data);err!=nil{tmp.Close();return err}
 if err=tmp.Close();err!=nil{return err}
 return os.Rename(name,filename)
}

// NOAA NCDS WMS 1.3.0 GetMap uses EPSG:3857, matching Leaflet XYZ tile
// extents. TMS y-axis reversal is NOT used for this online PNG cache.
func nauticalWMSRequestURL(base string,z,x,y int) (string,error) {
 u,err:=url.Parse(base)
 if err!=nil || u.Scheme=="" || u.Host=="" {return "",fmt.Errorf("invalid NOAA WMS URL")}
 const earthHalf=20037508.342789244
 size:=2*earthHalf/math.Exp2(float64(z))
 left:=-earthHalf+float64(x)*size
 right:=left+size
 top:=earthHalf-float64(y)*size
 bottom:=top-size
 q:=u.Query()
 q.Set("SERVICE","WMS")
 q.Set("VERSION","1.3.0")
 q.Set("REQUEST","GetMap")
 q.Set("LAYERS","0,1,2,3,4,5,6,7,8,9,10,11,12")
 q.Set("STYLES","")
 q.Set("CRS","EPSG:3857")
 q.Set("BBOX",fmt.Sprintf("%.6f,%.6f,%.6f,%.6f",left,bottom,right,top))
 q.Set("WIDTH","256")
 q.Set("HEIGHT","256")
 q.Set("FORMAT","image/png")
 q.Set("TRANSPARENT","TRUE")
 u.RawQuery=q.Encode()
 return u.String(),nil
}

func fetchNauticalWMSTile(ctx context.Context,client *http.Client,base string,z,x,y int)([]byte,error){
 target,err:=nauticalWMSRequestURL(base,z,x,y)
 if err!=nil{return nil,err}
 req,err:=http.NewRequestWithContext(ctx,http.MethodGet,target,nil)
 if err!=nil{return nil,err}
 req.Header.Set("Accept","image/png")
 req.Header.Set("User-Agent","SailingGo nautical tile cache")
 resp,err:=client.Do(req)
 if err!=nil{return nil,err}
 defer resp.Body.Close()
 if resp.StatusCode!=http.StatusOK {return nil,fmt.Errorf("NOAA status %d",resp.StatusCode)}
 data,err:=io.ReadAll(io.LimitReader(resp.Body,nauticalMaxTileBytes+1))
 if err!=nil{return nil,err}
 if !validNauticalPNG(data) {return nil,fmt.Errorf("NOAA returned non-tile or invalid PNG")}
 return data,nil
}

func parseNauticalTilePath(path string) (z, x, y int, ok bool) {
 if !strings.HasPrefix(path, nauticalTilePrefix) || !strings.HasSuffix(path, ".png") {
  return 0, 0, 0, false
 }
 parts := strings.Split(strings.TrimSuffix(strings.TrimPrefix(path, nauticalTilePrefix), ".png"), "/")
 if len(parts) != 3 {return 0,0,0,false}
 values := [3]int{}
 for i, part := range parts {
  if part == "" || strings.Trim(part, "0123456789") != "" || len(part) > 9 {return 0,0,0,false}
  n, err := strconv.Atoi(part)
  if err != nil {return 0,0,0,false}
  values[i] = n
 }
 z, x, y = values[0], values[1], values[2]
 if z < 0 || z > 18 || x >= 1<<uint(z) || y >= 1<<uint(z) {
  return 0, 0, 0, false
 }
 return z, x, y, true
}
