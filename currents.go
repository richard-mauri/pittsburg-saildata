package main

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	ndbcStationsURL = "https://www.ndbc.noaa.gov/activestations.xml"

	// Important: use NOAA CURRENT PREDICTION stations here, not merely
	// active real-time current meters.
	currentMetadataURL = "https://api.tidesandcurrents.noaa.gov/mdapi/prod/webapi/stations.json?type=currentpredictions&units=english"

	currentDataURL = "https://api.tidesandcurrents.noaa.gov/api/prod/datagetter"

	noaaCurrentTimeFormat = "2006-01-02 15:04"

	// Metadata changes infrequently. Cache it to avoid an MDAPI request for
	// every sailing report while still refreshing periodically.
	currentMetadataCacheTTL = 24 * time.Hour
)

type NDBCStations struct {
	Stations []NDBCStation `xml:"station"`
}

type NDBCStation struct {
	ID   string  `xml:"id,attr" json:"id"`
	Name string  `xml:"name,attr" json:"name"`
	Lat  float64 `xml:"lat,attr" json:"lat"`
	Lon  float64 `xml:"lon,attr" json:"lon"`
	Met  string  `xml:"met,attr" json:"met,omitempty"`
}

// CurrentStation represents a NOAA CO-OPS CURRENT PREDICTION station.
//
// CurrBin, Depth, DepthType, and PredictionType come from MDAPI's
// currentpredictions catalog. DistanceNM is calculated locally relative to
// the requested NDBC wind station.
type CurrentStation struct {
	ID             string  `json:"id"`
	Name           string  `json:"name"`
	Lat            float64 `json:"lat"`
	Lon            float64 `json:"lon"`
	DistanceNM     float64 `json:"distance_nm"`
	SelectionScore float64 `json:"selection_score,omitempty"`
	CurrBin        int     `json:"currbin,omitempty"`
	Depth          float64 `json:"metadata_depth_ft,omitempty"`
	DepthType      string  `json:"depth_type,omitempty"`
	PredictionType string  `json:"prediction_type,omitempty"`
}

type CurrentPrediction struct {
	Type         string  `json:"Type"`
	MeanFloodDir int     `json:"meanFloodDir"`
	Bin          string  `json:"Bin"`
	MeanEbbDir   int     `json:"meanEbbDir"`
	Time         string  `json:"Time"`
	Depth        string  `json:"Depth"`
	Velocity     float64 `json:"Velocity_Major"`
}

type TimedPrediction struct {
	Prediction CurrentPrediction `json:"prediction"`
	Time       time.Time         `json:"time"`
}

type CurrentEvent struct {
	Type      string    `json:"type"`
	Time      time.Time `json:"time"`
	SpeedKT   float64   `json:"speed_kt,omitempty"`
	Direction int       `json:"direction,omitempty"`
}

type CurrentSample struct {
	Time       time.Time `json:"time"`
	VelocityKT float64   `json:"velocity_kt"`
}

type CurrentComparison struct {
	Type                string    `json:"type"`
	Time                time.Time `json:"time"`
	SpeedKT             float64   `json:"speed_kt"`
	OtherTodayTime      time.Time `json:"other_today_time,omitempty"`
	OtherTodaySpeedKT   float64   `json:"other_today_speed_kt,omitempty"`
	TodayComparison     string    `json:"today_comparison,omitempty"`
	Prior7DayAverageKT  float64   `json:"prior_7_day_average_kt,omitempty"`
	Prior7DayMinKT      float64   `json:"prior_7_day_min_kt,omitempty"`
	Prior7DayMaxKT      float64   `json:"prior_7_day_max_kt,omitempty"`
	Prior7DaySampleSize int       `json:"prior_7_day_sample_size,omitempty"`
	Prior7DayComparison string    `json:"prior_7_day_comparison,omitempty"`
}

type CurrentReport struct {
	WindReference  *NDBCStation        `json:"wind_reference,omitempty"`
	CurrentStation *CurrentStation     `json:"current_station,omitempty"`
	SelectionMode  string              `json:"selection_mode,omitempty"`
	Start          time.Time           `json:"window_start,omitempty"`
	End            time.Time           `json:"window_end,omitempty"`
	Outlook        []string            `json:"outlook,omitempty"`
	Events         []CurrentEvent      `json:"events,omitempty"`
	Comparisons    []CurrentComparison `json:"comparisons,omitempty"`
	Series         []CurrentSample     `json:"series,omitempty"`
	Units          string              `json:"units,omitempty"`
	Depth          string              `json:"depth_ft,omitempty"`
	Bin            string              `json:"bin,omitempty"`
	Error          string              `json:"error,omitempty"`
}

type currentAPIResponse struct {
	CurrentPredictions struct {
		Units string              `json:"units"`
		CP    []CurrentPrediction `json:"cp"`
	} `json:"current_predictions"`

	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// NOAA documentation has historically described the collection as
// "stationList" while JSON examples/implementations may expose "stations".
// Supporting both costs almost nothing and makes the parser more robust.
type currentMetadataResponse struct {
	Count       int               `json:"count"`
	Stations    []metadataStation `json:"stations"`
	StationList []metadataStation `json:"stationList"`
}

type metadataStation struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	Lat            flexibleFloat64 `json:"lat"`
	Lng            flexibleFloat64 `json:"lng"`
	CurrBin        flexibleInt     `json:"currbin"`
	Depth          flexibleFloat64 `json:"depth"`
	DepthType      string          `json:"depthType"`
	PredictionType string          `json:"type"`
}

