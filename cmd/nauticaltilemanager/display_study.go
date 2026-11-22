package main

import (
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "image/png"
 "io"
 "net/http"
 "net/url"
 "os"
 "path/filepath"
 "strings"
 "time"
)

// writeDisplayStudy compares identical NOAA XYZ footprints under default
// rendering and an explicitly experimental SCAMIN override. It does not
// assume the public WMS honors vendor-specific display parameters.
func writeDisplayStudy(ctx context.Context,out,endpoint string,client *http.Client,timeout time.Duration,retries int) error {
 if strings.TrimSpace(out)=="" {return errors.New("-display-study-dir is required")}
 if timeout<=0||retries<0||retries>5 {return errors.New("invalid timeout/retries")}
 abs,err:=filepath.Abs(out);if err!=nil{return err}
 if abs==string(filepath.Separator){return errors.New("refusing filesystem root")}
 for parent:=filepath.Dir(abs);;parent=filepath.Dir(parent){
  if _,err:=os.Lstat(filepath.Join(parent,markerFile));err==nil{return errors.New("display study cannot be inside managed cache")}else if !os.IsNotExist(err){return err}
  if parent==filepath.Dir(parent){break}
 }
 if _,err:=os.Lstat(abs);err==nil{return errors.New("display study directory already exists")}else if !os.IsNotExist(err){return err}
 if client==nil {client=&http.Client{Timeout:timeout}}
 if err:=os.Mkdir(abs,0700);err!=nil{return err}
 // Same XYZ footprints at Pittsburg for every rendering variant.
 const layers="0,1,2,3,4,5,6,7"
 // This parameter is documented for Esri Maritime Chart Service but not
 // confirmed for NOAA's public WMS. Never interpret HTTP 200 as proof.
 override:=map[string]interface{}{"ECDISParameters":map[string]interface{}{
  "DynamicParameters":map[string]interface{}{"Parameter":[]map[string]interface{}{
   {"name":"HonorScamin","value":false},
  }},
 }}
 payload,err:=json.Marshal(override);if err!=nil{return err}
 for _,z:=range []int{13,14,15}{
  x,y:=xyz(-121.88,38.03,z)
  for _,variant:=range []struct{name,display string}{
   {"default",""},{"scamin-off",string(payload)},
  }{
   base,err:=requestURLWithLayers(endpoint,z,x,y,layers);if err!=nil{return err}
   u,err:=url.Parse(base);if err!=nil{return err}
   if variant.display!="" {q:=u.Query();q.Set("Display_params",variant.display);u.RawQuery=q.Encode()}
   // Preserve exact query in a separate manifest for reproducibility.
   filename:=fmt.Sprintf("pittsburg-z%d-%s.png",z,variant.name)
   meta:=fmt.Sprintf("GET %s\n",u.String())
   if err:=os.WriteFile(filepath.Join(abs,filename+".request.txt"),[]byte(meta),0600);err!=nil{return err}
   data,err:=fetchDisplayStudyPNG(ctx,client,u.String(),retries)
   if err!=nil{return fmt.Errorf("%s: %w (operational cache unchanged)",filename,err)}
   if err:=atomicPNG(filepath.Join(abs,filename),data);err!=nil{return err}
   fmt.Printf("  %s: %d bytes\n",filename,len(data))
  }
 }
 fmt.Printf("Display study complete: %s; compare images visually. A returned PNG does not establish override support.\n",abs)
 return nil
}

func fetchDisplayStudyPNG(ctx context.Context,client *http.Client,target string,retries int)([]byte,error){
 // Reuse the existing bounded retry/PNG validation behavior by routing
 // through a private HTTP client whose transport rewrites the requested URL.
 u,err:=url.Parse(target);if err!=nil{return nil,err}
 for attempt:=0;attempt<=retries;attempt++{
  req,err:=http.NewRequestWithContext(ctx,http.MethodGet,u.String(),nil);if err!=nil{return nil,err}
  req.Header.Set("Accept","image/png")
  resp,err:=client.Do(req)
  if err!=nil {
   if attempt==retries{return nil,err}
  }else{
   data,readErr:=readStudyResponse(resp)
   if readErr==nil{return data,nil}
   if resp.StatusCode!=429&&resp.StatusCode<500{return nil,readErr}
   if attempt==retries{return nil,readErr}
  }
  select{case <-ctx.Done():return nil,ctx.Err();case <-time.After(time.Duration(1<<uint(attempt))*time.Second):}
 }
 return nil,errors.New("exhausted attempts")
}

func readStudyResponse(resp *http.Response)([]byte,error){
 defer resp.Body.Close()
 if resp.StatusCode!=200{return nil,fmt.Errorf("NOAA HTTP %d",resp.StatusCode)}
 data,err:=io.ReadAll(io.LimitReader(resp.Body,maxPNGBytes+1));if err!=nil{return nil,err}
 if !validPNG(data){return nil,errors.New("non-256px or invalid PNG")}
 if _,err:=png.DecodeConfig(bytes.NewReader(data));err!=nil{return nil,err}
 return data,nil
}
