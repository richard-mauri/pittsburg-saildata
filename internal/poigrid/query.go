package poigrid

import(
 "container/heap"
 "context"
 "encoding/json"
 "errors"
 "math"
 "sort"
 "time"
)
const MaxRecords=500
// Reserve 16 KiB of the future 512 KiB HTTP limit for envelope metadata.
// This reader returns only the JSON array; the HTTP layer must cap the entire body.
const MaxArrayBytes=496<<10
var ErrBusy=errors.New("POI query admission limit")
var ErrQuery=errors.New("invalid POI query")
var ErrClosed=errors.New("POI reader closed")
type Query struct{West,South,East,North float64;Categories []string;Limit,ByteLimit int}
type Result struct{Records []json.RawMessage;Truncated bool;ArrayBytes int}
type candidate struct{id string;raw json.RawMessage}
type largest []candidate
func(h largest)Len()int{return len(h)}
func(h largest)Less(i,j int)bool{return h[i].id>h[j].id}
func(h largest)Swap(i,j int){h[i],h[j]=h[j],h[i]}
func(h *largest)Push(v any){*h=append(*h,v.(candidate))}
func(h *largest)Pop()any{a:=*h;n:=len(a)-1;v:=a[n];a[n]=candidate{};*h=a[:n];return v}
func validate(q Query)([len(categories)]bool,error){
 var mask [len(categories)]bool
 if !finite(q.West)||!finite(q.South)||!finite(q.East)||!finite(q.North)||q.West< -180||q.East>180||q.South< -90||q.North>90||q.West>q.East||q.South>q.North||q.East-q.West>.5||q.North-q.South>.5||(q.East-q.West)*(q.North-q.South)>.125||len(q.Categories)<1||len(q.Categories)>4||q.Limit<1||q.Limit>MaxRecords||q.ByteLimit<2||q.ByteLimit>MaxArrayBytes{return mask,ErrQuery}
 for _,c:=range q.Categories{found:=false;for i,k:=range categories{if c==k{if mask[i]{return mask,ErrQuery};mask[i]=true;found=true}};if !found{return mask,ErrQuery}}
 return mask,nil
}
func(r *Reader)Search(ctx context.Context,q Query)(Result,error){return r.search(ctx,q,false)}
// ScanForVerification is an offline comparison only, not a serving path.
func(r *Reader)ScanForVerification(ctx context.Context,q Query)(Result,error){return r.search(ctx,q,true)}
func(r *Reader)search(ctx context.Context,q Query,scan bool)(Result,error){
 mask,e:=validate(q);if e!=nil{return Result{},e}
 ctx,cancel:=context.WithTimeout(ctx,time.Second);defer cancel()
 if e=ctx.Err();e!=nil{return Result{},e}
 select{case r.slots<-struct{}{}:defer func(){<-r.slots}();default:return Result{},ErrBusy}
 r.mu.RLock();defer r.mu.RUnlock();if r.closed{return Result{},ErrClosed}
 h:=largest{};heap.Init(&h);matched:=0
 consume:=func(start,n int)error{
  for i:=start;i<start+n;i++{
   if e:=ctx.Err();e!=nil{return e}
   v,e:=r.at(i);if e!=nil{return e}
   if !mask[v.Cat]||v.Lon<q.West||v.Lon>q.East||v.Lat<q.South||v.Lat>q.North{continue}
   p,w,e:=r.record(v);if e!=nil{return e};matched++
   c:=candidate{w.ID,p}
   if len(h)<q.Limit{heap.Push(&h,c)}else if c.id<h[0].id{h[0]=c;heap.Fix(&h,0)}
  };return nil
 }
 if scan{if e=consume(0,r.rows);e!=nil{return Result{},e}}else{
  for x:=int(math.Floor(q.West/.05));x<=int(math.Floor(q.East/.05));x++{for y:=int(math.Floor(q.South/.05));y<=int(math.Floor(q.North/.05));y++{s:=r.cells[cellKey{x,y}];if e=consume(s.Start,s.Count);e!=nil{return Result{},e}}}
 }
 if e=ctx.Err();e!=nil{return Result{},e}
 sort.Slice(h,func(i,j int)bool{return h[i].id<h[j].id})
 out:=Result{Records:make([]json.RawMessage,0,len(h)),ArrayBytes:2,Truncated:matched>q.Limit}
 for _,c:=range h{
  n:=len(c.raw);if len(out.Records)>0{n++}
  if out.ArrayBytes+n>q.ByteLimit{out.Truncated=true;break}
  out.Records=append(out.Records,c.raw);out.ArrayBytes+=n
 }
 return out,nil
}
