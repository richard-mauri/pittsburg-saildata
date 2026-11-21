//go:build ignore

// Standalone, read-only NOAA regional catalog and downloaded MBTiles auditor.
// Usage: go run scripts/noaa_mbtiles_audit.go -catalog
//        go run scripts/noaa_mbtiles_audit.go -audit /path/to/chart.mbtiles
// The audit requires the sqlite3 command line utility; no Go dependency is added.
package main

import (
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "errors"
 "flag"
 "fmt"
 "io"
 "math"
 "net/http"
 "net/url"
 "os"
 "os/exec"
 "path/filepath"
 "strconv"
 "strings"
 "time"
)

const catalogURL = "https://encdirect.noaa.gov/arcgis/rest/services/MarineChart_Services/ncds_tilecache_metadata/MapServer/0/query"

type site struct { Name string; Lon, Lat float64 }
var sites = []site{
 {"Pittsburg", -121.885, 38.028},
 {"Delta", -121.590, 38.095},
 {"SF Bay", -122.365, 37.800},
 {"Golden Gate", -122.478, 37.819},
}

func xyzToTMS(lon, lat float64, z int) (int64,int64,int64,error) {
 if z < 0 || z > 22 || lon < -180 || lon > 180 || lat < -85.05112878 || lat > 85.05112878 {
  return 0,0,0,errors.New("invalid longitude, latitude or zoom")
 }
 n := int64(1) << z
 x := int64(math.Floor((lon+180)/360*float64(n)))
 y := int64(math.Floor((1-math.Asinh(math.Tan(lat*math.Pi/180))/math.Pi)/2*float64(n)))
 if x >= n {x=n-1}; if y>=n {y=n-1}
 if x < 0 || y < 0 {return 0,0,0,errors.New("invalid projected coordinate")}
 return x,y,n-1-y,nil
}

func detectTile(hexPrefix string) string {
 h:=strings.ToUpper(hexPrefix)
 switch {
 case strings.HasPrefix(h,"89504E470D0A1A0A"): return "png"
 case strings.HasPrefix(h,"FFD8FF"): return "jpeg"
 case strings.HasPrefix(h,"52494646") && len(h)>=24 && h[16:24]=="57454250": return "webp"
 case h=="": return "none"
 default: return "unknown"
 }
}

func queryCatalog() (map[string]any,error) {
 client:=&http.Client{Timeout:20*time.Second}
 result:=make(map[string]any)
 for _,s:=range sites {
  geometry,_:=json.Marshal(map[string]any{"x":s.Lon,"y":s.Lat,"spatialReference":map[string]int{"wkid":4326}})
  query:=url.Values{
   "geometry":{string(geometry)}, "geometryType":{"esriGeometryPoint"},
   "inSR":{"4326"}, "spatialRel":{"esriSpatialRelIntersects"},
   "where":{"1=1"}, "outFields":{"name,date_creat,size,baseline,ext_min_x,ext_max_x,ext_min_y,ext_max_y"},
   "returnGeometry":{"false"}, "f":{"json"},
  }
  req,err:=http.NewRequest(http.MethodGet,catalogURL+"?"+query.Encode(),nil); if err!=nil{return nil,err}
  req.Header.Set("User-Agent","SailingGo-NCDS-evaluation/1")
  resp,err:=client.Do(req); if err!=nil{return nil,fmt.Errorf("%s: %w",s.Name,err)}
  body,err:=io.ReadAll(io.LimitReader(resp.Body,2<<20));resp.Body.Close()
  if err!=nil{return nil,err}
  if resp.StatusCode!=200{return nil,fmt.Errorf("%s: NOAA HTTP %d",s.Name,resp.StatusCode)}
  var payload struct{ Features []struct{Attributes map[string]any `json:"attributes"`} `json:"features"`; Error any `json:"error"` }
  if err:=json.Unmarshal(body,&payload);err!=nil{return nil,err}
  if payload.Error!=nil{return nil,fmt.Errorf("%s: NOAA error: %v",s.Name,payload.Error)}
  attributes:=make([]map[string]any,0,len(payload.Features))
  for _,feature:=range payload.Features{attributes=append(attributes,feature.Attributes)}
  result[s.Name]=attributes
 }
 return result,nil
}

// sqlite3 -readonly is invoked on a fixed, local file. No SQL user input is accepted.
func sqlQuery(path,query string)(string,error){
 out,err:=exec.Command("sqlite3","-readonly","-separator","|",path,query).CombinedOutput()
 if err!=nil{return "",fmt.Errorf("sqlite3: %w: %s",err,strings.TrimSpace(string(out)))}
 return strings.TrimSpace(string(out)),nil
}