type flexibleFloat64 float64

func (f *flexibleFloat64) UnmarshalJSON(data []byte) error {
	if string(data) == "null" || string(data) == `""` {
		*f = 0
		return nil
	}

	var number json.Number
	if err := json.Unmarshal(data, &number); err == nil {
		v, err := number.Float64()
		if err == nil {
			*f = flexibleFloat64(v)
			return nil
		}
	}

	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		if err != nil {
			return err
		}
		*f = flexibleFloat64(v)
		return nil
	}

	return fmt.Errorf("cannot parse %q as number", string(data))
}

type flexibleInt int

func (i *flexibleInt) UnmarshalJSON(data []byte) error {
	if string(data) == "null" || string(data) == `""` {
		*i = 0
		return nil
	}

	var n int
	if err := json.Unmarshal(data, &n); err == nil {
		*i = flexibleInt(n)
		return nil
	}

	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		v, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil {
			return err
		}
		*i = flexibleInt(v)
		return nil
	}

	return fmt.Errorf("cannot parse %q as integer", string(data))
}

var currentStationCache = struct {
	sync.RWMutex
	stations []CurrentStation
	loadedAt time.Time
}{}

func BuildCurrentReport(
	windStationID string,
	currentStationOverride string,
	currentBinOverride int,
	date time.Time,
	startHour int,
	endHour int,
	loc *time.Location,
) (*CurrentReport, error) {
	if startHour < 0 || startHour > 23 {
		return nil, fmt.Errorf("invalid current start hour %d", startHour)
	}
	if endHour < 1 || endHour > 24 {
		return nil, fmt.Errorf("invalid current end hour %d", endHour)
	}
	if endHour <= startHour {
		return nil, fmt.Errorf("current end hour must be later than start hour")
	}

	windStation, err := fetchNDBCStation(windStationID)
	if err != nil {
		return nil, err
	}

	currentStations, err := getCurrentPredictionStations()
	if err != nil {
		return nil, err
	}
	if len(currentStations) == 0 {
		return nil, fmt.Errorf("NOAA metadata returned no current prediction stations")
	}

	// Work on a request-local copy because DistanceNM depends on the selected
	// wind station and must not mutate the shared cache.
	candidates := append([]CurrentStation(nil), currentStations...)

	for i := range candidates {
		candidates[i].DistanceNM = distanceNM(
			windStation.Lat,
			windStation.Lon,
			candidates[i].Lat,
			candidates[i].Lon,
		)

		candidates[i].SelectionScore =
			currentStationSelectionScore(
				candidates[i],
			)
	}

	sort.Slice(
		candidates,
		func(i, j int) bool {
			if candidates[i].SelectionScore ==
				candidates[j].SelectionScore {
				return candidates[i].DistanceNM <
					candidates[j].DistanceNM
			}

			return candidates[i].SelectionScore <
				candidates[j].SelectionScore
		},
	)

	if debugCurrentStationsEnabled() {
		printCurrentStationCandidates(
			windStation,
			candidates,
			10,
		)
	}

	// Automatic mode uses a shallow/open-water heuristic rather than raw
	// nearest-distance selection. Explicit station/bin overrides still win.
	currentStation := candidates[0]

	if strings.TrimSpace(currentStationOverride) != "" {
		overrideID := strings.ToUpper(
			strings.TrimSpace(currentStationOverride),
		)

		var matched *CurrentStation

		for i := range candidates {
			if !strings.EqualFold(candidates[i].ID, overrideID) {
				continue
			}

			if currentBinOverride > 0 &&
				candidates[i].CurrBin != currentBinOverride {
				continue
			}

			copy := candidates[i]
			matched = &copy
			break
		}

		if matched == nil {
			if currentBinOverride > 0 {
				return nil, fmt.Errorf(
					"NOAA current prediction station %s bin %d not found in metadata",
					overrideID,
					currentBinOverride,
				)
			}

			return nil, fmt.Errorf(
				"NOAA current prediction station %s not found in metadata",
				overrideID,
			)
		}

		currentStation = *matched
	}

	if currentBinOverride > 0 {
		currentStation.CurrBin = currentBinOverride
	}

	dateString := date.In(loc).Format("20060102")

	predictions, units, err := fetchCurrentPredictions(
		currentStation.ID,
		currentStation.CurrBin,
		dateString,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"current prediction failed for %s bin %d: %w",
			currentStation.ID,
			currentStation.CurrBin,
			err,
		)
	}

	timed, err := parseCurrentPredictions(predictions, loc)
	if err != nil {
		return nil, err
	}

	start, end, err := currentWindow(
		dateString,
		startHour,
		endHour,
		loc,
	)
	if err != nil {
		return nil, err
	}

	relevant := selectCurrentEvents(
		timed,
		start,
		end,
	)

	outlook := buildCurrentOutlook(
		relevant,
		start,
		end,
	)

	// Show all max/slack events for the reporting calendar day. The prose
	// remains sailing-window focused, but the event table gives the sailor
	// enough context to compare the morning and evening cycles.
	events := currentEventsFromTimed(timed)

	// Compare today's maxima with the previous seven days at the exact same
	// NOAA station/bin. NOAA harmonic predictions already include the
	// astronomical spring/neap cycle, so no separate lunar model is needed.
	lookbackStart := date.In(loc).AddDate(0, 0, -7)
	lookbackEnd := date.In(loc).AddDate(0, 0, -1)

	var historicalEvents []CurrentEvent
	historyPredictions, _, historyErr := fetchCurrentPredictionsRange(
		currentStation.ID,
		currentStation.CurrBin,
		lookbackStart.Format("20060102"),
		lookbackEnd.Format("20060102"),
	)
	if historyErr == nil {
		if historyTimed, parseErr := parseCurrentPredictions(
			historyPredictions,
			loc,
		); parseErr == nil {
			historicalEvents = currentEventsFromTimed(historyTimed)
		}
	}

	comparisons := buildCurrentComparisons(events, historicalEvents)

	for _, comparison := range comparisons {
		if comparison.Time.Before(start) ||
			comparison.Time.After(end) {
			continue
		}
		if line := currentComparisonLine(comparison); line != "" {
			outlook = append(outlook, line)
		}
	}

	var series []CurrentSample
	if currentPredictionTypeName(currentStation.PredictionType) == "harmonic/reference" {
		if densePredictions, _, denseErr := fetchCurrentDensePredictions(
			currentStation.ID,
			currentStation.CurrBin,
			dateString,
		); denseErr == nil {
			if denseSamples, denseParseErr := currentSamplesFromPredictions(
				densePredictions,
				loc,
			); denseParseErr == nil {
				series = denseSamples
			}
		}
	}

	selectionMode := "automatic-scored"
	if strings.TrimSpace(currentStationOverride) != "" ||
		currentBinOverride > 0 {
		selectionMode = "override"
	}

	report := &CurrentReport{
		WindReference:  &windStation,
		CurrentStation: &currentStation,
		SelectionMode:  selectionMode,
		Start:          start,
		End:            end,
		Outlook:        outlook,
		Events:         events,
		Comparisons:    comparisons,
		Series:         series,
		Units:          units,
	}

	if len(timed) > 0 {
		report.Depth = timed[0].Prediction.Depth
		report.Bin = timed[0].Prediction.Bin
	} else {
		if currentStation.Depth > 0 {
			report.Depth = strconv.FormatFloat(currentStation.Depth, 'f', -1, 64)
		}
		if currentStation.CurrBin > 0 {
			report.Bin = strconv.Itoa(currentStation.CurrBin)
		}
	}

	return report, nil
}

