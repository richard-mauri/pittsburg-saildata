package main

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"encoding/xml"
	"flag"
	"fmt"
	"html/template"
	"image"
	"image/color"
	"image/png"
	"io"
	"io/ioutil"
	"log"
	"math"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	appVersion                      = "1.16.0"
	buildVersion                    = "v393"
	defaultWindStation              = "PSBC1"
	windDistanceWarningNM           = 10.0
	defaultCurrentDistanceWarningNM = 15.0
	maxAutoCurrentStationDistanceNM = 30.0
	// Tidal-range context thresholds are relative to the surrounding lunar-cycle median.
	elevatedTideRangePercent      = 15.0
	largeTideRangePercent         = 30.0
	exceptionalTideRangePercent   = 45.0
	defaultStartHour              = -1
	defaultEndHour                = -1
	defaultWindReadingHours       = 4
	maxRecentWindObservationCount = 400
	timeZoneName                  = "America/Los_Angeles"
)

type SailingReport struct {
	Station            string                `json:"station"`
	ReportTime         time.Time             `json:"report_time"`
	Latest             *WindObservation      `json:"latest,omitempty"`
	Latest10           []WindObservation     `json:"latest_10,omitempty"`
	Last12Hours        *WindStats            `json:"last_12_hours,omitempty"`
	Afternoon          []PeriodReport        `json:"afternoon,omitempty"`
	Current            *CurrentReport        `json:"current,omitempty"`
	Historical         *HistoricalReport     `json:"historical,omitempty"`
	WindSelection      *WindStationSelection `json:"wind_selection,omitempty"`
	DebugWindSelection bool                  `json:"-"`
	WindError          string                `json:"wind_error,omitempty"`
	RequestQuery       url.Values            `json:"-"`
}

type CompactReport struct {
	Station    string          `json:"station"`
	Location   string          `json:"location,omitempty"`
	ReportTime time.Time       `json:"report_time"`
	BottomLine []string        `json:"bottom_line,omitempty"`
	Wind       *CompactWind    `json:"wind,omitempty"`
	Current    *CompactCurrent `json:"current,omitempty"`
}

type CompactWind struct {
	Time      time.Time `json:"time"`
	Direction string    `json:"direction,omitempty"`
	WindKT    float64   `json:"wind_kt,omitempty"`
	GustKT    float64   `json:"gust_kt,omitempty"`
}

type CompactCurrent struct {
	WindowStart  time.Time `json:"window_start,omitempty"`
	WindowEnd    time.Time `json:"window_end,omitempty"`
	PhaseAtStart string    `json:"phase_at_start,omitempty"`
	SlackTime    time.Time `json:"slack_time,omitempty"`
	NextPhase    string    `json:"next_phase,omitempty"`
	NextSpeedKT  float64   `json:"next_speed_kt,omitempty"`
	Strength     string    `json:"strength,omitempty"`
	StationID    string    `json:"station_id,omitempty"`
	StationName  string    `json:"station_name,omitempty"`
	DistanceNM   float64   `json:"distance_nm,omitempty"`
	Error        string    `json:"error,omitempty"`
}

type SaildroneObservation struct {
	DatasetID        string  `json:"dataset_id"`
	DroneID          string  `json:"drone_id"`
	Mission          string  `json:"mission"`
	Time             string  `json:"time"`
	Latitude         float64 `json:"latitude"`
	Longitude        float64 `json:"longitude"`
	WindDirectionDeg float64 `json:"wind_direction_deg,omitempty"`
	WindSpeedMPS     float64 `json:"wind_speed_mps,omitempty"`
	WaterTempC       float64 `json:"water_temp_c,omitempty"`
	SalinityPSU      float64 `json:"salinity_psu,omitempty"`
	CurrentSpeedMPS  float64 `json:"current_speed_mps,omitempty"`
	CurrentDirDeg    float64 `json:"current_direction_deg,omitempty"`
	WaveHeightM      float64 `json:"wave_height_m,omitempty"`
	WavePeriodS      float64 `json:"wave_period_s,omitempty"`
	AgeHours         float64 `json:"age_hours,omitempty"`
}

type SaildroneFeed struct {
	Updated      string                 `json:"updated"`
	Source       string                 `json:"source"`
	Observations []SaildroneObservation `json:"observations"`
	Errors       []string               `json:"errors,omitempty"`
}

var (
	saildroneCacheMu      sync.Mutex
	saildroneCacheAt      time.Time
	saildroneCachePayload SaildroneFeed

	swellUpstreamClient = &http.Client{
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: 8 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			MaxIdleConns:          24,
			MaxIdleConnsPerHost:   10,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 8 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
		Timeout: 18 * time.Second,
	}

	swellFrameCache = struct {
		sync.RWMutex
		items    map[string]swellFrameCacheEntry
		inflight map[string]chan struct{}
	}{
		items:    make(map[string]swellFrameCacheEntry),
		inflight: make(map[string]chan struct{}),
	}

	swellPrefetchSlots = make(chan struct{}, 2)
)

type swellFrameCacheEntry struct {
	Raw      []byte
	StoredAt time.Time
}

const swellFrameCacheTTL = 45 * time.Minute

func saildroneFloat(v interface{}) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, !math.IsNaN(x) && !math.IsInf(x, 0)
	case json.Number:
		f, err := x.Float64()
		return f, err == nil && !math.IsNaN(f) && !math.IsInf(f, 0)
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return f, err == nil && !math.IsNaN(f) && !math.IsInf(f, 0)
	default:
		return 0, false
	}
}

func fetchSaildroneVariableNames(client *http.Client, datasetID string) (map[string]bool, error) {
	remoteURL := "https://data.pmel.noaa.gov/generic/erddap/info/" +
		url.PathEscape(datasetID) + "/index.json"

	req, err := http.NewRequest(http.MethodGet, remoteURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "pittsburg-saildata/"+appVersion)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(io.LimitReader(resp.Body, 3<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		detail := strings.TrimSpace(string(body))
		if len(detail) > 240 {
			detail = detail[:240]
		}
		return nil, fmt.Errorf("metadata HTTP %d: %s", resp.StatusCode, detail)
	}

	var payload struct {
		Table struct {
			ColumnNames []string        `json:"columnNames"`
			Rows        [][]interface{} `json:"rows"`
		} `json:"table"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}

	rowTypeIndex := -1
	variableNameIndex := -1
	for i, name := range payload.Table.ColumnNames {
		switch strings.TrimSpace(strings.ToLower(name)) {
		case "row type":
			rowTypeIndex = i
		case "variable name":
			variableNameIndex = i
		}
	}
	if variableNameIndex < 0 {
		return nil, fmt.Errorf("metadata response has no Variable Name column")
	}

	names := make(map[string]bool)
	for _, row := range payload.Table.Rows {
		if variableNameIndex >= len(row) || row[variableNameIndex] == nil {
			continue
		}
		if rowTypeIndex >= 0 && rowTypeIndex < len(row) && row[rowTypeIndex] != nil {
			rowType := strings.ToLower(strings.TrimSpace(fmt.Sprint(row[rowTypeIndex])))
			if rowType != "variable" {
				continue
			}
		}
		name := strings.TrimSpace(fmt.Sprint(row[variableNameIndex]))
		if name != "" {
			names[name] = true
		}
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("metadata response listed no variables")
	}
	return names, nil
}

func firstSaildroneVariable(available map[string]bool, candidates ...string) string {
	for _, candidate := range candidates {
		if available[candidate] {
			return candidate
		}
	}
	return ""
}

func fetchLatestSaildroneObservation(client *http.Client, datasetID, mission string) (SaildroneObservation, error) {
	var result SaildroneObservation
	result.DatasetID = datasetID
	result.Mission = mission

	available, metadataErr := fetchSaildroneVariableNames(client, datasetID)
	if metadataErr != nil {
		// Keep the platform position useful even if the metadata endpoint has a
		// temporary problem. The ERDDAP coordinate/time variables are standard.
		available = map[string]bool{
			"time":      true,
			"latitude":  true,
			"longitude": true,
		}
	}

	required := []string{"time", "latitude", "longitude"}
	for _, name := range required {
		if !available[name] {
			return result, fmt.Errorf("dataset schema is missing required variable %q", name)
		}
	}

	droneIDVar := firstSaildroneVariable(
		available,
		"drone_id",
		"saildrone_id",
		"platform_id",
	)

	windDirectionVar := firstSaildroneVariable(
		available,
		"wind_direction_world_filtered",
		"wind_direction",
		"WIND_DIRECTION_MEAN",
		"wind_dir",
	)
	windSpeedVar := firstSaildroneVariable(
		available,
		"wind_speed_world_filtered",
		"wind_speed",
		"WIND_SPEED_MEAN",
	)
	waterTempVar := firstSaildroneVariable(
		available,
		"sbe37_temperature_filtered",
		"sea_water_temperature",
		"water_temperature",
		"TEMP_CTD_MEAN",
	)
	salinityVar := firstSaildroneVariable(
		available,
		"sbe37_practical_salinity_filtered",
		"sea_water_practical_salinity",
		"practical_salinity",
		"SAL_CTD_MEAN",
	)
	currentSpeedVar := firstSaildroneVariable(
		available,
		"water_current_speed_filtered",
		"water_current_speed",
		"current_speed",
	)
	currentDirectionVar := firstSaildroneVariable(
		available,
		"water_current_direction_filtered",
		"water_current_direction",
		"current_direction",
	)
	waveHeightVar := firstSaildroneVariable(
		available,
		"wave_significant_height",
		"significant_wave_height",
		"WAVE_SIGNIFICANT_HEIGHT",
	)
	wavePeriodVar := firstSaildroneVariable(
		available,
		"wave_dominant_period",
		"dominant_wave_period",
		"WAVE_DOMINANT_PERIOD",
	)

	vars := []string{"time", "latitude", "longitude"}
	for _, name := range []string{
		droneIDVar,
		windDirectionVar,
		windSpeedVar,
		waterTempVar,
		salinityVar,
		currentSpeedVar,
		currentDirectionVar,
		waveHeightVar,
		wavePeriodVar,
	} {
		if name != "" {
			vars = append(vars, name)
		}
	}

	remoteURL := "https://data.pmel.noaa.gov/generic/erddap/tabledap/" +
		url.PathEscape(datasetID) + ".json?" +
		strings.Join(vars, ",") +
		"&orderByMax(%22time%22)"

	req, err := http.NewRequest(http.MethodGet, remoteURL, nil)
	if err != nil {
		return result, err
	}
	req.Header.Set("User-Agent", "pittsburg-saildata/"+appVersion)

	resp, err := client.Do(req)
	if err != nil {
		return result, err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return result, err
	}
	if resp.StatusCode != http.StatusOK {
		detail := strings.TrimSpace(string(body))
		if len(detail) > 320 {
			detail = detail[:320]
		}
		if metadataErr != nil {
			return result, fmt.Errorf(
				"data HTTP %d after metadata fallback (%v): %s",
				resp.StatusCode,
				metadataErr,
				detail,
			)
		}
		return result, fmt.Errorf("data HTTP %d: %s", resp.StatusCode, detail)
	}

	var payload struct {
		Table struct {
			ColumnNames []string        `json:"columnNames"`
			Rows        [][]interface{} `json:"rows"`
		} `json:"table"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return result, err
	}
	if len(payload.Table.Rows) == 0 {
		return result, fmt.Errorf("no observations returned")
	}

	columns := map[string]int{}
	for i, name := range payload.Table.ColumnNames {
		columns[strings.TrimSpace(name)] = i
	}
	row := payload.Table.Rows[len(payload.Table.Rows)-1]

	stringAt := func(name string) string {
		if name == "" {
			return ""
		}
		i, ok := columns[name]
		if !ok || i < 0 || i >= len(row) || row[i] == nil {
			return ""
		}
		return strings.TrimSpace(fmt.Sprint(row[i]))
	}
	floatAt := func(name string) (float64, bool) {
		if name == "" {
			return 0, false
		}
		i, ok := columns[name]
		if !ok || i < 0 || i >= len(row) || row[i] == nil {
			return 0, false
		}
		return saildroneFloat(row[i])
	}

	result.Time = stringAt("time")
	result.DroneID = stringAt(droneIDVar)
	if result.DroneID == "" {
		if m := regexp.MustCompile(`^sd([0-9]+)_`).FindStringSubmatch(datasetID); len(m) == 2 {
			result.DroneID = m[1]
		} else {
			result.DroneID = datasetID
		}
	}

	lat, okLat := floatAt("latitude")
	lon, okLon := floatAt("longitude")
	if !okLat || !okLon {
		return result, fmt.Errorf("latest row is missing latitude/longitude")
	}
	result.Latitude = lat
	result.Longitude = lon

	if v, ok := floatAt(windDirectionVar); ok {
		result.WindDirectionDeg = v
	}
	if v, ok := floatAt(windSpeedVar); ok {
		result.WindSpeedMPS = v
	}
	if v, ok := floatAt(waterTempVar); ok {
		result.WaterTempC = v
	}
	if v, ok := floatAt(salinityVar); ok {
		result.SalinityPSU = v
	}
	if v, ok := floatAt(currentSpeedVar); ok {
		result.CurrentSpeedMPS = v
	}
	if v, ok := floatAt(currentDirectionVar); ok {
		result.CurrentDirDeg = v
	}
	if v, ok := floatAt(waveHeightVar); ok {
		result.WaveHeightM = v
	}
	if v, ok := floatAt(wavePeriodVar); ok {
		result.WavePeriodS = v
	}

	if t, err := time.Parse(time.RFC3339Nano, result.Time); err == nil {
		age := time.Since(t.UTC()).Hours()
		if age < 0 {
			age = 0
		}
		result.AgeHours = age
	}

	return result, nil
}

func fetchNOAASaildroneFeed() SaildroneFeed {
	saildroneCacheMu.Lock()
	if !saildroneCacheAt.IsZero() && time.Since(saildroneCacheAt) < 15*time.Minute {
		cached := saildroneCachePayload
		saildroneCacheMu.Unlock()
		return cached
	}
	saildroneCacheMu.Unlock()

	datasets := []struct {
		ID      string
		Mission string
	}{
		{"sd1033_tpos_2026", "TPOS Niño 3.4"},
		{"sd1077_tpos_2026", "TPOS Niño 3.4"},
		{"sd1080_tpos_2026", "TPOS Niño 3.4"},
		{"sd1090_tpos_2026", "TPOS Niño 3.4"},
		{"sd1030_hurricane_2026", "Atlantic Hurricane Monitoring"},
		{"sd1040_hurricane_2026", "Atlantic Hurricane Monitoring"},
		{"sd1045_hurricane_2026", "Atlantic Hurricane Monitoring"},
		{"sd1057_hurricane_2026", "Atlantic Hurricane Monitoring"},
		{"sd1068_hurricane_2026", "Atlantic Hurricane Monitoring"},
		{"sd1069_hurricane_2026", "Atlantic Hurricane Monitoring"},
		{"sd1074_hurricane_2026", "Atlantic Hurricane Monitoring"},
		{"sd1083_hurricane_2026", "Atlantic Hurricane Monitoring"},
		{"sd1084_hurricane_2026", "Atlantic Hurricane Monitoring"},
		{"sd1091_hurricane_2026", "Atlantic Hurricane Monitoring"},
	}

	client := &http.Client{Timeout: 18 * time.Second}
	type item struct {
		obs SaildroneObservation
		err error
	}
	ch := make(chan item, len(datasets))
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup

	for _, ds := range datasets {
		ds := ds
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			obs, err := fetchLatestSaildroneObservation(client, ds.ID, ds.Mission)
			<-sem
			ch <- item{obs: obs, err: err}
		}()
	}
	wg.Wait()
	close(ch)

	feed := SaildroneFeed{
		Updated: time.Now().UTC().Format(time.RFC3339),
		Source:  "NOAA PMEL public ERDDAP Saildrone datasets",
	}
	for entry := range ch {
		if entry.err != nil {
			feed.Errors = append(feed.Errors, entry.obs.DatasetID+": "+entry.err.Error())
			continue
		}
		feed.Observations = append(feed.Observations, entry.obs)
	}
	sort.Slice(feed.Observations, func(i, j int) bool {
		if feed.Observations[i].Mission != feed.Observations[j].Mission {
			return feed.Observations[i].Mission < feed.Observations[j].Mission
		}
		return feed.Observations[i].DroneID < feed.Observations[j].DroneID
	})
	sort.Strings(feed.Errors)

	saildroneCacheMu.Lock()
	saildroneCacheAt = time.Now()
	saildroneCachePayload = feed
	saildroneCacheMu.Unlock()
	return feed
}

var alertCaliforniaDiagnosticSerial uint64

func main() {
	// Mirror ScraperGo: report the version embedded in the compiled executable.
	if len(os.Args) == 2 && (os.Args[1] == "--version" || os.Args[1] == "-version") {
		fmt.Printf("SailGo v%s · Build %s\\n", appVersion, buildVersion)
		return
	}
	loc, err := time.LoadLocation(timeZoneName)
	if err != nil {
		fatal(err)
	}

	server := flag.Bool("server", false, "run as REST API server")
	at := flag.String("at", "", `historical date/time, e.g. "2026-08-20 15:00"`)
	port := flag.String("port", "8080", "HTTP server port")
	station := flag.String("station", defaultWindStation, "NDBC wind station ID")
	currentStation := flag.String("current-station", "", "NOAA current prediction station override, e.g. SFB1325")
	currentBin := flag.Int("current-bin", 0, "NOAA current prediction bin override, e.g. 9")
	startHour := flag.Int("start", defaultStartHour, "conditions window start hour; omit for sunrise")
	endHour := flag.Int("end", defaultEndHour, "conditions window end hour; omit for sunset")

	flag.Usage = printUsage
	flag.Parse()

	stationID := strings.ToUpper(strings.TrimSpace(*station))
	if !validStationID(stationID) {
		fatal(fmt.Errorf("invalid station ID %q", stationID))
	}

	if *server {
		runServer(*port, stationID, *startHour, *endHour, loc)
		return
	}

	observations, err := getWindStation(stationID)
	if *at != "" {
		observations, err = getNDBCWindStation(stationID)
	}
	if err != nil {
		fatal(err)
	}
	if len(observations) == 0 {
		fatal(fmt.Errorf("no usable observations found for %s", stationID))
	}

	var report *SailingReport

	if *at != "" {
		report, err = buildHistoricalWindReport(stationID, observations, *at, loc)
		if err != nil {
			fatal(err)
		}

		current, currentErr := BuildCurrentReport(
			stationID,
			*currentStation,
			*currentBin,
			report.Historical.Requested,
			*startHour,
			*endHour,
			loc,
		)
		if currentErr != nil {
			report.Current = &CurrentReport{
				Error: currentErr.Error(),
			}
		} else {
			report.Current = enforceAutomaticCurrentDistance(current, *currentStation)
		}
	} else {
		report = buildCurrentWindReport(stationID, observations, loc)

		current, currentErr := BuildCurrentReport(
			stationID,
			*currentStation,
			*currentBin,
			report.ReportTime,
			*startHour,
			*endHour,
			loc,
		)
		if currentErr != nil {
			report.Current = &CurrentReport{
				Error: currentErr.Error(),
			}
		} else {
			report.Current = enforceAutomaticCurrentDistance(current, *currentStation)
		}
	}

	writeTextReport(os.Stdout, report, loc)
}

func parseOptionalLatLon(
	q url.Values,
) (float64, float64, bool, error) {
	latText := strings.TrimSpace(q.Get("lat"))
	lonText := strings.TrimSpace(q.Get("lon"))

	if latText == "" && lonText == "" {
		return 0, 0, false, nil
	}
	if latText == "" || lonText == "" {
		return 0, 0, false, fmt.Errorf(
			"lat and lon must be provided together",
		)
	}

	var lat, lon float64
	if _, err := fmt.Sscanf(latText, "%f", &lat); err != nil {
		return 0, 0, false, fmt.Errorf("invalid lat %q", latText)
	}
	if _, err := fmt.Sscanf(lonText, "%f", &lon); err != nil {
		return 0, 0, false, fmt.Errorf("invalid lon %q", lonText)
	}
	if lat < -90 || lat > 90 {
		return 0, 0, false, fmt.Errorf(
			"lat must be between -90 and 90",
		)
	}
	if lon < -180 || lon > 180 {
		return 0, 0, false, fmt.Errorf(
			"lon must be between -180 and 180",
		)
	}

	return lat, lon, true, nil
}

func parseCurrentDate(
	q url.Values,
	fallback time.Time,
	loc *time.Location,
) (time.Time, error) {
	value := strings.TrimSpace(q.Get("current_date"))
	if value == "" {
		return fallback.In(loc), nil
	}

	parsed, err := time.ParseInLocation("2006-01-02", value, loc)
	if err != nil {
		return time.Time{}, fmt.Errorf(
			"invalid current_date %q; expected YYYY-MM-DD",
			value,
		)
	}

	// Noon is a stable representative time for a calendar date and avoids
	// midnight/DST edge cases. BuildCurrentReport uses the calendar date.
	return time.Date(
		parsed.Year(),
		parsed.Month(),
		parsed.Day(),
		12, 0, 0, 0,
		loc,
	), nil
}

func parseCurrentDays(q url.Values) int {
	value := strings.TrimSpace(q.Get("current_days"))
	switch value {
	case "3":
		return 3
	case "7":
		return 7
	default:
		return 1
	}
}

func parsePlanningTime(q url.Values, key, fallback string) (int, string) {
	value := strings.TrimSpace(q.Get(key))
	if value == "" {
		value = fallback
	}
	parsed, err := time.Parse("15:04", value)
	if err != nil {
		parsed, _ = time.Parse("15:04", fallback)
		value = fallback
	}
	return parsed.Hour()*60 + parsed.Minute(), value
}

func parsePlanningMaxEbb(q url.Values) float64 {
	value := strings.TrimSpace(q.Get("max_ebb"))
	if value == "" {
		return 3.0
	}
	var parsed float64
	if _, err := fmt.Sscanf(value, "%f", &parsed); err != nil || parsed <= 0 || parsed > 10 {
		return 3.0
	}
	// Keep UI/classification aligned with the one-decimal display.
	return math.Round(parsed*10) / 10
}

func parsePlanningMaxFlood(q url.Values) float64 {
	value := strings.TrimSpace(q.Get("max_flood"))
	if value == "" {
		return 3.0
	}
	var parsed float64
	if _, err := fmt.Sscanf(value, "%f", &parsed); err != nil || parsed <= 0 || parsed > 10 {
		return 3.0
	}
	return math.Round(parsed*10) / 10
}

func parsePlanningCautionEbb(q url.Values) float64 {
	value := strings.TrimSpace(q.Get("caution_ebb"))
	if value == "" {
		return 2.0
	}
	var parsed float64
	if _, err := fmt.Sscanf(value, "%f", &parsed); err != nil || parsed <= 0 || parsed > 10 {
		return 2.0
	}
	return math.Round(parsed*10) / 10
}

func parsePlanningCautionFlood(q url.Values) float64 {
	value := strings.TrimSpace(q.Get("caution_flood"))
	if value == "" {
		return 2.0
	}
	var parsed float64
	if _, err := fmt.Sscanf(value, "%f", &parsed); err != nil || parsed <= 0 || parsed > 10 {
		return 2.0
	}
	return math.Round(parsed*10) / 10
}

func parsePlanningCurrentDistanceWarning(q url.Values) float64 {
	value := strings.TrimSpace(q.Get("current_distance_warning"))
	if value == "" {
		return defaultCurrentDistanceWarningNM
	}
	var parsed float64
	if _, err := fmt.Sscanf(value, "%f", &parsed); err != nil ||
		parsed <= 0 ||
		parsed > maxAutoCurrentStationDistanceNM {
		return defaultCurrentDistanceWarningNM
	}
	return math.Round(parsed*10) / 10
}

func parsePlanningBuffer(q url.Values) int {
	value := strings.TrimSpace(q.Get("planning_buffer"))
	if value == "" {
		return 60
	}
	var parsed int
	if _, err := fmt.Sscanf(value, "%d", &parsed); err != nil || parsed < 0 || parsed > 360 {
		return 60
	}
	return parsed
}

func parseWindReadingHours(q url.Values) int {
	value := strings.TrimSpace(q.Get("wind_hours"))
	switch value {
	case "1":
		return 1
	case "8":
		return 8
	case "12":
		return 12
	case "16":
		return 16
	case "20":
		return 20
	case "24":
		return 24
	default:
		return defaultWindReadingHours
	}
}

func windObservationsForHours(observations []Observation, reference time.Time, loc *time.Location, hours int) []WindObservation {
	if hours <= 0 {
		hours = defaultWindReadingHours
	}
	items := makeWindObservationList(findLatestN(observations, maxRecentWindObservationCount), reference, loc)
	cutoff := reference.Add(-time.Duration(hours) * time.Hour)
	futureTolerance := reference.Add(5 * time.Minute)
	result := make([]WindObservation, 0, len(items))
	for _, item := range items {
		if item.Time.Before(cutoff) || item.Time.After(futureTolerance) {
			continue
		}
		result = append(result, item)
	}
	return result
}

const knotsToMPH = 1.15078

var windKTTokenRE = regexp.MustCompile(`([0-9]+(?:\.[0-9]+)?)\s+kt\b`)

func parseWindUnit(q url.Values) string {
	if strings.EqualFold(strings.TrimSpace(q.Get("wind_unit")), "mph") {
		return "mph"
	}
	return "kts"
}

func parseDistanceUnit(q url.Values) string {
	if strings.EqualFold(strings.TrimSpace(q.Get("distance_unit")), "mi") {
		return "mi"
	}
	return "nmi"
}

func distanceForDisplay(distanceNM float64, unit string) float64 {
	if unit == "mi" {
		return distanceNM * 1.15078
	}
	return distanceNM
}

func distanceUnitLabel(unit string) string {
	if unit == "mi" {
		return "mi"
	}
	return "nmi"
}

func formatDistanceNM(distanceNM float64, decimals int, unit string) string {
	return fmt.Sprintf("%.*f %s", decimals, distanceForDisplay(distanceNM, unit), distanceUnitLabel(unit))
}

func windUnitLabel(unit string) string {
	if unit == "mph" {
		return "mph"
	}
	return "kt"
}

func windSpeedForDisplay(speedKT float64, unit string) float64 {
	if unit == "mph" {
		return speedKT * knotsToMPH
	}
	return speedKT
}

func formatWindSpeed(speedKT float64, decimals int, unit string) string {
	return fmt.Sprintf(
		"%.*f %s",
		decimals,
		windSpeedForDisplay(speedKT, unit),
		windUnitLabel(unit),
	)
}

func convertWindTextUnits(text, unit string) string {
	if unit != "mph" {
		return text
	}
	return windKTTokenRE.ReplaceAllStringFunc(text, func(token string) string {
		match := windKTTokenRE.FindStringSubmatch(token)
		if len(match) != 2 {
			return token
		}
		value, err := strconv.ParseFloat(match[1], 64)
		if err != nil {
			return token
		}
		decimals := 0
		if strings.Contains(match[1], ".") {
			decimals = 1
		}
		return fmt.Sprintf("%.*f mph", decimals, value*knotsToMPH)
	})
}

func writeWindSummaryDisplay(
	w io.Writer,
	report *SailingReport,
	loc *time.Location,
	unit string,
) {
	var b strings.Builder
	writeWindSummaryText(&b, report, loc)
	io.WriteString(w, convertWindTextUnits(b.String(), unit))
}

func printWindObservationDisplay(
	w io.Writer,
	observation *WindObservation,
	loc *time.Location,
	reference time.Time,
	unit string,
) {
	var b strings.Builder
	printWindObservation(&b, observation, loc, reference)
	io.WriteString(w, convertWindTextUnits(b.String(), unit))
}

func printWindStatsDisplay(
	w io.Writer,
	stats *WindStats,
	loc *time.Location,
	unit string,
) {
	var b strings.Builder
	printWindStatsText(&b, stats, loc)
	io.WriteString(w, convertWindTextUnits(b.String(), unit))
}

func cloneQuery(values url.Values) url.Values {
	copy := make(url.Values, len(values))
	for key, list := range values {
		copy[key] = append([]string(nil), list...)
	}
	return copy
}

func buildStationBrowseSelection(
	station NDBCStation,
	observations []Observation,
) *WindStationSelection {
	// Use the selected station's own coordinates as the browsing anchor.
	// The automatic resolver then provides a distance-sorted candidate list
	// around that point. The report remains driven by the explicitly selected
	// station, not by whichever candidate AUTO prefers.
	_, _, nearby, _ := findNearestUsableWindStation(
		station.Lat,
		station.Lon,
	)

	selection := &WindStationSelection{
		Mode:              "station-browser",
		RequestedLat:      station.Lat,
		RequestedLon:      station.Lon,
		StationID:         station.ID,
		StationName:       station.Name,
		StationLat:        station.Lat,
		StationLon:        station.Lon,
		DistanceNM:        0,
		Candidates:        nearby.Candidates,
		CandidatesChecked: nearby.CandidatesChecked,
	}

	if latest, ok := latestUsableWindTime(observations); ok {
		age := time.Since(latest.UTC())
		if age < 0 {
			age = 0
		}
		selection.ObservationAgeMinutes =
			int(age.Round(time.Minute) / time.Minute)
	}

	// Tag the resolver's preferred station as AUTO while keeping the user's
	// current station as SELECTED.
	for i := range selection.Candidates {
		if strings.EqualFold(
			selection.Candidates[i].StationID,
			nearby.StationID,
		) && !strings.HasPrefix(
			selection.Candidates[i].Reason,
			"[AUTO] ",
		) {
			selection.Candidates[i].Reason =
				"[AUTO] " + selection.Candidates[i].Reason
		}
	}

	return selection
}

func resolveHTTPWindStation(
	r *http.Request,
	defaultStation string,
) (string, []Observation, *WindStationSelection, error) {
	q := r.URL.Query()

	explicitStation := strings.ToUpper(strings.TrimSpace(q.Get("station")))
	lat, lon, hasLocation, locationErr := parseOptionalLatLon(q)
	if locationErr != nil {
		return "", nil, nil, locationErr
	}

	if explicitStation != "" {
		if !validStationID(explicitStation) {
			return "", nil, nil, fmt.Errorf(
				"invalid station ID %q",
				explicitStation,
			)
		}

		observations, err := getWindStation(explicitStation)
		if err != nil {
			return explicitStation, observations, nil, err
		}

		stationMeta, metaErr := fetchNDBCStation(explicitStation)

		if hasLocation {
			// Keep the original sailing-location anchor while the user
			// manually browses different nearby wind stations.
			_, _, autoSelection, autoErr :=
				findNearestUsableWindStation(lat, lon)

			if metaErr != nil {
				return explicitStation, observations, nil, nil
			}

			selection := &WindStationSelection{
				Mode:         "manual-override",
				RequestedLat: lat,
				RequestedLon: lon,
				StationID:    stationMeta.ID,
				StationName:  stationMeta.Name,
				StationLat:   stationMeta.Lat,
				StationLon:   stationMeta.Lon,
				DistanceNM: distanceNM(
					lat,
					lon,
					stationMeta.Lat,
					stationMeta.Lon,
				),
			}

			if autoErr == nil || len(autoSelection.Candidates) > 0 {
				selection.Candidates = autoSelection.Candidates
				selection.CandidatesChecked =
					autoSelection.CandidatesChecked

				for i := range selection.Candidates {
					if strings.EqualFold(
						selection.Candidates[i].StationID,
						autoSelection.StationID,
					) {
						selection.Candidates[i].Reason =
							"[AUTO] " +
								selection.Candidates[i].Reason
					}
				}
			}

			if latest, ok := latestUsableWindTime(observations); ok {
				age := time.Since(latest.UTC())
				if age < 0 {
					age = 0
				}
				selection.ObservationAgeMinutes =
					int(age.Round(time.Minute) / time.Minute)
			}

			return explicitStation, observations, selection, nil
		}

		// No lat/lon: make the selected station itself the browsing anchor.
		if metaErr == nil {
			return explicitStation,
				observations,
				buildStationBrowseSelection(stationMeta, observations),
				nil
		}

		return explicitStation, observations, nil, nil
	}

	if hasLocation {
		station, observations, selection, err :=
			findNearestUsableWindStation(lat, lon)
		if err != nil {
			return "", nil, &selection, err
		}

		for i := range selection.Candidates {
			if strings.EqualFold(
				selection.Candidates[i].StationID,
				selection.StationID,
			) && !strings.HasPrefix(
				selection.Candidates[i].Reason,
				"[AUTO] ",
			) {
				selection.Candidates[i].Reason =
					"[AUTO] " + selection.Candidates[i].Reason
			}
		}

		return station.ID, observations, &selection, nil
	}

	// No parameters: retain PSBC1 (or configured default) as the report
	// station, but use it as an anchor for the nearby station browser.
	stationID := strings.ToUpper(strings.TrimSpace(defaultStation))
	if !validStationID(stationID) {
		return "", nil, nil, fmt.Errorf("invalid station ID %q", stationID)
	}

	observations, err := getWindStation(stationID)
	if err != nil {
		return stationID, observations, nil, err
	}

	if stationMeta, metaErr := fetchNDBCStation(stationID); metaErr == nil {
		return stationID,
			observations,
			buildStationBrowseSelection(stationMeta, observations),
			nil
	}

	return stationID, observations, nil, nil
}

