// poiprobe measures coordinate/category indexes only. It is not a provider,
// downloader or production database builder. No network or third-party modules.
package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	maxRows = 100000
	maxInput = 64 << 20
	recordSize = 24
	cellSize = .05
)

type point struct {
	id, cat uint32
	lat, lon float64
}
type box struct {
	w, s, e, n float64
	cat uint32 // zero = all
}
type cell struct{ x, y int }
type span struct{ offset int64; count int }
type grid struct {
	f *os.File
	cells map[cell]span
}

func validCoord(lat, lon float64) bool {
	return !math.IsNaN(lat) && !math.IsNaN(lon) && math.Abs(lat)<=90 && math.Abs(lon)<=180
}
func key(lat,lon float64) cell {
	return cell{int(math.Floor(lon/cellSize)),int(math.Floor(lat/cellSize))}
}
func matches(p point, q box) bool {
	return p.lon>=q.w && p.lon<=q.e && p.lat>=q.s && p.lat<=q.n && (q.cat==0 || p.cat==q.cat)
}
func sortedIDs(ids []uint32) []uint32 {
	sort.Slice(ids,func(i,j int)bool{return ids[i]<ids[j]})
	return ids
}
func brute(ps []point,q box) []uint32 {
	out:=make([]uint32,0)
	for _,p:=range ps {if matches(p,q){out=append(out,p.id)}}
	return sortedIDs(out)
}
func writeGrid(path string,ps []point)(*grid,error) {
	f,err:=os.OpenFile(path,os.O_CREATE|os.O_EXCL|os.O_RDWR,0600)
	if err!=nil{return nil,err}
	ok:=false
	defer func(){if !ok{f.Close()}}()
	buckets:=map[cell][]point{}
	for _,p:=range ps {k:=key(p.lat,p.lon);buckets[k]=append(buckets[k],p)}
	keys:=make([]cell,0,len(buckets))
	for k:=range buckets {keys=append(keys,k)}
	sort.Slice(keys,func(i,j int)bool{if keys[i].x!=keys[j].x{return keys[i].x<keys[j].x};return keys[i].y<keys[j].y})
	g:=&grid{f:f,cells:map[cell]span{}}
	var offset int64
	for _,k:=range keys {
		p:=buckets[k]
		buf:=make([]byte,len(p)*recordSize)
		for i,v:=range p {
			b:=buf[i*recordSize:]
			binary.LittleEndian.PutUint32(b,v.id)
			binary.LittleEndian.PutUint32(b[4:],v.cat)
			binary.LittleEndian.PutUint64(b[8:],math.Float64bits(v.lat))
			binary.LittleEndian.PutUint64(b[16:],math.Float64bits(v.lon))
		}
		if _,err=f.Write(buf);err!=nil{return nil,err}
		g.cells[k]=span{offset,len(p)}
		offset+=int64(len(buf))
	}
	if err=f.Sync();err!=nil{return nil,err}
	if err=f.Close();err!=nil{return nil,err}
	f,err=os.Open(path)
	if err!=nil{return nil,err}
	g.f=f
	ok=true
	return g,nil
}
func (g *grid) query(q box)([]uint32,error) {
	out:=make([]uint32,0)
	a,b:=key(q.s,q.w),key(q.n,q.e)
	for x:=a.x;x<=b.x;x++{for y:=a.y;y<=b.y;y++{
		s,ok:=g.cells[cell{x,y}];if !ok{continue}
		buf:=make([]byte,s.count*recordSize)
		if _,err:=g.f.ReadAt(buf,s.offset);err!=nil{return nil,err}
		for i:=0;i<s.count;i++{
			d:=buf[i*recordSize:]
			p:=point{binary.LittleEndian.Uint32(d),binary.LittleEndian.Uint32(d[4:]),math.Float64frombits(binary.LittleEndian.Uint64(d[8:])),math.Float64frombits(binary.LittleEndian.Uint64(d[16:]))}
			if matches(p,q){out=append(out,p.id)}
		}
	}}
	return sortedIDs(out),nil
}
func sampleBoxes() []box {
	return []box{{-122.43,37.77,-122.39,37.81,0},{-121.94,38,-121.84,38.06,0},{-120.58,38.16,-120.49,38.24,0},{-120.08,38.93,-119.94,39.02,0},{-117.23,32.66,-117.10,32.76,0}}
}
func synthetic(n int) []point {
	r:=rand.New(rand.NewSource(51))
	b:=sampleBoxes()
	ps:=make([]point,n)
	for i:=range ps {
		q:=b[i%len(b)]
		if i%4==0{q=box{-124,32.5,-114.2,42,0}} // synthetic extent, not state polygon
		ps[i]=point{uint32(i+1),uint32(1+i%11),q.s+r.Float64()*(q.n-q.s),q.w+r.Float64()*(q.e-q.w)}
	}
	return ps
}
// CSV is a local, already-normalized input. IDs are checked for uniqueness;
// benchmark keys are ordinal uint32, not production waypoint IDs. No raw
// taxonomy mapping, state clipping, geometry resolution or dedup is implied.
func readCSV(r io.Reader)([]point,map[string]int,error){
	c:=csv.NewReader(io.LimitReader(r,maxInput+1))
	h,err:=c.Read();if err!=nil{return nil,nil,err}
	if strings.Join(h,",")!="id,lat,lon,category,name"{return nil,nil,errors.New("expected id,lat,lon,category,name header")}
	ps:=make([]point,0);ids:=map[string]bool{};cats:=map[string]uint32{};counts:=map[string]int{}
	for {
		row,err:=c.Read()
		if err==io.EOF{break};if err!=nil{return nil,nil,err}
		if len(ps)>=maxRows||len(row[0])==0||len(row[0])>80||ids[row[0]]||len(row[3])==0||len(row[3])>32||strings.TrimSpace(row[4])==""||len(row[4])>320{return nil,nil,errors.New("invalid, duplicate or excess normalized row")}
		lat,e1:=strconv.ParseFloat(row[1],64);lon,e2:=strconv.ParseFloat(row[2],64)
		if e1!=nil||e2!=nil||!validCoord(lat,lon){return nil,nil,errors.New("invalid coordinate")}
		if cats[row[3]]==0{if len(cats)>=32{return nil,nil,errors.New("too many categories")};cats[row[3]]=uint32(len(cats)+1)}
		ids[row[0]]=true;counts[row[3]]++
		ps=append(ps,point{uint32(len(ps)+1),cats[row[3]],lat,lon})
	}
	if c.InputOffset()>maxInput{return nil,nil,errors.New("input exceeds 64 MiB")}
	return ps,counts,nil
}
func queries() []box {
	r:=rand.New(rand.NewSource(52))
	base:=sampleBoxes();out:=make([]box,0,100)
	for i:=0;i<100;i++{
		b:=base[i%len(base)]
		if i%4==0{b.cat=uint32(1+i%11)}
		if i>=10 {
			w,e:=b.w,b.e;s,n:=b.s,b.n
			b.w=w+r.Float64()*(e-w)/2;b.e=e-r.Float64()*(e-w)/2
			b.s=s+r.Float64()*(n-s)/2;b.n=n-r.Float64()*(n-s)/2
		}
		out=append(out,b)
	}
	return out
}
func equal(a,b []uint32)bool{
	if len(a)!=len(b){return false};for i:=range a{if a[i]!=b[i]{return false}};return true
}
type stats struct {
	Workers int
	WallMS, P50MS, P95MS, P99MS float64
}
func bench(g *grid,qs []box,want [][]uint32,workers int)(stats,error){
	d:=make([]time.Duration,len(qs))
	errs:=make([]error,len(qs));var wg sync.WaitGroup;start:=time.Now()
	for w:=0;w<workers;w++{
		wg.Add(1)
		go func(w int){defer wg.Done();for i:=w;i<len(qs);i+=workers{
			t:=time.Now();got,err:=g.query(qs[i]);d[i]=time.Since(t)
			if err!=nil{errs[i]=err}else if !equal(got,want[i]){errs[i]=fmt.Errorf("grid differs from scan at query %d",i)}
		}}(w)
	}
	wg.Wait();wall:=time.Since(start)
	for _,err:=range errs{if err!=nil{return stats{},err}}
	sort.Slice(d,func(i,j int)bool{return d[i]<d[j]})
	p:=func(n float64)float64{return float64(d[int(math.Ceil(n*float64(len(d))))-1])/float64(time.Millisecond)}
	return stats{workers,float64(wall)/float64(time.Millisecond),p(.5),p(.95),p(.99)},nil
}
func sqliteRun(ctx context.Context,bin,path,sql string)(string,error){
	cmd:=exec.CommandContext(ctx,bin,"-batch","-noheader","-init",os.DevNull,path)
	cmd.Stdin=strings.NewReader(".bail on\n"+sql)
	out,err:=cmd.Output()
	if err!=nil{return "",fmt.Errorf("sqlite probe: %w",err)}
	return string(out),nil
}
type sqliteStats struct{
	Bytes int64
	BuildMS, BatchMS float64
}
func sqliteCompare(bin,path string,ps []point,qs []box,want [][]uint32)(sqliteStats,error){
	ctx,cancel:=context.WithTimeout(context.Background(),2*time.Minute);defer cancel()
	var sql strings.Builder
	sql.WriteString("CREATE TABLE p(id INTEGER PRIMARY KEY, cat INTEGER NOT NULL, lat REAL NOT NULL, lon REAL NOT NULL); CREATE VIRTUAL TABLE r USING rtree(id,minlon,maxlon,minlat,maxlat); BEGIN;\n")
	for _,p:=range ps{
		fmt.Fprintf(&sql,"INSERT INTO p VALUES(%d,%d,%.17g,%.17g); INSERT INTO r VALUES(%d,%.17g,%.17g,%.17g,%.17g);\n",p.id,p.cat,p.lat,p.lon,p.id,p.lon,p.lon,p.lat,p.lat)
	}
	sql.WriteString("COMMIT;\n")
	start:=time.Now();if _,err:=sqliteRun(ctx,bin,path,sql.String());err!=nil{return sqliteStats{},err}
	build:=time.Since(start);sql.Reset()
	for i,q:=range qs{
		// Numeric fixture values only, no external text interpolated.
		fmt.Fprintf(&sql,"SELECT '%d:'||COALESCE(group_concat(id,','),'') FROM (SELECT p.id FROM r JOIN p ON p.id=r.id WHERE r.minlon<=%.17g AND r.maxlon>=%.17g AND r.minlat<=%.17g AND r.maxlat>=%.17g AND p.lon BETWEEN %.17g AND %.17g AND p.lat BETWEEN %.17g AND %.17g AND (%d=0 OR p.cat=%d) ORDER BY p.id);\n",i,q.e,q.w,q.n,q.s,q.w,q.e,q.s,q.n,q.cat,q.cat)
	}
	start=time.Now();out,err:=sqliteRun(ctx,bin,path,sql.String());wall:=time.Since(start)
	if err!=nil{return sqliteStats{},err}
	var expected bytes.Buffer
	for i,ids:=range want{
		fmt.Fprintf(&expected,"%d:",i)
		for j,id:=range ids{if j>0{expected.WriteByte(',')};fmt.Fprint(&expected,id)}
		expected.WriteByte('\n')
	}
	if out!=expected.String(){return sqliteStats{},errors.New("SQLite R-Tree results differ from scan")}
	info,err:=os.Stat(path);if err!=nil{return sqliteStats{},err}
	return sqliteStats{info.Size(),float64(build)/float64(time.Millisecond),float64(wall)/float64(time.Millisecond)},nil
}
func run()error{
	n:=flag.Int("synthetic",100000,"deterministic synthetic rows (1..100000)")
	input:=flag.String("input","","already-normalized local CSV; no network")
	sqlite:=flag.Bool("sqlite",false,"also compare using existing sqlite3 CLI; no installation")
	flag.Parse()
	if flag.NArg()!=0||*n<1||*n>maxRows{return errors.New("invalid arguments")}
	var ps []point;var counts map[string]int;var err error
	kind:="synthetic"
	if *input!=""{
		kind="local_normalized_csv"
		f,e:=os.Open(*input);if e!=nil{return e};defer f.Close()
		info,e:=f.Stat();if e!=nil{return e};if !info.Mode().IsRegular()||info.Size()>maxInput{return errors.New("input must be regular file <=64 MiB")}
		ps,counts,err=readCSV(f);if err!=nil{return err}
	}else{ps=synthetic(*n)}
	if len(ps)==0{return errors.New("no records")}
	dir,err:=os.MkdirTemp("","sailing-poiprobe-");if err!=nil{return err};defer os.RemoveAll(dir)
	qs:=queries();want:=make([][]uint32,len(qs));start:=time.Now()
	for i,q:=range qs{want[i]=brute(ps,q)}
	scan:=time.Since(start)
	start=time.Now();g,err:=writeGrid(filepath.Join(dir,"grid.bin"),ps);if err!=nil{return err};defer g.f.Close()
	build:=time.Since(start);info,err:=g.f.Stat();if err!=nil{return err}
	one,err:=bench(g,qs,want,1);if err!=nil{return err}
	four,err:=bench(g,qs,want,4);if err!=nil{return err}
	report:=map[string]any{"input_kind":kind,"rows":len(ps),"category_counts":counts,"queries":len(qs),"go":runtime.Version(),"os":runtime.GOOS,"arch":runtime.GOARCH,"scan_wall_ms":float64(scan)/float64(time.Millisecond),"grid_file_bytes":info.Size(),"grid_cells_in_memory":len(g.cells),"grid_build_ms":float64(build)/float64(time.Millisecond),"grid_single":one,"grid_four":four,"parity":"pass","sqlite":"not requested","limits":"index-only; grid directory memory not included in file bytes; no cold-cache/RSS or payload measurement"}
	if *sqlite{
		bin,e:=exec.LookPath("sqlite3");if e!=nil{return errors.New("sqlite3 not on PATH; no installation attempted")}
		s,e:=sqliteCompare(bin,filepath.Join(dir,"probe.sqlite"),ps,qs,want);if e!=nil{return e}
		report["sqlite"]=s
	}
	enc:=json.NewEncoder(os.Stdout);enc.SetIndent("","  ");return enc.Encode(report)
}
func main(){if err:=run();err!=nil{fmt.Fprintln(os.Stderr,err);os.Exit(1)}}