func writeCurrentText(
	w io.Writer,
	report *CurrentReport,
) {
	if report == nil {
		fmt.Fprintln(w, "Current prediction unavailable.")
		return
	}
	if report.Error != "" {
		fmt.Fprintf(w, "Current prediction unavailable: %s\n", report.Error)
		return
	}

	if report.SelectionMode != "" {
		fmt.Fprintf(w, "Selection: %s.\n", report.SelectionMode)
	}
	if !report.Start.IsZero() && !report.End.IsZero() {
		fmt.Fprintf(w, "Prediction date: %s.\n", report.Start.Format("Mon Jan 2, 2006"))
		fmt.Fprintf(w, "Sailing window: %s–%s.\n",
			report.Start.Format("3:04 PM"),
			report.End.Format("3:04 PM"))
	}
	if report.CurrentStation != nil {
		fmt.Fprintf(w, "Using %s — %s, %.1f nmi from %s.\n",
			report.CurrentStation.ID,
			report.CurrentStation.Name,
			report.CurrentStation.DistanceNM,
			report.WindReference.ID)
		if report.CurrentStation.PredictionType != "" {
			fmt.Fprintf(w, "Prediction station type: %s.\n",
				currentPredictionTypeName(report.CurrentStation.PredictionType))
		}
	}
	if report.Depth != "" {
		fmt.Fprintf(w, "Prediction depth: %s ft", report.Depth)
		if report.CurrentStation != nil && report.CurrentStation.DepthType != "" {
			fmt.Fprintf(w, " (%s)", currentDepthTypeName(report.CurrentStation.DepthType))
		}
		fmt.Fprintln(w, ".")
	}
	if report.Bin != "" {
		fmt.Fprintf(w, "Prediction bin: %s.\n", report.Bin)
	}

	for _, line := range report.Outlook {
		fmt.Fprintln(w, line)
	}

	if len(report.Events) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "CURRENT EVENTS — FULL DAY")
		fmt.Fprintln(w, "--------------------------------")
		for _, event := range report.Events {
			switch event.Type {
			case "flood":
				fmt.Fprintf(w, "%8s  Max flood  %.2f kt → %03d°\n",
					event.Time.Format("3:04 PM"), event.SpeedKT, event.Direction)
			case "ebb":
				fmt.Fprintf(w, "%8s  Max ebb    %.2f kt → %03d°\n",
					event.Time.Format("3:04 PM"), event.SpeedKT, event.Direction)
			case "slack":
				fmt.Fprintf(w, "%8s  Slack\n", event.Time.Format("3:04 PM"))
			}
		}
	}

	if len(report.Comparisons) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "CURRENT CONTEXT")
		fmt.Fprintln(w, "--------------------------------")
		for _, c := range report.Comparisons {
			fmt.Fprintf(w, "%8s  Max %-5s %.2f kt",
				c.Time.Format("3:04 PM"), c.Type, c.SpeedKT)

			if c.TodayComparison != "" && c.OtherTodaySpeedKT > 0 {
				fmt.Fprintf(w, " — %s (other today: %.2f kt at %s)",
					c.TodayComparison,
					c.OtherTodaySpeedKT,
					c.OtherTodayTime.Format("3:04 PM"))
			}
			if c.Prior7DayComparison != "" {
				fmt.Fprintf(w, "; %s (7-day avg %.2f kt, range %.2f–%.2f kt)",
					c.Prior7DayComparison,
					c.Prior7DayAverageKT,
					c.Prior7DayMinKT,
					c.Prior7DayMaxKT)
			}
			fmt.Fprintln(w)
		}
	}
}