func writeWindCandidateDiagnostics(
	w io.Writer,
	selection *WindStationSelection,
) {
	if selection == nil || len(selection.Candidates) == 0 {
		return
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "WIND STATION CANDIDATES")
	fmt.Fprintln(w, "--------------------------------")

	for i, candidate := range selection.Candidates {
		status := strings.ToUpper(candidate.WindStatus)
		if status == "" {
			status = "UNKNOWN"
		}

		fmt.Fprintf(
			w,
			"%2d  %-8s  %-38s %6.1f nmi  met=%-4s  %-8s",
			i+1,
			candidate.StationID,
			truncateWindStationName(candidate.StationName, 38),
			candidate.DistanceNM,
			candidate.Met,
			status,
		)

		if strings.TrimSpace(candidate.Reason) != "" {
			fmt.Fprintf(w, "  %s", candidate.Reason)
		}
		fmt.Fprintln(w)
	}
}

func truncateWindStationName(name string, width int) string {
	name = strings.TrimSpace(name)
	if len(name) <= width {
		return name
	}
	if width <= 3 {
		return name[:width]
	}
	return name[:width-3] + "..."
}

func writeWindSelectionText(
	w io.Writer,
	selection *WindStationSelection,
) {
	if selection == nil {
		return
	}

	fmt.Fprintf(
		w,
		"Wind location: %.5f, %.5f. Selected %s",
		selection.RequestedLat,
		selection.RequestedLon,
		selection.StationID,
	)
	if strings.TrimSpace(selection.StationName) != "" {
		fmt.Fprintf(w, " — %s", selection.StationName)
	}
	fmt.Fprintf(
		w,
		", %.1f nmi away (nearest station with usable wind).\n",
		selection.DistanceNM,
	)
}

func fetchNDBCAirTemperatureObservation(stationID string) (float64, time.Time, bool) {
	stationID = strings.ToUpper(strings.TrimSpace(stationID))
	if !validStationID(stationID) {
		return 0, time.Time{}, false
	}

	req, err := http.NewRequest(
		http.MethodGet,
		"https://www.ndbc.noaa.gov/data/realtime2/"+stationID+".txt",
		nil,
	)
	if err != nil {
		return 0, time.Time{}, false
	}
	req.Header.Set("User-Agent", "pittsburg-saildata/"+appVersion)

	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, time.Time{}, false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, time.Time{}, false
	}

	body, err := ioutil.ReadAll(io.LimitReader(resp.Body, 512<<10))
	if err != nil {
		return 0, time.Time{}, false
	}

	airTempColumn := -1
	for _, rawLine := range strings.Split(string(body), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "#") {
			header := strings.Fields(strings.TrimPrefix(line, "#"))
			for i, field := range header {
				if strings.EqualFold(field, "ATMP") {
					airTempColumn = i
					break
				}
			}
			continue
		}

		if airTempColumn < 0 {
			continue
		}

		fields := strings.Fields(line)
		if airTempColumn >= len(fields) {
			continue
		}

		value := strings.TrimSpace(fields[airTempColumn])
		if value == "" || strings.EqualFold(value, "MM") {
			continue
		}

		var airTempC float64
		if _, err := fmt.Sscanf(value, "%f", &airTempC); err != nil {
			continue
		}
		if airTempC < -90 || airTempC > 70 {
			continue
		}

		obsTime := time.Time{}
		if len(fields) >= 5 {
			year, yerr := strconv.Atoi(fields[0])
			month, merr := strconv.Atoi(fields[1])
			day, derr := strconv.Atoi(fields[2])
			hour, herr := strconv.Atoi(fields[3])
			minute, minerr := strconv.Atoi(fields[4])
			if yerr == nil && merr == nil && derr == nil && herr == nil && minerr == nil {
				if year < 100 {
					year += 2000
				}
				obsTime = time.Date(year, time.Month(month), day, hour, minute, 0, 0, time.UTC)
			}
		}

		return airTempC*9/5 + 32, obsTime, true
	}

	return 0, time.Time{}, false
}

func fetchNDBCAirTemperatureF(stationID string) (float64, bool) {
	tempF, _, ok := fetchNDBCAirTemperatureObservation(stationID)
	return tempF, ok
}

func latestNearbyStationWind(stationID, windUnit string) (string, string) {
	observations, err := getWindStation(stationID)
	if err != nil || len(observations) == 0 {
		return "", ""
	}

	report := buildCurrentWindReport(stationID, observations, time.UTC)
	if report == nil || report.Latest == nil {
		return "", ""
	}

	latest := report.Latest
	wind := ""
	switch {
	case latest.Direction != "" && latest.GustKT > 0:
		wind = fmt.Sprintf(
			"%s %.0f %s G%.0f",
			latest.Direction,
			windSpeedForDisplay(latest.WindKT, windUnit),
			windUnitLabel(windUnit),
			windSpeedForDisplay(latest.GustKT, windUnit),
		)
	case latest.Direction != "":
		wind = fmt.Sprintf("%s %.0f %s", latest.Direction, windSpeedForDisplay(latest.WindKT, windUnit), windUnitLabel(windUnit))
	case latest.GustKT > 0:
		wind = fmt.Sprintf("%.0f %s G%.0f", windSpeedForDisplay(latest.WindKT, windUnit), windUnitLabel(windUnit), windSpeedForDisplay(latest.GustKT, windUnit))
	default:
		wind = fmt.Sprintf("%.0f %s", windSpeedForDisplay(latest.WindKT, windUnit), windUnitLabel(windUnit))
	}

	age := time.Since(latest.Time.UTC())
	if age < 0 {
		age = 0
	}
	ageMinutes := int(age.Round(time.Minute) / time.Minute)

	return wind, fmt.Sprintf("%d min", ageMinutes)
}

func applyNOAACoastlineMask(
	sourcePNG []byte,
	west float64,
	south float64,
	east float64,
	north float64,
	width int,
	height int,
) ([]byte, error) {
	if len(sourcePNG) == 0 {
		return nil, fmt.Errorf("empty source PNG")
	}
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("invalid coastline-mask image size")
	}

	// NOAA ENC Direct Coastal Land_Area layer. Request the land mask at 2x
	// the CoastWatch display resolution and reduce it back to the source raster
	// with alpha coverage. ArcGIS renders this service without antialiasing, so
	// supersampling prevents the visibly stair-stepped shoreline produced by a
	// same-size binary mask. CoastWatch numeric data are not modified; this
	// affects display PNGs only.
	maskWidth := width * 2
	maskHeight := height * 2
	if maskWidth > 4096 {
		maskWidth = 4096
	}
	if maskHeight > 4096 {
		maskHeight = 4096
	}

	params := url.Values{}
	params.Set("f", "image")
	params.Set("bbox", fmt.Sprintf("%.6f,%.6f,%.6f,%.6f", west, south, east, north))
	params.Set("bboxSR", "4326")
	params.Set("imageSR", "4326")
	params.Set("size", fmt.Sprintf("%d,%d", maskWidth, maskHeight))
	params.Set("format", "png32")
	params.Set("transparent", "true")
	params.Set("layers", "show:171")

	maskURL := "https://maritimeboundaries.noaa.gov/arcgis/rest/services/encdirect/enc_coastal/MapServer/export?" +
		params.Encode()

	req, err := http.NewRequest(http.MethodGet, maskURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "pittsburg-saildata/"+appVersion)

	client := &http.Client{Timeout: 25 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("NOAA coastline mask request failed: %w", err)
	}
	defer resp.Body.Close()

	maskBytes, err := ioutil.ReadAll(io.LimitReader(resp.Body, 12<<20))
	if err != nil {
		return nil, fmt.Errorf("NOAA coastline mask read failed: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		detail := strings.TrimSpace(string(maskBytes))
		if len(detail) > 500 {
			detail = detail[:500]
		}
		return nil, fmt.Errorf(
			"NOAA coastline mask returned HTTP %d: %s",
			resp.StatusCode,
			detail,
		)
	}

	sourceImage, err := png.Decode(bytes.NewReader(sourcePNG))
	if err != nil {
		return nil, fmt.Errorf("source PNG decode failed: %w", err)
	}
	maskImage, err := png.Decode(bytes.NewReader(maskBytes))
	if err != nil {
		return nil, fmt.Errorf("NOAA coastline mask PNG decode failed: %w", err)
	}

	sourceBounds := sourceImage.Bounds()
	maskBounds := maskImage.Bounds()
	if maskBounds.Dx() <= 0 || maskBounds.Dy() <= 0 {
		return nil, fmt.Errorf("NOAA coastline mask returned an empty image")
	}

	sourceWidth := sourceBounds.Dx()
	sourceHeight := sourceBounds.Dy()
	maskImageWidth := maskBounds.Dx()
	maskImageHeight := maskBounds.Dy()
	clipped := image.NewNRGBA(sourceBounds)

	for y := sourceBounds.Min.Y; y < sourceBounds.Max.Y; y++ {
		sy := y - sourceBounds.Min.Y
		my0 := maskBounds.Min.Y + sy*maskImageHeight/sourceHeight
		my1 := maskBounds.Min.Y + (sy+1)*maskImageHeight/sourceHeight
		if my1 <= my0 {
			my1 = my0 + 1
		}
		if my1 > maskBounds.Max.Y {
			my1 = maskBounds.Max.Y
		}

		for x := sourceBounds.Min.X; x < sourceBounds.Max.X; x++ {
			sx := x - sourceBounds.Min.X
			mx0 := maskBounds.Min.X + sx*maskImageWidth/sourceWidth
			mx1 := maskBounds.Min.X + (sx+1)*maskImageWidth/sourceWidth
			if mx1 <= mx0 {
				mx1 = mx0 + 1
			}
			if mx1 > maskBounds.Max.X {
				mx1 = maskBounds.Max.X
			}

			var alphaSum int
			var samples int
			for my := my0; my < my1; my++ {
				for mx := mx0; mx < mx1; mx++ {
					mask := color.NRGBAModel.Convert(maskImage.At(mx, my)).(color.NRGBA)
					alphaSum += int(mask.A)
					samples++
				}
			}

			src := color.NRGBAModel.Convert(sourceImage.At(x, y)).(color.NRGBA)
			if samples > 0 && alphaSum > 0 {
				coverage := float64(alphaSum) / float64(samples*255)
				if coverage > 1 {
					coverage = 1
				}
				src.A = uint8(math.Round(float64(src.A) * (1 - coverage)))
			}
			clipped.SetNRGBA(x, y, src)
		}
	}

	var encoded bytes.Buffer
	if err := png.Encode(&encoded, clipped); err != nil {
		return nil, fmt.Errorf("coastline-clipped PNG encoding failed: %w", err)
	}
	return encoded.Bytes(), nil
}

func fetchNWSForecastZoneName(zoneID string) string {
	zoneID = strings.ToUpper(strings.TrimSpace(zoneID))
	if zoneID == "" {
		return ""
	}

	var payload struct {
		Properties struct {
			Name string `json:"name"`
		} `json:"properties"`
	}
	zoneURL := "https://api.weather.gov/zones/forecast/" + url.PathEscape(zoneID)
	if err := fetchNWSJSON(zoneURL, &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.Properties.Name)
}

func isOffshoreTripZone(zoneID, zoneName string) bool {
	zoneID = strings.ToUpper(strings.TrimSpace(zoneID))
	if zoneID == "" {
		return false
	}

	// NWS ocean/coastal marine forecast-zone prefixes. Land forecast zones,
	// Great Lakes zones, and other inland products do not qualify.
	oceanPrefix := false
	for _, prefix := range []string{"PZZ", "PKZ", "PHZ", "PMZ", "ANZ", "AMZ", "GMZ"} {
		if strings.HasPrefix(zoneID, prefix) {
			oceanPrefix = true
			break
		}
	}
	if !oceanPrefix {
		return false
	}

	// San Francisco/Monterey enclosed-water zones are marine forecast zones,
	// but they are not "offshore" for this fishing-planning card.
	switch zoneID {
	case "PZZ530", // San Pablo Bay, Suisun Bay, West Delta, SF Bay north of Bay Bridge
		"PZZ531", // San Francisco Bay south of Bay Bridge
		"PZZ535": // Monterey Bay
		return false
	}

	// Keep a defensive name check for the Bay/Delta wording used by NWS. This
	// also protects against future zone renumbering around the inland estuary.
	name := strings.ToLower(strings.TrimSpace(zoneName))
	for _, phrase := range []string{
		"san francisco bay",
		"san pablo bay",
		"suisun bay",
		"west delta",
		"sacramento-san joaquin delta",
	} {
		if strings.Contains(name, phrase) {
			return false
		}
	}

	return true
}

type offshoreBuoySnapshot struct {
	Station              string  `json:"station"`
	Name                 string  `json:"name,omitempty"`
	DistanceNM           float64 `json:"distance_nm"`
	ObservationTime      string  `json:"observation_time,omitempty"`
	WindKT               float64 `json:"wind_kt,omitempty"`
	GustKT               float64 `json:"gust_kt,omitempty"`
	WaveFT               float64 `json:"wave_ft,omitempty"`
	DominantPeriodSec    float64 `json:"dominant_period_sec,omitempty"`
	AveragePeriodSec     float64 `json:"average_period_sec,omitempty"`
	MeanWaveDirectionDeg float64 `json:"mean_wave_direction_deg,omitempty"`
}

func parseNDBCRealtimeSnapshot(
	stationID string,
	name string,
	distanceNM float64,
) (*offshoreBuoySnapshot, error) {
	stationID = strings.ToUpper(strings.TrimSpace(stationID))
	if !validStationID(stationID) {
		return nil, fmt.Errorf("invalid NDBC station %q", stationID)
	}

	req, err := http.NewRequest(
		http.MethodGet,
		"https://www.ndbc.noaa.gov/data/realtime2/"+stationID+".txt",
		nil,
	)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "pittsburg-saildata/"+appVersion)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("NDBC %s returned HTTP %d", stationID, resp.StatusCode)
	}

	body, err := ioutil.ReadAll(io.LimitReader(resp.Body, 1024<<10))
	if err != nil {
		return nil, err
	}

	var header []string
	for _, raw := range strings.Split(string(body), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			fields := strings.Fields(strings.TrimPrefix(line, "#"))
			if len(fields) > 8 && strings.EqualFold(fields[0], "YY") {
				header = fields
			}
			continue
		}
		if len(header) == 0 {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < len(header) {
			continue
		}

		col := make(map[string]int, len(header))
		for i, h := range header {
			col[strings.ToUpper(h)] = i
		}

		value := func(key string) (float64, bool) {
			i, ok := col[key]
			if !ok || i >= len(fields) {
				return 0, false
			}
			raw := strings.TrimSpace(fields[i])
			if raw == "" || strings.EqualFold(raw, "MM") {
				return 0, false
			}
			v, err := strconv.ParseFloat(raw, 64)
			return v, err == nil
		}

		snapshot := &offshoreBuoySnapshot{
			Station:    stationID,
			Name:       strings.TrimSpace(name),
			DistanceNM: distanceNM,
		}

		// The first five columns are always year, month, day, hour, minute.
		if len(fields) >= 5 {
			year, e1 := strconv.Atoi(fields[0])
			month, e2 := strconv.Atoi(fields[1])
			day, e3 := strconv.Atoi(fields[2])
			hour, e4 := strconv.Atoi(fields[3])
			minute, e5 := strconv.Atoi(fields[4])
			if e1 == nil && e2 == nil && e3 == nil && e4 == nil && e5 == nil {
				t := time.Date(year, time.Month(month), day, hour, minute, 0, 0, time.UTC)
				snapshot.ObservationTime = t.Format(time.RFC3339)
			}
		}

		if v, ok := value("WSPD"); ok {
			snapshot.WindKT = v * 1.9438444924406
		}
		if v, ok := value("GST"); ok {
			snapshot.GustKT = v * 1.9438444924406
		}
		if v, ok := value("WVHT"); ok {
			snapshot.WaveFT = v * 3.2808398950131
		}
		if v, ok := value("DPD"); ok {
			snapshot.DominantPeriodSec = v
		}
		if v, ok := value("APD"); ok {
			snapshot.AveragePeriodSec = v
		}
		if v, ok := value("MWD"); ok {
			snapshot.MeanWaveDirectionDeg = v
		}

		if snapshot.WaveFT <= 0 &&
			snapshot.WindKT <= 0 &&
			snapshot.GustKT <= 0 {
			return nil, fmt.Errorf("NDBC %s has no usable wind/wave observation", stationID)
		}
		return snapshot, nil
	}

	return nil, fmt.Errorf("NDBC %s returned no usable realtime rows", stationID)
}

