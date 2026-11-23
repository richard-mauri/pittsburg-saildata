// Offline chart API proof of concept. Standalone, loopback-only, read-only.
// Run: go run ./cmd/offlinechartpoc -root /path/to/isolated/chart-copy
package main

import (
 "encoding/json"
 "flag"
 "fmt"
 "log"
 "math"
 "net"
 "net/http"
 "os"
 "path/filepath"
 "strconv"
 "strings"
 "time"
)

const maxTiles = 256
type tile struct { Z int `json:"z"`; X int `json:"x"`; Y int `json:"y"`; Bytes int64 `json:"bytes"`; URL string `json:"url"` }
type manifest struct { Zoom int `json:"zoom"`; Bounds [4]float64 `json:"bounds"`; Tiles []tile `json:"tiles"`; TotalBytes int64 `json:"totalBytes"`; Missing int `json:"missing"`; Complete bool `json:"complete"` }
func mercX(lon float64,z int) int { n:=math.Exp2(float64(z));return int(math.Floor((lon+180)/360*n)) }
func mercY(lat float64,z int) int { lat=math.Max(-85.05112878,math.Min(85.05112878,lat));r:=lat*math.Pi/180;n:=math.Exp2(float64(z));return int(math.Floor((1-math.Log(math.Tan(r)+1/math.Cos(r))/math.Pi)/2*n)) }
func plan(root string,z int,b [4]float64)(manifest,error){
 m:=manifest{Zoom:z,Bounds:b,Tiles:[]tile{}}
 if z<13||z>15||!(b[0]>=-180&&b[2]<=180&&b[1]>=-85&&b[3]<=85&&b[0]<b[2]&&b[1]<b[3]) {return m,fmt.Errorf("invalid bounds or zoom")}
 x0,x1:=mercX(b[0],z),mercX(math.Nextafter(b[2],math.Inf(-1)),z)
 y0,y1:=mercY(math.Nextafter(b[3],math.Inf(-1)),z),mercY(b[1],z)
 if x1<x0||y1<y0||int64(x1-x0+1)*int64(y1-y0+1)>maxTiles{return m,fmt.Errorf("selection exceeds %d tiles",maxTiles)}
 for x:=x0;x<=x1;x++ {for y:=y0;y<=y1;y++ {
  path:=filepath.Join(root,strconv.Itoa(z),strconv.Itoa(x),fmt.Sprintf("%d.png",y))
  st,err:=os.Lstat(path)
  if err!=nil||!st.Mode().IsRegular()||st.Size()<8||st.Size()>4<<20 {m.Missing++;continue}
  // Check the PNG signature without reading the whole tile.
  f,err:=os.Open(path);if err!=nil {m.Missing++;continue}
  var sig [8]byte;n,e:=f.Read(sig[:]);f.Close()
  if e!=nil||n!=8||string(sig[:])!="\x89PNG\r\n\x1a\n" {m.Missing++;continue}
  m.Tiles=append(m.Tiles,tile{z,x,y,st.Size(),fmt.Sprintf("/tiles/%d/%d/%d.png",z,x,y)})
  m.TotalBytes+=st.Size()
 }}
 m.Complete=m.Missing==0
 return m,nil
}
func handler(root string)http.Handler {
 mux:=http.NewServeMux()
 mux.HandleFunc("/api/offline-charts/manifest",func(w http.ResponseWriter,r *http.Request){
  w.Header().Set("Cache-Control","no-store");w.Header().Set("Content-Type","application/json")
  if r.Method!="GET"{http.Error(w,"GET only",405);return}
  q:=r.URL.Query();z,e:=strconv.Atoi(q.Get("z"));if e!=nil {http.Error(w,"invalid zoom",400);return}
  var b [4]float64
  for i,k:=range []string{"west","south","east","north"} {v,e:=strconv.ParseFloat(q.Get(k),64);if e!=nil||math.IsNaN(v)||math.IsInf(v,0){http.Error(w,"invalid "+k,400);return};b[i]=v}
  m,e:=plan(root,z,b);if e!=nil {http.Error(w,e.Error(),400);return}
  json.NewEncoder(w).Encode(m)
 })
 mux.HandleFunc("/tiles/",func(w http.ResponseWriter,r *http.Request){
  if r.Method!="GET"&&r.Method!="HEAD"{http.Error(w,"method not allowed",405);return}
  var z,x,y int;var tail string
  n,e:=fmt.Sscanf(strings.TrimPrefix(r.URL.Path,"/tiles/"),"%d/%d/%d.png%s",&z,&x,&y,&tail)
  // Sscanf returns 3 values and EOF on a correctly formed filename.
  if n!=3||(e!=nil&&e.Error()!="EOF")||tail!=""||z<13||z>15||x<0||y<0||x>=1<<z||y>=1<<z{http.Error(w,"invalid tile",400);return}
  p:=filepath.Join(root,strconv.Itoa(z),strconv.Itoa(x),fmt.Sprintf("%d.png",y))
  st,e:=os.Lstat(p);if e!=nil||!st.Mode().IsRegular()||st.Size()>4<<20{http.NotFound(w,r);return}
  w.Header().Set("Cache-Control","no-store");w.Header().Set("X-Content-Type-Options","nosniff")
  http.ServeFile(w,r,p)
 })
 return mux
}
func main(){
 root:=flag.String("root","","isolated read-only XYZ PNG root (not the live cache)")
 listen:=flag.String("listen","127.0.0.1:8099","loopback listen address")
 flag.Parse()
 if *root==""{log.Fatal("explicit -root required")}
 host,_,err:=net.SplitHostPort(*listen);if err!=nil||host!="127.0.0.1"{log.Fatal("POC must bind 127.0.0.1 only")}
 st,err:=os.Stat(*root);if err!=nil||!st.IsDir(){log.Fatal("root must be an existing directory")}
 s:=&http.Server{Addr:*listen,Handler:handler(*root),ReadHeaderTimeout:5*time.Second}
 log.Printf("POC manifest at http://%s/api/offline-charts/manifest (not public HTTPS)",*listen)
 log.Fatal(s.ListenAndServe())
}