func fetchNDBCStation(stationID string) (NDBCStation, error) {
	client := &http.Client{Timeout: 15 * time.Second}

	resp, err := client.Get(ndbcStationsURL)
	if err != nil {
		return NDBCStation{}, fmt.Errorf(
			"NDBC station metadata request failed: %w",
			err,
		)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return NDBCStation{}, fmt.Errorf(
			"NDBC station metadata returned HTTP %d",
			resp.StatusCode,
		)
	}

	var stations NDBCStations

	if err := xml.NewDecoder(resp.Body).Decode(&stations); err != nil {
		return NDBCStation{}, fmt.Errorf(
			"unable to parse NDBC station metadata: %w",
			err,
		)
	}

	for _, station := range stations.Stations {
		if strings.EqualFold(station.ID, stationID) {
			return station, nil
		}
	}

	return NDBCStation{}, fmt.Errorf(
		"NDBC station %q not found in active station list",
		stationID,
	)
}

// getCurrentPredictionStations returns a cached copy of NOAA's
// currentpredictions catalog, refreshing it at most once per cache TTL.
func getCurrentPredictionStations() ([]CurrentStation, error) {
	currentStationCache.RLock()
	if len(currentStationCache.stations) > 0 &&
		time.Since(currentStationCache.loadedAt) < currentMetadataCacheTTL {
		result := append([]CurrentStation(nil), currentStationCache.stations...)
		currentStationCache.RUnlock()
		return result, nil
	}
	currentStationCache.RUnlock()

	currentStationCache.Lock()
	defer currentStationCache.Unlock()

	// Another request may have populated the cache while we waited for the lock.
	if len(currentStationCache.stations) > 0 &&
		time.Since(currentStationCache.loadedAt) < currentMetadataCacheTTL {
		return append([]CurrentStation(nil), currentStationCache.stations...), nil
	}

	stations, err := fetchCurrentPredictionStations()
	if err != nil {
		// If a stale cache exists, prefer stale metadata over breaking the entire
		// sailing report because NOAA MDAPI is temporarily unavailable.
		if len(currentStationCache.stations) > 0 {
			return append([]CurrentStation(nil), currentStationCache.stations...), nil
		}
		return nil, err
	}

	currentStationCache.stations = append([]CurrentStation(nil), stations...)
	currentStationCache.loadedAt = time.Now()

	return append([]CurrentStation(nil), stations...), nil
}

func fetchCurrentPredictionStations() ([]CurrentStation, error) {
	client := &http.Client{Timeout: 20 * time.Second}

	resp, err := client.Get(currentMetadataURL)
	if err != nil {
		return nil, fmt.Errorf(
			"NOAA current-prediction metadata request failed: %w",
			err,
		)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"NOAA current-prediction metadata returned HTTP %d",
			resp.StatusCode,
		)
	}

	var raw currentMetadataResponse

	decoder := json.NewDecoder(resp.Body)
	decoder.UseNumber()

	if err := decoder.Decode(&raw); err != nil {
		return nil, fmt.Errorf(
			"unable to decode NOAA current-prediction metadata: %w",
			err,
		)
	}

	records := raw.Stations
	if len(records) == 0 {
		records = raw.StationList
	}

	if len(records) == 0 {
		return nil, fmt.Errorf(
			"NOAA current-prediction metadata contained no stations",
		)
	}

	stations := make([]CurrentStation, 0, len(records))

	for _, record := range records {
		id := strings.TrimSpace(record.ID)
		name := strings.TrimSpace(record.Name)
		lat := float64(record.Lat)
		lon := float64(record.Lng)

		// Lat/lon of 0,0 is not a useful marine station and usually means
		// incomplete metadata. Keep legitimate zero latitude or longitude
		// stations, but reject an all-zero coordinate pair.
		if id == "" || (lat == 0 && lon == 0) {
			continue
		}

		stations = append(
			stations,
			CurrentStation{
				ID:             id,
				Name:           name,
				Lat:            lat,
				Lon:            lon,
				CurrBin:        int(record.CurrBin),
				Depth:          float64(record.Depth),
				DepthType:      strings.ToUpper(strings.TrimSpace(record.DepthType)),
				PredictionType: strings.ToUpper(strings.TrimSpace(record.PredictionType)),
			},
		)
	}

	if len(stations) == 0 {
		return nil, fmt.Errorf(
			"NOAA current-prediction metadata had no usable station records",
		)
	}

	return stations, nil
}

