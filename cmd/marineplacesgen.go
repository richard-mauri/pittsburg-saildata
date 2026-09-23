package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html"
	"io"
	"io/ioutil"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	generatorVersion        = "v21"
	defaultOverpassEndpoint = "https://overpass-api.de/api/interpreter"
)

type bbox struct {
	Name             string
	South, West      float64
	North, East      float64
	RestaurantRadius float64
}

var regions = []bbox{
	// Smaller, overlapping boxes are intentional. Public Overpass instances are
	// much more reliable with modest query areas, and dedupePlaces removes
	// duplicates introduced by the overlap.
	{Name: "South San Francisco Bay", South: 37.20, West: -122.55, North: 37.62, East: -121.90, RestaurantRadius: 900},
	{Name: "Central San Francisco Bay", South: 37.52, West: -122.68, North: 37.98, East: -122.12, RestaurantRadius: 900},
	{Name: "San Pablo and North Bay", South: 37.84, West: -122.70, North: 38.32, East: -122.08, RestaurantRadius: 900},
	{Name: "Carquinez and Suisun Bay", South: 37.82, West: -122.25, North: 38.25, East: -121.64, RestaurantRadius: 900},
	{Name: "West Delta", South: 37.78, West: -121.88, North: 38.28, East: -121.35, RestaurantRadius: 900},
	{Name: "Central and South Delta", South: 37.58, West: -121.70, North: 38.16, East: -121.18, RestaurantRadius: 900},
	{Name: "Bodega Bay and Sonoma Coast", South: 38.18, West: -123.22, North: 38.62, East: -122.82, RestaurantRadius: 1100},
	{Name: "Half Moon Bay coast", South: 37.30, West: -122.66, North: 37.60, East: -122.28, RestaurantRadius: 1100},
	{Name: "Pescadero and north Santa Cruz coast", South: 37.05, West: -122.48, North: 37.33, East: -122.16, RestaurantRadius: 1100},
	{Name: "Santa Cruz north coast", South: 36.94, West: -122.28, North: 37.14, East: -121.98, RestaurantRadius: 1100},
	{Name: "Santa Cruz Harbor and north Monterey Bay", South: 36.86, West: -122.08, North: 37.03, East: -121.76, RestaurantRadius: 1100},
	{Name: "Moss Landing and Elkhorn Slough", South: 36.70, West: -122.03, North: 36.88, East: -121.72, RestaurantRadius: 1100},
	{Name: "Monterey and Pacific Grove", South: 36.50, West: -122.06, North: 36.68, East: -121.78, RestaurantRadius: 1100},
}

type overpassResponse struct {
	Elements []overpassElement `json:"elements"`
}

type overpassElement struct {
	Type   string            `json:"type"`
	ID     int64             `json:"id"`
	Lat    float64           `json:"lat"`
	Lon    float64           `json:"lon"`
	Center *overpassCenter   `json:"center"`
	Tags   map[string]string `json:"tags"`
}

type overpassCenter struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

type category struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type place struct {
	Name        string   `json:"name"`
	Aliases     []string `json:"aliases,omitempty"`
	Category    string   `json:"category"`
	City        string   `json:"city,omitempty"`
	Lat         float64  `json:"lat"`
	Lon         float64  `json:"lon"`
	Address     string   `json:"address,omitempty"`
	Website     string   `json:"website,omitempty"`
	Phone       string   `json:"phone,omitempty"`
	Note        string   `json:"note,omitempty"`
	Source      string   `json:"source,omitempty"`
	SourceID    string   `json:"source_id,omitempty"`
	CoordSource string   `json:"coord_source,omitempty"`
	CoordStatus string   `json:"coord_status,omitempty"`
}

type asset struct {
	Version          string     `json:"version"`
	GeneratorVersion string     `json:"generator_version"`
	Updated          string     `json:"updated"`
	Region           string     `json:"region"`
	SourceNote       string     `json:"source_note"`
	FailedRegions    []string   `json:"failed_regions,omitempty"`
	Categories       []category `json:"categories"`
	Places           []place    `json:"places"`
}

type coordinateAuditRecord struct {
	Name        string  `json:"name"`
	Category    string  `json:"category"`
	City        string  `json:"city,omitempty"`
	Lat         float64 `json:"lat"`
	Lon         float64 `json:"lon"`
	Address     string  `json:"address,omitempty"`
	CoordSource string  `json:"coord_source"`
	Status      string  `json:"status"`
	Source      string  `json:"source,omitempty"`
	SourceID    string  `json:"source_id,omitempty"`
}

type dbwDeltaSkippedFacility struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	City     string   `json:"city,omitempty"`
	Address  string   `json:"address,omitempty"`
	Type     string   `json:"type,omitempty"`
	Services []string `json:"services,omitempty"`
	Reason   string   `json:"reason"`
}

type dbwDeltaAudit struct {
	BodyPages                   int                       `json:"body_pages"`
	FacilityIDs                 int                       `json:"facility_ids"`
	FacilitiesFetched           int                       `json:"facilities_fetched"`
	FacilitiesRepresented       int                       `json:"facilities_represented"`
	SkippedNoCoordinates        int                       `json:"skipped_no_coordinates"`
	SkippedNoRecognizedCategory int                       `json:"skipped_no_recognized_category"`
	SkippedFacilities           []dbwDeltaSkippedFacility `json:"skipped_facilities,omitempty"`
}

type coordinateAudit struct {
	GeneratorVersion  string                  `json:"generator_version"`
	Updated           string                  `json:"updated"`
	TotalPlaces       int                     `json:"total_places"`
	NeedsVerification int                     `json:"needs_verification"`
	UnknownProvenance int                     `json:"unknown_provenance"`
	DBWDelta          dbwDeltaAudit           `json:"dbw_delta_discovery"`
	Records           []coordinateAuditRecord `json:"records"`
}

type overrideFile struct {
	Add     []place         `json:"add"`
	Exclude []overrideMatch `json:"exclude"`
	Patch   []overridePatch `json:"patch"`
}

type overrideMatch struct {
	Name     string `json:"name,omitempty"`
	SourceID string `json:"source_id,omitempty"`
	Category string `json:"category,omitempty"`
}

type overridePatch struct {
	Match overrideMatch `json:"match"`
	Set   place         `json:"set"`
}

type rawCandidate struct {
	Place place
	Tags  map[string]string
}

func canonicalRegionName(input string) (string, bool) {
	want := strings.TrimSpace(input)
	for _, region := range regions {
		if strings.EqualFold(want, region.Name) {
			return region.Name, true
		}
	}
	return "", false
}

func printValidRegions(w *os.File) {
	fmt.Fprintln(w, "Valid REGION values:")
	for _, region := range regions {
		fmt.Fprintf(w, "  %s\n", region.Name)
	}
}

func printUsage(w *os.File) {
	fmt.Fprintf(w, "marineplacesgen %s\n\n", generatorVersion)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  go run ./cmd/marineplacesgen.go [options]")
	fmt.Fprintln(w, "  ./placesgen.sh [options]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Default behavior:")
	fmt.Fprintln(w, "  Reuse every successful cached infrastructure job and query only missing pieces.")
	fmt.Fprintln(w, "  Automatic restaurant discovery is disabled; curated restaurant import is used instead.")
	fmt.Fprintln(w, "  Curated restaurants are read from assets/marine_restaurants.json when present.")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Options:")
	fmt.Fprintln(w, "  -help, -h")
	fmt.Fprintln(w, "        Show this help.")
	fmt.Fprintln(w, "  -refresh")
	fmt.Fprintln(w, "        Ignore cache and refresh all selected regions/jobs.")
	fmt.Fprintln(w, "  -refresh-region=REGION")
	fmt.Fprintln(w, "        Refresh exactly one named region. The value is case-insensitive, but")
	fmt.Fprintln(w, "        must otherwise match one of the region names listed below.")
	fmt.Fprintln(w, "  -skip-restaurants")
	fmt.Fprintln(w, "        Compatibility option; automatic restaurant discovery is already disabled.")
	fmt.Fprintln(w, "  -skip-dbw")
	fmt.Fprintln(w, "        Skip California State Parks/DBW boating-facility aggregation.")
	fmt.Fprintln(w, "  -cache-ttl=DURATION")
	fmt.Fprintln(w, "        Treat cache entries older than DURATION as stale. Default 0 means")
	fmt.Fprintln(w, "        cached successes never expire. Example: -cache-ttl=168h")
	fmt.Fprintln(w, "  -timeout=DURATION")
	fmt.Fprintln(w, "        Per-request Overpass timeout. Default: 30s.")
	fmt.Fprintln(w, "  -cache-dir=PATH")
	fmt.Fprintln(w, "        Per-job cache directory. Default: cache/marineplaces")
	fmt.Fprintln(w, "  -out=PATH")
	fmt.Fprintln(w, "        Output JSON asset. Default: assets/marine_places.json")
	fmt.Fprintln(w, "  -audit=PATH")
	fmt.Fprintln(w, "        Coordinate provenance audit JSON. Default: assets/marine_places_audit.json")
	fmt.Fprintln(w, "  -restaurants=PATH")
	fmt.Fprintln(w, "        Optional curated waterfront restaurant JSON. Missing file is allowed.")
	fmt.Fprintln(w, "        Default: assets/marine_restaurants.json")
	fmt.Fprintln(w, "  -overrides=PATH")
	fmt.Fprintln(w, "        Persistent local override file. Default: assets/marine_places_overrides.json")
	fmt.Fprintln(w, "  -endpoint=URL")
	fmt.Fprintln(w, "        Primary Overpass interpreter endpoint.")
	fmt.Fprintln(w, "")
	printValidRegions(w)
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Examples:")
	fmt.Fprintln(w, "  ./placesgen.sh")
	fmt.Fprintln(w, "  ./placesgen.sh -refresh")
	fmt.Fprintln(w, "  ./placesgen.sh -refresh-region=\"Bodega Bay and Sonoma Coast\"")
	fmt.Fprintln(w, "  ./placesgen.sh -refresh-region=\"Half Moon Bay coast\"")
	fmt.Fprintln(w, "  ./placesgen.sh -refresh-region=\"Carquinez and Suisun Bay\"")
	fmt.Fprintln(w, "  ./placesgen.sh -refresh-region=\"Moss Landing and Elkhorn Slough\"")
	fmt.Fprintln(w, "  ./placesgen.sh -refresh-region=\"Monterey and Pacific Grove\"")
	fmt.Fprintln(w, "  ./placesgen.sh -skip-restaurants")
	fmt.Fprintln(w, "  ./placesgen.sh -skip-dbw")
	fmt.Fprintln(w, "  ./placesgen.sh -cache-ttl=168h")
}

