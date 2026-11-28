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
	"os"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

const maxAsset = 8 << 20

type place struct {
	Name string `json:"name"`
	Category string `json:"category"`
	Lat *float64 `json:"lat"`
	Lon *float64 `json:"lon"`
	Source string `json:"source"`
	SourceID string `json:"source_id"`
	CoordSource string `json:"coord_source"`
	CoordStatus string `json:"coord_status"`
}

type asset struct {
	Version string `json:"version"`
	GeneratorVersion string `json:"generator_version"`
	Updated string `json:"updated"`
	Region string `json:"region"`
	Places []place `json:"places"`
}

type waypoint struct {
	ID string `json:"id"`
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
	Name string `json:"name"`
	Category string `json:"category"`
	Notes string `json:"notes"`
	Created string `json:"created"`
}

type record struct {
	Waypoint waypoint `json:"waypoint"`
	ProviderCategory string `json:"provider_category"`
	Source string `json:"source"`
	SourceID string `json:"source_id"`
	CoordSource string `json:"coord_source"`
	CoordStatus string `json:"coord_status"`
}

type report struct {
	AssetVersion string
	GeneratorVersion string
	Updated string
	Region string
	SHA256 string
	Total int
	Accepted int
	Skipped int
	Counts map[string]int
	Sources map[string]int
	Publication string
}

func clean(s string, n int) bool {
	return utf8.ValidString(s) && strings.TrimSpace(s) != "" && len(utf16.Encode([]rune(s))) <= n && strings.IndexFunc(s, unicode.IsControl) < 0
}

func adapt(raw []byte) (report, []record, error) {
	h := sha256.Sum256(raw)
	out := report{SHA256: hex.EncodeToString(h[:]), Counts: map[string]int{}, Sources: map[string]int{}, Publication: "local study only; redistribution not cleared"}
	var a asset
	if len(raw) > maxAsset || !utf8.Valid(raw) { return out, nil, errors.New("invalid or oversized asset") }
	if err := json.Unmarshal(raw, &a); err != nil { return out, nil, err }
	date, err := time.Parse("2006-01-02", a.Updated)
	if err != nil || a.Version == "" || a.GeneratorVersion == "" || a.Region == "" || len(a.Places) == 0 || len(a.Places) > 10000 { return out, nil, errors.New("invalid asset metadata") }
	out.AssetVersion, out.GeneratorVersion, out.Updated, out.Region, out.Total = a.Version, a.GeneratorVersion, a.Updated, a.Region, len(a.Places)
	mapping := map[string]string{"marinas":"marina", "launch_ramps":"boat_ramp", "fuel_docks":"marine_fuel"}
	seen := map[string]bool{}
	var rows []record
	for _, p := range a.Places {
		c, ok := mapping[p.Category]
		if !ok { out.Skipped++; continue }
		if !clean(p.Name, 80) || !clean(p.Source, 100) || !clean(p.CoordSource, 100) || !clean(p.CoordStatus, 100) || p.Lat == nil || p.Lon == nil || math.IsNaN(*p.Lat) || math.IsNaN(*p.Lon) || math.IsInf(*p.Lat,0) || math.IsInf(*p.Lon,0) || math.Abs(*p.Lat)>90 || math.Abs(*p.Lon)>180 { return out,nil, errors.New("invalid scoped marine record") }
		identity := p.SourceID
		if identity == "" {
			if p.Source != "local override" || p.Name != "Antioch Marina Launch Ramp" || c != "boat_ramp" || p.CoordStatus != "explicit_override" { return out,nil, errors.New("unmapped source identity") }
			identity = "local-antioch-marina-launch-ramp"
		}
		if !clean(identity, 200) { return out,nil, errors.New("invalid source identity") }
		d := sha256.Sum256([]byte(c+"\x00"+p.Source+"\x00"+identity))
		id := "poi-marine-"+hex.EncodeToString(d[:])
		if seen[id] { return out,nil, errors.New("duplicate scoped marine identity") }
		seen[id] = true
		w := waypoint{ID:id, Lat:*p.Lat, Lon:*p.Lon, Name:p.Name, Category:"marina", Notes:"Source: "+p.Source+". Representative location; verify access and navigation independently.", Created:date.UTC().Format("2006-01-02T15:04:05.000Z")}
		rows = append(rows, record{w,c,p.Source,p.SourceID,p.CoordSource,p.CoordStatus})
		out.Counts[c]++
		out.Sources[p.Source]++
	}
	sort.Slice(rows,func(i,j int)bool{return rows[i].Waypoint.ID<rows[j].Waypoint.ID})
	out.Accepted = len(rows)
	return out,rows,nil
}

func run() error {
	input := flag.String("input","assets/marine_places.json","tracked marine asset")
	export := flag.Bool("export",false,"emit local study records to stdout, not authorized for redistribution")
	flag.Parse()
	if flag.NArg()!=0 { return errors.New("unexpected arguments") }
	f,err := os.Open(*input)
	if err!=nil{return err}
	defer f.Close()
	raw,err:=io.ReadAll(io.LimitReader(f,maxAsset+1))
	if err!=nil{return err}
	rep,rows,err:=adapt(raw)
	if err!=nil{return err}
	e:=json.NewEncoder(os.Stdout)
	e.SetIndent("","  ")
	if *export { return e.Encode(struct{Report report `json:"report"`; Records []record `json:"records"`}{rep,rows}) }
	return e.Encode(rep)
}

func main(){if err:=run();err!=nil{fmt.Fprintln(os.Stderr,err);os.Exit(1)}}