// currentStationSelectionScore ranks NOAA current-prediction candidates.
//
// Lower scores are better.
//
// The goal is not to encode specific station IDs. Instead, this heuristic
// favors the sort of current prediction useful to a small-boat sailor:
//
//   - nearby
//   - harmonic/reference station
//   - shallow prediction depth, centered around about 6 ft
//   - open-water / point locations rather than narrow sloughs, creeks,
//     rivers, bridges, channels, or entrances
//
// This is intentionally transparent and tunable. The explicit
// current_station/bin override remains available whenever local knowledge
// should take precedence over the automatic heuristic.
func currentStationSelectionScore(
	station CurrentStation,
) float64 {
	score := station.DistanceNM

	// Prefer harmonic/reference prediction stations over subordinate
	// stations because they carry direct harmonic predictions for their
	// location/depth.
	switch strings.ToUpper(
		strings.TrimSpace(
			station.PredictionType,
		),
	) {
	case "H":
		// No penalty.
	case "S":
		score += 3.0
	case "W":
		score += 4.0
	default:
		score += 1.0
	}

	// Prefer shallow predictions around 6 ft, roughly matching the
	// near-surface current depth BASK uses at Simmons Point.
	if station.Depth > 0 {
		score += math.Abs(station.Depth-6.0) * 0.15
	} else {
		score += 1.0
	}

	// Avoid automatically selecting a nearby but hydrodynamically narrow
	// feature when the sailing area is broader water. These are generic
	// location-type penalties, not station-ID mappings.
	name := strings.ToLower(station.Name)

	narrowFeaturePenalties := map[string]float64{
		"slough":   2.0,
		"creek":    2.0,
		"river":    2.0,
		"bridge":   1.25,
		"channel":  1.25,
		"entrance": 0.75,
	}

	for word, penalty := range narrowFeaturePenalties {
		if strings.Contains(name, word) {
			score += penalty
		}
	}

	return score
}

func debugCurrentStationsEnabled() bool {
	value := strings.ToLower(
		strings.TrimSpace(
			os.Getenv("DEBUG_CURRENT_STATIONS"),
		),
	)

	switch value {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func printCurrentStationCandidates(
	wind NDBCStation,
	candidates []CurrentStation,
	limit int,
) {
	if limit <= 0 || limit > len(candidates) {
		limit = len(candidates)
	}

	fmt.Fprintf(
		os.Stderr,
		"\nNEAREST NOAA CURRENT-PREDICTION CANDIDATES TO %s — %s\n",
		wind.ID,
		wind.Name,
	)
	fmt.Fprintln(
		os.Stderr,
		"--------------------------------------------------------------------------------",
	)
	fmt.Fprintf(
		os.Stderr,
		"%-3s %-14s %-34s %8s %8s %6s %9s %-14s %-10s\n",
		"#",
		"ID",
		"Name",
		"Dist",
		"Score",
		"Bin",
		"Depth",
		"DepthType",
		"Type",
	)

	for i := 0; i < limit; i++ {
		s := candidates[i]

		fmt.Fprintf(
			os.Stderr,
			"%-3d %-14s %-34.34s %6.2fNM %8.2f %6d %7.1fft %-14s %-10s\n",
			i+1,
			s.ID,
			s.Name,
			s.DistanceNM,
			s.SelectionScore,
			s.CurrBin,
			s.Depth,
			currentDepthTypeName(s.DepthType),
			currentPredictionTypeName(s.PredictionType),
		)
	}

	fmt.Fprintln(
		os.Stderr,
		"--------------------------------------------------------------------------------",
	)

	// Print every Simmons Point / SFB1325 metadata row. NOAA may publish
	// multiple prediction bins/depths under the same station ID. BASK's
	// reference is SFB1325_9, so seeing all rows lets us verify exactly
	// which NOAA currbin/depth corresponds to that prediction.
	fmt.Fprintln(os.Stderr, "ALL SFB1325 / SIMMONS POINT CANDIDATES")
	fmt.Fprintln(
		os.Stderr,
		"--------------------------------------------------------------------------------",
	)

	foundSimmons := false

	for _, s := range candidates {
		id := strings.ToUpper(strings.TrimSpace(s.ID))
		name := strings.ToLower(strings.TrimSpace(s.Name))

		if id != "SFB1325" &&
			!strings.Contains(id, "SFB1325_") &&
			!strings.Contains(name, "simmons point") {
			continue
		}

		foundSimmons = true

		compositeID := s.ID
		if s.CurrBin > 0 &&
			!strings.HasSuffix(
				strings.ToUpper(compositeID),
				fmt.Sprintf("_%d", s.CurrBin),
			) {
			compositeID = fmt.Sprintf("%s_%d", s.ID, s.CurrBin)
		}

		fmt.Fprintf(
			os.Stderr,
			"ID=%-12s NOAA_ID=%-14s distance=%5.2fNM score=%5.2f bin=%-3d depth=%5.1fft depthType=%-18s type=%s name=%q\n",
			s.ID,
			compositeID,
			s.DistanceNM,
			s.SelectionScore,
			s.CurrBin,
			s.Depth,
			currentDepthTypeName(s.DepthType),
			currentPredictionTypeName(s.PredictionType),
			s.Name,
		)
	}

	if !foundSimmons {
		fmt.Fprintln(
			os.Stderr,
			"No SFB1325 / Simmons Point rows were found in the NOAA currentpredictions metadata cache.",
		)
	}

	fmt.Fprintln(
		os.Stderr,
		"--------------------------------------------------------------------------------",
	)
	fmt.Fprintln(os.Stderr)
}

// dataGetterStationID handles composite metadata labels such as SFB1325_9.
// NOAA's Data Retrieval API expects the station portion and bin separately.
func dataGetterStationID(id string) string {
	id = strings.TrimSpace(id)

	if cut := strings.LastIndex(id, "_"); cut > 0 && cut < len(id)-1 {
		if _, err := strconv.Atoi(id[cut+1:]); err == nil {
			return id[:cut]
		}
	}

	return id
}

func fetchCurrentPredictions(
	station string,
	bin int,
	date string,
) ([]CurrentPrediction, string, error) {
	return fetchCurrentPredictionsRange(station, bin, date, date)
}

func fetchCurrentPredictionsRange(
	station string,
	bin int,
	beginDate string,
	endDate string,
) ([]CurrentPrediction, string, error) {
	params := url.Values{}

	params.Set("product", "currents_predictions")
	params.Set("application", "pittsburg-saildata")
	params.Set("begin_date", beginDate)
	params.Set("end_date", endDate)
	params.Set("station", dataGetterStationID(station))
	params.Set("time_zone", "lst_ldt")
	params.Set("units", "english")
	params.Set("interval", "max_slack")
	params.Set("format", "json")

	if bin > 0 {
		params.Set("bin", strconv.Itoa(bin))
	}

	requestURL := currentDataURL + "?" + params.Encode()

	client := &http.Client{Timeout: 15 * time.Second}

	resp, err := client.Get(requestURL)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf(
			"NOAA current API returned HTTP %d",
			resp.StatusCode,
		)
	}

	var data currentAPIResponse

	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, "", err
	}

	if data.Error != nil {
		return nil, "", fmt.Errorf(
			"NOAA current error: %s",
			strings.TrimSpace(data.Error.Message),
		)
	}

	if len(data.CurrentPredictions.CP) == 0 {
		return nil, "", fmt.Errorf(
			"NOAA returned no current predictions for station %s bin %d",
			station,
			bin,
		)
	}

	return data.CurrentPredictions.CP,
		data.CurrentPredictions.Units,
		nil
}

