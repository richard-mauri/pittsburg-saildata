package main

import (
 "bytes"
 "fmt"
 "image/png"
 "net/http"
 "os"
 "path/filepath"
 "strconv"
 "strings"
)

const nauticalTilePrefix = "/api/charts/tiles/"
const nauticalMaxTileBytes = 4 << 20

// Nautical is a managed, read-only XYZ dataset at runtime. Only the separate
// cmd/nauticaltilemanager may download or generate files. The application
// never downloads NOAA data, generates mock imagery, or writes chart tiles.
func defaultNauticalCacheDir() string {
 if custom:=strings.TrimSpace(os.Getenv("SAILING_NAUTICAL_TILE_DIR"));custom!=""{return custom}
 base,err:=os.UserCacheDir()
 if err!=nil{return ""}
 return filepath.Join(base,"sailing-go","nautical-tiles")
}
func nauticalTileDiagnostics(w http.ResponseWriter,source,result string) {
 w.Header().Set("X-Sailing-Chart-Source",source)
 w.Header().Set("X-Sailing-Chart-Result",result)
}
func nauticalTileHandler(directory string) http.Handler {
 return nauticalTileHandlerWithUpstream(directory,"",nil)
}
// The historical helper signature stays available for legacy integration
// tests. Its upstream/client arguments are intentionally ignored: an HTTP
// request can never initiate a NOAA download or a disk write.
func nauticalTileHandlerWithUpstream(directory,_ string,_ *http.Client) http.Handler {
 root:=strings.TrimSpace(directory)
 return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  w.Header().Set("X-Content-Type-Options","nosniff")
  w.Header().Set("Cache-Control","no-store")
  if r.Method!=http.MethodGet&&r.Method!=http.MethodHead {
   w.Header().Set("Allow","GET, HEAD")
   nauticalTileDiagnostics(w,"unavailable","invalid-method")
   http.Error(w,"method not allowed",http.StatusMethodNotAllowed);return
  }
  z,x,y,ok:=parseNauticalTilePath(r.URL.Path)
  if !ok {
   nauticalTileDiagnostics(w,"unavailable","invalid-coordinates")
   http.Error(w,"invalid nautical tile coordinates",http.StatusBadRequest);return
  }
  if root=="" {
   nauticalTileDiagnostics(w,"unavailable","cache-unavailable")
   http.Error(w,"nautical chart directory not configured",http.StatusServiceUnavailable);return
  }
  path:=filepath.Join(root,strconv.Itoa(z),strconv.Itoa(x),fmt.Sprintf("%d.png",y))
  info,err:=os.Lstat(path)
  if os.IsNotExist(err) {
   // Leaflet leaves this cell transparent, revealing the Street underlay.
   // Keep Nautical selected; never fetch or create missing tiles here.
   nauticalTileDiagnostics(w,"unavailable","tile-not-cached")
   http.Error(w,"nautical tile not cached",http.StatusNotFound);return
  }
  if err!=nil {
   nauticalTileDiagnostics(w,"unavailable","cache-read-error")
   http.Error(w,"nautical chart directory unreadable",http.StatusServiceUnavailable);return
  }
  if !info.Mode().IsRegular() {
   nauticalTileDiagnostics(w,"unavailable","cache-invalid")
   http.Error(w,"invalid cached nautical tile",http.StatusServiceUnavailable);return
  }
  data,err:=os.ReadFile(path)
  if err!=nil {
   nauticalTileDiagnostics(w,"unavailable","cache-read-error")
   http.Error(w,"nautical chart cache unreadable",http.StatusServiceUnavailable);return
  }
  if !validNauticalPNG(data) {
   nauticalTileDiagnostics(w,"unavailable","cache-invalid")
   http.Error(w,"invalid cached nautical tile",http.StatusServiceUnavailable);return
  }
  nauticalTileDiagnostics(w,"cache","cache-hit")
  writeNauticalTile(w,r,data)
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
 if err!=nil || config.Width!=256 || config.Height!=256 {return false}
 _,err=png.Decode(bytes.NewReader(data))
 return err==nil
}
func parseNauticalTilePath(path string) (z,x,y int,ok bool){
 if !strings.HasPrefix(path,nauticalTilePrefix)||!strings.HasSuffix(path,".png"){return 0,0,0,false}
 parts:=strings.Split(strings.TrimSuffix(strings.TrimPrefix(path,nauticalTilePrefix),".png"),"/")
 if len(parts)!=3{return 0,0,0,false}
 values:=[3]int{}
 for i,part:=range parts {
  if part==""||strings.Trim(part,"0123456789")!=""||len(part)>9{return 0,0,0,false}
  n,err:=strconv.Atoi(part);if err!=nil{return 0,0,0,false}
  values[i]=n
 }
 z,x,y=values[0],values[1],values[2]
 if z<9||z>16||x>=1<<uint(z)||y>=1<<uint(z){return 0,0,0,false}
 return z,x,y,true
}
