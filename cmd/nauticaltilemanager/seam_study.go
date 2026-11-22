package main

import (
 "bytes"
 "context"
 "fmt"
 "image"
 "image/draw"
 "image/png"
 "io"
 "math"
 "net/http"
 "net/url"
 "os"
 "path/filepath"
 "strings"
)

// writeSeamStudy compares four independently rendered 256px WMS requests
// with one 512px WMS request covering precisely the same geographic box.
// It never reads or writes the operational or managed tile caches.
func writeSeamStudy(ctx context.Context, out, endpoint string, client *http.Client, lon, lat float64) error {
 if strings.TrimSpace(out)=="" {return fmt.Errorf("-seam-study-dir required")}
 if math.IsNaN(lon)||math.IsNaN(lat)||lon < -180||lon > 180||lat < -85||lat > 85 {return fmt.Errorf("invalid diagnostic coordinate")}
 abs,err:=filepath.Abs(out);if err!=nil{return err}
 if abs==string(filepath.Separator){return fmt.Errorf("refusing filesystem root")}
 for p:=filepath.Dir(abs);;p=filepath.Dir(p){
  if _,e:=os.Lstat(filepath.Join(p,markerFile));e==nil{return fmt.Errorf("cannot write inside managed cache")}else if !os.IsNotExist(e){return e}
  if p==filepath.Dir(p){break}
 }
 if _,e:=os.Lstat(abs);e==nil{return fmt.Errorf("output already exists: %s",abs)}else if !os.IsNotExist(e){return e}
 if client==nil {client=&http.Client{}}
 if err:=os.Mkdir(abs,0700);err!=nil{return err}
 const z=15
 x,y:=xyz(lon,lat,z)
 // Center the 2x2 footprint on the selected XYZ cell and its right/bottom neighbors.
 baseX,baseY:=x,y
 mosaic:=image.NewRGBA(image.Rect(0,0,512,512))
 var manifest strings.Builder
 fmt.Fprintf(&manifest,"center_lon=%.8f center_lat=%.8f\nzoom=15\norigin_xyz=%d/%d/%d\n",lon,lat,z,baseX,baseY)
 for dy:=0;dy<2;dy++{for dx:=0;dx<2;dx++{
  tx,ty:=baseX+dx,baseY+dy
  target,e:=requestURL(endpoint,z,tx,ty);if e!=nil{return e}
  target,e=withSCAMINDisabled(target);if e!=nil{return e}
  fmt.Fprintf(&manifest,"tile_%d_%d=%s\n",dx,dy,target)
  b,e:=seamFetch(ctx,client,target,256);if e!=nil{return fmt.Errorf("tile %d/%d/%d: %w",z,tx,ty,e)}
  name:=fmt.Sprintf("tile-%d-%d-%d.png",z,tx,ty)
  if e=os.WriteFile(filepath.Join(abs,name),b,0600);e!=nil{return e}
  im,e:=png.Decode(bytes.NewReader(b));if e!=nil{return e}
  draw.Draw(mosaic,image.Rect(dx*256,dy*256,(dx+1)*256,(dy+1)*256),im,image.Point{},draw.Src)
 }}
 var buf bytes.Buffer
 if err:=png.Encode(&buf,mosaic);err!=nil{return err}
 if err:=os.WriteFile(filepath.Join(abs,"mosaic-4x256.png"),buf.Bytes(),0600);err!=nil{return err}
 // WMS 1.3 EPSG:3857 box covering exactly 2x2 native XYZ tiles.
 const half=20037508.342789244
 size:=2*half/math.Exp2(z)
 left:=-half+float64(baseX)*size
 top:=half-float64(baseY)*size
 target,err:=requestURL(endpoint,z,baseX,baseY);if err!=nil{return err}
 u,err:=url.Parse(target);if err!=nil{return err}
 q:=u.Query()
 q.Set("BBOX",fmt.Sprintf("%.6f,%.6f,%.6f,%.6f",left,top-2*size,left+2*size,top))
 q.Set("WIDTH","512");q.Set("HEIGHT","512")
 u.RawQuery=q.Encode()
 target,err=withSCAMINDisabled(u.String());if err!=nil{return err}
 fmt.Fprintf(&manifest,"single_512=%s\n",target)
 b,err:=seamFetch(ctx,client,target,512);if err!=nil{return fmt.Errorf("single 512 WMS request: %w",err)}
 if err:=os.WriteFile(filepath.Join(abs,"single-512.png"),b,0600);err!=nil{return err}
 if err:=os.WriteFile(filepath.Join(abs,"requests.txt"),[]byte(manifest.String()),0600);err!=nil{return err}
 fmt.Printf("Seam study complete: %s\nCompare mosaic-4x256.png with single-512.png at identical extent; do not infer cause without inspection.\n",abs)
 return nil
}

func seamFetch(ctx context.Context,client *http.Client,target string,width int)([]byte,error){
 req,err:=http.NewRequestWithContext(ctx,http.MethodGet,target,nil);if err!=nil{return nil,err}
 req.Header.Set("Accept","image/png")
 resp,err:=client.Do(req);if err!=nil{return nil,err}
 defer resp.Body.Close()
 b,err:=io.ReadAll(io.LimitReader(resp.Body,maxPNGBytes+1));if err!=nil{return nil,err}
 if resp.StatusCode!=200{return nil,fmt.Errorf("HTTP %d",resp.StatusCode)}
 if len(b)>maxPNGBytes{return nil,fmt.Errorf("PNG exceeds byte limit")}
 cfg,err:=png.DecodeConfig(bytes.NewReader(b));if err!=nil{return nil,fmt.Errorf("invalid PNG: %w",err)}
 if cfg.Width!=width||cfg.Height!=width{return nil,fmt.Errorf("wrong PNG dimensions %dx%d",cfg.Width,cfg.Height)}
 if _,err=png.Decode(bytes.NewReader(b));err!=nil{return nil,err}
 return b,nil
}
