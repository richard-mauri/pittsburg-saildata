package main

import (
 "encoding/csv"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "math"
 "os"
 "path/filepath"
 "sort"
 "strconv"
 "strings"
)

const auditPairCap=2000000
type categoryAudit struct {
 Rows,StatusMissing,StatusOpen,ConfidenceMissing,ConfidenceBelow05,Confidence05To08,ConfidenceAtLeast08 int
 RetainedAt05,RetainedAt08 int
 NearbySameNamePairs,NearbySameNameRecords int
 PrimaryCategories map[string]int
}
type reviewRecord struct { ID,Name,Category,Primary,Status,Confidence string; Lat,Lon float64 }
type auditReport struct{
 Sample,Release,Note string
 Accepted int
 Categories map[string]*categoryAudit
 ExcludedReviewTokens map[string]int
 NearbySameNamePairs,NearbySameNameRecords int
 PairChecks int
 Review []reviewRecord `json:"Review,omitempty"`
}
func normName(s string)string{return strings.ToLower(strings.Join(strings.Fields(s)," "))}
func distanceM(a,b row)float64{
 const rad=math.Pi/180
 x:=math.Sin((b.Lat-a.Lat)*rad/2);y:=math.Sin((b.Lon-a.Lon)*rad/2)
 h:=x*x+math.Cos(a.Lat*rad)*math.Cos(b.Lat*rad)*y*y
 return 12742000*math.Asin(math.Sqrt(math.Min(1,h)))
}
// Review candidates only: same category and exact case/whitespace-folded name
// within 50 m. Never auto-merge separate source IDs.
func nearby(rows []row,r *auditReport)error{
 groups:=map[string][]row{}
 for _,v:=range rows{key:=v.Category+"\x00"+normName(v.Name);groups[key]=append(groups[key],v)}
 marked:=map[string]bool{}
 for _,g:=range groups{
  sort.Slice(g,func(i,j int)bool{return g[i].Lat<g[j].Lat})
  for i,a:=range g{
   for j:=i+1;j<len(g);j++{
    b:=g[j];if b.Lat-a.Lat>0.00046{break}
    r.PairChecks++;if r.PairChecks>auditPairCap{return errors.New("duplicate audit pair budget exceeded")}
    if distanceM(a,b)<=50{
     r.NearbySameNamePairs++;r.Categories[a.Category].NearbySameNamePairs++
     for _,v:=range []row{a,b}{if !marked[v.ID]{marked[v.ID]=true;r.Categories[v.Category].NearbySameNameRecords++}}
    }
   }
  }
 }
 r.NearbySameNameRecords=len(marked);return nil
}
func missing(v string)string{if v=="NULL"{return ""};return v}
func auditRows(in io.Reader,rows []row,sample string,review bool)(auditReport,error){
 r:=auditReport{Sample:sample,Release:release,Accepted:len(rows),Note:"Read-only audit of current accepted pilot; confidence threshold sensitivity excludes missing confidence at thresholds. Same-name <=50 m pairs are review candidates only. No real-world existence or access verification; excluded tokens counted only if current mapper returns no category.",Categories:map[string]*categoryAudit{},ExcludedReviewTokens:map[string]int{}}
 b,ok:=boxes[sample];if !ok{return r,errors.New("unknown sample")}
 accepted:=map[string]row{};matched:=map[string]bool{}
 for _,v:=range rows{accepted[v.ID]=v;if r.Categories[v.Category]==nil{r.Categories[v.Category]=&categoryAudit{PrimaryCategories:map[string]int{}}}}
 reviewTokens:=map[string]bool{"fueling_station":true,"convenience_store":true,"warehouse_club_store":true,"superstore":true,"fast_food_restaurant":true,"casual_eatery":true,"food_truck_stand":true,"market":true,"bed_and_breakfast":true,"inn":true,"campground":true,"private_lodging":true,"marina":true}
 c:=csv.NewReader(io.LimitReader(in,rawCap+1));hdr,e:=c.Read();if e!=nil{return r,e}
 if strings.Join(hdr,",")!="id,name,basic_category,primary_category,hierarchy,operating_status,confidence,wkb,sources"{return r,errors.New("invalid audit header")}
 count:=0
 var examples []reviewRecord
 for{
  f,e:=c.Read();if e==io.EOF{break};if e!=nil{return r,e};count++
  if count>maxRows||c.InputOffset()>rawCap{return r,errors.New("audit input limit")}
  for _,i:=range []int{1,2,3,4,5,6,8}{f[i]=missing(f[i])}
  lat,lon,valid:=point(f[7]);if !valid||lat<b.S||lat>b.N||lon<b.W||lon>b.E{continue}
  var hierarchy []string
  if f[4]!=""&&json.Unmarshal([]byte(f[4]),&hierarchy)!=nil{return r,errors.New("invalid audit hierarchy")}
  cat:=category(f[2],f[3],hierarchy)
  if cat==""{
   tokens:=map[string]bool{};for _,k:=range []string{f[2],f[3]}{if reviewTokens[k]{tokens[k]=true}}
   for k:=range tokens{r.ExcludedReviewTokens[k]++};continue
  }
  id:="poi-ovt-"+f[0];v,ok:=accepted[id];if !ok||matched[id]||v.Category!=cat||v.Name!=strings.TrimSpace(f[1])||v.Lat!=lat||v.Lon!=lon{continue}
  if f[5]!=""&&f[5]!="open"{continue}
  var confidence float64
  if f[6]!=""{confidence,e=strconv.ParseFloat(f[6],64);if e!=nil||!finite(confidence)||confidence<=0||confidence>1{continue}}
  matched[id]=true;a:=r.Categories[cat];a.Rows++;a.PrimaryCategories[counter(f[3])]++
  if f[5]==""{a.StatusMissing++}else{a.StatusOpen++}
  if f[6]==""{a.ConfidenceMissing++}else if confidence<0.5{a.ConfidenceBelow05++}else if confidence<0.8{a.Confidence05To08++;a.RetainedAt05++}else{a.ConfidenceAtLeast08++;a.RetainedAt05++;a.RetainedAt08++}
  if review{examples=append(examples,reviewRecord{id,v.Name,cat,counter(f[3]),counter(f[5]),counter(f[6]),lat,lon})}
 }
 if len(matched)!=len(rows){return r,errors.New("audit could not reconcile accepted source IDs")}
 if e:=nearby(rows,&r);e!=nil{return r,e}
 // At most 3 records per category per confidence band, sorted by stable ID.
 // These are local manual-review examples, not a representative random sample.
 if review{
  sort.Slice(examples,func(i,j int)bool{return examples[i].ID<examples[j].ID})
  bins:=map[string]int{}
  for _,v:=range examples{band:="missing";if n,e:=strconv.ParseFloat(v.Confidence,64);e==nil{if n<0.5{band="low"}else if n<0.8{band="mid"}else{band="high"}}
   key:=v.Category+"|"+band;if bins[key]<3{r.Review=append(r.Review,v);bins[key]++}
  }
 }
 return r,nil
}
func runAudit(sample string,review bool)error{
 if _,ok:=boxes[sample];!ok{return errors.New("sample must be delta or sf")}
 if e:=realDir("private");e!=nil{return e};if e:=realDir(studyPath);e!=nil{return e}
 dir:=filepath.Join(studyPath,sample);if e:=realDir(dir);e!=nil{return e}
 p:=filepath.Join(dir,"features.csv");st,e:=os.Lstat(p)
 if e!=nil||!st.Mode().IsRegular()||st.Size()<=0||st.Size()>rawCap{return errors.New("invalid saved CSV")}
 f,e:=os.Open(p);if e!=nil{return e};defer f.Close()
 rows,rep,e:=normalize(f,sample);if e!=nil{return e}
 if _,e=f.Seek(0,io.SeekStart);e!=nil{return e}
 // Re-normalize same bytes; compare row counts with completed report, not an
 // independent checksum of the original extraction.
 reportPath:=filepath.Join(dir,"report.json");st,e=os.Lstat(reportPath)
 if e!=nil||!st.Mode().IsRegular()||st.Size()>rawCap{return errors.New("completed sample report required")}
 raw,e:=os.ReadFile(reportPath);if e!=nil{return e};var previous report
 if json.Unmarshal(raw,&previous)!=nil||previous.Sample!=sample||previous.Release!=release||previous.Rows!=rep.Rows||previous.Features!=rep.Features{return errors.New("sample report mismatch")}
 r,e:=auditRows(f,rows,sample,review);if e!=nil{return e}
 fmt.Fprintln(os.Stderr,"Audit only: no files modified")
 return printJSON(r)
}