func fetchNearestOffshoreBuoySnapshot(
	lat float64,
	lon float64,
) (*offshoreBuoySnapshot, error) {
	stations, err := getActiveNDBCStations()
	if err != nil {
		return nil, err
	}

	type candidate struct {
		ID       string
		Name     string
		Distance float64
	}
	candidates := make([]candidate, 0, len(stations))
	for _, station := range stations {
		d := distanceNM(lat, lon, station.Lat, station.Lon)
		if d > 180 {
			continue
		}
		candidates = append(candidates, candidate{
			ID:       strings.ToUpper(strings.TrimSpace(station.ID)),
			Name:     strings.TrimSpace(station.Name),
			Distance: d,
		})
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Distance < candidates[j].Distance
	})
	if len(candidates) > 12 {
		candidates = candidates[:12]
	}

	var lastErr error
	for _, c := range candidates {
		snapshot, err := parseNDBCRealtimeSnapshot(c.ID, c.Name, c.Distance)
		if err == nil && snapshot != nil {
			return snapshot, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("no active NDBC station found within 180 nmi")
}

type placeSearchResult struct {
	DisplayName string    `json:"display_name"`
	Lat         float64   `json:"lat"`
	Lon         float64   `json:"lon"`
	Type        string    `json:"type,omitempty"`
	BoundingBox []float64 `json:"bounding_box,omitempty"`
}

type placeSearchCacheEntry struct {
	Results   []placeSearchResult
	Scope     string
	ExpiresAt time.Time
}

type nominatimSearchResult struct {
	Lat         string   `json:"lat"`
	Lon         string   `json:"lon"`
	DisplayName string   `json:"display_name"`
	Type        string   `json:"type"`
	Class       string   `json:"class"`
	BoundingBox []string `json:"boundingbox"`
}

type ndfdLegendColor struct {
	R, G, B uint8
	TempF   float64
}

var (
	ndfdLegendMu    sync.Mutex
	ndfdLegendCache = make(map[int][]ndfdLegendColor)
)

func ndfdTemperatureLayerIDServer(hours int) (int, bool) {
	ids := map[int]int{0: 8, 3: 12, 6: 16, 9: 20, 12: 24, 15: 28, 18: 32, 21: 36, 24: 40}
	id, ok := ids[hours]
	return id, ok
}

func parseNDFDLegendTemperature(label string) (float64, bool) {
	parts := strings.Split(label, " - ")
	if len(parts) != 2 {
		return 0, false
	}
	lo, err1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	hi, err2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err1 != nil || err2 != nil {
		return 0, false
	}
	if hi-lo > 5 {
		if lo < -30 {
			return hi, true
		}
		return lo, true
	}
	return (lo + hi) / 2, true
}

func fetchNDFDLegendColors(layerID int) ([]ndfdLegendColor, error) {
	ndfdLegendMu.Lock()
	if cached := ndfdLegendCache[layerID]; len(cached) > 0 {
		out := append([]ndfdLegendColor(nil), cached...)
		ndfdLegendMu.Unlock()
		return out, nil
	}
	ndfdLegendMu.Unlock()

	client := &http.Client{Timeout: 12 * time.Second}
	resp, err := client.Get("https://mapservices.weather.noaa.gov/raster/rest/services/NDFD/NDFD_temp/MapServer/legend?f=json")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("NDFD legend HTTP %d", resp.StatusCode)
	}
	var payload struct {
		Layers []struct {
			LayerID int `json:"layerId"`
			Legend  []struct {
				Label     string `json:"label"`
				ImageData string `json:"imageData"`
			} `json:"legend"`
		} `json:"layers"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&payload); err != nil {
		return nil, err
	}
	colors := make([]ndfdLegendColor, 0, 200)
	for _, layer := range payload.Layers {
		if layer.LayerID != layerID {
			continue
		}
		for _, entry := range layer.Legend {
			tempF, ok := parseNDFDLegendTemperature(entry.Label)
			if !ok || strings.TrimSpace(entry.ImageData) == "" {
				continue
			}
			raw, err := base64.StdEncoding.DecodeString(entry.ImageData)
			if err != nil {
				continue
			}
			img, err := png.Decode(bytes.NewReader(raw))
			if err != nil {
				continue
			}
			b := img.Bounds()
			r, g, b0, a := img.At(b.Min.X+b.Dx()/2, b.Min.Y+b.Dy()/2).RGBA()
			if a == 0 {
				continue
			}
			colors = append(colors, ndfdLegendColor{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b0 >> 8), TempF: tempF})
		}
		break
	}
	if len(colors) == 0 {
		return nil, fmt.Errorf("NDFD legend contained no colors for layer %d", layerID)
	}
	ndfdLegendMu.Lock()
	ndfdLegendCache[layerID] = append([]ndfdLegendColor(nil), colors...)
	ndfdLegendMu.Unlock()
	return colors, nil
}

func webMercatorXY(lat, lon float64) (float64, float64) {
	const earthRadius = 6378137.0
	lat = math.Max(-85.05112878, math.Min(85.05112878, lat))
	x := earthRadius * lon * math.Pi / 180
	y := earthRadius * math.Log(math.Tan(math.Pi/4+lat*math.Pi/360))
	return x, y
}

func sampleNDFDRenderedTemperature(lat, lon float64, layerID int) (float64, error) {
	colors, err := fetchNDFDLegendColors(layerID)
	if err != nil {
		return 0, err
	}
	x, y := webMercatorXY(lat, lon)
	const halfSpan = 3500.0
	params := url.Values{}
	params.Set("bbox", fmt.Sprintf("%.3f,%.3f,%.3f,%.3f", x-halfSpan, y-halfSpan, x+halfSpan, y+halfSpan))
	params.Set("bboxSR", "3857")
	params.Set("imageSR", "3857")
	params.Set("size", "9,9")
	params.Set("format", "png32")
	params.Set("transparent", "true")
	params.Set("adjustAspectRatio", "false")
	params.Set("interpolation", "RSP_BilinearInterpolation")
	params.Set("layers", fmt.Sprintf("show:%d", layerID))
	params.Set("f", "image")
	upstream := "https://mapservices.weather.noaa.gov/raster/rest/services/NDFD/NDFD_temp/MapServer/export?" + params.Encode()
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(upstream)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("NDFD export HTTP %d", resp.StatusCode)
	}
	img, err := png.Decode(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return 0, err
	}
	b := img.Bounds()
	bestDist := math.MaxFloat64
	bestTemp := 0.0
	found := false
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			px := b.Min.X + b.Dx()/2 + dx
			py := b.Min.Y + b.Dy()/2 + dy
			if px < b.Min.X || px >= b.Max.X || py < b.Min.Y || py >= b.Max.Y {
				continue
			}
			r, g, b0, a := img.At(px, py).RGBA()
			if a < 0x4000 {
				continue
			}
			r8, g8, b8 := float64(r>>8), float64(g>>8), float64(b0>>8)
			for _, c := range colors {
				dr, dg, db := r8-float64(c.R), g8-float64(c.G), b8-float64(c.B)
				d := dr*dr + dg*dg + db*db
				if d < bestDist {
					bestDist, bestTemp, found = d, c.TempF, true
				}
			}
		}
	}
	if !found {
		return 0, fmt.Errorf("NDFD raster has no temperature at this point")
	}
	return bestTemp, nil
}

func buildSwellUpstreamURL(
	targetText string,
	latStart int,
	latStop int,
	lonStart int,
	lonStop int,
	stride int,
) string {
	subset := fmt.Sprintf(
		"[(%s)][(0.0)][%d:%d:%d][%d:%d:%d]",
		targetText, latStart, stride, latStop, lonStart, stride, lonStop,
	)
	projection := "shgt" + subset + ",sper" + subset + ",sdir" + subset
	return "https://pae-paha.pacioos.hawaii.edu/erddap/griddap/ww3_global_lon180.json?" +
		url.QueryEscape(projection)
}

func cachedSwellRaw(key string) ([]byte, bool) {
	swellFrameCache.RLock()
	entry, ok := swellFrameCache.items[key]
	swellFrameCache.RUnlock()
	if !ok {
		return nil, false
	}
	if time.Since(entry.StoredAt) > swellFrameCacheTTL {
		swellFrameCache.Lock()
		delete(swellFrameCache.items, key)
		swellFrameCache.Unlock()
		return nil, false
	}
	return append([]byte(nil), entry.Raw...), true
}

func fetchSwellRaw(ctx context.Context, upstreamURL string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, upstreamURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "MauriWeatherWaterConditions/"+appVersion)

		resp, err := swellUpstreamClient.Do(req)
		if err == nil && resp != nil {
			body, readErr := ioutil.ReadAll(io.LimitReader(resp.Body, 8<<20))
			resp.Body.Close()
			if readErr == nil && resp.StatusCode == http.StatusOK {
				return body, nil
			}
			if readErr != nil {
				err = readErr
			} else {
				detail := strings.TrimSpace(string(body))
				if detail == "" {
					detail = resp.Status
				}
				err = fmt.Errorf("PacIOOS WaveWatch III: %s", detail)
				if resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode < 500 {
					return nil, err
				}
			}
		}
		lastErr = err
		if attempt == 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(1500 * time.Millisecond):
			}
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("PacIOOS WaveWatch III request failed")
	}
	return nil, lastErr
}

func getOrFetchSwellRaw(ctx context.Context, upstreamURL string, forceRefresh bool) ([]byte, string, error) {
	var fallback []byte
	if raw, ok := cachedSwellRaw(upstreamURL); ok {
		if !forceRefresh {
			return raw, "HIT", nil
		}
		fallback = raw
	}

	swellFrameCache.Lock()
	if wait, ok := swellFrameCache.inflight[upstreamURL]; ok {
		swellFrameCache.Unlock()
		select {
		case <-ctx.Done():
			return nil, "", ctx.Err()
		case <-wait:
			if raw, ok := cachedSwellRaw(upstreamURL); ok {
				return raw, "COALESCED", nil
			}
			if len(fallback) > 0 {
				return append([]byte(nil), fallback...), "STALE", nil
			}
			return nil, "", fmt.Errorf("PacIOOS WaveWatch III shared request failed")
		}
	}
	wait := make(chan struct{})
	swellFrameCache.inflight[upstreamURL] = wait
	swellFrameCache.Unlock()

	raw, err := fetchSwellRaw(ctx, upstreamURL)

	swellFrameCache.Lock()
	if err == nil {
		swellFrameCache.items[upstreamURL] = swellFrameCacheEntry{
			Raw:      append([]byte(nil), raw...),
			StoredAt: time.Now(),
		}
	}
	delete(swellFrameCache.inflight, upstreamURL)
	close(wait)
	swellFrameCache.Unlock()

	if err != nil {
		if len(fallback) > 0 {
			return append([]byte(nil), fallback...), "STALE", nil
		}
		return nil, "", err
	}
	if forceRefresh {
		return raw, "REFRESH", nil
	}
	return raw, "MISS", nil
}

func prefetchSwellRaw(upstreamURL string) {
	if _, ok := cachedSwellRaw(upstreamURL); ok {
		return
	}
	go func() {
		select {
		case swellPrefetchSlots <- struct{}{}:
			defer func() { <-swellPrefetchSlots }()
		default:
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		_, _, _ = getOrFetchSwellRaw(ctx, upstreamURL, false)
	}()
}

func runServer(
	port string,
	defaultStation string,
	defaultStart int,
	defaultEnd int,
	loc *time.Location,
) {
	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintln(w, "OK")
	})

	mux.HandleFunc("/assets/hero.jpg", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		http.ServeFile(w, r, "assets/hero.jpg")
	})

	mux.HandleFunc("/assets/marine_places.json", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache, max-age=0, must-revalidate")
		http.ServeFile(w, r, "assets/marine_places.json")
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		q.Set("format", "html")
		http.Redirect(w, r, "/report?"+q.Encode(), http.StatusTemporaryRedirect)
	})

	mux.HandleFunc("/welcome", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		setDynamicHTMLHeaders(w)
		data := struct {
			Yogiism      string
			AppVersion   string
			BuildVersion string
		}{
			Yogiism:      randomYogiism(),
			AppVersion:   appVersion,
			BuildVersion: buildVersion,
		}
		if err := welcomeHTMLTemplate.Execute(w, data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	// Current inland observations come from Aviation Weather Center's complete
	// METAR cache. AWC recommends its cache files for larger observation sets;
	// keeping a short server-side cache avoids repeated full-feed downloads as
	// the user pans the map.
	type cachedMetarObservation struct {
		Station     string
		Time        time.Time
		Lat         float64
		Lon         float64
		Wdir        float64
		HasWdir     bool
		Wspd        float64
		Wgst        float64
		HasWgst     bool
		PressureMB  float64
		HasPressure bool
		TempC       float64
		HasTemp     bool
	}
	var metarCacheMu sync.Mutex
	var metarCacheAt time.Time
	var metarCache []cachedMetarObservation

	fetchCurrentMetars := func() ([]cachedMetarObservation, error) {
		metarCacheMu.Lock()
		if !metarCacheAt.IsZero() && time.Since(metarCacheAt) < 2*time.Minute && len(metarCache) > 0 {
			cached := append([]cachedMetarObservation(nil), metarCache...)
			metarCacheMu.Unlock()
			return cached, nil
		}
		metarCacheMu.Unlock()

		req, err := http.NewRequest(http.MethodGet, "https://aviationweather.gov/data/cache/metars.cache.xml.gz", nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/gzip, application/octet-stream, application/xml, text/xml")
		req.Header.Set("User-Agent", "pittsburg-saildata/"+appVersion)
		client := &http.Client{Timeout: 15 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body, _ := ioutil.ReadAll(io.LimitReader(resp.Body, 512))
			return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}

		gz, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("gzip: %w", err)
		}
		defer gz.Close()

		type metarXML struct {
			Station     string `xml:"station_id"`
			ObsTime     string `xml:"observation_time"`
			Lat         string `xml:"latitude"`
			Lon         string `xml:"longitude"`
			Wdir        string `xml:"wind_dir_degrees"`
			Wspd        string `xml:"wind_speed_kt"`
			Wgst        string `xml:"wind_gust_kt"`
			Pressure    string `xml:"sea_level_pressure_mb"`
			PressureSLP string `xml:"slp_mb"`
			TempC       string `xml:"temp_c"`
		}
		var payload struct {
			Data struct {
				METARs []metarXML `xml:"METAR"`
			} `xml:"data"`
		}
		decoder := xml.NewDecoder(io.LimitReader(gz, 32<<20))
		if err := decoder.Decode(&payload); err != nil {
			return nil, fmt.Errorf("XML: %w", err)
		}

		result := make([]cachedMetarObservation, 0, len(payload.Data.METARs))
		for _, m := range payload.Data.METARs {
			station := strings.ToUpper(strings.TrimSpace(m.Station))
			lat, latErr := strconv.ParseFloat(strings.TrimSpace(m.Lat), 64)
			lon, lonErr := strconv.ParseFloat(strings.TrimSpace(m.Lon), 64)
			wspd, speedErr := strconv.ParseFloat(strings.TrimSpace(m.Wspd), 64)
			if station == "" || latErr != nil || lonErr != nil || speedErr != nil ||
				math.IsNaN(lat) || math.IsNaN(lon) || math.IsNaN(wspd) ||
				math.IsInf(lat, 0) || math.IsInf(lon, 0) || math.IsInf(wspd, 0) || wspd < 0 {
				continue
			}
			obsTime, timeErr := time.Parse(time.RFC3339, strings.TrimSpace(m.ObsTime))
			if timeErr != nil {
				obsTime = time.Time{}
			}
			item := cachedMetarObservation{Station: station, Time: obsTime, Lat: lat, Lon: lon, Wspd: wspd}
			if wdir, err := strconv.ParseFloat(strings.TrimSpace(m.Wdir), 64); err == nil &&
				!math.IsNaN(wdir) && !math.IsInf(wdir, 0) && wdir >= 0 && wdir <= 360 {
				item.Wdir = math.Mod(wdir+360, 360)
				item.HasWdir = true
			}
			if wgst, err := strconv.ParseFloat(strings.TrimSpace(m.Wgst), 64); err == nil &&
				!math.IsNaN(wgst) && !math.IsInf(wgst, 0) && wgst >= 0 {
				item.Wgst = wgst
				item.HasWgst = true
			}
			pressureText := strings.TrimSpace(m.Pressure)
			if pressureText == "" {
				pressureText = strings.TrimSpace(m.PressureSLP)
			}
			if pressure, err := strconv.ParseFloat(pressureText, 64); err == nil &&
				!math.IsNaN(pressure) && !math.IsInf(pressure, 0) && pressure >= 850 && pressure <= 1100 {
				item.PressureMB = pressure
				item.HasPressure = true
			}
			if tempC, err := strconv.ParseFloat(strings.TrimSpace(m.TempC), 64); err == nil &&
				!math.IsNaN(tempC) && !math.IsInf(tempC, 0) && tempC >= -90 && tempC <= 70 {
				item.TempC = tempC
				item.HasTemp = true
			}
			result = append(result, item)
		}
		if len(result) == 0 {
			return nil, fmt.Errorf("cache contained no usable METAR observations")
		}

		metarCacheMu.Lock()
		metarCacheAt = time.Now()
		metarCache = append([]cachedMetarObservation(nil), result...)
		metarCacheMu.Unlock()
		return result, nil
	}

	mux.HandleFunc("/wind-barbs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		q := r.URL.Query()
		parseBound := func(key string) (float64, error) {
			text := strings.TrimSpace(q.Get(key))
			if text == "" {
				return 0, fmt.Errorf("%s is required", key)
			}
			value, err := strconv.ParseFloat(text, 64)
			if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
				return 0, fmt.Errorf("invalid %s %q", key, text)
			}
			return value, nil
		}

		minLat, err := parseBound("min_lat")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		maxLat, err := parseBound("max_lat")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		minLon, err := parseBound("min_lon")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		maxLon, err := parseBound("max_lon")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if minLat < -90 || maxLat > 90 || minLat >= maxLat || minLon < -180 || maxLon > 180 || minLon >= maxLon {
			http.Error(w, "invalid wind-barb bounds", http.StatusBadRequest)
			return
		}

		includeMarine := q.Get("include_marine") != "0"
		includeInland := q.Get("include_inland") == "1"

		type windBarbObservation struct {
			Station        string  `json:"station"`
			Name           string  `json:"name"`
			Wind           string  `json:"wind,omitempty"`
			ObservationAge string  `json:"observation_age,omitempty"`
			Lat            float64 `json:"lat"`
			Lon            float64 `json:"lon"`
			Class          string  `json:"class"`
			Source         string  `json:"source,omitempty"`
		}

		items := make([]windBarbObservation, 0)
		errorsOut := make([]string, 0, 2)
		windUnit := parseWindUnit(q)

		if includeMarine {
			stations, stationErr := getActiveNDBCStations()
			if stationErr != nil {
				errorsOut = append(errorsOut, "NOAA/NDBC: "+stationErr.Error())
			} else {
				type stationResult struct {
					item windBarbObservation
					ok   bool
				}
				type windBarbNDBCCandidate struct {
					ID   string
					Name string
					Lat  float64
					Lon  float64
				}
				candidates := make([]windBarbNDBCCandidate, 0)
				for _, station := range stations {
					if station.Lat >= minLat && station.Lat <= maxLat && station.Lon >= minLon && station.Lon <= maxLon {
						candidates = append(candidates, windBarbNDBCCandidate{ID: station.ID, Name: station.Name, Lat: station.Lat, Lon: station.Lon})
					}
				}
				results := make(chan stationResult, len(candidates))
				sem := make(chan struct{}, 6)
				var wg sync.WaitGroup
				for _, station := range candidates {
					station := station
					wg.Add(1)
					go func() {
						defer wg.Done()
						sem <- struct{}{}
						stationID := strings.ToUpper(strings.TrimSpace(station.ID))
						wind, age := latestNearbyStationWind(stationID, windUnit)
						<-sem
						if strings.TrimSpace(wind) == "" {
							return
						}
						results <- stationResult{item: windBarbObservation{
							Station: stationID, Name: station.Name, Wind: wind, ObservationAge: age,
							Lat: station.Lat, Lon: station.Lon, Class: "marine", Source: "NOAA/NDBC",
						}, ok: true}
					}()
				}
				wg.Wait()
				close(results)
				for result := range results {
					if result.ok {
						items = append(items, result.item)
					}
				}
			}
		}

		if includeInland {
			metars, metarErr := fetchCurrentMetars()
			if metarErr != nil {
				errorsOut = append(errorsOut, "Aviation Weather Center METAR cache: "+metarErr.Error())
			} else {
				compass := []string{"N", "NNE", "NE", "ENE", "E", "ESE", "SE", "SSE", "S", "SSW", "SW", "WSW", "W", "WNW", "NW", "NNW"}
				for _, m := range metars {
					if m.Lat < minLat || m.Lat > maxLat || m.Lon < minLon || m.Lon > maxLon {
						continue
					}
					if m.Wspd >= 3 && !m.HasWdir {
						continue
					}
					directionDeg := 0.0
					if m.HasWdir {
						directionDeg = m.Wdir
					}
					idx := int(math.Floor((directionDeg+11.25)/22.5)) % len(compass)
					windText := fmt.Sprintf("%s %.0f %s", compass[idx], windSpeedForDisplay(m.Wspd, windUnit), windUnitLabel(windUnit))
					if m.HasWgst && m.Wgst > m.Wspd {
						windText += fmt.Sprintf(" G%.0f", windSpeedForDisplay(m.Wgst, windUnit))
					}
					ageText := ""
					if !m.Time.IsZero() {
						ageMinutes := int(math.Round(time.Since(m.Time.UTC()).Minutes()))
						if ageMinutes < 0 {
							ageMinutes = 0
						}
						if ageMinutes < 60 {
							ageText = fmt.Sprintf("%d min", ageMinutes)
						} else {
							ageText = fmt.Sprintf("%dh %02dm", ageMinutes/60, ageMinutes%60)
						}
					}
					items = append(items, windBarbObservation{
						Station: m.Station, Name: m.Station, Wind: windText, ObservationAge: ageText,
						Lat: m.Lat, Lon: m.Lon, Class: "inland", Source: "Aviation Weather Center METAR",
					})
				}
			}
		}

		sort.Slice(items, func(i, j int) bool {
			if items[i].Class != items[j].Class {
				return items[i].Class < items[j].Class
			}
			return items[i].Station < items[j].Station
		})
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "private, max-age=60")
		if err := json.NewEncoder(w).Encode(map[string]interface{}{"observations": items, "errors": errorsOut}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	mux.HandleFunc("/air-temperature-overlay", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		q := r.URL.Query()
		parseBound := func(key string) (float64, error) {
			text := strings.TrimSpace(q.Get(key))
			if text == "" {
				return 0, fmt.Errorf("%s is required", key)
			}
			value, err := strconv.ParseFloat(text, 64)
			if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
				return 0, fmt.Errorf("invalid %s %q", key, text)
			}
			return value, nil
		}
		minLat, err := parseBound("min_lat")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		maxLat, err := parseBound("max_lat")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		minLon, err := parseBound("min_lon")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		maxLon, err := parseBound("max_lon")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if minLat < -90 || maxLat > 90 || minLat >= maxLat || minLon >= maxLon {
			http.Error(w, "invalid air-temperature bounds", http.StatusBadRequest)
			return
		}

		// The temperature overlay is intentionally limited to the contiguous U.S.
		// domain. This avoids expensive world-scale interpolation and Leaflet world-
		// wrap repaint behavior that is not useful for the Bay/Delta sailing service.
		const (
			conusSouth = 20.0
			conusNorth = 55.0
			conusWest  = -130.0
			conusEast  = -60.0
		)
		minLat = math.Max(minLat, conusSouth)
		maxLat = math.Min(maxLat, conusNorth)
		minLon = math.Max(minLon, conusWest)
		maxLon = math.Min(maxLon, conusEast)
		if minLat >= maxLat || minLon >= maxLon {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("Cache-Control", "private, max-age=60")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"observations": []interface{}{}, "min_f": -10, "max_f": 110,
				"note": "Air-temperature overlay is limited to the contiguous United States.",
			})
			return
		}

		type temperatureObservation struct {
			Station string  `json:"station"`
			Name    string  `json:"name,omitempty"`
			Time    string  `json:"time,omitempty"`
			Lat     float64 `json:"lat"`
			Lon     float64 `json:"lon"`
			TempF   float64 `json:"temp_f"`
			Source  string  `json:"source"`
		}
		items := make([]temperatureObservation, 0, 160)
		errorsOut := make([]string, 0, 2)
		cutoff := time.Now().UTC().Add(-90 * time.Minute)
		latest := time.Time{}

		metars, metarErr := fetchCurrentMetars()
		if metarErr != nil {
			errorsOut = append(errorsOut, "Aviation Weather Center METAR cache: "+metarErr.Error())
		} else {
			for _, m := range metars {
				if !m.HasTemp || m.Lat < minLat || m.Lat > maxLat || m.Lon < minLon || m.Lon > maxLon {
					continue
				}
				if !m.Time.IsZero() && m.Time.UTC().Before(cutoff) {
					continue
				}
				if m.Time.After(latest) {
					latest = m.Time
				}
				item := temperatureObservation{
					Station: m.Station, Name: m.Station, Lat: m.Lat, Lon: m.Lon,
					TempF:  math.Round((m.TempC*9/5+32)*10) / 10,
					Source: "NOAA/NWS Aviation Weather Center METAR",
				}
				if !m.Time.IsZero() {
					item.Time = m.Time.UTC().Format(time.RFC3339)
				}
				items = append(items, item)
			}
		}

		stations, stationErr := getActiveNDBCStations()
		if stationErr != nil {
			errorsOut = append(errorsOut, "NOAA/NDBC: "+stationErr.Error())
		} else {
			type candidate struct {
				ID, Name string
				Lat, Lon float64
			}
			candidates := make([]candidate, 0)
			for _, station := range stations {
				if station.Lat >= minLat && station.Lat <= maxLat && station.Lon >= minLon && station.Lon <= maxLon {
					candidates = append(candidates, candidate{ID: station.ID, Name: station.Name, Lat: station.Lat, Lon: station.Lon})
				}
			}
			// Keep global/very-wide views from fan-out fetching hundreds of individual realtime files.
			if len(candidates) > 100 {
				candidates = candidates[:100]
			}
			type stationResult struct {
				item    temperatureObservation
				obsTime time.Time
				ok      bool
			}
			results := make(chan stationResult, len(candidates))
			sem := make(chan struct{}, 8)
			var wg sync.WaitGroup
			for _, station := range candidates {
				station := station
				wg.Add(1)
				go func() {
					defer wg.Done()
					sem <- struct{}{}
					tempF, obsTime, ok := fetchNDBCAirTemperatureObservation(station.ID)
					<-sem
					if !ok || (!obsTime.IsZero() && obsTime.UTC().Before(cutoff)) {
						return
					}
					item := temperatureObservation{
						Station: strings.ToUpper(strings.TrimSpace(station.ID)), Name: station.Name,
						Lat: station.Lat, Lon: station.Lon, TempF: math.Round(tempF*10) / 10,
						Source: "NOAA/NDBC",
					}
					if !obsTime.IsZero() {
						item.Time = obsTime.UTC().Format(time.RFC3339)
					}
					results <- stationResult{item: item, obsTime: obsTime, ok: true}
				}()
			}
			wg.Wait()
			close(results)
			for result := range results {
				if result.ok {
					items = append(items, result.item)
					if result.obsTime.After(latest) {
						latest = result.obsTime
					}
				}
			}
		}
		sort.Slice(items, func(i, j int) bool {
			if items[i].Source != items[j].Source {
				return items[i].Source < items[j].Source
			}
			return items[i].Station < items[j].Station
		})
		payload := struct {
			Updated      string                   `json:"updated,omitempty"`
			Observations []temperatureObservation `json:"observations"`
			Errors       []string                 `json:"errors,omitempty"`
			MinF         float64                  `json:"min_f"`
			MaxF         float64                  `json:"max_f"`
			Note         string                   `json:"note"`
		}{Observations: items, Errors: errorsOut, MinF: -10, MaxF: 110,
			Note: "CONUS-only observed air-temperature heat map interpolated from current NOAA/NDBC and NOAA/NWS METAR stations; forecast mode uses NOAA/NWS NDFD gridded temperature guidance."}
		if !latest.IsZero() {
			payload.Updated = latest.UTC().Format(time.RFC3339)
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "private, max-age=60")
		if err := json.NewEncoder(w).Encode(payload); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	mux.HandleFunc("/air-temperature-grid", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		q := r.URL.Query()
		parseFloat := func(name string) (float64, error) {
			v, err := strconv.ParseFloat(strings.TrimSpace(q.Get(name)), 64)
			if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
				return 0, fmt.Errorf("invalid %s", name)
			}
			return v, nil
		}
		west, err := parseFloat("west")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		south, err := parseFloat("south")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		east, err := parseFloat("east")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		north, err := parseFloat("north")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		hours, err := strconv.Atoi(strings.TrimSpace(q.Get("hours")))
		if err != nil {
			http.Error(w, "invalid hours", http.StatusBadRequest)
			return
		}
		width, err := strconv.Atoi(strings.TrimSpace(q.Get("width")))
		if err != nil {
			http.Error(w, "invalid width", http.StatusBadRequest)
			return
		}
		height, err := strconv.Atoi(strings.TrimSpace(q.Get("height")))
		if err != nil {
			http.Error(w, "invalid height", http.StatusBadRequest)
			return
		}
		if west < -130 || east > -60 || south < 20 || north > 55 || west >= east || south >= north ||
			hours < 0 || hours > 24 || hours%3 != 0 || width < 48 || width > 360 || height < 32 || height > 220 {
			http.Error(w, "invalid CONUS NDFD temperature grid request", http.StatusBadRequest)
			return
		}
		raw, err := fetchNDFDTemperatureGrid(west, south, east, north, hours, width, height)
		if err != nil {
			http.Error(w, "NDFD temperature grid unavailable: "+err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "private, max-age=120")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"west": west, "south": south, "east": east, "north": north,
			"width": width, "height": height, "hours": hours,
			"encoding": "int16-tenths-f-base64-le",
			"nodata":   -32768,
			"data":     base64.StdEncoding.EncodeToString(raw),
			"source":   "NOAA/NWS NDFD raster decoded to temperature cells using the NOAA legend",
		})
	})

	mux.HandleFunc("/air-temperature-point", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		q := r.URL.Query()
		lat, latErr := strconv.ParseFloat(strings.TrimSpace(q.Get("lat")), 64)
		lon, lonErr := strconv.ParseFloat(strings.TrimSpace(q.Get("lon")), 64)
		hours, hoursErr := strconv.Atoi(strings.TrimSpace(q.Get("hours")))
		if latErr != nil || lonErr != nil || hoursErr != nil || math.IsNaN(lat) || math.IsNaN(lon) ||
			lat < 20 || lat > 55 || lon < -130 || lon > -60 || hours < 3 || hours > 24 || hours%3 != 0 {
			http.Error(w, "invalid CONUS forecast temperature point", http.StatusBadRequest)
			return
		}
		layerID, ok := ndfdTemperatureLayerIDServer(hours)
		if !ok {
			http.Error(w, "unsupported forecast hour", http.StatusBadRequest)
			return
		}
		tempF, err := sampleNDFDRenderedTemperature(lat, lon, layerID)
		if err != nil {
			http.Error(w, "NDFD forecast temperature unavailable at this point", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "private, max-age=60")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"temp_f": math.Round(tempF*10) / 10,
			"hours":  hours,
			"source": "NOAA/NWS NDFD rendered temperature field",
		})
	})

	mux.HandleFunc("/smoke-overlay", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		analysisDate, collection, err := fetchNOAAHMSSmoke()
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "private, max-age=300")

		if err != nil {
			if encodeErr := json.NewEncoder(w).Encode(map[string]interface{}{
				"error": err.Error(),
			}); encodeErr != nil {
				http.Error(w, encodeErr.Error(), http.StatusInternalServerError)
			}
			return
		}

		if collection == nil {
			if encodeErr := json.NewEncoder(w).Encode(map[string]interface{}{
				"error": "NOAA HMS satellite smoke analysis returned no usable data.",
			}); encodeErr != nil {
				http.Error(w, encodeErr.Error(), http.StatusInternalServerError)
			}
			return
		}

		if encodeErr := json.NewEncoder(w).Encode(map[string]interface{}{
			"analysis_date": analysisDate,
			"geojson":       collection,
		}); encodeErr != nil {
			http.Error(w, encodeErr.Error(), http.StatusInternalServerError)
		}
	})

	mux.HandleFunc("/fire-detections", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		q := r.URL.Query()
		parseCoord := func(name string) (float64, error) {
			value := strings.TrimSpace(q.Get(name))
			if value == "" {
				return 0, fmt.Errorf("%s is required", name)
			}
			parsed, err := strconv.ParseFloat(value, 64)
			if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
				return 0, fmt.Errorf("%s must be a finite number", name)
			}
			return parsed, nil
		}
		west, err := parseCoord("west")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		south, err := parseCoord("south")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		east, err := parseCoord("east")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		north, err := parseCoord("north")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if west < -180 || west > 180 || east < -180 || east > 180 ||
			south < -90 || south > 90 || north < -90 || north > 90 ||
			west >= east || south >= north {
			http.Error(w, "invalid map bounds", http.StatusBadRequest)
			return
		}

		analysisDate, detections, err := fetchNOAAHMSFireDetections()
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "private, max-age=300")
		if err != nil {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": err.Error()})
			return
		}

		visible := make([]hmsFireDetection, 0)
		for _, detection := range detections {
			if detection.Lon >= west && detection.Lon <= east &&
				detection.Lat >= south && detection.Lat <= north {
				visible = append(visible, detection)
			}
		}
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"analysis_date": analysisDate,
			"detections":    visible,
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	mux.HandleFunc("/fire-perimeters", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		q := r.URL.Query()
		parseCoord := func(name string) (float64, error) {
			value := strings.TrimSpace(q.Get(name))
			if value == "" {
				return 0, fmt.Errorf("%s is required", name)
			}
			parsed, err := strconv.ParseFloat(value, 64)
			if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
				return 0, fmt.Errorf("%s must be a finite number", name)
			}
			return parsed, nil
		}
		west, err := parseCoord("west")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		south, err := parseCoord("south")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		east, err := parseCoord("east")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		north, err := parseCoord("north")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if west < -180 || west > 180 || east < -180 || east > 180 ||
			south < -90 || south > 90 || north < -90 || north > 90 ||
			west >= east || south >= north {
			http.Error(w, "invalid map bounds", http.StatusBadRequest)
			return
		}

		upstream, err := url.Parse(
			"https://services3.arcgis.com/T4QMspbfLg3qTGWY/arcgis/rest/services/" +
				"WFIGS_Interagency_Perimeters_Current/FeatureServer/0/query",
		)
		if err != nil {
			http.Error(w, "fire perimeter service configuration error", http.StatusInternalServerError)
			return
		}
		params := upstream.Query()
		params.Set("where", "attr_IncidentTypeCategory IN ('WF','RX')")
		params.Set("geometry", fmt.Sprintf("%.6f,%.6f,%.6f,%.6f", west, south, east, north))
		params.Set("geometryType", "esriGeometryEnvelope")
		params.Set("inSR", "4326")
		params.Set("outSR", "4326")
		params.Set("spatialRel", "esriSpatialRelIntersects")
		params.Set("outFields",
			"poly_IncidentName,poly_GISAcres,poly_Acres_AutoCalc,"+
				"poly_DateCurrent,poly_PolygonDateTime,poly_IRWINID,"+
				"poly_FeatureStatus,poly_FeatureAccess,poly_IsVisible,"+
				"attr_IncidentName,attr_IncidentSize,attr_CalculatedAcres,"+
				"attr_PercentContained,attr_IncidentTypeCategory,"+
				"attr_POOState,attr_POOCounty,attr_IrwinID,"+
				"attr_ModifiedOnDateTime_dt")
		params.Set("returnGeometry", "true")
		params.Set("geometryPrecision", "5")
		params.Set("maxAllowableOffset", "0.0005")
		params.Set("f", "geojson")
		upstream.RawQuery = params.Encode()

		req, err := http.NewRequest(http.MethodGet, upstream.String(), nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		req.Header.Set("User-Agent", "pittsburg-saildata/"+appVersion)
		req.Header.Set("Accept", "application/geo+json, application/json")

		client := &http.Client{Timeout: 14 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			http.Error(w, "NIFC/WFIGS fire perimeter request failed", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			http.Error(w, fmt.Sprintf("NIFC/WFIGS returned HTTP %d", resp.StatusCode), http.StatusBadGateway)
			return
		}
		body, err := ioutil.ReadAll(io.LimitReader(resp.Body, 10<<20))
		if err != nil {
			http.Error(w, "read NIFC/WFIGS fire perimeter response failed", http.StatusBadGateway)
			return
		}

		var probe struct {
			Type  string `json:"type"`
			Error *struct {
				Code    int      `json:"code"`
				Message string   `json:"message"`
				Details []string `json:"details"`
			} `json:"error,omitempty"`
		}
		if err := json.Unmarshal(body, &probe); err != nil {
			http.Error(w, "invalid NIFC/WFIGS fire perimeter response", http.StatusBadGateway)
			return
		}
		if probe.Error != nil {
			message := strings.TrimSpace(probe.Error.Message)
			if message == "" {
				message = "NIFC/WFIGS fire perimeter service error"
			}
			http.Error(w, message, http.StatusBadGateway)
			return
		}

		filteredBody, _, _, err := filterLatestWFIGSPerimeters(body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}

		w.Header().Set("Content-Type", "application/geo+json; charset=utf-8")
		w.Header().Set("Cache-Control", "private, max-age=120")
		_, _ = w.Write(filteredBody)
	})

	mux.HandleFunc("/pressure-observations", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		q := r.URL.Query()
		parseBound := func(key string) (float64, error) {
			text := strings.TrimSpace(q.Get(key))
			if text == "" {
				return 0, fmt.Errorf("%s is required", key)
			}
			value, err := strconv.ParseFloat(text, 64)
			if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
				return 0, fmt.Errorf("invalid %s %q", key, text)
			}
			return value, nil
		}

		minLat, err := parseBound("min_lat")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		maxLat, err := parseBound("max_lat")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		minLon, err := parseBound("min_lon")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		maxLon, err := parseBound("max_lon")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if minLat < -90 || maxLat > 90 || minLat >= maxLat || minLon < -180 || maxLon > 180 || minLon >= maxLon {
			http.Error(w, "invalid pressure-observation bounds", http.StatusBadRequest)
			return
		}

		// Pull pressure observations from a generous halo around the visible map.
		// The additional stations stabilize the interpolated pressure field near
		// viewport edges without affecting the displayed map extent.
		latSpan := maxLat - minLat
		lonSpan := maxLon - minLon
		latPad := math.Max(1.0, math.Min(6.0, latSpan*0.65))
		lonPad := math.Max(1.0, math.Min(8.0, lonSpan*0.65))
		queryMinLat := math.Max(-90, minLat-latPad)
		queryMaxLat := math.Min(90, maxLat+latPad)
		queryMinLon := math.Max(-180, minLon-lonPad)
		queryMaxLon := math.Min(180, maxLon+lonPad)

		type pressureObservation struct {
			Station    string  `json:"station"`
			Time       string  `json:"time,omitempty"`
			Lat        float64 `json:"lat"`
			Lon        float64 `json:"lon"`
			PressureMB float64 `json:"pressure_mb"`
		}
		items := make([]pressureObservation, 0, 128)
		metars, metarErr := fetchCurrentMetars()
		if metarErr != nil {
			http.Error(w, "Aviation Weather Center METAR cache: "+metarErr.Error(), http.StatusBadGateway)
			return
		}
		latest := time.Time{}
		cutoff := time.Now().UTC().Add(-3 * time.Hour)
		for _, m := range metars {
			if !m.HasPressure ||
				m.Lat < queryMinLat || m.Lat > queryMaxLat ||
				m.Lon < queryMinLon || m.Lon > queryMaxLon {
				continue
			}
			if !m.Time.IsZero() && m.Time.UTC().Before(cutoff) {
				continue
			}
			if m.Time.After(latest) {
				latest = m.Time
			}
			item := pressureObservation{
				Station:    m.Station,
				Lat:        m.Lat,
				Lon:        m.Lon,
				PressureMB: math.Round(m.PressureMB*10) / 10,
			}
			if !m.Time.IsZero() {
				item.Time = m.Time.UTC().Format(time.RFC3339)
			}
			items = append(items, item)
		}
		sort.Slice(items, func(i, j int) bool {
			return items[i].Station < items[j].Station
		})

		payload := struct {
			Updated      string                `json:"updated,omitempty"`
			Observations []pressureObservation `json:"observations"`
			Source       string                `json:"source"`
			Note         string                `json:"note"`
		}{
			Observations: items,
			Source:       "NOAA/NWS Aviation Weather Center METAR sea-level pressure",
			Note:         "Pressure contours are interpolated locally from current METAR mean sea-level pressure observations and are a planning aid, not an official surface-analysis product.",
		}
		if !latest.IsZero() {
			payload.Updated = latest.UTC().Format(time.RFC3339)
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "private, max-age=60")
		if err := json.NewEncoder(w).Encode(payload); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	// Global swell forecast used by the optional surfer-oriented map overlay.
	// The browser may request any whole-hour forecast offset from 0 through 120.
	// Source: PacIOOS public ERDDAP WaveWatch III global model, lon +/-180
	// variant. The model is ~0.5 degree (~50 km) resolution and is appropriate
	// for basin-scale swell tracking, not surf-break-scale forecasting.
	mux.HandleFunc("/swell-forecast", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		q := r.URL.Query()
		parseFinite := func(name string) (float64, error) {
			raw := strings.TrimSpace(q.Get(name))
			if raw == "" {
				return 0, fmt.Errorf("%s is required", name)
			}
			v, err := strconv.ParseFloat(raw, 64)
			if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
				return 0, fmt.Errorf("%s must be a finite number", name)
			}
			return v, nil
		}

		west, err := parseFinite("west")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		south, err := parseFinite("south")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		east, err := parseFinite("east")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		north, err := parseFinite("north")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if west < -180 || east > 180 || south < -90 || north > 90 ||
			west >= east || south >= north {
			http.Error(w, "invalid swell map bounds", http.StatusBadRequest)
			return
		}

		// PacIOOS ww3_global_lon180 exposes longitude samples from -179.5
		// through +179.5. Clamp to the actual ERDDAP axis before constructing
		// the griddap constraint so padded/world-wrap requests never ask for
		// a non-existent 180.0-degree grid point.
		west = math.Max(-179.5, west)
		east = math.Min(179.5, east)

		// The operational global model spans roughly +/-77.5 latitude.
		south = math.Max(-77.5, south)
		north = math.Min(77.5, north)
		if west >= east || south >= north {
			http.Error(w, "map bounds are outside the WaveWatch III grid", http.StatusBadRequest)
			return
		}

		hoursAhead := 0
		if raw := strings.TrimSpace(q.Get("hours")); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < 0 || parsed > 120 {
				http.Error(w, "hours must be an integer from 0 through 120", http.StatusBadRequest)
				return
			}
			hoursAhead = parsed
		}

		// Use one stride for every world-wrap segment in a browser view. When
		// supplied by the browser, this keeps all segments on the same global
		// WW3 sampling lattice. Legacy/direct callers still get the automatic
		// responsive stride calculation.
		const nativeStep = 0.5
		stride := 0
		if raw := strings.TrimSpace(q.Get("stride")); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < 1 || parsed > 20 {
				http.Error(w, "stride must be an integer from 1 through 20", http.StatusBadRequest)
				return
			}
			stride = parsed
		}
		if stride == 0 {
			lonCells := math.Max(1, (east-west)/nativeStep)
			latCells := math.Max(1, (north-south)/nativeStep)
			stride = int(math.Ceil(math.Max(lonCells/48.0, latCells/32.0)))
			if stride < 1 {
				stride = 1
			}
			if stride > 20 {
				stride = 20
			}
		}

		// Axis geometry for ww3_global_lon180. Build latitude/longitude subsets
		// with integer indices aligned to the global origin instead of using
		// coordinate-valued starts. ERDDAP strides are index-based, so this
		// guarantees that independently requested wrapped segments use the same
		// congruence class and cannot drift onto different coarse lattices.
		const (
			lonMinIndex = 0
			lonMaxIndex = 718 // -179.5 .. +179.5 at 0.5 degree spacing
			latMinIndex = 0
			latMaxIndex = 310 // -77.5 .. +77.5 at 0.5 degree spacing
			lonOrigin   = -179.5
			latOrigin   = -77.5
		)

		indexFloor := func(value, origin float64) int {
			return int(math.Floor((value-origin)/nativeStep + 1e-9))
		}
		indexCeil := func(value, origin float64) int {
			return int(math.Ceil((value-origin)/nativeStep - 1e-9))
		}
		alignDown := func(index, step int) int {
			if index < 0 {
				return 0
			}
			return (index / step) * step
		}
		alignUp := func(index, step, maximum int) int {
			if index < 0 {
				index = 0
			}
			aligned := ((index + step - 1) / step) * step
			if aligned > maximum {
				aligned = maximum
			}
			return aligned
		}

		lonStart := alignDown(indexFloor(west, lonOrigin), stride)
		lonStop := alignUp(indexCeil(east, lonOrigin), stride, lonMaxIndex)
		latStart := alignDown(indexFloor(south, latOrigin), stride)
		latStop := alignUp(indexCeil(north, latOrigin), stride, latMaxIndex)

		if lonStart < lonMinIndex {
			lonStart = lonMinIndex
		}
		if lonStop > lonMaxIndex {
			lonStop = lonMaxIndex
		}
		if latStart < latMinIndex {
			latStart = latMinIndex
		}
		if latStop > latMaxIndex {
			latStop = latMaxIndex
		}
		if lonStart > lonStop || latStart > latStop {
			http.Error(w, "map bounds produce an empty WaveWatch III index subset", http.StatusBadRequest)
			return
		}

		forecastBase := time.Now().UTC().Truncate(time.Hour)
		target := forecastBase.Add(time.Duration(hoursAhead) * time.Hour)
		targetText := target.Format("2006-01-02T15:04:05Z")
		upstreamURL := buildSwellUpstreamURL(
			targetText,
			latStart,
			latStop,
			lonStart,
			lonStop,
			stride,
		)

		forceRefresh := strings.TrimSpace(q.Get("refresh")) == "1"
		primeRequest := strings.TrimSpace(q.Get("prime")) == "1"
		raw, cacheStatus, err := getOrFetchSwellRaw(r.Context(), upstreamURL, forceRefresh)
		if err != nil {
			http.Error(w, "PacIOOS WaveWatch III request failed after retry: "+err.Error(), http.StatusBadGateway)
			return
		}

		var upstream struct {
			Table struct {
				ColumnNames []string        `json:"columnNames"`
				Rows        [][]interface{} `json:"rows"`
			} `json:"table"`
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		if err := decoder.Decode(&upstream); err != nil {
			http.Error(w, "could not decode PacIOOS WaveWatch III response: "+err.Error(), http.StatusBadGateway)
			return
		}

		columnIndex := map[string]int{}
		for i, name := range upstream.Table.ColumnNames {
			columnIndex[name] = i
		}
		required := []string{"time", "latitude", "longitude", "shgt", "sper", "sdir"}
		for _, name := range required {
			if _, ok := columnIndex[name]; !ok {
				http.Error(w, "PacIOOS WaveWatch III response missing "+name, http.StatusBadGateway)
				return
			}
		}

		type swellPoint struct {
			Lat          float64 `json:"lat"`
			Lon          float64 `json:"lon"`
			HeightM      float64 `json:"height_m"`
			PeriodS      float64 `json:"period_s"`
			DirectionDEG float64 `json:"direction_deg"`
		}
		points := make([]swellPoint, 0, len(upstream.Table.Rows))
		actualTime := ""

		asFloat := func(row []interface{}, index int) (float64, bool) {
			if index < 0 || index >= len(row) || row[index] == nil {
				return 0, false
			}
			v, ok := row[index].(float64)
			if !ok || math.IsNaN(v) || math.IsInf(v, 0) {
				return 0, false
			}
			return v, true
		}

		for _, row := range upstream.Table.Rows {
			if actualTime == "" {
				if idx := columnIndex["time"]; idx >= 0 && idx < len(row) {
					if t, ok := row[idx].(string); ok {
						actualTime = strings.TrimSpace(t)
					}
				}
			}
			lat, okLat := asFloat(row, columnIndex["latitude"])
			lon, okLon := asFloat(row, columnIndex["longitude"])
			height, okHeight := asFloat(row, columnIndex["shgt"])
			period, okPeriod := asFloat(row, columnIndex["sper"])
			direction, okDirection := asFloat(row, columnIndex["sdir"])
			if !okLat || !okLon || !okHeight || !okPeriod || !okDirection ||
				height < 0 || period <= 0 {
				continue
			}
			points = append(points, swellPoint{
				Lat:          lat,
				Lon:          lon,
				HeightM:      math.Round(height*100) / 100,
				PeriodS:      math.Round(period*10) / 10,
				DirectionDEG: math.Mod(math.Mod(direction, 360)+360, 360),
			})
		}

		payload := struct {
			ForecastTime string       `json:"forecast_time"`
			HoursAhead   int          `json:"hours_ahead"`
			GridStepDEG  float64      `json:"grid_step_deg"`
			GridStride   int          `json:"grid_stride"`
			Points       []swellPoint `json:"points"`
			Source       string       `json:"source"`
			Note         string       `json:"note"`
		}{
			ForecastTime: actualTime,
			HoursAhead:   hoursAhead,
			GridStepDEG:  nativeStep * float64(stride),
			GridStride:   stride,
			Points:       points,
			Source:       "PacIOOS / NOAA-NCEP WaveWatch III global swell forecast",
			Note:         "Approximately 0.5-degree (~50 km) model for basin-scale swell tracking; not a surf-break forecast.",
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "private, max-age=300")
		w.Header().Set("X-Swell-Cache", cacheStatus)
		if err := json.NewEncoder(w).Encode(payload); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Ordinary interactive requests still warm a few likely neighbors. Explicit
		// full playback-cache prime requests suppress this secondary warming so the
		// browser's bounded prime queue remains the only source of upstream load.
		if !primeRequest {
			for _, delta := range []int{3, 6, 1} {
				neighborHours := hoursAhead + delta
				if neighborHours < 0 || neighborHours > 120 {
					continue
				}
				neighborText := forecastBase.
					Add(time.Duration(neighborHours) * time.Hour).
					Format("2006-01-02T15:04:05Z")
				prefetchSwellRaw(buildSwellUpstreamURL(
					neighborText,
					latStart,
					latStop,
					lonStart,
					lonStop,
					stride,
				))
			}
		}
	})

	// NOAA PMEL Saildrone observations used by the optional map overlay.
	// Keep this endpoint independent of the rest of the report so enabling the
	// overlay only fetches the moving-platform feed when requested.
	mux.HandleFunc("/saildrone-observations", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		feed := fetchNOAASaildroneFeed()
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "private, max-age=300")
		if err := json.NewEncoder(w).Encode(feed); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	// Diagnostic sequence number for camera-region requests.
	// ALERTCalifornia / UC San Diego camera locations for the optional map
	// overlay. Query only the current browser viewport, but page through the
	// ArcGIS service so dense views are not silently truncated at its per-query
	// record limit. The browser applies the display-density policy.
	mux.HandleFunc("/alertcalifornia-cameras", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		q := r.URL.Query()
		parseCoord := func(name string) (float64, error) {
			value := strings.TrimSpace(q.Get(name))
			if value == "" {
				return 0, fmt.Errorf("%s is required", name)
			}
			parsed, err := strconv.ParseFloat(value, 64)
			if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
				return 0, fmt.Errorf("%s must be a finite number", name)
			}
			return parsed, nil
		}

		west, err := parseCoord("west")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		south, err := parseCoord("south")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		east, err := parseCoord("east")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		north, err := parseCoord("north")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if west < -180 || west > 180 || east < -180 || east > 180 ||
			south < -90 || south > 90 || north < -90 || north > 90 ||
			west >= east || south >= north {
			http.Error(w, "invalid map bounds", http.StatusBadRequest)
			return
		}

		// Each incoming selection gets a diagnostic ID. Logging has no effect on
		// query count, cache behavior, timeouts, or the client response format.
		requestID := atomic.AddUint64(&alertCaliforniaDiagnosticSerial, 1)
		selectionStarted := time.Now()
		log.Printf("ALERTCAM req=%d incoming bounds=%.6f,%.6f,%.6f,%.6f remote=%s", requestID, west, south, east, north, r.RemoteAddr)
		defer func() {
			log.Printf("ALERTCAM req=%d finished elapsed_ms=%d context_err=%v", requestID, time.Since(selectionStarted).Milliseconds(), r.Context().Err())
		}()

		// Every selection is a fresh ID-first query; no application-level region cache.
		// Stage 1: small ArcGIS ID response. Never fetch full metadata for a
		// geographically dense selection; the client can request a smaller box.
		const detailThreshold = 100
		const service = "https://services8.arcgis.com/X84q166Srnyl4JMV/ArcGIS/rest/services/ALERTCalifornia_Camera_Feed/FeatureServer/0/query"
		client := &http.Client{Timeout: 12 * time.Second}
		ctx, cancel := context.WithTimeout(r.Context(), 22*time.Second)
		defer cancel()
		query := func(stage string, params url.Values) ([]byte, error) {
			upstream, err := url.Parse(service)
			if err != nil {
				return nil, err
			}
			upstream.RawQuery = params.Encode()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, upstream.String(), nil)
			if err != nil {
				return nil, err
			}
			req.Header.Set("Accept", "application/json")
			req.Header.Set("User-Agent", "pittsburg-saildata/"+appVersion)
			upstreamStarted := time.Now()
			log.Printf("ALERTCAM req=%d stage=%s outbound method=GET url=%s accept=%q user_agent=%q", requestID, stage, req.URL.String(), req.Header.Get("Accept"), req.Header.Get("User-Agent"))
			resp, err := client.Do(req)
			if err != nil {
				log.Printf("ALERTCAM req=%d stage=%s transport_error elapsed_ms=%d error=%v", requestID, stage, time.Since(upstreamStarted).Milliseconds(), err)
				return nil, fmt.Errorf("ArcGIS request: %w", err)
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(io.LimitReader(resp.Body, 6<<20))
			if err != nil {
				log.Printf("ALERTCAM req=%d stage=%s read_error http_status=%d elapsed_ms=%d error=%v", requestID, stage, resp.StatusCode, time.Since(upstreamStarted).Milliseconds(), err)
				return nil, fmt.Errorf("ArcGIS read: %w", err)
			}
			log.Printf("ALERTCAM req=%d stage=%s response http_status=%d elapsed_ms=%d bytes=%d retry_after=%q", requestID, stage, resp.StatusCode, time.Since(upstreamStarted).Milliseconds(), len(body), resp.Header.Get("Retry-After"))
			if resp.StatusCode != http.StatusOK {
				return nil, fmt.Errorf("ArcGIS HTTP %d: %.250s", resp.StatusCode, body)
			}
			return body, nil
		}
		params := url.Values{}
		params.Set("where", "1=1")
		params.Set("geometry", fmt.Sprintf("%.6f,%.6f,%.6f,%.6f", west, south, east, north))
		params.Set("geometryType", "esriGeometryEnvelope")
		params.Set("inSR", "4326")
		params.Set("spatialRel", "esriSpatialRelIntersects")
		params.Set("returnIdsOnly", "true")
		params.Set("f", "json")
		idBody, err := query("IDs", params)
		if err != nil {
			http.Error(w, "camera ID query failed: "+err.Error(), http.StatusBadGateway)
			return
		}
		var idsResponse struct {
			ObjectIDs []int `json:"objectIds"`
			Error     *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(idBody, &idsResponse); err != nil {
			log.Printf("ALERTCAM req=%d stage=IDs invalid_json error=%v sample=%.180s", requestID, err, idBody)
			http.Error(w, "invalid ArcGIS ID response", http.StatusBadGateway)
			return
		}
		if idsResponse.Error != nil {
			log.Printf("ALERTCAM req=%d stage=IDs arcgis_error code=%d message=%q", requestID, idsResponse.Error.Code, idsResponse.Error.Message)
			http.Error(w, "ArcGIS ID query: "+idsResponse.Error.Message, http.StatusBadGateway)
			return
		}
		sort.Ints(idsResponse.ObjectIDs)
		log.Printf("ALERTCAM req=%d stage=IDs success id_count=%d threshold=%d", requestID, len(idsResponse.ObjectIDs), detailThreshold)
		// Blue summary: no full feature data is transferred for large regions.
		if len(idsResponse.ObjectIDs) > detailThreshold {
			data, _ := json.Marshal(struct {
				Type         string            `json:"type"`
				Features     []json.RawMessage `json:"features"`
				Summary      bool              `json:"summary"`
				SummaryCount int               `json:"summaryCount"`
			}{Type: "FeatureCollection", Features: []json.RawMessage{}, Summary: true, SummaryCount: len(idsResponse.ObjectIDs)})
			w.Header().Set("Content-Type", "application/geo+json; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			_, _ = w.Write(data)
			log.Printf("ALERTCAM req=%d result=summary count=%d detail_requests=0", requestID, len(idsResponse.ObjectIDs))
			return
		}
		allFeatures := make([]json.RawMessage, 0, len(idsResponse.ObjectIDs))
		for offset := 0; offset < len(idsResponse.ObjectIDs); offset += 100 {
			end := offset + 100
			if end > len(idsResponse.ObjectIDs) {
				end = len(idsResponse.ObjectIDs)
			}
			batch := idsResponse.ObjectIDs[offset:end]
			parts := make([]string, 0, len(batch))
			for _, id := range batch {
				parts = append(parts, strconv.Itoa(id))
			}
			detailParams := url.Values{}
			detailParams.Set("objectIds", strings.Join(parts, ","))
			detailParams.Set("outFields", "OBJECTID,cameraName,siteId,organization,isActive,isOnline,county,cameraURL,networkURL,positionPan,positionTilt,viewZoomScale,viewTime,imageURL")
			detailParams.Set("returnGeometry", "true")
			detailParams.Set("outSR", "4326")
			detailParams.Set("geometryPrecision", "6")
			detailParams.Set("f", "geojson")
			log.Printf("ALERTCAM req=%d stage=DETAILS batch_start=%d batch_count=%d", requestID, offset, len(batch))
			body, err := query("DETAILS", detailParams)
			if err != nil {
				http.Error(w, "camera detail query failed: "+err.Error(), http.StatusBadGateway)
				return
			}
			var page struct {
				Features []json.RawMessage `json:"features"`
				Error    *struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if json.Unmarshal(body, &page) != nil || page.Error != nil || page.Features == nil {
				http.Error(w, "invalid ArcGIS camera detail response", http.StatusBadGateway)
				return
			}
			log.Printf("ALERTCAM req=%d stage=DETAILS batch_result=%d", requestID, len(page.Features))
			allFeatures = append(allFeatures, page.Features...)
		}
		if len(allFeatures) != len(idsResponse.ObjectIDs) {
			http.Error(w, fmt.Sprintf("incomplete ArcGIS camera detail response: got %d of %d", len(allFeatures), len(idsResponse.ObjectIDs)), http.StatusBadGateway)
			return
		}
		data, encodeErr := json.Marshal(struct {
			Type     string            `json:"type"`
			Features []json.RawMessage `json:"features"`
		}{Type: "FeatureCollection", Features: allFeatures})
		if encodeErr != nil {
			http.Error(w, "camera encoding failed", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/geo+json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(data)
		log.Printf("ALERTCAM req=%d result=features count=%d", requestID, len(allFeatures))
	})

	mux.HandleFunc("/wind-stations", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		q := r.URL.Query()

		// lat/lon remain the selected sailing location used for reported
		// distances and station links.
		selectedLat, selectedLon, hasSelected, err := parseOptionalLatLon(q)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if !hasSelected {
			http.Error(w, "lat and lon are required", http.StatusBadRequest)
			return
		}

		// selected_station is the committed wind source. Find Stations must
		// never return it as a candidate marker.
		selectedStation := strings.ToUpper(strings.Trim(
			strings.TrimSpace(q.Get("selected_station")),
			`"'`,
		))

		stations, err := getActiveNDBCStations()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}

		type nearby struct {
			Station          NDBCStation
			SearchDistance   float64
			SelectedDistance float64
		}
		candidates := make([]nearby, 0, len(stations))
		for _, station := range stations {
			searchDistance := distanceNM(selectedLat, selectedLon, station.Lat, station.Lon)
			if searchDistance > windStationMaxDistanceNM {
				continue
			}
			candidates = append(candidates, nearby{
				Station:          station,
				SearchDistance:   searchDistance,
				SelectedDistance: distanceNM(selectedLat, selectedLon, station.Lat, station.Lon),
			})
		}

		sort.Slice(candidates, func(i, j int) bool {
			return candidates[i].SearchDistance < candidates[j].SearchDistance
		})
		if len(candidates) > windStationMaxCandidates {
			candidates = candidates[:windStationMaxCandidates]
		}

		type stationMapCandidate struct {
			Station         string  `json:"station"`
			Name            string  `json:"name"`
			Distance        string  `json:"distance"`
			Wind            string  `json:"wind,omitempty"`
			ObservationAge  string  `json:"observation_age,omitempty"`
			Lat             float64 `json:"lat"`
			Lon             float64 `json:"lon"`
			URL             string  `json:"url"`
			CurrentStation  string  `json:"current_station,omitempty"`
			CurrentName     string  `json:"current_name,omitempty"`
			CurrentDistance string  `json:"current_distance,omitempty"`
			CurrentLat      float64 `json:"current_lat,omitempty"`
			CurrentLon      float64 `json:"current_lon,omitempty"`
			CurrentNote     string  `json:"current_note,omitempty"`
		}
		windUnit := parseWindUnit(q)
		distanceUnit := parseDistanceUnit(q)
		items := make([]stationMapCandidate, 0, len(candidates))
		for _, c := range candidates {
			candidateID := strings.ToUpper(strings.Trim(
				strings.TrimSpace(c.Station.ID),
				`"'`,
			))
			if selectedStation != "" && candidateID == selectedStation {
				continue
			}

			linkQ := cloneQuery(q)
			linkQ.Set("format", "html")
			linkQ.Set("lat", fmt.Sprintf("%.5f", selectedLat))
			linkQ.Set("lon", fmt.Sprintf("%.5f", selectedLon))
			linkQ.Set("station", strings.ToUpper(c.Station.ID))
			linkQ.Del("selected_station")
			linkQ.Del("current_station")
			linkQ.Del("bin")
			item := stationMapCandidate{
				Station:  strings.ToUpper(c.Station.ID),
				Name:     c.Station.Name,
				Distance: formatDistanceNM(c.SelectedDistance, 1, distanceUnit),
				Lat:      c.Station.Lat,
				Lon:      c.Station.Lon,
				URL:      "/report?" + linkQ.Encode(),
			}
			item.Wind, item.ObservationAge = latestNearbyStationWind(item.Station, windUnit)
			currentPreview, currentPreviewErr := previewCurrentStationForPoint(c.Station.Lat, c.Station.Lon)
			switch {
			case currentPreviewErr != nil:
				item.CurrentNote = "Currents preview unavailable."
			case currentPreview == nil:
				item.CurrentNote = fmt.Sprintf(
					"No nearby currents prediction station within %.0f nmi.",
					maxAutoCurrentStationDistanceNM,
				)
			default:
				item.CurrentStation = currentPreview.ID
				item.CurrentName = currentPreview.Name
				item.CurrentDistance = formatDistanceNM(currentPreview.DistanceNM, 1, distanceUnit)
				item.CurrentLat = currentPreview.Lat
				item.CurrentLon = currentPreview.Lon
			}
			items = append(items, item)
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"candidates": items,
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	// Point-weather endpoint for the Local Conditions card. Keep this request
	// independent of the marine-zone forecast so a missing marine text product
	// cannot suppress otherwise valid NWS point forecast data.
	mux.HandleFunc("/point-weather", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		lat, latErr := strconv.ParseFloat(strings.TrimSpace(r.URL.Query().Get("lat")), 64)
		lon, lonErr := strconv.ParseFloat(strings.TrimSpace(r.URL.Query().Get("lon")), 64)
		if latErr != nil || lonErr != nil || lat < -90 || lat > 90 || lon < -180 || lon > 180 {
			http.Error(w, "valid lat and lon are required", http.StatusBadRequest)
			return
		}

		weather, weatherErr := fetchPointWeatherForPoint(lat, lon, loc)
		if weatherErr != nil {
			weather.Error = weatherErr.Error()
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
		if err := json.NewEncoder(w).Encode(weather); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	// Dynamic NWS marine forecast endpoint used when the selected map point
	// changes. This endpoint was referenced by the browser but was not
	// registered in v245.
	mux.HandleFunc("/marine-forecast", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		lat, latErr := strconv.ParseFloat(strings.TrimSpace(r.URL.Query().Get("lat")), 64)
		lon, lonErr := strconv.ParseFloat(strings.TrimSpace(r.URL.Query().Get("lon")), 64)
		if latErr != nil || lonErr != nil || lat < -90 || lat > 90 || lon < -180 || lon > 180 {
			http.Error(w, "valid lat and lon are required", http.StatusBadRequest)
			return
		}

		zone, updated, periods, alerts, forecastErr :=
			fetchMarineForecastForPoint(lat, lon, loc)

		var geometry interface{}
		if zone != "" {
			if encoded := fetchMarineZoneGeometry(zone); encoded != "" {
				_ = json.Unmarshal([]byte(encoded), &geometry)
			}
		}

		payload := map[string]interface{}{
			"zone":     zone,
			"updated":  updated,
			"periods":  periods,
			"alerts":   alerts,
			"geometry": geometry,
		}
		if forecastErr != nil {
			payload["error"] = forecastErr.Error()
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
		if err := json.NewEncoder(w).Encode(payload); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	// Voice-oriented endpoint: return only the live Bottom Line sentences.
	// Reuse the /report calculation path so voice output cannot drift from the
	// browser/text report logic. All normal report query parameters, including
	// station, lat/lon, current overrides, and planning hours, are preserved.
	mux.HandleFunc("/voice", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		voiceRequest := r.Clone(r.Context())
		voiceURL := *r.URL
		q := cloneQuery(r.URL.Query())
		q.Set("bottom_line", "1")
		q.Del("format")
		q.Del("compact")
		voiceURL.Path = "/report"
		voiceURL.RawQuery = q.Encode()
		voiceRequest.URL = &voiceURL
		mux.ServeHTTP(w, voiceRequest)
	})

	mux.HandleFunc("/planning", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		planningRequest := r.Clone(r.Context())
		planningURL := *r.URL
		q := cloneQuery(r.URL.Query())
		q.Set("format", "html")
		q.Set("planning", "1")
		planningURL.Path = "/report"
		planningURL.RawQuery = q.Encode()
		planningRequest.URL = &planningURL
		mux.ServeHTTP(w, planningRequest)
	})

	// History selector AJAX endpoint. Keep its response aligned with the
	// initial HTML wind table, including unit formatting and timestamp ages.
	mux.HandleFunc("/wind-readings", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		stationID := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("station")))
		if stationID == "" {
			stationID = defaultStation
		}
		if !validStationID(stationID) {
			http.Error(w, "invalid station ID", http.StatusBadRequest)
			return
		}
		observations, err := getWindStation(stationID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		if len(observations) == 0 {
			http.Error(w, "no wind observations available", http.StatusBadGateway)
			return
		}
		hours := parseWindReadingHours(r.URL.Query())
		unit := parseWindUnit(r.URL.Query())
		report := buildCurrentWindReport(stationID, observations, loc)
		readings := windObservationsForHours(observations, report.ReportTime, loc, hours)
		items := make([]map[string]string, 0, len(readings))
		for _, reading := range readings {
			windText, gustText := "—", "—"
			direction := strings.TrimSpace(reading.Direction)
			if direction == "" {
				direction = "—"
			}
			if reading.WindKT > 0 {
				windText = formatWindSpeed(reading.WindKT, 1, unit)
			}
			if reading.GustKT > 0 {
				gustText = formatWindSpeed(reading.GustKT, 1, unit)
			}
			age := report.ReportTime.Sub(reading.Time)
			if age < 0 {
				age = 0
			}
			items = append(items, map[string]string{
				"time":      reading.Time.In(loc).Format("3:04 PM"),
				"direction": direction,
				"wind":      windText,
				"gust":      gustText,
				"age":       formatAge(age),
			})
		}
		var summary strings.Builder
		writeWindSummaryDisplay(&summary, report, loc, unit)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"hours":    hours,
			"readings": items,
			"summary":  strings.TrimSpace(summary.String()),
		})
	})

	mux.HandleFunc("/report", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		requestedFormat := strings.ToLower(
			strings.TrimSpace(r.URL.Query().Get("format")),
		)
		htmlRequested := requestedFormat == "html"

		stationID, observations, windSelection, err :=
			resolveHTTPWindStation(r, defaultStation)
		if err != nil {
			// Browser/HTML requests should always get a useful branded page,
			// even if automatic wind-station selection or NOAA retrieval fails.
			if htmlRequested {
				report := &SailingReport{
					Station:            "Requested location",
					RequestQuery:       cloneQuery(r.URL.Query()),
					ReportTime:         time.Now(),
					WindSelection:      windSelection,
					DebugWindSelection: queryBool(r, "debug_wind"),
					WindError:          err.Error(),
					Current: &CurrentReport{
						Error: "Current prediction was not attempted because no usable wind reference station was resolved.",
					},
				}
				writeHTMLReport(w, report, loc)
				return
			}

			// Text diagnostics retain the detailed candidate output.
			if queryBool(r, "debug_wind") &&
				windSelection != nil {
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				fmt.Fprintf(w, "Wind station selection failed: %v\n", err)
				writeWindCandidateDiagnostics(w, windSelection)
				return
			}
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		at := r.URL.Query().Get("at")
		var report *SailingReport

		startHour := queryInt(r, "start", defaultStart)
		endHour := queryInt(r, "end", defaultEnd)

		currentStation := strings.TrimSpace(
			r.URL.Query().Get("current_station"),
		)
		currentBin := queryInt(r, "bin", 0)

		if at != "" {
			// Historical reports retain the NDBC archive rather than the
			// CO-OPS recent-only wind window.
			observations, err = getNDBCWindStation(stationID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			report, err = buildHistoricalWindReport(stationID, observations, at, loc)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			currentDate, currentDateErr := parseCurrentDate(
				r.URL.Query(),
				report.Historical.Requested,
				loc,
			)
			if currentDateErr != nil {
				http.Error(w, currentDateErr.Error(), http.StatusBadRequest)
				return
			}

			current, currentErr := BuildCurrentReport(
				stationID,
				currentStation,
				currentBin,
				currentDate,
				startHour,
				endHour,
				loc,
			)
			if currentErr != nil {
				report.Current = &CurrentReport{
					Error: currentErr.Error(),
				}
			} else {
				report.Current = enforceAutomaticCurrentDistance(current, currentStation)
			}
		} else {
			report = buildCurrentWindReport(stationID, observations, loc)

			currentDate, currentDateErr := parseCurrentDate(
				r.URL.Query(),
				report.ReportTime,
				loc,
			)
			if currentDateErr != nil {
				http.Error(w, currentDateErr.Error(), http.StatusBadRequest)
				return
			}

			current, currentErr := BuildCurrentReport(
				stationID,
				currentStation,
				currentBin,
				currentDate,
				startHour,
				endHour,
				loc,
			)
			if currentErr != nil {
				report.Current = &CurrentReport{
					Error: currentErr.Error(),
				}
			} else {
				report.Current = enforceAutomaticCurrentDistance(current, currentStation)
			}
		}

		if report.Historical == nil {
			windReadingHours := parseWindReadingHours(r.URL.Query())
			report.Latest10 = windObservationsForHours(
				observations,
				report.ReportTime,
				loc,
				windReadingHours,
			)
		}

		report.WindSelection = windSelection
		report.DebugWindSelection = queryBool(r, "debug_wind")
		report.RequestQuery = cloneQuery(r.URL.Query())

		if queryBool(r, "bottom_line") {
			writeVoiceBottomLine(w, report, loc)
			return
		}

		compact := queryBool(r, "compact")
		format := requestedFormat
		if format == "html" {
			writeHTMLReport(w, report, loc)
			return
		}
		jsonOutput := format == "json" || wantsJSON(r)

		if jsonOutput {
			if compact && report.Historical == nil {
				writeCompactJSONReport(w, report)
			} else {
				writeJSONReport(w, report)
			}
		} else if compact && report.Historical == nil {
			writeCompactTextReport(w, report, loc)
		} else {
			writeTextReport(w, report, loc)
		}
	})

	envPort := os.Getenv("PORT")
	if envPort != "" {
		port = envPort
	}

	addr := ":" + port

	fmt.Println("Delta sailing API")
	fmt.Printf("Default wind station: %s\n", defaultStation)
	fmt.Printf("Listening on http://localhost%s\n", addr)

	if err := http.ListenAndServe(addr, mux); err != nil {
		fatal(err)
	}
}