func main() {
	var outPath, auditPath, overridesPath, restaurantsPath, endpoint, cacheDir, refreshRegion string
	var timeout, cacheTTL time.Duration
	var refreshAll, refreshRestaurants, skipRestaurants, skipDBW bool
	var showHelp, showHelpShort bool
	flag.StringVar(&outPath, "out", "assets/marine_places.json", "output JSON asset")
	flag.StringVar(&auditPath, "audit", "assets/marine_places_audit.json", "coordinate provenance audit JSON")
	flag.StringVar(&overridesPath, "overrides", "assets/marine_places_overrides.json", "persistent local overrides")
	flag.StringVar(&restaurantsPath, "restaurants", "assets/marine_restaurants.json", "optional curated waterfront restaurant JSON")
	flag.StringVar(&endpoint, "endpoint", defaultOverpassEndpoint, "primary Overpass interpreter endpoint")
	flag.StringVar(&cacheDir, "cache-dir", "cache/marineplaces", "per-job cache directory")
	flag.DurationVar(&cacheTTL, "cache-ttl", 0, "reuse successful cached jobs newer than this age; 0 means no expiry")
	flag.BoolVar(&refreshAll, "refresh", false, "ignore cache and refresh every selected job")
	flag.StringVar(&refreshRegion, "refresh-region", "", "ignore cache only for jobs whose region name contains this text")
	flag.BoolVar(&skipRestaurants, "skip-restaurants", false, "skip restaurant jobs")
	flag.BoolVar(&refreshRestaurants, "refresh-restaurants", false, "refresh only anchor-based restaurant jobs")
	flag.BoolVar(&skipDBW, "skip-dbw", false, "skip California State Parks/DBW facility aggregation")
	flag.DurationVar(&timeout, "timeout", 30*time.Second, "timeout for each Overpass request")
	flag.BoolVar(&showHelp, "help", false, "show help")
	flag.BoolVar(&showHelpShort, "h", false, "show help")
	flag.Usage = func() {
		printUsage(os.Stderr)
	}
	flag.Parse()

	// v19: restaurant discovery remains curated/import-only. DBW Delta discovery now crawls the official body-of-water index in addition to the legacy city pass.
	skipRestaurants = true

	if showHelp || showHelpShort {
		printUsage(os.Stdout)
		return
	}

	if refreshRestaurants {
		fatalf("-refresh-restaurants is retired; edit/replace assets/marine_restaurants.json and run normally")
	}

	if strings.TrimSpace(refreshRegion) != "" {
		canonical, ok := canonicalRegionName(refreshRegion)
		if !ok {
			fmt.Fprintf(os.Stderr, "marineplacesgen %s: unknown region %q\n\n", generatorVersion, refreshRegion)
			printValidRegions(os.Stderr)
			fmt.Fprintln(os.Stderr, "\nRun with -help for full usage and examples.")
			os.Exit(2)
		}
		refreshRegion = canonical
	}

	fmt.Printf("marineplacesgen %s\n", generatorVersion)
	fmt.Println("Restaurant discovery: curated/import-only; DBW Delta body-of-water discovery + service classification + skipped-facility audit enabled")
	mode := "fill-missing"
	if refreshAll {
		mode = "refresh-all"
	} else if strings.TrimSpace(refreshRegion) != "" {
		mode = "refresh-region=" + refreshRegion
	}
	ttlLabel := "no expiry"
	if cacheTTL > 0 {
		ttlLabel = cacheTTL.String()
	}
	fmt.Printf("Mode: %s  Cache: %s  TTL: %s\n", mode, cacheDir, ttlLabel)

	ctx := context.Background()
	client := &http.Client{Timeout: timeout}
	endpoints := overpassEndpoints(endpoint)

	// Phase 1: fetch relatively sparse marine infrastructure only. Restaurant
	// discovery is deliberately deferred until we have real marine anchors.
	jobs := buildFetchJobs(true)
	var infrastructureRaw []rawCandidate
	failedByRegion := map[string]bool{}
	for _, job := range jobs {
		forceRefresh := refreshAll || regionMatchesRefresh(job, refreshRegion)
		result := runFetchJob(ctx, client, endpoints, cacheDir, cacheTTL, forceRefresh, job)
		if result.Err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: %s / %s failed after retries: %v\n", result.Job.ParentRegion, result.Job.Kind, result.Err)
			failedByRegion[result.Job.ParentRegion] = true
			continue
		}
		infrastructureRaw = append(infrastructureRaw, result.Candidates...)
	}
	if len(infrastructureRaw) == 0 && skipDBW {
		fatalf("all Overpass infrastructure jobs failed and DBW aggregation is disabled; refusing to overwrite %s with an empty dataset", outPath)
	}

	var dbwPlaces []place
	var deltaAudit dbwDeltaAudit
	if !skipDBW {
		fmt.Println("\nAggregating California State Parks/DBW boating facilities...")
		var err error
		dbwPlaces, deltaAudit, err = fetchDBWPlaces(ctx, client, cacheDir, refreshAll)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: DBW aggregation incomplete: %v\n", err)
		}
		fmt.Printf("DBW aggregation contributed %d categorized place records\n", len(dbwPlaces))
		fmt.Printf("DBW Delta discovery: %d body pages, %d facility IDs, %d represented, %d without coordinates, %d without a recognized category\n",
			deltaAudit.BodyPages, deltaAudit.FacilityIDs, deltaAudit.FacilitiesRepresented,
			deltaAudit.SkippedNoCoordinates, deltaAudit.SkippedNoRecognizedCategory)
	}

	overrides, err := readOverrides(overridesPath)
	if err != nil {
		fatalf("read overrides: %v", err)
	}

	curatedRestaurants, err := readCuratedRestaurants(ctx, client, cacheDir, restaurantsPath)
	if err != nil {
		fatalf("read curated restaurants: %v", err)
	}
	if len(curatedRestaurants) > 0 {
		fmt.Printf("Curated waterfront restaurants: %d records from %s\n", len(curatedRestaurants), restaurantsPath)
	}

	basePlaces := classifyAndFilter(infrastructureRaw)
	basePlaces = append(basePlaces, dbwPlaces...)
	basePlaces = applyOverrides(basePlaces, overrides)
	basePlaces = dedupePlaces(basePlaces)

	// Phase 2: discover restaurants only in small boxes centered on the marine
	// infrastructure we just found. This avoids sweeping every restaurant in a
	// large Bay/Delta bounding box and dramatically reduces Overpass load.
	var restaurantPlaces []place
	restaurantPlaces = append(restaurantPlaces, curatedRestaurants...)
	if !skipRestaurants {
		restaurantJobs := buildRestaurantAnchorJobs(basePlaces)
		fmt.Printf("\nRestaurant discovery: %d anchor clusters\n", len(restaurantJobs))
		for _, job := range restaurantJobs {
			forceRefresh := refreshAll || refreshRestaurants || regionMatchesRefresh(job, refreshRegion)
			result := runFetchJob(ctx, client, endpoints, cacheDir, cacheTTL, forceRefresh, job)
			if result.Err != nil {
				fmt.Fprintf(os.Stderr, "WARNING: %s / restaurants failed after retries: %v\n", result.Job.ParentRegion, result.Err)
				failedByRegion[result.Job.ParentRegion] = true
				continue
			}
			restaurantPlaces = append(restaurantPlaces, classifyRestaurantCandidates(result.Candidates, basePlaces)...)
		}
	}

	var failedRegions []string
	for _, region := range regions {
		if failedByRegion[region.Name] {
			failedRegions = append(failedRegions, region.Name)
		}
	}

	places := append([]place{}, basePlaces...)
	places = append(places, restaurantPlaces...)
	places = applyOverrides(places, overrides)
	places = dedupePlaces(places)
	sortPlaces(places)

	out := asset{
		Version:          "13",
		GeneratorVersion: generatorVersion,
		Updated:          time.Now().UTC().Format("2006-01-02"),
		Region:           "Bodega Bay and Sonoma Coast through San Francisco Bay and Delta, Half Moon Bay, Santa Cruz, Moss Landing/Elkhorn Slough, and Monterey/Pacific Grove",
		SourceNote:       fmt.Sprintf("Generated by Marine Places generator %s from OpenStreetMap data via Overpass plus California State Parks/Division of Boating and Waterways facility listings. Delta DBW discovery crawls the official Body-of-Water index for Sacramento-San Joaquin Delta pages and merges those facility IDs with the legacy city-based DBW pass; DBW facility type and service lines contribute marina/tie-up, launch/valet, fuel, repair/dry-storage, marine-supply, and waterfront-restaurant classifications, while skipped Delta facilities are recorded individually in the audit file. U.S. Census address geocoding is used where needed; restaurant discovery is curated/import-only; waterfront restaurants come from the optional curated restaurant file and local overrides; website metadata is preserved for map popups; coordinate provenance and Delta DBW coverage counters are retained in the audit file; sources are cached, normalized, geographically filtered, deduplicated, and amended by assets/marine_places_overrides.json. Verify critical details before relying on them; local businesses and services can change.", generatorVersion),
		FailedRegions:    failedRegions,
		Categories: []category{
			{ID: "marinas", Label: "Marinas"},
			{ID: "boatyards", Label: "Boatyards & Repair"},
			{ID: "fuel_docks", Label: "Fuel Docks"},
			{ID: "launch_ramps", Label: "Launch Ramps"},
			{ID: "marine_supply", Label: "Marine Supply"},
			{ID: "yacht_clubs", Label: "Yacht Clubs"},
			{ID: "waterfront_restaurants", Label: "Waterfront Restaurants"},
		},
		Places: places,
	}

	if err := writeJSON(outPath, out); err != nil {
		fatalf("write %s: %v", outPath, err)
	}

	audit := buildCoordinateAudit(places, deltaAudit)
	if err := writeJSON(auditPath, audit); err != nil {
		fatalf("write %s: %v", auditPath, err)
	}

	counts := map[string]int{}
	for _, p := range places {
		counts[p.Category]++
	}
	fmt.Printf("\nWrote %s with %d places:\n", outPath, len(places))
	for _, c := range out.Categories {
		fmt.Printf("  %-24s %d\n", c.Label, counts[c.ID])
	}
	fmt.Printf("\nWrote %s: %d need verification, %d unknown provenance\n", auditPath, audit.NeedsVerification, audit.UnknownProvenance)
	if len(failedRegions) > 0 {
		fmt.Printf("\nWARNING: output is partial; one or more jobs failed in: %s\n", strings.Join(failedRegions, ", "))
	}
}

