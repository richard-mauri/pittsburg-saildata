package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strings"
	"time"
)

const release = "2026-11-18.0"
const root = "https://stac.overturemaps.org/"+release+"/catalog.json"
const capBytes = 16 << 20
const maxRequests = 512

type link struct { Rel string `json:"rel"`; Href string `json:"href"` }
type asset struct { Href string `json:"href"`; Size int64 `json:"file:size"` }
type node struct {
	Type string `json:"type"`
	ID string `json:"id"`
	Collection string `json:"collection"`
	BBox []float64 `json:"bbox"`
	Links []link `json:"links"`
	Assets map[string]asset `json:"assets"`
}
type candidate struct {
	Item string
	BBox []float64
	AWS string
	Bytes int64
	Samples []string
}
type result struct {
	Release string
	CatalogBytes int64
	Requests int
	ItemsInspected int
	Candidates []candidate
	FullFileBytes int64
	Note string
}
type fetcher func(context.Context,string)(node,error)

func catalogURL(base,href string)(string,error){
	b,err:=url.Parse(base);if err!=nil{return "",err}
	h,err:=url.Parse(href);if err!=nil{return "",err}
	u:=b.ResolveReference(h)
	if u.Scheme!="https"||u.Host!="stac.overturemaps.org"||u.User!=nil||u.RawQuery!=""||u.Fragment!=""||u.RawPath!=""||!strings.HasPrefix(u.Path,"/"+release+"/")||path.Clean(u.Path)!=u.Path||!strings.HasSuffix(u.Path,".json"){return "",errors.New("unexpected catalog link")}
	return u.String(),nil
}

func placePath(u string)bool{
	p,_:=url.Parse(u)
	parts:=strings.Split(strings.Trim(p.Path,"/"),"/")
	return len(parts)>=3 && (parts[1]=="places"||parts[1]=="theme=places")
}

func overlaps(a,b []float64)bool{
	return a[0]<=b[2]&&a[2]>=b[0]&&a[1]<=b[3]&&a[3]>=b[1]
}
func validBox(b []float64)bool{
	return len(b)==4 && b[0]>=-180&&b[2]<=180&&b[1]>=-90&&b[3]<=90&&b[0]<=b[2]&&b[1]<=b[3]
}

func scan(ctx context.Context,get fetcher)(result,error){
	out:=result{Release:release,Note:"Metadata only; whole-file byte estimate is not feature download or coverage."}
	queue:=[]string{root}
	seen:=map[string]bool{}
	assets:=map[string]bool{}
	for len(queue)>0{
		u:=queue[0];queue=queue[1:]
		if seen[u]{continue}
		if len(seen)>=maxRequests{return out,errors.New("catalog request limit")}
		seen[u]=true
		n,err:=get(ctx,u);if err!=nil{return out,err}
		switch n.Type{
		case "Catalog","Collection":
			for _,l:=range n.Links{
				if l.Rel!="child"&&l.Rel!="item"{continue}
				v,err:=catalogURL(u,l.Href);if err!=nil{return out,err}
				if placePath(v){queue=append(queue,v)}
				if len(queue)>maxRequests{return out,errors.New("catalog queue limit")}
			}
		case "Feature":
			if n.Collection!="place"||!validBox(n.BBox){return out,errors.New("unexpected place item schema")}
			out.ItemsInspected++
			var samples []string
			if overlaps(n.BBox,[]float64{-121.94,38,-121.84,38.06}){samples=append(samples,"delta")}
			if overlaps(n.BBox,[]float64{-122.43,37.77,-122.39,37.81}){samples=append(samples,"sf")}
			if len(samples)==0{continue}
			a:=n.Assets["aws"]
			assetURL, err := url.Parse(a.Href)
			prefix := "/release/"+release+"/theme=places/type=place/"
			if err != nil || assetURL.User != nil || assetURL.RawQuery != "" || assetURL.Fragment != "" || !strings.HasPrefix(assetURL.Path,prefix) || path.Clean(assetURL.Path)!=assetURL.Path || !strings.HasSuffix(assetURL.Path,".parquet") || a.Size<=0 || a.Size>100000000000 {return out,errors.New("invalid AWS item metadata")}
			if !((assetURL.Scheme=="https" && assetURL.Host=="overturemaps-us-west-2.s3.us-west-2.amazonaws.com") || (assetURL.Scheme=="s3" && assetURL.Host=="overturemaps-us-west-2")) {return out,errors.New("unexpected AWS asset host")}
			if assets[assetURL.Path] {return out,errors.New("duplicate AWS item metadata")}
			assets[assetURL.Path]=true
			out.Candidates=append(out.Candidates,candidate{n.ID,n.BBox,a.Href,a.Size,samples})
			out.FullFileBytes+=a.Size
		default:return out,errors.New("unknown catalog node type")
		}
	}
	if out.ItemsInspected==0||len(out.Candidates)==0{return out,errors.New("no matching place inventory; verify STAC layout")}
	sort.Slice(out.Candidates,func(i,j int)bool{return out.Candidates[i].AWS<out.Candidates[j].AWS})
	return out,nil
}

func run()error{
	if len(os.Args)!=1{return errors.New("no arguments: fixed release and pilot boxes")}
	ctx,cancel:=context.WithTimeout(context.Background(),90*time.Second);defer cancel()
	tr:=&http.Transport{DisableCompression:true}
	client:=&http.Client{Transport:tr,CheckRedirect:func(*http.Request,[]*http.Request)error{return errors.New("redirect refused")}}
	var total int64
	var requests int
	get:=func(ctx context.Context,u string)(node,error){
		var n node
		if _,err:=catalogURL(root,u);err!=nil{return n,err}
		if requests>=maxRequests||total>=capBytes{return n,errors.New("metadata budget exhausted")}
		req,err:=http.NewRequestWithContext(ctx,http.MethodGet,u,nil);if err!=nil{return n,err}
		requests++
		resp,err:=client.Do(req);if err!=nil{return n,err}
		defer resp.Body.Close()
		remaining:=int64(capBytes)-total
		if resp.StatusCode!=200||resp.ContentLength<0||resp.ContentLength>remaining||resp.Header.Get("Content-Encoding")!=""{return n,errors.New("unexpected or oversized metadata response")}
		raw,err:=io.ReadAll(io.LimitReader(resp.Body,resp.ContentLength))
		total+=int64(len(raw))
		if err!=nil||int64(len(raw))!=resp.ContentLength{return n,errors.New("incomplete metadata response")}
		return n,json.Unmarshal(raw,&n)
	}
	out,err:=scan(ctx,get)
	if err!=nil{return fmt.Errorf("metadata inventory stopped after %d requests, %d body bytes: %w",requests,total,err)}
	out.Requests,out.CatalogBytes=requests,total
	e:=json.NewEncoder(os.Stdout);e.SetIndent("","  ");return e.Encode(out)
}

func main(){if err:=run();err!=nil{fmt.Fprintln(os.Stderr,err);os.Exit(1)}}