func fetchNDFDTemperatureGrid(west, south, east, north float64, hours, width, height int) ([]byte, error) {
	layerID, ok := ndfdTemperatureLayerIDServer(hours)
	if !ok {
		return nil, fmt.Errorf("unsupported NDFD forecast hour %d", hours)
	}
	colors, err := fetchNDFDLegendColors(layerID)
	if err != nil {
		return nil, err
	}
	x0, y0 := webMercatorXY(south, west)
	x1, y1 := webMercatorXY(north, east)
	if x0 >= x1 || y0 >= y1 {
		return nil, fmt.Errorf("invalid NDFD grid bounds")
	}
	params := url.Values{}
	params.Set("bbox", fmt.Sprintf("%.3f,%.3f,%.3f,%.3f", x0, y0, x1, y1))
	params.Set("bboxSR", "3857")
	params.Set("imageSR", "3857")
	params.Set("size", fmt.Sprintf("%d,%d", width, height))
	params.Set("format", "png32")
	params.Set("transparent", "true")
	params.Set("adjustAspectRatio", "false")
	// Nearest-neighbor preserves NOAA's discrete legend colors so each returned
	// raster cell can be converted back to a temperature deterministically.
	params.Set("interpolation", "RSP_NearestNeighbor")
	params.Set("layers", fmt.Sprintf("show:%d", layerID))
	params.Set("f", "image")
	upstream := "https://mapservices.weather.noaa.gov/raster/rest/services/NDFD/NDFD_temp/MapServer/export?" + params.Encode()

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(upstream)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("NDFD grid export HTTP %d", resp.StatusCode)
	}
	img, err := png.Decode(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	if b.Dx() != width || b.Dy() != height {
		return nil, fmt.Errorf("unexpected NDFD grid dimensions %dx%d", b.Dx(), b.Dy())
	}

	const noData int16 = -32768
	raw := make([]byte, width*height*2)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			r, g, b0, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			value := noData
			if a >= 0x4000 {
				r8, g8, b8 := float64(r>>8), float64(g>>8), float64(b0>>8)
				bestDist := math.MaxFloat64
				bestTemp := 0.0
				for _, c := range colors {
					dr := r8 - float64(c.R)
					dg := g8 - float64(c.G)
					db := b8 - float64(c.B)
					d := dr*dr + dg*dg + db*db
					if d < bestDist {
						bestDist = d
						bestTemp = c.TempF
					}
				}
				if bestDist < math.MaxFloat64 {
					scaled := math.Round(bestTemp * 10)
					if scaled > math.MaxInt16 {
						scaled = math.MaxInt16
					}
					if scaled < math.MinInt16+1 {
						scaled = math.MinInt16 + 1
					}
					value = int16(scaled)
				}
			}
			off := (y*width + x) * 2
			binary.LittleEndian.PutUint16(raw[off:off+2], uint16(value))
		}
	}
	return raw, nil
}

func previewCurrentStationForPoint(lat, lon float64) (*CurrentStation, error) {
	stations, err := getCurrentPredictionStations()
	if err != nil {
		return nil, err
	}
	if len(stations) == 0 {
		return nil, fmt.Errorf("NOAA metadata returned no current prediction stations")
	}

	var best *CurrentStation
	for i := range stations {
		c := stations[i]
		c.DistanceNM = distanceNM(lat, lon, c.Lat, c.Lon)
		c.SelectionScore = currentStationSelectionScore(c)

		if best == nil ||
			c.SelectionScore < best.SelectionScore ||
			(c.SelectionScore == best.SelectionScore && c.DistanceNM < best.DistanceNM) {
			copy := c
			best = &copy
		}
	}
	if best != nil && best.DistanceNM > maxAutoCurrentStationDistanceNM {
		return nil, nil
	}
	return best, nil
}

func enforceAutomaticCurrentDistance(
	current *CurrentReport,
	currentStationOverride string,
) *CurrentReport {
	if current == nil ||
		current.CurrentStation == nil ||
		strings.TrimSpace(currentStationOverride) != "" {
		return current
	}

	if current.CurrentStation.DistanceNM <= maxAutoCurrentStationDistanceNM {
		return current
	}

	return &CurrentReport{
		Error: fmt.Sprintf(
			"No nearby current prediction station available; nearest suitable station is %.1f nmi from the wind station (automatic limit %.0f nmi).",
			current.CurrentStation.DistanceNM,
			maxAutoCurrentStationDistanceNM,
		),
	}
}

type htmlWindCandidate struct {
	Rank            int
	Station         string
	Name            string
	Distance        string
	Wind            string
	ObservationAge  string
	Lat             float64
	Lon             float64
	Met             string
	Status          string
	Reason          string
	Class           string
	URL             string
	JSURL           template.JS
	IsAuto          bool
	IsSelected      bool
	CurrentStation  string
	CurrentName     string
	CurrentDistance string
	CurrentLat      float64
	CurrentLon      float64
	CurrentNote     string
	HasCurrent      bool
}

type htmlWindReading struct {
	Time, Direction, Wind, Gust, Age string
}

type htmlCurrentEvent struct{ Time, Label, Speed, Direction, Class string }

type currentPlanningHint struct {
	Date, Status, Class, Detail      string
	WindowMaxEbbKT, WindowMaxFloodKT float64
	BufferMaxEbbKT, BufferMaxFloodKT float64
}

type tidePredictionStation struct {
	ID   string  `json:"id"`
	Name string  `json:"name"`
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lng"`
	Type string  `json:"type"`
}

type tideRangeDay struct {
	Date  string
	Range float64
}

var tideStationCache struct {
	sync.Mutex
	Fetched time.Time
	Items   []tidePredictionStation
}

type htmlMarineForecastPeriod struct {
	Name     string `json:"name"`
	Forecast string `json:"forecast"`
}

type htmlPointWeather struct {
	Location      string `json:"location,omitempty"`
	AirTemp       string `json:"air_temp,omitempty"`
	HighTemp      string `json:"high_temp,omitempty"`
	LowTemp       string `json:"low_temp,omitempty"`
	ShortForecast string `json:"short_forecast,omitempty"`
	Updated       string `json:"updated,omitempty"`
	Error         string `json:"error,omitempty"`
}

type htmlReportData struct {
	AppVersion                                                    string
	BuildVersion                                                  string
	Title, Station, ReportTime                                    string
	Historical                                                    bool
	RequestedTime                                                 string
	WindDirection, WindSpeed, WindGust, WindAirTemp, WindObserved string
	WindObservedAge                                               string
	WindSummary                                                   string
	WindSelection                                                 string
	WindDistanceWarning                                           string
	WindReadingHours                                              int
	WindUnit                                                      string
	DistanceUnit                                                  string
	WindReadings                                                  []htmlWindReading
	WindCandidates                                                []htmlWindCandidate
	DebugWind                                                     bool
	WindError                                                     string
	UseNearestURL                                                 string
	MapCenterLat, MapCenterLon                                    float64
	MapRequestLat, MapRequestLon                                  float64
	MapWindLat, MapWindLon                                        float64
	MapCurrentLat, MapCurrentLon                                  float64
	MapHasRequest, MapHasWind, MapHasCurrent                      bool
	MapWindStation, MapCurrentStation                             string
	CurrentStation, CurrentMeta                                   string
	CurrentDistanceWarning                                        string
	TideContextMoon                                               string
	TideContextCycle                                              string
	TideContextStation                                            string
	TideContextStationMeta                                        string
	TideContextRange                                              string
	TideContextComparison                                         string
	TideContextNote                                               string
	TideRanges                                                    []tideRangeDay
	TideRangeOverlayAvailable                                     bool
	TideRangeLegendTypical                                        string
	TideRangeLegendElevated                                       string
	TideRangeLegendLarge                                          string
	TideRangeLegendExceptional                                    string
	CurrentAvailabilityStatus                                     string
	CurrentAvailabilityDetail                                     string
	CurrentWindow, CurrentWindowMode                              string
	CurrentDateLabel, CurrentDateISO                              string
	CurrentDays                                                   int
	CurrentRangeLabel                                             string
	FullDetailsURL                                                string
	WindStationsURL                                               string
	PlanningDetails                                               bool
	PlanningDetailsURL, ConditionsURL                             string
	CurrentPrevURL, CurrentTodayURL, CurrentNextURL               string
	CurrentIsToday                                                bool
	CurrentOutlook                                                []string
	CurrentEvents                                                 []htmlCurrentEvent
	CurrentPlanningHints                                          []currentPlanningHint
	PlanningPeriodStatus                                          string
	PlanningPeriodClass                                           string
	PlanningPeriodCause                                           string
	PlanningPeriodDetail                                          string
	PlanningStart, PlanningEnd                                    string
	PlanningCautionEbb, PlanningCautionFlood                      string
	PlanningMaxEbb, PlanningMaxFlood, PlanningBuffer              string
	PlanningCurrentDistanceWarning, PlanningAutoCurrentLimit      string
	CurrentChart                                                  template.HTML
	BottomLineCurrentChart                                        template.HTML
	BottomLineWindSource, BottomLineCurrentSource                 string
	BottomLine                                                    []string
	BottomLineNarrative                                           []string
	FullText                                                      string
	MarineForecastStation                                         string
	MarineForecastZone                                            string
	MarineForecastUpdated                                         string
	MarineForecastPeriods                                         []htmlMarineForecastPeriod
	MarineForecastAlerts                                          []string
	MarineForecastError                                           string
	MarineForecastGeometry                                        template.JS
	SelectedWeatherLocation                                       string
	SelectedWeatherAirTemp                                        string
	SelectedWeatherHighTemp                                       string
	SelectedWeatherLowTemp                                        string
	SelectedWeatherShortForecast                                  string
	SelectedWeatherUpdated                                        string
	SelectedWeatherError                                          string
	Yogiism                                                       string
}

func fetchNWSJSON(endpoint string, target interface{}) error {
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set(
		"User-Agent",
		"pittsburg-saildata/"+appVersion+" (https://github.com/richard-mauri/pittsburg-saildata)",
	)
	req.Header.Set("Accept", "application/geo+json")

	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("NWS returned HTTP %d", resp.StatusCode)
	}

	body, err := ioutil.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("decode NWS response: %w", err)
	}
	return nil
}

type hmsSmokeFeatureCollection struct {
	Type     string            `json:"type"`
	Features []hmsSmokeFeature `json:"features"`
}

type hmsSmokeFeature struct {
	Type       string           `json:"type"`
	Properties hmsSmokeProperty `json:"properties"`
	Geometry   hmsSmokeGeometry `json:"geometry"`
}

type hmsSmokeProperty struct {
	Density string `json:"density"`
}

type hmsSmokeGeometry struct {
	Type        string        `json:"type"`
	Coordinates [][][]float64 `json:"coordinates"`
}

type hmsKMLData struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value"`
}

type hmsKMLSimpleData struct {
	Name  string `xml:"name,attr"`
	Value string `xml:",chardata"`
}

type hmsKMLPolygon struct {
	Outer string   `xml:"outerBoundaryIs>LinearRing>coordinates"`
	Inner []string `xml:"innerBoundaryIs>LinearRing>coordinates"`
}

type hmsKMLPlacemark struct {
	Name        string             `xml:"name"`
	Description string             `xml:"description"`
	Data        []hmsKMLData       `xml:"ExtendedData>Data"`
	SimpleData  []hmsKMLSimpleData `xml:"ExtendedData>SchemaData>SimpleData"`
	Polygon     hmsKMLPolygon      `xml:"Polygon"`
	Polygons    []hmsKMLPolygon    `xml:"MultiGeometry>Polygon"`
}