type fetchJob struct {
	Index        int
	ParentRegion string
	Region       bbox
	Kind         string
}

func buildFetchJobs(skipRestaurants bool) []fetchJob {
	_ = skipRestaurants // retained for compatibility with older call sites
	var jobs []fetchJob
	for _, region := range regions {
		jobs = append(jobs, fetchJob{Index: len(jobs), ParentRegion: region.Name, Region: region, Kind: "infrastructure"})
	}
	return jobs
}

func buildRestaurantAnchorJobs(places []place) []fetchJob {
	type cluster struct {
		Region string
		LatSum float64
		LonSum float64
		Count  int
	}
	clusters := map[string]*cluster{}
	for _, p := range places {
		if !isRestaurantAnchorCategory(p.Category) || !validCoord(p.Lat, p.Lon) {
			continue
		}
		regionName := regionNameForPoint(p.Lat, p.Lon)
		if regionName == "" {
			continue
		}
		// Roughly 2 km grid cells. Nearby facilities intentionally share one
		// restaurant query, keeping the number of Overpass calls manageable.
		latCell := int(math.Floor(p.Lat / 0.018))
		lonCell := int(math.Floor(p.Lon / 0.022))
		key := fmt.Sprintf("%s:%d:%d", regionName, latCell, lonCell)
		c := clusters[key]
		if c == nil {
			c = &cluster{Region: regionName}
			clusters[key] = c
		}
		c.LatSum += p.Lat
		c.LonSum += p.Lon
		c.Count++
	}
	keys := make([]string, 0, len(clusters))
	for k := range clusters {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	jobs := make([]fetchJob, 0, len(keys))
	perRegion := map[string]int{}
	for _, k := range keys {
		c := clusters[k]
		lat := c.LatSum / float64(c.Count)
		lon := c.LonSum / float64(c.Count)
		perRegion[c.Region]++
		name := fmt.Sprintf("%s anchor %02d", c.Region, perRegion[c.Region])
		b := bbox{Name: name, South: lat - 0.010, West: lon - 0.014, North: lat + 0.010, East: lon + 0.014, RestaurantRadius: 650}
		jobs = append(jobs, fetchJob{Index: len(jobs), ParentRegion: c.Region, Region: b, Kind: "restaurants"})
	}
	return jobs
}

func isRestaurantAnchorCategory(category string) bool {
	switch category {
	case "marinas", "boatyards", "fuel_docks", "launch_ramps", "yacht_clubs":
		return true
	}
	return false
}

func regionNameForPoint(lat, lon float64) string {
	for _, r := range regions {
		if lat >= r.South && lat <= r.North && lon >= r.West && lon <= r.East {
			return r.Name
		}
	}
	return ""
}

func classifyRestaurantCandidates(raw []rawCandidate, anchors []place) []place {
	out := make([]place, 0, len(raw))
	for _, r := range raw {
		if r.Tags["amenity"] != "restaurant" {
			continue
		}
		p := r.Place
		p.Category = "waterfront_restaurants"
		if restaurantLooksWaterfront(r.Tags, p, anchors) {
			out = append(out, p)
		}
	}
	return out
}

func regionMatchesRefresh(job fetchJob, refreshRegion string) bool {
	if strings.TrimSpace(refreshRegion) == "" {
		return false
	}
	return strings.EqualFold(job.ParentRegion, refreshRegion)
}

func splitRestaurantRegion(region bbox) []bbox {
	midLat := (region.South + region.North) / 2
	midLon := (region.West + region.East) / 2
	return []bbox{
		{Name: region.Name + " SW", South: region.South, West: region.West, North: midLat, East: midLon, RestaurantRadius: region.RestaurantRadius},
		{Name: region.Name + " SE", South: region.South, West: midLon, North: midLat, East: region.East, RestaurantRadius: region.RestaurantRadius},
		{Name: region.Name + " NW", South: midLat, West: region.West, North: region.North, East: midLon, RestaurantRadius: region.RestaurantRadius},
		{Name: region.Name + " NE", South: midLat, West: midLon, North: region.North, East: region.East, RestaurantRadius: region.RestaurantRadius},
	}
}

type fetchResult struct {
	Job        fetchJob
	Candidates []rawCandidate
	Err        error
}

func runFetchJob(ctx context.Context, client *http.Client, endpoints []string, cacheDir string, cacheTTL time.Duration, forceRefresh bool, job fetchJob) fetchResult {
	label := job.Region.Name + " / " + job.Kind
	cachePath := filepath.Join(cacheDir, cacheFileName(job))
	if !forceRefresh {
		if cached, ok := readJobCache(cachePath, cacheTTL); ok {
			fmt.Printf("%s: cache hit (%d candidates)\n", label, len(cached))
			return fetchResult{Job: job, Candidates: cached}
		}
	} else {
		fmt.Printf("%s: forced refresh\n", label)
	}
	fmt.Printf("Fetching %s...\n", label)
	candidates, err := fetchJobResilient(ctx, client, endpoints, job)
	if err != nil {
		return fetchResult{Job: job, Err: err}
	}
	fmt.Printf("%s: received %d candidate features\n", label, len(candidates))
	if err := writeJobCache(cachePath, candidates); err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: cache write %s: %v\n", cachePath, err)
	}
	return fetchResult{Job: job, Candidates: candidates}
}

func cacheFileName(job fetchJob) string {
	name := strings.ToLower(job.Region.Name + "-" + job.Kind)
	name = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(name, "-")
	name = strings.Trim(name, "-")
	return name + ".json"
}

func readJobCache(path string, ttl time.Duration) ([]rawCandidate, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, false
	}
	if ttl > 0 && time.Since(info.ModTime()) > ttl {
		return nil, false
	}
	b, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var candidates []rawCandidate
	if err := json.Unmarshal(b, &candidates); err != nil {
		return nil, false
	}
	// Caches created before v16 do not contain coordinate provenance fields.
	// Overpass candidates already carry geometry from the OSM object itself, so
	// repair that provenance when loading old cache files instead of reporting it
	// as unknown in the audit.
	for i := range candidates {
		p := &candidates[i].Place
		if p.CoordSource == "" && (p.Source == "OpenStreetMap" || strings.HasPrefix(p.SourceID, "osm:")) && validCoord(p.Lat, p.Lon) {
			p.CoordSource = "osm_geometry"
			p.CoordStatus = "source_provided"
		}
	}
	return candidates, true
}