func fetchCurrentDensePredictions(
	station string,
	bin int,
	date string,
) ([]CurrentPrediction, string, error) {
	params := url.Values{}

	params.Set("product", "currents_predictions")
	params.Set("application", "pittsburg-saildata")
	params.Set("begin_date", date)
	params.Set("end_date", date)
	params.Set("station", dataGetterStationID(station))
	params.Set("time_zone", "lst_ldt")
	params.Set("units", "english")
	params.Set("interval", "6")
	params.Set("format", "json")

	if bin > 0 {
		params.Set("bin", strconv.Itoa(bin))
	}

	requestURL := currentDataURL + "?" + params.Encode()
	client := &http.Client{Timeout: 15 * time.Second}

	resp, err := client.Get(requestURL)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf(
			"NOAA dense current API returned HTTP %d",
			resp.StatusCode,
		)
	}

	var data currentAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, "", err
	}
	if data.Error != nil {
		return nil, "", fmt.Errorf(
			"NOAA dense current error: %s",
			strings.TrimSpace(data.Error.Message),
		)
	}
	if len(data.CurrentPredictions.CP) == 0 {
		return nil, "", fmt.Errorf(
			"NOAA returned no 6-minute current predictions for station %s bin %d",
			station,
			bin,
		)
	}

	return data.CurrentPredictions.CP,
		data.CurrentPredictions.Units,
		nil
}

func currentSamplesFromPredictions(
	predictions []CurrentPrediction,
	loc *time.Location,
) ([]CurrentSample, error) {
	timed, err := parseCurrentPredictions(predictions, loc)
	if err != nil {
		return nil, err
	}

	samples := make([]CurrentSample, 0, len(timed))
	for _, item := range timed {
		samples = append(samples, CurrentSample{
			Time:       item.Time,
			VelocityKT: item.Prediction.Velocity,
		})
	}
	return samples, nil
}

func currentPredictionTypeName(value string) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "H":
		return "harmonic/reference"
	case "S":
		return "subordinate"
	case "W":
		return "weak/variable"
	default:
		return value
	}
}

func currentDepthTypeName(value string) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "S":
		return "below surface"
	case "B":
		return "below chart datum"
	case "U":
		return "depth reference unknown"
	default:
		return value
	}
}

func parseCurrentPredictions(
	predictions []CurrentPrediction,
	loc *time.Location,
) ([]TimedPrediction, error) {
	result := make(
		[]TimedPrediction,
		0,
		len(predictions),
	)

	for _, p := range predictions {
		t, err := time.ParseInLocation(
			noaaCurrentTimeFormat,
			p.Time,
			loc,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"unable to parse NOAA current time %q: %w",
				p.Time,
				err,
			)
		}

		result = append(
			result,
			TimedPrediction{
				Prediction: p,
				Time:       t,
			},
		)
	}

	return result, nil
}

func currentWindow(
	date string,
	startHour int,
	endHour int,
	loc *time.Location,
) (
	time.Time,
	time.Time,
	error,
) {
	base, err := time.ParseInLocation(
		"20060102",
		date,
		loc,
	)
	if err != nil {
		return time.Time{},
			time.Time{},
			fmt.Errorf(
				"invalid current date %q",
				date,
			)
	}

	start := time.Date(
		base.Year(),
		base.Month(),
		base.Day(),
		startHour,
		0, 0, 0,
		loc,
	)

	end := time.Date(
		base.Year(),
		base.Month(),
		base.Day(),
		endHour,
		0, 0, 0,
		loc,
	)

	return start, end, nil
}