func normalizeHMSSmokeDensity(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch {
	case value == "21", strings.Contains(value, "heavy"):
		return "heavy"
	case value == "16", strings.Contains(value, "medium"), strings.Contains(value, "moderate"):
		return "medium"
	case value == "5", strings.Contains(value, "light"):
		return "light"
	default:
		return "unknown"
	}
}

func hmsSmokeDensity(placemark hmsKMLPlacemark) string {
	for _, item := range placemark.Data {
		if strings.EqualFold(strings.TrimSpace(item.Name), "density") {
			return normalizeHMSSmokeDensity(item.Value)
		}
	}
	for _, item := range placemark.SimpleData {
		if strings.EqualFold(strings.TrimSpace(item.Name), "density") {
			return normalizeHMSSmokeDensity(item.Value)
		}
	}
	return normalizeHMSSmokeDensity(placemark.Name + " " + placemark.Description)
}

func parseHMSCoordinateRing(text string) [][]float64 {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) < 3 {
		return nil
	}

	points := make([][]float64, 0, len(fields))
	for _, field := range fields {
		parts := strings.Split(field, ",")
		if len(parts) < 2 {
			continue
		}
		lon, lonErr := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
		lat, latErr := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
		if lonErr != nil || latErr != nil ||
			lat < -90 || lat > 90 || lon < -180 || lon > 180 {
			continue
		}
		points = append(points, []float64{lon, lat})
	}
	if len(points) < 3 {
		return nil
	}
	return points
}

func hmsSmokeFeatureFromPolygon(
	polygon hmsKMLPolygon,
	density string,
) (hmsSmokeFeature, bool) {
	outer := parseHMSCoordinateRing(polygon.Outer)
	if len(outer) < 3 {
		return hmsSmokeFeature{}, false
	}

	rings := [][][]float64{outer}
	for _, innerText := range polygon.Inner {
		if inner := parseHMSCoordinateRing(innerText); len(inner) >= 3 {
			rings = append(rings, inner)
		}
	}

	return hmsSmokeFeature{
		Type: "Feature",
		Properties: hmsSmokeProperty{
			Density: density,
		},
		Geometry: hmsSmokeGeometry{
			Type:        "Polygon",
			Coordinates: rings,
		},
	}, true
}

func parseNOAAHMSSmokeKML(body []byte) (*hmsSmokeFeatureCollection, error) {
	decoder := xml.NewDecoder(strings.NewReader(string(body)))
	collection := &hmsSmokeFeatureCollection{
		Type:     "FeatureCollection",
		Features: make([]hmsSmokeFeature, 0),
	}

	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse NOAA HMS smoke KML: %w", err)
		}

		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "Placemark" {
			continue
		}

		var placemark hmsKMLPlacemark
		if err := decoder.DecodeElement(&placemark, &start); err != nil {
			return nil, fmt.Errorf("parse NOAA HMS smoke placemark: %w", err)
		}

		density := hmsSmokeDensity(placemark)
		polygons := placemark.Polygons
		if strings.TrimSpace(placemark.Polygon.Outer) != "" {
			polygons = append([]hmsKMLPolygon{placemark.Polygon}, polygons...)
		}

		for _, polygon := range polygons {
			if feature, ok := hmsSmokeFeatureFromPolygon(polygon, density); ok {
				collection.Features = append(collection.Features, feature)
			}
		}
	}

	return collection, nil
}

func fetchNOAAHMSKML(sourceURL string) ([]byte, int, error) {
	req, err := http.NewRequest(http.MethodGet, sourceURL, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", "pittsburg-saildata/"+appVersion)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, fmt.Errorf("NOAA returned HTTP %d", resp.StatusCode)
	}

	body, err := ioutil.ReadAll(io.LimitReader(resp.Body, 6<<20))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}

type hmsDBFField struct {
	name   string
	offset int
	length int
}

func parseHMSDBFDensities(body []byte) ([]string, error) {
	if len(body) < 32 {
		return nil, fmt.Errorf("NOAA HMS DBF is too short")
	}

	recordCount := int(binary.LittleEndian.Uint32(body[4:8]))
	headerLength := int(binary.LittleEndian.Uint16(body[8:10]))
	recordLength := int(binary.LittleEndian.Uint16(body[10:12]))
	if recordCount < 0 || headerLength < 33 || recordLength < 2 || headerLength > len(body) {
		return nil, fmt.Errorf("NOAA HMS DBF has an invalid header")
	}

	fields := make([]hmsDBFField, 0, 12)
	offset := 1
	for pos := 32; pos+32 <= headerLength; pos += 32 {
		if body[pos] == 0x0D {
			break
		}
		rawName := body[pos : pos+11]
		if nul := bytes.IndexByte(rawName, 0); nul >= 0 {
			rawName = rawName[:nul]
		}
		name := strings.TrimSpace(string(rawName))
		length := int(body[pos+16])
		if length <= 0 {
			continue
		}
		fields = append(fields, hmsDBFField{
			name:   name,
			offset: offset,
			length: length,
		})
		offset += length
	}

	densityField := hmsDBFField{}
	foundDensity := false
	for _, field := range fields {
		if strings.EqualFold(field.name, "density") {
			densityField = field
			foundDensity = true
			break
		}
	}
	if !foundDensity {
		return nil, fmt.Errorf("NOAA HMS DBF has no Density field")
	}

	densities := make([]string, 0, recordCount)
	for i := 0; i < recordCount; i++ {
		start := headerLength + i*recordLength
		end := start + recordLength
		if start < 0 || end > len(body) {
			break
		}
		record := body[start:end]
		if len(record) == 0 || record[0] == '*' {
			densities = append(densities, "light")
			continue
		}
		fieldStart := densityField.offset
		fieldEnd := fieldStart + densityField.length
		if fieldStart < 0 || fieldEnd > len(record) {
			densities = append(densities, "light")
			continue
		}
		densities = append(
			densities,
			normalizeHMSSmokeDensity(string(record[fieldStart:fieldEnd])),
		)
	}
	return densities, nil
}

func parseHMSShapefile(
	body []byte,
	densities []string,
) (*hmsSmokeFeatureCollection, error) {
	if len(body) < 100 {
		return nil, fmt.Errorf("NOAA HMS shapefile is too short")
	}

	collection := &hmsSmokeFeatureCollection{
		Type:     "FeatureCollection",
		Features: make([]hmsSmokeFeature, 0),
	}

	recordIndex := 0
	for pos := 100; pos+8 <= len(body); {
		contentWords := int(binary.BigEndian.Uint32(body[pos+4 : pos+8]))
		contentBytes := contentWords * 2
		recordStart := pos + 8
		recordEnd := recordStart + contentBytes
		if contentBytes < 4 || recordEnd > len(body) {
			break
		}

		record := body[recordStart:recordEnd]
		shapeType := int32(binary.LittleEndian.Uint32(record[0:4]))
		if shapeType == 0 {
			recordIndex++
			pos = recordEnd
			continue
		}

		// Polygon (5), PolygonZ (15), and PolygonM (25) share the same
		// initial polygon/point layout. Z/M payloads follow the XY points
		// and are not needed by the browser overlay.
		if shapeType != 5 && shapeType != 15 && shapeType != 25 {
			recordIndex++
			pos = recordEnd
			continue
		}
		if len(record) < 44 {
			recordIndex++
			pos = recordEnd
			continue
		}

		numParts := int(int32(binary.LittleEndian.Uint32(record[36:40])))
		numPoints := int(int32(binary.LittleEndian.Uint32(record[40:44])))
		if numParts <= 0 || numPoints < 3 {
			recordIndex++
			pos = recordEnd
			continue
		}

		partsStart := 44
		pointsStart := partsStart + 4*numParts
		pointsEnd := pointsStart + 16*numPoints
		if pointsStart > len(record) || pointsEnd > len(record) {
			recordIndex++
			pos = recordEnd
			continue
		}

		partStarts := make([]int, numParts+1)
		validParts := true
		for i := 0; i < numParts; i++ {
			partStarts[i] = int(int32(binary.LittleEndian.Uint32(
				record[partsStart+4*i : partsStart+4*i+4],
			)))
			if partStarts[i] < 0 || partStarts[i] >= numPoints {
				validParts = false
				break
			}
		}
		partStarts[numParts] = numPoints
		if !validParts {
			recordIndex++
			pos = recordEnd
			continue
		}

		density := "light"
		if recordIndex < len(densities) && strings.TrimSpace(densities[recordIndex]) != "" {
			density = densities[recordIndex]
		}

		for part := 0; part < numParts; part++ {
			first := partStarts[part]
			last := partStarts[part+1]
			if first < 0 || last > numPoints || last-first < 3 {
				continue
			}

			ring := make([][]float64, 0, last-first+1)
			for pointIndex := first; pointIndex < last; pointIndex++ {
				pointOffset := pointsStart + 16*pointIndex
				lonBits := binary.LittleEndian.Uint64(record[pointOffset : pointOffset+8])
				latBits := binary.LittleEndian.Uint64(record[pointOffset+8 : pointOffset+16])
				lon := math.Float64frombits(lonBits)
				lat := math.Float64frombits(latBits)
				if math.IsNaN(lon) || math.IsNaN(lat) ||
					math.IsInf(lon, 0) || math.IsInf(lat, 0) ||
					lon < -180 || lon > 180 || lat < -90 || lat > 90 {
					continue
				}
				ring = append(ring, []float64{lon, lat})
			}
			if len(ring) < 3 {
				continue
			}

			firstPoint := ring[0]
			lastPoint := ring[len(ring)-1]
			if firstPoint[0] != lastPoint[0] || firstPoint[1] != lastPoint[1] {
				ring = append(ring, []float64{firstPoint[0], firstPoint[1]})
			}

			collection.Features = append(collection.Features, hmsSmokeFeature{
				Type: "Feature",
				Properties: hmsSmokeProperty{
					Density: density,
				},
				Geometry: hmsSmokeGeometry{
					Type:        "Polygon",
					Coordinates: [][][]float64{ring},
				},
			})
		}

		recordIndex++
		pos = recordEnd
	}

	if len(collection.Features) == 0 {
		return nil, fmt.Errorf("NOAA HMS shapefile contained no usable smoke polygons")
	}
	return collection, nil
}

func parseNOAAHMSSmokeZip(body []byte) (*hmsSmokeFeatureCollection, error) {
	reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return nil, fmt.Errorf("open NOAA HMS smoke ZIP: %w", err)
	}

	var shpBody []byte
	var dbfBody []byte
	for _, file := range reader.File {
		name := strings.ToLower(strings.TrimSpace(file.Name))
		if !strings.HasSuffix(name, ".shp") && !strings.HasSuffix(name, ".dbf") {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			return nil, err
		}
		data, readErr := ioutil.ReadAll(io.LimitReader(rc, 12<<20))
		closeErr := rc.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		switch {
		case strings.HasSuffix(name, ".shp"):
			shpBody = data
		case strings.HasSuffix(name, ".dbf"):
			dbfBody = data
		}
	}

	if len(shpBody) == 0 || len(dbfBody) == 0 {
		return nil, fmt.Errorf("NOAA HMS smoke ZIP is missing .shp or .dbf data")
	}

	densities, err := parseHMSDBFDensities(dbfBody)
	if err != nil {
		return nil, err
	}
	return parseHMSShapefile(shpBody, densities)
}

func fetchNOAAHMSSmokeZip(sourceURL string) ([]byte, time.Time, error) {
	req, err := http.NewRequest(http.MethodGet, sourceURL, nil)
	if err != nil {
		return nil, time.Time{}, err
	}
	req.Header.Set("User-Agent", "pittsburg-saildata/"+appVersion)

	client := &http.Client{Timeout: 12 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, time.Time{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, time.Time{}, fmt.Errorf("NOAA returned HTTP %d", resp.StatusCode)
	}

	body, err := ioutil.ReadAll(io.LimitReader(resp.Body, 12<<20))
	if err != nil {
		return nil, time.Time{}, err
	}

	var modified time.Time
	if value := strings.TrimSpace(resp.Header.Get("Last-Modified")); value != "" {
		if parsed, parseErr := http.ParseTime(value); parseErr == nil {
			modified = parsed.UTC()
		}
	}
	return body, modified, nil
}

func fetchNOAAHMSSmoke() (string, *hmsSmokeFeatureCollection, error) {
	// NOAA's dated KML publication stopped advancing in September 2026 while
	// the ArcGIS/WFS-derived shapefile product continued to update. Prefer
	// NOAA's compact current smoke shapefile ZIP and retain recent dated
	// shapefiles as a fallback.
	currentURL :=
		"https://satepsanone.nesdis.noaa.gov/pub/FIRE/web/HMS/" +
			"Smoke_Polygons/Shapefile/ArcGIS_WFS_source/hms_smoke.zip"

	if body, modified, err := fetchNOAAHMSSmokeZip(currentURL); err == nil {
		if collection, parseErr := parseNOAAHMSSmokeZip(body); parseErr == nil &&
			collection != nil && len(collection.Features) > 0 {
			dateText := "latest"
			if !modified.IsZero() {
				dateText = modified.Format("Jan 2, 2006")
			}
			return dateText, collection, nil
		}
	}

	now := time.Now().UTC()
	for daysBack := 0; daysBack <= 7; daysBack++ {
		day := now.AddDate(0, 0, -daysBack)
		dateText := day.Format("20060102")
		sourceURL := fmt.Sprintf(
			"https://satepsanone.nesdis.noaa.gov/pub/FIRE/web/HMS/"+
				"Smoke_Polygons/Shapefile/%s/%s/hms_smoke%s.zip",
			day.Format("2006"),
			day.Format("01"),
			dateText,
		)

		body, _, err := fetchNOAAHMSSmokeZip(sourceURL)
		if err != nil {
			continue
		}
		collection, err := parseNOAAHMSSmokeZip(body)
		if err != nil || collection == nil || len(collection.Features) == 0 {
			continue
		}
		return day.Format("Jan 2, 2006"), collection, nil
	}

	return "", nil, fmt.Errorf(
		"NOAA HMS satellite smoke analysis is temporarily unavailable; " +
			"no current or recent shapefile analysis could be loaded.",
	)
}

func firePerimeterPropertyString(properties map[string]interface{}, names ...string) string {
	for _, name := range names {
		value, ok := properties[name]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case string:
			if trimmed := strings.TrimSpace(typed); trimmed != "" {
				return trimmed
			}
		default:
			if formatted := strings.TrimSpace(fmt.Sprint(typed)); formatted != "" &&
				formatted != "<nil>" {
				return formatted
			}
		}
	}
	return ""
}

func firePerimeterPropertyNumber(properties map[string]interface{}, names ...string) float64 {
	for _, name := range names {
		value, ok := properties[name]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case float64:
			if !math.IsNaN(typed) && !math.IsInf(typed, 0) {
				return typed
			}
		case float32:
			parsed := float64(typed)
			if !math.IsNaN(parsed) && !math.IsInf(parsed, 0) {
				return parsed
			}
		case int:
			return float64(typed)
		case int64:
			return float64(typed)
		case json.Number:
			if parsed, err := typed.Float64(); err == nil &&
				!math.IsNaN(parsed) && !math.IsInf(parsed, 0) {
				return parsed
			}
		case string:
			if parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64); err == nil &&
				!math.IsNaN(parsed) && !math.IsInf(parsed, 0) {
				return parsed
			}
		}
	}
	return 0
}

func firePerimeterPropertyTime(properties map[string]interface{}, names ...string) float64 {
	for _, name := range names {
		value, ok := properties[name]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case float64:
			if !math.IsNaN(typed) && !math.IsInf(typed, 0) {
				return typed
			}
		case json.Number:
			if parsed, err := typed.Float64(); err == nil {
				return parsed
			}
		case string:
			valueText := strings.TrimSpace(typed)
			if valueText == "" {
				continue
			}
			if parsed, err := strconv.ParseFloat(valueText, 64); err == nil {
				return parsed
			}
			layouts := []string{
				time.RFC3339Nano,
				time.RFC3339,
				"2006-01-02 15:04:05",
				"01/02/2006 03:04:05 PM",
				"1/2/2006 3:04:05 PM",
			}
			for _, layout := range layouts {
				if parsed, err := time.Parse(layout, valueText); err == nil {
					return float64(parsed.UnixNano() / int64(time.Millisecond))
				}
			}
		}
	}
	return 0
}

func firePerimeterIncidentKey(properties map[string]interface{}) string {
	incidentID := firePerimeterPropertyString(
		properties,
		"poly_IRWINID",
		"attr_IrwinID",
	)
	incidentID = strings.ToLower(strings.Trim(strings.TrimSpace(incidentID), "{}"))
	if incidentID != "" {
		return "irwin:" + incidentID
	}

	name := strings.ToLower(strings.TrimSpace(firePerimeterPropertyString(
		properties,
		"attr_IncidentName",
		"poly_IncidentName",
	)))
	category := strings.ToUpper(strings.TrimSpace(firePerimeterPropertyString(
		properties,
		"attr_IncidentTypeCategory",
	)))
	if name != "" {
		return "name:" + category + ":" + name
	}
	return ""
}

func firePerimeterFeatureIsUsable(properties map[string]interface{}) bool {
	status := strings.ToLower(strings.TrimSpace(firePerimeterPropertyString(
		properties,
		"poly_FeatureStatus",
	)))
	if status != "" && status != "approved" {
		return false
	}

	access := strings.ToLower(strings.TrimSpace(firePerimeterPropertyString(
		properties,
		"poly_FeatureAccess",
	)))
	if access != "" && access != "public" {
		return false
	}

	visible := strings.ToLower(strings.TrimSpace(firePerimeterPropertyString(
		properties,
		"poly_IsVisible",
	)))
	if visible != "" && visible != "yes" && visible != "true" && visible != "1" {
		return false
	}

	return true
}

func firePerimeterFeatureRank(properties map[string]interface{}) (float64, float64) {
	timestamp := firePerimeterPropertyTime(
		properties,
		"poly_PolygonDateTime",
		"poly_DateCurrent",
		"attr_ModifiedOnDateTime_dt",
	)
	acres := firePerimeterPropertyNumber(
		properties,
		"poly_GISAcres",
		"poly_Acres_AutoCalc",
		"attr_IncidentSize",
		"attr_CalculatedAcres",
	)
	return timestamp, acres
}

func filterLatestWFIGSPerimeters(body []byte) ([]byte, int, int, error) {
	var document map[string]json.RawMessage
	if err := json.Unmarshal(body, &document); err != nil {
		return nil, 0, 0, fmt.Errorf("decode NIFC/WFIGS GeoJSON: %w", err)
	}

	var features []json.RawMessage
	if rawFeatures, ok := document["features"]; ok {
		if err := json.Unmarshal(rawFeatures, &features); err != nil {
			return nil, 0, 0, fmt.Errorf("decode NIFC/WFIGS features: %w", err)
		}
	}

	type candidate struct {
		raw       json.RawMessage
		timestamp float64
		acres     float64
	}

	sourceCount := len(features)
	selected := make(map[string]candidate)
	unkeyed := make([]json.RawMessage, 0)

	for _, rawFeature := range features {
		var feature struct {
			Properties map[string]interface{} `json:"properties"`
		}
		decoder := json.NewDecoder(bytes.NewReader(rawFeature))
		decoder.UseNumber()
		if err := decoder.Decode(&feature); err != nil {
			continue
		}
		if !firePerimeterFeatureIsUsable(feature.Properties) {
			continue
		}

		key := firePerimeterIncidentKey(feature.Properties)
		if key == "" {
			unkeyed = append(unkeyed, rawFeature)
			continue
		}

		timestamp, acres := firePerimeterFeatureRank(feature.Properties)
		existing, found := selected[key]
		if !found ||
			timestamp > existing.timestamp ||
			(timestamp == existing.timestamp && acres > existing.acres) {
			selected[key] = candidate{
				raw:       rawFeature,
				timestamp: timestamp,
				acres:     acres,
			}
		}
	}

	filtered := make([]json.RawMessage, 0, len(selected)+len(unkeyed))
	for _, candidate := range selected {
		filtered = append(filtered, candidate.raw)
	}
	filtered = append(filtered, unkeyed...)

	filteredJSON, err := json.Marshal(filtered)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("encode filtered NIFC/WFIGS features: %w", err)
	}
	document["features"] = filteredJSON

	sourceCountJSON, _ := json.Marshal(sourceCount)
	incidentCountJSON, _ := json.Marshal(len(filtered))
	document["source_feature_count"] = sourceCountJSON
	document["incident_count"] = incidentCountJSON

	filteredBody, err := json.Marshal(document)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("encode filtered NIFC/WFIGS GeoJSON: %w", err)
	}
	return filteredBody, sourceCount, len(filtered), nil
}

type hmsFireDetection struct {
	Lat       float64 `json:"lat"`
	Lon       float64 `json:"lon"`
	YearDay   string  `json:"year_day,omitempty"`
	TimeUTC   string  `json:"time_utc,omitempty"`
	Satellite string  `json:"satellite,omitempty"`
	Method    string  `json:"method,omitempty"`
	Ecosystem string  `json:"ecosystem,omitempty"`
	FRP       string  `json:"frp,omitempty"`
}

func parseHMSDBFRecords(body []byte) ([]map[string]string, error) {
	if len(body) < 32 {
		return nil, fmt.Errorf("NOAA HMS fire DBF is too short")
	}

	recordCount := int(binary.LittleEndian.Uint32(body[4:8]))
	headerLength := int(binary.LittleEndian.Uint16(body[8:10]))
	recordLength := int(binary.LittleEndian.Uint16(body[10:12]))
	if recordCount < 0 || headerLength < 33 || recordLength < 2 || headerLength > len(body) {
		return nil, fmt.Errorf("NOAA HMS fire DBF has an invalid header")
	}

	fields := make([]hmsDBFField, 0, 16)
	offset := 1
	for pos := 32; pos+32 <= headerLength; pos += 32 {
		if body[pos] == 0x0D {
			break
		}
		rawName := body[pos : pos+11]
		if nul := bytes.IndexByte(rawName, 0); nul >= 0 {
			rawName = rawName[:nul]
		}
		name := strings.TrimSpace(string(rawName))
		length := int(body[pos+16])
		if name == "" || length <= 0 {
			continue
		}
		fields = append(fields, hmsDBFField{name: name, offset: offset, length: length})
		offset += length
	}

	records := make([]map[string]string, 0, recordCount)
	for i := 0; i < recordCount; i++ {
		start := headerLength + i*recordLength
		end := start + recordLength
		if start < 0 || end > len(body) {
			break
		}
		record := body[start:end]
		values := make(map[string]string, len(fields))
		if len(record) == 0 || record[0] == '*' {
			records = append(records, values)
			continue
		}
		for _, field := range fields {
			fieldStart := field.offset
			fieldEnd := fieldStart + field.length
			if fieldStart < 0 || fieldEnd > len(record) {
				continue
			}
			values[strings.ToLower(field.name)] = strings.TrimSpace(string(record[fieldStart:fieldEnd]))
		}
		records = append(records, values)
	}
	return records, nil
}

func parseHMSFireShapefile(
	body []byte,
	records []map[string]string,
) ([]hmsFireDetection, error) {
	if len(body) < 100 {
		return nil, fmt.Errorf("NOAA HMS fire shapefile is too short")
	}

	detections := make([]hmsFireDetection, 0)
	recordIndex := 0
	for pos := 100; pos+8 <= len(body); {
		contentWords := int(binary.BigEndian.Uint32(body[pos+4 : pos+8]))
		contentBytes := contentWords * 2
		recordStart := pos + 8
		recordEnd := recordStart + contentBytes
		if contentBytes < 20 || recordEnd > len(body) {
			break
		}
		record := body[recordStart:recordEnd]
		shapeType := int32(binary.LittleEndian.Uint32(record[0:4]))
		if shapeType == 1 || shapeType == 11 || shapeType == 21 {
			lon := math.Float64frombits(binary.LittleEndian.Uint64(record[4:12]))
			lat := math.Float64frombits(binary.LittleEndian.Uint64(record[12:20]))
			if !math.IsNaN(lon) && !math.IsNaN(lat) &&
				!math.IsInf(lon, 0) && !math.IsInf(lat, 0) &&
				lon >= -180 && lon <= 180 && lat >= -90 && lat <= 90 {
				fields := map[string]string{}
				if recordIndex < len(records) {
					fields = records[recordIndex]
				}
				first := func(names ...string) string {
					for _, name := range names {
						if value := strings.TrimSpace(fields[strings.ToLower(name)]); value != "" {
							return value
						}
					}
					return ""
				}
				detections = append(detections, hmsFireDetection{
					Lat:       lat,
					Lon:       lon,
					YearDay:   first("yearday", "year_day"),
					TimeUTC:   first("time", "time_utc"),
					Satellite: first("satellite"),
					Method:    first("method", "source"),
					Ecosystem: first("ecosystem", "ecosys"),
					FRP:       first("frp"),
				})
			}
		}
		recordIndex++
		pos = recordEnd
	}
	if len(detections) == 0 {
		return nil, fmt.Errorf("NOAA HMS fire shapefile contained no usable fire detections")
	}
	return detections, nil
}

func parseNOAAHMSFireZip(body []byte) ([]hmsFireDetection, error) {
	reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return nil, fmt.Errorf("open NOAA HMS fire ZIP: %w", err)
	}

	var shpBody []byte
	var dbfBody []byte
	for _, file := range reader.File {
		name := strings.ToLower(strings.TrimSpace(file.Name))
		if !strings.HasSuffix(name, ".shp") && !strings.HasSuffix(name, ".dbf") {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			return nil, err
		}
		data, readErr := ioutil.ReadAll(io.LimitReader(rc, 12<<20))
		closeErr := rc.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		switch {
		case strings.HasSuffix(name, ".shp"):
			shpBody = data
		case strings.HasSuffix(name, ".dbf"):
			dbfBody = data
		}
	}
	if len(shpBody) == 0 || len(dbfBody) == 0 {
		return nil, fmt.Errorf("NOAA HMS fire ZIP is missing .shp or .dbf data")
	}
	records, err := parseHMSDBFRecords(dbfBody)
	if err != nil {
		return nil, err
	}
	return parseHMSFireShapefile(shpBody, records)
}

func fetchNOAAHMSFireDetections() (string, []hmsFireDetection, error) {
	currentURL :=
		"https://satepsanone.nesdis.noaa.gov/pub/FIRE/web/HMS/" +
			"Fire_Points/Shapefile/ArcGIS_WFS_source/hms_fire.zip"

	if body, modified, err := fetchNOAAHMSSmokeZip(currentURL); err == nil {
		if detections, parseErr := parseNOAAHMSFireZip(body); parseErr == nil && len(detections) > 0 {
			dateText := "latest"
			if !modified.IsZero() {
				dateText = modified.Format("Jan 2, 2006")
			}
			return dateText, detections, nil
		}
	}

	now := time.Now().UTC()
	for daysBack := 0; daysBack <= 7; daysBack++ {
		day := now.AddDate(0, 0, -daysBack)
		dateText := day.Format("20060102")
		sourceURL := fmt.Sprintf(
			"https://satepsanone.nesdis.noaa.gov/pub/FIRE/web/HMS/"+
				"Fire_Points/Shapefile/%s/%s/hms_fire%s.zip",
			day.Format("2006"),
			day.Format("01"),
			dateText,
		)
		body, _, err := fetchNOAAHMSSmokeZip(sourceURL)
		if err != nil {
			continue
		}
		detections, err := parseNOAAHMSFireZip(body)
		if err == nil && len(detections) > 0 {
			return day.Format("Jan 2, 2006"), detections, nil
		}
	}
	return "", nil, fmt.Errorf(
		"NOAA HMS satellite fire detections are temporarily unavailable; " +
			"no current or recent shapefile analysis could be loaded.",
	)
}

func marineZoneID(zoneURL string) string {
	zoneURL = strings.TrimRight(strings.TrimSpace(zoneURL), "/")
	if zoneURL == "" {
		return ""
	}
	if index := strings.LastIndex(zoneURL, "/"); index >= 0 && index+1 < len(zoneURL) {
		return strings.ToUpper(strings.TrimSpace(zoneURL[index+1:]))
	}
	return strings.ToUpper(zoneURL)
}

func fetchMarineZoneGeometry(zoneID string) template.JS {
	zoneID = strings.ToUpper(strings.TrimSpace(zoneID))
	if len(zoneID) != 6 {
		return ""
	}
	for i, r := range zoneID {
		if i < 3 {
			if r < 'A' || r > 'Z' {
				return ""
			}
		} else if r < '0' || r > '9' {
			return ""
		}
	}

	var payload struct {
		Geometry json.RawMessage `json:"geometry"`
	}
	if err := fetchNWSJSON(
		"https://api.weather.gov/zones/forecast/"+url.PathEscape(zoneID),
		&payload,
	); err != nil {
		return ""
	}
	if len(payload.Geometry) == 0 || string(payload.Geometry) == "null" {
		return ""
	}

	// Geometry comes directly from the NWS GeoJSON response. Re-marshal it so
	// only syntactically valid JSON is passed to the browser template.
	var geometry interface{}
	if err := json.Unmarshal(payload.Geometry, &geometry); err != nil {
		return ""
	}
	encoded, err := json.Marshal(geometry)
	if err != nil {
		return ""
	}
	return template.JS(encoded)
}

func fetchNWSText(endpoint string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set(
		"User-Agent",
		"pittsburg-saildata/"+appVersion+" (https://github.com/richard-mauri/pittsburg-saildata)",
	)
	req.Header.Set("Accept", "text/plain")

	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("NWS returned HTTP %d", resp.StatusCode)
	}

	body, err := ioutil.ReadAll(io.LimitReader(resp.Body, 512<<10))
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func parseMarineZoneForecastText(
	body string,
	zoneID string,
	loc *time.Location,
) (string, []htmlMarineForecastPeriod, error) {
	zoneID = strings.ToUpper(strings.TrimSpace(zoneID))
	lines := strings.Split(body, "\n")
	inZone := false
	updated := ""
	periods := make([]htmlMarineForecastPeriod, 0, 4)

	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}

		if !inZone {
			if strings.HasPrefix(strings.ToUpper(line), zoneID+"-") {
				inZone = true
			}
			continue
		}

		if line == "$$" {
			break
		}

		// Advisory headlines in the text product begin with three dots.
		// Active alerts are retrieved separately from api.weather.gov.
		if strings.HasPrefix(line, "...") {
			continue
		}

		if strings.HasPrefix(line, ".") {
			if split := strings.Index(line[1:], "..."); split >= 0 {
				split++
				name := strings.TrimSpace(line[1:split])
				forecast := strings.TrimSpace(line[split+3:])
				if name != "" {
					if len(periods) == 4 {
						break
					}
					periods = append(periods, htmlMarineForecastPeriod{
						Name:     strings.Title(strings.ToLower(name)),
						Forecast: forecast,
					})
					continue
				}
			}
		}

		if len(periods) > 0 {
			last := &periods[len(periods)-1]
			if last.Forecast == "" {
				last.Forecast = line
			} else {
				last.Forecast += " " + line
			}
			continue
		}

		// Before the first period, the final ordinary line is the issuance time.
		// Zone-description lines are overwritten until that issuance line arrives.
		updated = line
	}

	if len(periods) == 0 {
		return "", nil, fmt.Errorf("NWS marine text forecast returned no forecast periods")
	}

	// NWS marine text products use local-zone timestamps such as
	// "429 AM PDT Mon Aug 31 2026". Keep the published string as-is; it is
	// already concise and avoids inventing an offset from an abbreviation.
	_ = loc
	return updated, periods, nil
}

func fetchMarineForecastForPoint(
	lat float64,
	lon float64,
	loc *time.Location,
) (string, string, []htmlMarineForecastPeriod, []string, error) {
	if lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return "", "", nil, nil, fmt.Errorf("invalid forecast coordinates")
	}

	pointsURL := fmt.Sprintf(
		"https://api.weather.gov/points/%.4f,%.4f",
		lat,
		lon,
	)
	var pointResponse struct {
		Properties struct {
			ForecastZone string `json:"forecastZone"`
		} `json:"properties"`
	}
	if err := fetchNWSJSON(pointsURL, &pointResponse); err != nil {
		return "", "", nil, nil, fmt.Errorf("NWS point lookup: %w", err)
	}

	zoneURL := strings.TrimRight(
		strings.TrimSpace(pointResponse.Properties.ForecastZone),
		"/",
	)
	zoneID := marineZoneID(zoneURL)
	if zoneURL == "" || zoneID == "" {
		return "", "", nil, nil, fmt.Errorf(
			"NWS point lookup did not return a forecast zone",
		)
	}
	if len(zoneID) < 2 {
		return zoneID, "", nil, nil, fmt.Errorf("invalid NWS forecast zone %q", zoneID)
	}

	// Alerts are point-based and remain useful whether the selected NWS
	// forecast zone is marine or land-based.
	alertsURL := fmt.Sprintf(
		"https://api.weather.gov/alerts/active?point=%.4f,%.4f",
		lat,
		lon,
	)
	var alertsResponse struct {
		Features []struct {
			Properties struct {
				Event string `json:"event"`
			} `json:"properties"`
		} `json:"features"`
	}
	alerts := make([]string, 0, 3)
	if err := fetchNWSJSON(alertsURL, &alertsResponse); err == nil {
		seen := make(map[string]bool)
		for _, feature := range alertsResponse.Features {
			event := strings.TrimSpace(feature.Properties.Event)
			if event == "" || seen[event] {
				continue
			}
			seen[event] = true
			alerts = append(alerts, event)
			if len(alerts) == 3 {
				break
			}
		}
	}

	// Coastal marine-zone text forecasts are published by NWS/TGFTP and are
	// indexed by the first two letters of the zone ID, e.g. PZZ530 -> pz/pzz530.txt.
	// A normal public forecast zone such as CAZ302 still has useful NWS geometry,
	// but there is no coastal marine text product for it. Treat that as an
	// expected product distinction rather than exposing an HTTP 404 to the user.
	marineTextURL := fmt.Sprintf(
		"https://tgftp.nws.noaa.gov/data/forecasts/marine/coastal/%s/%s.txt",
		strings.ToLower(zoneID[:2]),
		strings.ToLower(zoneID),
	)
	body, err := fetchNWSText(marineTextURL)
	if err != nil {
		if strings.Contains(err.Error(), "HTTP 404") {
			return zoneID, "", nil, alerts, fmt.Errorf(
				"No coastal marine text forecast is published for NWS forecast zone %s. The forecast-zone boundary is still available on the map for geographic context.",
				zoneID,
			)
		}
		return zoneID, "", nil, alerts, fmt.Errorf(
			"NWS marine forecast is temporarily unavailable for forecast zone %s.",
			zoneID,
		)
	}
	updated, periods, err := parseMarineZoneForecastText(body, zoneID, loc)
	if err != nil {
		return zoneID, "", nil, alerts, fmt.Errorf(
			"NWS marine forecast could not be read for forecast zone %s.",
			zoneID,
		)
	}

	return zoneID, updated, periods, alerts, nil
}