func writeJobCache(path string, candidates []rawCandidate) error {
	return writeJSON(path, candidates)
}

func overpassEndpoints(primary string) []string {
	candidates := []string{
		primary,
		"https://overpass.private.coffee/api/interpreter",
		"https://maps.mail.ru/osm/tools/overpass/api/interpreter",
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(candidates))
	for _, ep := range candidates {
		ep = strings.TrimSpace(ep)
		if ep == "" || seen[ep] {
			continue
		}
		seen[ep] = true
		out = append(out, ep)
	}
	return out
}

func fetchJobResilient(ctx context.Context, client *http.Client, endpoints []string, job fetchJob) ([]rawCandidate, error) {
	const maxAttempts = 2
	var lastErr error
	if len(endpoints) == 0 {
		return nil, errors.New("no Overpass endpoints configured")
	}
	startEndpoint := job.Index % len(endpoints)
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		endpoint := endpoints[(startEndpoint+attempt-1)%len(endpoints)]
		fmt.Printf("  %s / %s attempt %d/%d via %s...\n", job.Region.Name, job.Kind, attempt, maxAttempts, endpointHost(endpoint))
		candidates, err := fetchJobOnce(ctx, client, endpoint, job)
		if err == nil {
			return candidates, nil
		}
		lastErr = err
		fmt.Printf("  attempt %d failed: %v\n", attempt, compactError(err))
		if !isRetryableOverpassError(err) {
			return nil, err
		}
		if attempt < maxAttempts {
			delay := time.Duration(attempt) * time.Second
			fmt.Printf("  retrying in %s on next endpoint...\n", delay)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}
	return nil, lastErr
}

func isRetryableOverpassError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, token := range []string{"http 429", "http 502", "http 503", "http 504", "timeout", "timed out", "temporary", "connection reset", "eof"} {
		if strings.Contains(msg, token) {
			return true
		}
	}
	return false
}

func compactError(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.TrimSpace(err.Error())
	if len(msg) > 240 {
		return msg[:240] + "..."
	}
	return msg
}

func endpointHost(endpoint string) string {
	s := strings.TrimPrefix(endpoint, "https://")
	s = strings.TrimPrefix(s, "http://")
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	return s
}

func fetchJobOnce(ctx context.Context, client *http.Client, endpoint string, job fetchJob) ([]rawCandidate, error) {
	q := buildJobQuery(job.Region, job.Kind)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewBufferString("data="+urlFormEscape(q)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "MauriWeatherWaterConditions-marineplacesgen/"+strings.TrimPrefix(generatorVersion, "v"))

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Overpass HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var decoded overpassResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, err
	}
	out := make([]rawCandidate, 0, len(decoded.Elements))
	for _, el := range decoded.Elements {
		lat, lon, ok := elementPoint(el)
		if !ok || el.Tags == nil {
			continue
		}
		name := firstNonEmpty(el.Tags["name"], el.Tags["brand"], el.Tags["operator"])
		if name == "" {
			continue
		}
		p := place{
			Name:        name,
			Lat:         lat,
			Lon:         lon,
			CoordSource: "osm_geometry",
			CoordStatus: "source_provided",
			City:        cityFromTags(el.Tags),
			Address:     addressFromTags(el.Tags),
			Website:     firstNonEmpty(el.Tags["website"], el.Tags["contact:website"]),
			Phone:       firstNonEmpty(el.Tags["phone"], el.Tags["contact:phone"]),
			Source:      "OpenStreetMap",
			SourceID:    fmt.Sprintf("osm:%s/%d", el.Type, el.ID),
		}
		out = append(out, rawCandidate{Place: p, Tags: el.Tags})
	}
	return out, nil
}

func buildJobQuery(b bbox, kind string) string {
	box := fmt.Sprintf("(%0.6f,%0.6f,%0.6f,%0.6f)", b.South, b.West, b.North, b.East)
	if kind == "restaurants" {
		return `[out:json][timeout:28];(` +
			`nwr["amenity"="restaurant"]["name"]` + box + `;` +
			`);out center tags;`
	}
	return `[out:json][timeout:28];(` +
		`nwr["leisure"="marina"]["name"]` + box + `;` +
		`nwr["leisure"="slipway"]["name"]` + box + `;` +
		`nwr["club"~"^(sailing|yacht)$",i]["name"]` + box + `;` +
		`nwr["sport"~"(sailing|yachting)",i]["name"]` + box + `;` +
		`nwr["shop"~"^(boat|marine|fishing)$",i]["name"]` + box + `;` +
		`nwr["craft"~"(boat|ship)",i]["name"]` + box + `;` +
		`nwr["industrial"~"(boatyard|shipyard|boat|ship)",i]["name"]` + box + `;` +
		`nwr["waterway"="fuel"]["name"]` + box + `;` +
		`nwr["seamark:type"="bunker_station"]["name"]` + box + `;` +
		`nwr["amenity"="fuel"]["boat"="yes"]["name"]` + box + `;` +
		`nwr["amenity"="fuel"]["fuel:marine_diesel"="yes"]["name"]` + box + `;` +
		`);out center tags;`
}

func classifyAndFilter(raw []rawCandidate) []place {
	var anchors []place
	for _, r := range raw {
		if categoryFor(r.Tags, r.Place.Name) == "marinas" {
			p := r.Place
			p.Category = "marinas"
			anchors = append(anchors, p)
		}
	}

	out := make([]place, 0, len(raw))
	for _, r := range raw {
		cat := categoryFor(r.Tags, r.Place.Name)
		if cat == "" {
			continue
		}
		p := r.Place
		p.Category = cat
		if cat == "waterfront_restaurants" {
			if !restaurantLooksWaterfront(r.Tags, p, anchors) {
				continue
			}
		}
		if cat == "fuel_docks" && !fuelLooksMarine(r.Tags, p, anchors) {
			continue
		}
		out = append(out, p)
	}
	return out
}

func categoryFor(tags map[string]string, name string) string {
	lowerName := strings.ToLower(strings.TrimSpace(name))
	if tags["leisure"] == "marina" {
		if clearlyNonMarinaName(lowerName) {
			return ""
		}
		return "marinas"
	}
	if tags["leisure"] == "slipway" {
		if clearlyNonBoatLaunchName(lowerName) {
			return ""
		}
		return "launch_ramps"
	}
	boatyardByName := regexp.MustCompile(`(?i)(boatyard|shipyard|boat yard|marine repair|boat repair|boatworks|boat works)`).MatchString(name)
	boatyardByTags := strings.Contains(strings.ToLower(tags["craft"]), "boat") ||
		strings.Contains(strings.ToLower(tags["craft"]), "ship") ||
		strings.Contains(strings.ToLower(tags["industrial"]), "boatyard") ||
		strings.Contains(strings.ToLower(tags["industrial"]), "shipyard")
	if boatyardByName || boatyardByTags {
		if !boatyardByName && clearlyGenericShipyardFeatureName(lowerName) {
			return ""
		}
		return "boatyards"
	}
	if tags["waterway"] == "fuel" || tags["seamark:type"] == "bunker_station" || tags["amenity"] == "fuel" {
		return "fuel_docks"
	}
	club := strings.ToLower(tags["club"])
	sport := strings.ToLower(tags["sport"])
	if club == "sailing" || club == "yacht" || strings.Contains(lowerName, "yacht club") || strings.Contains(lowerName, "sailing club") || strings.Contains(lowerName, "sailing center") || strings.Contains(lowerName, "sailing centre") {
		return "yacht_clubs"
	}
	// sport=sailing/yachting alone is too broad: surf shops, wind-sport schools,
	// and other businesses can carry the tag without being a yacht/sailing club.
	if (strings.Contains(sport, "sailing") || strings.Contains(sport, "yachting")) &&
		(strings.Contains(lowerName, "sailing") || strings.Contains(lowerName, "yacht")) {
		return "yacht_clubs"
	}
	shop := strings.ToLower(tags["shop"])
	if shop == "boat" || shop == "marine" || shop == "fishing" || strings.Contains(lowerName, "marine supply") || strings.Contains(lowerName, "boat supply") {
		return "marine_supply"
	}
	if tags["amenity"] == "restaurant" {
		return "waterfront_restaurants"
	}
	return ""
}

func clearlyNonBoatLaunchName(lowerName string) bool {
	for _, token := range []string{"kite launch", "kitesurf", "windsurf", "wing launch", "foil kite", "wing foil", "kayak launch", "kayaker", "canoe launch"} {
		if strings.Contains(lowerName, token) {
			return true
		}
	}
	return false
}