func auditMBTiles(path string)(map[string]any,error){
 path,err:=filepath.Abs(path);if err!=nil{return nil,err}
 file,err:=os.Open(path);if err!=nil{return nil,err};defer file.Close()
 info,err:=file.Stat();if err!=nil{return nil,err}
 if !info.Mode().IsRegular(){return nil,errors.New("chart path is not a regular file")}
 hash:=sha256.New()
 if _,err=io.Copy(hash,file);err!=nil{return nil,err}
 integrity,err:=sqlQuery(path,"PRAGMA integrity_check;");if err!=nil{return nil,err}
 if integrity!="ok"{return nil,fmt.Errorf("SQLite integrity failure: %s",integrity)}
 levels,err:=sqlQuery(path,"SELECT zoom_level,COUNT(*) FROM tiles GROUP BY zoom_level ORDER BY zoom_level;")
 if err!=nil{return nil,err}
 if levels==""{return nil,errors.New("no MBTiles rows")}
 metadata,err:=sqlQuery(path,"SELECT name,value FROM metadata ORDER BY name;");if err!=nil{return nil,err}
 result:=map[string]any{"file":filepath.Base(path),"bytes":info.Size(),"sha256":hex.EncodeToString(hash.Sum(nil)),
  "integrity":integrity,"metadata":metadata,"zoom_counts":levels}
 samples:=map[string]any{}
 for _,s:=range sites {
  siteSamples:=map[string]any{}
  for _,line:=range strings.Split(levels,"\n"){
   cols:=strings.Split(line,"|"); if len(cols)!=2{return nil,fmt.Errorf("invalid level row %q",line)}
   z,err:=strconv.Atoi(cols[0]);if err!=nil{return nil,err}
   x,_,y,err:=xyzToTMS(s.Lon,s.Lat,z);if err!=nil{return nil,err}
   sql:=fmt.Sprintf("SELECT length(tile_data),hex(substr(tile_data,1,12)) FROM tiles WHERE zoom_level=%d AND tile_column=%d AND tile_row=%d LIMIT 1;",z,x,y)
   sample,err:=sqlQuery(path,sql);if err!=nil{return nil,err}
   if sample==""{siteSamples[strconv.Itoa(z)]=map[string]any{"present":false};continue}
   parts:=strings.SplitN(sample,"|",2);if len(parts)!=2{return nil,fmt.Errorf("malformed sample %q",sample)}
   bytes,err:=strconv.ParseInt(parts[0],10,64);if err!=nil{return nil,err}
   siteSamples[strconv.Itoa(z)]=map[string]any{"present":true,"bytes":bytes,"format":detectTile(parts[1])}
  }
  samples[s.Name]=siteSamples
 }
 result["site_samples"]=samples
 return result,nil
}

// runCLI is intentionally interactive and diagnostic: network outages, missing files,
// and invalid arguments are reported without returning a nonzero process exit code.
// In an owner's shell configured with errexit, a failure must not close Terminal.
func runCLI(args []string, fetchCatalog func() (map[string]any, error), auditFile func(string) (map[string]any, error), stdout, stderr io.Writer) {
 flags := flag.NewFlagSet("noaa_mbtiles_audit", flag.ContinueOnError)
 flags.SetOutput(stderr)
 catalog := flags.Bool("catalog", false, "query official NOAA regional polygons for four sites")
 audit := flags.String("audit", "", "read-only audit of an existing local MBTiles file")
 if err := flags.Parse(args); err != nil {
  fmt.Fprintf(stderr, "Invalid arguments: %v. No chart data changed.\\n", err)
  return
 }
 if *catalog == (*audit != "") || flags.NArg() != 0 {
  fmt.Fprintln(stderr, "Specify exactly one of -catalog or -audit /path/chart.mbtiles. No chart data changed.")
  return
 }
 var result map[string]any
 var err error
 if *catalog {
  result, err = fetchCatalog()
 } else {
  result, err = auditFile(*audit)
 }
 if err != nil {
  fmt.Fprintf(stderr, "NOAA chart evaluation unavailable: %v\\nNo result verified; this diagnostic is non-fatal. Your terminal session should remain open.\\n", err)
  return
 }
 encoder := json.NewEncoder(stdout)
 encoder.SetIndent("", "  ")
 if err := encoder.Encode(result); err != nil {
  fmt.Fprintf(stderr, "Could not display audit result: %v\\n", err)
 }
}

func main() {
 runCLI(os.Args[1:], queryCatalog, auditMBTiles, os.Stdout, os.Stderr)
}