func formatNWSForecastTemperature(value int, unit string) string {
	unit = strings.ToUpper(strings.TrimSpace(unit))
	if unit == "" {
		unit = "F"
	}
	return fmt.Sprintf("%d°%s", value, unit)
}

func fetchPointWeatherForPoint(
	lat float64,
	lon float64,
	loc *time.Location,
) (htmlPointWeather, error) {
	var result htmlPointWeather
	if lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return result, fmt.Errorf("invalid forecast coordinates")
	}

	pointsURL := fmt.Sprintf(
		"https://api.weather.gov/points/%.4f,%.4f",
		lat,
		lon,
	)
	var pointResponse struct {
		Properties struct {
			Forecast         string `json:"forecast"`
			ForecastHourly   string `json:"forecastHourly"`
			RelativeLocation struct {
				Properties struct {
					City  string `json:"city"`
					State string `json:"state"`
				} `json:"properties"`
			} `json:"relativeLocation"`
		} `json:"properties"`
	}
	if err := fetchNWSJSON(pointsURL, &pointResponse); err != nil {
		return result, fmt.Errorf("NWS point weather lookup: %w", err)
	}

	city := strings.TrimSpace(pointResponse.Properties.RelativeLocation.Properties.City)
	state := strings.TrimSpace(pointResponse.Properties.RelativeLocation.Properties.State)
	switch {
	case city != "" && state != "":
		result.Location = city + ", " + state
	case city != "":
		result.Location = city
	case state != "":
		result.Location = state
	}

	type forecastPeriod struct {
		StartTime       string `json:"startTime"`
		IsDaytime       bool   `json:"isDaytime"`
		Temperature     int    `json:"temperature"`
		TemperatureUnit string `json:"temperatureUnit"`
		ShortForecast   string `json:"shortForecast"`
	}

	forecastURL := strings.TrimSpace(pointResponse.Properties.Forecast)
	if forecastURL != "" {
		var forecastResponse struct {
			Properties struct {
				Updated string           `json:"updated"`
				Periods []forecastPeriod `json:"periods"`
			} `json:"properties"`
		}
		if err := fetchNWSJSON(forecastURL, &forecastResponse); err == nil {
			result.Updated = strings.TrimSpace(forecastResponse.Properties.Updated)
			for _, period := range forecastResponse.Properties.Periods {
				if result.ShortForecast == "" {
					result.ShortForecast = strings.TrimSpace(period.ShortForecast)
				}
				if period.IsDaytime && result.HighTemp == "" {
					result.HighTemp = formatNWSForecastTemperature(period.Temperature, period.TemperatureUnit)
				}
				if !period.IsDaytime && result.LowTemp == "" {
					result.LowTemp = formatNWSForecastTemperature(period.Temperature, period.TemperatureUnit)
				}
				if result.HighTemp != "" && result.LowTemp != "" && result.ShortForecast != "" {
					break
				}
			}
		}
	}

	hourlyURL := strings.TrimSpace(pointResponse.Properties.ForecastHourly)
	if hourlyURL != "" {
		var hourlyResponse struct {
			Properties struct {
				Updated string           `json:"updated"`
				Periods []forecastPeriod `json:"periods"`
			} `json:"properties"`
		}
		if err := fetchNWSJSON(hourlyURL, &hourlyResponse); err == nil && len(hourlyResponse.Properties.Periods) > 0 {
			period := hourlyResponse.Properties.Periods[0]
			result.AirTemp = formatNWSForecastTemperature(period.Temperature, period.TemperatureUnit)
			if strings.TrimSpace(period.ShortForecast) != "" {
				result.ShortForecast = strings.TrimSpace(period.ShortForecast)
			}
			if result.Updated == "" {
				result.Updated = strings.TrimSpace(hourlyResponse.Properties.Updated)
			}
		}
	}

	if result.AirTemp == "" && result.HighTemp == "" && result.LowTemp == "" && result.ShortForecast == "" {
		return result, fmt.Errorf("NWS point forecast returned no usable weather periods")
	}

	if parsed, err := time.Parse(time.RFC3339, result.Updated); err == nil {
		result.Updated = parsed.In(loc).Format("Jan 2, 3:04 PM")
	}
	return result, nil
}

func randomYogiism() string {
	body, err := ioutil.ReadFile("assets/yogiisms.txt")
	if err != nil {
		return ""
	}

	quotes := make([]string, 0)
	for _, rawLine := range strings.Split(string(body), "\n") {
		quote := strings.TrimSpace(rawLine)
		if quote == "" {
			continue
		}
		if len(quote) >= 2 && quote[0] == '"' && quote[len(quote)-1] == '"' {
			quote = strings.TrimSpace(quote[1 : len(quote)-1])
		}
		if quote != "" {
			quotes = append(quotes, quote)
		}
	}

	if len(quotes) == 0 {
		return ""
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	return quotes[rng.Intn(len(quotes))]
}

func setDynamicHTMLHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
}

func writeHTMLReport(w http.ResponseWriter, report *SailingReport, loc *time.Location) {
	setDynamicHTMLHeaders(w)
	data := makeHTMLReportData(report, loc)
	data.Yogiism = randomYogiism()
	if strings.TrimSpace(report.RequestQuery.Get("details")) == "1" {
		if err := sailingDetailsHTMLTemplate.Execute(w, data); err != nil {
			http.Error(w, err.Error(), 500)
		}
		return
	}
	if strings.TrimSpace(report.RequestQuery.Get("stations")) == "1" {
		if err := sailingStationsHTMLTemplate.Execute(w, data); err != nil {
			http.Error(w, err.Error(), 500)
		}
		return
	}
	if err := sailingHTMLTemplate.Execute(w, data); err != nil {
		http.Error(w, err.Error(), 500)
	}
}

func heroLocationTitle(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return name
	}

	parts := strings.SplitN(name, " - ", 2)
	if len(parts) != 2 {
		return name
	}

	prefix := strings.TrimSpace(parts[0])
	if prefix == "" {
		return name
	}
	for _, r := range prefix {
		if r < '0' || r > '9' {
			return name
		}
	}

	title := strings.TrimSpace(parts[1])
	if title == "" {
		return name
	}
	return title
}

func getTidePredictionStations() ([]tidePredictionStation, error) {
	tideStationCache.Lock()
	defer tideStationCache.Unlock()

	if len(tideStationCache.Items) > 0 &&
		time.Since(tideStationCache.Fetched) < 24*time.Hour {
		return append([]tidePredictionStation(nil), tideStationCache.Items...), nil
	}

	req, err := http.NewRequest(
		http.MethodGet,
		"https://api.tidesandcurrents.noaa.gov/mdapi/prod/webapi/stations.json?type=tidepredictions",
		nil,
	)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "pittsburg-saildata/"+appVersion)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("NOAA tide-station metadata returned HTTP %d", resp.StatusCode)
	}

	var payload struct {
		Stations []tidePredictionStation `json:"stations"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&payload); err != nil {
		return nil, err
	}
	if len(payload.Stations) == 0 {
		return nil, fmt.Errorf("NOAA tide-station metadata returned no prediction stations")
	}

	tideStationCache.Items = append([]tidePredictionStation(nil), payload.Stations...)
	tideStationCache.Fetched = time.Now()

	return append([]tidePredictionStation(nil), payload.Stations...), nil
}

func selectTidePredictionStation(
	windLat, windLon float64,
	currentStation *CurrentStation,
) (*tidePredictionStation, float64, float64, error) {
	stations, err := getTidePredictionStations()
	if err != nil {
		return nil, 0, 0, err
	}

	var best *tidePredictionStation
	bestScore := math.MaxFloat64
	bestWindDistance := 0.0
	bestCurrentDistance := 0.0

	for i := range stations {
		station := stations[i]
		windDistance := distanceNM(windLat, windLon, station.Lat, station.Lon)
		currentDistance := windDistance
		score := windDistance

		if currentStation != nil {
			currentDistance = distanceNM(
				currentStation.Lat,
				currentStation.Lon,
				station.Lat,
				station.Lon,
			)
			// Prefer a tide station that represents the same general water
			// body as both selected stations, rather than merely the one
			// geographically closest to the wind sensor.
			score = math.Max(windDistance, currentDistance) +
				0.25*(windDistance+currentDistance)
		}

		if score < bestScore {
			copy := station
			best = &copy
			bestScore = score
			bestWindDistance = windDistance
			bestCurrentDistance = currentDistance
		}
	}

	if best == nil {
		return nil, 0, 0, fmt.Errorf("no NOAA tide prediction station found")
	}
	return best, bestWindDistance, bestCurrentDistance, nil
}

func fetchTideHighLowRanges(
	stationID string,
	centerDay time.Time,
	loc *time.Location,
) ([]tideRangeDay, error) {
	begin := centerDay.AddDate(0, 0, -14)
	end := centerDay.AddDate(0, 0, 14)

	q := url.Values{}
	q.Set("product", "predictions")
	q.Set("application", "pittsburg-saildata")
	q.Set("begin_date", begin.Format("20060102"))
	q.Set("end_date", end.Format("20060102"))
	q.Set("datum", "MLLW")
	q.Set("station", stationID)
	q.Set("time_zone", "gmt")
	q.Set("units", "english")
	q.Set("interval", "hilo")
	q.Set("format", "json")

	req, err := http.NewRequest(
		http.MethodGet,
		"https://api.tidesandcurrents.noaa.gov/api/prod/datagetter?"+q.Encode(),
		nil,
	)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "pittsburg-saildata/"+appVersion)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("NOAA tide predictions returned HTTP %d", resp.StatusCode)
	}

	var payload struct {
		Predictions []struct {
			Time  string `json:"t"`
			Value string `json:"v"`
			Type  string `json:"type"`
		} `json:"predictions"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&payload); err != nil {
		return nil, err
	}
	if payload.Error != nil && strings.TrimSpace(payload.Error.Message) != "" {
		return nil, fmt.Errorf("%s", strings.TrimSpace(payload.Error.Message))
	}

	type extremes struct {
		Min    float64
		Max    float64
		HasMin bool
		HasMax bool
	}
	byDay := make(map[string]extremes)

	for _, prediction := range payload.Predictions {
		t, err := time.Parse("2006-01-02 15:04", prediction.Time)
		if err != nil {
			continue
		}
		t = t.UTC().In(loc)

		var value float64
		if _, err := fmt.Sscanf(prediction.Value, "%f", &value); err != nil {
			continue
		}

		key := t.Format("2006-01-02")
		x := byDay[key]
		switch strings.ToUpper(strings.TrimSpace(prediction.Type)) {
		case "H":
			if !x.HasMax || value > x.Max {
				x.Max = value
				x.HasMax = true
			}
		case "L":
			if !x.HasMin || value < x.Min {
				x.Min = value
				x.HasMin = true
			}
		}
		byDay[key] = x
	}

	var ranges []tideRangeDay
	for offset := -14; offset <= 14; offset++ {
		day := centerDay.AddDate(0, 0, offset)
		key := day.Format("2006-01-02")
		x, ok := byDay[key]
		if !ok || !x.HasMin || !x.HasMax {
			continue
		}
		ranges = append(ranges, tideRangeDay{
			Date:  key,
			Range: x.Max - x.Min,
		})
	}
	if len(ranges) == 0 {
		return nil, fmt.Errorf("NOAA returned no usable high/low tide predictions")
	}
	return ranges, nil
}

func approximateMoonContext(day time.Time) (string, string) {
	const synodicMonth = 29.530588853

	// Commonly used mean-new-moon epoch. This is intentionally an
	// approximate lunar-cycle indicator, not an astronomical ephemeris.
	epoch := time.Date(2000, 1, 6, 18, 14, 0, 0, time.UTC)
	noon := time.Date(day.Year(), day.Month(), day.Day(), 12, 0, 0, 0, day.Location()).UTC()
	age := noon.Sub(epoch).Hours() / 24.0
	age = math.Mod(age, synodicMonth)
	if age < 0 {
		age += synodicMonth
	}

	phase := ""
	switch {
	case age < 1.84566:
		phase = "New moon"
	case age < 5.53699:
		phase = "Waxing crescent"
	case age < 9.22831:
		phase = "First quarter"
	case age < 12.91963:
		phase = "Waxing gibbous"
	case age < 16.61096:
		phase = "Full moon"
	case age < 20.30228:
		phase = "Waning gibbous"
	case age < 23.99361:
		phase = "Last quarter"
	case age < 27.68493:
		phase = "Waning crescent"
	default:
		phase = "New moon"
	}

	circularDistance := func(a, b float64) float64 {
		d := math.Abs(a - b)
		return math.Min(d, synodicMonth-d)
	}

	toNew := circularDistance(age, 0)
	toFull := circularDistance(age, synodicMonth/2)
	toFirstQuarter := circularDistance(age, synodicMonth/4)
	toLastQuarter := circularDistance(age, 3*synodicMonth/4)

	cycle := "Between spring- and neap-tide phases."
	switch {
	case toNew <= 3:
		cycle = fmt.Sprintf("Spring-tide period near new moon (about %.1f days from new moon).", toNew)
	case toFull <= 3:
		cycle = fmt.Sprintf("Spring-tide period near full moon (about %.1f days from full moon).", toFull)
	case toFirstQuarter <= 3 || toLastQuarter <= 3:
		quarterDistance := math.Min(toFirstQuarter, toLastQuarter)
		cycle = fmt.Sprintf("Neap-tide period near quarter moon (about %.1f days from quarter moon).", quarterDistance)
	}

	return "Approximate lunar phase: " + phase + ".", cycle
}

func tideRangeContext(
	ranges []tideRangeDay,
	centerDay time.Time,
) (string, string) {
	targetKey := centerDay.Format("2006-01-02")
	target := -1.0
	values := make([]float64, 0, len(ranges))

	for _, day := range ranges {
		if day.Range <= 0 {
			continue
		}
		values = append(values, day.Range)
		if day.Date == targetKey {
			target = day.Range
		}
	}
	if target < 0 || len(values) == 0 {
		return "", ""
	}

	sort.Float64s(values)
	median := values[len(values)/2]
	maxRange := values[len(values)-1]

	summary := fmt.Sprintf(
		"Predicted tidal range today: %.1f ft (high-to-low range at the tide reference station).",
		target,
	)

	comparison := ""
	switch {
	case maxRange > 0 && target >= 0.90*maxRange:
		comparison = fmt.Sprintf(
			"Today's predicted range is near the largest in the surrounding 28-day window (maximum %.1f ft).",
			maxRange,
		)
	case median > 0 && target >= 1.15*median:
		comparison = fmt.Sprintf(
			"Today's predicted range is elevated versus the surrounding 28-day median of %.1f ft.",
			median,
		)
	case median > 0 && target <= 0.85*median:
		comparison = fmt.Sprintf(
			"Today's predicted range is smaller than the surrounding 28-day median of %.1f ft.",
			median,
		)
	default:
		comparison = fmt.Sprintf(
			"Today's predicted range is near the surrounding 28-day median of %.1f ft.",
			median,
		)
	}

	return summary, comparison
}

func populateTideContext(
	d *htmlReportData,
	report *SailingReport,
	day time.Time,
	loc *time.Location,
) {
	if d == nil {
		return
	}

	d.TideContextMoon, d.TideContextCycle = approximateMoonContext(day)
	d.TideContextNote =
		"Lunar phase and tide range provide context only; they do not by themselves change the Preferred/Caution/Red Flag planning classification."

	if report == nil || report.Current == nil || report.Current.WindReference == nil {
		d.TideContextStationMeta =
			"NOAA tide-station context unavailable for this report."
		return
	}

	wind := report.Current.WindReference
	station, windDistance, currentDistance, err := selectTidePredictionStation(
		wind.Lat,
		wind.Lon,
		report.Current.CurrentStation,
	)
	if err != nil {
		d.TideContextStationMeta =
			"NOAA tide-station context unavailable: " + err.Error()
		return
	}

	d.TideContextStation = station.Name + " (" + station.ID + ")"
	if report.Current.CurrentStation != nil {
		d.TideContextStationMeta = fmt.Sprintf(
			"Automatically selected tide reference · %.1f nmi from wind station · %.1f nmi from currents station.",
			windDistance,
			currentDistance,
		)
	} else {
		d.TideContextStationMeta = fmt.Sprintf(
			"Automatically selected tide reference · %.1f nmi from wind station.",
			windDistance,
		)
	}

	ranges, err := fetchTideHighLowRanges(station.ID, day, loc)
	if err != nil {
		d.TideContextRange =
			"Tide-range comparison unavailable: " + err.Error()
		return
	}
	d.TideRanges = append([]tideRangeDay(nil), ranges...)
	d.TideRangeOverlayAvailable = len(d.TideRanges) > 0
	d.TideContextRange, d.TideContextComparison = tideRangeContext(ranges, day)
}

func formatObservationAge(age time.Duration) string {
	if age < 0 {
		age = 0
	}

	totalMinutes := int(age.Round(time.Minute) / time.Minute)
	if totalMinutes < 1 {
		return "JUST NOW"
	}
	if totalMinutes < 60 {
		return fmt.Sprintf("%d MIN AGO", totalMinutes)
	}

	hours := totalMinutes / 60
	minutes := totalMinutes % 60
	if hours < 24 {
		if minutes == 0 {
			if hours == 1 {
				return "1 HR AGO"
			}
			return fmt.Sprintf("%d HRS AGO", hours)
		}
		if hours == 1 {
			return fmt.Sprintf("1 HR %d MIN AGO", minutes)
		}
		return fmt.Sprintf("%d HRS %d MIN AGO", hours, minutes)
	}

	days := totalMinutes / (24 * 60)
	if days == 1 {
		return "1 DAY AGO"
	}
	return fmt.Sprintf("%d DAYS AGO", days)
}