func clearlyNonMarinaName(lowerName string) bool {
	for _, token := range []string{"bike rental", "bicycle rental", "kayak rental", "canoe rental"} {
		if strings.Contains(lowerName, token) && !strings.Contains(lowerName, "marina") && !strings.Contains(lowerName, "harbor") && !strings.Contains(lowerName, "harbour") {
			return true
		}
	}
	return false
}

func clearlyGenericShipyardFeatureName(lowerName string) bool {
	if lowerName == "latrine" || strings.HasPrefix(lowerName, "parcel ") {
		return true
	}
	if regexp.MustCompile(`^pier[ -]?[0-9]+$`).MatchString(lowerName) {
		return true
	}
	return false
}

func restaurantLooksWaterfront(tags map[string]string, p place, marinas []place) bool {
	if yes(tags["waterfront"]) || yes(tags["outdoor_seating:waterfront"]) || tags["seamark:type"] != "" {
		return true
	}
	name := strings.ToLower(p.Name)
	for _, word := range []string{"waterfront", "harbor", "harbour", "marina", "wharf", "pier", "dock", "boathouse", "yacht", "ferry"} {
		if strings.Contains(name, word) {
			return true
		}
	}
	return nearestDistanceMeters(p, marinas) <= restaurantRadiusFor(p)
}

func restaurantRadiusFor(p place) float64 {
	for _, r := range regions {
		if p.Lat >= r.South && p.Lat <= r.North && p.Lon >= r.West && p.Lon <= r.East {
			return r.RestaurantRadius
		}
	}
	return 800
}

func fuelLooksMarine(tags map[string]string, p place, marinas []place) bool {
	if tags["waterway"] == "fuel" || tags["seamark:type"] == "bunker_station" || yes(tags["boat"]) || yes(tags["fuel:marine_diesel"]) {
		return true
	}
	name := strings.ToLower(p.Name)
	for _, word := range []string{"marina", "harbor", "harbour", "marine", "yacht", "dock", "boat"} {
		if strings.Contains(name, word) {
			return true
		}
	}
	return nearestDistanceMeters(p, marinas) <= 450
}

func nearestDistanceMeters(p place, candidates []place) float64 {
	best := math.Inf(1)
	for _, q := range candidates {
		d := haversineMeters(p.Lat, p.Lon, q.Lat, q.Lon)
		if d < best {
			best = d
		}
	}
	return best
}

func haversineMeters(lat1, lon1, lat2, lon2 float64) float64 {
	const earth = 6371000.0
	r1, r2 := lat1*math.Pi/180, lat2*math.Pi/180
	dlat := (lat2 - lat1) * math.Pi / 180
	dlon := (lon2 - lon1) * math.Pi / 180
	a := math.Sin(dlat/2)*math.Sin(dlat/2) + math.Cos(r1)*math.Cos(r2)*math.Sin(dlon/2)*math.Sin(dlon/2)
	return earth * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

func dedupePlaces(in []place) []place {
	byID := map[string]int{}
	var out []place
	for _, p := range in {
		if p.Name == "" || p.Category == "" || !validCoord(p.Lat, p.Lon) {
			continue
		}
		if p.SourceID != "" {
			if idx, ok := byID[p.SourceID]; ok {
				out[idx] = preferPlace(out[idx], p)
				continue
			}
		}
		dup := -1
		for i := range out {
			if out[i].Category == p.Category && normalizeName(out[i].Name) == normalizeName(p.Name) && haversineMeters(out[i].Lat, out[i].Lon, p.Lat, p.Lon) < 250 {
				dup = i
				break
			}
		}
		if dup >= 0 {
			out[dup] = preferPlace(out[dup], p)
			continue
		}
		out = append(out, p)
		if p.SourceID != "" {
			byID[p.SourceID] = len(out) - 1
		}
	}
	return out
}

func preferPlace(a, b place) place {
	if a.Address == "" && b.Address != "" {
		a.Address = b.Address
	}
	if a.City == "" && b.City != "" {
		a.City = b.City
	}
	if a.Website == "" && b.Website != "" {
		a.Website = b.Website
	}
	if a.Phone == "" && b.Phone != "" {
		a.Phone = b.Phone
	}
	if a.Note == "" && b.Note != "" {
		a.Note = b.Note
	}
	if a.SourceID == "" && b.SourceID != "" {
		a.SourceID = b.SourceID
	}
	if a.Source == "" && b.Source != "" {
		a.Source = b.Source
	}
	if a.CoordSource == "" && b.CoordSource != "" {
		a.CoordSource = b.CoordSource
		a.CoordStatus = b.CoordStatus
	}
	return a
}

func readCuratedRestaurants(ctx context.Context, client *http.Client, cacheDir, path string) ([]place, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var places []place
	var a asset
	if err := json.Unmarshal(data, &a); err == nil && len(a.Places) > 0 {
		places = a.Places
	} else if err := json.Unmarshal(data, &places); err != nil {
		return nil, fmt.Errorf("%s: expected a JSON array of places or an asset with a places array: %v", path, err)
	}
	out := make([]place, 0, len(places))
	geocoded := 0
	skipped := 0
	for _, p := range places {
		if strings.TrimSpace(p.Name) == "" {
			skipped++
			continue
		}
		p.Website = strings.TrimSpace(p.Website)
		if strings.HasPrefix(strings.ToLower(p.Website), "www.") {
			p.Website = "https://" + p.Website
		}
		if validCoord(p.Lat, p.Lon) {
			p.CoordSource = "curated_explicit"
			p.CoordStatus = "explicit"
		}
		if !validCoord(p.Lat, p.Lon) {
			addr := strings.TrimSpace(p.Address)
			if addr == "" {
				skipped++
				fmt.Fprintf(os.Stderr, "WARNING: curated restaurant %q has no coordinates or address; skipped\n", p.Name)
				continue
			}
			lat, lon, gerr := censusGeocode(ctx, client, cacheDir, addr)
			coordSource := "census_geocode"
			if gerr != nil {
				lat, lon, gerr = nominatimGeocode(ctx, client, cacheDir, addr)
				coordSource = "nominatim_geocode"
			}
			if gerr != nil {
				skipped++
				fmt.Fprintf(os.Stderr, "WARNING: curated restaurant %q geocode failed: %v\n", p.Name, gerr)
				continue
			}
			p.Lat, p.Lon = lat, lon
			p.CoordSource = coordSource
			p.CoordStatus = "needs_verification"
			geocoded++
		}
		p.Category = "waterfront_restaurants"
		if strings.TrimSpace(p.Source) == "" {
			p.Source = "curated restaurant import"
		}
		out = append(out, p)
	}
	if geocoded > 0 || skipped > 0 {
		fmt.Printf("Curated restaurant geocoding: %d geocoded, %d skipped\n", geocoded, skipped)
	}
	return out, nil
}

var nominatimLastRequest time.Time

func nominatimGeocode(ctx context.Context, client *http.Client, cacheDir, address string) (float64, float64, error) {
	key := slug(address)
	path := filepath.Join(cacheDir, "nominatim", key+".json")
	if b, err := ioutil.ReadFile(path); err == nil {
		var v struct{ Lat, Lon float64 }
		if json.Unmarshal(b, &v) == nil && validCoord(v.Lat, v.Lon) {
			return v.Lat, v.Lon, nil
		}
	}
	if d := time.Second - time.Since(nominatimLastRequest); d > 0 {
		time.Sleep(d)
	}
	u := "https://nominatim.openstreetmap.org/search?format=jsonv2&limit=1&countrycodes=us&q=" + url.QueryEscape(address)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, 0, err
	}
	req.Header.Set("User-Agent", "MauriWeatherWaterConditions-marineplacesgen/"+strings.TrimPrefix(generatorVersion, "v")+" (curated restaurant geocoder)")
	nominatimLastRequest = time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()
	b, err := ioutil.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return 0, 0, err
	}
	if resp.StatusCode != 200 {
		return 0, 0, fmt.Errorf("Nominatim HTTP %d", resp.StatusCode)
	}
	var rows []struct {
		Lat string `json:"lat"`
		Lon string `json:"lon"`
	}
	if err := json.Unmarshal(b, &rows); err != nil {
		return 0, 0, err
	}
	if len(rows) == 0 {
		return 0, 0, errors.New("no Nominatim geocode match")
	}
	lat, err1 := strconv.ParseFloat(rows[0].Lat, 64)
	lon, err2 := strconv.ParseFloat(rows[0].Lon, 64)
	if err1 != nil || err2 != nil || !validCoord(lat, lon) {
		return 0, 0, errors.New("invalid Nominatim geocode")
	}
	_ = writeJSON(path, struct{ Lat, Lon float64 }{lat, lon})
	return lat, lon, nil
}

func readOverrides(path string) (overrideFile, error) {
	var o overrideFile
	b, err := ioutil.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return o, nil
	}
	if err != nil {
		return o, err
	}
	if err := json.Unmarshal(b, &o); err != nil {
		return o, err
	}
	return o, nil
}

