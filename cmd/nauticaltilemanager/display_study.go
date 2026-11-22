package main

import (
 "context"
 "encoding/json"
 "encoding/hex"
 "errors"
 "fmt"
 "io"
 "net/http"
 "net/url"
 "unicode/utf8"
 "os"
 "path/filepath"
 "strings"
 "time"
)

// writeDisplayStudy compares identical NOAA XYZ footprints under default
// rendering and an explicitly experimental integer SCAMIN override. It does not
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
 // NOAA validates HonorScamin as integer 1 or 2. Esri documentation
 // identifies 1 as disabling SCAMIN; compare images before concluding effect.
 override:=map[string]interface{}{"ECDISParameters":map[string]interface{}{
  "DynamicParameters":map[string]interface{}{"Parameter":[]map[string]interface{}{
   {"name":"HonorScamin","value":1},
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
   data,status,ctype,err:=fetchDisplayStudyPNG(ctx,client,u.String(),retries)
   result:=fmt.Sprintf("HTTP status: %d\nContent-Type: %s\n",status,ctype)
   if err!=nil {
    result+=fmt.Sprintf("Result: %v\n",err)
    if len(data)>0 {if utf8.Valid(data){result+="Response body (bounded):\n"+string(data)+"\n"}else{result+="Response hex prefix: "+hex.EncodeToString(data[:minInt(len(data),256)])+"\n"}}
    if writeErr:=os.WriteFile(filepath.Join(abs,filename+".response.txt"),[]byte(result),0600);writeErr!=nil{return writeErr}
    fmt.Printf("  %s: rejected (HTTP %d, %s); details in .response.txt; continuing\n",filename,status,ctype)
    continue
   }
   if err:=os.WriteFile(filepath.Join(abs,filename+".response.txt"),[]byte(result+"Result: valid PNG\n"),0600);err!=nil{return err}
   if err:=atomicPNG(filepath.Join(abs,filename),data);err!=nil{return err}
   fmt.Printf("  %s: %d bytes\n",filename,len(data))
  }
 }
 fmt.Printf("Display study complete: %s; compare images visually. A returned PNG does not establish override support.\n",abs)
 return nil
}

func minInt(a,b int)int{if a<b{return a};return b}

// fetchDisplayStudyPNG returns bounded response bodies even for WMS exception
// documents, so failures are inspectable without ever writing an invalid PNG.
func fetchDisplayStudyPNG(ctx context.Context,client *http.Client,target string,retries int)([]byte,int,string,error){
 for attempt:=0;attempt<=retries;attempt++{
  req,err:=http.NewRequestWithContext(ctx,http.MethodGet,target,nil);if err!=nil{return nil,0,"",err}
  req.Header.Set("Accept","image/png")
  resp,err:=client.Do(req)
  if err!=nil{
   if ctx.Err()!=nil{return nil,0,"",ctx.Err()}
   if attempt==retries{return nil,0,"",err}
  }else{
   data,readErr:=io.ReadAll(io.LimitReader(resp.Body,maxPNGBytes+1))
   resp.Body.Close()
   status,ctype:=resp.StatusCode,resp.Header.Get("Content-Type")
   if readErr!=nil{return nil,status,ctype,readErr}
   if status==http.StatusOK&&validPNG(data){return data,status,ctype,nil}
   failure:=fmt.Errorf("HTTP %d: response is not a valid 256x256 PNG",status)
   if status!=429&&status<500{return data,status,ctype,failure}
   if attempt==retries{return data,status,ctype,failure}
  }
  select{case <-ctx.Done():return nil,0,"",ctx.Err();case <-time.After(time.Duration(1<<uint(attempt))*time.Second):}
 }
 return nil,0,"",errors.New("exhausted attempts")
}