func makeHTMLReportData(report *SailingReport, loc *time.Location) htmlReportData {
	d := htmlReportData{
		AppVersion:   appVersion,
		BuildVersion: buildVersion,
		Station:      report.Station,
		Title:        report.Station,
	}
	d.DebugWind = report.DebugWindSelection
	d.WindError = strings.TrimSpace(report.WindError)
	d.WindUnit = parseWindUnit(report.RequestQuery)
	d.DistanceUnit = parseDistanceUnit(report.RequestQuery)
	detailsQuery := cloneQuery(report.RequestQuery)
	detailsQuery.Set("format", "html")
	detailsQuery.Set("details", "1")
	detailsQuery.Del("stations")
	d.FullDetailsURL = "/report?" + detailsQuery.Encode()

	stationsQuery := cloneQuery(report.RequestQuery)
	stationsQuery.Set("format", "html")
	stationsQuery.Set("stations", "1")
	stationsQuery.Del("details")
	d.WindStationsURL = "/report?" + stationsQuery.Encode()

	d.PlanningDetails = strings.TrimSpace(report.RequestQuery.Get("planning")) == "1"
	planningQuery := cloneQuery(report.RequestQuery)
	planningQuery.Del("format")
	planningQuery.Del("details")
	planningQuery.Del("stations")
	planningQuery.Set("planning", "1")
	d.PlanningDetailsURL = "/planning?" + planningQuery.Encode()

	conditionsQuery := cloneQuery(report.RequestQuery)
	conditionsQuery.Set("format", "html")
	conditionsQuery.Del("planning")
	conditionsQuery.Del("details")
	conditionsQuery.Del("stations")
	d.ConditionsURL = "/report?" + conditionsQuery.Encode()
	if !report.ReportTime.IsZero() {
		d.ReportTime = report.ReportTime.In(loc).Format("Mon Jan 2, 2006 · 3:04 PM MST")
	}
	if report.Historical != nil {
		d.Historical = true
		d.RequestedTime = report.Historical.Requested.In(loc).Format("Mon Jan 2, 2006 · 3:04 PM")
	}
	if report.Current != nil && report.Current.WindReference != nil && strings.TrimSpace(report.Current.WindReference.Name) != "" {
		d.Title = heroLocationTitle(report.Current.WindReference.Name)
	}
	var latest *WindObservation

	// Historical reports should show the observation closest to the
	// requested timestamp. Current reports should show Latest. Latest10 is
	// a defensive fallback so the HTML card never goes blank when recent
	// observations are available.
	if report.Historical != nil && report.Historical.Closest != nil {
		latest = report.Historical.Closest
	} else if report.Latest != nil {
		latest = report.Latest
	} else if len(report.Latest10) > 0 {
		copy := report.Latest10[0]
		latest = &copy
	}

	if latest != nil {
		d.WindDirection = latest.Direction
		if d.WindDirection == "" {
			d.WindDirection = "—"
		}

		if latest.WindKT > 0 {
			d.WindSpeed = formatWindSpeed(latest.WindKT, 0, d.WindUnit)
		} else {
			d.WindSpeed = "—"
		}

		if latest.GustKT > 0 {
			d.WindGust = formatWindSpeed(latest.GustKT, 0, d.WindUnit)
		} else {
			d.WindGust = "—"
		}

		d.WindObserved = latest.Time.In(loc).Format("3:04 PM")
		if report.Historical == nil {
			d.WindObservedAge = formatObservationAge(time.Since(latest.Time))
			if latest.Source != "" {
				d.WindObservedAge += " · " + latest.Source
			}
			if latest.Stale {
				d.WindObservedAge += " · STALE"
			}
		}
	} else {
		d.WindDirection = "—"
		d.WindSpeed = "—"
		d.WindGust = "—"
		d.WindObserved = "Wind observation unavailable"
	}

	// NDBC realtime2 observations include ATMP (air temperature) at stations
	// that report it. Keep temperature tied to the selected wind station. Do not
	// mix a live temperature into a historical report, and avoid the extra NOAA
	// request on the voice-only Bottom Line path.
	if report.Historical == nil &&
		strings.TrimSpace(report.RequestQuery.Get("bottom_line")) != "1" {
		if airTempF, ok := fetchNDBCAirTemperatureF(report.Station); ok {
			d.WindAirTemp = fmt.Sprintf("%.0f°F", airTempF)
		}
	}

	// The browser marine forecast follows the selected map location whenever
	// lat/lon are present. If no location has been selected yet, fall back to the
	// committed wind station so the initial page can still provide useful marine
	// context. The browser refreshes this forecast and the zone overlay when the
	// selected location changes.
	if report.Historical == nil &&
		strings.TrimSpace(report.RequestQuery.Get("bottom_line")) != "1" &&
		strings.TrimSpace(report.RequestQuery.Get("details")) != "1" &&
		strings.TrimSpace(report.RequestQuery.Get("stations")) != "1" &&
		d.WindError == "" {
		forecastLat, forecastLon, hasForecastPoint, _ :=
			parseOptionalLatLon(report.RequestQuery)
		if hasForecastPoint {
			d.MarineForecastStation = "selected location"
		} else if stationMeta, metaErr := fetchNDBCStation(report.Station); metaErr == nil {
			forecastLat = stationMeta.Lat
			forecastLon = stationMeta.Lon
			hasForecastPoint = true
			d.MarineForecastStation = report.Station
		}
		if hasForecastPoint {
			zone, updated, periods, alerts, forecastErr :=
				fetchMarineForecastForPoint(forecastLat, forecastLon, loc)
			d.MarineForecastZone = zone
			d.MarineForecastUpdated = updated
			d.MarineForecastPeriods = periods
			d.MarineForecastAlerts = alerts
			if zone != "" {
				d.MarineForecastGeometry = fetchMarineZoneGeometry(zone)
			}
			if forecastErr != nil {
				d.MarineForecastError = forecastErr.Error()
			}
		}
	}

	// Point weather is tied only to an explicitly selected sailing location.
	// The browser refreshes this block immediately when the selected location changes.
	if report.Historical == nil &&
		strings.TrimSpace(report.RequestQuery.Get("bottom_line")) != "1" &&
		strings.TrimSpace(report.RequestQuery.Get("details")) != "1" &&
		strings.TrimSpace(report.RequestQuery.Get("stations")) != "1" {
		if selectedLat, selectedLon, hasSelectedPoint, _ := parseOptionalLatLon(report.RequestQuery); hasSelectedPoint {
			weather, weatherErr := fetchPointWeatherForPoint(selectedLat, selectedLon, loc)
			d.SelectedWeatherLocation = weather.Location
			d.SelectedWeatherAirTemp = weather.AirTemp
			d.SelectedWeatherHighTemp = weather.HighTemp
			d.SelectedWeatherLowTemp = weather.LowTemp
			d.SelectedWeatherShortForecast = weather.ShortForecast
			d.SelectedWeatherUpdated = weather.Updated
			if weatherErr != nil {
				d.SelectedWeatherError = weatherErr.Error()
			}
		}
	}

	// Generate the same wind summary used by the text report when wind
	// data are available. Error pages use the explicit WindError instead.
	if d.WindError == "" {
		var windText strings.Builder
		if report.Historical != nil {
			if report.Historical.Closest != nil {
				printWindObservationDisplay(
					&windText,
					report.Historical.Closest,
					loc,
					report.Historical.Requested,
					d.WindUnit,
				)
			}
		} else {
			writeWindSummaryDisplay(
				&windText,
				report,
				loc,
				d.WindUnit,
			)
		}
		d.WindSummary = strings.TrimSpace(windText.String())
	}

	d.WindReadingHours = parseWindReadingHours(report.RequestQuery)
	if report.Historical == nil {
		for _, reading := range report.Latest10 {
			windText := "—"
			gustText := "—"
			direction := strings.TrimSpace(reading.Direction)
			if direction == "" {
				direction = "—"
			}
			if reading.WindKT > 0 {
				windText = formatWindSpeed(reading.WindKT, 1, d.WindUnit)
			}
			if reading.GustKT > 0 {
				gustText = formatWindSpeed(reading.GustKT, 1, d.WindUnit)
			}
			age := report.ReportTime.Sub(reading.Time)
			if age < 0 {
				age = 0
			}
			d.WindReadings = append(d.WindReadings, htmlWindReading{
				Time:      reading.Time.In(loc).Format("3:04 PM"),
				Direction: direction,
				Wind:      windText,
				Gust:      gustText,
				Age:       formatAge(age),
			})
		}
	}

	if report.WindSelection != nil {
		s := report.WindSelection

		if _, _, hasSelectedLocation, _ := parseOptionalLatLon(report.RequestQuery); hasSelectedLocation &&
			s.DistanceNM >= windDistanceWarningNM {
			d.WindDistanceWarning = fmt.Sprintf(
				"Wind station is %.1f nmi from the selected location. Local wind may differ significantly.",
				s.DistanceNM,
			)
		}

		name := strings.TrimSpace(s.StationName)
		selectionPrefix := "Selected"
		if strings.EqualFold(s.Mode, "manual-override") {
			selectionPrefix = "Manual override"
		}

		if strings.EqualFold(s.Mode, "station-browser") {
			if name != "" {
				d.WindSelection = fmt.Sprintf(
					"%s %s — %s; observation %d min old",
					selectionPrefix,
					s.StationID,
					name,
					s.ObservationAgeMinutes,
				)
			} else {
				d.WindSelection = fmt.Sprintf(
					"%s %s; observation %d min old",
					selectionPrefix,
					s.StationID,
					s.ObservationAgeMinutes,
				)
			}
		} else if strings.TrimSpace(s.StationID) == "" {
			d.WindSelection = fmt.Sprintf(
				"Requested location %.5f, %.5f",
				s.RequestedLat,
				s.RequestedLon,
			)
		} else if name != "" {
			d.WindSelection = fmt.Sprintf(
				"%s %s — %s, %.1f nmi from %.5f, %.5f; observation %d min old",
				selectionPrefix,
				s.StationID,
				name,
				s.DistanceNM,
				s.RequestedLat,
				s.RequestedLon,
				s.ObservationAgeMinutes,
			)
		} else {
			d.WindSelection = fmt.Sprintf(
				"%s %s, %.1f nmi from %.5f, %.5f; observation %d min old",
				selectionPrefix,
				s.StationID,
				s.DistanceNM,
				s.RequestedLat,
				s.RequestedLon,
				s.ObservationAgeMinutes,
			)
		}
	}

	windSourceID := strings.ToUpper(strings.TrimSpace(report.Station))
	if report.WindSelection != nil {
		windSourceID = strings.ToUpper(strings.TrimSpace(report.WindSelection.StationID))
		windSourceName := strings.TrimSpace(report.WindSelection.StationName)
		if windSourceID != "" && windSourceName != "" {
			d.BottomLineWindSource = fmt.Sprintf("%s — %s", windSourceID, windSourceName)
		} else {
			d.BottomLineWindSource = windSourceID
		}
	} else {
		d.BottomLineWindSource = windSourceID
	}

	if report.Current != nil && report.Current.CurrentStation != nil {
		s := report.Current.CurrentStation
		currentID := strings.TrimSpace(s.ID)
		currentName := strings.TrimSpace(s.Name)
		switch {
		case currentID != "" && currentName != "":
			d.BottomLineCurrentSource = fmt.Sprintf("%s — %s", currentID, currentName)
		case currentID != "":
			d.BottomLineCurrentSource = currentID
		default:
			d.BottomLineCurrentSource = currentName
		}
		if strings.TrimSpace(report.Current.Bin) != "" {
			d.BottomLineCurrentSource += fmt.Sprintf(" · bin %s", report.Current.Bin)
		}
		if s.DistanceNM > 0 {
			d.BottomLineCurrentSource += fmt.Sprintf(" · %.1f nmi from wind station", s.DistanceNM)
		}
	}

	if _, _, hasUserLocation, _ := parseOptionalLatLon(report.RequestQuery); report.WindSelection != nil && hasUserLocation {
		for i, candidate := range report.WindSelection.Candidates {
			className := "candidate-bad"
			if strings.EqualFold(candidate.WindStatus, "usable") {
				className = "candidate-good"
			}

			isSelected := strings.EqualFold(
				candidate.StationID,
				report.WindSelection.StationID,
			)
			isAuto := strings.HasPrefix(
				candidate.Reason,
				"[AUTO] ",
			)
			reason := strings.TrimPrefix(
				candidate.Reason,
				"[AUTO] ",
			)

			if isSelected {
				className += " candidate-selected"
			}
			if isAuto {
				className += " candidate-auto"
			}

			linkQuery := cloneQuery(report.RequestQuery)
			linkQuery.Set(
				"station",
				strings.ToUpper(candidate.StationID),
			)
			linkQuery.Set("format", "html")
			linkQuery.Del("stations")
			linkQuery.Del("details")
			if report.DebugWindSelection {
				linkQuery.Set("debug_wind", "1")
			} else {
				linkQuery.Del("debug_wind")
			}

			item := htmlWindCandidate{
				Rank:       i + 1,
				Station:    strings.ToUpper(candidate.StationID),
				Name:       candidate.StationName,
				Distance:   formatDistanceNM(candidate.DistanceNM, 1, d.DistanceUnit),
				Lat:        candidate.Lat,
				Lon:        candidate.Lon,
				Met:        candidate.Met,
				Status:     strings.ToUpper(candidate.WindStatus),
				Reason:     reason,
				Class:      className,
				URL:        "/report?" + linkQuery.Encode(),
				JSURL:      template.JS(fmt.Sprintf("%q", "/report?"+linkQuery.Encode())),
				IsAuto:     isAuto,
				IsSelected: isSelected,
			}
			item.Wind, item.ObservationAge = latestNearbyStationWind(item.Station, d.WindUnit)
			currentPreview, currentPreviewErr := previewCurrentStationForPoint(candidate.Lat, candidate.Lon)
			switch {
			case currentPreviewErr != nil:
				item.CurrentNote = "Currents preview unavailable."
			case currentPreview == nil:
				item.CurrentNote = fmt.Sprintf(
					"No nearby currents prediction station within %.0f nmi.",
					maxAutoCurrentStationDistanceNM,
				)
			default:
				item.HasCurrent = true
				item.CurrentStation = currentPreview.ID
				item.CurrentName = currentPreview.Name
				item.CurrentDistance = formatDistanceNM(currentPreview.DistanceNM, 1, d.DistanceUnit)
				item.CurrentLat = currentPreview.Lat
				item.CurrentLon = currentPreview.Lon
			}
			d.WindCandidates = append(d.WindCandidates, item)
		}
	}

	if report.WindSelection != nil {
		_, _, hasUserLocation, _ :=
			parseOptionalLatLon(report.RequestQuery)
		if hasUserLocation {
			nearestQuery := cloneQuery(report.RequestQuery)
			nearestQuery.Del("station")
			nearestQuery.Del("stations")
			nearestQuery.Del("details")
			nearestQuery.Set("format", "html")
			if report.DebugWindSelection {
				nearestQuery.Set("debug_wind", "1")
			} else {
				nearestQuery.Del("debug_wind")
			}
			d.UseNearestURL = "/report?" + nearestQuery.Encode()
		}
	}

	// Map chooser: the user's requested lat/lon is the primary point.
	// Otherwise center on the selected/default wind station. Also expose
	// wind/current source locations so the user can see where the data come from.
	if lat, lon, ok, _ := parseOptionalLatLon(report.RequestQuery); ok {
		d.MapHasRequest = true
		d.MapRequestLat = lat
		d.MapRequestLon = lon
		d.MapCenterLat = lat
		d.MapCenterLon = lon
	}

	if report.WindSelection != nil &&
		(report.WindSelection.StationLat != 0 ||
			report.WindSelection.StationLon != 0) {
		d.MapHasWind = true
		d.MapWindLat = report.WindSelection.StationLat
		d.MapWindLon = report.WindSelection.StationLon
		d.MapWindStation = report.WindSelection.StationID

		if !d.MapHasRequest {
			d.MapCenterLat = d.MapWindLat
			d.MapCenterLon = d.MapWindLon
		}
	} else if stationMeta, err := fetchNDBCStation(report.Station); err == nil {
		d.MapHasWind = true
		d.MapWindLat = stationMeta.Lat
		d.MapWindLon = stationMeta.Lon
		d.MapWindStation = stationMeta.ID

		if !d.MapHasRequest {
			d.MapCenterLat = stationMeta.Lat
			d.MapCenterLon = stationMeta.Lon
		}
	}

	if report.Current != nil && report.Current.Error != "" {
		const noNearbyPrefix = "No nearby current prediction station available;"
		if strings.HasPrefix(report.Current.Error, noNearbyPrefix) {
			d.CurrentAvailabilityStatus = "no-nearby-station"
			d.CurrentAvailabilityDetail = strings.TrimSpace(strings.TrimPrefix(report.Current.Error, noNearbyPrefix))
		}
	}

	// Classify the automatic "no nearby currents station" case independently
	// from BuildCurrentReport's generic error.  The candidate preview already
	// uses this exact geographic test; repeat it here for the committed wind
	// station so the Current card cannot collapse this state into an endpoint
	// failure.
	if d.CurrentAvailabilityStatus == "" &&
		strings.TrimSpace(report.RequestQuery.Get("current_station")) == "" &&
		report.WindSelection != nil &&
		(report.WindSelection.StationLat != 0 || report.WindSelection.StationLon != 0) {
		currentPreview, currentPreviewErr := previewCurrentStationForPoint(
			report.WindSelection.StationLat,
			report.WindSelection.StationLon,
		)
		if currentPreviewErr == nil && currentPreview == nil {
			d.CurrentAvailabilityStatus = "no-nearby-station"
			d.CurrentAvailabilityDetail = fmt.Sprintf(
				"The nearest suitable station is beyond the %.0f nmi automatic-selection limit.",
				maxAutoCurrentStationDistanceNM,
			)
		}
	}

	if report.Current != nil &&
		report.Current.CurrentStation != nil {
		currentStation := report.Current.CurrentStation
		if currentStation.Lat != 0 || currentStation.Lon != 0 {
			d.MapHasCurrent = true
			d.MapCurrentLat = currentStation.Lat
			d.MapCurrentLon = currentStation.Lon
			d.MapCurrentStation = currentStation.ID
		}
	}

	// Last-resort Bay/Delta center. This should rarely be needed because the
	// default PSBC1 station normally supplies the center.
	if d.MapCenterLat == 0 && d.MapCenterLon == 0 {
		d.MapCenterLat = 37.90
		d.MapCenterLon = -122.05
	}

	if report.Current != nil && report.Current.Error == "" {
		currentDay := report.Current.Start.In(loc)
		if currentDay.IsZero() {
			currentDay = report.ReportTime.In(loc)
		}
		today := time.Now().In(loc)
		d.CurrentDays = parseCurrentDays(report.RequestQuery)

		rangeEnd := currentDay.AddDate(0, 0, d.CurrentDays-1)
		if d.CurrentDays == 1 {
			d.CurrentDateLabel = currentDay.Format("Mon Jan 2, 2006")
			d.CurrentRangeLabel = d.CurrentDateLabel
		} else {
			d.CurrentDateLabel = currentDay.Format("Mon Jan 2")
			d.CurrentRangeLabel = fmt.Sprintf(
				"%s – %s",
				currentDay.Format("Mon Jan 2"),
				rangeEnd.Format("Mon Jan 2, 2006"),
			)
		}
		d.CurrentDateISO = currentDay.Format("2006-01-02")
		d.CurrentIsToday =
			currentDay.Year() == today.Year() &&
				currentDay.YearDay() == today.YearDay()

		populateTideContext(&d, report, currentDay, loc)
		if tideMedian := tideRangeMedian(d.TideRanges); tideMedian > 0 {
			elevatedThreshold := tideMedian * (1 + elevatedTideRangePercent/100)
			largeThreshold := tideMedian * (1 + largeTideRangePercent/100)
			exceptionalThreshold := tideMedian * (1 + exceptionalTideRangePercent/100)
			d.TideRangeLegendTypical = fmt.Sprintf("Normal-cycle < %.1f ft", elevatedThreshold)
			d.TideRangeLegendElevated = fmt.Sprintf("Elevated ≥ %.1f ft (+%.0f%%)", elevatedThreshold, elevatedTideRangePercent)
			d.TideRangeLegendLarge = fmt.Sprintf("Large ≥ %.1f ft (+%.0f%%)", largeThreshold, largeTideRangePercent)
			d.TideRangeLegendExceptional = fmt.Sprintf("Exceptional ≥ %.1f ft (+%.0f%%)", exceptionalThreshold, exceptionalTideRangePercent)
		}

		dateURL := func(day time.Time, todayMode bool) string {
			q := cloneQuery(report.RequestQuery)
			q.Set("format", "html")
			if todayMode {
				q.Del("current_date")
			} else {
				q.Set("current_date", day.Format("2006-01-02"))
			}
			if d.CurrentDays == 1 {
				q.Del("current_days")
			} else {
				q.Set("current_days", fmt.Sprintf("%d", d.CurrentDays))
			}
			return "/report?" + q.Encode()
		}

		d.CurrentPrevURL = dateURL(currentDay.AddDate(0, 0, -d.CurrentDays), false)
		d.CurrentNextURL = dateURL(currentDay.AddDate(0, 0, d.CurrentDays), false)
		d.CurrentTodayURL = dateURL(today, true)

		planningCurrentDistanceWarning := parsePlanningCurrentDistanceWarning(report.RequestQuery)
		d.PlanningCurrentDistanceWarning = fmt.Sprintf("%.1f", planningCurrentDistanceWarning)
		d.PlanningAutoCurrentLimit = fmt.Sprintf("%.0f", maxAutoCurrentStationDistanceNM)

		if report.Current.CurrentStation != nil {
			s := report.Current.CurrentStation
			d.CurrentStation = s.Name
			d.CurrentMeta = fmt.Sprintf("%s · bin %s · %s ft depth · %.1f nmi away", s.ID, report.Current.Bin, report.Current.Depth, s.DistanceNM)
			if s.DistanceNM > planningCurrentDistanceWarning {
				d.CurrentDistanceWarning = fmt.Sprintf(
					"Currents station is %.1f nmi from the selected wind station, beyond the %.1f nmi warning threshold.",
					s.DistanceNM,
					planningCurrentDistanceWarning,
				)
			}
		}
		if !report.Current.Start.IsZero() && !report.Current.End.IsZero() {
			d.CurrentWindow = fmt.Sprintf(
				"%s → %s",
				report.Current.Start.In(loc).Format("3:04 PM"),
				report.Current.End.In(loc).Format("3:04 PM"),
			)
			if _, hasStart := report.RequestQuery["start"]; !hasStart {
				if _, hasEnd := report.RequestQuery["end"]; !hasEnd {
					d.CurrentWindowMode = "Daylight window · sunrise to sunset"
				}
			}
		}
		d.CurrentOutlook = append(d.CurrentOutlook, report.Current.Outlook...)
		for _, e := range report.Current.Events {
			x := htmlCurrentEvent{Time: e.Time.In(loc).Format("3:04 PM"), Class: e.Type}
			switch e.Type {
			case "flood":
				x.Label = "Max flood"
				x.Speed = fmt.Sprintf("%.2f kt", e.SpeedKT)
				x.Direction = fmt.Sprintf("%03d°", e.Direction)
			case "ebb":
				x.Label = "Max ebb"
				x.Speed = fmt.Sprintf("%.2f kt", e.SpeedKT)
				x.Direction = fmt.Sprintf("%03d°", e.Direction)
			default:
				x.Label = "Slack"
			}
			d.CurrentEvents = append(d.CurrentEvents, x)
		}
		planningStartMinutes, planningStart := parsePlanningTime(
			report.RequestQuery, "planning_start", "12:00",
		)
		planningEndMinutes, planningEnd := parsePlanningTime(
			report.RequestQuery, "planning_end", "17:00",
		)
		if planningEndMinutes <= planningStartMinutes {
			planningStartMinutes, planningStart = 12*60, "12:00"
			planningEndMinutes, planningEnd = 17*60, "17:00"
		}
		planningCautionEbb := parsePlanningCautionEbb(report.RequestQuery)
		planningCautionFlood := parsePlanningCautionFlood(report.RequestQuery)
		planningMaxEbb := parsePlanningMaxEbb(report.RequestQuery)
		planningMaxFlood := parsePlanningMaxFlood(report.RequestQuery)
		planningBuffer := parsePlanningBuffer(report.RequestQuery)
		d.PlanningStart = planningStart
		d.PlanningEnd = planningEnd
		d.PlanningCautionEbb = fmt.Sprintf("%.1f", planningCautionEbb)
		d.PlanningCautionFlood = fmt.Sprintf("%.1f", planningCautionFlood)
		d.PlanningMaxEbb = fmt.Sprintf("%.1f", planningMaxEbb)
		d.PlanningMaxFlood = fmt.Sprintf("%.1f", planningMaxFlood)
		d.PlanningBuffer = fmt.Sprintf("%d", planningBuffer)

		d.CurrentPlanningHints = buildCurrentPlanningHints(
			report.Current,
			currentDay,
			d.CurrentDays,
			loc,
			planningCautionEbb,
			planningCautionFlood,
			planningMaxEbb,
			planningMaxFlood,
			planningStartMinutes,
			planningEndMinutes,
			planningBuffer,
		)

		distanceCaution := false
		distanceCautionReason := ""
		if report.Current.CurrentStation != nil &&
			report.Current.CurrentStation.DistanceNM > planningCurrentDistanceWarning {
			distanceCaution = true
			distanceCautionReason = fmt.Sprintf(
				"selected currents station is %.1f nmi from the wind station, beyond the %.1f nmi warning threshold",
				report.Current.CurrentStation.DistanceNM,
				planningCurrentDistanceWarning,
			)

			for i := range d.CurrentPlanningHints {
				hint := &d.CurrentPlanningHints[i]
				switch hint.Class {
				case "preferred":
					hint.Class = "caution"
					hint.Status = "Caution"
					hint.Detail = "Current strength is preferred, but the " +
						distanceCautionReason + "."
				case "caution", "redflag":
					hint.Detail = strings.TrimSuffix(hint.Detail, ".") +
						". Also, the " + distanceCautionReason + "."
				}
			}
		}

		preferredCount, cautionCount, redCount := 0, 0, 0
		for _, hint := range d.CurrentPlanningHints {
			switch hint.Class {
			case "redflag":
				redCount++
			case "caution":
				cautionCount++
			default:
				preferredCount++
			}
		}

		switch {
		case redCount > 0:
			d.PlanningPeriodStatus = "Red flag"
			d.PlanningPeriodClass = "redflag"
		case cautionCount > 0 || distanceCaution:
			d.PlanningPeriodStatus = "Caution"
			d.PlanningPeriodClass = "caution"
		default:
			d.PlanningPeriodStatus = "Preferred"
			d.PlanningPeriodClass = "preferred"
		}

		currentCauseClass := d.PlanningPeriodClass
		if redCount > 0 {
			currentCauseClass = "redflag"
		} else if cautionCount > 0 {
			currentCauseClass = "caution"
		} else {
			currentCauseClass = "preferred"
		}
		currentCause := planningPeriodCause(
			d.CurrentPlanningHints,
			currentCauseClass,
			planningCautionEbb,
			planningCautionFlood,
			planningMaxEbb,
			planningMaxFlood,
		)

		switch {
		case currentCause != "" && distanceCautionReason != "":
			d.PlanningPeriodCause = strings.TrimSuffix(currentCause, ".") +
				" and because the " + distanceCautionReason + "."
		case currentCause != "":
			d.PlanningPeriodCause = currentCause
		case distanceCautionReason != "":
			d.PlanningPeriodCause = "Caution because the " + distanceCautionReason + "."
		}

		if d.CurrentDays > 1 {
			d.PlanningPeriodDetail = fmt.Sprintf(
				"Overall planning classification for this %d-day window: %d preferred, %d caution, %d red flag.",
				d.CurrentDays,
				preferredCount,
				cautionCount,
				redCount,
			)
		}
		d.CurrentChart = buildCurrentChartSVG(
			report.Current,
			report.ReportTime,
			loc,
			d.CurrentDays,
			planningStartMinutes,
			planningEndMinutes,
			d.TideRanges,
		)
		d.BottomLineCurrentChart = buildCurrentChartSVG(
			report.Current,
			report.ReportTime,
			loc,
			1,
			planningStartMinutes,
			planningEndMinutes,
			d.TideRanges,
		)
	}
	if d.WindError != "" {
		d.BottomLine = append(
			d.BottomLine,
			"Wind station selection is unavailable for the requested location.",
			"Try nearby coordinates, an explicit NDBC station ID, or enable debug_wind=1 to inspect nearby candidates.",
		)
		d.BottomLineNarrative = append(d.BottomLineNarrative, d.BottomLine...)
		d.FullText = fmt.Sprintf(
			"WIND STATION SELECTION UNAVAILABLE\n--------------------------------\n%s",
			d.WindError,
		)
	} else {
		var b strings.Builder
		writeBottomLineText(&b, report, d.WindUnit)
		for _, line := range strings.Split(strings.TrimSpace(b.String()), "\n") {
			line = strings.TrimSpace(line)
			if line != "" && line != "BOTTOM LINE" && !strings.HasPrefix(line, "---") {
				d.BottomLine = append(d.BottomLine, line)
				if d.BottomLineCurrentChart == "" && !strings.HasPrefix(line, "Latest wind at ") {
					d.BottomLineNarrative = append(d.BottomLineNarrative, line)
				}
			}
		}
		var full strings.Builder
		writeTextReport(&full, report, loc)
		d.FullText = strings.TrimSpace(full.String())
	}

	return d
}

func dayStartForCurrentChart(report *CurrentReport, loc *time.Location) time.Time {
	t := report.Start.In(loc)
	if t.IsZero() && len(report.Series) > 0 {
		t = report.Series[0].Time.In(loc)
	}
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
}

func planningPeriodCause(
	hints []currentPlanningHint,
	periodClass string,
	cautionEbbKT float64,
	cautionFloodKT float64,
	maxEbbKT float64,
	maxFloodKT float64,
) string {
	if periodClass == "" || periodClass == "preferred" {
		return ""
	}

	hasBufferOnly := false
	hasUnavailable := false
	windowMaxEbb := 0.0
	windowMaxFlood := 0.0
	bufferMaxEbb := 0.0
	bufferMaxFlood := 0.0

	for _, hint := range hints {
		if hint.Class != periodClass {
			continue
		}

		detail := strings.ToLower(hint.Detail)
		hasUnavailable = hasUnavailable ||
			strings.Contains(detail, "not enough prediction samples")
		if strings.Contains(detail, "within the ") &&
			strings.Contains(detail, "-minute buffer") &&
			!strings.Contains(detail, " during ") {
			hasBufferOnly = true
		}

		windowMaxEbb = math.Max(windowMaxEbb, hint.WindowMaxEbbKT)
		windowMaxFlood = math.Max(windowMaxFlood, hint.WindowMaxFloodKT)
		bufferMaxEbb = math.Max(bufferMaxEbb, hint.BufferMaxEbbKT)
		bufferMaxFlood = math.Max(bufferMaxFlood, hint.BufferMaxFloodKT)
	}

	switch periodClass {
	case "redflag":
		ebbTriggered := windowMaxEbb >= maxEbbKT
		floodTriggered := windowMaxFlood >= maxFloodKT
		switch {
		case ebbTriggered && floodTriggered:
			return fmt.Sprintf(
				"Red flag due to ebb current reaching %.1f kt and flood current reaching %.1f kt during the preferred sailing period; red-flag thresholds are %.1f kt ebb and %.1f kt flood.",
				windowMaxEbb,
				windowMaxFlood,
				maxEbbKT,
				maxFloodKT,
			)
		case floodTriggered:
			return fmt.Sprintf(
				"Red flag due to flood current reaching %.1f kt during the preferred sailing period; flood red-flag threshold is %.1f kt.",
				windowMaxFlood,
				maxFloodKT,
			)
		case ebbTriggered:
			return fmt.Sprintf(
				"Red flag due to ebb current reaching %.1f kt during the preferred sailing period; ebb red-flag threshold is %.1f kt.",
				windowMaxEbb,
				maxEbbKT,
			)
		}

	case "caution":
		if hasUnavailable {
			return "Caution because current prediction samples are incomplete."
		}

		ebbTriggered := windowMaxEbb >= cautionEbbKT
		floodTriggered := windowMaxFlood >= cautionFloodKT
		switch {
		case ebbTriggered && floodTriggered:
			return fmt.Sprintf(
				"Caution due to ebb current reaching %.1f kt and flood current reaching %.1f kt during the preferred sailing period; caution thresholds are %.1f kt ebb and %.1f kt flood.",
				windowMaxEbb,
				windowMaxFlood,
				cautionEbbKT,
				cautionFloodKT,
			)
		case floodTriggered:
			return fmt.Sprintf(
				"Caution due to flood current reaching %.1f kt during the preferred sailing period; flood caution threshold is %.1f kt.",
				windowMaxFlood,
				cautionFloodKT,
			)
		case ebbTriggered:
			return fmt.Sprintf(
				"Caution due to ebb current reaching %.1f kt during the preferred sailing period; ebb caution threshold is %.1f kt.",
				windowMaxEbb,
				cautionEbbKT,
			)
		}

		bufferEbbTriggered := bufferMaxEbb >= cautionEbbKT
		bufferFloodTriggered := bufferMaxFlood >= cautionFloodKT
		switch {
		case bufferEbbTriggered && bufferFloodTriggered:
			return fmt.Sprintf(
				"Caution due to ebb current reaching %.1f kt and flood current reaching %.1f kt within the planning buffer; caution thresholds are %.1f kt ebb and %.1f kt flood.",
				bufferMaxEbb,
				bufferMaxFlood,
				cautionEbbKT,
				cautionFloodKT,
			)
		case bufferFloodTriggered:
			return fmt.Sprintf(
				"Caution due to flood current reaching %.1f kt within the planning buffer; flood caution threshold is %.1f kt.",
				bufferMaxFlood,
				cautionFloodKT,
			)
		case bufferEbbTriggered:
			return fmt.Sprintf(
				"Caution due to ebb current reaching %.1f kt within the planning buffer; ebb caution threshold is %.1f kt.",
				bufferMaxEbb,
				cautionEbbKT,
			)
		case hasBufferOnly:
			return "Caution due to strong current within the planning buffer."
		}
	}

	return ""
}

func buildCurrentPlanningHints(
	report *CurrentReport,
	startDay time.Time,
	days int,
	loc *time.Location,
	cautionEbbKT float64,
	cautionFloodKT float64,
	maxEbbKT float64,
	maxFloodKT float64,
	planningStartMinutes int,
	planningEndMinutes int,
	planningBufferMinutes int,
) []currentPlanningHint {
	if report == nil || report.CurrentStation == nil || days < 1 {
		return nil
	}
	if cautionEbbKT <= 0 {
		cautionEbbKT = 2.0
	}
	if cautionFloodKT <= 0 {
		cautionFloodKT = 2.0
	}
	if maxEbbKT <= cautionEbbKT {
		maxEbbKT = 3.0
	}
	if maxFloodKT <= cautionFloodKT {
		maxFloodKT = 3.0
	}

	// Fetch the entire displayed range once, then partition by each sample's
	// actual local calendar date. This keeps the planning classifier aligned
	// with the graph/slider even across timezone and DST boundaries.
	rangeStart := time.Date(
		startDay.In(loc).Year(),
		startDay.In(loc).Month(),
		startDay.In(loc).Day(),
		0, 0, 0, 0,
		loc,
	)
	rangeEnd := rangeStart.AddDate(0, 0, days-1)

	dense, _, err := fetchCurrentPredictionsRange(
		report.CurrentStation.ID,
		report.CurrentStation.CurrBin,
		rangeStart.Format("20060102"),
		rangeEnd.Format("20060102"),
	)
	if err != nil {
		return nil
	}
	samples, err := currentSamplesFromPredictions(dense, loc)
	if err != nil || len(samples) < 3 {
		return nil
	}

	byDay := make(map[string][]CurrentSample, days)
	for _, sample := range samples {
		local := sample.Time.In(loc)
		key := local.Format("2006-01-02")
		byDay[key] = append(byDay[key], CurrentSample{
			Time:       local,
			VelocityKT: sample.VelocityKT,
		})
	}

	var hints []currentPlanningHint

	for offset := 0; offset < days; offset++ {
		day := rangeStart.AddDate(0, 0, offset)
		key := day.Format("2006-01-02")
		daySamples := byDay[key]

		if len(daySamples) < 3 {
			hints = append(hints, currentPlanningHint{
				Date:   day.Format("Mon Jan 2"),
				Status: "Unavailable",
				Class:  "caution",
				Detail: "Not enough prediction samples for this local calendar day.",
			})
			continue
		}

		// Detect local ebb maxima strictly within this local calendar day.
		var ebbs []CurrentSample
		for i := 1; i < len(daySamples)-1; i++ {
			prev := daySamples[i-1]
			cur := daySamples[i]
			next := daySamples[i+1]

			if cur.VelocityKT < 0 &&
				cur.VelocityKT <= prev.VelocityKT &&
				cur.VelocityKT <= next.VelocityKT {
				ebbs = append(ebbs, cur)
			}
		}

		sort.Slice(ebbs, func(i, j int) bool {
			return math.Abs(ebbs[i].VelocityKT) >
				math.Abs(ebbs[j].VelocityKT)
		})

		if len(ebbs) == 0 {
			hints = append(hints, currentPlanningHint{
				Date:   day.Format("Mon Jan 2"),
				Status: "Preferred",
				Class:  "preferred",
				Detail: "No ebb maximum found during this local calendar day.",
			})
			continue
		}

		windowStart := time.Date(
			day.Year(), day.Month(), day.Day(),
			planningStartMinutes/60, planningStartMinutes%60, 0, 0,
			loc,
		)
		windowEnd := time.Date(
			day.Year(), day.Month(), day.Day(),
			planningEndMinutes/60, planningEndMinutes%60, 0, 0,
			loc,
		)
		bufferStart := windowStart.Add(-time.Duration(planningBufferMinutes) * time.Minute)
		bufferEnd := windowEnd.Add(time.Duration(planningBufferMinutes) * time.Minute)

		windowLabel := fmt.Sprintf(
			"%s–%s",
			windowStart.Format("3:04 PM"),
			windowEnd.Format("3:04 PM"),
		)

		windowMaxEbb := 0.0
		windowMaxEbbTime := time.Time{}
		windowMaxFlood := 0.0
		windowMaxFloodTime := time.Time{}
		bufferMaxEbb := 0.0
		bufferMaxEbbTime := time.Time{}
		bufferMaxFlood := 0.0
		bufferMaxFloodTime := time.Time{}

		for _, sample := range daySamples {
			local := sample.Time.In(loc)
			if local.Year() != day.Year() || local.YearDay() != day.YearDay() {
				continue
			}
			speed := math.Round(math.Abs(sample.VelocityKT)*10) / 10
			inWindow := !local.Before(windowStart) && !local.After(windowEnd)
			inBuffer := planningBufferMinutes > 0 &&
				!local.Before(bufferStart) && !local.After(bufferEnd) && !inWindow

			if sample.VelocityKT < 0 {
				if inWindow && speed > windowMaxEbb {
					windowMaxEbb, windowMaxEbbTime = speed, local
				} else if inBuffer && speed > bufferMaxEbb {
					bufferMaxEbb, bufferMaxEbbTime = speed, local
				}
			} else if sample.VelocityKT > 0 {
				if inWindow && speed > windowMaxFlood {
					windowMaxFlood, windowMaxFloodTime = speed, local
				} else if inBuffer && speed > bufferMaxFlood {
					bufferMaxFlood, bufferMaxFloodTime = speed, local
				}
			}
		}

		h := currentPlanningHint{
			Date:             day.Format("Mon Jan 2"),
			WindowMaxEbbKT:   windowMaxEbb,
			WindowMaxFloodKT: windowMaxFlood,
			BufferMaxEbbKT:   bufferMaxEbb,
			BufferMaxFloodKT: bufferMaxFlood,
		}
		var redReasons, cautionReasons, bufferReasons []string
		if windowMaxEbb >= maxEbbKT {
			redReasons = append(redReasons, fmt.Sprintf("ebb %.1f kt at %s", windowMaxEbb, windowMaxEbbTime.Format("3:04 PM")))
		} else if windowMaxEbb >= cautionEbbKT {
			cautionReasons = append(cautionReasons, fmt.Sprintf("ebb %.1f kt at %s", windowMaxEbb, windowMaxEbbTime.Format("3:04 PM")))
		}
		if windowMaxFlood >= maxFloodKT {
			redReasons = append(redReasons, fmt.Sprintf("flood %.1f kt at %s", windowMaxFlood, windowMaxFloodTime.Format("3:04 PM")))
		} else if windowMaxFlood >= cautionFloodKT {
			cautionReasons = append(cautionReasons, fmt.Sprintf("flood %.1f kt at %s", windowMaxFlood, windowMaxFloodTime.Format("3:04 PM")))
		}
		if bufferMaxEbb >= cautionEbbKT {
			bufferReasons = append(bufferReasons, fmt.Sprintf("ebb %.1f kt at %s", bufferMaxEbb, bufferMaxEbbTime.Format("3:04 PM")))
		}
		if bufferMaxFlood >= cautionFloodKT {
			bufferReasons = append(bufferReasons, fmt.Sprintf("flood %.1f kt at %s", bufferMaxFlood, bufferMaxFloodTime.Format("3:04 PM")))
		}

		switch {
		case len(redReasons) > 0:
			h.Status, h.Class = "Red flag", "redflag"
			h.Detail = fmt.Sprintf("%s during %s.", strings.Join(redReasons, "; "), windowLabel)
			if len(cautionReasons) > 0 {
				h.Detail += fmt.Sprintf(" Also at caution level: %s.", strings.Join(cautionReasons, "; "))
			}
			if len(bufferReasons) > 0 {
				h.Detail += fmt.Sprintf(" Near the window: %s.", strings.Join(bufferReasons, "; "))
			}
		case len(cautionReasons) > 0:
			h.Status, h.Class = "Caution", "caution"
			h.Detail = fmt.Sprintf("%s during %s.", strings.Join(cautionReasons, "; "), windowLabel)
			if len(bufferReasons) > 0 {
				h.Detail += fmt.Sprintf(" Also near the window: %s.", strings.Join(bufferReasons, "; "))
			}
		case len(bufferReasons) > 0:
			h.Status, h.Class = "Caution", "caution"
			h.Detail = fmt.Sprintf("%s within the %d-minute buffer around %s.",
				strings.Join(bufferReasons, "; "), planningBufferMinutes, windowLabel)
		default:
			h.Status, h.Class = "Preferred", "preferred"
			h.Detail = fmt.Sprintf("During %s, ebb stays below %.1f kt and flood stays below %.1f kt.",
				windowLabel, cautionEbbKT, cautionFloodKT)
		}

		hints = append(hints, h)
	}

	return hints
}

func fetchChartCurrentPredictionsGMT(
	station string,
	bin int,
	beginDate string,
	endDate string,
	interval string,
) ([]CurrentPrediction, error) {
	params := url.Values{}
	params.Set("product", "currents_predictions")
	params.Set("application", "pittsburg-saildata")
	params.Set("begin_date", beginDate)
	params.Set("end_date", endDate)
	params.Set("station", dataGetterStationID(station))
	params.Set("time_zone", "gmt")
	params.Set("units", "english")
	params.Set("interval", interval)
	params.Set("format", "json")
	if bin > 0 {
		params.Set("bin", fmt.Sprintf("%d", bin))
	}

	req, err := http.NewRequest(
		http.MethodGet,
		currentDataURL+"?"+params.Encode(),
		nil,
	)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "pittsburg-saildata/"+appVersion)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"NOAA chart current API returned HTTP %d",
			resp.StatusCode,
		)
	}

	var data currentAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	if data.Error != nil {
		return nil, fmt.Errorf(
			"NOAA chart current error: %s",
			strings.TrimSpace(data.Error.Message),
		)
	}
	if len(data.CurrentPredictions.CP) == 0 {
		return nil, fmt.Errorf("NOAA returned no chart current predictions")
	}
	return data.CurrentPredictions.CP, nil
}

func chartTimedPredictionsFromGMT(
	predictions []CurrentPrediction,
	loc *time.Location,
) ([]TimedPrediction, error) {
	result := make([]TimedPrediction, 0, len(predictions))
	for _, p := range predictions {
		t, err := time.ParseInLocation(
			noaaCurrentTimeFormat,
			p.Time,
			time.UTC,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"unable to parse NOAA GMT current time %q: %w",
				p.Time,
				err,
			)
		}
		result = append(result, TimedPrediction{
			Prediction: p,
			Time:       t.In(loc),
		})
	}
	return result, nil
}

func chartSamplesFromGMT(
	predictions []CurrentPrediction,
	loc *time.Location,
	start time.Time,
	end time.Time,
) ([]CurrentSample, error) {
	timed, err := chartTimedPredictionsFromGMT(predictions, loc)
	if err != nil {
		return nil, err
	}
	samples := make([]CurrentSample, 0, len(timed))
	for _, item := range timed {
		if item.Time.Before(start) || !item.Time.Before(end) {
			continue
		}
		samples = append(samples, CurrentSample{
			Time:       item.Time,
			VelocityKT: item.Prediction.Velocity,
		})
	}
	sort.Slice(samples, func(i, j int) bool {
		return samples[i].Time.Before(samples[j].Time)
	})
	return samples, nil
}

func tideRangeMedian(ranges []tideRangeDay) float64 {
	values := make([]float64, 0, len(ranges))
	for _, day := range ranges {
		if day.Range > 0 {
			values = append(values, day.Range)
		}
	}
	if len(values) == 0 {
		return 0
	}
	sort.Float64s(values)
	mid := len(values) / 2
	if len(values)%2 == 0 {
		return (values[mid-1] + values[mid]) / 2
	}
	return values[mid]
}

func tideRangeClass(value, median float64) (string, string) {
	if value <= 0 || median <= 0 {
		return "typical", "Normal-cycle"
	}
	percentAbove := (value/median - 1) * 100
	switch {
	case percentAbove >= exceptionalTideRangePercent:
		return "exceptional", "Exceptional"
	case percentAbove >= largeTideRangePercent:
		return "large", "Large"
	case percentAbove >= elevatedTideRangePercent:
		return "elevated", "Elevated"
	default:
		return "typical", "Normal-cycle"
	}
}