func applyOverrides(in []place, o overrideFile) []place {
	out := append([]place(nil), in...)
	filtered := out[:0]
	for _, p := range out {
		excluded := false
		for _, m := range o.Exclude {
			if matches(p, m) {
				excluded = true
				break
			}
		}
		if !excluded {
			filtered = append(filtered, p)
		}
	}
	out = filtered
	for _, patch := range o.Patch {
		for i := range out {
			if !matches(out[i], patch.Match) {
				continue
			}
			applyPatch(&out[i], patch.Set)
		}
	}
	for _, p := range o.Add {
		alreadyPresent := false
		for _, existing := range out {
			if samePinnedPlace(existing, p) {
				alreadyPresent = true
				break
			}
		}
		if alreadyPresent {
			continue
		}
		if p.Source == "" {
			p.Source = "local override"
		}
		if validCoord(p.Lat, p.Lon) && p.CoordSource == "" {
			p.CoordSource = "local_override"
			p.CoordStatus = "explicit_override"
		}
		out = append(out, p)
	}
	return out
}

// samePinnedPlace treats an Add override as an ensure-present fallback. If an
// upstream source already supplies the same logical place in the same category,
// keep the upstream record instead of appending a duplicate local marker.
func samePinnedPlace(a, b place) bool {
	if a.Category != b.Category {
		return false
	}
	if normalizeName(a.Name) != normalizeName(b.Name) {
		return false
	}
	if a.City != "" && b.City != "" && !strings.EqualFold(strings.TrimSpace(a.City), strings.TrimSpace(b.City)) {
		return false
	}
	return true
}

func matches(p place, m overrideMatch) bool {
	if m.SourceID != "" && p.SourceID != m.SourceID {
		return false
	}
	if m.Category != "" && p.Category != m.Category {
		return false
	}
	if m.Name != "" && normalizeName(p.Name) != normalizeName(m.Name) {
		return false
	}
	return m.SourceID != "" || m.Category != "" || m.Name != ""
}

func applyPatch(dst *place, src place) {
	if src.Name != "" {
		dst.Name = src.Name
	}
	if len(src.Aliases) != 0 {
		dst.Aliases = src.Aliases
	}
	if src.Category != "" {
		dst.Category = src.Category
	}
	if src.City != "" {
		dst.City = src.City
	}
	coordsPatched := false
	if src.Lat != 0 {
		dst.Lat = src.Lat
		coordsPatched = true
	}
	if src.Lon != 0 {
		dst.Lon = src.Lon
		coordsPatched = true
	}
	if coordsPatched && validCoord(dst.Lat, dst.Lon) {
		dst.CoordSource = "local_override"
		dst.CoordStatus = "explicit_override"
	}
	if src.Address != "" {
		dst.Address = src.Address
	}
	if src.Website != "" {
		dst.Website = src.Website
	}
	if src.Phone != "" {
		dst.Phone = src.Phone
	}
	if src.Note != "" {
		dst.Note = src.Note
	}
}

func sortPlaces(places []place) {
	sort.Slice(places, func(i, j int) bool {
		if places[i].Category != places[j].Category {
			return places[i].Category < places[j].Category
		}
		if places[i].City != places[j].City {
			return places[i].City < places[j].City
		}
		return strings.ToLower(places[i].Name) < strings.ToLower(places[j].Name)
	})
}

func buildCoordinateAudit(places []place, deltaAudit dbwDeltaAudit) coordinateAudit {
	a := coordinateAudit{
		GeneratorVersion: generatorVersion,
		Updated:          time.Now().UTC().Format("2006-01-02"),
		TotalPlaces:      len(places),
		DBWDelta:         deltaAudit,
		Records:          make([]coordinateAuditRecord, 0, len(places)),
	}
	for _, p := range places {
		coordSource := strings.TrimSpace(p.CoordSource)
		status := strings.TrimSpace(p.CoordStatus)
		if status == "" {
			if coordSource == "" {
				status = "unknown_provenance"
			} else if strings.Contains(coordSource, "geocode") {
				status = "needs_verification"
			} else {
				status = "source_provided"
			}
		}
		if status == "needs_verification" {
			a.NeedsVerification++
		}
		if status == "unknown_provenance" {
			a.UnknownProvenance++
		}
		a.Records = append(a.Records, coordinateAuditRecord{
			Name: p.Name, Category: p.Category, City: p.City, Lat: p.Lat, Lon: p.Lon,
			Address: p.Address, CoordSource: coordSource, Status: status, Source: p.Source, SourceID: p.SourceID,
		})
	}
	sort.Slice(a.Records, func(i, j int) bool {
		ri, rj := a.Records[i], a.Records[j]
		priority := func(s string) int {
			if s == "needs_verification" {
				return 0
			}
			if s == "unknown_provenance" {
				return 1
			}
			return 2
		}
		pi, pj := priority(ri.Status), priority(rj.Status)
		if pi != pj {
			return pi < pj
		}
		if ri.Category != rj.Category {
			return ri.Category < rj.Category
		}
		if ri.City != rj.City {
			return ri.City < rj.City
		}
		return strings.ToLower(ri.Name) < strings.ToLower(rj.Name)
	})
	return a
}

func writeJSON(path string, v interface{}) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return ioutil.WriteFile(path, b, 0o644)
}

func elementPoint(el overpassElement) (float64, float64, bool) {
	if validCoord(el.Lat, el.Lon) {
		return el.Lat, el.Lon, true
	}
	if el.Center != nil && validCoord(el.Center.Lat, el.Center.Lon) {
		return el.Center.Lat, el.Center.Lon, true
	}
	return 0, 0, false
}

func validCoord(lat, lon float64) bool {
	return lat >= -90 && lat <= 90 && lon >= -180 && lon <= 180 && !(lat == 0 && lon == 0)
}
func yes(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return s == "yes" || s == "true" || s == "1"
}
func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}
func normalizeName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(s, " ")
	return strings.Join(strings.Fields(s), " ")
}

func cityFromTags(t map[string]string) string {
	city := firstNonEmpty(t["addr:city"], t["addr:place"])
	state := t["addr:state"]
	if city != "" && state != "" {
		return city + ", " + state
	}
	return city
}

func addressFromTags(t map[string]string) string {
	var first string
	if t["addr:housenumber"] != "" || t["addr:street"] != "" {
		first = strings.TrimSpace(t["addr:housenumber"] + " " + t["addr:street"])
	}
	var parts []string
	if first != "" {
		parts = append(parts, first)
	}
	if t["addr:city"] != "" {
		parts = append(parts, t["addr:city"])
	}
	if t["addr:state"] != "" {
		parts = append(parts, t["addr:state"])
	}
	if t["addr:postcode"] != "" {
		parts = append(parts, t["addr:postcode"])
	}
	return strings.Join(parts, ", ")
}

func urlFormEscape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' || c == '~':
			b.WriteByte(c)
		case c == ' ':
			b.WriteByte('+')
		default:
			b.WriteString("%")
			b.WriteString(strings.ToUpper(strconv.FormatInt(int64(c), 16)))
		}
	}
	return b.String()
}

type dbwFacility struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Address     string   `json:"address"`
	City        string   `json:"city"`
	Phone       string   `json:"phone,omitempty"`
	Website     string   `json:"website,omitempty"`
	Services    []string `json:"services,omitempty"`
	Lat         float64  `json:"lat,omitempty"`
	Lon         float64  `json:"lon,omitempty"`
	CoordSource string   `json:"coord_source,omitempty"`
	CoordStatus string   `json:"coord_status,omitempty"`
}

var dbwCities = []string{
	"Alameda", "Alviso", "Antioch", "Bay Point", "Belvedere", "Benicia", "Berkeley", "Bethel Island",
	"Bodega Bay", "Brisbane", "Burlingame", "Emeryville", "Foster City", "Half Moon Bay", "Martinez", "Mill Valley", "Monterey", "Moss Landing", "Pacific Grove",
	"Oakland", "Pacifica", "Pittsburg", "Redwood City", "Richmond", "San Francisco", "San Leandro",
	"San Mateo", "Santa Cruz", "Sausalito", "South San Francisco", "Suisun City", "Vallejo",
}

// deltaBodyFallbacks are used only if the DBW body-of-water index cannot be
// parsed. Normal v19 operation discovers the current list directly from DBW.
var deltaBodyFallbacks = []string{
	"Sacramento-SanJoaquinDelta",
	"Sacramento-SanJoaquinDelta-Clifton Court",
	"Sacramento-SanJoaquinDelta-Consumnes River",
	"Sacramento-SanJoaquinDelta-Deep Water Channel",
	"Sacramento-SanJoaquinDelta-FourteenmileSlough",
	"Sacramento-SanJoaquinDelta-Franks Tract",
	"Sacramento-SanJoaquinDelta-Georgiana Slough",
	"Sacramento-SanJoaquinDelta-LittlePotatoSlough",
	"Sacramento-SanJoaquinDelta-Middle River",
	"Sacramento-SanJoaquinDelta-Miner Slough",
	"Sacramento-SanJoaquinDelta-Mokelumne River",
	"Sacramento-SanJoaquinDelta-Sacramento River",
	"Sacramento-SanJoaquinDelta-San Joaquin River",
	"Sacramento-SanJoaquinDelta-Seven Mile Slough",
	"Sacramento-SanJoaquinDelta-SevenMile Slough",
	"Sacramento-SanJoaquinDelta-Snodgrass Slough",
	"Sacramento-SanJoaquinDelta-Steamboat Slough",
	"Sacramento-San Joaquin Delta-Steamboat Slough",
	"Sacramento-SanJoaquinDelta-Suisun Bay",
	"Sacramento-SanJoaquinDelta-Taylor Slough",
	"Sacramento-SanJoaquinDelta-Three Mile Slough",
	"Sacramento-SanJoaquinDelta-Turner Cut",
}