func selectCurrentEvents(
	events []TimedPrediction,
	start time.Time,
	end time.Time,
) []TimedPrediction {
	var before *TimedPrediction
	var after *TimedPrediction
	var inside []TimedPrediction

	for i := range events {
		event := events[i]

		if event.Time.Before(start) {
			copy := event
			before = &copy
			continue
		}

		if !event.Time.After(end) {
			inside = append(inside, event)
			continue
		}

		if after == nil {
			copy := event
			after = &copy
		}
	}

	var result []TimedPrediction

	if before != nil {
		result = append(result, *before)
	}

	result = append(result, inside...)

	if after != nil {
		result = append(result, *after)
	}

	return result
}

func buildCurrentOutlook(
	events []TimedPrediction,
	start time.Time,
	end time.Time,
) []string {
	if len(events) == 0 {
		return []string{
			"No current prediction events are available for this period.",
		}
	}

	var lines []string

	switch currentAtStart(events, start) {
	case "flood":
		lines = append(
			lines,
			"The sailing window starts on a flood current.",
		)
	case "ebb":
		lines = append(
			lines,
			"The sailing window starts on an ebb current.",
		)
	case "slack":
		lines = append(
			lines,
			"The sailing window begins close to slack water.",
		)
	}

	for _, event := range events {
		if event.Time.Before(start) || event.Time.After(end) {
			continue
		}

		p := event.Prediction

		switch p.Type {
		case "slack":
			next := nextNonSlackCurrent(
				events,
				event.Time,
			)

			if next == nil {
				lines = append(
					lines,
					fmt.Sprintf(
						"Slack is around %s.",
						event.Time.Format("3:04 PM"),
					),
				)
				continue
			}

			switch next.Prediction.Type {
			case "ebb":
				lines = append(
					lines,
					fmt.Sprintf(
						"Slack is around %s, followed by an ebb.",
						event.Time.Format("3:04 PM"),
					),
				)
			case "flood":
				lines = append(
					lines,
					fmt.Sprintf(
						"Slack is around %s, followed by a flood.",
						event.Time.Format("3:04 PM"),
					),
				)
			}

		case "flood":
			lines = append(
				lines,
				fmt.Sprintf(
					"The flood peaks around %s at %.1f kt.",
					event.Time.Format("3:04 PM"),
					absFloat(p.Velocity),
				),
			)

		case "ebb":
			lines = append(
				lines,
				fmt.Sprintf(
					"The ebb peaks around %s at %.1f kt.",
					event.Time.Format("3:04 PM"),
					absFloat(p.Velocity),
				),
			)
		}
	}

	after := firstCurrentEventAfter(events, end)
	if after != nil &&
		after.Prediction.Type == "slack" {
		lines = append(
			lines,
			fmt.Sprintf(
				"The current then weakens toward another slack around %s.",
				after.Time.Format("3:04 PM"),
			),
		)
	}

	lines = append(
		lines,
		currentOverallAssessment(
			events,
			start,
			end,
		),
	)

	return lines
}

func currentAtStart(
	events []TimedPrediction,
	start time.Time,
) string {
	var previous *TimedPrediction
	var next *TimedPrediction

	for i := range events {
		event := events[i]

		if !event.Time.After(start) {
			copy := event
			previous = &copy
			continue
		}

		copy := event
		next = &copy
		break
	}

	if previous == nil {
		if next != nil {
			return next.Prediction.Type
		}
		return ""
	}

	if previous.Prediction.Type == "slack" {
		if next != nil {
			return next.Prediction.Type
		}
		return "slack"
	}

	return previous.Prediction.Type
}

func nextNonSlackCurrent(
	events []TimedPrediction,
	after time.Time,
) *TimedPrediction {
	for i := range events {
		event := events[i]

		if !event.Time.After(after) {
			continue
		}
		if event.Prediction.Type == "slack" {
			continue
		}

		copy := event
		return &copy
	}

	return nil
}

func firstCurrentEventAfter(
	events []TimedPrediction,
	after time.Time,
) *TimedPrediction {
	for i := range events {
		event := events[i]
		if event.Time.After(after) {
			copy := event
			return &copy
		}
	}

	return nil
}

func currentOverallAssessment(
	events []TimedPrediction,
	start time.Time,
	end time.Time,
) string {
	var peak *TimedPrediction

	for i := range events {
		event := events[i]
		if event.Time.Before(start) ||
			event.Time.After(end) ||
			event.Prediction.Type == "slack" {
			continue
		}

		if peak == nil ||
			absFloat(event.Prediction.Velocity) >
				absFloat(peak.Prediction.Velocity) {
			copy := event
			peak = &copy
		}
	}

	if peak == nil {
		return "No maximum-current event falls inside the sailing window."
	}

	return fmt.Sprintf(
		"Peak predicted current during the sailing window is %.1f kt (%s) around %s.",
		absFloat(peak.Prediction.Velocity),
		peak.Prediction.Type,
		peak.Time.Format("3:04 PM"),
	)
}

func currentEventsFromTimed(timed []TimedPrediction) []CurrentEvent {
	events := make([]CurrentEvent, 0, len(timed))
	for _, event := range timed {
		p := event.Prediction
		e := CurrentEvent{Type: p.Type, Time: event.Time}
		switch p.Type {
		case "flood":
			e.SpeedKT = absFloat(p.Velocity)
			e.Direction = p.MeanFloodDir
		case "ebb":
			e.SpeedKT = absFloat(p.Velocity)
			e.Direction = p.MeanEbbDir
		}
		events = append(events, e)
	}
	return events
}