func buildCurrentChartSVG(
	report *CurrentReport,
	reportTime time.Time,
	loc *time.Location,
	days int,
	planningStartMinutes int,
	planningEndMinutes int,
	tideRanges []tideRangeDay,
) template.HTML {
	if report == nil || len(report.Series) < 2 {
		return ""
	}
	if days != 3 && days != 7 {
		days = 1
	}

	dayStart := dayStartForCurrentChart(report, loc)
	dayEnd := dayStart.AddDate(0, 0, days)

	// NOAA's lst_ldt timestamps are ambiguous during the repeated hour when
	// daylight saving time ends. Fetch chart data in GMT, then convert each
	// unambiguous instant to local time. This keeps the curve continuous
	// across DST transitions.
	series := append([]CurrentSample(nil), report.Series...)
	if report.CurrentStation != nil {
		utcBegin := dayStart.UTC().Format("20060102")
		utcEnd := dayEnd.UTC().Format("20060102")
		if predictions, err := fetchChartCurrentPredictionsGMT(
			report.CurrentStation.ID,
			report.CurrentStation.CurrBin,
			utcBegin,
			utcEnd,
			"6",
		); err == nil {
			if samples, parseErr := chartSamplesFromGMT(
				predictions,
				loc,
				dayStart,
				dayEnd,
			); parseErr == nil && len(samples) >= 2 {
				series = samples
			}
		}
	}
	if len(series) < 2 {
		return ""
	}

	// Max flood, max ebb, and slack events are useful landmarks even in
	// multi-day planning views. Fetch the NOAA max/slack series across the
	// whole displayed range so every day can carry compact time labels.
	chartEvents := append([]CurrentEvent(nil), report.Events...)
	if report.CurrentStation != nil {
		utcBegin := dayStart.UTC().Format("20060102")
		utcEnd := dayEnd.UTC().Format("20060102")
		if predictions, err := fetchChartCurrentPredictionsGMT(
			report.CurrentStation.ID,
			report.CurrentStation.CurrBin,
			utcBegin,
			utcEnd,
			"max_slack",
		); err == nil {
			if timed, parseErr := chartTimedPredictionsFromGMT(
				predictions,
				loc,
			); parseErr == nil {
				filtered := make([]TimedPrediction, 0, len(timed))
				for _, item := range timed {
					if item.Time.Before(dayStart) || !item.Time.Before(dayEnd) {
						continue
					}
					filtered = append(filtered, item)
				}
				chartEvents = currentEventsFromTimed(filtered)
			}
		}
	}

	const (
		width  = 860.0
		height = 330.0
		left   = 54.0
		right  = 66.0
		top    = 18.0
		bottom = 42.0
	)

	plotW := width - left - right
	plotH := height - top - bottom

	tideMedian := tideRangeMedian(tideRanges)
	tideByDate := make(map[string]float64, len(tideRanges))
	maxTideRange := 0.0
	for _, item := range tideRanges {
		tideByDate[item.Date] = item.Range
		day, err := time.ParseInLocation("2006-01-02", item.Date, loc)
		if err == nil && !day.Before(dayStart) && day.Before(dayEnd) && item.Range > maxTideRange {
			maxTideRange = item.Range
		}
	}
	if maxTideRange > 0 {
		// Keep the tidal-range axis stable for date-to-date comparison.
		// Use 0–10 ft normally and expand only when a displayed range
		// actually exceeds 10 ft.
		maxTideRange = math.Max(10.0, math.Ceil(maxTideRange))
	}

	// Keep the current-speed scale stable while stepping through dates so
	// vertical height remains visually comparable from one range to the next.
	// Expand beyond +/-3.5 kt only when the displayed predictions require it.
	maxAbs := 3.5
	for _, sample := range series {
		if v := math.Abs(sample.VelocityKT); v > maxAbs {
			maxAbs = v
		}
	}
	maxAbs = math.Ceil(maxAbs*2.0) / 2.0

	xFor := func(t time.Time) float64 {
		f := t.Sub(dayStart).Seconds() / dayEnd.Sub(dayStart).Seconds()
		if f < 0 {
			f = 0
		}
		if f > 1 {
			f = 1
		}
		return left + f*plotW
	}
	yFor := func(v float64) float64 {
		return top + (maxAbs-v)/(2*maxAbs)*plotH
	}
	tideYFor := func(v float64) float64 {
		if maxTideRange <= 0 {
			return top + plotH
		}
		f := v / maxTideRange
		if f < 0 {
			f = 0
		}
		if f > 1 {
			f = 1
		}
		return top + (1-f)*plotH
	}
	zeroY := yFor(0)

	var path strings.Builder
	for i, sample := range series {
		x := xFor(sample.Time.In(loc))
		y := yFor(sample.VelocityKT)
		if i == 0 {
			fmt.Fprintf(&path, "M %.2f %.2f", x, y)
		} else {
			fmt.Fprintf(&path, " L %.2f %.2f", x, y)
		}
	}

	firstX := xFor(series[0].Time.In(loc))
	lastX := xFor(series[len(series)-1].Time.In(loc))
	areaPath := fmt.Sprintf(
		"M %.2f %.2f L %.2f %.2f %s L %.2f %.2f Z",
		firstX, zeroY,
		firstX, yFor(series[0].VelocityKT),
		strings.TrimPrefix(path.String(), fmt.Sprintf("M %.2f %.2f", firstX, yFor(series[0].VelocityKT))),
		lastX, zeroY,
	)

	var svg strings.Builder
	fmt.Fprintf(&svg, `<svg class="current-chart-svg" viewBox="0 0 %.0f %.0f" role="img" aria-label="Predicted tidal current speed and direction across the selected date range">`, width, height)
	fmt.Fprintf(&svg, `<defs><clipPath id="floodClip"><rect x="%.2f" y="%.2f" width="%.2f" height="%.2f"/></clipPath><clipPath id="ebbClip"><rect x="%.2f" y="%.2f" width="%.2f" height="%.2f"/></clipPath></defs>`,
		left, top, plotW, zeroY-top,
		left, zeroY, plotW, top+plotH-zeroY,
	)

	// Make night visually distinct, then cut light daylight windows into it.
	// This is intentionally stronger than the old white/light-blue treatment.
	fmt.Fprintf(&svg, `<rect class="night-window" x="%.2f" y="%.2f" width="%.2f" height="%.2f"/>`,
		left, top, plotW, plotH)

	// Shade daylight separately for every displayed day.
	if report.WindReference != nil {
		for offset := 0; offset < days; offset++ {
			day := dayStart.AddDate(0, 0, offset)
			rise, set, err := daylightWindow(
				day,
				report.WindReference.Lat,
				report.WindReference.Lon,
				loc,
			)
			if err == nil {
				x1 := xFor(rise)
				x2 := xFor(set)
				fmt.Fprintf(&svg, `<rect class="sail-window" x="%.2f" y="%.2f" width="%.2f" height="%.2f"/>`,
					x1, top, x2-x1, plotH)
			}
		}
	}

	// Highlight the preferred planning period, noon through 5 PM local time.
	// This is layered over the daylight/night background so the planning
	// window can be scanned without using the event browser.
	for offset := 0; offset < days; offset++ {
		day := dayStart.AddDate(0, 0, offset)
		preferredStart := time.Date(
			day.Year(), day.Month(), day.Day(),
			planningStartMinutes/60, planningStartMinutes%60, 0, 0, loc,
		)
		preferredEnd := time.Date(
			day.Year(), day.Month(), day.Day(),
			planningEndMinutes/60, planningEndMinutes%60, 0, 0, loc,
		)
		x1 := xFor(preferredStart)
		x2 := xFor(preferredEnd)
		fmt.Fprintf(&svg, `<rect class="preferred-window" x="%.2f" y="%.2f" width="%.2f" height="%.2f"/>`,
			x1, top, x2-x1, plotH)
	}

	// Horizontal grid and y labels.
	for v := -maxAbs; v <= maxAbs+0.001; v += 0.5 {
		y := yFor(v)
		className := "grid-line"
		if math.Abs(v) < 0.001 {
			className = "zero-line"
		}
		fmt.Fprintf(&svg, `<line class="%s" x1="%.2f" y1="%.2f" x2="%.2f" y2="%.2f"/>`,
			className, left, y, left+plotW, y)
		if math.Abs(v*2-math.Round(v*2)) < 0.001 {
			fmt.Fprintf(&svg, `<text class="axis-label y-label" x="%.2f" y="%.2f">%.1f</text>`,
				left-10, y+4, v)
		}
	}

	// Time/day grid. One-day view keeps 3-hour labels; multi-day views
	// emphasize day boundaries and use noon as a light orientation marker.
	if days == 1 {
		for hour := 0; hour <= 24; hour += 3 {
			t := dayStart.Add(time.Duration(hour) * time.Hour)
			x := xFor(t)
			fmt.Fprintf(&svg, `<line class="v-grid-line" x1="%.2f" y1="%.2f" x2="%.2f" y2="%.2f"/>`,
				x, top, x, top+plotH)
			if hour < 24 {
				label := t.Format("3 PM")
				if hour == 0 {
					label = "12 AM"
				}
				if hour == 12 {
					label = "Noon"
				}
				fmt.Fprintf(&svg, `<text class="axis-label x-label" x="%.2f" y="%.2f">%s</text>`,
					x, height-13, label)
			}
		}
	} else {
		for offset := 0; offset <= days; offset++ {
			t := dayStart.AddDate(0, 0, offset)
			x := xFor(t)
			fmt.Fprintf(&svg, `<line class="day-grid-line" x1="%.2f" y1="%.2f" x2="%.2f" y2="%.2f"/>`,
				x, top, x, top+plotH)
			if offset < days {
				mid := t.Add(12 * time.Hour)
				midX := xFor(mid)
				fmt.Fprintf(&svg, `<line class="v-grid-line" x1="%.2f" y1="%.2f" x2="%.2f" y2="%.2f"/>`,
					midX, top, midX, top+plotH)
				fmt.Fprintf(&svg, `<text class="axis-label x-label" x="%.2f" y="%.2f">%s</text>`,
					xFor(t.Add(6*time.Hour)), height-13, t.Format("Mon Jan 2"))
			}
		}
	}

	// Filled areas from the same NOAA 6-minute curve.
	fmt.Fprintf(&svg, `<path class="flood-area" d="%s" clip-path="url(#floodClip)"/>`, areaPath)
	fmt.Fprintf(&svg, `<path class="ebb-area" d="%s" clip-path="url(#ebbClip)"/>`, areaPath)
	fmt.Fprintf(&svg, `<path class="current-line" d="%s"/>`, path.String())

	// Draw the tidal-range overlay after the current fills and curve so its
	// category color remains fully visible over both daylight and night.
	if maxTideRange > 0 {
		svg.WriteString(`<g class="tide-range-layer" aria-label="Daily predicted tidal range">`)
		for offset := 0; offset < days; offset++ {
			day := dayStart.AddDate(0, 0, offset)
			value, ok := tideByDate[day.Format("2006-01-02")]
			if !ok || value <= 0 {
				continue
			}
			className, label := tideRangeClass(value, tideMedian)
			x := xFor(day.Add(12 * time.Hour))
			y := tideYFor(value)
			baseY := top + plotH
			labelX := x
			labelY := baseY - 10
			valueClass := "tide-range-value " + className
			capHalfWidth := 5.0

			// In the one-day Conditions Now/current view, the single tidal-range
			// marker belongs outside the graph, to the right of the 0-10 tidal
			// range numbers, while still staying left of the vertical axis title.
			if days == 1 {
				x = left + plotW + 28
				labelX = x - 7
				labelY = y + 4
				valueClass += " axis-adjacent"
			}

			fmt.Fprintf(&svg, `<g class="tide-range-marker %s"><title>%s tidal range · %.1f ft · %s</title><line class="tide-range-bar %s" x1="%.2f" y1="%.2f" x2="%.2f" y2="%.2f"/><line class="tide-range-bar %s" x1="%.2f" y1="%.2f" x2="%.2f" y2="%.2f"/></g>`,
				className, template.HTMLEscapeString(day.Format("Mon Jan 2")), value, label,
				className, x, baseY, x, y,
				className, x-capHalfWidth, y, x+capHalfWidth, y)
			fmt.Fprintf(&svg, `<text class="%s" x="%.2f" y="%.2f">%.1f</text>`,
				valueClass, labelX, labelY, value)
		}
		for v := 0.0; v <= maxTideRange+0.001; v += 1.0 {
			y := tideYFor(v)
			fmt.Fprintf(&svg, `<text class="axis-label tide-y-label" x="%.2f" y="%.2f">%.0f</text>`,
				left+plotW+10, y+4, v)
		}
		fmt.Fprintf(&svg, `<text class="axis-title tide-axis-title" x="%.2f" y="%.2f" transform="rotate(90 %.2f %.2f)">Tidal range (ft)</text>`,
			width-13, top+plotH/2, width-13, top+plotH/2)
		svg.WriteString(`</g>`)
	}

	// Mark max flood, max ebb, and slack throughout the displayed range.
	// Multi-day labels are deliberately compact: the curve/zero line tells
	// the event type while the text supplies the planning-critical time.
	slackIndex := 0
	for _, event := range chartEvents {
		eventTime := event.Time.In(loc)
		if eventTime.Before(dayStart) || !eventTime.Before(dayEnd) {
			continue
		}
		x := xFor(eventTime)
		y := zeroY
		labelY := zeroY - 8
		if event.Type == "flood" {
			y = yFor(event.SpeedKT)
			labelY = y - 8
		} else if event.Type == "ebb" {
			y = yFor(-event.SpeedKT)
			labelY = y + 15
		} else {
			// Alternate slack labels above/below zero to reduce collisions.
			if slackIndex%2 == 1 {
				labelY = zeroY + 15
			}
			slackIndex++
		}
		eventLabel := "Slack water"
		if event.Type == "flood" {
			eventLabel = fmt.Sprintf("Max flood · %.1f kt", event.SpeedKT)
		} else if event.Type == "ebb" {
			eventLabel = fmt.Sprintf("Max ebb · %.1f kt", event.SpeedKT)
		}
		fmt.Fprintf(&svg, `<circle class="event-point %s" cx="%.2f" cy="%.2f" r="3.5" data-event-time="%s" data-event-label="%s" data-event-date="%s" data-event-hour="%.4f" data-event-type="%s" data-event-speed="%.3f"/>`,
			event.Type, x, y,
			template.HTMLEscapeString(eventTime.Format("Mon Jan 2 · 3:04 PM")),
			template.HTMLEscapeString(eventLabel),
			eventTime.Format("2006-01-02"),
			float64(eventTime.Hour())+float64(eventTime.Minute())/60.0,
			event.Type,
			event.SpeedKT)
		if days == 1 {
			timeLabel := strings.ToLower(eventTime.Format("3:04PM"))
			timeLabel = strings.TrimSuffix(timeLabel, "m")
			fmt.Fprintf(&svg, `<text class="event-time %s" x="%.2f" y="%.2f">%s</text>`,
				event.Type, x, labelY, timeLabel)
		}
	}

	// "Now" marker represents the actual current time, not a historical
	// report/request timestamp. Omit it entirely when now is outside the
	// plotted range so date navigation cannot pin a misleading red line
	// to the left or right edge.
	nowLocal := time.Now().In(loc)
	if !nowLocal.Before(dayStart) && nowLocal.Before(dayEnd) {
		x := xFor(nowLocal)
		fmt.Fprintf(&svg, `<line class="now-line" x1="%.2f" y1="%.2f" x2="%.2f" y2="%.2f"/>`,
			x, top, x, top+plotH)
		fmt.Fprintf(&svg, `<text class="now-label" x="%.2f" y="%.2f">NOW</text>`,
			x+5, top+14)
	}

	fmt.Fprintf(&svg, `<text class="axis-title" x="15" y="%.2f" transform="rotate(-90 15 %.2f)">Current speed (kt)</text>`,
		top+plotH/2, top+plotH/2)
	svg.WriteString(`</svg>`)

	return template.HTML(svg.String())
}









func queryInt(r *http.Request, key string, fallback int) int {
	value := strings.TrimSpace(r.URL.Query().Get(key))
	if value == "" {
		return fallback
	}

	var parsed int
	if _, err := fmt.Sscanf(value, "%d", &parsed); err != nil {
		return fallback
	}
	return parsed
}

func queryBool(r *http.Request, key string) bool {
	value := strings.ToLower(strings.TrimSpace(r.URL.Query().Get(key)))
	switch value {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func wantsJSON(r *http.Request) bool {
	return strings.Contains(
		strings.ToLower(r.Header.Get("Accept")),
		"application/json",
	)
}

func writeJSONReport(w http.ResponseWriter, report *SailingReport) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(report); err != nil {
		fmt.Println("JSON encoding error:", err)
	}
}

func writeCompactJSONReport(
	w http.ResponseWriter,
	report *SailingReport,
) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(buildCompactReport(report)); err != nil {
		fmt.Println("compact JSON encoding error:", err)
	}
}

func buildCompactReport(report *SailingReport) *CompactReport {
	result := &CompactReport{
		Station:    report.Station,
		ReportTime: report.ReportTime,
		BottomLine: bottomLineLines(report, parseWindUnit(report.RequestQuery)),
	}

	if stationMeta, err := fetchNDBCStation(report.Station); err == nil {
		result.Location = strings.TrimSpace(stationMeta.Name)
	}

	if report.Latest != nil {
		result.Wind = &CompactWind{
			Time:      report.Latest.Time,
			Direction: report.Latest.Direction,
			WindKT:    report.Latest.WindKT,
			GustKT:    report.Latest.GustKT,
		}
	}

	if report.Current != nil {
		c := &CompactCurrent{
			WindowStart:  report.Current.Start,
			WindowEnd:    report.Current.End,
			PhaseAtStart: currentPhaseAtStart(report.Current),
			Error:        report.Current.Error,
		}

		if report.Current.CurrentStation != nil {
			c.StationID = report.Current.CurrentStation.ID
			c.StationName = report.Current.CurrentStation.Name
			c.DistanceNM = report.Current.CurrentStation.DistanceNM
		}

		for i, event := range report.Current.Events {
			if event.Type != "slack" ||
				event.Time.Before(report.Current.Start) ||
				event.Time.After(report.Current.End) {
				continue
			}

			c.SlackTime = event.Time

			for j := i + 1; j < len(report.Current.Events); j++ {
				next := report.Current.Events[j]
				if next.Type == "flood" || next.Type == "ebb" {
					c.NextPhase = next.Type
					c.NextSpeedKT = next.SpeedKT
					c.Strength = currentStrength(next.SpeedKT)
					break
				}
			}
			break
		}

		result.Current = c
	}

	return result
}

func writeVoiceBottomLine(
	w http.ResponseWriter,
	report *SailingReport,
	loc *time.Location,
) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")

	// Reuse the same HTML report-data path that drives the visible Bottom Line
	// card. Voice emits the complete planning cause sentence, not a redundant
	// standalone status line.
	d := makeHTMLReportData(report, loc)
	cause := strings.TrimSpace(d.PlanningPeriodCause)

	if cause == "" && len(d.CurrentPlanningHints) > 0 {
		worstClass := "preferred"
		for _, hint := range d.CurrentPlanningHints {
			switch hint.Class {
			case "redflag":
				worstClass = "redflag"
			case "caution":
				if worstClass != "redflag" {
					worstClass = "caution"
				}
			}
		}

		cause = planningPeriodCause(
			d.CurrentPlanningHints,
			worstClass,
			parsePlanningCautionEbb(report.RequestQuery),
			parsePlanningCautionFlood(report.RequestQuery),
			parsePlanningMaxEbb(report.RequestQuery),
			parsePlanningMaxFlood(report.RequestQuery),
		)
	}

	if cause != "" {
		fmt.Fprintln(w, cause)
	}
	for _, line := range d.BottomLine {
		line = strings.TrimSpace(line)
		if line != "" {
			fmt.Fprintln(w, line)
		}
	}
}

func writeCompactTextReport(
	w io.Writer,
	report *SailingReport,
	loc *time.Location,
) {
	headingName := report.Station
	if stationMeta, err := fetchNDBCStation(report.Station); err == nil {
		if name := strings.TrimSpace(stationMeta.Name); name != "" {
			headingName = name
		}
	}

	fmt.Fprintf(w, "WIND & CURRENT CONDITIONS — %s (%s)\n", headingName, report.Station)
	fmt.Fprintln(w, "================================")
	fmt.Fprintf(
		w,
		"Report time: %s\n",
		report.ReportTime.In(loc).Format("Mon Jan 2, 2006 3:04:05 PM MST"),
	)
	writeWindSelectionText(w, report.WindSelection)
	if report.DebugWindSelection {
		writeWindCandidateDiagnostics(w, report.WindSelection)
	}
	fmt.Fprintln(w)

	fmt.Fprintln(w, "BOTTOM LINE")
	fmt.Fprintln(w, "--------------------------------")
	writeBottomLineText(w, report, parseWindUnit(report.RequestQuery))

	fmt.Fprintln(w)
	fmt.Fprintln(w, "WIND")
	fmt.Fprintln(w, "--------------------------------")
	writeWindSummaryDisplay(w, report, loc, parseWindUnit(report.RequestQuery))

	fmt.Fprintln(w)
	fmt.Fprintln(w, "CURRENT")
	fmt.Fprintln(w, "--------------------------------")
	writeCompactCurrentText(w, report.Current)
}

func writeCompactCurrentText(w io.Writer, report *CurrentReport) {
	if report == nil {
		fmt.Fprintln(w, "Current prediction unavailable.")
		return
	}
	if report.Error != "" {
		fmt.Fprintf(w, "Current prediction unavailable: %s\n", report.Error)
		return
	}

	if report.CurrentStation != nil {
		fmt.Fprintf(
			w,
			"Using %s — %s, %.1f nmi from %s.\n",
			report.CurrentStation.ID,
			report.CurrentStation.Name,
			report.CurrentStation.DistanceNM,
			report.WindReference.ID,
		)
	}

	for _, line := range report.Outlook {
		fmt.Fprintln(w, line)
	}
}

func bottomLineLines(report *SailingReport, windUnit string) []string {
	var b strings.Builder
	writeBottomLineText(&b, report, windUnit)

	raw := strings.Split(strings.TrimSpace(b.String()), "\n")
	lines := make([]string, 0, len(raw))
	for _, line := range raw {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func currentPhaseAtStart(report *CurrentReport) string {
	if report == nil {
		return ""
	}

	for _, line := range report.Outlook {
		switch {
		case strings.Contains(line, "starts on a flood"):
			return "flood"
		case strings.Contains(line, "starts on an ebb"):
			return "ebb"
		case strings.Contains(line, "begins close to slack"):
			return "slack"
		}
	}
	return ""
}

func writeTextReport(
	w io.Writer,
	report *SailingReport,
	loc *time.Location,
) {
	if report.Historical != nil {
		var historicalWind strings.Builder
		writeHistoricalWindText(&historicalWind, report, loc)
		io.WriteString(
			w,
			convertWindTextUnits(
				historicalWind.String(),
				parseWindUnit(report.RequestQuery),
			),
		)

		fmt.Fprintln(w)
		fmt.Fprintln(w, "CURRENT")
		fmt.Fprintln(w, "--------------------------------")
		writeCurrentText(w, report.Current)

		return
	}

	headingName := report.Station

	if stationMeta, err := fetchNDBCStation(report.Station); err == nil {
		name := strings.TrimSpace(stationMeta.Name)
		if name != "" {
			headingName = name
		}
	}

	fmt.Fprintf(
		w,
		"WIND & CURRENT CONDITIONS — %s (%s)\n",
		headingName,
		report.Station,
	)
	fmt.Fprintln(w, "================================")
	fmt.Fprintf(
		w,
		"Report time: %s\n",
		report.ReportTime.In(loc).Format("Mon Jan 2, 2006 3:04:05 PM MST"),
	)
	writeWindSelectionText(w, report.WindSelection)
	if report.DebugWindSelection {
		writeWindCandidateDiagnostics(w, report.WindSelection)
	}
	fmt.Fprintln(w)

	fmt.Fprintln(w, "BOTTOM LINE")
	fmt.Fprintln(w, "--------------------------------")
	writeBottomLineText(w, report, parseWindUnit(report.RequestQuery))

	fmt.Fprintln(w)
	fmt.Fprintln(w, "WIND")
	fmt.Fprintln(w, "--------------------------------")
	writeWindSummaryDisplay(w, report, loc, parseWindUnit(report.RequestQuery))

	fmt.Fprintln(w)
	fmt.Fprintln(w, "CURRENT")
	fmt.Fprintln(w, "--------------------------------")
	writeCurrentText(w, report.Current)

	fmt.Fprintln(w)
	fmt.Fprintln(w, "WIND DETAILS")
	fmt.Fprintln(w, "--------------------------------")
	writeSharedWindDetailsText(w, report, loc)
}

func writeSharedWindDetailsText(
	w io.Writer,
	report *SailingReport,
	loc *time.Location,
) {
	if report.Latest != nil {
		fmt.Fprintf(w, "LATEST %s OBSERVATION\n", report.Station)
		fmt.Fprintln(w, "--------------------------------")
		printWindObservationDisplay(w, report.Latest, loc, report.ReportTime, parseWindUnit(report.RequestQuery))
	}

	if len(report.Latest10) > 0 {
		fmt.Fprintln(w)
		windHours := parseWindReadingHours(report.RequestQuery)
		fmt.Fprintf(w, "WIND OBSERVATIONS — LAST %d HOUR", windHours)
		if windHours != 1 {
			fmt.Fprint(w, "S")
		}
		fmt.Fprintln(w)
		fmt.Fprintln(w, "--------------------------------")
		fmt.Fprintf(w, "%-9s %-3s %6s  %s\n", "Time", "Dir", "Wind", "Gust")

		for _, o := range report.Latest10 {
			fmt.Fprintf(
				w,
				"%-9s %-3s %8s  %8s\n",
				o.Time.In(loc).Format("3:04 PM"),
				o.Direction,
				formatWindSpeed(
					o.WindKT,
					1,
					parseWindUnit(report.RequestQuery),
				),
				formatWindSpeed(
					o.GustKT,
					1,
					parseWindUnit(report.RequestQuery),
				),
			)
		}
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "LAST 12 HOURS")
	fmt.Fprintln(w, "--------------------------------")
	printWindStatsDisplay(w, report.Last12Hours, loc, parseWindUnit(report.RequestQuery))

	for _, period := range report.Afternoon {
		fmt.Fprintln(w)
		fmt.Fprintf(
			w,
			"%s — %s — 12 PM–5 PM\n",
			strings.ToUpper(period.Label),
			period.Date.In(loc).Format("Mon Jan 2, 2006"),
		)
		fmt.Fprintln(w, "--------------------------------")
		printWindStatsDisplay(w, period.Stats, loc, parseWindUnit(report.RequestQuery))
	}
}

func writeBottomLineText(
	w io.Writer,
	report *SailingReport,
	windUnit string,
) {
	if report == nil || report.Latest == nil {
		fmt.Fprintln(w, "Insufficient data for a combined conditions summary.")
		return
	}

	latest := report.Latest
	airTempText := ""
	if report.Historical == nil {
		if airTempF, ok := fetchNDBCAirTemperatureF(report.Station); ok {
			airTempText = fmt.Sprintf(", air temperature %.0f°F", airTempF)
		}
	}

	if latest.GustKT > 0 {
		fmt.Fprintf(
			w,
			"Latest wind at %s: %s %s, gusting %s%s.\n",
			latest.Time.Format("3:04 PM"),
			latest.Direction,
			formatWindSpeed(latest.WindKT, 0, windUnit),
			formatWindSpeed(latest.GustKT, 0, windUnit),
			airTempText,
		)
	} else {
		fmt.Fprintf(
			w,
			"Latest wind at %s: %s %s%s.\n",
			latest.Time.Format("3:04 PM"),
			latest.Direction,
			formatWindSpeed(latest.WindKT, 0, windUnit),
			airTempText,
		)
	}

	if report.Current == nil || report.Current.Error != "" {
		fmt.Fprintln(w, "Current prediction is unavailable.")
		return
	}

	currentDay := report.Current.Start
	reportDay := report.ReportTime.In(currentDay.Location())
	if !currentDay.IsZero() &&
		(currentDay.Year() != reportDay.Year() ||
			currentDay.YearDay() != reportDay.YearDay()) {
		fmt.Fprintf(
			w,
			"Current prediction is shown for %s; see the current section for that day's cycle.\n",
			currentDay.Format("Mon Jan 2"),
		)
		for i := len(report.Current.Outlook) - 1; i >= 0; i-- {
			line := report.Current.Outlook[i]
			if strings.HasPrefix(line, "Peak predicted current") ||
				strings.HasPrefix(line, "No maximum-current") {
				fmt.Fprintln(w, line)
				break
			}
		}
		return
	}

	// Daylight is the context window; Bottom Line is oriented to report time.
	// For historical reports ReportTime is the requested historical time.
	reference := report.ReportTime
	if reference.IsZero() {
		reference = latest.Time
	}

	phaseTime := reference
	if phaseTime.Before(report.Current.Start) {
		phaseTime = report.Current.Start
	}
	if phaseTime.After(report.Current.End) {
		phaseTime = report.Current.End
	}

	if phase := predictedCurrentPhaseAt(report.Current, phaseTime); phase != "" {
		switch {
		case phaseTime.Equal(reference):
			fmt.Fprintf(w, "At %s, current is predicted to be %s.\n",
				phaseTime.Format("3:04 PM"), phase)
		case reference.Before(report.Current.Start):
			fmt.Fprintf(w, "At sunrise (%s), current is predicted to be %s.\n",
				phaseTime.Format("3:04 PM"), phase)
		default:
			fmt.Fprintf(w, "At sunset (%s), current is predicted to be %s.\n",
				phaseTime.Format("3:04 PM"), phase)
		}
	}

	// Report the next slack from now/requested time, not the first one after sunrise.
	eventFloor := reference
	if eventFloor.Before(report.Current.Start) {
		eventFloor = report.Current.Start
	}

	for i, event := range report.Current.Events {
		if event.Type != "slack" ||
			event.Time.Before(eventFloor) ||
			event.Time.After(report.Current.End) {
			continue
		}

		var next *CurrentEvent
		for j := i + 1; j < len(report.Current.Events); j++ {
			if report.Current.Events[j].Time.After(report.Current.End) {
				break
			}
			if report.Current.Events[j].Type == "flood" ||
				report.Current.Events[j].Type == "ebb" {
				copy := report.Current.Events[j]
				next = &copy
				break
			}
		}

		if next != nil {
			fmt.Fprintf(w,
				"Next slack is around %s, then the current turns to a %s, peaking around %s at %.2f kt",
				event.Time.Format("3:04 PM"), next.Type,
				next.Time.Format("3:04 PM"), next.SpeedKT)

			if comparison := findCurrentComparison(report.Current, *next); comparison != nil {
				if comparison.TodayComparison != "" && comparison.OtherTodaySpeedKT > 0 {
					fmt.Fprintf(w, ", %s (other %s max: %.2f kt)",
						comparison.TodayComparison, next.Type, comparison.OtherTodaySpeedKT)
				}
				if comparison.Prior7DayComparison != "" {
					fmt.Fprintf(w, "; %s", comparison.Prior7DayComparison)
				}
			}
			fmt.Fprintln(w, ".")
		} else {
			fmt.Fprintf(w, "Next slack is around %s.\n", event.Time.Format("3:04 PM"))
		}
		break
	}

	// Retain the full daylight-window peak as broader context.
	for i := len(report.Current.Outlook) - 1; i >= 0; i-- {
		line := report.Current.Outlook[i]
		if strings.HasPrefix(line, "Peak predicted current") ||
			strings.HasPrefix(line, "No maximum-current") {
			fmt.Fprintln(w, line)
			break
		}
	}
}

func predictedCurrentPhaseAt(current *CurrentReport, at time.Time) string {
	if current == nil {
		return ""
	}

	if len(current.Series) > 0 {
		best := current.Series[0]
		bestDelta := absDuration(best.Time.Sub(at))
		for _, sample := range current.Series[1:] {
			delta := absDuration(sample.Time.Sub(at))
			if delta < bestDelta {
				best = sample
				bestDelta = delta
			}
		}

		const slackThresholdKT = 0.05
		switch {
		case best.VelocityKT > slackThresholdKT:
			return "flooding"
		case best.VelocityKT < -slackThresholdKT:
			return "ebbing"
		default:
			return "near slack"
		}
	}

	var previous, next *CurrentEvent
	for i := range current.Events {
		event := current.Events[i]
		if !event.Time.After(at) {
			copy := event
			previous = &copy
			continue
		}
		copy := event
		next = &copy
		break
	}

	if previous != nil {
		switch previous.Type {
		case "flood":
			return "flooding"
		case "ebb":
			return "ebbing"
		case "slack":
			if next != nil {
				if next.Type == "flood" {
					return "flooding"
				}
				if next.Type == "ebb" {
					return "ebbing"
				}
			}
			return "near slack"
		}
	}

	if next != nil {
		switch next.Type {
		case "flood":
			return "flooding"
		case "ebb":
			return "ebbing"
		case "slack":
			return "near slack"
		}
	}
	return ""
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `
Delta Sailing Data

Usage:
  sailing-go [options]

Options:
  -server
        Run as a REST API server.

  -station STATION
        NDBC wind station ID. Default: PSBC1

  -current-station ID
        Optional NOAA current prediction station override.
        Example: SFB1325

  -current-bin BIN
        Optional NOAA current prediction bin override.
        Example: 9

  -port PORT
        Local HTTP server port. Default: 8080
        Render's PORT environment variable takes precedence.

  -at DATETIME
        Historical wind report plus current predictions for the
        same local date, e.g.:
          2026-08-20T15:00
          2026-08-20 15:00

  -start HOUR
        Optional conditions-window start hour; omit start/end for sunrise-to-sunset.
        Default: 12

  -end HOUR
        Optional conditions-window end hour; omit start/end for sunrise-to-sunset.
        Default: 17

Examples:

  Current combined report:
    ./sailing-go

  Richmond-area combined report:
    ./sailing-go -station RCMC1

  Historical wind + current report:
    ./sailing-go -at "2026-08-20T15:00"

  Start REST server:
    ./sailing-go -server

  Local current combined report:
    curl -sS "http://localhost:8080/report"

  Richmond report:
    curl -sS "http://localhost:8080/report?station=RCMC1"

  Resolve nearest usable wind station from decimal coordinates:
    curl -sS "http://localhost:8080/report?lat=37.9105&lon=-122.3602"

  Diagnose wind-station selection:
    curl -sS "http://localhost:8080/report?lat=37.9105&lon=-122.3602&debug_wind=1"

  Force Simmons Point current prediction for PSBC1:
    curl -sS       "http://localhost:8080/report?station=PSBC1&current_station=SFB1325&bin=9"

  Change current window:
    curl -sS "http://localhost:8080/report?station=PSBC1&start=11&end=18"

  Full JSON:
    curl -sS \
      "http://localhost:8080/report?station=PSBC1&format=json"

  Compact text (BOTTOM LINE, WIND, CURRENT only):
    curl -sS \
      "http://localhost:8080/report?station=PSBC1&compact=1"

  Voice-friendly Bottom Line only:
    curl -sS \
      "http://localhost:8080/voice?station=PSBC1"

  Compact JSON for Alexa / assistants:
    curl -sS \
      "http://localhost:8080/report?station=PSBC1&format=json&compact=1"

  The Accept: application/json header is still supported.

`)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