func fetchDBWPlaces(ctx context.Context, client *http.Client, cacheDir string, forceRefresh bool) ([]place, dbwDeltaAudit, error) {
	ids := map[string]bool{}
	deltaIDs := map[string]bool{}
	var errs []string

	// Preserve the existing city-based pass for Bay/coastal coverage.
	for _, city := range dbwCities {
		cityIDs, err := fetchDBWCityFacilityIDs(ctx, client, cacheDir, city, forceRefresh)
		if err != nil {
			errs = append(errs, city+": "+err.Error())
			continue
		}
		for _, id := range cityIDs {
			ids[id] = true
		}
	}

	// v19: the Delta is discovered from DBW's authoritative Body-of-Water
	// hierarchy rather than from a short list of incorporated/community names.
	deltaList, bodyPages, err := fetchDBWDeltaFacilityIDs(ctx, client, cacheDir, forceRefresh)
	if err != nil {
		errs = append(errs, "Delta body-of-water discovery: "+err.Error())
	}
	for _, id := range deltaList {
		ids[id] = true
		deltaIDs[id] = true
	}

	audit := dbwDeltaAudit{BodyPages: bodyPages, FacilityIDs: len(deltaIDs)}
	var ordered []string
	for id := range ids {
		ordered = append(ordered, id)
	}
	sort.Strings(ordered)
	fmt.Printf("  DBW discovered %d unique facility records (%d Delta IDs across %d body pages; %d legacy cities)\n", len(ordered), len(deltaIDs), bodyPages, len(dbwCities))

	var out []place
	for i, id := range ordered {
		f, err := fetchDBWFacility(ctx, client, cacheDir, id)
		if err != nil {
			errs = append(errs, "facility "+id+": "+err.Error())
			continue
		}
		isDelta := deltaIDs[id]
		if isDelta {
			audit.FacilitiesFetched++
		}
		if validCoord(f.Lat, f.Lon) && strings.TrimSpace(f.CoordSource) == "" {
			f.CoordSource = "dbw_listing"
			f.CoordStatus = "source_provided"
		}
		if !validCoord(f.Lat, f.Lon) && strings.TrimSpace(f.Address) != "" {
			lat, lon, err := censusGeocode(ctx, client, cacheDir, f.Address)
			if err == nil {
				f.Lat, f.Lon = lat, lon
				f.CoordSource = "census_geocode"
				f.CoordStatus = "needs_verification"
			}
		}
		if !validCoord(f.Lat, f.Lon) {
			if isDelta {
				audit.SkippedNoCoordinates++
				audit.SkippedFacilities = append(audit.SkippedFacilities, dbwDeltaSkippedFacility{
					ID: f.ID, Name: f.Name, City: f.City, Address: f.Address, Type: f.Type,
					Services: append([]string(nil), f.Services...), Reason: "no_coordinates",
				})
			}
			continue
		}
		facilityPlaces := dbwFacilityPlaces(f)
		if len(facilityPlaces) == 0 {
			if isDelta {
				audit.SkippedNoRecognizedCategory++
				audit.SkippedFacilities = append(audit.SkippedFacilities, dbwDeltaSkippedFacility{
					ID: f.ID, Name: f.Name, City: f.City, Address: f.Address, Type: f.Type,
					Services: append([]string(nil), f.Services...), Reason: "no_recognized_category",
				})
			}
			continue
		}
		if isDelta {
			audit.FacilitiesRepresented++
		}
		out = append(out, facilityPlaces...)
		if (i+1)%25 == 0 {
			fmt.Printf("  DBW processed %d/%d facilities\n", i+1, len(ordered))
		}
	}
	if len(errs) > 0 {
		return out, audit, fmt.Errorf("%d DBW fetch/geocode operations failed; first: %s", len(errs), errs[0])
	}
	return out, audit, nil
}

func fetchDBWCityFacilityIDs(ctx context.Context, client *http.Client, cacheDir, city string, forceRefresh bool) ([]string, error) {
	cachePath := filepath.Join(cacheDir, "dbw", "city-"+slug(city)+".html")
	body, err := cachedHTTPGetMaybeRefresh(ctx, client, cachePath, "https://dbw.parks.ca.gov/BoatingFacilities/City/"+url.PathEscape(city), forceRefresh)
	if err != nil {
		return nil, err
	}
	return extractDBWFacilityIDs(body), nil
}

func fetchDBWDeltaFacilityIDs(ctx context.Context, client *http.Client, cacheDir string, forceRefresh bool) ([]string, int, error) {
	indexURL := "https://dbw.parks.ca.gov/BoatingFacilities/Body-of-Water"
	indexPath := filepath.Join(cacheDir, "dbw", "body-of-water-index.html")
	body, indexErr := cachedHTTPGetMaybeRefresh(ctx, client, indexPath, indexURL, forceRefresh)

	bodyNames := []string{}
	if indexErr == nil {
		bodyNames = extractDeltaBodyNames(body)
	}
	if len(bodyNames) == 0 {
		bodyNames = append(bodyNames, deltaBodyFallbacks...)
	}
	bodyNames = uniqueStrings(bodyNames)

	ids := map[string]bool{}
	var errs []string
	if indexErr != nil {
		errs = append(errs, "body-of-water index: "+indexErr.Error())
	}
	pagesFetched := 0
	for _, bodyName := range bodyNames {
		cachePath := filepath.Join(cacheDir, "dbw", "body-"+slug(bodyName)+".html")
		u := "https://dbw.parks.ca.gov/BoatingFacilities/Body-of-Water/" + url.PathEscape(bodyName)
		page, err := cachedHTTPGetMaybeRefresh(ctx, client, cachePath, u, forceRefresh)
		if err != nil {
			errs = append(errs, bodyName+": "+err.Error())
			continue
		}
		pagesFetched++
		for _, id := range extractDBWFacilityIDs(page) {
			ids[id] = true
		}
	}
	var out []string
	for id := range ids {
		out = append(out, id)
	}
	sort.Strings(out)
	if len(errs) > 0 {
		return out, pagesFetched, fmt.Errorf("%d Delta DBW pages/index operations failed; first: %s", len(errs), errs[0])
	}
	return out, pagesFetched, nil
}

func extractDBWFacilityIDs(body []byte) []string {
	re := regexp.MustCompile(`(?i)href=["'][^"']*/BoatingFacilities/f/([0-9]+)["']`)
	m := re.FindAllStringSubmatch(string(body), -1)
	seen := map[string]bool{}
	var ids []string
	for _, x := range m {
		if len(x) > 1 && !seen[x[1]] {
			seen[x[1]] = true
			ids = append(ids, x[1])
		}
	}
	return ids
}

func extractDeltaBodyNames(body []byte) []string {
	re := regexp.MustCompile(`(?i)href=["']([^"']*/BoatingFacilities/Body-of-Water/[^"'#?]+)["']`)
	seen := map[string]bool{}
	var names []string
	for _, m := range re.FindAllStringSubmatch(string(body), -1) {
		if len(m) < 2 {
			continue
		}
		href := html.UnescapeString(strings.TrimSpace(m[1]))
		u, err := url.Parse(href)
		if err != nil {
			continue
		}
		path := u.Path
		marker := "/BoatingFacilities/Body-of-Water/"
		idx := strings.Index(path, marker)
		if idx < 0 {
			continue
		}
		name, err := url.PathUnescape(strings.TrimPrefix(path[idx:], marker))
		if err != nil || !isDeltaBodyName(name) {
			continue
		}
		name = strings.TrimSpace(name)
		if name != "" && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func isDeltaBodyName(name string) bool {
	normalized := strings.ToLower(regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(name, ""))
	return strings.HasPrefix(normalized, "sacramentosanjoaquindelta")
}

func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func cachedHTTPGetMaybeRefresh(ctx context.Context, client *http.Client, cachePath, u string, forceRefresh bool) ([]byte, error) {
	if forceRefresh {
		_ = os.Remove(cachePath)
	}
	return cachedHTTPGet(ctx, client, cachePath, u)
}

func fetchDBWFacility(ctx context.Context, client *http.Client, cacheDir, id string) (dbwFacility, error) {
	jsonPath := filepath.Join(cacheDir, "dbw", "facility-"+id+".json")
	if b, err := ioutil.ReadFile(jsonPath); err == nil {
		var f dbwFacility
		if json.Unmarshal(b, &f) == nil && f.Name != "" {
			return f, nil
		}
	}
	body, err := cachedHTTPGet(ctx, client, filepath.Join(cacheDir, "dbw", "facility-"+id+".html"), "https://dbw.parks.ca.gov/BoatingFacilities/f/"+id)
	if err != nil {
		return dbwFacility{}, err
	}
	text := htmlText(string(body))
	f := dbwFacility{ID: id}
	f.Name = extractHeading(string(body))
	f.Address = extractLabelBlock(text, "Facility Address:", []string{"Phone:", "Website:", "Body of Water:", "County:", "Type of Facility:"})
	f.Phone = extractLabelValue(text, "Phone:")
	f.Website = extractLabelValue(text, "Website:")
	f.Type = extractLabelValue(text, "Type of Facility:")
	f.City = cityFromAddress(f.Address)
	f.Services = extractServiceLines(text)
	if lat, lon, ok := extractLatLon(string(body)); ok {
		f.Lat, f.Lon = lat, lon
		f.CoordSource = "dbw_listing"
		f.CoordStatus = "source_provided"
	}
	if f.Name == "" {
		return f, errors.New("unable to parse facility name")
	}
	if err := writeJSON(jsonPath, f); err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: DBW facility cache write: %v\n", err)
	}
	return f, nil
}

func cachedHTTPGet(ctx context.Context, client *http.Client, cachePath, u string) ([]byte, error) {
	if b, err := ioutil.ReadFile(cachePath); err == nil && len(b) > 0 {
		return b, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "MauriWeatherWaterConditions-marineplacesgen/"+strings.TrimPrefix(generatorVersion, "v"))
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := ioutil.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0755); err == nil {
		_ = ioutil.WriteFile(cachePath, b, 0644)
	}
	return b, nil
}