func buildCurrentComparisons(today, history []CurrentEvent) []CurrentComparison {
	var result []CurrentComparison

	for _, event := range today {
		if event.Type != "ebb" && event.Type != "flood" {
			continue
		}
		c := CurrentComparison{
			Type:    event.Type,
			Time:    event.Time,
			SpeedKT: event.SpeedKT,
		}

		for _, other := range today {
			if other.Type == event.Type && !other.Time.Equal(event.Time) {
				c.OtherTodayTime = other.Time
				c.OtherTodaySpeedKT = other.SpeedKT
				c.TodayComparison = relativeCurrentPhrase(event.SpeedKT, other.SpeedKT)
				break
			}
		}

		var vals []float64
		for _, prior := range history {
			if prior.Type == event.Type && prior.SpeedKT > 0 {
				vals = append(vals, prior.SpeedKT)
			}
		}
		if len(vals) > 0 {
			sum, minV, maxV := 0.0, vals[0], vals[0]
			for _, v := range vals {
				sum += v
				if v < minV {
					minV = v
				}
				if v > maxV {
					maxV = v
				}
			}
			c.Prior7DaySampleSize = len(vals)
			c.Prior7DayAverageKT = sum / float64(len(vals))
			c.Prior7DayMinKT = minV
			c.Prior7DayMaxKT = maxV
			c.Prior7DayComparison = recentCurrentPhrase(
				event.SpeedKT,
				c.Prior7DayAverageKT,
				minV,
				maxV,
			)
		}
		result = append(result, c)
	}
	return result
}

func relativeCurrentPhrase(value, other float64) string {
	if value <= 0 || other <= 0 {
		return ""
	}
	r := value / other
	switch {
	case r >= 0.80 && r <= 1.20:
		return "similar in strength to the other same-phase maximum today"
	case r >= 0.65:
		return "somewhat weaker than the other same-phase maximum today"
	case r >= 0.55:
		return "substantially weaker than the other same-phase maximum today"
	case r >= 0.45:
		return "about half as strong as the other same-phase maximum today"
	case r < 0.45:
		return "much weaker than the other same-phase maximum today"
	case r <= 1.35:
		return "somewhat stronger than the other same-phase maximum today"
	case r < 1.80:
		return "substantially stronger than the other same-phase maximum today"
	case r <= 2.20:
		return "about twice as strong as the other same-phase maximum today"
	default:
		return "much stronger than the other same-phase maximum today"
	}
}

func recentCurrentPhrase(value, average, minV, maxV float64) string {
	if value <= 0 || average <= 0 {
		return ""
	}
	span := maxV - minV
	if span > 0 {
		if value <= minV+0.15*span {
			return "near the low end of the previous 7 days"
		}
		if value >= maxV-0.15*span {
			return "near the high end of the previous 7 days"
		}
	}
	r := value / average
	switch {
	case r < 0.80:
		return "below the previous 7-day average"
	case r > 1.20:
		return "above the previous 7-day average"
	default:
		return "near the previous 7-day average"
	}
}

func currentComparisonLine(c CurrentComparison) string {
	var parts []string
	if c.TodayComparison != "" && c.OtherTodaySpeedKT > 0 {
		parts = append(parts, fmt.Sprintf(
			"%s; the other %s max is %.2f kt around %s",
			c.TodayComparison,
			c.Type,
			c.OtherTodaySpeedKT,
			c.OtherTodayTime.Format("3:04 PM"),
		))
	}
	if c.Prior7DayComparison != "" {
		parts = append(parts, fmt.Sprintf(
			"%s (7-day average %.2f kt)",
			c.Prior7DayComparison,
			c.Prior7DayAverageKT,
		))
	}
	if len(parts) == 0 {
		return ""
	}
	return fmt.Sprintf(
		"The %s max at %s (%.2f kt) is %s.",
		c.Type,
		c.Time.Format("3:04 PM"),
		c.SpeedKT,
		strings.Join(parts, "; "),
	)
}

func findCurrentComparison(report *CurrentReport, event CurrentEvent) *CurrentComparison {
	if report == nil {
		return nil
	}
	for i := range report.Comparisons {
		c := &report.Comparisons[i]
		if c.Type == event.Type && c.Time.Equal(event.Time) {
			return c
		}
	}
	return nil
}

func currentStrength(knots float64) string {
	switch {
	case knots < 0.5:
		return "weak"
	case knots < 1.0:
		return "moderate"
	case knots < 1.5:
		return "strong"
	default:
		return "very strong"
	}
}

func distanceNM(
	lat1 float64,
	lon1 float64,
	lat2 float64,
	lon2 float64,
) float64 {
	const earthRadiusNM = 3440.065

	phi1 := degreesToRadians(lat1)
	phi2 := degreesToRadians(lat2)

	dPhi := degreesToRadians(lat2 - lat1)
	dLambda := degreesToRadians(lon2 - lon1)

	a :=
		math.Sin(dPhi/2)*math.Sin(dPhi/2) +
			math.Cos(phi1)*
				math.Cos(phi2)*
				math.Sin(dLambda/2)*
				math.Sin(dLambda/2)

	c := 2 * math.Atan2(
		math.Sqrt(a),
		math.Sqrt(1-a),
	)

	return earthRadiusNM * c
}

func degreesToRadians(degrees float64) float64 {
	return degrees * math.Pi / 180
}

func stringValue(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return ""
	}
}

func floatValue(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true

	case string:
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return 0, false
		}
		return f, true

	case json.Number:
		f, err := v.Float64()
		if err != nil {
			return 0, false
		}
		return f, true

	default:
		return 0, false
	}
}

func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
