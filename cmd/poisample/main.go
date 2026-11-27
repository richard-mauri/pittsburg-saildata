// poisample is an isolated source evaluation utility, not a production installer.
package main

import (
	"bufio"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

const sourceFile = "norcal-261126.osm.pbf"
const sourceURL = "https://download.geofabrik.de/north-america/us/california/norcal/" + sourceFile
const capBytes int64 = 1000000000
const maxInput int64 = 128 << 20
type bbox struct { W,S,E,N float64 }
var samples=map[string]bbox{"delta":{-121.94,38,-121.84,38.06},"sf":{-122.43,37.77,-122.39,37.81}}
type feature struct {
	Type string
	Geometry struct { Type string; Coordinates json.RawMessage }
	Properties map[string]json.RawMessage
}
type row struct { ID,Category,Name string; Lat,Lon float64 }
type report struct {
	Sample,Source,Note string
	Bounds bbox
	Features,Rows int
	RawCategories,AcceptedCategories,Rejected,Geometry map[string]int
}
func tag(f feature,k string)string{
	var s string
	_ = json.Unmarshal(f.Properties[k],&s)
	return s
}
func categories(f feature)[]string{
	a,shop,tourism,water,seamark:=tag(f,"amenity"),tag(f,"shop"),tag(f,"tourism"),tag(f,"waterway"),tag(f,"seamark:type")
	var out []string
	add:=func(ok bool,k string){if ok{out=append(out,k)}}
	marine:=water=="fuel"||seamark=="fuel_station"
	add(marine,"marine_fuel")
	add(water=="sanitary_dump_station"||seamark=="sanitary_dump_station","pumpout")
	add(tag(f,"leisure")=="slipway","boat_ramp")
	add(tag(f,"leisure")=="marina","marina")
	add(a=="fuel"&&!marine,"fuel")
	add(a=="restaurant","restaurant")
	add(a=="cafe","coffee")
	add(shop=="supermarket"||shop=="grocery","grocery")
	add(tourism=="hotel"||tourism=="motel"||tourism=="guest_house"||tourism=="hostel","lodging")
	add(a=="charging_station","ev_charging")
	add(a=="parking","parking")
	return out
}
func validName(s string)bool{
	if s==""||!utf8.ValidString(s)||len(utf16.Encode([]rune(s)))>80{return false}
	for _,r:=range s{if unicode.IsControl(r)||r==0xfffe||r==0xffff||r==utf8.RuneError{return false}}
	return true
}
func identity(f feature)(string,bool){
	p:=map[string]string{"node":"n","way":"w","relation":"r"}[tag(f,"@type")]
	var id int64
	if p==""||json.Unmarshal(f.Properties["@id"],&id)!=nil||id<=0{return "",false}
	return fmt.Sprintf("poi-osm-%s-%d",p,id),true
}
func finite(v float64)bool{return !math.IsNaN(v)&&!math.IsInf(v,0)}
func normalize(in io.Reader,sample string)([]row,report,error){
	b,ok:=samples[sample]
	r:=report{Sample:sample,Source:sourceURL,Bounds:b,RawCategories:map[string]int{},AcceptedCategories:map[string]int{},Rejected:map[string]int{},Geometry:map[string]int{},Note:"Pilot: exported feature counts, point-only accepted rows, non-point and unnamed excluded. Interior sample boxes only, no state polygon; no access or navigation assertion."}
	if !ok{return nil,r,errors.New("unknown interior sample")}
	seen:=map[string]row{}
	sc:=bufio.NewScanner(in)
	sc.Buffer(make([]byte,64<<10),2<<20)
	for sc.Scan(){
		line:=strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(sc.Text()),"\x1e"))
		if line==""{continue}
		r.Features++
		if r.Features>100000{return nil,r,errors.New("feature cap exceeded")}
		if !utf8.ValidString(line){return nil,r,errors.New("invalid UTF-8")}
		var f feature
		if json.Unmarshal([]byte(line),&f)!=nil||f.Type!="Feature"{return nil,r,errors.New("malformed GeoJSON")}
		c:=categories(f)
		if len(c)==0{r.Rejected["unclassified"]++;continue}
		for _,k:=range c{r.RawCategories[k]++}
		r.Geometry[f.Geometry.Type]++
		if len(c)>1{r.Rejected["secondary_memberships"]+=len(c)-1}
		if tag(f,"disused")=="yes"||tag(f,"abandoned")=="yes"||tag(f,"demolished")=="yes"||tag(f,"closed")=="yes"||tag(f,"operating_status")=="closed"{r.Rejected["closed_or_disused"]++;continue}
		if f.Geometry.Type!="Point"{r.Rejected["non_point_geometry"]++;continue}
		var xy []float64
		if json.Unmarshal(f.Geometry.Coordinates,&xy)!=nil||len(xy)!=2||!finite(xy[0])||!finite(xy[1])||math.Abs(xy[0])>180||math.Abs(xy[1])>90{r.Rejected["invalid_coordinates"]++;continue}
		if xy[0]<b.W||xy[0]>b.E||xy[1]<b.S||xy[1]>b.N{r.Rejected["outside_sample"]++;continue}
		id,ok:=identity(f)
		if !ok{r.Rejected["invalid_id"]++;continue}
		name:=strings.TrimSpace(tag(f,"name"))
		if !validName(name){r.Rejected["unnamed_or_invalid_name"]++;continue}
		v:=row{id,c[0],name,xy[1],xy[0]}
		if prev,ok:=seen[id];ok{
			if prev!=v{return nil,r,fmt.Errorf("conflicting duplicate %s",id)}
			r.Rejected["duplicate"]++;continue
		}
		seen[id]=v;r.AcceptedCategories[v.Category]++
	}
	if err:=sc.Err();err!=nil{return nil,r,err}
	out:=make([]row,0,len(seen));for _,v:=range seen{out=append(out,v)}
	sort.Slice(out,func(i,j int)bool{return out[i].ID<out[j].ID})
	r.Rows=len(out)
	return out,r,nil
}
func writeJSON(path string,v any)error{
	f,err:=os.OpenFile(path,os.O_WRONLY|os.O_CREATE|os.O_EXCL,0600)
	if err!=nil{return err}
	enc:=json.NewEncoder(f);enc.SetIndent("","  ");err=enc.Encode(v)
	if e:=f.Close();err==nil{err=e};return err
}
func writeCSV(path string,rows []row)error{
	f,err:=os.OpenFile(path,os.O_WRONLY|os.O_CREATE|os.O_EXCL,0600)
	if err!=nil{return err}
	w:=csv.NewWriter(f);err=w.Write([]string{"id","lat","lon","category","name"})
	for _,v:=range rows{if err!=nil{break};err=w.Write([]string{v.ID,strconv.FormatFloat(v.Lat,'f',-1,64),strconv.FormatFloat(v.Lon,'f',-1,64),v.Category,v.Name})}
	w.Flush();if err==nil{err=w.Error()};if e:=f.Close();err==nil{err=e};return err
}
func get(ctx context.Context,c *http.Client,url string)(*http.Response,error){
	req,err:=http.NewRequestWithContext(ctx,http.MethodGet,url,nil);if err!=nil{return nil,err}
	req.Header.Set("User-Agent","SailingGo-POI-study/1")
	res,err:=c.Do(req);if err!=nil{return nil,err}
	if res.StatusCode!=200{res.Body.Close();return nil,fmt.Errorf("source HTTP %d",res.StatusCode)}
	return res,nil
}
func parseMD5(s string)(string,error){
	f:=strings.Fields(s)
	if len(f)!=2||strings.TrimPrefix(f[1],"*")!=sourceFile{return "",errors.New("unexpected checksum filename")}
	b,err:=hex.DecodeString(f[0]);if err!=nil||len(b)!=md5.Size{return "",errors.New("invalid MD5")}
	return hex.EncodeToString(b),nil
}
func copyChecked(w io.Writer,r io.Reader,n,cap int64,want string)(string,error){
	if n<=0||n>cap{return "",errors.New("missing or excessive Content-Length")}
	m,s:=md5.New(),sha256.New()
	got,err:=io.CopyN(io.MultiWriter(w,m,s),r,n)
	if err!=nil||got!=n{return "",errors.New("truncated download")}
	if hex.EncodeToString(m.Sum(nil))!=want{return "",errors.New("publisher MD5 mismatch")}
	return hex.EncodeToString(s.Sum(nil)),nil
}
func download(apply bool)error{
	if !apply{fmt.Printf("DRY RUN: %s\nbody cap %d; deadline 15 minutes; new private/poi-study-* only\n",sourceURL,capBytes);return nil}
	if err:=os.MkdirAll("private",0700);err!=nil{return err}
	info,err:=os.Lstat("private")
	if err!=nil||!info.IsDir()||info.Mode()&os.ModeSymlink!=0{return errors.New("private must be a real directory")}
	dir,err:=os.MkdirTemp("private","poi-study-");if err!=nil{return err}
	ok:=false;defer func(){if !ok{_ = os.RemoveAll(dir)}}()
	ctx,cancel:=context.WithTimeout(context.Background(),15*time.Minute);defer cancel()
	tr:=http.DefaultTransport.(*http.Transport).Clone();tr.Proxy=nil;tr.DisableCompression=true
	c:=&http.Client{Transport:tr,CheckRedirect:func(*http.Request,[]*http.Request)error{return errors.New("redirect refused")}}
	defer tr.CloseIdleConnections()
	start:=time.Now()
	res,err:=get(ctx,c,sourceURL+".md5");if err!=nil{return err}
	if res.ContentLength<=0||res.ContentLength>4096{res.Body.Close();return errors.New("unexpected checksum length")}
	side,err:=io.ReadAll(io.LimitReader(res.Body,res.ContentLength));res.Body.Close()
	if err!=nil||int64(len(side))!=res.ContentLength{return errors.New("truncated checksum")}
	want,err:=parseMD5(string(side));if err!=nil{return err}
	res,err=get(ctx,c,sourceURL);if err!=nil{return err};defer res.Body.Close()
	if res.ContentLength<=0||res.ContentLength>capBytes-4096{return errors.New("download size unknown or over budget")}
	f,err:=os.OpenFile(filepath.Join(dir,sourceFile),os.O_WRONLY|os.O_CREATE|os.O_EXCL,0600);if err!=nil{return err}
	sha,err:=copyChecked(f,res.Body,res.ContentLength,capBytes-4096,want)
	if err==nil{err=f.Sync()};if e:=f.Close();err==nil{err=e};if err!=nil{return err}
	if err:=writeJSON(filepath.Join(dir,"source.json"),map[string]any{"url":sourceURL,"release":"261126","publisher_md5":want,"sha256":sha,"pbf_bytes":res.ContentLength,"checksum_bytes":len(side),"elapsed_seconds":time.Since(start).Seconds(),"downloaded_at":time.Now().UTC(),"license":"ODbL-1.0","attribution":"© OpenStreetMap contributors; extract by Geofabrik"});err!=nil{return err}
	ok=true;fmt.Println(dir);return nil
}
func run(args []string)error{
	if len(args)==0{return errors.New("usage: poisample download [-apply] | normalize -sample delta|sf -input FILE -output NEW_DIRECTORY")}
	switch args[0]{
	case "download":
		fs:=flag.NewFlagSet("download",flag.ContinueOnError);apply:=fs.Bool("apply",false,"explicitly download dated extract")
		if err:=fs.Parse(args[1:]);err!=nil{return err};if fs.NArg()!=0{return errors.New("unexpected arguments")};return download(*apply)
	case "normalize":
		fs:=flag.NewFlagSet("normalize",flag.ContinueOnError)
		sample:=fs.String("sample","","delta or sf");input:=fs.String("input","","local GeoJSON sequence");output:=fs.String("output","","new output directory")
		if err:=fs.Parse(args[1:]);err!=nil{return err}
		if *input==""||*output==""||fs.NArg()!=0{return errors.New("input and new output required")}
		f,err:=os.Open(*input);if err!=nil{return err};defer f.Close()
		info,err:=f.Stat();if err!=nil{return err}
		if !info.Mode().IsRegular()||info.Size()>maxInput{return errors.New("input must be regular file <=128 MiB")}
		rows,r,err:=normalize(io.LimitReader(f,info.Size()),*sample);if err!=nil{return err}
		if err:=os.Mkdir(*output,0700);err!=nil{return err}
		ok:=false;defer func(){if !ok{_ = os.RemoveAll(*output)}}()
		if err:=writeCSV(filepath.Join(*output,"normalized.csv"),rows);err!=nil{return err}
		if err:=writeJSON(filepath.Join(*output,"report.json"),r);err!=nil{return err}
		ok=true;enc:=json.NewEncoder(os.Stdout);enc.SetIndent("","  ");return enc.Encode(r)
	default:return errors.New("unknown subcommand")
	}
}
func main(){if err:=run(os.Args[1:]);err!=nil{fmt.Fprintln(os.Stderr,err);os.Exit(1)}}