func htmlText(s string) string {
	s = regexp.MustCompile(`(?i)<br\s*/?>`).ReplaceAllString(s, "\n")
	s = regexp.MustCompile(`(?i)</(p|div|li|h[1-6]|tr|td|th)>`).ReplaceAllString(s, "\n")
	s = regexp.MustCompile(`<[^>]+>`).ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	s = strings.ReplaceAll(s, "\r", "")
	lines := strings.Split(s, "\n")
	var out []string
	for _, line := range lines {
		line = strings.TrimSpace(regexp.MustCompile(`\s+`).ReplaceAllString(line, " "))
		if line != "" {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

func extractHeading(raw string) string {
	re := regexp.MustCompile(`(?is)<h1[^>]*>(.*?)</h1>`)
	m := re.FindStringSubmatch(raw)
	if len(m) < 2 {
		return ""
	}
	return strings.TrimSpace(html.UnescapeString(regexp.MustCompile(`<[^>]+>`).ReplaceAllString(m[1], "")))
}

func extractLabelValue(text, label string) string {
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		if strings.TrimSpace(l) == label && i+1 < len(lines) {
			return strings.TrimSpace(lines[i+1])
		}
		if strings.HasPrefix(l, label) {
			return strings.TrimSpace(strings.TrimPrefix(l, label))
		}
	}
	return ""
}
func extractLabelBlock(text, label string, stops []string) string {
	lines := strings.Split(text, "\n")
	start := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == label {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return ""
	}
	var vals []string
	for i := start; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		stop := false
		for _, x := range stops {
			if line == x || strings.HasPrefix(line, x) {
				stop = true
				break
			}
		}
		if stop {
			break
		}
		vals = append(vals, line)
	}
	return strings.Join(vals, ", ")
}
func extractServiceLines(text string) []string {
	lines := strings.Split(text, "\n")
	start := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == "Services" {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return nil
	}
	var out []string
	for i := start; i < len(lines); i++ {
		l := strings.TrimSpace(lines[i])
		if l == "Environmental Services" || strings.HasPrefix(l, "Mailing Address:") {
			break
		}
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}
func extractLatLon(raw string) (float64, float64, bool) {
	// Decimal latitude/longitude pairs sometimes appear in embedded map data.
	re := regexp.MustCompile(`(?i)(3[6-8]\.[0-9]{3,})[^0-9-]+(-12[1-3]\.[0-9]{3,})`)
	m := re.FindStringSubmatch(raw)
	if len(m) < 3 {
		return 0, 0, false
	}
	lat, e1 := strconv.ParseFloat(m[1], 64)
	lon, e2 := strconv.ParseFloat(m[2], 64)
	return lat, lon, e1 == nil && e2 == nil
}
func cityFromAddress(a string) string {
	parts := strings.Split(a, ",")
	if len(parts) >= 2 {
		return strings.TrimSpace(parts[len(parts)-2]) + ", CA"
	}
	return ""
}
func dbwFacilityPlaces(f dbwFacility) []place {
	base := place{Name: f.Name, City: f.City, Lat: f.Lat, Lon: f.Lon, Address: f.Address, Website: f.Website, Phone: f.Phone, Source: "California State Parks/DBW", SourceID: "dbw:" + f.ID, CoordSource: f.CoordSource, CoordStatus: f.CoordStatus}
	cats := map[string]bool{}
	lt := strings.ToLower(f.Type)
	ln := strings.ToLower(strings.TrimSpace(f.Name))
	if strings.Contains(lt, "marina") {
		cats["marinas"] = true
	}
	if strings.Contains(lt, "launch") || strings.Contains(lt, "boating access") {
		cats["launch_ramps"] = true
	}
	if strings.Contains(lt, "yacht club") {
		cats["yacht_clubs"] = true
	}
	if strings.Contains(lt, "dry storage") || strings.Contains(lt, "drystorage") {
		cats["boatyards"] = true
	}
	// A facility explicitly named as a dock is useful to a boater even when DBW
	// uses the generic NoFacility type. Do not infer marina status merely from a
	// name containing "marina", because DBW also lists planning/redevelopment records.
	if strings.Contains(ln, " dock") || strings.HasSuffix(ln, "dock") {
		cats["marinas"] = true
	}
	for _, svc := range f.Services {
		x := strings.ToLower(strings.TrimSpace(svc))
		if strings.Contains(x, "haul out") || strings.Contains(x, "boat repair") || strings.Contains(x, "dry dock") || strings.Contains(x, "dry storage") {
			cats["boatyards"] = true
		}
		if strings.Contains(x, "fuel sales") || strings.Contains(x, "marine fuel") {
			cats["fuel_docks"] = true
		}
		if strings.Contains(x, "fishing tackle") || strings.Contains(x, "bait sales") || strings.Contains(x, "marine suppl") || strings.Contains(x, "chandlery") {
			cats["marine_supply"] = true
		}
		if strings.Contains(x, "transient berth") || strings.Contains(x, "tie up") || strings.Contains(x, "tie-up") || strings.Contains(x, "guest dock") {
			cats["marinas"] = true
		}
		if strings.Contains(x, "launching valet") || strings.Contains(x, "launch valet") {
			cats["launch_ramps"] = true
		}
		if x == "restaurant" || strings.Contains(x, "restaurant") {
			cats["waterfront_restaurants"] = true
		}
	}
	var out []place
	for cat := range cats {
		p := base
		p.Category = cat
		p.SourceID = base.SourceID + ":" + cat
		out = append(out, p)
	}
	return out
}

func censusGeocode(ctx context.Context, client *http.Client, cacheDir, address string) (float64, float64, error) {
	key := slug(address)
	path := filepath.Join(cacheDir, "census", key+".json")
	if b, err := ioutil.ReadFile(path); err == nil {
		var v struct{ Lat, Lon float64 }
		if json.Unmarshal(b, &v) == nil && validCoord(v.Lat, v.Lon) {
			return v.Lat, v.Lon, nil
		}
	}
	u := "https://geocoding.geo.census.gov/geocoder/locations/onelineaddress?benchmark=Public_AR_Current&format=json&address=" + url.QueryEscape(address)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, 0, err
	}
	req.Header.Set("User-Agent", "MauriWeatherWaterConditions-marineplacesgen/"+strings.TrimPrefix(generatorVersion, "v"))
	resp, err := client.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()
	b, err := ioutil.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return 0, 0, err
	}
	if resp.StatusCode != 200 {
		return 0, 0, fmt.Errorf("Census HTTP %d", resp.StatusCode)
	}
	var decoded struct {
		Result struct {
			AddressMatches []struct {
				Coordinates struct {
					X float64 `json:"x"`
					Y float64 `json:"y"`
				} `json:"coordinates"`
			} `json:"addressMatches"`
		} `json:"result"`
	}
	if err := json.Unmarshal(b, &decoded); err != nil {
		return 0, 0, err
	}
	if len(decoded.Result.AddressMatches) == 0 {
		return 0, 0, errors.New("no Census geocode match")
	}
	lat := decoded.Result.AddressMatches[0].Coordinates.Y
	lon := decoded.Result.AddressMatches[0].Coordinates.X
	if !validCoord(lat, lon) {
		return 0, 0, errors.New("invalid Census geocode")
	}
	_ = writeJSON(path, struct{ Lat, Lon float64 }{lat, lon})
	return lat, lon, nil
}
func slug(s string) string {
	s = strings.ToLower(s)
	s = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "marineplacesgen %s: "+format+"\n", append([]interface{}{generatorVersion}, args...)...)
	os.Exit(1)
}
