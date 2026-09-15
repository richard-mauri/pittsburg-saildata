package main

import (
	"bytes"
	"context"
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
	"time"
)

const (
	appVersion                      = "1.9.3"
	buildVersion                    = "v172"
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

func main() {
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

func fetchNDBCAirTemperatureF(stationID string) (float64, bool) {
	stationID = strings.ToUpper(strings.TrimSpace(stationID))
	if !validStationID(stationID) {
		return 0, false
	}

	req, err := http.NewRequest(
		http.MethodGet,
		"https://www.ndbc.noaa.gov/data/realtime2/"+stationID+".txt",
		nil,
	)
	if err != nil {
		return 0, false
	}
	req.Header.Set("User-Agent", "pittsburg-saildata/"+appVersion)

	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, false
	}

	body, err := ioutil.ReadAll(io.LimitReader(resp.Body, 512<<10))
	if err != nil {
		return 0, false
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

		return airTempC*9/5 + 32, true
	}

	return 0, false
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

type coastWatchWindowStats struct {
	Point float64
	Min   float64
	Max   float64
	Count int
}

func coastWatchIPv4Client(timeout time.Duration) (*http.Client, error) {
	baseTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("default HTTP transport is unavailable")
	}
	transport := baseTransport.Clone()
	dialer := &net.Dialer{
		Timeout:   20 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return dialer.DialContext(ctx, "tcp4", address)
	}
	transport.TLSHandshakeTimeout = 20 * time.Second
	transport.ResponseHeaderTimeout = 35 * time.Second
	return &http.Client{Transport: transport, Timeout: timeout}, nil
}

func fetchCoastWatchWindowStats(
	dataset string,
	variable string,
	dataRequest string,
	targetLat float64,
	targetLon float64,
) (coastWatchWindowStats, error) {
	var result coastWatchWindowStats

	encoded := url.QueryEscape(dataRequest)
	encoded = strings.ReplaceAll(encoded, "+", "%20")
	remoteURL := fmt.Sprintf(
		"https://coastwatch.noaa.gov/erddap/griddap/%s.json?%s",
		dataset,
		encoded,
	)

	client, err := coastWatchIPv4Client(55 * time.Second)
	if err != nil {
		return result, err
	}
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

	body, err := ioutil.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return result, err
	}
	if resp.StatusCode != http.StatusOK {
		detail := strings.TrimSpace(string(body))
		if len(detail) > 500 {
			detail = detail[:500]
		}
		return result, fmt.Errorf("CoastWatch %s returned HTTP %d: %s", variable, resp.StatusCode, detail)
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

	latCol, lonCol, valueCol := -1, -1, -1
	for i, name := range payload.Table.ColumnNames {
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "latitude":
			latCol = i
		case "longitude":
			lonCol = i
		default:
			if strings.EqualFold(strings.TrimSpace(name), variable) {
				valueCol = i
			}
		}
	}
	if latCol < 0 || lonCol < 0 || valueCol < 0 {
		return result, fmt.Errorf("CoastWatch %s response is missing expected columns", variable)
	}

	asFloat := func(v interface{}) (float64, bool) {
		switch x := v.(type) {
		case float64:
			return x, !math.IsNaN(x) && !math.IsInf(x, 0)
		case json.Number:
			f, err := x.Float64()
			return f, err == nil
		case string:
			f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
			return f, err == nil && !math.IsNaN(f) && !math.IsInf(f, 0)
		default:
			return 0, false
		}
	}

	minVal := math.Inf(1)
	maxVal := math.Inf(-1)
	bestDistance := math.Inf(1)
	bestValue := 0.0
	count := 0

	for _, row := range payload.Table.Rows {
		maxIndex := latCol
		if lonCol > maxIndex {
			maxIndex = lonCol
		}
		if valueCol > maxIndex {
			maxIndex = valueCol
		}
		if len(row) <= maxIndex {
			continue
		}
		lat, okLat := asFloat(row[latCol])
		lon, okLon := asFloat(row[lonCol])
		value, okValue := asFloat(row[valueCol])
		if !okLat || !okLon || !okValue {
			continue
		}
		count++
		if value < minVal {
			minVal = value
		}
		if value > maxVal {
			maxVal = value
		}
		d := math.Hypot(lat-targetLat, (lon-targetLon)*math.Cos(targetLat*math.Pi/180))
		if d < bestDistance {
			bestDistance = d
			bestValue = value
		}
	}

	if count == 0 {
		return result, fmt.Errorf("CoastWatch %s returned no usable values", variable)
	}

	result.Point = bestValue
	result.Min = minVal
	result.Max = maxVal
	result.Count = count
	return result, nil
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

	mux.HandleFunc("/wind-readings", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		stationID := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("station")))
		if !validStationID(stationID) {
			http.Error(w, "valid station is required", http.StatusBadRequest)
			return
		}

		hours := parseWindReadingHours(r.URL.Query())
		windUnit := parseWindUnit(r.URL.Query())
		observations, err := getWindStation(stationID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}

		report := buildCurrentWindReport(stationID, observations, loc)
		report.Latest10 = windObservationsForHours(
			observations,
			report.ReportTime,
			loc,
			hours,
		)

		type reading struct {
			Time      string `json:"time"`
			Direction string `json:"direction"`
			Wind      string `json:"wind"`
			Gust      string `json:"gust"`
			Age       string `json:"age"`
		}
		payload := struct {
			Station  string    `json:"station"`
			Hours    int       `json:"hours"`
			Count    int       `json:"count"`
			Summary  string    `json:"summary"`
			Readings []reading `json:"readings"`
		}{
			Station: stationID,
			Hours:   hours,
			Count:   len(report.Latest10),
		}

		var summary strings.Builder
		writeWindSummaryDisplay(&summary, report, loc, windUnit)
		payload.Summary = strings.TrimSpace(summary.String())

		for _, item := range report.Latest10 {
			direction := strings.TrimSpace(item.Direction)
			if direction == "" {
				direction = "—"
			}
			windText := "—"
			gustText := "—"
			if item.WindKT > 0 {
				windText = formatWindSpeed(item.WindKT, 1, windUnit)
			}
			if item.GustKT > 0 {
				gustText = formatWindSpeed(item.GustKT, 1, windUnit)
			}
			age := report.ReportTime.Sub(item.Time)
			if age < 0 {
				age = 0
			}
			payload.Readings = append(payload.Readings, reading{
				Time:      item.Time.In(loc).Format("3:04 PM"),
				Direction: direction,
				Wind:      windText,
				Gust:      gustText,
				Age:       formatAge(age),
			})
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if err := json.NewEncoder(w).Encode(payload); err != nil {
			fmt.Println("wind-readings JSON encoding error:", err)
		}
	})

	mux.HandleFunc("/fishing-reports", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		body, err := ioutil.ReadFile("assets/fishing_reports.json")
		if err != nil {
			http.Error(
				w,
				"fishing report feed is unavailable: assets/fishing_reports.json could not be read: "+err.Error(),
				http.StatusServiceUnavailable,
			)
			return
		}
		if !json.Valid(body) {
			http.Error(
				w,
				"fishing report feed is unavailable: assets/fishing_reports.json is not valid JSON",
				http.StatusInternalServerError,
			)
			return
		}

		var payload struct {
			SchemaVersion int               `json:"schema_version"`
			Updated       string            `json:"updated"`
			Reports       []json.RawMessage `json:"reports"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			http.Error(w, "fishing report feed validation failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if payload.SchemaVersion != 1 {
			http.Error(w, "unsupported fishing report feed schema_version", http.StatusInternalServerError)
			return
		}
		if len(payload.Reports) == 0 {
			http.Error(w, "fishing report feed contains no reports", http.StatusServiceUnavailable)
			return
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		_, _ = w.Write(body)
	})

	mux.HandleFunc("/smoke-overlay", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		analysisDate, collection, smokeErr := fetchNOAAHMSSmoke()
		payload := struct {
			AnalysisDate string                     `json:"analysis_date,omitempty"`
			GeoJSON      *hmsSmokeFeatureCollection `json:"geojson,omitempty"`
			Error        string                     `json:"error,omitempty"`
			Note         string                     `json:"note"`
		}{
			AnalysisDate: analysisDate,
			GeoJSON:      collection,
			Note:         "NOAA HMS smoke polygons are qualitative satellite analysis (light/medium/heavy), not AQI or measured particulate concentration.",
		}
		if smokeErr != nil {
			payload.Error = smokeErr.Error()
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=900")
		if err := json.NewEncoder(w).Encode(payload); err != nil {
			fmt.Println("smoke-overlay JSON encoding error:", err)
		}
	})

	mux.HandleFunc("/sst-info", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		const metadataURL = "https://coastwatch.noaa.gov/erddap/griddap/noaacwBLENDEDsstDNDaily.das"
		req, err := http.NewRequest(http.MethodGet, metadataURL, nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		req.Header.Set("User-Agent", "pittsburg-saildata/"+appVersion)

		client := &http.Client{Timeout: 8 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			http.Error(w, fmt.Sprintf("NOAA CoastWatch returned HTTP %d", resp.StatusCode), http.StatusBadGateway)
			return
		}

		body, err := ioutil.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}

		latest := ""
		reCoverage := regexp.MustCompile(`String\s+time_coverage_end\s+"([^"]+)"`)
		if match := reCoverage.FindSubmatch(body); len(match) == 2 {
			latest = string(match[1])
		}
		if latest == "" {
			reTimeRange := regexp.MustCompile(`(?s)time\s*\{.*?Float64\s+actual_range\s+[0-9.eE+\-]+,\s*([0-9.eE+\-]+);`)
			if match := reTimeRange.FindSubmatch(body); len(match) == 2 {
				if seconds, parseErr := strconv.ParseFloat(string(match[1]), 64); parseErr == nil {
					latest = time.Unix(int64(seconds), 0).UTC().Format(time.RFC3339)
				}
			}
		}
		if latest == "" {
			latest = "current"
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=900")
		if err := json.NewEncoder(w).Encode(map[string]string{
			"time":     latest,
			"dataset":  "noaacwBLENDEDsstDNDaily",
			"variable": "analysed_sst",
		}); err != nil {
			fmt.Println("sst-info JSON encoding error:", err)
		}
	})

	mux.HandleFunc("/sst-overlay", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		q := r.URL.Query()
		parseFloat := func(name string) (float64, error) {
			raw := strings.TrimSpace(q.Get(name))
			if raw == "" {
				return 0, fmt.Errorf("%s is required", name)
			}
			v, err := strconv.ParseFloat(raw, 64)
			if err != nil {
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

		if west < -180 || west > 180 || east < -180 || east > 180 ||
			south < -89.9 || south > 89.9 || north < -89.9 || north > 89.9 ||
			west >= east || south >= north {
			http.Error(w, "invalid SST map bounds", http.StatusBadRequest)
			return
		}

		width := 1200
		height := 900
		if raw := strings.TrimSpace(q.Get("width")); raw != "" {
			if v, err := strconv.Atoi(raw); err == nil {
				width = v
			}
		}
		if raw := strings.TrimSpace(q.Get("height")); raw != "" {
			if v, err := strconv.Atoi(raw); err == nil {
				height = v
			}
		}
		if width < 256 {
			width = 256
		}
		if width > 2400 {
			width = 2400
		}
		if height < 256 {
			height = 256
		}
		if height > 1800 {
			height = 1800
		}

		timeValue := strings.TrimSpace(q.Get("time"))
		if timeValue == "" {
			timeValue = "current"
		}

		// Use ERDDAP griddap instead of WMS so the SST color scale is fixed.
		// The 45-75 F range (7.222-23.889 C) is intentionally fishing-oriented,
		// and 30 discrete sections create approximately 1 F color bands so
		// temperature breaks are easier to see and compare between map views.
		timeSelector := "(last)"
		if timeValue != "" && !strings.EqualFold(timeValue, "current") {
			timeSelector = "(" + timeValue + ")"
		}
		dataRequest := fmt.Sprintf(
			"analysed_sst[%s][(%.6f):(%.6f)][(%.6f):(%.6f)]",
			timeSelector,
			south,
			north,
			west,
			east,
		)

		graphics := url.Values{}
		graphics.Set(".draw", "surface")
		graphics.Set(".vars", "longitude|latitude|analysed_sst")
		graphics.Set(".colorBar", "Rainbow|D|Linear|7.222222|23.888889|30")
		graphics.Set(".land", "off")
		graphics.Set(".legend", "Off")
		graphics.Set(".size", strconv.Itoa(width)+"|"+strconv.Itoa(height))

		encodedDataRequest := url.QueryEscape(dataRequest)
		encodedDataRequest = strings.ReplaceAll(encodedDataRequest, "+", "%20")

		baseTransport, ok := http.DefaultTransport.(*http.Transport)
		if !ok {
			http.Error(w, "NOAA CoastWatch SST transport is unavailable", http.StatusInternalServerError)
			return
		}
		transport := baseTransport.Clone()
		dialer := &net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}
		// CoastWatch was resolving to IPv6 on the local network, but the IPv6
		// route timed out. Force SST traffic over IPv4 while leaving the rest of
		// the application networking unchanged.
		transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			return dialer.DialContext(ctx, "tcp4", address)
		}
		transport.TLSHandshakeTimeout = 30 * time.Second
		transport.ResponseHeaderTimeout = 45 * time.Second

		client := &http.Client{
			Transport: transport,
			Timeout:   75 * time.Second,
		}

		upstreams := []struct {
			label string
			base  string
		}{
			{
				label: "CoastWatch Central ERDDAP",
				base:  "https://coastwatch.noaa.gov/erddap/griddap/noaacwBLENDEDsstDNDaily.transparentPng?",
			},
		}

		var resp *http.Response
		var body []byte
		var upstreamLabel string
		var attemptErrors []string

		for _, upstream := range upstreams {
			remoteURL := upstream.base + encodedDataRequest + "&" + graphics.Encode()
			req, err := http.NewRequest(http.MethodGet, remoteURL, nil)
			if err != nil {
				attemptErrors = append(attemptErrors, upstream.label+": "+err.Error())
				continue
			}
			req.Header.Set("User-Agent", "pittsburg-saildata/"+appVersion)

			candidateResp, err := client.Do(req)
			if err != nil {
				attemptErrors = append(attemptErrors, upstream.label+": "+err.Error())
				continue
			}

			candidateBody, readErr := ioutil.ReadAll(io.LimitReader(candidateResp.Body, 12<<20))
			candidateResp.Body.Close()
			if readErr != nil {
				attemptErrors = append(attemptErrors, upstream.label+": response read failed: "+readErr.Error())
				continue
			}

			resp = candidateResp
			body = candidateBody
			upstreamLabel = upstream.label
			break
		}

		if resp == nil {
			detail := strings.Join(attemptErrors, " | ")
			if detail == "" {
				detail = "all configured NOAA CoastWatch SST endpoints failed"
			}
			http.Error(w, "NOAA CoastWatch Sea Surface Temp request failed: "+detail, http.StatusBadGateway)
			return
		}

		w.Header().Set("X-SST-Upstream", upstreamLabel)
		if resp.StatusCode != http.StatusOK {
			detail := strings.TrimSpace(string(body))
			if len(detail) > 500 {
				detail = detail[:500]
			}
			if detail == "" {
				detail = http.StatusText(resp.StatusCode)
			}
			http.Error(
				w,
				fmt.Sprintf("NOAA CoastWatch SST returned HTTP %d: %s", resp.StatusCode, detail),
				http.StatusBadGateway,
			)
			return
		}
		contentType := strings.ToLower(resp.Header.Get("Content-Type"))
		if !strings.Contains(contentType, "image/png") {
			detail := strings.TrimSpace(string(body))
			if len(detail) > 240 {
				detail = detail[:240]
			}
			if detail == "" {
				detail = "unexpected non-PNG response"
			}
			http.Error(w, "NOAA CoastWatch SST: "+detail, http.StatusBadGateway)
			return
		}

		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "public, max-age=900")
		_, _ = w.Write(body)
	})

	mux.HandleFunc("/chlorophyll-info", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		const metadataURL = "https://coastwatch.noaa.gov/erddap/griddap/noaacwNPPN20S3ASCIDINEOF2kmDaily.das"
		req, err := http.NewRequest(http.MethodGet, metadataURL, nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		req.Header.Set("User-Agent", "pittsburg-saildata/"+appVersion)

		client := &http.Client{Timeout: 12 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			http.Error(w, "NOAA chlorophyll metadata request failed: "+err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		body, err := ioutil.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if err != nil {
			http.Error(w, "NOAA chlorophyll metadata read failed: "+err.Error(), http.StatusBadGateway)
			return
		}
		if resp.StatusCode != http.StatusOK {
			detail := strings.TrimSpace(string(body))
			if len(detail) > 500 {
				detail = detail[:500]
			}
			http.Error(
				w,
				fmt.Sprintf("NOAA chlorophyll metadata returned HTTP %d: %s", resp.StatusCode, detail),
				http.StatusBadGateway,
			)
			return
		}

		// Prefer the time axis actual_range endpoint: it is the dataset's
		// actual last indexed coordinate and is safer than time_coverage_end
		// for constructing/displaying the latest field time.
		latest := ""
		reTimeRange := regexp.MustCompile(`(?s)time\s*\{.*?Float64\s+actual_range\s+[0-9.eE+\-]+,\s*([0-9.eE+\-]+);`)
		if match := reTimeRange.FindSubmatch(body); len(match) == 2 {
			if seconds, parseErr := strconv.ParseFloat(string(match[1]), 64); parseErr == nil {
				latest = time.Unix(int64(seconds), 0).UTC().Format(time.RFC3339)
			}
		}
		if latest == "" {
			reCoverage := regexp.MustCompile(`String\s+time_coverage_end\s+"([^"]+)"`)
			if match := reCoverage.FindSubmatch(body); len(match) == 2 {
				latest = string(match[1])
			}
		}
		if latest == "" {
			latest = "current"
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=900")
		if err := json.NewEncoder(w).Encode(map[string]string{
			"time":     latest,
			"dataset":  "noaacwNPPN20S3ASCIDINEOF2kmDaily",
			"variable": "chlor_a",
		}); err != nil {
			fmt.Println("chlorophyll-info JSON encoding error:", err)
		}
	})

	mux.HandleFunc("/chlorophyll-field", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		q := r.URL.Query()
		parseFloat := func(name string) (float64, error) {
			raw := strings.TrimSpace(q.Get(name))
			if raw == "" {
				return 0, fmt.Errorf("%s is required", name)
			}
			v, err := strconv.ParseFloat(raw, 64)
			if err != nil {
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

		if west < -180 || west > 180 || east < -180 || east > 180 ||
			south < -89.9 || south > 89.9 || north < -89.9 || north > 89.9 ||
			west >= east || south >= north {
			http.Error(w, "invalid chlorophyll map bounds", http.StatusBadRequest)
			return
		}

		width := 1200
		height := 900
		if raw := strings.TrimSpace(q.Get("width")); raw != "" {
			if v, err := strconv.Atoi(raw); err == nil {
				width = v
			}
		}
		if raw := strings.TrimSpace(q.Get("height")); raw != "" {
			if v, err := strconv.Atoi(raw); err == nil {
				height = v
			}
		}
		if width < 256 {
			width = 256
		}
		if width > 1800 {
			width = 1800
		}
		if height < 256 {
			height = 256
		}
		if height > 1400 {
			height = 1400
		}

		dataRequest := fmt.Sprintf(
			"chlor_a[last][0][(%.6f):(%.6f)][(%.6f):(%.6f)]",
			south,
			north,
			west,
			east,
		)

		graphics := url.Values{}
		graphics.Set(".draw", "surface")
		graphics.Set(".vars", "longitude|latitude|chlor_a")
		graphics.Set(".colorBar", "Rainbow|C|Log|0.1|1|")
		graphics.Set(".land", "off")
		graphics.Set(".legend", "Off")
		graphics.Set(".size", strconv.Itoa(width)+"|"+strconv.Itoa(height))

		encodedDataRequest := url.QueryEscape(dataRequest)
		encodedDataRequest = strings.ReplaceAll(encodedDataRequest, "+", "%20")
		remoteURL := "https://coastwatch.noaa.gov/erddap/griddap/noaacwNPPN20S3ASCIDINEOF2kmDaily.transparentPng?" +
			encodedDataRequest + "&" + graphics.Encode()

		baseTransport, ok := http.DefaultTransport.(*http.Transport)
		if !ok {
			http.Error(w, "NOAA CoastWatch chlorophyll transport is unavailable", http.StatusInternalServerError)
			return
		}
		transport := baseTransport.Clone()
		dialer := &net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}
		transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			return dialer.DialContext(ctx, "tcp4", address)
		}
		transport.TLSHandshakeTimeout = 30 * time.Second
		transport.ResponseHeaderTimeout = 45 * time.Second
		client := &http.Client{Transport: transport, Timeout: 75 * time.Second}

		req, err := http.NewRequest(http.MethodGet, remoteURL, nil)
		if err != nil {
			http.Error(w, "chlorophyll field request construction failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		req.Header.Set("User-Agent", "pittsburg-saildata/"+appVersion)

		resp, err := client.Do(req)
		if err != nil {
			http.Error(w, "NOAA CoastWatch chlorophyll field request failed: "+err.Error()+" | upstream="+remoteURL, http.StatusBadGateway)
			return
		}
		body, readErr := ioutil.ReadAll(io.LimitReader(resp.Body, 12<<20))
		resp.Body.Close()
		if readErr != nil {
			http.Error(w, "NOAA chlorophyll field response read failed: "+readErr.Error(), http.StatusBadGateway)
			return
		}
		if resp.StatusCode != http.StatusOK {
			detail := strings.TrimSpace(string(body))
			if len(detail) > 1200 {
				detail = detail[:1200]
			}
			http.Error(w, fmt.Sprintf("NOAA chlorophyll field returned HTTP %d: %s | upstream=%s", resp.StatusCode, detail, remoteURL), http.StatusBadGateway)
			return
		}
		if !strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "image/png") {
			http.Error(w, "NOAA chlorophyll field returned non-PNG content | upstream="+remoteURL, http.StatusBadGateway)
			return
		}

		w.Header().Set("X-Chlorophyll-Upstream", "CoastWatch Central ERDDAP · DINEOF field")
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "public, max-age=900")
		_, _ = w.Write(body)
	})

	mux.HandleFunc("/chlorophyll-overlay", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		q := r.URL.Query()
		parseFloat := func(name string) (float64, error) {
			raw := strings.TrimSpace(q.Get(name))
			if raw == "" {
				return 0, fmt.Errorf("%s is required", name)
			}
			v, err := strconv.ParseFloat(raw, 64)
			if err != nil {
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

		if west < -180 || west > 180 || east < -180 || east > 180 ||
			south < -89.9 || south > 89.9 || north < -89.9 || north > 89.9 ||
			west >= east || south >= north {
			http.Error(w, "invalid chlorophyll map bounds", http.StatusBadRequest)
			return
		}

		width := 1200
		height := 900
		if raw := strings.TrimSpace(q.Get("width")); raw != "" {
			if v, err := strconv.Atoi(raw); err == nil {
				width = v
			}
		}
		if raw := strings.TrimSpace(q.Get("height")); raw != "" {
			if v, err := strconv.Atoi(raw); err == nil {
				height = v
			}
		}
		if width < 256 {
			width = 256
		}
		if width > 1600 {
			width = 1600
		}
		if height < 256 {
			height = 256
		}
		if height > 1200 {
			height = 1200
		}

		// The source grid is ~2 km. Limit the numeric request to roughly
		// 220 samples per axis at large map extents by using ERDDAP stride.
		latSpan := north - south
		lonSpan := east - west
		latPoints := int(math.Ceil(latSpan/0.018)) + 1
		lonPoints := int(math.Ceil(lonSpan/0.018)) + 1
		latStride := 1
		lonStride := 1
		if latPoints > 220 {
			latStride = int(math.Ceil(float64(latPoints) / 220.0))
		}
		if lonPoints > 220 {
			lonStride = int(math.Ceil(float64(lonPoints) / 220.0))
		}

		dataRequest := fmt.Sprintf(
			"chlor_a[last][0][(%.6f):%d:(%.6f)][(%.6f):%d:(%.6f)]",
			south,
			latStride,
			north,
			west,
			lonStride,
			east,
		)
		encodedRequest := url.QueryEscape(dataRequest)
		encodedRequest = strings.ReplaceAll(encodedRequest, "+", "%20")
		remoteURL := "https://coastwatch.noaa.gov/erddap/griddap/noaacwNPPN20S3ASCIDINEOF2kmDaily.json?" + encodedRequest

		baseTransport, ok := http.DefaultTransport.(*http.Transport)
		if !ok {
			http.Error(w, "NOAA CoastWatch chlorophyll transport is unavailable", http.StatusInternalServerError)
			return
		}
		transport := baseTransport.Clone()
		dialer := &net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}
		transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			return dialer.DialContext(ctx, "tcp4", address)
		}
		transport.TLSHandshakeTimeout = 30 * time.Second
		transport.ResponseHeaderTimeout = 45 * time.Second

		client := &http.Client{
			Transport: transport,
			Timeout:   75 * time.Second,
		}

		req, err := http.NewRequest(http.MethodGet, remoteURL, nil)
		if err != nil {
			http.Error(w, "chlorophyll contour request construction failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		req.Header.Set("User-Agent", "pittsburg-saildata/"+appVersion)

		resp, err := client.Do(req)
		if err != nil {
			http.Error(
				w,
				"NOAA CoastWatch chlorophyll numeric request failed: "+err.Error()+" | upstream="+remoteURL,
				http.StatusBadGateway,
			)
			return
		}
		body, readErr := ioutil.ReadAll(io.LimitReader(resp.Body, 24<<20))
		resp.Body.Close()
		if readErr != nil {
			http.Error(w, "NOAA chlorophyll numeric response read failed: "+readErr.Error(), http.StatusBadGateway)
			return
		}
		if resp.StatusCode != http.StatusOK {
			detail := strings.TrimSpace(string(body))
			if len(detail) > 1200 {
				detail = detail[:1200]
			}
			http.Error(
				w,
				fmt.Sprintf("NOAA chlorophyll numeric request returned HTTP %d: %s | upstream=%s", resp.StatusCode, detail, remoteURL),
				http.StatusBadGateway,
			)
			return
		}

		var payload struct {
			Table struct {
				ColumnNames []string        `json:"columnNames"`
				Rows        [][]interface{} `json:"rows"`
			} `json:"table"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			http.Error(w, "NOAA chlorophyll JSON decode failed: "+err.Error(), http.StatusBadGateway)
			return
		}
		if len(payload.Table.Rows) == 0 {
			http.Error(w, "NOAA chlorophyll numeric request returned no rows", http.StatusBadGateway)
			return
		}

		col := map[string]int{}
		for i, name := range payload.Table.ColumnNames {
			col[strings.ToLower(strings.TrimSpace(name))] = i
		}
		latCol, okLat := col["latitude"]
		lonCol, okLon := col["longitude"]
		valCol, okVal := col["chlor_a"]
		timeCol, okTime := col["time"]
		if !okLat || !okLon || !okVal {
			http.Error(w, "NOAA chlorophyll JSON is missing latitude/longitude/chlor_a columns", http.StatusBadGateway)
			return
		}

		asFloat := func(v interface{}) (float64, bool) {
			switch n := v.(type) {
			case float64:
				return n, true
			case string:
				f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
				return f, err == nil
			default:
				return 0, false
			}
		}

		type gridPoint struct {
			lat float64
			lon float64
			val float64
		}
		points := make([]gridPoint, 0, len(payload.Table.Rows))
		latSet := map[float64]struct{}{}
		lonSet := map[float64]struct{}{}
		servedTime := ""

		for _, row := range payload.Table.Rows {
			if latCol >= len(row) || lonCol >= len(row) || valCol >= len(row) {
				continue
			}
			lat, ok1 := asFloat(row[latCol])
			lon, ok2 := asFloat(row[lonCol])
			val, ok3 := asFloat(row[valCol])
			if !ok1 || !ok2 || !ok3 || math.IsNaN(val) || math.IsInf(val, 0) {
				continue
			}
			points = append(points, gridPoint{lat: lat, lon: lon, val: val})
			latSet[lat] = struct{}{}
			lonSet[lon] = struct{}{}
			if servedTime == "" && okTime && timeCol < len(row) {
				servedTime = fmt.Sprint(row[timeCol])
			}
		}
		if len(points) == 0 {
			http.Error(w, "NOAA chlorophyll numeric response contained no usable values", http.StatusBadGateway)
			return
		}

		lats := make([]float64, 0, len(latSet))
		for v := range latSet {
			lats = append(lats, v)
		}
		lons := make([]float64, 0, len(lonSet))
		for v := range lonSet {
			lons = append(lons, v)
		}
		sort.Float64s(lats)
		sort.Float64s(lons)
		if len(lats) < 2 || len(lons) < 2 {
			http.Error(w, "NOAA chlorophyll grid is too small for contours", http.StatusBadGateway)
			return
		}

		latIndex := make(map[float64]int, len(lats))
		for i, v := range lats {
			latIndex[v] = i
		}
		lonIndex := make(map[float64]int, len(lons))
		for i, v := range lons {
			lonIndex[v] = i
		}

		grid := make([][]float64, len(lats))
		for i := range grid {
			grid[i] = make([]float64, len(lons))
			for j := range grid[i] {
				grid[i][j] = math.NaN()
			}
		}
		for _, p := range points {
			grid[latIndex[p.lat]][lonIndex[p.lon]] = p.val
		}

		canvas := image.NewNRGBA(image.Rect(0, 0, width, height))

		type contourStyle struct {
			level float64
			halo  color.NRGBA
			core  color.NRGBA
			rHalo int
			rCore int
		}
		styles := []contourStyle{
			{level: 0.2, halo: color.NRGBA{R: 8, G: 40, B: 56, A: 235}, core: color.NRGBA{R: 99, G: 230, B: 255, A: 255}, rHalo: 2, rCore: 1},
			{level: 0.3, halo: color.NRGBA{R: 8, G: 40, B: 56, A: 245}, core: color.NRGBA{R: 255, G: 253, B: 231, A: 255}, rHalo: 3, rCore: 2},
			{level: 0.5, halo: color.NRGBA{R: 8, G: 40, B: 56, A: 235}, core: color.NRGBA{R: 255, G: 209, B: 102, A: 255}, rHalo: 2, rCore: 1},
		}

		putDisc := func(cx, cy, radius int, c color.NRGBA) {
			for dy := -radius; dy <= radius; dy++ {
				for dx := -radius; dx <= radius; dx++ {
					if dx*dx+dy*dy > radius*radius {
						continue
					}
					x := cx + dx
					y := cy + dy
					if x < 0 || x >= width || y < 0 || y >= height {
						continue
					}
					canvas.SetNRGBA(x, y, c)
				}
			}
		}
		drawLine := func(x0, y0, x1, y1 float64, radius int, c color.NRGBA) {
			dx := math.Abs(x1 - x0)
			dy := math.Abs(y1 - y0)
			steps := int(math.Ceil(math.Max(dx, dy)))
			if steps < 1 {
				putDisc(int(math.Round(x0)), int(math.Round(y0)), radius, c)
				return
			}
			for step := 0; step <= steps; step++ {
				t := float64(step) / float64(steps)
				x := int(math.Round(x0 + (x1-x0)*t))
				y := int(math.Round(y0 + (y1-y0)*t))
				putDisc(x, y, radius, c)
			}
		}
		toPixel := func(lat, lon float64) (float64, float64) {
			x := (lon - west) / (east - west) * float64(width-1)
			y := (north - lat) / (north - south) * float64(height-1)
			return x, y
		}
		interp := func(a, b, level float64) float64 {
			if a == b {
				return 0.5
			}
			t := (level - a) / (b - a)
			if t < 0 {
				t = 0
			}
			if t > 1 {
				t = 1
			}
			return t
		}

		type xy struct{ x, y float64 }
		drawContour := func(style contourStyle) {
			level := style.level
			for iy := 0; iy < len(lats)-1; iy++ {
				for ix := 0; ix < len(lons)-1; ix++ {
					v00 := grid[iy][ix]
					v10 := grid[iy][ix+1]
					v11 := grid[iy+1][ix+1]
					v01 := grid[iy+1][ix]
					if math.IsNaN(v00) || math.IsNaN(v10) || math.IsNaN(v11) || math.IsNaN(v01) {
						continue
					}

					var crossings []xy
					// Bottom edge: (lat[iy], lon[ix]) -> (lat[iy], lon[ix+1])
					if (v00 < level) != (v10 < level) {
						t := interp(v00, v10, level)
						lat := lats[iy]
						lon := lons[ix] + (lons[ix+1]-lons[ix])*t
						x, y := toPixel(lat, lon)
						crossings = append(crossings, xy{x, y})
					}
					// Right edge.
					if (v10 < level) != (v11 < level) {
						t := interp(v10, v11, level)
						lat := lats[iy] + (lats[iy+1]-lats[iy])*t
						lon := lons[ix+1]
						x, y := toPixel(lat, lon)
						crossings = append(crossings, xy{x, y})
					}
					// Top edge.
					if (v01 < level) != (v11 < level) {
						t := interp(v01, v11, level)
						lat := lats[iy+1]
						lon := lons[ix] + (lons[ix+1]-lons[ix])*t
						x, y := toPixel(lat, lon)
						crossings = append(crossings, xy{x, y})
					}
					// Left edge.
					if (v00 < level) != (v01 < level) {
						t := interp(v00, v01, level)
						lat := lats[iy] + (lats[iy+1]-lats[iy])*t
						lon := lons[ix]
						x, y := toPixel(lat, lon)
						crossings = append(crossings, xy{x, y})
					}

					if len(crossings) == 2 {
						drawLine(crossings[0].x, crossings[0].y, crossings[1].x, crossings[1].y, style.rHalo, style.halo)
						drawLine(crossings[0].x, crossings[0].y, crossings[1].x, crossings[1].y, style.rCore, style.core)
					} else if len(crossings) == 4 {
						center := (v00 + v10 + v11 + v01) / 4
						if center >= level {
							drawLine(crossings[0].x, crossings[0].y, crossings[3].x, crossings[3].y, style.rHalo, style.halo)
							drawLine(crossings[0].x, crossings[0].y, crossings[3].x, crossings[3].y, style.rCore, style.core)
							drawLine(crossings[1].x, crossings[1].y, crossings[2].x, crossings[2].y, style.rHalo, style.halo)
							drawLine(crossings[1].x, crossings[1].y, crossings[2].x, crossings[2].y, style.rCore, style.core)
						} else {
							drawLine(crossings[0].x, crossings[0].y, crossings[1].x, crossings[1].y, style.rHalo, style.halo)
							drawLine(crossings[0].x, crossings[0].y, crossings[1].x, crossings[1].y, style.rCore, style.core)
							drawLine(crossings[2].x, crossings[2].y, crossings[3].x, crossings[3].y, style.rHalo, style.halo)
							drawLine(crossings[2].x, crossings[2].y, crossings[3].x, crossings[3].y, style.rCore, style.core)
						}
					}
				}
			}
		}

		for _, style := range styles {
			drawContour(style)
		}

		var encoded bytes.Buffer
		if err := png.Encode(&encoded, canvas); err != nil {
			http.Error(w, "chlorophyll contour PNG encoding failed: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("X-Chlorophyll-Upstream", "CoastWatch Central ERDDAP · numeric DINEOF")
		if servedTime != "" {
			w.Header().Set("X-Chlorophyll-Time", servedTime)
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "public, max-age=900")
		_, _ = w.Write(encoded.Bytes())
	})

	mux.HandleFunc("/fishing-water", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		lat, lon, ok, err := parseOptionalLatLon(r.URL.Query())
		if err != nil || !ok {
			if err == nil {
				err = fmt.Errorf("lat and lon are required")
			}
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Roughly a 15 nmi neighborhood around the selected destination.
		halfLat := 0.25
		cosLat := math.Max(0.25, math.Cos(lat*math.Pi/180))
		halfLon := math.Min(0.50, 0.25/cosLat)
		south := math.Max(-89.8, lat-halfLat)
		north := math.Min(89.8, lat+halfLat)
		west := math.Max(-179.8, lon-halfLon)
		east := math.Min(179.8, lon+halfLon)

		sstRequest := fmt.Sprintf(
			"analysed_sst[last][(%.6f):(%.6f)][(%.6f):(%.6f)]",
			south, north, west, east,
		)
		chlRequest := fmt.Sprintf(
			"chlor_a[last][0][(%.6f):(%.6f)][(%.6f):(%.6f)]",
			south, north, west, east,
		)

		sstStats, sstErr := fetchCoastWatchWindowStats(
			"noaacwBLENDEDsstDNDaily",
			"analysed_sst",
			sstRequest,
			lat,
			lon,
		)
		chlStats, chlErr := fetchCoastWatchWindowStats(
			"noaacwNPPN20S3ASCIDINEOF2kmDaily",
			"chlor_a",
			chlRequest,
			lat,
			lon,
		)

		type sstPayload struct {
			PointF  float64 `json:"point_f,omitempty"`
			MinF    float64 `json:"min_f,omitempty"`
			MaxF    float64 `json:"max_f,omitempty"`
			SpreadF float64 `json:"spread_f,omitempty"`
			Count   int     `json:"count,omitempty"`
		}
		type chlPayload struct {
			Point float64 `json:"point_mg_m3,omitempty"`
			Min   float64 `json:"min_mg_m3,omitempty"`
			Max   float64 `json:"max_mg_m3,omitempty"`
			Count int     `json:"count,omitempty"`
		}
		payload := struct {
			SST         *sstPayload `json:"sst,omitempty"`
			Chlorophyll *chlPayload `json:"chlorophyll,omitempty"`
			Error       string      `json:"error,omitempty"`
		}{}

		var errs []string
		if sstErr != nil {
			errs = append(errs, "Sea Surface Temp: "+sstErr.Error())
		} else {
			toF := func(c float64) float64 { return c*9/5 + 32 }
			payload.SST = &sstPayload{
				PointF:  toF(sstStats.Point),
				MinF:    toF(sstStats.Min),
				MaxF:    toF(sstStats.Max),
				SpreadF: (sstStats.Max - sstStats.Min) * 9 / 5,
				Count:   sstStats.Count,
			}
		}
		if chlErr != nil {
			errs = append(errs, "Chlorophyll: "+chlErr.Error())
		} else {
			payload.Chlorophyll = &chlPayload{
				Point: chlStats.Point,
				Min:   chlStats.Min,
				Max:   chlStats.Max,
				Count: chlStats.Count,
			}
		}
		if len(errs) > 0 {
			payload.Error = strings.Join(errs, " | ")
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if err := json.NewEncoder(w).Encode(payload); err != nil {
			fmt.Println("fishing-water JSON encoding error:", err)
		}
	})

	mux.HandleFunc("/offshore-trip", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		lat, lon, ok, err := parseOptionalLatLon(r.URL.Query())
		if err != nil || !ok {
			if err == nil {
				err = fmt.Errorf("lat and lon are required")
			}
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		zone, updated, periods, alerts, forecastErr :=
			fetchMarineForecastForPoint(lat, lon, loc)
		zoneName := fetchNWSForecastZoneName(zone)
		isOffshore := isOffshoreTripZone(zone, zoneName)

		payload := struct {
			IsOffshore bool                       `json:"is_offshore"`
			Zone       string                     `json:"zone,omitempty"`
			ZoneName   string                     `json:"zone_name,omitempty"`
			Updated    string                     `json:"updated,omitempty"`
			Periods    []htmlMarineForecastPeriod `json:"periods,omitempty"`
			Alerts     []string                   `json:"alerts,omitempty"`
			Buoy       *offshoreBuoySnapshot      `json:"buoy,omitempty"`
			Error      string                     `json:"error,omitempty"`
		}{
			IsOffshore: isOffshore,
			Zone:       zone,
			ZoneName:   zoneName,
			Updated:    updated,
			Periods:    periods,
			Alerts:     alerts,
		}

		// Do not perform offshore buoy work for land, Delta, SF Bay, or other
		// non-offshore selections. The browser will keep the entire card hidden.
		if isOffshore {
			buoy, buoyErr := fetchNearestOffshoreBuoySnapshot(lat, lon)
			payload.Buoy = buoy

			var errs []string
			if forecastErr != nil {
				errs = append(errs, "NWS marine forecast: "+forecastErr.Error())
			}
			if buoyErr != nil {
				errs = append(errs, "NDBC observation: "+buoyErr.Error())
			}
			if len(errs) > 0 {
				payload.Error = strings.Join(errs, " | ")
			}
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if err := json.NewEncoder(w).Encode(payload); err != nil {
			fmt.Println("offshore-trip JSON encoding error:", err)
		}
	})

	mux.HandleFunc("/marine-forecast", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		lat, lon, ok, err := parseOptionalLatLon(r.URL.Query())
		if err != nil || !ok {
			if err == nil {
				err = fmt.Errorf("lat and lon are required")
			}
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		zone, updated, periods, alerts, forecastErr :=
			fetchMarineForecastForPoint(lat, lon, loc)
		weather, weatherErr := fetchPointWeatherForPoint(lat, lon, loc)
		if weatherErr != nil {
			weather.Error = weatherErr.Error()
		}

		var geometry json.RawMessage
		if zone != "" {
			if encoded := fetchMarineZoneGeometry(zone); encoded != "" {
				geometry = json.RawMessage(string(encoded))
			}
		}

		payload := struct {
			Zone     string                     `json:"zone"`
			Updated  string                     `json:"updated,omitempty"`
			Periods  []htmlMarineForecastPeriod `json:"periods,omitempty"`
			Alerts   []string                   `json:"alerts,omitempty"`
			Geometry json.RawMessage            `json:"geometry,omitempty"`
			Weather  htmlPointWeather           `json:"weather"`
			Error    string                     `json:"error,omitempty"`
		}{
			Zone:     zone,
			Updated:  updated,
			Periods:  periods,
			Alerts:   alerts,
			Geometry: geometry,
			Weather:  weather,
		}
		if forecastErr != nil {
			payload.Error = forecastErr.Error()
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if err := json.NewEncoder(w).Encode(payload); err != nil {
			fmt.Println("marine-forecast JSON encoding error:", err)
		}
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
				Distance: fmt.Sprintf("%.1f nmi", c.SelectedDistance),
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
				item.CurrentDistance = fmt.Sprintf("%.1f nmi", currentPreview.DistanceNM)
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

func fetchNOAAHMSSmoke() (string, *hmsSmokeFeatureCollection, error) {
	// Use NOAA's dated HMS archive directly. The general "current" KML URL
	// is not consistently accessible to server-side clients, while the dated
	// archive is the documented persistent product location.
	now := time.Now().UTC()
	for daysBack := 0; daysBack <= 7; daysBack++ {
		day := now.AddDate(0, 0, -daysBack)
		dateText := day.Format("20060102")
		sourceURL := fmt.Sprintf(
			"https://satepsanone.nesdis.noaa.gov/pub/FIRE/web/HMS/Smoke_Polygons/KML/%s/%s/hms_smoke%s.kml",
			day.Format("2006"),
			day.Format("01"),
			dateText,
		)

		body, _, err := fetchNOAAHMSKML(sourceURL)
		if err != nil {
			continue
		}
		collection, err := parseNOAAHMSSmokeKML(body)
		if err != nil || collection == nil || len(collection.Features) == 0 {
			continue
		}
		return day.Format("Jan 2, 2006"), collection, nil
	}

	return "", nil, fmt.Errorf(
		"NOAA HMS satellite smoke analysis is temporarily unavailable; no recent dated analysis could be loaded.",
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
				Distance:   fmt.Sprintf("%.1f nmi", candidate.DistanceNM),
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
				item.CurrentDistance = fmt.Sprintf("%.1f nmi", currentPreview.DistanceNM)
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
			labelY := baseY - 10
			capHalfWidth := 5.0
			fmt.Fprintf(&svg, `<g class="tide-range-marker %s"><title>%s tidal range · %.1f ft · %s</title><line class="tide-range-bar %s" x1="%.2f" y1="%.2f" x2="%.2f" y2="%.2f"/><line class="tide-range-bar %s" x1="%.2f" y1="%.2f" x2="%.2f" y2="%.2f"/></g>`,
				className, template.HTMLEscapeString(day.Format("Mon Jan 2")), value, label,
				className, x, baseY, x, y,
				className, x-capHalfWidth, y, x+capHalfWidth, y)
			fmt.Fprintf(&svg, `<text class="tide-range-value %s" x="%.2f" y="%.2f">%.1f</text>`,
				className, x, labelY, value)
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

var welcomeHTMLTemplate = template.Must(template.New("welcome").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="description" content="Mauri's Weather & Water Conditions — current wind observations, predicted tidal currents, NWS local forecast context, and planning maps for sailors and other people on the water.">
<meta property="og:title" content="Mauri's Weather & Water Conditions">
<meta property="og:description" content="See Conditions Now, then use Planning and Details for location weather, wind and current stations, forecasts, and map overlays.">
<meta property="og:type" content="website">
<meta property="og:url" content="https://pittsburg-saildata.onrender.com/welcome">
<meta property="og:image" content="https://pittsburg-saildata.onrender.com/assets/hero.jpg">
<meta property="og:image:alt" content="Sailing on the supported coastal and inland waters">
<meta name="twitter:card" content="summary_large_image">
<meta name="twitter:title" content="Mauri's Weather & Water Conditions">
<meta name="twitter:description" content="See Conditions Now, then use Planning and Details for location weather, wind and current stations, forecasts, and map overlays.">
<meta name="twitter:image" content="https://pittsburg-saildata.onrender.com/assets/hero.jpg">
<title>Mauri's Weather & Water Conditions — Welcome</title>
<style>
:root{--navy:#082b45;--blue:#126b91;--sea:#0b8793;--ink:#153242;--muted:#607886;--paper:#f5fafc;--card:#fff;--line:#d8e7ed;--shadow:0 12px 34px rgba(8,43,69,.10)}
*{box-sizing:border-box}
body{margin:0;background:linear-gradient(180deg,#dff3f8,#f7fbfc 32rem);color:var(--ink);font-family:-apple-system,BlinkMacSystemFont,"Segoe UI","Avenir Next",Avenir,Helvetica,Arial,sans-serif;line-height:1.55}
.shell{max-width:900px;margin:auto;padding:28px 18px 64px}
.hero{color:#fff;padding:34px 30px 30px;border-radius:24px;min-height:390px;display:flex;flex-direction:column;justify-content:flex-end;background:
linear-gradient(180deg,rgba(4,24,38,.05) 10%,rgba(4,24,38,.28) 48%,rgba(4,24,38,.88) 100%),
url('/assets/hero.jpg') center 48%/cover no-repeat;box-shadow:var(--shadow);text-shadow:0 2px 12px rgba(0,0,0,.45)}
.eyebrow{text-transform:uppercase;letter-spacing:.14em;font-weight:800;font-size:.76rem;opacity:.84}
.hero h1{font-size:clamp(2rem,6vw,3.5rem);line-height:1.02;margin:.35rem 0 .6rem}
.hero p{max-width:650px;font-size:1.05rem;margin:0 0 18px}
.cta-row{display:flex;gap:10px;flex-wrap:wrap}
.cta{display:inline-block;text-decoration:none;font-weight:850;border-radius:999px;padding:11px 17px}
.cta.primary{background:#fff;color:var(--navy)}
.cta.secondary{border:1px solid rgba(255,255,255,.72);color:#fff;background:rgba(255,255,255,.08)}
.grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:16px;margin-top:18px}
.card{background:var(--card);border:1px solid var(--line);border-radius:18px;padding:22px;box-shadow:var(--shadow)}
.card.full{grid-column:1/-1}
h2{margin:.1rem 0 .7rem;color:var(--navy);font-size:1.28rem}
h3{margin:1.2rem 0 .35rem;color:var(--navy)}
.quick ol{padding-left:1.25rem}
.qa details{border-top:1px solid var(--line);padding:12px 0}
.qa details:first-of-type{border-top:0}
.qa summary{cursor:pointer;font-weight:800;color:var(--navy)}
.qa p{margin:.65rem 0 0}
.note{color:var(--muted)}
.github-actions{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:10px;margin-top:12px}
.github-actions a{display:block;text-decoration:none;border:1px solid var(--line);border-radius:14px;padding:12px 14px;color:var(--blue);font-weight:800;background:#fbfdfe}
.footer{margin-top:18px;text-align:center;color:var(--muted);font-size:.88rem}.hero .yogiism{margin:16px 0 0;max-width:720px;color:#fff;font-style:italic;font-size:.96rem;line-height:1.4;opacity:.96}
@media(max-width:680px){.grid{grid-template-columns:1fr}.github-actions{grid-template-columns:1fr}.hero{min-height:340px;padding:26px 22px}}
</style>
</head>
<body>
<main class="shell">
<section class="hero">
<div class="eyebrow">Mauri's Weather & Water Conditions</div>
<h1>Check conditions now. Plan with the details.</h1>
<p>A free planning dashboard for sailors, paddlers, and other people on the water. Start with current wind and the one-day current outlook, then open Planning and Details for location weather, station choices, forecasts, current timing, maps, and overlays.</p>
<div class="cta-row">
<a class="cta primary" href="/report?format=html">Open Conditions Now</a>
<a class="cta secondary" href="https://github.com/richard-mauri/pittsburg-saildata">View on GitHub</a>
</div>
{{if .Yogiism}}<div class="yogiism">“{{.Yogiism}}” — Yogi Berra</div>{{end}}
</section>

<div class="grid">
<section class="card quick">
<h2>The 30-second version</h2>
<ol>
<li>Open <strong>Conditions Now</strong> for the latest wind observation and one-day current outlook.</li>
<li>Open <strong>Planning and Details</strong> when you want to choose a location, compare stations, or customize the planning view.</li>
<li>Click the map to set a ★ selected sailing location.</li>
<li>Review <strong>Local Conditions</strong>, nearby wind-station candidates, current timing, forecast context, and map layers.</li>
<li>Return to <strong>Conditions Now</strong> with your selected station/location state preserved.</li>
</ol>
<p class="note">You do not need to know buoy IDs, NOAA station numbers, or coordinates. The map and station tools can resolve the nearby sources for you.</p>
</section>

<section class="card">
<h2>What it combines</h2>
<p><strong>Wind:</strong> recent NOAA/NDBC observations, wind history, and nearby station alternatives.</p>
<p><strong>Currents:</strong> NOAA CO-OPS ebb, flood, slack, maximum-current timing, and 1/3/7-day current graphs.</p>
<p><strong>Weather:</strong> NWS local conditions/forecast context and forecast-zone information for a selected map point.</p>
<p><strong>Map context:</strong> street, nautical, satellite, and hybrid basemaps plus forecast-zone, smoke, sea-surface-temperature, cloud-cover, and radar overlays.</p>
</section>

<section class="card full qa">
<h2>Questions people on the water will probably ask</h2>
<details open><summary>What is Conditions Now?</summary><p>The streamlined landing page shows the latest selected wind observation, its observation age, and a one-day predicted tidal-current graph. It is meant to answer the first question quickly before you open the full planning dashboard.</p></details>
<details><summary>What is Planning and Details?</summary><p>It is the full dashboard for choosing a sailing location, comparing nearby wind stations, inspecting the associated currents station, viewing wind history, changing current-planning thresholds, browsing 1/3/7-day current ranges, and using forecast/map tools.</p></details>
<details><summary>What does selecting a location do?</summary><p>Clicking the map sets a ★ selected sailing location. That selected point is separate from the map viewport center. It drives nearby-station discovery and the Local Conditions panel; simply panning or using Center Map does not silently change the selected sailing location.</p></details>
<details><summary>What does Local Conditions show?</summary><p>For the selected map point, the app uses the NWS point forecast to show the nearby city/state label, current-hour forecast temperature, expected high/low, and a short forecast phrase.</p></details>
<details><summary>Can I choose another wind station?</summary><p>Yes. Nearby wind-station candidates appear after you select a location. You can compare them on the map/table and choose the station you think best represents the water you care about.</p></details>
<details><summary>Is this tide data or current data?</summary><p><strong>Current data.</strong> The graph is predicted speed and direction of moving water — flood above zero, ebb below zero, and crossings near slack. Tide height and current are related, but they are not the same thing.</p></details>
<details><summary>What map choices are available?</summary><p>Planning and Details includes Street Map, Nautical Chart, Satellite, and Hybrid basemaps; independent forecast-zone, satellite-smoke, NOAA CoastWatch Sea Surface Temp, NOAA/NESDIS cloud-cover, and NEXRAD radar overlays; and Center Map actions for your location, entered coordinates, selected location, wind station, and currents station.</p></details>
<details><summary>Is this for navigation or safety decisions?</summary><p>No. It is a conditions-planning and exploration tool. Observations can be delayed or missing, station exposure differs, and current/forecast products have limitations. Use official marine forecasts, charts, notices, local knowledge, and prudent seamanship.</p></details>
</section>

<section class="card full">
<h2>Know these waters? Your feedback is useful.</h2>
<p>If something looks questionable, that's worth reporting. Examples: a wind station that doesn't represent Alameda well, confusing current wording, an unexpectedly weak or strong ebb, or a feature that would make the report more useful.</p>
<p>You do not need to be a programmer to contribute useful local knowledge.</p>
</section>

<section class="card full">
<h2>Follow or help improve the project</h2>
<p>The project is open source:</p>
<p><strong><a href="https://github.com/richard-mauri/pittsburg-saildata">github.com/richard-mauri/pittsburg-saildata</a></strong></p>
<div class="github-actions">
<a href="https://github.com/richard-mauri/pittsburg-saildata">⭐ Star the repository</a>
<a href="https://github.com/richard-mauri/pittsburg-saildata/subscription">👀 Watch project activity</a>
<a href="https://github.com/richard-mauri/pittsburg-saildata/issues">💡 Open an Issue</a>
<a href="https://github.com/richard-mauri/pittsburg-saildata/pulls">🔧 View / submit Pull Requests</a>
</div>
<p class="note">Issues are not just for software bugs. Local conditions knowledge, terminology, station-selection concerns, and feature ideas are all useful.</p>
</section>

<section class="card full">
<h2>Want the geeky version?</h2>
<p>The Go service combines live NDBC observations, NOAA CO-OPS current predictions, NWS point/zone forecast context, and browser map state. It caches active station metadata, computes geographic distance, probes nearby candidates concurrently for usable wind, and keeps the selected sailing location separate from the map viewport. The same service also exposes text and JSON output for scripts and integrations.</p>
</section>
</div>

<div class="footer">Mauri's Weather & Water Conditions · Conditions-planning utility, not a navigation system.<br>Version {{.AppVersion}} · Build {{.BuildVersion}}</div>
</main>
</body>
</html>`))

var sailingStationsHTMLTemplate = template.Must(template.New("stations").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Nearby Wind Stations — Mauri's Weather & Water Conditions</title>
<link rel="stylesheet" href="https://unpkg.com/leaflet@1.9.4/dist/leaflet.css" crossorigin="">
<script src="https://unpkg.com/leaflet@1.9.4/dist/leaflet.js" crossorigin=""></script>
<style>
:root{--navy:#082b45;--blue:#126b91;--ink:#153242;--muted:#607886;--paper:#f5fafc;--card:#fff;--line:#d8e7ed;--good:#16805f}
*{box-sizing:border-box}body{margin:0;background:var(--paper);color:var(--ink);font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;line-height:1.45}
main{max-width:1050px;margin:0 auto;padding:24px 16px 48px}.back{display:inline-block;margin-bottom:16px;text-decoration:none;color:var(--blue);font-weight:850}
h1{margin:0 0 4px;color:var(--navy);font-size:clamp(1.6rem,4vw,2.3rem)}.intro{color:var(--muted);margin:0 0 18px}
.card{background:#fff;border:1px solid var(--line);border-radius:16px;padding:18px;margin-bottom:16px}.map{height:430px;border-radius:13px;overflow:hidden}
table{width:100%;border-collapse:collapse;font-size:.9rem}th,td{text-align:left;padding:10px 8px;border-bottom:1px solid var(--line);vertical-align:top}th{color:var(--muted);font-size:.72rem;text-transform:uppercase;letter-spacing:.06em}.num{text-align:right;white-space:nowrap}.scroll{overflow-x:auto}
a.station{color:var(--blue);font-weight:800;text-decoration:none}.badge{display:inline-block;border-radius:999px;padding:2px 7px;font-size:.68rem;font-weight:900;margin-right:4px}.auto{background:#e6eef8;color:#24538a}.selected{background:#e4f3ed;color:#146146}
.actions{display:flex;gap:10px;flex-wrap:wrap;margin-top:14px}.button{display:inline-block;text-decoration:none;border:1px solid var(--line);border-radius:999px;padding:8px 12px;color:var(--blue);font-weight:850;background:#fff}.yogiism{text-align:center;color:var(--muted);font-style:italic;font-size:.9rem;margin:18px auto 0;max-width:760px}
@media(max-width:650px){.map{height:360px}th,td{padding:9px 6px}}
</style></head><body><main>
<a class="back" href="javascript:history.back()">← Back to conditions</a>
<h1>Nearby Wind Stations</h1>
<p class="intro">These are the candidate observation stations for the selected location. Distances are measured from that selected location. Use the map or table to choose the station you think best represents the water you care about.</p>
<div class="card"><div id="station-map" class="map" aria-label="Nearby wind station candidates"></div></div>
<div class="card">
{{if .UseNearestURL}}<div class="actions"><a class="button" href="{{.UseNearestURL}}">Use nearest usable station</a></div>{{end}}
<div class="scroll"><table><thead><tr><th>#</th><th>Station</th><th>Name</th><th>State</th><th class="num">From Selected Location</th>{{if .DebugWind}}<th>Status</th><th>Reason</th>{{end}}</tr></thead><tbody>
{{range .WindCandidates}}<tr><td>{{.Rank}}</td><td><a class="station" href="{{.URL}}">{{.Station}}</a></td><td><a class="station" href="{{.URL}}">{{.Name}}</a></td><td>{{if .IsAuto}}<span class="badge auto">AUTO</span>{{end}}{{if .IsSelected}}<span class="badge selected">SELECTED</span>{{end}}</td><td class="num">{{.Distance}}</td>{{if $.DebugWind}}<td>{{.Status}}</td><td>{{.Reason}}</td>{{end}}</tr>{{else}}<tr><td colspan="5">No nearby station candidates are available.</td></tr>{{end}}
</tbody></table></div></div>
{{if .Yogiism}}<div class="yogiism">“{{.Yogiism}}” — Yogi Berra</div>{{end}}
</main>
<script>
(function(){
  var el=document.getElementById("station-map");
  if(!el||typeof L==="undefined") return;
  var initialZoom=Number(new URL(window.location.href).searchParams.get("map_zoom"));
  if(!Number.isFinite(initialZoom)||initialZoom<3||initialZoom>18)initialZoom=10;
  var map=L.map(el,{scrollWheelZoom:true}).setView([{{printf "%.6f" .MapCenterLat}},{{printf "%.6f" .MapCenterLon}}],initialZoom);
  var streetLayer=L.tileLayer("https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png",{maxZoom:18,attribution:"&copy; OpenStreetMap contributors"});
  var nauticalLayer=L.tileLayer.wms("https://gis.charttools.noaa.gov/arcgis/rest/services/MCS/NOAAChartDisplay/MapServer/exts/MaritimeChartService/WMSServer",{
    layers:"0,1,2,3,4,5,6,7,8,9,10,11,12",
    format:"image/png",
    transparent:false,
    version:"1.3.0",
    attribution:"NOAA Office of Coast Survey"
  });
  var satelliteLayer=L.tileLayer("https://server.arcgisonline.com/ArcGIS/rest/services/World_Imagery/MapServer/tile/{z}/{y}/{x}",{
    maxZoom:19,
    attribution:"Tiles &copy; Esri"
  });
  var hybridImageryLayer=L.tileLayer("https://server.arcgisonline.com/ArcGIS/rest/services/World_Imagery/MapServer/tile/{z}/{y}/{x}",{
    maxZoom:19,
    attribution:"Tiles &copy; Esri"
  });
  var hybridReferenceLayer=L.tileLayer("https://server.arcgisonline.com/ArcGIS/rest/services/Reference/World_Boundaries_and_Places/MapServer/tile/{z}/{y}/{x}",{
    maxZoom:19,
    attribution:"Reference &copy; Esri",
    pane:"overlayPane"
  });
  var hybridLayer=L.layerGroup([hybridImageryLayer,hybridReferenceLayer]);
  var mapLayerParam=new URL(window.location.href).searchParams.get("map_layer");
  var initialBaseLayer=streetLayer;
  if(mapLayerParam==="nautical")initialBaseLayer=nauticalLayer;
  else if(mapLayerParam==="satellite")initialBaseLayer=satelliteLayer;
  else if(mapLayerParam==="hybrid")initialBaseLayer=hybridLayer;
  initialBaseLayer.addTo(map);
  L.control.layers({"Street Map":streetLayer,"Nautical Chart":nauticalLayer,"Satellite":satelliteLayer,"Hybrid":hybridLayer},null,{collapsed:true,position:"topright"}).addTo(map);
  map.on("baselayerchange",function(e){
    var target=new URL(window.location.href);
    if(e.layer===nauticalLayer)target.searchParams.set("map_layer","nautical");
    else if(e.layer===satelliteLayer)target.searchParams.set("map_layer","satellite");
    else if(e.layer===hybridLayer)target.searchParams.set("map_layer","hybrid");
    else target.searchParams.delete("map_layer");
    window.history.replaceState({},"",target.pathname+"?"+target.searchParams.toString()+target.hash);
  });
  var pts=[];
  function add(lat,lon,label,fill,radius,url){
    var m=L.circleMarker([lat,lon],{radius:radius,color:"#fff",weight:2,fillColor:fill,fillOpacity:1}).addTo(map);
    if(url){
      m.bindTooltip(label+"<br><strong>Click to use this station</strong>",{direction:"top",opacity:.96});
      m.on("click",function(e){if(e&&e.originalEvent)L.DomEvent.stopPropagation(e.originalEvent);window.location.assign(url);});
    }else{m.bindPopup(label);}
    pts.push([lat,lon]);
  }
  {{if .MapHasRequest}}add({{printf "%.6f" .MapRequestLat}},{{printf "%.6f" .MapRequestLon}},"Selected location","#126b91",9,"");{{end}}
  {{range .WindCandidates}}add({{printf "%.6f" .Lat}},{{printf "%.6f" .Lon}},{{printf "%q" .Station}}+" — "+{{printf "%q" .Name}}+"<br>"+{{printf "%q" .Distance}}+" from selected location",{{if .IsSelected}}"#16805f"{{else if .IsAuto}}"#24538a"{{else}}"#718794"{{end}},{{if .IsSelected}}9{{else if .IsAuto}}8{{else}}7{{end}},{{.JSURL}});{{end}}
  {{if .MapHasRequest}}map.setView([{{printf "%.6f" .MapRequestLat}},{{printf "%.6f" .MapRequestLon}}],initialZoom);{{else if .WindCandidates}}if(pts.length>1)map.fitBounds(pts,{padding:[35,35],maxZoom:10});{{end}}
})();
</script></body></html>`))

var sailingDetailsHTMLTemplate = template.Must(template.New("details").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Full report details — Mauri's Weather & Water Conditions</title>
<style>:root{--navy:#082b45;--blue:#126b91;--ink:#153242;--muted:#607886;--paper:#f5fafc;--card:#fff;--line:#d8e7ed}*{box-sizing:border-box}body{margin:0;background:var(--paper);color:var(--ink);font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}main{max-width:1000px;margin:0 auto;padding:24px 16px 48px}a{color:var(--blue)}.back{display:inline-block;margin-bottom:18px;text-decoration:none;font-weight:800}.card{background:var(--card);border:1px solid var(--line);border-radius:16px;padding:20px}h1{margin:0 0 6px;color:var(--navy);font-size:clamp(1.5rem,4vw,2.2rem)}.meta{color:var(--muted);margin-bottom:18px}.yogiism{text-align:center;color:var(--muted);font-style:italic;font-size:.9rem;margin:18px auto 0;max-width:760px}pre{white-space:pre-wrap;overflow-wrap:anywhere;margin:0;font:14px/1.55 ui-monospace,SFMono-Regular,Menlo,Monaco,Consolas,monospace}</style>
</head><body><main><a class="back" href="javascript:history.back()">← Back to conditions</a><div class="card"><h1>Full report details</h1><div class="meta">{{.Title}}{{if .ReportTime}} · {{.ReportTime}}{{end}} · Version {{.AppVersion}} · Build {{.BuildVersion}}</div><pre>{{.FullText}}</pre></div>{{if .Yogiism}}<div class="yogiism">“{{.Yogiism}}” — Yogi Berra</div>{{end}}</main></body></html>`))

var sailingHTMLTemplate = template.Must(template.New("sailing").Parse(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="description" content="Wind observations and predicted currents for supported waters, presented for people planning time on the water.">
<meta property="og:title" content="Mauri's Weather & Water Conditions">
<meta property="og:description" content="Wind observations and predicted currents for supported waters, presented for people planning time on the water.">
<meta property="og:type" content="website">
<meta property="og:url" content="https://pittsburg-saildata.onrender.com/">
<meta property="og:image" content="https://pittsburg-saildata.onrender.com/assets/hero.jpg">
<meta property="og:image:alt" content="Sailing on the supported coastal and inland waters">
<meta name="twitter:card" content="summary_large_image">
<meta name="twitter:title" content="Mauri's Weather & Water Conditions">
<meta name="twitter:description" content="Wind observations and predicted currents for supported waters, presented for people planning time on the water.">
<meta name="twitter:image" content="https://pittsburg-saildata.onrender.com/assets/hero.jpg">
<title>Mauri's Weather & Water Conditions — {{.Title}}</title>
<link rel="stylesheet" href="https://unpkg.com/leaflet@1.9.4/dist/leaflet.css" crossorigin="">
<script src="https://unpkg.com/leaflet@1.9.4/dist/leaflet.js" crossorigin=""></script>
<style>:root{--navy:#082b45;--blue:#126b91;--sea:#0b8793;--ink:#153242;--muted:#607886;--paper:#f5fafc;--card:#fff;--line:#d8e7ed;--flood:#087f8c;--ebb:#365f91;--slack:#756d64;--shadow:0 12px 34px rgba(8,43,69,.10)}*{box-sizing:border-box}body{margin:0;background:linear-gradient(180deg,#dff3f8,#f7fbfc 32rem);color:var(--ink);font-family:-apple-system,BlinkMacSystemFont,"Segoe UI","Avenir Next",Avenir,Helvetica,Arial,sans-serif;line-height:1.45}.shell{max-width:880px;margin:auto;padding:28px 18px 64px}.hero{color:#fff;padding:34px 30px 30px;border-radius:24px;min-height:360px;display:flex;flex-direction:column;justify-content:flex-end;background:
linear-gradient(180deg,rgba(4,24,38,.06) 12%,rgba(4,24,38,.24) 48%,rgba(4,24,38,.86) 100%),
url('/assets/hero.jpg') center 48%/cover no-repeat;box-shadow:var(--shadow);text-shadow:0 2px 12px rgba(0,0,0,.45)}.eyebrow{text-transform:uppercase;letter-spacing:.14em;font-weight:800;font-size:.76rem;opacity:.8}.photo-tag{margin-top:14px;font-size:.72rem;letter-spacing:.12em;text-transform:uppercase;opacity:.72}h1{font-size:clamp(1.8rem,6vw,3.2rem);line-height:1.05;margin:.4rem 0 .6rem;letter-spacing:-.035em}.sub{opacity:.82}.grid{display:grid;grid-template-columns:1fr 1fr;gap:18px;margin-top:18px}.card{background:var(--card);border:1px solid var(--line);border-radius:20px;padding:22px;box-shadow:var(--shadow)}.full{grid-column:1/-1}h2{font-size:.82rem;letter-spacing:.13em;text-transform:uppercase;color:var(--blue);margin:0 0 16px}.bottom{font-size:1.13rem}.metrics{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:10px}.wind-card .metric{min-width:0}.wind-card .value{white-space:nowrap}@media(max-width:640px){.wind-card .metrics{grid-template-columns:repeat(2,minmax(0,1fr))}}.metric{background:var(--paper);border-radius:15px;padding:14px}.label{font-size:.73rem;text-transform:uppercase;letter-spacing:.08em;color:var(--muted);font-weight:700}.value{font-size:1.55rem;font-weight:800;color:var(--navy)}.meta{color:var(--muted);font-size:.88rem;margin-top:12px}.station{font-weight:800;font-size:1.1rem;color:var(--navy)}.wind-distance-warning{margin-top:10px;padding:10px 12px;border:1px solid #e7c978;border-radius:12px;background:#fff8df;color:#654d08;font-size:.9rem}.wind-distance-warning strong{color:#4e3a00}.wind-summary{white-space:pre-line;margin-top:14px;padding:13px 14px;background:#eef7fa;border-left:4px solid var(--sea);border-radius:10px;color:var(--ink);font-size:.92rem}.event{display:grid;grid-template-columns:88px 12px 1fr;gap:12px;align-items:center;min-height:58px}.time{font-weight:800;color:var(--navy)}.dot{width:12px;height:12px;border-radius:50%;background:var(--slack);box-shadow:0 0 0 5px #edf3f5}.flood .dot{background:var(--flood)}.ebb .dot{background:var(--ebb)}.eventbody{border-left:2px solid var(--line);padding:8px 0 8px 18px}.eventlabel{font-weight:800}.eventdata{color:var(--muted);font-size:.9rem}.badge{display:inline-block;border-radius:999px;padding:5px 10px;background:#e9f6fb;color:var(--blue);font-size:.75rem;font-weight:800;margin-top:12px}.footer{text-align:center;color:var(--muted);font-size:.78rem;margin-top:22px}.hero .yogiism{margin:18px 0 0;max-width:720px;color:#fff;font-style:italic;font-size:.96rem;line-height:1.4;opacity:.96}.full-report{margin:0;white-space:pre-wrap;overflow-wrap:anywhere;font-family:"SFMono-Regular",Consolas,"Liberation Mono",Menlo,monospace;font-size:.88rem;line-height:1.55;background:#071f31;color:#e7f4f8;border-radius:14px;padding:18px;overflow-x:auto}.wind-readings{margin-top:14px;padding-top:12px;border-top:1px solid var(--line)}.wind-readings-header{display:flex;align-items:flex-end;justify-content:space-between;gap:12px;flex-wrap:wrap;margin-bottom:8px}.wind-readings-title{font-weight:850;color:var(--navy)}.wind-reading-control{display:flex;align-items:flex-end;gap:7px;flex-wrap:wrap}.wind-reading-control label{display:flex;flex-direction:column;gap:3px;color:var(--muted);font-size:.72rem;font-weight:850;text-transform:uppercase;letter-spacing:.04em}.wind-reading-control select{min-width:76px;padding:6px 28px 6px 8px;border:1px solid var(--line);border-radius:8px;background:#fff;font:inherit}.wind-card-head{display:flex;align-items:flex-start;justify-content:space-between;gap:14px;flex-wrap:wrap}.wind-card-head h2{margin-bottom:16px}.wind-unit-control{display:flex;align-items:center;gap:7px;color:var(--muted);font-size:.72rem;font-weight:850;text-transform:uppercase;letter-spacing:.04em}.wind-unit-control select{padding:6px 28px 6px 8px;border:1px solid var(--line);border-radius:8px;background:#fff;font:inherit;color:var(--ink)}.wind-reading-chart{margin:4px 0 12px;border:1px solid var(--line);border-radius:10px;background:#fff;padding:8px}.wind-reading-chart svg{display:block;width:100%;height:auto;min-height:260px;max-height:320px}.wind-chart-grid{stroke:#dce6e9;stroke-width:1}.wind-chart-axis{stroke:#8aa0a8;stroke-width:1}.wind-chart-wind{fill:none;stroke:#126b91;stroke-width:2.5;stroke-linejoin:round;stroke-linecap:round}.wind-chart-gust{fill:none;stroke:#a95a24;stroke-width:1.7;stroke-linejoin:round;stroke-linecap:round}.wind-chart-label{fill:#60747c;font-size:11px;font-family:system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}.wind-chart-legend{display:flex;gap:14px;align-items:center;flex-wrap:wrap;margin:0 0 6px;color:var(--muted);font-size:.78rem;font-weight:750}.wind-chart-key{display:inline-flex;align-items:center;gap:6px}.wind-chart-key-line{display:inline-block;width:22px;border-top:3px solid #126b91}.wind-chart-key-line.gust{border-top-color:#a95a24;border-top-width:2px;border-top-style:solid}.wind-chart-empty{padding:34px 12px;text-align:center;color:var(--muted);font-size:.85rem}.wind-chart-cursor{stroke:#263b46;stroke-width:1.4;pointer-events:none}.wind-chart-cursor-dot{fill:#fff;stroke-width:2;pointer-events:none}.wind-chart-cursor-dot.wind{stroke:#126b91}.wind-chart-cursor-dot.gust{stroke:#a95a24}.wind-chart-hit{fill:transparent;cursor:crosshair;touch-action:none}.wind-chart-readout{margin:-2px 0 8px;color:var(--ink);font-size:.82rem;font-weight:750;min-height:1.2em}.wind-readings-wrap{max-height:132px;overflow:auto;border:1px solid var(--line);border-radius:10px}.wind-readings-table{width:100%;border-collapse:separate;border-spacing:0;font-size:.84rem}.wind-readings-table th,.wind-readings-table td{padding:7px 9px;border-top:1px solid var(--line);text-align:left;white-space:nowrap}.wind-readings-table thead th{position:sticky;top:0;z-index:1;border-top:0;background:#f7fbfc;color:var(--muted);font-size:.7rem;text-transform:uppercase;letter-spacing:.05em}.wind-readings-table tbody tr:first-child td{border-top:0}.card-action-row{margin-top:14px;padding-top:12px;border-top:1px solid var(--line)}#current-summary-card,.wind-card{min-width:0}.timeline-scope-note{margin:.15rem 0 1rem;color:var(--muted);font-size:.88rem}.current-events-integrated{margin:14px 0 4px;padding:10px 0 0;border-top:1px solid var(--line)}.current-events-head{display:flex;justify-content:space-between;align-items:baseline;gap:10px;flex-wrap:wrap;margin-bottom:8px}.current-events-head strong{color:var(--navy);font-size:.92rem}.current-events-head span{color:var(--muted);font-size:.78rem}.current-key-times{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));column-gap:28px;row-gap:2px}.current-key-time{display:grid;grid-template-columns:76px minmax(0,1fr);gap:10px;align-items:baseline;padding:5px 0;border-bottom:1px solid #edf2f3;font-size:.86rem}.current-key-time-time{font-weight:800;color:var(--navy);white-space:nowrap}.current-key-time-label{color:var(--ink)}.current-key-time-label strong{font-weight:800}.current-key-time-meta{color:var(--muted);margin-left:6px;white-space:nowrap}@media(max-width:640px){.current-key-times{grid-template-columns:1fr}.current-key-time{grid-template-columns:72px minmax(0,1fr)}}.details-link-card{display:flex;align-items:center;justify-content:space-between;gap:18px;flex-wrap:wrap}.details-link-card h2{margin-bottom:.2rem}.details-link{display:inline-block;text-decoration:none;border:1px solid var(--line);border-radius:999px;padding:9px 13px;background:#fff;color:var(--blue);font-weight:850;white-space:nowrap}.details-note{color:var(--muted);font-size:.88rem;margin:-4px 0 14px}.current-chart-header{display:flex;justify-content:space-between;align-items:flex-start;gap:14px;flex-wrap:wrap}.current-window-inline{display:flex;align-items:baseline;gap:10px;flex-wrap:wrap;margin:2px 0 10px;color:var(--muted);font-size:.86rem}.current-window-inline strong{color:var(--navy);font-size:.86rem}.current-window-inline span{white-space:nowrap}.current-chart-header h2{margin-bottom:.15rem}.current-date-label{color:var(--muted);font-weight:750;font-size:.9rem}.current-range-toolbar{display:flex;gap:10px;align-items:flex-end;flex-wrap:wrap;margin:8px 0 10px;padding:12px 14px;border:1px solid var(--line);border-radius:14px;background:var(--paper)}.current-range-toolbar .current-date-nav{display:inline-flex;align-items:center;min-height:44px;text-decoration:none;border:1px solid var(--line);border-radius:999px;padding:8px 12px;background:#fff;color:var(--blue);font-size:.86rem;font-weight:850}.current-range-toolbar .current-date-nav.is-current{background:var(--navy);border-color:var(--navy);color:#fff}.current-control-label{display:flex;flex-direction:column;gap:4px;color:var(--muted);font-size:.75rem;font-weight:850}.current-control-label .current-date-picker{min-height:44px;font-size:1rem}@media(max-width:640px){.current-range-toolbar{align-items:stretch}.current-control-label{flex:1 1 140px}.current-range-toolbar .current-date-nav{justify-content:center;flex:1 1 135px}}.current-date-controls{display:flex;gap:7px;flex-wrap:wrap;align-items:center}.current-date-picker{border:1px solid var(--line);border-radius:999px;padding:6px 10px;background:#fff;color:var(--navy);font:inherit;font-size:.82rem;font-weight:750;min-height:34px}.current-date-picker:focus{outline:2px solid var(--blue);outline-offset:2px}.current-refreshing{opacity:.55;transition:opacity .15s ease}.current-date-controls a{display:inline-block;text-decoration:none;border:1px solid var(--line);border-radius:999px;padding:7px 11px;background:#fff;color:var(--blue);font-size:.82rem;font-weight:850}.current-date-controls a:hover{background:var(--paper)}.current-date-controls a.is-current{background:var(--navy);border-color:var(--navy);color:#fff}.current-planning{margin:16px 0 12px;padding:14px 16px;border:1px solid var(--line);border-radius:16px;background:#fff}.current-planning-head{display:flex;justify-content:space-between;gap:10px;align-items:baseline;flex-wrap:wrap;margin-bottom:10px}.current-planning-head strong{color:var(--navy);font-size:1rem}.current-planning-head span{color:var(--muted);font-size:.82rem}.planning-preferences{display:flex;flex-direction:column;gap:10px;margin:8px 0 12px}.planning-preferences-row{display:flex;gap:10px;flex-wrap:wrap;align-items:flex-end;width:100%}.planning-preferences label{display:flex;gap:5px;align-items:center;color:var(--muted);font-size:.78rem;font-weight:850}.planning-preferences input{min-height:40px;border:1px solid var(--line);border-radius:10px;background:#fff;color:var(--navy);font:inherit;font-size:1rem;padding:6px 8px}.planning-preferences input[type="number"]{width:76px}.planning-preferences b{color:var(--muted);font-size:.82rem}@media(max-width:640px){.planning-preferences label{flex:1 1 130px;justify-content:space-between}.planning-preferences input[type="time"]{min-width:110px}}.planning-help{margin:2px 0 12px;padding:10px 12px;border-radius:10px;background:#f7f9fa;color:var(--ink);font-size:.82rem;line-height:1.45}.planning-help strong{color:var(--navy)}.current-planning-days{display:grid;grid-template-columns:repeat(auto-fit,minmax(125px,1fr));gap:8px}.planning-day{border:1px solid var(--line);border-radius:12px;padding:10px;min-width:0}.planning-day.preferred{background:#eef8f3}.planning-day.caution{background:#fff8df;border-color:#e7c978}.planning-day.redflag{background:#fff0ed;border-color:#dfa297}.planning-date{font-size:.78rem;font-weight:850;color:var(--navy)}.planning-status{font-size:.92rem;font-weight:900;margin-top:2px}.preferred .planning-status{color:#176246}.caution .planning-status{color:#775900}.redflag .planning-status{color:#9a3328}.planning-detail{font-size:.78rem;line-height:1.35;color:var(--ink);margin-top:5px}.planning-disclaimer{color:var(--muted);font-size:.75rem;margin-top:9px}@media(max-width:640px){.current-planning-days{grid-template-columns:1fr 1fr}.planning-detail{font-size:.8rem}}.current-chart-wrap .event-point,.current-chart-wrap .event-point:hover{cursor:default!important;pointer-events:none}.current-chart-wrap{margin-top:16px}.current-chart-svg{display:block;width:100%;height:auto;background:#f8fbfc;border:1px solid var(--line);border-radius:16px}.grid-line{stroke:#d9e4e8;stroke-width:1}.v-grid-line{stroke:#e6eef1;stroke-width:1}.day-grid-line{stroke:#b7cbd4;stroke-width:1.4}.zero-line{stroke:#17384a;stroke-width:2}.axis-label{fill:#657d89;font-size:11px;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}.y-label{text-anchor:end}.x-label{text-anchor:middle}.axis-title{fill:#657d89;font-size:11px;text-anchor:middle}.tide-y-label{text-anchor:start}.tide-range-bar{opacity:1;stroke-width:3;stroke-linecap:round;fill:none}.tide-range-bar.typical{stroke:#4a6473}.tide-range-bar.elevated{stroke:#d5ad28}.tide-range-bar.large{stroke:#d9791f}.tide-range-bar.exceptional{stroke:#c94a3f}.tide-range-value{font-size:11px;font-weight:900;text-anchor:middle;paint-order:stroke;stroke:#fff;stroke-width:3px;stroke-linejoin:round;fill:#17384a}.tide-range-toggle{display:flex;align-items:center;gap:7px;color:var(--ink);font-size:.84rem;font-weight:800}.tide-range-toggle input{margin:0}.tide-range-legend{display:flex;gap:10px;flex-wrap:wrap;align-items:center;color:var(--muted);font-size:.76rem}.tide-range-key{display:inline-flex;align-items:center;gap:5px}.tide-range-swatch{width:11px;height:11px;border-radius:3px;display:inline-block}.tide-range-swatch.typical{background:#4a6473}.tide-range-swatch.elevated{background:#d5ad28}.tide-range-swatch.large{background:#d9791f}.tide-range-swatch.exceptional{background:#c94a3f}.night-window{fill:#aebdc4;opacity:.78}.sail-window{fill:#f8fbfc;opacity:.96}.preferred-window{fill:#f0d46d;opacity:.34}.flood-area{fill:#6d8fd0;opacity:.86}.ebb-area{fill:#0b9d83;opacity:.90}.current-line{fill:none;stroke:#214b62;stroke-width:1.5;stroke-linejoin:round;stroke-linecap:round}.event-point{stroke:#fff;stroke-width:1.5}.event-point.flood{fill:#5478bd}.event-point.ebb{fill:#078a75}.event-point.slack{fill:#756d64}.event-time{fill:#17384a;font-size:9px;font-weight:800;text-anchor:middle;paint-order:stroke;stroke:#fff;stroke-width:2.5px;stroke-linejoin:round}.event-time.flood{fill:#294f91}.event-time.ebb{fill:#066c5d}.event-time.slack{fill:#514b46}.now-line{stroke:#c63a2b;stroke-width:2.5}.now-label{fill:#c63a2b;font-size:11px;font-weight:800}.chart-explainer{color:var(--ink);font-size:.94rem;line-height:1.45;margin:2px 0 12px}.chart-note{color:var(--muted);font-size:.82rem;margin-top:9px}.candidate-table{width:100%;border-collapse:collapse;font-size:.86rem}.candidate-table th,.candidate-table td{padding:10px 8px;border-bottom:1px solid var(--line);text-align:left;vertical-align:top}.candidate-table th{font-size:.72rem;text-transform:uppercase;letter-spacing:.06em;color:var(--muted)}.candidate-table td.num,.candidate-table th.num{text-align:right;white-space:nowrap}.candidate-good td.status{font-weight:800}.candidate-bad{opacity:.82}.candidate-selected{background:rgba(20,120,100,.08)}.candidate-selected td:first-child{font-weight:800}.candidate-note{color:var(--muted);font-size:.82rem;margin:0 0 12px}.candidate-scroll{overflow-x:auto}.candidate-link{color:var(--blue);text-decoration:none;font-weight:800}.candidate-link:hover{text-decoration:underline}.candidate-actions{display:flex;gap:12px;align-items:center;flex-wrap:wrap;margin:0 0 12px}.nearest-link{display:inline-block;border:1px solid var(--line);border-radius:999px;padding:7px 12px;color:var(--blue);font-weight:800;text-decoration:none;background:#fff}.nearest-link:hover{background:var(--paper)}.map-card{overflow:hidden}.map-intro{display:flex;justify-content:space-between;gap:14px;align-items:flex-start;flex-wrap:wrap;margin-bottom:12px}.map-help{color:var(--muted);font-size:.9rem;max-width:600px}.map-wrap{border:1px solid var(--line);border-radius:16px;overflow:hidden;background:#dfecef}.location-map{height:390px;width:100%}.map-controls{display:flex;gap:10px;align-items:center;flex-wrap:wrap;margin-top:12px}.map-coordinate{font-variant-numeric:tabular-nums;color:var(--muted);font-size:.9rem}.map-go{display:inline-block;border:0;border-radius:999px;padding:10px 16px;background:var(--blue);color:#fff;font-weight:850;text-decoration:none;cursor:pointer}.map-go[aria-disabled="true"]{opacity:.45;pointer-events:none}.map-search-area{border:0;cursor:pointer}.map-search-area[aria-disabled="true"]{opacity:.55;pointer-events:none}.map-search-area[hidden]{display:none}.map-search-status{color:var(--muted);font-size:.82rem}.map-reset{border:1px solid var(--line);border-radius:999px;padding:9px 13px;background:#fff;color:var(--blue);font-weight:800;cursor:pointer}.map-reset:disabled{opacity:.45;cursor:default}.map-navigation{display:flex;gap:10px;align-items:center;flex-wrap:wrap;margin-top:10px}.map-state-controls{display:flex;gap:14px;align-items:center;flex-wrap:wrap;margin-top:10px}.map-current-toggle{display:inline-flex;align-items:center;gap:7px;color:var(--ink);font-weight:750;font-size:.88rem}.map-current-toggle input{margin:0}.map-overlays-menu{position:relative}.map-overlays-menu summary{list-style:none;cursor:pointer;border:1px solid var(--line);border-radius:999px;padding:9px 13px;background:#fff;color:var(--blue);font-weight:850;font-size:.88rem}.map-overlays-menu summary::-webkit-details-marker{display:none}.map-overlays-menu summary::after{content:" ▾";font-size:.78em}.map-overlays-menu[open] summary::after{content:" ▴"}.map-overlays-panel{position:absolute;z-index:700;top:calc(100% + 6px);left:0;min-width:270px;max-width:min(360px,88vw);padding:12px 14px;border:1px solid var(--line);border-radius:14px;background:#fff;box-shadow:0 10px 28px rgba(7,31,49,.18);display:flex;flex-direction:column;gap:10px}.map-overlay-toggle{display:flex;align-items:flex-start;gap:8px;color:var(--ink);font-weight:750;font-size:.88rem;line-height:1.3}.map-overlay-toggle input{margin-top:2px}.map-overlay-note{color:var(--muted);font-size:.76rem;line-height:1.35;padding-top:2px;border-top:1px solid var(--line)}.map-center-panel{min-width:240px}.map-center-action{width:100%;border:0;border-radius:10px;padding:9px 10px;background:transparent;color:var(--ink);font:inherit;font-size:.88rem;font-weight:750;text-align:left;cursor:pointer}.map-center-action:hover,.map-center-action:focus-visible{background:var(--paper);outline:none}.map-center-action:disabled{opacity:.45;cursor:default;background:transparent}.map-nav-button{border:1px solid var(--line);border-radius:999px;padding:9px 13px;background:#fff;color:var(--blue);font-weight:850;cursor:pointer}.map-nav-button:disabled{opacity:.45;cursor:default}.map-layer-note{margin-top:8px;color:var(--muted);font-size:.8rem;line-height:1.4}.map-smoke-legend{display:flex;align-items:center;gap:12px;flex-wrap:wrap;margin-top:8px;color:var(--muted);font-size:.78rem}.map-smoke-legend[hidden]{display:none}.map-smoke-legend span{display:inline-flex;align-items:center;gap:5px}.smoke-swatch{display:inline-block;width:18px;height:10px;border:1px solid rgba(70,70,70,.5);border-radius:2px}.smoke-swatch.light{background:rgba(174,181,184,.32)}.smoke-swatch.medium{background:rgba(226,163,63,.42)}.smoke-swatch.heavy{background:rgba(194,79,67,.52)}.map-smoke-note{font-style:italic}.map-symbol{display:inline-flex;align-items:center;justify-content:center;width:32px;height:32px;margin-right:4px;font-size:27px;font-weight:950;line-height:1;vertical-align:-6px;text-shadow:-1px -1px 0 #fff,1px -1px 0 #fff,-1px 1px 0 #fff,1px 1px 0 #fff,0 2px 3px rgba(0,0,0,.35)}
.map-symbol.request{color:#126b91}.map-symbol.wind{color:#2f855a}.map-symbol.current{color:#7d55a6}.map-symbol.wind-candidate{color:#4f6978}
.map-leaflet-symbol{background:transparent;border:0}
.location-map,
.location-map.leaflet-container,
.location-map.leaflet-container *{
  cursor:default;
}
.location-map.leaflet-container.leaflet-dragging,
.location-map.leaflet-container.leaflet-dragging *{cursor:default!important;}
.location-map .map-wind-candidate,
.location-map .map-wind-candidate *,
.location-map a,
.location-map button,
.location-map [role="button"]{
  cursor:pointer!important;
}
.location-map .map-leaflet-symbol:not(.map-wind-candidate),
.location-map .map-leaflet-symbol:not(.map-wind-candidate) *{
  cursor:default!important;
}

.map-leaflet-symbol{cursor:default!important}
.map-leaflet-symbol.map-wind-candidate{cursor:pointer!important;pointer-events:auto!important;width:32px!important;height:32px!important}
.map-leaflet-symbol .marker-symbol{
  background:transparent;
  border-radius:0;
  box-shadow:none;
}
.map-leaflet-symbol .marker-symbol.request{
  animation:selectedPulse 2.4s ease-in-out infinite;
}
@keyframes selectedPulse{
  0%,100%{transform:scale(1);opacity:1}
  50%{transform:scale(1.18);opacity:.72}
}
@media (prefers-reduced-motion: reduce){
  .map-leaflet-symbol .marker-symbol.request{animation:none}
}
.map-overlay-control{background:rgba(255,255,255,.96);border:1px solid #9aa8ae;border-radius:6px;box-shadow:0 1px 4px rgba(0,0,0,.28);padding:7px 9px;color:#17394b;font-size:.82rem;font-weight:750}
.map-overlay-control label{display:flex;align-items:center;gap:6px;cursor:pointer;white-space:nowrap}
.map-overlay-control input{margin:0}
.map-zone-tooltip{font-weight:800}
.map-leaflet-symbol .marker-symbol{display:flex;align-items:center;justify-content:center;width:32px;height:32px;font-size:27px;font-weight:950;line-height:1;text-shadow:-2px -2px 0 #fff,0 -2px 0 #fff,2px -2px 0 #fff,-2px 0 0 #fff,2px 0 0 #fff,-2px 2px 0 #fff,0 2px 0 #fff,2px 2px 0 #fff,0 3px 4px rgba(0,0,0,.5)}
.map-leaflet-symbol .marker-symbol.request{color:#126b91}.map-leaflet-symbol .marker-symbol.wind{color:#2f855a}.map-leaflet-symbol .marker-symbol.current{color:#7d55a6}.map-leaflet-symbol .marker-symbol.wind-candidate{color:#4f6978}

.legend-triangle{
  position:relative;
  width:32px;
  height:32px;
  display:inline-block;
}
.marker-triangle{
  position:relative;
  width:26px;
  height:24px;
  display:inline-block;
}
.marker-triangle:before{top:0}
.marker-triangle:after{top:5px}
.legend-triangle:before{
  top:4px;
}
.legend-triangle:after{
  top:9px;
}
.legend-triangle:before,
.marker-triangle:before{
  content:"";
  position:absolute;
  left:50%;
  transform:translateX(-50%);
  width:0;
  height:0;
  border-left:13px solid transparent;
  border-right:13px solid transparent;
  border-bottom:24px solid #4f6978;
}
.legend-triangle:after,
.marker-triangle:after{
  content:"";
  position:absolute;
  left:50%;
  transform:translateX(-50%);
  width:0;
  height:0;
  border-left:9px solid transparent;
  border-right:9px solid transparent;
  border-bottom:17px solid #fff;
}
.map-wind-info{position:absolute;left:50%;bottom:12px;transform:translateX(-50%);z-index:760;width:min(520px,calc(100% - 28px));box-sizing:border-box;background:rgba(255,255,255,.97);border:1px solid #9fb1bc;border-radius:9px;box-shadow:0 2px 9px rgba(0,0,0,.28);padding:9px 12px;color:#17394b;font-size:.9rem;line-height:1.35;pointer-events:auto}
.map-wind-info[hidden]{display:none}
.map-wind-info strong{font-weight:900}
.map-wind-info a{display:inline-block;margin-top:5px;color:#126b91;font-weight:900;text-decoration:underline;cursor:pointer}
.map-wind-info-close{float:right;border:0;background:transparent;color:#607886;font:inherit;font-weight:900;cursor:pointer;padding:0 0 4px 10px}


.location-map-wrap{position:relative}
.map-resize-handle{height:18px;display:flex;align-items:center;justify-content:center;cursor:ns-resize;touch-action:none;user-select:none;background:#f5f8f9;border-top:1px solid var(--line);color:var(--muted)}
.map-resize-handle::before{content:"";width:54px;height:4px;border-radius:999px;background:#9aabb1;box-shadow:0 -6px 0 -1px #c1ccd0}
.map-resize-handle:focus{outline:2px solid var(--blue);outline-offset:-2px}
.map-resize-handle[aria-grabbed="true"]{background:#edf4f6}
body.map-resizing{cursor:ns-resize!important;user-select:none!important}
@media(max-width:600px){.map-resize-handle{height:22px}}
.map-location-info-grid{display:grid;grid-template-columns:minmax(290px,.9fr) minmax(360px,1.1fr);gap:14px;align-items:stretch;margin-top:10px}.map-location-info-grid .map-coordinate-entry{margin-top:0;align-self:start}.selected-location-weather{margin-top:0;padding:12px 14px;border:1px solid var(--line);border-radius:14px;background:var(--paper);min-height:128px}.selected-location-weather-head{display:flex;align-items:baseline;justify-content:space-between;gap:10px;flex-wrap:wrap;margin-bottom:4px}.selected-location-weather-head strong{color:var(--navy);font-size:.95rem}.selected-location-weather-place{font-size:.84rem;font-weight:750;color:var(--ink);margin:0 0 7px}.selected-location-weather-updated{color:var(--muted);font-size:.75rem}.selected-location-weather-metrics{display:flex;gap:14px;flex-wrap:wrap;align-items:baseline}.selected-location-weather-metric{font-size:.88rem;color:var(--ink)}@media(max-width:700px){.map-location-info-grid{grid-template-columns:1fr}.selected-location-weather{min-height:0}}.selected-location-weather-metric b{color:var(--navy)}.selected-location-weather-forecast{margin:7px 0 0;color:var(--ink);font-size:.9rem}.selected-location-weather-note{margin:6px 0 0;color:var(--muted);font-size:.75rem}.selected-location-weather-error{margin:6px 0 0;color:#8b2c2c;font-size:.82rem}.map-coordinate-entry{display:flex;gap:8px;align-items:end;flex-wrap:wrap;margin-top:10px}.map-coordinate-field{display:flex;flex-direction:column;gap:3px}.map-coordinate-field label{font-size:.72rem;font-weight:800;color:var(--muted);text-transform:uppercase;letter-spacing:.04em}.map-coordinate-field input{width:132px;padding:7px 9px;border:1px solid var(--line);border-radius:9px;background:#fff;color:var(--ink);font:inherit}.map-coordinate-use{padding:8px 12px;border:1px solid #126b91;border-radius:9px;background:#126b91;color:#fff;font-weight:850;cursor:pointer}.map-coordinate-use:hover{filter:brightness(.97)}.map-coordinate-error{font-size:.8rem;color:#9b3027;min-height:1.2em}
.map-station-list{margin-top:12px}.map-station-list-title{font-weight:850;color:var(--navy);margin:0 0 8px}.map-station-table-wrap{max-height:220px;overflow:auto;border:1px solid var(--line);border-radius:14px;background:#fff}.map-station-table{width:100%;border-collapse:separate;border-spacing:0;font-size:.86rem}.map-station-table th,.map-station-table td{padding:8px 10px;border-top:1px solid var(--line);text-align:left;vertical-align:top;background:#fff}.map-station-table thead th{position:sticky;top:0;z-index:1;border-top:0;background:#f7fbfc}.map-station-table th{color:var(--muted);font-size:.72rem;text-transform:uppercase;letter-spacing:.06em}.map-station-table tbody tr:first-child td{border-top:0}.map-station-table a{color:var(--blue);font-weight:800;text-decoration:none}.map-station-table a:hover{text-decoration:underline}.map-scale-status{background:rgba(255,255,255,.94);border:1px solid #9fb5bf;border-radius:8px;padding:5px 8px;color:#234654;font-size:.74rem;font-weight:800;line-height:1.25;box-shadow:0 1px 4px rgba(25,55,70,.18);white-space:nowrap}.map-nautical-zoom-note{position:absolute;top:12px;left:50%;transform:translateX(-50%);z-index:850;background:rgba(7,31,49,.92);color:#fff;border-radius:999px;padding:7px 12px;font-size:.78rem;font-weight:850;box-shadow:0 2px 8px rgba(0,0,0,.2);pointer-events:none;white-space:nowrap;max-width:calc(100% - 32px);overflow:hidden;text-overflow:ellipsis}.map-overlay-toggle.is-unavailable{opacity:.5}.map-sst-legend{display:flex;align-items:center;gap:9px;flex-wrap:wrap;margin-top:8px;padding:8px 10px;border:1px solid var(--line);border-radius:10px;background:#f8fbfc;color:var(--muted);font-size:.76rem}.map-sst-bar{width:min(320px,60vw);height:12px;border-radius:999px;border:1px solid rgba(0,0,0,.16);background:linear-gradient(90deg,#263b9b 0%,#1677d2 18%,#1ccad8 36%,#65c94f 54%,#f0d63a 72%,#f28b2d 86%,#c9342f 100%)}.map-sst-scale{display:flex;gap:10px;align-items:center;justify-content:space-between;min-width:min(320px,60vw);font-weight:800;color:var(--ink)}.map-sst-note{flex:1 1 260px}.map-sst-legend[hidden]{display:none}.map-chl-field-legend{display:flex;align-items:center;gap:9px;flex-wrap:wrap;margin-top:8px;padding:8px 10px;border:1px solid var(--line);border-radius:10px;background:#f8fbfc;color:var(--muted);font-size:.76rem}.map-chl-field-bar{width:min(320px,60vw);height:12px;border-radius:999px;border:1px solid rgba(0,0,0,.16);background:linear-gradient(90deg,#334db3 0%,#2485c6 20%,#2fc6be 40%,#68ce63 60%,#d4dc45 80%,#e4b83f 100%)}.map-chl-field-scale{display:flex;gap:8px;align-items:center;justify-content:space-between;min-width:min(320px,60vw);font-weight:800;color:var(--ink)}.map-chl-field-note{flex:1 1 300px}.map-chl-field-legend[hidden]{display:none}.map-chl-field-status{margin-top:6px;color:var(--muted);font-size:.78rem}.map-chl-field-status[hidden]{display:none}.map-chl-legend{display:flex;align-items:center;gap:12px;flex-wrap:wrap;margin-top:8px;padding:8px 10px;border:1px solid var(--line);border-radius:10px;background:#f8fbfc;color:var(--muted);font-size:.76rem}.map-chl-contour-key{display:inline-flex;align-items:center;gap:5px;font-weight:800;color:var(--ink)}.map-chl-contour-line{display:inline-block;width:34px;height:0;border-top-style:solid;filter:drop-shadow(0 0 1px #102a38)}.map-chl-contour-line.c02{border-top-width:2px;border-top-color:#63e6ff}.map-chl-contour-line.c03{border-top-width:4px;border-top-color:#fffde7}.map-chl-contour-line.c05{border-top-width:2px;border-top-color:#ffd166}.map-chl-note{flex:1 1 320px}.map-chl-legend[hidden]{display:none}.map-chl-status{margin-top:6px;color:var(--muted);font-size:.78rem}.map-chl-status[hidden]{display:none}.map-structure-status{margin-top:6px;color:var(--muted);font-size:.78rem}.map-structure-status[hidden]{display:none}.map-fishing-status{margin-top:6px;color:var(--muted);font-size:.78rem}.map-fishing-status[hidden]{display:none}.map-fishing-legend{display:flex;gap:12px;align-items:center;flex-wrap:wrap;margin-top:8px;padding:7px 9px;border:1px solid var(--line);border-radius:10px;background:#f8fbfc;color:var(--muted);font-size:.75rem}.map-fishing-legend[hidden]{display:none}.map-fishing-key{display:inline-flex;align-items:center;gap:5px}.map-fishing-dot{width:10px;height:10px;border-radius:50%;display:inline-block;border:2px solid #fff;box-shadow:0 0 0 1px #173645}.map-fishing-dot.albacore{background:#18a6a6}.map-fishing-dot.bluefin{background:#2658b8}.map-fishing-zone-key{width:18px;height:10px;border:2px dashed #2658b8;background:rgba(38,88,184,.12);display:inline-block}.map-undersea-feature-label{background:transparent!important;border:0!important;box-shadow:none!important;white-space:nowrap;pointer-events:none}.map-undersea-feature-label .undersea-dot{display:inline-block;width:7px;height:7px;border-radius:50%;margin-right:5px;background:#ffd166;border:1px solid #20323c;vertical-align:1px;box-shadow:0 0 0 1px rgba(255,255,255,.75)}.map-undersea-feature-label .undersea-name{font-size:13px;font-weight:850;letter-spacing:.01em;color:#fff7d1;text-shadow:-2px -2px 0 #102a38,2px -2px 0 #102a38,-2px 2px 0 #102a38,2px 2px 0 #102a38,0 2px 3px rgba(0,0,0,.85)}.map-legend{display:flex;gap:12px;flex-wrap:wrap;margin-top:10px;color:var(--muted);font-size:.78rem}.map-key{display:inline-flex;align-items:center;gap:5px}.map-dot{width:10px;height:10px;border-radius:50%;display:inline-block}.map-dot.request{background:#126b91}.map-dot.wind{background:#2f855a}.map-dot.current{background:#7d55a6}@media(max-width:600px){.location-map{height:330px}}.candidate-state{display:flex;gap:5px;flex-wrap:wrap}.candidate-badge{display:inline-block;border-radius:999px;padding:3px 7px;font-size:.68rem;font-weight:900;letter-spacing:.04em}.badge-auto{background:#e8f0fb;color:#24538a}.badge-selected{background:#e8f5ef;color:#176246}.candidate-auto td:first-child{font-weight:800}
.offshore-trip-card{border-left:5px solid #126b91}.offshore-trip-head{display:flex;justify-content:space-between;gap:12px;align-items:flex-start;flex-wrap:wrap}.offshore-trip-coords{color:var(--muted);font-size:.82rem;font-weight:750}.offshore-trip-summary{margin:10px 0 12px;padding:10px 12px;border:1px solid var(--line);border-radius:12px;background:#f7fbfc}.offshore-trip-summary strong{color:var(--navy)}.offshore-trip-metrics{display:grid;grid-template-columns:repeat(5,minmax(0,1fr));gap:8px;margin:10px 0}.offshore-trip-metric{padding:9px 10px;border:1px solid var(--line);border-radius:11px;background:#fff}.offshore-trip-metric .label{font-size:.68rem;text-transform:uppercase;letter-spacing:.05em;color:var(--muted);font-weight:850}.offshore-trip-metric .value{margin-top:3px;color:var(--navy);font-weight:900;font-size:1rem}.offshore-trip-buoy{margin:4px 0 8px;color:var(--ink);font-size:.86rem}.offshore-trip-watch{margin:8px 0 0;padding-left:20px;color:var(--ink)}.offshore-trip-watch li{margin:3px 0}.offshore-trip-forecast{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:8px;margin-top:10px}.offshore-trip-period{padding:10px 12px;border:1px solid var(--line);border-radius:11px;background:#fbfdfe}.offshore-trip-period strong{display:block;color:var(--navy);margin-bottom:3px}.offshore-trip-period p{margin:0;line-height:1.42;font-size:.88rem}.offshore-trip-note{margin:9px 0 0;color:var(--muted);font-size:.78rem;line-height:1.45}.offshore-trip-error{color:#8b2c2c;font-size:.88rem}.offshore-trip-loading{color:var(--muted);font-weight:750}.fishing-planning{margin-top:18px;padding-top:16px;border-top:2px solid #dce8ed}.fishing-planning-head{display:flex;justify-content:space-between;gap:10px;align-items:baseline;flex-wrap:wrap}.fishing-planning-head h3{margin:0;color:var(--navy);font-size:1.05rem}.fishing-planning-status{color:var(--muted);font-size:.78rem}.fishing-planning-summary{margin:10px 0;padding:10px 12px;border:1px solid var(--line);border-radius:12px;background:#fffdf5;line-height:1.45}.fishing-planning-grid{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:8px}.fishing-planning-item{padding:10px 11px;border:1px solid var(--line);border-radius:11px;background:#fff}.fishing-planning-item .label{font-size:.68rem;text-transform:uppercase;letter-spacing:.05em;color:var(--muted);font-weight:850}.fishing-planning-item .value{margin-top:3px;color:var(--navy);font-weight:900;line-height:1.25}.fishing-planning-item .detail{margin-top:4px;color:var(--muted);font-size:.77rem;line-height:1.35}.fishing-planning-error{margin:8px 0 0;color:#8b2c2c;font-size:.82rem}@media(max-width:850px){.fishing-planning-grid{grid-template-columns:repeat(2,minmax(0,1fr))}}@media(max-width:520px){.fishing-planning-grid{grid-template-columns:1fr}}@media(max-width:800px){.offshore-trip-metrics{grid-template-columns:repeat(2,minmax(0,1fr))}}@media(max-width:640px){.offshore-trip-forecast{grid-template-columns:1fr}}.marine-forecast-head{display:flex;justify-content:space-between;gap:12px;align-items:flex-start;flex-wrap:wrap}
.marine-forecast-zone{color:var(--muted);font-size:.84rem;font-weight:750}
.marine-alerts{margin:8px 0 14px;display:flex;gap:7px;flex-wrap:wrap}
.marine-alert{display:inline-block;border-radius:999px;padding:5px 9px;background:#fff0ef;border:1px solid #e0a39d;color:#9b3027;font-size:.78rem;font-weight:900}
.marine-periods{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:10px;margin-top:12px}
.marine-period{border:1px solid var(--line);border-radius:14px;padding:12px 14px;background:#fbfdfe}
.marine-period strong{display:block;color:var(--navy);margin-bottom:4px}
.marine-period p{margin:0;line-height:1.45}
.marine-forecast-note{color:var(--muted);font-size:.82rem;margin:10px 0 0}
.marine-forecast-error{color:var(--muted);font-size:.9rem}
@media(max-width:640px){.marine-periods{grid-template-columns:1fr}}
.bottom-source-context{display:grid;gap:3px;margin:0 0 12px;padding:9px 11px;border:1px solid var(--line);border-radius:12px;background:#f7fbfc;color:var(--muted);font-size:.86rem}.bottom-source-context strong{color:var(--navy)}
.error-card{border-left:5px solid #b64735;background:#fff7f4}.error-card h2{color:#8f3025}.error-message{font-weight:650;line-height:1.5}.error-help{color:var(--muted);font-size:.9rem}@media(max-width:640px){.shell{padding:14px 12px 40px}.hero{padding:24px 20px;min-height:430px;background-position:center 42%}.grid{grid-template-columns:1fr}.full{grid-column:auto}.metrics{grid-template-columns:1fr 1fr}.metric:first-child{grid-column:1/-1}.card{padding:18px}}.bottom.planning-preferred{background:#eff8f1;border-color:#b8d8c0}.bottom.planning-caution{background:#fff8e6;border-color:#e6c66a}.bottom.planning-redflag{background:#fff0ef;border-color:#e0a39d}.bottom .planning-period-status{margin:0 0 10px;font-weight:900;font-size:1.05rem}.bottom .planning-period-status.preferred{color:#176246}.bottom .planning-period-status.caution{color:#8a5a00}.bottom .planning-period-status.redflag{color:#9b3027}.bottom-wind-summary{margin:10px 0 14px}.bottom-wind-summary .metrics{margin-bottom:7px}.conditions-now-heading{display:flex;align-items:baseline;gap:8px;flex-wrap:wrap}.conditions-now-heading .conditions-now-asof{font-size:.72em;font-weight:800;color:var(--muted);letter-spacing:.01em}.page-loading-overlay{position:fixed;inset:0;z-index:6000;display:flex;align-items:center;justify-content:center;background:rgba(245,250,252,.82);backdrop-filter:blur(2px);opacity:0;visibility:hidden;pointer-events:none;transition:opacity .12s ease,visibility .12s ease}.page-loading-overlay.active{opacity:1;visibility:visible;pointer-events:auto}.page-loading-box{display:flex;align-items:center;gap:12px;padding:15px 18px;border:1px solid var(--line);border-radius:16px;background:#fff;box-shadow:0 14px 38px rgba(8,43,69,.18);color:var(--navy);font-weight:850}.page-loading-spinner{width:30px;height:30px;border:4px solid #d8e7ed;border-top-color:var(--blue);border-radius:50%;animation:page-loading-spin .8s linear infinite}@keyframes page-loading-spin{to{transform:rotate(360deg)}}@media (prefers-reduced-motion:reduce){.page-loading-spinner{animation-duration:1.8s}}.map-sources-card h2{margin-bottom:7px}.map-sources-note{margin:0;color:var(--muted);font-size:.84rem;line-height:1.5}.planning-page-link-card{display:flex;align-items:center;justify-content:space-between;gap:16px;flex-wrap:wrap}.planning-page-link-card h2{margin-bottom:4px}.planning-page-link-card p{margin:0;color:var(--muted)}.planning-page-link{display:inline-block;padding:11px 16px;border-radius:999px;background:var(--navy);color:#fff;text-decoration:none;font-weight:900}.planning-page-link:hover{filter:brightness(1.08)}@media(max-width:640px){.planning-page-link{width:100%;text-align:center}}</style></head><body><main class="shell">
<section class="hero"><div class="eyebrow">Mauri's Weather & Water Conditions</div><h1>{{.Title}}</h1><div class="sub">{{.ReportTime}} · {{.Station}}</div>{{if .Historical}}<span class="badge">Historical · {{.RequestedTime}}</span>{{end}}{{if .Yogiism}}<div class="yogiism">“{{.Yogiism}}” — Yogi Berra</div>{{end}}</section><div class="grid">
{{if not .PlanningDetails}}
<section id="bottom-line-card" class="card full bottom{{if .PlanningPeriodClass}} planning-{{.PlanningPeriodClass}}{{end}}"><h2 class="conditions-now-heading"><span>CONDITIONS NOW</span>{{if .WindObserved}}<span class="conditions-now-asof">AS OF {{.WindObserved}}{{if .WindObservedAge}} · {{.WindObservedAge}}{{end}}</span>{{end}}</h2><div class="bottom-source-context">{{if .BottomLineWindSource}}<div><strong>Wind station:</strong> {{.BottomLineWindSource}}</div>{{end}}{{if .BottomLineCurrentSource}}<div><strong>Currents station:</strong> {{.BottomLineCurrentSource}}</div>{{else}}<div><strong>Currents station:</strong> unavailable</div>{{end}}</div>{{if .PlanningPeriodCause}}<p><strong>{{.PlanningPeriodCause}}</strong></p>{{end}}{{if .PlanningPeriodDetail}}<p>{{.PlanningPeriodDetail}}</p>{{end}}{{if not .WindError}}<div class="bottom-wind-summary" aria-label="Latest wind observation"><div class="metrics"><div class="metric"><div class="label">Direction</div><div class="value">{{if .WindDirection}}{{.WindDirection}}{{else}}—{{end}}</div></div><div class="metric"><div class="label">Wind</div><div class="value">{{if .WindSpeed}}{{.WindSpeed}}{{else}}—{{end}}</div></div><div class="metric"><div class="label">Gust</div><div class="value">{{if .WindGust}}{{.WindGust}}{{else}}—{{end}}</div></div><div class="metric"><div class="label">Air temp</div><div class="value">{{if .WindAirTemp}}{{.WindAirTemp}}{{else}}—{{end}}</div></div></div></div>{{end}}{{if .BottomLineCurrentChart}}<div class="chart-explainer"><strong>1-day tidal current outlook.</strong> Above zero = flood; below zero = ebb; crossings = slack water.</div><div class="current-chart-wrap">{{.BottomLineCurrentChart}}</div>{{else}}{{range .BottomLineNarrative}}<p>{{.}}</p>{{else}}{{if .WindError}}<p>Summary unavailable.</p>{{end}}{{end}}{{end}}</section>
<section class="card full planning-page-link-card"><div><h2>Planning and Details</h2><p>Open the full planning dashboard for location, wind, currents, forecasts, map layers, and customization controls.</p></div><a id="planning-page-link" class="planning-page-link" href="{{.PlanningDetailsURL}}">Planning and Details →</a></section>
{{else}}
<section class="card full planning-page-link-card"><div><h2>Planning and Details</h2><p>Full planning dashboard and customization controls.</p></div><a class="planning-page-link" href="{{.ConditionsURL}}">← Back to Conditions Now</a></section>
<section class="card full map-card"><div class="map-intro"><div><h2>Choose Location</h2><div class="map-help">Click the map to choose a sailing location, then use Find stations near selected location. The latitude/longitude fields always show the current map center; you may edit them and use Center Map → Latitude & Longitude to pan there without changing the selected sailing location. My location also centers the map only. Panning changes the view, not the selected ★ location. Click a nearby wind station to pin its details and preview the associated currents station, then use the selection link in the map panel to commit the wind-station choice. Candidate stations and distances always refer to the selected location.</div></div></div><div class="location-map-wrap"><div id="sailing-location-map" class="location-map" aria-label="Interactive supported coastal and inland waters conditions map"></div><div id="map-nautical-zoom-note" class="map-nautical-zoom-note" hidden aria-live="polite">Nautical chart available at Zoom 9+.</div><div id="map-resize-handle" class="map-resize-handle" role="separator" aria-label="Resize map vertically" aria-orientation="horizontal" aria-valuemin="260" aria-valuemax="900" aria-valuenow="390" aria-grabbed="false" tabindex="0" title="Drag up or down to resize map"></div><div id="map-wind-info" class="map-wind-info" hidden aria-live="polite"></div></div><div class="map-state-controls" aria-label="Map controls"><details id="map-types-menu" class="map-overlays-menu"><summary>Map Types</summary><div class="map-overlays-panel"><label class="map-overlay-toggle"><input type="radio" name="map-type" value="map"> <span>Street Map</span></label><label id="map-type-nautical-label" class="map-overlay-toggle"><input id="map-type-nautical" type="radio" name="map-type" value="nautical"> <span>Nautical Chart <small>(Zoom 9+)</small></span></label><label class="map-overlay-toggle"><input type="radio" name="map-type" value="satellite"> <span>Satellite</span></label><label class="map-overlay-toggle"><input type="radio" name="map-type" value="hybrid"> <span>Hybrid</span></label></div></details><details id="map-overlays-menu" class="map-overlays-menu"><summary>Map Overlays</summary><div class="map-overlays-panel"><label id="map-marine-zone-control" class="map-overlay-toggle" {{if not .MarineForecastGeometry}}hidden{{end}}><input type="checkbox" id="map-show-marine-zone"> <span id="map-marine-zone-label">NWS forecast zone {{.MarineForecastZone}}</span></label><label class="map-overlay-toggle"><input type="checkbox" id="map-show-smoke"> <span>Satellite smoke (NOAA HMS)</span></label><label class="map-overlay-toggle"><input type="checkbox" id="map-show-sst"> <span>Sea Surface Temp (NOAA CoastWatch)</span></label><label class="map-overlay-toggle"><input type="checkbox" id="map-show-chlorophyll-field"> <span>Chlorophyll Field (NOAA gap-filled 2 km)</span></label><label class="map-overlay-toggle"><input type="checkbox" id="map-show-chlorophyll"> <span>Chlorophyll Contours (NOAA gap-filled 2 km)</span></label><label class="map-overlay-toggle"><input type="checkbox" id="map-show-structure"> <span>Underwater Structure (NOAA bathymetry + names)</span></label><label class="map-overlay-toggle"><input type="checkbox" id="map-show-fishing-reports"> <span>Fishing Reports (external data file)</span></label><label class="map-overlay-toggle"><input type="checkbox" id="map-show-clouds"> <span>Satellite Cloud Cover (NOAA)</span></label><label class="map-overlay-toggle"><input type="checkbox" id="map-show-radar"> <span>Weather radar (NOAA/NWS)</span></label><div class="map-overlay-note">Sea Surface Temp uses NOAA CoastWatch Geo-Polar Blended daily analysed sea-surface temperature imagery. The fixed fishing-oriented color scale emphasizes temperature breaks and boundaries between cooler and warmer water. It is a multi-satellite global Level-4 analysis at about 5 km resolution; near-shore and inland values should still be interpreted cautiously. Chlorophyll Field uses NOAA CoastWatch multi-sensor Level-4 DINEOF gap-filled daily chlorophyll-a at about 2 km as a restrained semi-transparent background raster so broad water-mass features remain visible. Chlorophyll Contours are a separate overlay derived from the same NOAA numeric grid and draw only three concentration contours: 0.2, 0.3, and 0.5 mg/m³. Use either layer independently or combine them with Sea Surface Temp and underwater structure. NOAA performs the cloud-gap filling upstream. Underwater Structure combines NOAA/NCEI ETOPO shaded relief with NOAA Marine Cadastre official undersea feature points. Fishing-relevant features such as seamounts, banks, ridges, hills, knolls, shoals, reefs, rises, plateaus, pinnacles, and escarpments are labeled locally for better contrast. It is intended for fishing-planning context, not navigation; smaller pinnacles may not appear in the global relief model. Fishing Reports is loaded from assets/fishing_reports.json so report updates do not require changing Go or map UI code. Exact positions are plotted only when the source provides them; fisherman shorthand such as “20×20” is shown as a derived approximate position with an uncertainty circle, and broad reports such as “Cordell to Monterey” are shown as regional zones rather than fake point coordinates. Satellite Cloud Cover uses NOAA/NESDIS GOES imagery rendered for the current map view; daylight areas appear natural-color-like and nighttime areas use infrared imagery. Radar uses Iowa State IEM's Web-Mercator CONUS NEXRAD N0Q WMS layer; a clear/transparent radar layer can simply mean no precipitation echoes are present.</div></div></details><details id="map-center-menu" class="map-overlays-menu"><summary>Center Map</summary><div class="map-overlays-panel map-center-panel"><button type="button" id="map-geolocate" class="map-center-action" title="Center the map on your device location without changing the selected location">My location</button><button type="button" id="map-nav-coordinates" class="map-center-action" title="Center the map on the latitude and longitude shown below">Latitude &amp; Longitude</button><button type="button" id="map-nav-selected" class="map-center-action" {{if not .MapHasRequest}}disabled{{end}}>Selected location</button><button type="button" id="map-nav-wind" class="map-center-action" {{if not .MapHasWind}}disabled{{end}}>Selected wind station</button><button type="button" id="map-nav-current" class="map-center-action" {{if not .MapHasCurrent}}disabled{{end}}>Selected currents station</button></div></details><button id="map-reset" class="map-reset" type="button" aria-disabled="{{if .MapHasRequest}}false{{else}}true{{end}}" {{if not .MapHasRequest}}disabled{{end}}>Clear selected location &amp; candidates</button></div><div class="map-location-info-grid"><div class="map-coordinate-entry" aria-label="Map center coordinates"><div class="map-coordinate-field"><label for="map-lat-input">Latitude</label><input id="map-lat-input" type="text" inputmode="decimal" value="{{printf "%.5f" .MapCenterLat}}" aria-label="Map center latitude"></div><div class="map-coordinate-field"><label for="map-lon-input">Longitude</label><input id="map-lon-input" type="text" inputmode="decimal" value="{{printf "%.5f" .MapCenterLon}}" aria-label="Map center longitude"></div><span id="map-coordinate-error" class="map-coordinate-error" aria-live="polite"></span></div><div id="selected-location-weather" class="selected-location-weather" aria-live="polite"><div class="selected-location-weather-head"><strong>Local Conditions</strong><span id="selected-location-weather-updated" class="selected-location-weather-updated">{{if .SelectedWeatherUpdated}}Updated {{.SelectedWeatherUpdated}}{{end}}</span></div><div id="selected-location-weather-place" class="selected-location-weather-place">{{if .MapHasRequest}}{{if .SelectedWeatherLocation}}{{.SelectedWeatherLocation}}{{else}}Near selected location{{end}}{{else}}Select a location for local conditions{{end}}</div><div id="selected-location-weather-content" {{if not .MapHasRequest}}hidden{{end}}><div class="selected-location-weather-metrics"><span class="selected-location-weather-metric"><b>Air temp:</b> <span id="selected-location-weather-air">{{if .SelectedWeatherAirTemp}}{{.SelectedWeatherAirTemp}}{{else}}—{{end}}</span></span><span class="selected-location-weather-metric"><b>High:</b> <span id="selected-location-weather-high">{{if .SelectedWeatherHighTemp}}{{.SelectedWeatherHighTemp}}{{else}}—{{end}}</span></span><span class="selected-location-weather-metric"><b>Low:</b> <span id="selected-location-weather-low">{{if .SelectedWeatherLowTemp}}{{.SelectedWeatherLowTemp}}{{else}}—{{end}}</span></span></div><p id="selected-location-weather-forecast" class="selected-location-weather-forecast" {{if not .SelectedWeatherShortForecast}}hidden{{end}}>{{.SelectedWeatherShortForecast}}</p><p id="selected-location-weather-error" class="selected-location-weather-error" {{if not .SelectedWeatherError}}hidden{{end}}>{{.SelectedWeatherError}}</p><p class="selected-location-weather-note">NWS point forecast near the selected location. Air temperature is the current-hour forecast, not a direct observation.</p></div></div></div><div class="map-controls"><span id="map-find-point" class="map-go map-search-area" role="button" tabindex="0" aria-disabled="true">Select a location to find stations</span><span id="map-search-status" class="map-search-status" aria-live="polite"></span></div><div id="map-smoke-status" class="map-layer-note" hidden aria-live="polite"></div><div id="map-weather-overlay-status" class="map-layer-note" hidden aria-live="polite"></div><div id="map-smoke-legend" class="map-smoke-legend" hidden><span><i class="smoke-swatch light"></i>Light</span><span><i class="smoke-swatch medium"></i>Medium</span><span><i class="smoke-swatch heavy"></i>Heavy</span><span class="map-smoke-note">NOAA HMS satellite analysis; qualitative smoke density, not AQI.</span></div><div id="map-sst-status" class="map-layer-note" hidden aria-live="polite"></div><div id="map-structure-status" class="map-structure-status" hidden></div><div id="map-chl-field-legend" class="map-chl-field-legend" hidden><strong>Chlorophyll Field</strong><span class="map-chl-field-bar" aria-hidden="true"></span><span class="map-chl-field-scale"><span>0.1</span><span>0.2</span><span>0.3</span><span>0.5</span><span>0.7</span><span>1 mg/m³</span></span><span class="map-chl-field-note">Semi-transparent NOAA gap-filled chlorophyll background. Blue/cyan = lower chlorophyll / cleaner water; green/yellow = higher chlorophyll. Use this layer to see broad water-mass features such as pockets and eddies.</span></div><div id="map-chl-field-status" class="map-chl-field-status" hidden></div><div id="map-chl-legend" class="map-chl-legend" hidden><strong>Chlorophyll Contours</strong><span class="map-chl-contour-key"><i class="map-chl-contour-line c02"></i>0.2 mg/m³</span><span class="map-chl-contour-key"><i class="map-chl-contour-line c03"></i>0.3 mg/m³</span><span class="map-chl-contour-key"><i class="map-chl-contour-line c05"></i>0.5 mg/m³</span><span class="map-chl-note">Exact chlorophyll concentration contours from the NOAA numeric grid. The 0.3 mg/m³ line is emphasized as the primary clear-water transition reference; compare it with Sea Surface Temp breaks and offshore structure.</span></div><div id="map-chl-status" class="map-chl-status" hidden></div><div id="map-fishing-legend" class="map-fishing-legend" hidden><strong>Fishing Reports</strong><span class="map-fishing-key"><i class="map-fishing-dot albacore"></i>Albacore</span><span class="map-fishing-key"><i class="map-fishing-dot bluefin"></i>Bluefin</span><span class="map-fishing-key"><i class="map-fishing-zone-key"></i>Broad regional report</span><span>Dashed uncertainty circles = derived shorthand positions.</span></div><div id="map-fishing-status" class="map-fishing-status" hidden></div><div id="map-sst-legend" class="map-sst-legend" hidden><strong>Sea Surface Temp / Temp Breaks</strong><span class="map-sst-bar" aria-hidden="true"></span><span class="map-sst-scale"><span>45°F</span><span>50</span><span>55</span><span>60</span><span>65</span><span>70</span><span>75°F</span></span><span class="map-sst-note">Fixed 45–75°F NOAA sea-surface temperature scale in 1°F bands. Closely packed color changes make temperature breaks easier to see; values below/above the range saturate at the end colors. NOAA Geo-Polar Blended daily sea-surface temperature, about 5 km resolution.</span></div><div class="map-layer-note">Drag the handle directly below the map to make the map taller or shorter. Keyboard users can focus the handle and use ↑/↓ (Shift for larger steps).</div><div class="map-legend"><span class="map-key"><span class="map-symbol request" aria-hidden="true">★</span>Selected location</span><span class="map-key"><span class="map-symbol wind" aria-hidden="true">▲</span>Selected wind station</span><span class="map-key"><span class="map-symbol wind-candidate legend-triangle" aria-hidden="true"><span></span></span>Nearby wind stations</span><span class="map-key"><span class="map-symbol current" aria-hidden="true">◆</span>Selected currents station</span></div><div id="map-station-list" class="map-station-list" aria-live="polite">{{if .MapHasWind}}<div class="meta"><strong>Selected wind source:</strong> {{.MapWindStation}}</div>{{end}}{{if .WindCandidates}}<div class="map-station-list-title">Nearby Wind Stations</div><div class="map-station-table-wrap"><table class="map-station-table"><thead><tr><th>Station</th><th>Name</th><th>Wind</th><th>Age</th><th>From selected location</th></tr></thead><tbody>{{range .WindCandidates}}<tr><td><a class="map-station-report-link" href="{{.URL}}" data-base-href="{{.URL}}">{{.Station}}</a></td><td><a class="map-station-report-link" href="{{.URL}}" data-base-href="{{.URL}}">{{.Name}}</a></td><td>{{if .Wind}}{{.Wind}}{{else}}—{{end}}</td><td>{{if .ObservationAge}}{{.ObservationAge}}{{else}}—{{end}}</td><td>{{.Distance}}</td></tr>{{end}}</tbody></table></div>{{end}}</div></section>
{{if .WindError}}<section class="card full error-card"><h2>Wind station selection unavailable</h2><p class="error-message">{{.WindError}}</p><p class="error-help">The page is still available so you can inspect the request and nearby station diagnostics. Try nearby coordinates or an explicit NDBC station ID.</p></section>{{end}}
<section class="card full wind-card"><div class="wind-card-head"><h2>Wind</h2><label class="wind-unit-control" for="wind-unit-select">Wind speed<select id="wind-unit-select"><option value="kts" {{if eq .WindUnit "kts"}}selected{{end}}>Knots</option><option value="mph" {{if eq .WindUnit "mph"}}selected{{end}}>MPH</option></select></label></div>
<div class="metrics">
<div class="metric"><div class="label">Direction</div><div class="value">{{if .WindDirection}}{{.WindDirection}}{{else}}—{{end}}</div></div>
<div class="metric"><div class="label">Wind</div><div class="value">{{if .WindSpeed}}{{.WindSpeed}}{{else}}—{{end}}</div></div>
<div class="metric"><div class="label">Gust</div><div class="value">{{if .WindGust}}{{.WindGust}}{{else}}—{{end}}</div></div>
<div class="metric"><div class="label">Air temp</div><div class="value">{{if .WindAirTemp}}{{.WindAirTemp}}{{else}}—{{end}}</div></div>
</div>
<div class="meta"><strong>Observed:</strong> {{if .WindObserved}}{{.WindObserved}}{{else}}unavailable{{end}}</div>
{{if .WindSelection}}<div class="meta"><strong>Wind station:</strong> {{.WindSelection}}</div>{{end}}
{{if .WindDistanceWarning}}<div class="wind-distance-warning"><strong>Wind station distance warning:</strong> {{.WindDistanceWarning}}</div>{{end}}
{{if .WindSummary}}<div class="wind-summary">{{.WindSummary}}</div>{{end}}
{{if .WindReadings}}<div class="wind-readings">
<div class="wind-readings-header"><div class="wind-readings-title">Latest wind readings</div>
<div class="wind-reading-control"><label for="wind-reading-hours">History<select id="wind-reading-hours" data-station="{{.Station}}"><option value="1" {{if eq .WindReadingHours 1}}selected{{end}}>1h</option><option value="4" {{if eq .WindReadingHours 4}}selected{{end}}>4h</option><option value="8" {{if eq .WindReadingHours 8}}selected{{end}}>8h</option><option value="12" {{if eq .WindReadingHours 12}}selected{{end}}>12h</option><option value="16" {{if eq .WindReadingHours 16}}selected{{end}}>16h</option><option value="20" {{if eq .WindReadingHours 20}}selected{{end}}>20h</option><option value="24" {{if eq .WindReadingHours 24}}selected{{end}}>24h</option></select></label><span id="wind-reading-status" class="meta" aria-live="polite"></span></div></div>
<div class="wind-chart-legend" aria-hidden="true"><span class="wind-chart-key"><span class="wind-chart-key-line"></span>Sustained</span><span class="wind-chart-key"><span class="wind-chart-key-line gust"></span>Gust</span></div>
<div id="wind-reading-chart" class="wind-reading-chart" role="img" aria-label="Recent sustained wind and gust history"></div>
<div class="wind-readings-wrap"><table class="wind-readings-table"><thead><tr><th>Time</th><th>Dir</th><th>Wind</th><th>Gust</th><th>Age</th></tr></thead><tbody id="wind-readings-body">{{range .WindReadings}}<tr><td>{{.Time}}</td><td>{{.Direction}}</td><td>{{.Wind}}</td><td>{{.Gust}}</td><td>{{.Age}}</td></tr>{{end}}</tbody></table></div>
</div>{{end}}
</section>

<section id="offshore-trip-card" class="card full offshore-trip-card" hidden>
<div class="offshore-trip-head"><div><h2>Offshore Trip Planning</h2><div id="offshore-trip-coords" class="offshore-trip-coords"></div></div></div>
<div id="offshore-trip-loading" class="offshore-trip-loading">Select an offshore ★ destination to evaluate conditions.</div>
<div id="offshore-trip-content" hidden>
<div id="offshore-trip-summary" class="offshore-trip-summary"></div>
<div id="offshore-trip-buoy" class="offshore-trip-buoy"></div>
<div class="offshore-trip-metrics">
<div class="offshore-trip-metric"><div class="label">Observed wind</div><div id="offshore-trip-wind" class="value">—</div></div>
<div class="offshore-trip-metric"><div class="label">Gust</div><div id="offshore-trip-gust" class="value">—</div></div>
<div class="offshore-trip-metric"><div class="label">Wave height</div><div id="offshore-trip-wave" class="value">—</div></div>
<div class="offshore-trip-metric"><div class="label">Dominant period</div><div id="offshore-trip-period" class="value">—</div></div>
<div class="offshore-trip-metric"><div class="label">Wave direction</div><div id="offshore-trip-direction" class="value">—</div></div>
</div>
<div id="offshore-trip-watch-wrap" hidden><strong>Watch items</strong><ul id="offshore-trip-watch" class="offshore-trip-watch"></ul></div>
<div id="offshore-trip-forecast" class="offshore-trip-forecast"></div>
<p class="offshore-trip-note">Observed values come from the nearest usable NDBC station found near the selected destination. Forecast text and alerts come from the NWS forecast zone for the selected point. The buoy may be tens of miles from the destination and the zone forecast covers a broad area; use this as planning context, not a point-specific guarantee.</p>
<div id="fishing-planning" class="fishing-planning">
<div class="fishing-planning-head"><h3>Fishing Planning</h3><span id="fishing-planning-status" class="fishing-planning-status">Loading water and fishing context…</span></div>
<div id="fishing-planning-summary" class="fishing-planning-summary">Select a ★ destination to evaluate the fishing setup.</div>
<div class="fishing-planning-grid">
<div class="fishing-planning-item"><div class="label">Sea Surface Temp</div><div id="fishing-planning-sst" class="value">—</div><div id="fishing-planning-sst-detail" class="detail"></div></div>
<div class="fishing-planning-item"><div class="label">Chlorophyll</div><div id="fishing-planning-chl" class="value">—</div><div id="fishing-planning-chl-detail" class="detail"></div></div>
<div class="fishing-planning-item"><div class="label">Structure</div><div id="fishing-planning-structure" class="value">—</div><div id="fishing-planning-structure-detail" class="detail"></div></div>
<div class="fishing-planning-item"><div class="label">Recent reports</div><div id="fishing-planning-reports" class="value">—</div><div id="fishing-planning-reports-detail" class="detail"></div></div>
</div>
<p id="fishing-planning-error" class="fishing-planning-error" hidden></p>
</div>
</div>
<p id="offshore-trip-error" class="offshore-trip-error" hidden></p>
</section>

<section id="marine-forecast-card" class="card full marine-forecast-card" {{if not (or .MarineForecastPeriods .MarineForecastError)}}hidden{{end}}>
<div class="marine-forecast-head"><div><h2 id="marine-forecast-title">NWS Forecast{{if .MarineForecastStation}}{{if eq .MarineForecastStation "selected location"}} — selected location{{else}} — near {{.MarineForecastStation}}{{end}}{{end}}</h2><div id="marine-forecast-zone" class="marine-forecast-zone" {{if not .MarineForecastZone}}hidden{{end}}>National Weather Service forecast zone {{.MarineForecastZone}}{{if .MarineForecastUpdated}} · Marine forecast updated {{.MarineForecastUpdated}}{{end}}</div></div></div>
<div id="marine-forecast-alerts" class="marine-alerts" aria-label="Active National Weather Service alerts" {{if not .MarineForecastAlerts}}hidden{{end}}>{{range .MarineForecastAlerts}}<span class="marine-alert">⚠ NWS alert — {{.}}</span>{{end}}</div>
<div id="marine-forecast-periods" class="marine-periods" {{if not .MarineForecastPeriods}}hidden{{end}}>{{range .MarineForecastPeriods}}<div class="marine-period">{{if .Name}}<strong>{{.Name}}</strong>{{end}}{{if .Forecast}}<p>{{.Forecast}}</p>{{end}}</div>{{end}}</div>
<p id="marine-forecast-note" class="marine-forecast-note" {{if not .MarineForecastPeriods}}hidden{{end}}>Official NWS marine-zone forecast selected from the selected location. Forecasts and marine-zone advisories apply to the broader zone, not specifically to that point; use the map overlay to see the zone extent. Conditions can vary within the zone.</p>
<p id="marine-forecast-error" class="marine-forecast-error" {{if not .MarineForecastError}}hidden{{end}}>{{if .MarineForecastError}}{{.MarineForecastError}}{{end}}</p>
</section>

<section id="tide-context-card" class="card full"><h2>Tidal &amp; Lunar Context{{if .CurrentDateLabel}} — {{.CurrentDateLabel}}{{end}}</h2>{{if .TideContextMoon}}<p><strong>{{.TideContextMoon}}</strong></p>{{end}}{{if .TideContextCycle}}<p>{{.TideContextCycle}}</p>{{end}}{{if .TideContextStation}}<div class="station">{{.TideContextStation}}</div>{{end}}{{if .TideContextStationMeta}}<div class="meta">{{.TideContextStationMeta}}</div>{{end}}{{if .TideContextRange}}<p><strong>Tidal range context:</strong> {{.TideContextRange}}</p>{{end}}{{if .TideContextComparison}}<p><strong>{{.TideContextComparison}}</strong></p>{{end}}{{if .TideContextNote}}<p class="note">{{.TideContextNote}} NOAA does not provide a universal “king tide” classification here; the 28-day range comparison provides the quantitative context across roughly one lunar cycle.</p>{{end}}</section>


{{if .CurrentChart}}<section id="current-chart-card" class="card full"><div class="current-chart-header"><div><h2>Tidal Current</h2>{{if .CurrentRangeLabel}}<div class="current-date-label">{{.CurrentRangeLabel}}</div>{{end}}</div></div><div class="current-range-toolbar" aria-label="Current graph date controls"><a class="current-date-nav" href="{{.CurrentPrevURL}}" aria-label="Previous date range">← Previous range</a><label class="current-control-label"><span>Start date</span><input id="current-date-picker" class="current-date-picker" type="date" value="{{.CurrentDateISO}}" aria-label="Choose starting date"></label><label class="current-control-label"><span>Range</span><select id="current-days-picker" class="current-date-picker" aria-label="Number of days"><option value="1" {{if eq .CurrentDays 1}}selected{{end}}>1 day</option><option value="3" {{if eq .CurrentDays 3}}selected{{end}}>3 days</option><option value="7" {{if eq .CurrentDays 7}}selected{{end}}>7 days</option></select></label><a class="current-date-nav {{if .CurrentIsToday}}is-current{{end}}" href="{{.CurrentTodayURL}}">Today</a><a class="current-date-nav" href="{{.CurrentNextURL}}" aria-label="Next date range">Next range →</a></div>{{if .CurrentWindow}}<div class="current-window-inline"><strong>{{if .CurrentWindowMode}}{{.CurrentWindowMode}}{{else}}Conditions window{{end}}</strong><span>{{.CurrentWindow}}</span></div>{{end}}<div class="chart-explainer"><strong>This is current, not tide height.</strong> Above zero = flood; below zero = ebb; crossings = slack water.</div>{{if .TideRangeOverlayAvailable}}<div class="tide-range-legend"><label class="tide-range-toggle"><input id="show-tide-range-overlay" type="checkbox" checked> Show daily tidal range on right axis</label><span class="tide-range-key"><span class="tide-range-swatch typical"></span>{{if .TideRangeLegendTypical}}{{.TideRangeLegendTypical}}{{else}}Normal-cycle (&lt; +15%){{end}}</span><span class="tide-range-key"><span class="tide-range-swatch elevated"></span>{{if .TideRangeLegendElevated}}{{.TideRangeLegendElevated}}{{else}}Elevated (≥ +15%){{end}}</span><span class="tide-range-key"><span class="tide-range-swatch large"></span>{{if .TideRangeLegendLarge}}{{.TideRangeLegendLarge}}{{else}}Large (≥ +30%){{end}}</span><span class="tide-range-key"><span class="tide-range-swatch exceptional"></span>{{if .TideRangeLegendExceptional}}{{.TideRangeLegendExceptional}}{{else}}Exceptional (≥ +45%){{end}}</span></div>{{end}}<div class="current-chart-wrap">{{.CurrentChart}}</div><div class="chart-note">NOAA 6-minute harmonic current predictions. The current-speed axis stays at ±3.5 kt for date-to-date comparison and expands only when needed. Darker bands are night; light areas are daylight; warm bands mark the configured preferred planning period. When enabled, thin daily markers use a stable 0–10 ft right axis for predicted high-to-low tidal range, expanding only above 10 ft when needed; marker color is classified relative to the surrounding lunar-cycle median, where Normal-cycle means less than 15% above that median. {{if eq .CurrentDays 1}}Max flood, max ebb, and slack events are labeled with their times.{{else}}Small dots mark max flood, max ebb, and slack across the displayed range.{{end}} {{if .CurrentIsToday}}Red line marks report time when it falls inside the displayed range.{{end}}{{if gt .CurrentDays 1}} Day boundaries are emphasized for multi-day planning.{{end}}</div><div class="current-events-integrated"><div class="current-events-head"><strong>Key current times{{if .CurrentDateLabel}} — {{.CurrentDateLabel}}{{end}}</strong>{{if gt .CurrentDays 1}}<span>Selected start date only; graph covers {{.CurrentDays}} days.</span>{{end}}</div><div class="current-key-times">{{range .CurrentEvents}}<div class="current-key-time"><div class="current-key-time-time">{{.Time}}</div><div class="current-key-time-label"><strong>{{.Label}}</strong>{{if .Speed}}<span class="current-key-time-meta">{{.Speed}} · {{.Direction}}</span>{{end}}</div></div>{{else}}<p>No key current times in the conditions window.</p>{{end}}</div></div>{{if .CurrentPlanningHints}}<div class="current-planning"><div class="current-planning-head"><strong>Preferred-period planning hint{{if eq .CurrentDays 1}} — today / selected day{{end}}</strong><span>Current strength has separate caution and red-flag thresholds; the time buffer also warns about strong current just outside the preferred period.</span></div><div class="planning-preferences"><div class="planning-preferences-row"><label><span>Start</span><input id="planning-start" type="time" value="{{.PlanningStart}}" aria-label="Preferred period start"></label><label><span>End</span><input id="planning-end" type="time" value="{{.PlanningEnd}}" aria-label="Preferred period end"></label></div><div class="planning-preferences-row"><label><span>Ebb caution</span><input id="planning-caution-ebb" type="number" min="0.1" max="10" step="0.1" value="{{.PlanningCautionEbb}}" aria-label="Ebb caution threshold in knots"><b>kt</b></label><label><span>Ebb red</span><input id="planning-max-ebb" type="number" min="0.1" max="10" step="0.1" value="{{.PlanningMaxEbb}}" aria-label="Ebb red flag threshold in knots"><b>kt</b></label></div><div class="planning-preferences-row"><label><span>Flood caution</span><input id="planning-caution-flood" type="number" min="0.1" max="10" step="0.1" value="{{.PlanningCautionFlood}}" aria-label="Flood caution threshold in knots"><b>kt</b></label><label><span>Flood red</span><input id="planning-max-flood" type="number" min="0.1" max="10" step="0.1" value="{{.PlanningMaxFlood}}" aria-label="Flood red flag threshold in knots"><b>kt</b></label></div><div class="planning-preferences-row"><label><span>Caution time before/after period</span><input id="planning-buffer" type="number" min="0" max="360" step="15" value="{{.PlanningBuffer}}" aria-label="Caution time before or after preferred planning period in minutes"><b>min</b></label></div><div class="planning-preferences-row"><label><span>Currents station distance caution</span><input id="planning-current-distance-warning" type="number" min="0.1" max="{{.PlanningAutoCurrentLimit}}" step="0.1" value="{{.PlanningCurrentDistanceWarning}}" aria-label="Currents station distance caution threshold in nautical miles"><b>nmi</b></label></div></div><div class="planning-help"><strong>How these settings work:</strong> By default, ebb or flood below 2.0 kt is <strong>Preferred</strong>, 2.0 kt up to but not including 3.0 kt is <strong>Caution</strong>, and 3.0 kt or more during the preferred period is a <strong>Red flag</strong>. Ebb and flood thresholds can be adjusted independently. The caution time before/after period setting also warns when caution-level or stronger current occurs within that many minutes immediately before or after the preferred planning period; a threshold reached only there is reported as <strong>Caution</strong>. A currents station farther than the configured distance-caution threshold also makes the overall Conditions Now status <strong>Caution</strong>, without changing the current-strength classification. Automatic current-station selection will not use a station beyond {{.PlanningAutoCurrentLimit}} nmi.</div><div class="current-planning-days">{{range .CurrentPlanningHints}}<div class="planning-day {{.Class}}"><div class="planning-date">{{.Date}}</div><div class="planning-status">{{if eq .Class "preferred"}}✓{{else if eq .Class "redflag"}}⚠{{else}}△{{end}} {{.Status}}</div><div class="planning-detail">{{.Detail}}</div></div>{{end}}</div><div class="planning-disclaimer">Current-based planning hint only; wind, swell, weather, traffic, and local effects still matter.</div></div>{{end}}</section>{{end}}
<section class="card full map-sources-card"><h2>Map &amp; Data Sources</h2><p class="map-sources-note">Base maps: <strong>Street Map</strong> uses OpenStreetMap; <strong>Nautical Chart</strong> uses NOAA's ENC-based Chart Display Service and is available at Zoom 9 or closer; <strong>Satellite</strong> uses Esri World Imagery; <strong>Hybrid</strong> combines Esri imagery with place/boundary labels. NWS forecast-zone, NOAA smoke, <strong>Sea Surface Temp</strong>, <strong>Satellite Cloud Cover</strong>, and radar layers remain independent overlays. The nautical chart layer is for planning/reference and does not replace official navigation products.</p></section>
<section id="full-report-card" class="card full details-link-card"><div><h2>Need the details?</h2><p class="details-note">Open the complete text-style report, including diagnostic and supporting information.</p></div><a class="details-link" href="{{.FullDetailsURL}}">View full report details →</a></section>
{{end}}</div>
<div class="footer"><strong>Mauri's Weather & Water Conditions</strong><br>NOAA/NDBC observations + NWS forecast context + NOAA CO-OPS current predictions · Conditions-planning aid, not a navigation system<br>Version {{.AppVersion}} · Build {{.BuildVersion}}</div></main><div id="planning-loading-overlay" class="page-loading-overlay" aria-hidden="true"><div class="page-loading-box" role="status" aria-live="polite"><span class="page-loading-spinner" aria-hidden="true"></span><span>Loading Planning and Details…</span></div></div><script>
(function(){
  var el = document.getElementById("sailing-location-map");
  if (!el || typeof L === "undefined") return;

  var pageURL = new URL(window.location.href);
  var centerLatText = pageURL.searchParams.get("map_center_lat");
  var centerLonText = pageURL.searchParams.get("map_center_lon");
  var centerLat = centerLatText === null ? NaN : Number(centerLatText);
  var centerLon = centerLonText === null ? NaN : Number(centerLonText);
  if (!Number.isFinite(centerLat) || centerLat < -90 || centerLat > 90) centerLat = {{printf "%.6f" .MapCenterLat}};
  if (!Number.isFinite(centerLon) || centerLon < -180 || centerLon > 180) centerLon = {{printf "%.6f" .MapCenterLon}};
  var initialZoom = Number(pageURL.searchParams.get("map_zoom"));
  if (!Number.isFinite(initialZoom) || initialZoom < 3 || initialZoom > 18) initialZoom = 10;
  var map = L.map(el, {scrollWheelZoom:true}).setView([centerLat, centerLon], initialZoom);

  var mapScaleControl = L.control({position:"bottomright"});
  mapScaleControl.onAdd = function() {
    var box = L.DomUtil.create("div", "map-scale-status");
    box.setAttribute("role", "status");
    box.setAttribute("aria-live", "polite");
    box.setAttribute("title", "Approximate horizontal distance represented by 120 screen pixels at the map center");
    L.DomEvent.disableClickPropagation(box);
    this._box = box;
    return box;
  };
  mapScaleControl.addTo(map);

  function formatMapScaleDistance(value) {
    if (!Number.isFinite(value) || value < 0) return "—";
    if (value >= 100) return Math.round(value).toString();
    if (value >= 10) return value.toFixed(1);
    return value.toFixed(2);
  }

  function updateMapScaleStatus() {
    if (!mapScaleControl || !mapScaleControl._box) return;
    var size = map.getSize();
    if (!size || size.x <= 0 || size.y <= 0) return;
    var samplePixels = Math.min(120, Math.max(40, Math.round(size.x * 0.18)));
    var centerX = Math.round(size.x / 2);
    var centerY = Math.round(size.y / 2);
    var x0 = Math.max(0, centerX - Math.round(samplePixels / 2));
    var x1 = Math.min(size.x, centerX + Math.round(samplePixels / 2));
    var left = map.containerPointToLatLng([x0, centerY]);
    var right = map.containerPointToLatLng([x1, centerY]);
    var meters = map.distance(left, right);
    var nauticalMiles = meters / 1852;
    var statuteMiles = meters / 1609.344;
    mapScaleControl._box.textContent =
      "Scale (" + (x1 - x0) + " px): " +
      formatMapScaleDistance(nauticalMiles) + " nmi / " +
      formatMapScaleDistance(statuteMiles) + " mi · Zoom " + map.getZoom();
  }
  map.createPane("bathymetryOverlayPane");
  map.getPane("bathymetryOverlayPane").style.zIndex = 410;
  map.getPane("bathymetryOverlayPane").style.pointerEvents = "none";

  map.createPane("underseaNamesPane");
  map.getPane("underseaNamesPane").style.zIndex = 425;
  map.getPane("underseaNamesPane").style.pointerEvents = "none";

  map.createPane("fishingReportsPane");
  map.getPane("fishingReportsPane").style.zIndex = 428;


  map.createPane("sstOverlayPane");
  map.getPane("sstOverlayPane").style.zIndex = 420;
  map.getPane("sstOverlayPane").style.pointerEvents = "none";

  map.createPane("chlorophyllFieldPane");
  map.getPane("chlorophyllFieldPane").style.zIndex = 421;
  map.getPane("chlorophyllFieldPane").style.pointerEvents = "none";

  map.createPane("chlorophyllOverlayPane");
  map.getPane("chlorophyllOverlayPane").style.zIndex = 423;
  map.getPane("chlorophyllOverlayPane").style.pointerEvents = "none";

  map.createPane("cloudOverlayPane");
  map.getPane("cloudOverlayPane").style.zIndex = 430;
  map.getPane("cloudOverlayPane").style.pointerEvents = "none";

  map.createPane("radarOverlayPane");
  map.getPane("radarOverlayPane").style.zIndex = 440;
  map.getPane("radarOverlayPane").style.pointerEvents = "none";

  map.createPane("forecastZoneHaloPane");
  map.getPane("forecastZoneHaloPane").style.zIndex = 455;
  map.getPane("forecastZoneHaloPane").style.pointerEvents = "none";

  map.createPane("forecastZonePane");
  map.getPane("forecastZonePane").style.zIndex = 460;
  map.getPane("forecastZonePane").style.pointerEvents = "none";

  map.createPane("smokeOutlinePane");
  map.getPane("smokeOutlinePane").style.zIndex = 470;
  map.getPane("smokeOutlinePane").style.pointerEvents = "none";

  var resizeHandle = document.getElementById("map-resize-handle");
  if (resizeHandle) {
    var minMapHeight = 260;
    var maxMapHeight = function() {
      return Math.max(minMapHeight, Math.min(900, Math.floor(window.innerHeight * 0.85)));
    };
    var resizeStartY = 0;
    var resizeStartHeight = 0;
    var resizePointerID = null;

    function clampMapHeight(height) {
      return Math.max(minMapHeight, Math.min(maxMapHeight(), Math.round(height)));
    }

    function setMapHeight(height) {
      var nextHeight = clampMapHeight(height);
      el.style.height = nextHeight + "px";
      resizeHandle.setAttribute("aria-valuemax", String(maxMapHeight()));
      resizeHandle.setAttribute("aria-valuenow", String(nextHeight));
      map.invalidateSize({pan:false});
      updateMapScaleStatus();
    }

    function finishMapResize() {
      if (resizePointerID !== null && resizeHandle.hasPointerCapture &&
          resizeHandle.hasPointerCapture(resizePointerID)) {
        resizeHandle.releasePointerCapture(resizePointerID);
      }
      resizePointerID = null;
      resizeHandle.setAttribute("aria-grabbed", "false");
      document.body.classList.remove("map-resizing");
      map.invalidateSize({pan:false});
      updateMapScaleStatus();
    }

    resizeHandle.addEventListener("pointerdown", function(event) {
      if (event.button !== undefined && event.button !== 0) return;
      resizeStartY = event.clientY;
      resizeStartHeight = el.getBoundingClientRect().height;
      resizePointerID = event.pointerId;
      resizeHandle.setPointerCapture(event.pointerId);
      resizeHandle.setAttribute("aria-grabbed", "true");
      document.body.classList.add("map-resizing");
      event.preventDefault();
    });

    resizeHandle.addEventListener("pointermove", function(event) {
      if (resizePointerID === null || event.pointerId !== resizePointerID) return;
      setMapHeight(resizeStartHeight + (event.clientY - resizeStartY));
      event.preventDefault();
    });

    resizeHandle.addEventListener("pointerup", finishMapResize);
    resizeHandle.addEventListener("pointercancel", finishMapResize);

    resizeHandle.addEventListener("keydown", function(event) {
      var step = event.shiftKey ? 80 : 30;
      var currentHeight = el.getBoundingClientRect().height;
      if (event.key === "ArrowDown") {
        setMapHeight(currentHeight + step);
        event.preventDefault();
      } else if (event.key === "ArrowUp") {
        setMapHeight(currentHeight - step);
        event.preventDefault();
      } else if (event.key === "Home") {
        setMapHeight(minMapHeight);
        event.preventDefault();
      } else if (event.key === "End") {
        setMapHeight(maxMapHeight());
        event.preventDefault();
      }
    });

    resizeHandle.setAttribute(
      "aria-valuenow",
      String(Math.round(el.getBoundingClientRect().height))
    );
    resizeHandle.setAttribute("aria-valuemax", String(maxMapHeight()));
  }

  var streetLayer = L.tileLayer("https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png", {
    maxZoom: 18,
    attribution: '&copy; OpenStreetMap contributors'
  });

  var nauticalLayer = L.tileLayer.wms(
    "https://gis.charttools.noaa.gov/arcgis/rest/services/MCS/NOAAChartDisplay/MapServer/exts/MaritimeChartService/WMSServer",
    {
      layers: "0,1,2,3,4,5,6,7,8,9,10,11,12",
      format: "image/png",
      transparent: false,
      version: "1.3.0",
      attribution: "NOAA Office of Coast Survey"
    }
  );

  var satelliteLayer = L.tileLayer(
    "https://server.arcgisonline.com/ArcGIS/rest/services/World_Imagery/MapServer/tile/{z}/{y}/{x}",
    {
      maxZoom: 19,
      attribution: "Tiles &copy; Esri"
    }
  );

  var hybridImageryLayer = L.tileLayer(
    "https://server.arcgisonline.com/ArcGIS/rest/services/World_Imagery/MapServer/tile/{z}/{y}/{x}",
    {
      maxZoom: 19,
      attribution: "Tiles &copy; Esri"
    }
  );

  var hybridReferenceLayer = L.tileLayer(
    "https://server.arcgisonline.com/ArcGIS/rest/services/Reference/World_Boundaries_and_Places/MapServer/tile/{z}/{y}/{x}",
    {
      maxZoom: 19,
      attribution: "Reference &copy; Esri",
      pane: "overlayPane"
    }
  );

  var hybridLayer = L.layerGroup([hybridImageryLayer, hybridReferenceLayer]);

  var minimumNauticalZoom = 9;
  var nauticalZoomNote = document.getElementById("map-nautical-zoom-note");
  var nauticalTypeInput = document.getElementById("map-type-nautical");
  var nauticalTypeLabel = document.getElementById("map-type-nautical-label");

  var mapLayerParam = new URL(window.location.href).searchParams.get("map_layer");
  var preferredMapLayerName = "map";
  if (mapLayerParam === "nautical") preferredMapLayerName = "nautical";
  else if (mapLayerParam === "satellite") preferredMapLayerName = "satellite";
  else if (mapLayerParam === "hybrid") preferredMapLayerName = "hybrid";

  // activeMapLayerName is the basemap actually visible on the map. It can
  // temporarily differ from preferredMapLayerName when Nautical is selected
  // but the map is zoomed farther out than the NOAA chart is useful.
  var activeMapLayerName = "map";

  function baseLayerForName(name) {
    if (name === "nautical") return nauticalLayer;
    if (name === "satellite") return satelliteLayer;
    if (name === "hybrid") return hybridLayer;
    return streetLayer;
  }

  function effectiveMapLayerName() {
    if (preferredMapLayerName === "nautical" && map.getZoom() < minimumNauticalZoom) {
      return "map";
    }
    return preferredMapLayerName;
  }

  function updateNauticalZoomUI() {
    var belowMinimum = map.getZoom() < minimumNauticalZoom;
    if (nauticalTypeInput) {
      nauticalTypeInput.disabled = belowMinimum;
      nauticalTypeInput.setAttribute(
        "aria-disabled",
        belowMinimum ? "true" : "false"
      );
      nauticalTypeInput.title = belowMinimum
        ? "Nautical chart is available at Zoom 9 or closer."
        : "Use NOAA Nautical Chart.";
    }
    if (nauticalTypeLabel) {
      nauticalTypeLabel.classList.toggle("is-unavailable", belowMinimum);
    }
    if (nauticalZoomNote) {
      var showNotice = preferredMapLayerName === "nautical" && belowMinimum;
      nauticalZoomNote.hidden = !showNotice;
      if (showNotice) {
        nauticalZoomNote.textContent =
          "Nautical chart available at Zoom 9+ — showing Street Map temporarily.";
      }
    }
  }

  function persistPreferredMapLayer() {
    var targetURL = new URL(window.location.href);
    if (preferredMapLayerName === "map") targetURL.searchParams.delete("map_layer");
    else targetURL.searchParams.set("map_layer", preferredMapLayerName);
    window.history.replaceState({}, "", targetURL.toString());
  }

  function applyPreferredBaseMap() {
    var effectiveName = effectiveMapLayerName();
    var targetLayer = baseLayerForName(effectiveName);

    [streetLayer, nauticalLayer, satelliteLayer, hybridLayer].forEach(function(layer) {
      if (layer !== targetLayer && map.hasLayer(layer)) map.removeLayer(layer);
    });
    if (!map.hasLayer(targetLayer)) targetLayer.addTo(map);

    activeMapLayerName = effectiveName;
    updateNauticalZoomUI();

    document.querySelectorAll('input[name="map-type"]').forEach(function(input) {
      input.checked = input.value === preferredMapLayerName;
    });

    if (sstLayer && mapState.sstOverlayVisible && map.hasLayer(sstLayer)) {
      sstLayer.bringToFront();
    }
    if (cloudLayer && mapState.cloudOverlayVisible && map.hasLayer(cloudLayer)) {
      cloudLayer.bringToFront();
    }
    if (radarLayer && mapState.radarOverlayVisible && map.hasLayer(radarLayer)) {
      radarLayer.bringToFront();
    }
    if (marineZoneHaloLayer && map.hasLayer(marineZoneHaloLayer)) {
      marineZoneHaloLayer.setStyle(marineZoneHaloStyle);
      marineZoneHaloLayer.bringToFront();
    }
    if (marineZoneLayer && map.hasLayer(marineZoneLayer)) {
      marineZoneLayer.setStyle(marineZoneStyle);
      marineZoneLayer.bringToFront();
    }
    if (smokeLayer && map.hasLayer(smokeLayer)) {
      smokeLayer.setStyle(smokeStyle);
    }
    if (smokeOutlineLayer && map.hasLayer(smokeOutlineLayer)) {
      smokeOutlineLayer.setStyle(smokeOutlineStyle);
      smokeOutlineLayer.bringToFront();
    }
    updateSmokeLegendForBaseMap();
  }

  function setBaseMapLayer(name) {
    name = String(name || "map").toLowerCase();
    if (name !== "nautical" && name !== "satellite" && name !== "hybrid") {
      name = "map";
    }
    if (name === "nautical" && map.getZoom() < minimumNauticalZoom) {
      updateNauticalZoomUI();
      return;
    }

    preferredMapLayerName = name;
    persistPreferredMapLayer();
    applyPreferredBaseMap();
  }

  applyPreferredBaseMap();


  // These overlay objects must remain live for the checkbox handlers below.
  // Do not reinitialize cloudLayer or radarLayer to null later in this script.
  var sstLatestTime = "";
  var sstMetadataLoaded = false;
  var sstMetadataLoading = false;
  var sstLayer = null;
  var sstObjectURL = "";
  var sstRequestSerial = 0;
  var sstRefreshTimer = null;

  var chlLatestTime = "";
  var chlMetadataLoaded = false;
  var chlMetadataLoading = false;
  var chlLayer = null;
  var chlObjectURL = "";
  var chlRequestSerial = 0;
  var chlRefreshTimer = null;

  var chlFieldLayer = null;
  var chlFieldObjectURL = "";
  var chlFieldRequestSerial = 0;
  var chlFieldRefreshTimer = null;

  var structureReliefLayer = null;
  var structureNamesLayer = null;
  var structureRequestSerial = 0;
  var structureRefreshTimer = null;
  var fishingReportsLayer = null;
  var structureReliefServiceURL =
    "https://gis.ngdc.noaa.gov/arcgis/rest/services/etopo1/MapServer/export";
  var structureNamesServiceURL =
    "https://coast.noaa.gov/arcgis/rest/services/MarineCadastre/UnderseaFeaturePlaceNames/MapServer/0/query";

  var cloudLayer = null;
  var cloudRequestSerial = 0;
  var cloudRefreshTimer = null;
  var cloudImageServiceURL =
    "https://satellitemaps.nesdis.noaa.gov/arcgis/rest/services/Most_Recent_MERGEDGC/ImageServer/exportImage";

  var radarLayer = L.tileLayer.wms(
    "https://mesonet.agron.iastate.edu/cgi-bin/wms/nexrad/n0q.cgi?",
    {
      layers: "nexrad-n0q-900913",
      format: "image/png",
      transparent: true,
      version: "1.1.1",
      opacity: 0.72,
      pane: "radarOverlayPane",
      attribution: "NWS NEXRAD radar via Iowa State IEM"
    }
  );

  function setSSTStatus(message, isError) {
    var status = document.getElementById("map-sst-status");
    if (!status) return;
    message = String(message || "").trim();
    status.hidden = !message;
    status.textContent = message;
    status.style.color = isError ? "#9a352f" : "";
  }

  function setSSTLegendVisible(visible) {
    var legend = document.getElementById("map-sst-legend");
    if (legend) legend.hidden = !visible;
  }

  function formatSSTDatasetTime(value) {
    var parsed = new Date(value);
    if (!Number.isFinite(parsed.getTime())) return String(value || "");
    return parsed.toLocaleString([], {
      month: "short",
      day: "numeric",
      hour: "numeric",
      minute: "2-digit",
      timeZoneName: "short"
    });
  }

  function ensureSSTMetadata(callback) {
    if (sstMetadataLoaded) {
      callback(true);
      return;
    }
    if (sstMetadataLoading) {
      window.setTimeout(function() { ensureSSTMetadata(callback); }, 120);
      return;
    }

    sstMetadataLoading = true;
    fetch("/sst-info", {headers: {"Accept":"application/json"}})
      .then(function(response) {
        if (!response.ok) throw new Error("HTTP " + response.status);
        return response.json();
      })
      .then(function(payload) {
        sstMetadataLoading = false;
        sstLatestTime = String(payload.time || "");
        if (!sstLatestTime) throw new Error("missing dataset time");
        sstMetadataLoaded = true;
        callback(true);
      })
      .catch(function(err) {
        sstMetadataLoading = false;
        // ERDDAP defines time=current as the latest available field, so
        // metadata failure must not prevent the SST image from rendering.
        sstLatestTime = "current";
        sstMetadataLoaded = true;
        setSSTStatus("NOAA CoastWatch SST · using latest available daily field.", false);
        callback(true);
      });
  }

  function sstOverlayURL(bounds) {
    var mapSize = map.getSize();
    // ERDDAP renders this raster on demand. Keep the request modest and avoid
    // Retina/device-pixel doubling so the overlay loads reliably.
    var width = Math.max(256, Math.min(1200, Math.round(mapSize.x)));
    var height = Math.max(256, Math.min(900, Math.round(mapSize.y)));
    var params = new URLSearchParams();
    params.set("west", bounds.getWest().toFixed(6));
    params.set("south", bounds.getSouth().toFixed(6));
    params.set("east", bounds.getEast().toFixed(6));
    params.set("north", bounds.getNorth().toFixed(6));
    params.set("width", String(width));
    params.set("height", String(height));
    if (sstLatestTime) params.set("time", sstLatestTime);
    return "/sst-overlay?" + params.toString();
  }

  function refreshSSTOverlay() {
    if (!mapState.sstOverlayVisible || !sstMetadataLoaded) return;

    var bounds = map.getBounds();
    if (bounds.getWest() < -180 || bounds.getEast() > 180) {
      setSSTStatus(
        "Satellite SST is unavailable while the map view crosses the international date line.",
        true
      );
      return;
    }

    var requestSerial = ++sstRequestSerial;
    var overlayURL = sstOverlayURL(bounds);

    setSSTLegendVisible(false);
    setSSTStatus("Loading NOAA CoastWatch SST image…", false);

    var sstUpstreamLabel = "";
    fetch(overlayURL, {
      headers: {"Accept": "image/png"}
    })
      .then(function(response) {
        sstUpstreamLabel = String(response.headers.get("X-SST-Upstream") || "").trim();
        var contentType = String(response.headers.get("Content-Type") || "").toLowerCase();
        if (!response.ok) {
          return response.text().then(function(detail) {
            detail = String(detail || "").trim();
            if (detail.length > 1200) detail = detail.slice(0, 1200) + "…";
            throw new Error(detail || ("HTTP " + response.status));
          });
        }
        if (contentType.indexOf("image/png") === -1) {
          return response.text().then(function(detail) {
            detail = String(detail || "").trim();
            if (detail.length > 1200) detail = detail.slice(0, 1200) + "…";
            throw new Error(
              "Expected PNG from SST proxy but received " +
              (contentType || "an unknown content type") +
              (detail ? ": " + detail : "")
            );
          });
        }
        return response.blob();
      })
      .then(function(blob) {
        if (!mapState.sstOverlayVisible || requestSerial !== sstRequestSerial) return;

        var objectURL = URL.createObjectURL(blob);
        var nextLayer = L.imageOverlay(objectURL, bounds, {
          opacity: 0.72,
          pane: "sstOverlayPane",
          interactive: false,
          attribution: "NOAA CoastWatch Geo-Polar Blended SST"
        });

        nextLayer.once("load", function() {
          if (!mapState.sstOverlayVisible || requestSerial !== sstRequestSerial) {
            if (map.hasLayer(nextLayer)) map.removeLayer(nextLayer);
            URL.revokeObjectURL(objectURL);
            return;
          }

          var previousLayer = sstLayer;
          var previousObjectURL = sstObjectURL;
          sstLayer = nextLayer;
          sstObjectURL = objectURL;
          sstLayer.bringToFront();

          if (previousLayer && previousLayer !== sstLayer && map.hasLayer(previousLayer)) {
            map.removeLayer(previousLayer);
          }
          if (previousObjectURL && previousObjectURL !== sstObjectURL) {
            URL.revokeObjectURL(previousObjectURL);
          }

          setSSTLegendVisible(true);
          var sstTimeLabel = sstLatestTime === "current"
            ? "latest available daily field"
            : "latest daily field: " + formatSSTDatasetTime(sstLatestTime);
          var upstreamSuffix = sstUpstreamLabel ? " · via " + sstUpstreamLabel : "";
          setSSTStatus("NOAA CoastWatch Geo-Polar SST · fixed 45–75°F temp-break scale · " + sstTimeLabel + upstreamSuffix + " · IPv4", false);

          if (cloudLayer && mapState.cloudOverlayVisible && map.hasLayer(cloudLayer)) cloudLayer.bringToFront();
          if (radarLayer && mapState.radarOverlayVisible && map.hasLayer(radarLayer)) radarLayer.bringToFront();
        });

        nextLayer.once("error", function() {
          if (map.hasLayer(nextLayer)) map.removeLayer(nextLayer);
          URL.revokeObjectURL(objectURL);
          if (requestSerial !== sstRequestSerial || !mapState.sstOverlayVisible) return;
          setSSTLegendVisible(false);
          setSSTStatus("SST PNG was received but could not be displayed by the map.", true);
        });

        nextLayer.addTo(map);
        nextLayer.bringToFront();
      })
      .catch(function(err) {
        if (requestSerial !== sstRequestSerial || !mapState.sstOverlayVisible) return;
        setSSTLegendVisible(false);
        var detail = String(err && err.message ? err.message : err || "unknown error").trim();
        setSSTStatus("NOAA CoastWatch Sea Surface Temp request failed: " + detail, true);
      });
  }

  function scheduleSSTRefresh() {
    if (!mapState.sstOverlayVisible) return;
    if (sstRefreshTimer) window.clearTimeout(sstRefreshTimer);
    sstRefreshTimer = window.setTimeout(function() {
      sstRefreshTimer = null;
      if (!sstMetadataLoaded) {
        setSSTStatus("Loading latest NOAA CoastWatch sea-surface temperature…", false);
        ensureSSTMetadata(function(ok) {
          if (!mapState.sstOverlayVisible || !ok) return;
          refreshSSTOverlay();
        });
        return;
      }
      refreshSSTOverlay();
    }, 180);
  }

  function setSSTOverlayVisible(visible) {
    mapState.sstOverlayVisible = !!visible;
    if (!mapState.sstOverlayVisible) {
      sstRequestSerial++;
      if (sstRefreshTimer) {
        window.clearTimeout(sstRefreshTimer);
        sstRefreshTimer = null;
      }
      if (sstLayer && map.hasLayer(sstLayer)) map.removeLayer(sstLayer);
      sstLayer = null;
      if (sstObjectURL) {
        URL.revokeObjectURL(sstObjectURL);
        sstObjectURL = "";
      }
      setSSTLegendVisible(false);
      setSSTStatus("", false);
      return;
    }

    setSSTStatus("Loading latest NOAA CoastWatch sea-surface temperature…", false);
    ensureSSTMetadata(function(ok) {
      if (!mapState.sstOverlayVisible || !ok) return;
      refreshSSTOverlay();
    });
  }

  function setChlorophyllFieldStatus(message, isError) {
    var status = document.getElementById("map-chl-field-status");
    if (!status) return;
    message = String(message || "").trim();
    status.hidden = !message;
    status.textContent = message;
    status.style.color = isError ? "#9a352f" : "";
  }

  function setChlorophyllFieldLegendVisible(visible) {
    var legend = document.getElementById("map-chl-field-legend");
    if (legend) legend.hidden = !visible;
  }

  function chlorophyllFieldRequest(bounds) {
    var mapSize = map.getSize();
    var width = Math.max(256, Math.min(1400, Math.round(mapSize.x)));
    var height = Math.max(256, Math.min(1000, Math.round(mapSize.y)));
    var params = new URLSearchParams();
    params.set("west", bounds.getWest().toFixed(6));
    params.set("south", bounds.getSouth().toFixed(6));
    params.set("east", bounds.getEast().toFixed(6));
    params.set("north", bounds.getNorth().toFixed(6));
    params.set("width", String(width));
    params.set("height", String(height));
    return "/chlorophyll-field?" + params.toString();
  }

  function refreshChlorophyllField() {
    if (!mapState.chlorophyllFieldVisible || !chlMetadataLoaded) return;

    var bounds = map.getBounds();
    if (bounds.getWest() < -180 || bounds.getEast() > 180) {
      setChlorophyllFieldStatus(
        "Satellite chlorophyll is unavailable while the map view crosses the international date line.",
        true
      );
      return;
    }

    var requestSerial = ++chlFieldRequestSerial;
    var fieldURL = chlorophyllFieldRequest(bounds);
    setChlorophyllFieldLegendVisible(false);
    setChlorophyllFieldStatus("Loading NOAA CoastWatch chlorophyll field…", false);

    var upstreamLabel = "";
    var servedTime = "";
    fetch(fieldURL, {headers: {"Accept":"image/png"}})
      .then(function(response) {
        upstreamLabel = String(response.headers.get("X-Chlorophyll-Upstream") || "").trim();
        servedTime = String(response.headers.get("X-Chlorophyll-Time") || "").trim();
        var contentType = String(response.headers.get("Content-Type") || "").toLowerCase();
        if (!response.ok) {
          return response.text().then(function(detail) {
            detail = String(detail || "").trim();
            if (detail.length > 1200) detail = detail.slice(0, 1200) + "…";
            throw new Error(detail || ("HTTP " + response.status));
          });
        }
        if (contentType.indexOf("image/png") === -1) {
          throw new Error("Expected PNG from chlorophyll field proxy but received " + contentType);
        }
        return response.blob();
      })
      .then(function(blob) {
        if (!mapState.chlorophyllFieldVisible || requestSerial !== chlFieldRequestSerial) return;

        var objectURL = URL.createObjectURL(blob);
        var nextLayer = L.imageOverlay(objectURL, bounds, {
          opacity: 0.34,
          pane: "chlorophyllFieldPane",
          interactive: false,
          attribution: "NOAA CoastWatch DINEOF chlorophyll field"
        });

        nextLayer.once("load", function() {
          if (!mapState.chlorophyllFieldVisible || requestSerial !== chlFieldRequestSerial) {
            if (map.hasLayer(nextLayer)) map.removeLayer(nextLayer);
            URL.revokeObjectURL(objectURL);
            return;
          }

          var previousLayer = chlFieldLayer;
          var previousObjectURL = chlFieldObjectURL;
          chlFieldLayer = nextLayer;
          chlFieldObjectURL = objectURL;

          if (previousLayer && previousLayer !== chlFieldLayer && map.hasLayer(previousLayer)) {
            map.removeLayer(previousLayer);
          }
          if (previousObjectURL && previousObjectURL !== chlFieldObjectURL) {
            URL.revokeObjectURL(previousObjectURL);
          }

          setChlorophyllFieldLegendVisible(true);
          var displayTime = servedTime || chlLatestTime;
          var timeLabel = !displayTime || displayTime === "current"
            ? "latest available daily field"
            : "data field: " + formatChlorophyllDatasetTime(displayTime);
          var upstreamSuffix = upstreamLabel ? " · via " + upstreamLabel : "";
          setChlorophyllFieldStatus(
            "NOAA DINEOF chlorophyll field · gap-filled 2 km · 0.1–1 mg/m³ display · " +
            timeLabel + upstreamSuffix + " · IPv4",
            false
          );

          if (chlLayer && mapState.chlorophyllOverlayVisible && map.hasLayer(chlLayer)) chlLayer.bringToFront();
          if (structureNamesLayer && mapState.structureOverlayVisible && map.hasLayer(structureNamesLayer)) {
            structureNamesLayer.eachLayer(function(layer) {
              if (layer.bringToFront) layer.bringToFront();
            });
          }
        });

        nextLayer.once("error", function() {
          if (map.hasLayer(nextLayer)) map.removeLayer(nextLayer);
          URL.revokeObjectURL(objectURL);
          if (requestSerial !== chlFieldRequestSerial || !mapState.chlorophyllFieldVisible) return;
          setChlorophyllFieldLegendVisible(false);
          setChlorophyllFieldStatus("Chlorophyll field PNG was received but could not be displayed by the map.", true);
        });

        nextLayer.addTo(map);
      })
      .catch(function(err) {
        if (requestSerial !== chlFieldRequestSerial || !mapState.chlorophyllFieldVisible) return;
        setChlorophyllFieldLegendVisible(false);
        var detail = String(err && err.message ? err.message : err || "unknown error").trim();
        setChlorophyllFieldStatus("NOAA CoastWatch chlorophyll field request failed: " + detail, true);
      });
  }

  function scheduleChlorophyllFieldRefresh() {
    if (!mapState.chlorophyllFieldVisible) return;
    if (chlFieldRefreshTimer) window.clearTimeout(chlFieldRefreshTimer);
    chlFieldRefreshTimer = window.setTimeout(function() {
      chlFieldRefreshTimer = null;
      if (!chlMetadataLoaded) {
        ensureChlorophyllMetadata(function(ok) {
          if (!mapState.chlorophyllFieldVisible || !ok) return;
          refreshChlorophyllField();
        });
        return;
      }
      refreshChlorophyllField();
    }, 180);
  }

  function setChlorophyllFieldVisible(visible) {
    mapState.chlorophyllFieldVisible = !!visible;
    if (!mapState.chlorophyllFieldVisible) {
      chlFieldRequestSerial++;
      if (chlFieldRefreshTimer) {
        window.clearTimeout(chlFieldRefreshTimer);
        chlFieldRefreshTimer = null;
      }
      if (chlFieldLayer && map.hasLayer(chlFieldLayer)) map.removeLayer(chlFieldLayer);
      chlFieldLayer = null;
      if (chlFieldObjectURL) {
        URL.revokeObjectURL(chlFieldObjectURL);
        chlFieldObjectURL = "";
      }
      setChlorophyllFieldLegendVisible(false);
      setChlorophyllFieldStatus("", false);
      return;
    }

    setChlorophyllFieldStatus("Loading latest NOAA CoastWatch chlorophyll field…", false);
    ensureChlorophyllMetadata(function(ok) {
      if (!mapState.chlorophyllFieldVisible || !ok) return;
      refreshChlorophyllField();
    });
  }

  function setChlorophyllStatus(message, isError) {
    var status = document.getElementById("map-chl-status");
    if (!status) return;
    message = String(message || "").trim();
    status.hidden = !message;
    status.textContent = message;
    status.style.color = isError ? "#9a352f" : "";
  }

  function setChlorophyllLegendVisible(visible) {
    var legend = document.getElementById("map-chl-legend");
    if (legend) legend.hidden = !visible;
  }

  function formatChlorophyllDatasetTime(value) {
    var parsed = new Date(value);
    if (!Number.isFinite(parsed.getTime())) return String(value || "");
    return parsed.toLocaleString([], {
      month: "short",
      day: "numeric",
      hour: "numeric",
      minute: "2-digit",
      timeZoneName: "short"
    });
  }

  function ensureChlorophyllMetadata(callback) {
    if (chlMetadataLoaded) {
      callback(true);
      return;
    }
    if (chlMetadataLoading) {
      window.setTimeout(function() { ensureChlorophyllMetadata(callback); }, 120);
      return;
    }

    chlMetadataLoading = true;
    fetch("/chlorophyll-info", {headers: {"Accept":"application/json"}})
      .then(function(response) {
        if (!response.ok) throw new Error("HTTP " + response.status);
        return response.json();
      })
      .then(function(payload) {
        chlMetadataLoading = false;
        chlLatestTime = String(payload.time || "");
        if (!chlLatestTime) throw new Error("missing dataset time");
        chlMetadataLoaded = true;
        callback(true);
      })
      .catch(function() {
        chlMetadataLoading = false;
        chlLatestTime = "current";
        chlMetadataLoaded = true;
        setChlorophyllStatus(
          "NOAA CoastWatch chlorophyll · using latest available daily field.",
          false
        );
        callback(true);
      });
  }

  function chlorophyllOverlayRequest(bounds) {
    var mapSize = map.getSize();
    var width = Math.max(256, Math.min(1400, Math.round(mapSize.x)));
    var height = Math.max(256, Math.min(1000, Math.round(mapSize.y)));
    var params = new URLSearchParams();
    params.set("west", bounds.getWest().toFixed(6));
    params.set("south", bounds.getSouth().toFixed(6));
    params.set("east", bounds.getEast().toFixed(6));
    params.set("north", bounds.getNorth().toFixed(6));
    params.set("width", String(width));
    params.set("height", String(height));
    return {
      url: "/chlorophyll-overlay?" + params.toString(),
      bounds: bounds
    };
  }

  function refreshChlorophyllOverlay() {
    if (!mapState.chlorophyllOverlayVisible || !chlMetadataLoaded) return;

    var bounds = map.getBounds();
    if (bounds.getWest() < -180 || bounds.getEast() > 180) {
      setChlorophyllStatus(
        "Satellite chlorophyll is unavailable while the map view crosses the international date line.",
        true
      );
      return;
    }

    var request = chlorophyllOverlayRequest(bounds);
    var requestSerial = ++chlRequestSerial;
    var overlayURL = request.url;
    var overlayBounds = request.bounds;

    setChlorophyllLegendVisible(false);
    setChlorophyllStatus("Loading NOAA CoastWatch chlorophyll image…", false);

    var upstreamLabel = "";
    var servedTime = "";
    fetch(overlayURL, {headers: {"Accept":"image/png"}})
      .then(function(response) {
        upstreamLabel = String(response.headers.get("X-Chlorophyll-Upstream") || "").trim();
        servedTime = String(response.headers.get("X-Chlorophyll-Time") || "").trim();
        var contentType = String(response.headers.get("Content-Type") || "").toLowerCase();
        if (!response.ok) {
          return response.text().then(function(detail) {
            detail = String(detail || "").trim();
            if (detail.length > 500) detail = detail.slice(0, 500) + "…";
            throw new Error(detail || ("HTTP " + response.status));
          });
        }
        if (contentType.indexOf("image/png") === -1) {
          return response.text().then(function(detail) {
            detail = String(detail || "").trim();
            if (detail.length > 500) detail = detail.slice(0, 500) + "…";
            throw new Error(
              "Expected PNG from chlorophyll proxy but received " +
              (contentType || "an unknown content type") +
              (detail ? ": " + detail : "")
            );
          });
        }
        return response.blob();
      })
      .then(function(blob) {
        if (!mapState.chlorophyllOverlayVisible || requestSerial !== chlRequestSerial) return;

        var objectURL = URL.createObjectURL(blob);
        var nextLayer = L.imageOverlay(objectURL, overlayBounds, {
          opacity: 0.98,
          pane: "chlorophyllOverlayPane",
          interactive: false,
          attribution: "NOAA CoastWatch DINEOF chlorophyll contours"
        });

        nextLayer.once("load", function() {
          if (!mapState.chlorophyllOverlayVisible || requestSerial !== chlRequestSerial) {
            if (map.hasLayer(nextLayer)) map.removeLayer(nextLayer);
            URL.revokeObjectURL(objectURL);
            return;
          }

          var previousLayer = chlLayer;
          var previousObjectURL = chlObjectURL;
          chlLayer = nextLayer;
          chlObjectURL = objectURL;
          chlLayer.bringToFront();

          if (previousLayer && previousLayer !== chlLayer && map.hasLayer(previousLayer)) {
            map.removeLayer(previousLayer);
          }
          if (previousObjectURL && previousObjectURL !== chlObjectURL) {
            URL.revokeObjectURL(previousObjectURL);
          }

          setChlorophyllLegendVisible(true);
          var displayTime = servedTime || chlLatestTime;
          var timeLabel = !displayTime || displayTime === "current"
            ? "latest available daily field"
            : "data field: " + formatChlorophyllDatasetTime(displayTime);
          var upstreamSuffix = upstreamLabel ? " · via " + upstreamLabel : "";
          setChlorophyllStatus(
            "NOAA DINEOF chlorophyll contours · 0.2 / 0.3 / 0.5 mg/m³ · " +
            timeLabel + upstreamSuffix + " · IPv4",
            false
          );

          if (structureNamesLayer && mapState.structureOverlayVisible && map.hasLayer(structureNamesLayer)) {
            structureNamesLayer.eachLayer(function(layer) {
              if (layer.bringToFront) layer.bringToFront();
            });
          }
          if (cloudLayer && mapState.cloudOverlayVisible && map.hasLayer(cloudLayer)) cloudLayer.bringToFront();
          if (radarLayer && mapState.radarOverlayVisible && map.hasLayer(radarLayer)) radarLayer.bringToFront();
        });

        nextLayer.once("error", function() {
          if (map.hasLayer(nextLayer)) map.removeLayer(nextLayer);
          URL.revokeObjectURL(objectURL);
          if (requestSerial !== chlRequestSerial || !mapState.chlorophyllOverlayVisible) return;
          setChlorophyllLegendVisible(false);
          setChlorophyllStatus("Chlorophyll contour PNG was received but could not be displayed by the map.", true);
        });

        nextLayer.addTo(map);
        nextLayer.bringToFront();
      })
      .catch(function(err) {
        if (requestSerial !== chlRequestSerial || !mapState.chlorophyllOverlayVisible) return;
        setChlorophyllLegendVisible(false);
        var detail = String(err && err.message ? err.message : err || "unknown error").trim();
        setChlorophyllStatus("NOAA CoastWatch chlorophyll request failed: " + detail, true);
      });
  }

  function scheduleChlorophyllRefresh() {
    if (!mapState.chlorophyllOverlayVisible) return;
    if (chlRefreshTimer) window.clearTimeout(chlRefreshTimer);
    chlRefreshTimer = window.setTimeout(function() {
      chlRefreshTimer = null;
      if (!chlMetadataLoaded) {
        setChlorophyllStatus("Loading latest NOAA CoastWatch chlorophyll…", false);
        ensureChlorophyllMetadata(function(ok) {
          if (!mapState.chlorophyllOverlayVisible || !ok) return;
          refreshChlorophyllOverlay();
        });
        return;
      }
      refreshChlorophyllOverlay();
    }, 180);
  }

  function setChlorophyllOverlayVisible(visible) {
    mapState.chlorophyllOverlayVisible = !!visible;
    if (!mapState.chlorophyllOverlayVisible) {
      chlRequestSerial++;
      if (chlRefreshTimer) {
        window.clearTimeout(chlRefreshTimer);
        chlRefreshTimer = null;
      }
      if (chlLayer && map.hasLayer(chlLayer)) map.removeLayer(chlLayer);
      chlLayer = null;
      if (chlObjectURL) {
        URL.revokeObjectURL(chlObjectURL);
        chlObjectURL = "";
      }
      setChlorophyllLegendVisible(false);
      setChlorophyllStatus("", false);
      return;
    }

    setChlorophyllStatus("Loading latest NOAA CoastWatch chlorophyll…", false);
    ensureChlorophyllMetadata(function(ok) {
      if (!mapState.chlorophyllOverlayVisible || !ok) return;
      refreshChlorophyllOverlay();
    });
  }

  var fishingReportsPayload = null;
  var fishingReportsLoading = false;
  var fishingReportsLoadCallbacks = [];

  function loadFishingReports(callback) {
    if (fishingReportsPayload) {
      callback(true, fishingReportsPayload);
      return;
    }
    fishingReportsLoadCallbacks.push(callback);
    if (fishingReportsLoading) return;

    fishingReportsLoading = true;
    fetch("/fishing-reports", {headers: {"Accept":"application/json"}})
      .then(function(response) {
        if (!response.ok) {
          return response.text().then(function(detail) {
            detail = String(detail || "").trim();
            throw new Error(detail || ("HTTP " + response.status));
          });
        }
        return response.json();
      })
      .then(function(payload) {
        if (!payload || !Array.isArray(payload.reports)) {
          throw new Error("fishing report feed is missing reports[]");
        }
        fishingReportsPayload = payload;
        fishingReportsLoading = false;
        var callbacks = fishingReportsLoadCallbacks.slice();
        fishingReportsLoadCallbacks = [];
        callbacks.forEach(function(fn) { fn(true, fishingReportsPayload); });
      })
      .catch(function(err) {
        fishingReportsLoading = false;
        var callbacks = fishingReportsLoadCallbacks.slice();
        fishingReportsLoadCallbacks = [];
        callbacks.forEach(function(fn) { fn(false, err); });
      });
  }

  function setFishingReportsStatus(message, isError) {
    var status = document.getElementById("map-fishing-status");
    if (!status) return;
    message = String(message || "").trim();
    status.hidden = !message;
    status.textContent = message;
    status.style.color = isError ? "#9a352f" : "";
  }

  function setFishingReportsLegendVisible(visible) {
    var legend = document.getElementById("map-fishing-legend");
    if (legend) legend.hidden = !visible;
  }

  function fishingSpeciesStyle(species) {
    if (String(species || "").toLowerCase() === "bluefin") {
      return {color:"#17366f", fillColor:"#2658b8"};
    }
    return {color:"#0b5b5b", fillColor:"#18a6a6"};
  }

  function fishingPopupHTML(report) {
    var html = '<div style="min-width:220px">';
    html += '<strong>' + escapeHTML(report.species) + ' · ' + escapeHTML(report.date) + '</strong>';
    if (report.count) html += '<br>' + escapeHTML(report.count);
    if (report.size) html += ' · ' + escapeHTML(report.size);
    if (report.locationText) html += '<br><strong>Location:</strong> ' + escapeHTML(report.locationText);
    if (report.position_type === "derived_point" || report.confidence === "derived") {
      html += '<br><strong>Position:</strong> approximate, derived from report shorthand';
    } else if (report.position_type === "region" || report.confidence === "regional") {
      html += '<br><strong>Position:</strong> broad regional report';
    }
    if (report.notes) html += '<br><span style="color:#526a74">' + escapeHTML(report.notes) + '</span>';
    if (report.sourceURL) {
      html += '<br><a href="' + escapeHTML(report.sourceURL) + '" target="_blank" rel="noopener noreferrer">' +
        escapeHTML(report.source || "Source report") + '</a>';
    }
    html += '</div>';
    return html;
  }

  function renderFishingReports(payload) {
    if (fishingReportsLayer && map.hasLayer(fishingReportsLayer)) {
      map.removeLayer(fishingReportsLayer);
    }
    fishingReportsLayer = L.layerGroup();

    var reports = payload && Array.isArray(payload.reports) ? payload.reports : [];
    var pointCount = 0;
    var regionCount = 0;

    reports.forEach(function(report) {
      var style = fishingSpeciesStyle(report.species);
      var positionType = String(report.position_type || "").toLowerCase();

      if (positionType === "derived_point" || positionType === "exact_point") {
        var lat = Number(report.lat);
        var lon = Number(report.lon);
        if (!Number.isFinite(lat) || !Number.isFinite(lon)) return;

        var marker = L.circleMarker([lat, lon], {
          pane: "fishingReportsPane",
          radius: 7,
          color: style.color,
          weight: 2,
          fillColor: style.fillColor,
          fillOpacity: 0.92
        }).bindPopup(fishingPopupHTML(report));
        marker.addTo(fishingReportsLayer);
        pointCount++;

        if (positionType === "derived_point" && Number(report.uncertaintyNM) > 0) {
          L.circle([lat, lon], {
            pane: "fishingReportsPane",
            radius: Number(report.uncertaintyNM) * 1852,
            color: style.color,
            weight: 1.5,
            opacity: 0.75,
            dashArray: "5 5",
            fillColor: style.fillColor,
            fillOpacity: 0.045,
            interactive: false
          }).addTo(fishingReportsLayer);
        }
        return;
      }

      if (positionType === "region" && Array.isArray(report.polygon) && report.polygon.length >= 3) {
        var polygon = report.polygon
          .map(function(pair) {
            return Array.isArray(pair) && pair.length >= 2
              ? [Number(pair[0]), Number(pair[1])]
              : null;
          })
          .filter(function(pair) {
            return pair && Number.isFinite(pair[0]) && Number.isFinite(pair[1]);
          });
        if (polygon.length < 3) return;

        L.polygon(polygon, {
          pane: "fishingReportsPane",
          color: style.color,
          weight: 2,
          opacity: 0.85,
          dashArray: "7 6",
          fillColor: style.fillColor,
          fillOpacity: 0.07
        }).bindPopup(fishingPopupHTML(report)).addTo(fishingReportsLayer);
        regionCount++;
      }
    });

    fishingReportsLayer.addTo(map);
    setFishingReportsLegendVisible(true);

    var updated = String(payload && payload.updated || "").trim();
    var suffix = updated ? " · feed updated " + updated : "";
    setFishingReportsStatus(
      "Fishing Reports feed · " + pointCount + " point reports + " + regionCount +
      " regional reports" + suffix + ". Approximate locations are explicitly marked.",
      false
    );
  }

  function setFishingReportsVisible(visible) {
    mapState.fishingReportsVisible = !!visible;
    if (!mapState.fishingReportsVisible) {
      if (fishingReportsLayer && map.hasLayer(fishingReportsLayer)) map.removeLayer(fishingReportsLayer);
      setFishingReportsLegendVisible(false);
      setFishingReportsStatus("", false);
      return;
    }

    setFishingReportsStatus("Loading fishing reports…", false);
    loadFishingReports(function(ok, result) {
      if (!mapState.fishingReportsVisible) return;
      if (!ok) {
        setFishingReportsLegendVisible(false);
        var detail = String(result && result.message ? result.message : result || "unknown error");
        setFishingReportsStatus("Fishing Reports feed failed: " + detail, true);
        return;
      }
      renderFishingReports(result);
    });
  }

  function setStructureStatus(message, isError) {
    var status = document.getElementById("map-structure-status");
    if (!status) return;
    message = String(message || "").trim();
    status.hidden = !message;
    status.textContent = message;
    status.style.color = isError ? "#9a352f" : "";
  }

  function structureReliefExportURL(bounds) {
    var sw = map.options.crs.project(bounds.getSouthWest());
    var ne = map.options.crs.project(bounds.getNorthEast());
    var mapSize = map.getSize();
    var width = Math.max(256, Math.min(1600, Math.round(mapSize.x)));
    var height = Math.max(256, Math.min(1000, Math.round(mapSize.y)));

    var params = new URLSearchParams();
    params.set("f", "image");
    params.set("bbox", [sw.x, sw.y, ne.x, ne.y].join(","));
    params.set("bboxSR", "3857");
    params.set("imageSR", "3857");
    params.set("size", width + "," + height);
    params.set("format", "png32");
    params.set("transparent", "true");
    params.set("dpi", "96");
    params.set("layers", "show:0");
    return structureReliefServiceURL + "?" + params.toString();
  }

  function structureFeatureQueryURL(bounds) {
    var params = new URLSearchParams();
    params.set("f", "geojson");
    params.set("where", "1=1");
    params.set(
      "geometry",
      [
        bounds.getWest().toFixed(6),
        bounds.getSouth().toFixed(6),
        bounds.getEast().toFixed(6),
        bounds.getNorth().toFixed(6)
      ].join(",")
    );
    params.set("geometryType", "esriGeometryEnvelope");
    params.set("inSR", "4326");
    params.set("outSR", "4326");
    params.set("spatialRel", "esriSpatialRelIntersects");
    params.set("outFields", "name");
    params.set("returnGeometry", "true");
    return structureNamesServiceURL + "?" + params.toString();
  }

  function isFishingStructureName(name) {
    return /\b(seamount|bank|ridge|hills?|knoll|shoal|reef|rise|plateau|pinnacle|escarpment)\b/i.test(
      String(name || "")
    );
  }

  function structureNamePriority(name) {
    name = String(name || "");
    if (/\b(seamount|bank|ridge|plateau)\b/i.test(name)) return 0;
    if (/\b(rise|escarpment)\b/i.test(name)) return 1;
    return 2;
  }

  function structureLabelLimitForZoom(zoom) {
    if (zoom < 6) return 0;
    if (zoom === 6) return 10;
    if (zoom === 7) return 24;
    if (zoom === 8) return 45;
    if (zoom === 9) return 65;
    return 85;
  }

  function structureLabelBoxesOverlap(a, b) {
    return !(
      a.right < b.left ||
      a.left > b.right ||
      a.bottom < b.top ||
      a.top > b.bottom
    );
  }

  function renderStructureNames(featureCollection) {
    if (structureNamesLayer && map.hasLayer(structureNamesLayer)) {
      map.removeLayer(structureNamesLayer);
    }
    structureNamesLayer = L.layerGroup();

    var zoom = map.getZoom();
    var limit = structureLabelLimitForZoom(zoom);
    if (limit <= 0) {
      structureNamesLayer.addTo(map);
      return {shown:0, eligible:0, suppressed:true, limit:0};
    }

    var features = featureCollection && Array.isArray(featureCollection.features)
      ? featureCollection.features
      : [];
    var center = map.getCenter();
    var candidates = [];

    features.forEach(function(feature) {
      var name = String(feature && feature.properties && feature.properties.name || "").trim();
      var coords = feature && feature.geometry && feature.geometry.coordinates;
      if (!name || !isFishingStructureName(name) ||
          !Array.isArray(coords) || coords.length < 2) return;

      var lon = Number(coords[0]);
      var lat = Number(coords[1]);
      if (!Number.isFinite(lat) || !Number.isFinite(lon)) return;

      var latlng = L.latLng(lat, lon);
      var point = map.latLngToLayerPoint(latlng);
      var distance = center ? map.distance(center, latlng) : 0;

      candidates.push({
        name: name,
        lat: lat,
        lon: lon,
        point: point,
        priority: structureNamePriority(name),
        distance: distance
      });
    });

    candidates.sort(function(a, b) {
      if (a.priority !== b.priority) return a.priority - b.priority;
      if (a.distance !== b.distance) return a.distance - b.distance;
      return a.name.localeCompare(b.name);
    });

    var occupied = [];
    var shown = 0;
    var padding = zoom <= 7 ? 8 : 5;

    candidates.some(function(candidate) {
      if (shown >= limit) return true;

      // Estimate the rendered label footprint before adding it. The local
      // label uses ~13 px bold text plus a dot, so 7.4 px/character is a
      // conservative collision estimate without forcing DOM measurement.
      var width = Math.min(260, Math.max(72, 18 + candidate.name.length * 7.4));
      var height = 23;
      var box = {
        left: candidate.point.x - 5 - padding,
        right: candidate.point.x + width + padding,
        top: candidate.point.y - 12 - padding,
        bottom: candidate.point.y + height - 12 + padding
      };

      for (var i = 0; i < occupied.length; i++) {
        if (structureLabelBoxesOverlap(box, occupied[i])) return false;
      }

      var icon = L.divIcon({
        className: "map-undersea-feature-label",
        html:
          '<span class="undersea-dot" aria-hidden="true"></span>' +
          '<span class="undersea-name">' + escapeHTML(candidate.name) + "</span>",
        iconSize: null,
        iconAnchor: [3, 4]
      });

      L.marker([candidate.lat, candidate.lon], {
        icon: icon,
        pane: "underseaNamesPane",
        interactive: false,
        keyboard: false,
        riseOnHover: false
      }).addTo(structureNamesLayer);

      occupied.push(box);
      shown++;
      return false;
    });

    structureNamesLayer.addTo(map);
    return {
      shown: shown,
      eligible: candidates.length,
      suppressed: false,
      limit: limit
    };
  }

  function refreshStructureOverlay() {
    if (!mapState.structureOverlayVisible) return;

    var bounds = map.getBounds();
    var requestSerial = ++structureRequestSerial;
    var reliefURL = structureReliefExportURL(bounds);
    var featuresURL = structureFeatureQueryURL(bounds);

    setStructureStatus("Loading NOAA underwater structure…", false);

    var reliefImage = new Image();
    var reliefReady = false;
    var featureData = null;
    var featuresReady = false;
    var failed = false;

    function finishIfReady() {
      if (failed || !reliefReady || !featuresReady) return;
      if (!mapState.structureOverlayVisible || requestSerial !== structureRequestSerial) return;

      var nextRelief = L.imageOverlay(reliefURL, bounds, {
        opacity: 0.52,
        pane: "bathymetryOverlayPane",
        interactive: false,
        attribution: "NOAA/NCEI ETOPO shaded relief"
      });

      nextRelief.once("load", function() {
        if (!mapState.structureOverlayVisible || requestSerial !== structureRequestSerial) {
          if (map.hasLayer(nextRelief)) map.removeLayer(nextRelief);
          return;
        }

        var oldRelief = structureReliefLayer;
        structureReliefLayer = nextRelief;
        if (oldRelief && oldRelief !== structureReliefLayer && map.hasLayer(oldRelief)) {
          map.removeLayer(oldRelief);
        }

        var labelResult = renderStructureNames(featureData);

        if (sstLayer && mapState.sstOverlayVisible && map.hasLayer(sstLayer)) {
          sstLayer.bringToFront();
        }
        if (structureNamesLayer && map.hasLayer(structureNamesLayer)) {
          structureNamesLayer.eachLayer(function(layer) {
            if (layer.bringToFront) layer.bringToFront();
          });
        }

        if (labelResult.suppressed) {
          setStructureStatus(
            "NOAA underwater structure · ETOPO shaded relief · feature names hidden below Zoom 6 to prevent map clutter · planning aid, not for navigation.",
            false
          );
        } else {
          setStructureStatus(
            "NOAA underwater structure · ETOPO shaded relief + " +
            labelResult.shown +
            " label" +
            (labelResult.shown === 1 ? "" : "s") +
            " shown from " +
            labelResult.eligible +
            " fishing-relevant official features in view · Zoom " +
            map.getZoom() +
            " cap " +
            labelResult.limit +
            " with collision suppression · planning aid, not for navigation.",
            false
          );
        }
      });

      nextRelief.once("error", function() {
        if (requestSerial === structureRequestSerial) {
          setStructureStatus("NOAA bathymetry relief could not be displayed.", true);
        }
      });

      nextRelief.addTo(map);
      nextRelief.bringToFront();
    }

    reliefImage.onload = function() {
      reliefReady = true;
      finishIfReady();
    };
    reliefImage.onerror = function() {
      if (requestSerial !== structureRequestSerial) return;
      failed = true;
      setStructureStatus("NOAA/NCEI bathymetry could not be loaded.", true);
    };

    fetch(featuresURL, {headers: {"Accept":"application/geo+json, application/json"}})
      .then(function(response) {
        if (!response.ok) throw new Error("HTTP " + response.status);
        return response.json();
      })
      .then(function(payload) {
        if (!mapState.structureOverlayVisible || requestSerial !== structureRequestSerial) return;
        featureData = payload;
        featuresReady = true;
        finishIfReady();
      })
      .catch(function(err) {
        if (!mapState.structureOverlayVisible || requestSerial !== structureRequestSerial) return;
        failed = true;
        setStructureStatus(
          "NOAA undersea feature names could not be loaded: " +
          String(err && err.message ? err.message : err || "unknown error"),
          true
        );
      });

    reliefImage.src = reliefURL;
  }

  function scheduleStructureRefresh() {
    if (!mapState.structureOverlayVisible) return;
    if (structureRefreshTimer) window.clearTimeout(structureRefreshTimer);
    structureRefreshTimer = window.setTimeout(function() {
      structureRefreshTimer = null;
      refreshStructureOverlay();
    }, 180);
  }

  function setStructureOverlayVisible(visible) {
    mapState.structureOverlayVisible = !!visible;
    if (!mapState.structureOverlayVisible) {
      structureRequestSerial++;
      if (structureRefreshTimer) {
        window.clearTimeout(structureRefreshTimer);
        structureRefreshTimer = null;
      }
      if (structureReliefLayer && map.hasLayer(structureReliefLayer)) map.removeLayer(structureReliefLayer);
      if (structureNamesLayer && map.hasLayer(structureNamesLayer)) map.removeLayer(structureNamesLayer);
      structureReliefLayer = null;
      structureNamesLayer = null;
      setStructureStatus("", false);
      return;
    }
    refreshStructureOverlay();
  }

  function setWeatherOverlayStatus(message, isError) {
    var status = document.getElementById("map-weather-overlay-status");
    if (!status) return;
    message = String(message || "").trim();
    status.hidden = !message;
    status.textContent = message;
    status.style.color = isError ? "#9a352f" : "";
  }

  function refreshWeatherOverlayStatus() {
    if (!mapState.cloudOverlayVisible && !mapState.radarOverlayVisible) {
      setWeatherOverlayStatus("", false);
    }
  }

  function cloudExportURL(bounds) {
    var sw = map.options.crs.project(bounds.getSouthWest());
    var ne = map.options.crs.project(bounds.getNorthEast());
    var mapSize = map.getSize();
    var pixelRatio = Math.min(2, Math.max(1, window.devicePixelRatio || 1));
    var width = Math.max(256, Math.min(2400, Math.round(mapSize.x * pixelRatio)));
    var height = Math.max(256, Math.min(1800, Math.round(mapSize.y * pixelRatio)));

    var params = new URLSearchParams();
    params.set("f", "image");
    params.set("bbox", [sw.x, sw.y, ne.x, ne.y].join(","));
    params.set("bboxSR", "3857");
    params.set("imageSR", "3857");
    params.set("size", width + "," + height);
    params.set("format", "png32");
    params.set("interpolation", "RSP_BilinearInterpolation");
    params.set("adjustAspectRatio", "false");

    return cloudImageServiceURL + "?" + params.toString();
  }

  function refreshCloudOverlay() {
    if (!mapState.cloudOverlayVisible) return;

    var bounds = map.getBounds();
    var requestSerial = ++cloudRequestSerial;
    var url = cloudExportURL(bounds);
    var testImage = new Image();

    testImage.onload = function() {
      if (!mapState.cloudOverlayVisible || requestSerial !== cloudRequestSerial) return;

      var nextLayer = L.imageOverlay(url, bounds, {
        opacity: 0.72,
        pane: "cloudOverlayPane",
        interactive: false,
        attribution: "NOAA/NESDIS GOES GeoColor"
      });

      nextLayer.once("load", function() {
        if (!mapState.cloudOverlayVisible || requestSerial !== cloudRequestSerial) {
          if (map.hasLayer(nextLayer)) map.removeLayer(nextLayer);
          return;
        }
        var previousLayer = cloudLayer;
        cloudLayer = nextLayer;
        cloudLayer.bringToFront();
        if (previousLayer && previousLayer !== cloudLayer && map.hasLayer(previousLayer)) {
          map.removeLayer(previousLayer);
        }
        if (!mapState.radarOverlayVisible) setWeatherOverlayStatus("", false);
      });

      nextLayer.once("error", function() {
        if (requestSerial === cloudRequestSerial && mapState.cloudOverlayVisible) {
          setWeatherOverlayStatus(
            "NOAA/NESDIS GeoColor satellite imagery could not be displayed.",
            true
          );
        }
      });

      nextLayer.addTo(map);
      nextLayer.bringToFront();
      if (radarLayer && mapState.radarOverlayVisible && map.hasLayer(radarLayer)) {
        radarLayer.bringToFront();
      }
    };

    testImage.onerror = function() {
      if (requestSerial === cloudRequestSerial && mapState.cloudOverlayVisible) {
        setWeatherOverlayStatus(
          "NOAA/NESDIS GeoColor satellite imagery could not be loaded.",
          true
        );
      }
    };

    testImage.src = url;
  }

  function scheduleCloudRefresh() {
    if (!mapState.cloudOverlayVisible) return;
    if (cloudRefreshTimer) window.clearTimeout(cloudRefreshTimer);
    cloudRefreshTimer = window.setTimeout(function() {
      cloudRefreshTimer = null;
      refreshCloudOverlay();
    }, 180);
  }

  function setCloudOverlayVisible(visible) {
    mapState.cloudOverlayVisible = !!visible;

    if (!mapState.cloudOverlayVisible) {
      cloudRequestSerial++;
      if (cloudRefreshTimer) {
        window.clearTimeout(cloudRefreshTimer);
        cloudRefreshTimer = null;
      }
      if (cloudLayer && map.hasLayer(cloudLayer)) map.removeLayer(cloudLayer);
      cloudLayer = null;
      refreshWeatherOverlayStatus();
      return;
    }

    refreshCloudOverlay();
  }

  function setRadarOverlayVisible(visible) {
    mapState.radarOverlayVisible = !!visible;
    if (!radarLayer) return;
    if (mapState.radarOverlayVisible) {
      radarTilesLoaded = 0;
      radarTileErrors = 0;
      if (!map.hasLayer(radarLayer)) radarLayer.addTo(map);
      radarLayer.bringToFront();
      radarLayer.redraw();
    } else if (map.hasLayer(radarLayer)) {
      map.removeLayer(radarLayer);
      refreshWeatherOverlayStatus();
    }
  }

  radarLayer.on("tileload", function() {
    radarTilesLoaded++;
  });

  radarLayer.on("tileerror", function() {
    radarTileErrors++;
  });

  radarLayer.on("load", function() {
    if (!mapState.radarOverlayVisible) return;
    if (radarTilesLoaded === 0 && radarTileErrors > 0) {
      setWeatherOverlayStatus(
        "Weather radar could not be loaded from Iowa State IEM.",
        true
      );
    } else if (!mapState.cloudOverlayVisible) {
      setWeatherOverlayStatus("", false);
    }
  });

  function updateRecenterControls() {
    var selectedButton = document.getElementById("map-nav-selected");
    var windButton = document.getElementById("map-nav-wind");
    var currentsButton = document.getElementById("map-nav-current");

    function setAvailable(button, available) {
      if (!button) return;
      button.disabled = !available;
      button.setAttribute("aria-disabled", available ? "false" : "true");
    }

    setAvailable(selectedButton, !!selectedMarker);
    setAvailable(windButton, !!selectedWindMarker);
    setAvailable(currentsButton, !!currentStationMarker);
  }

  function wireMapNavigation() {
    var coordinateButton = document.getElementById("map-nav-coordinates");
    var selectedButton = document.getElementById("map-nav-selected");
    var windButton = document.getElementById("map-nav-wind");
    var currentsButton = document.getElementById("map-nav-current");
    var centerMenu = document.getElementById("map-center-menu");

    function finishCenterChoice() {
      if (centerMenu) centerMenu.open = false;
    }

    if (coordinateButton) {
      coordinateButton.addEventListener("click", function() {
        var latField = document.getElementById("map-lat-input");
        var lonField = document.getElementById("map-lon-input");
        var errorField = document.getElementById("map-coordinate-error");
        var latText = latField ? String(latField.value || "").trim() : "";
        var lonText = lonField ? String(lonField.value || "").trim() : "";
        var lat = Number(latText);
        var lon = Number(lonText);

        if (!latText || !lonText || !Number.isFinite(lat) || !Number.isFinite(lon) ||
            lat < -90 || lat > 90 || lon < -180 || lon > 180) {
          if (errorField) {
            errorField.textContent =
              "Enter a valid latitude (-90 to 90) and longitude (-180 to 180).";
          }
          return;
        }

        if (errorField) errorField.textContent = "";
        map.panTo([lat, lon]);
        finishCenterChoice();
      });
    }

    if (selectedButton) {
      selectedButton.addEventListener("click", function() {
        if (!selectedButton.disabled && selectedMarker) {
          map.panTo(selectedMarker.getLatLng());
          finishCenterChoice();
        }
      });
    }

    if (windButton) {
      windButton.addEventListener("click", function() {
        if (!windButton.disabled && selectedWindMarker) {
          map.panTo(selectedWindMarker.getLatLng());
          finishCenterChoice();
        }
      });
    }

    if (currentsButton) {
      currentsButton.addEventListener("click", function() {
        if (!currentsButton.disabled && currentStationMarker) {
          map.panTo(currentStationMarker.getLatLng());
          finishCenterChoice();
        }
      });
    }
  }

  var selectedMarker = null;
  var selectedWindMarker = null;
  var currentStationMarker = null;
  var selectedCurrentLatLng = null;
  var selectedCurrentLabel = "";
  var previewingCurrentStation = false;
  // Authoritative interactive-map state.
  // Event handlers mutate this object, then render. Leaflet markers and DOM
  // controls are views of this state, never independent sources of truth.
  var mapState = {
    selectedLocation: {{if .MapHasRequest}}{
      lat: {{printf "%.6f" .MapRequestLat}},
      lon: {{printf "%.6f" .MapRequestLon}}
    }{{else}}null{{end}},
    selectedWindStationID: normalizeWindStationID({{.MapWindStation}}),
    windCandidates: [],
    stationSearch: {
      busy: false,
      mode: "",
      message: ""
    },
    marineZoneOverlayVisible: false,
    smokeOverlayVisible: false,
    smokeOverlayLoaded: false,
    sstOverlayVisible: false,
    chlorophyllFieldVisible: false,
    chlorophyllOverlayVisible: false,
    structureOverlayVisible: false,
    fishingReportsVisible: false,
    cloudOverlayVisible: false,
    radarOverlayVisible: false
  };

  var sourcePoints = [];
  var candidateLayer = L.layerGroup().addTo(map);
  var marineZoneLayer = null;
  var marineZoneHaloLayer = null;
  var smokeLayer = null;
  var smokeOutlineLayer = null;
  var radarTilesLoaded = 0;
  var radarTileErrors = 0;

  function marineZoneStyle() {
    var imageryBase = activeMapLayerName === "satellite" || activeMapLayerName === "hybrid";
    if (imageryBase) {
      return {pane:"forecastZonePane",color:"#63e6ff",weight:3.4,opacity:1,fillColor:"#63e6ff",fillOpacity:.055,interactive:false,lineJoin:"round"};
    }
    return {pane:"forecastZonePane",color:"#126b91",weight:2.7,opacity:.98,fillColor:"#126b91",fillOpacity:.08,interactive:false,lineJoin:"round"};
  }

  function marineZoneHaloStyle() {
    var imageryBase = activeMapLayerName === "satellite" || activeMapLayerName === "hybrid";
    return {pane:"forecastZoneHaloPane",color:imageryBase ? "#071b24" : "#ffffff",weight:imageryBase ? 7.2 : 5.2,opacity:imageryBase ? .96 : .88,fill:false,interactive:false,lineJoin:"round"};
  }

  function setMarineZoneOverlay(zone, geometry) {
    if (marineZoneLayer && map.hasLayer(marineZoneLayer)) map.removeLayer(marineZoneLayer);
    if (marineZoneHaloLayer && map.hasLayer(marineZoneHaloLayer)) map.removeLayer(marineZoneHaloLayer);
    marineZoneLayer = null;
    marineZoneHaloLayer = null;

    var control = document.getElementById("map-marine-zone-control");
    var checkbox = document.getElementById("map-show-marine-zone");
    var label = document.getElementById("map-marine-zone-label");

    zone = String(zone || "").trim().toUpperCase();
    if (!zone || !geometry) {
      if (control) control.hidden = true;
      if (checkbox) checkbox.disabled = true;
      if (label) label.textContent = "Show NWS forecast zone";
      return;
    }

    var zoneFeature = {type:"Feature",properties:{zone:zone},geometry:geometry};
    marineZoneHaloLayer = L.geoJSON(zoneFeature, {style:marineZoneHaloStyle,interactive:false,pane:"forecastZoneHaloPane"});
    marineZoneLayer = L.geoJSON(zoneFeature, {style:marineZoneStyle,interactive:false,pane:"forecastZonePane"});
    marineZoneLayer.bindTooltip("NWS forecast zone " + zone, {sticky:true,direction:"center",className:"map-zone-tooltip"});

    if (control) control.hidden = false;
    if (checkbox) {
      checkbox.disabled = false;
      checkbox.checked = !!mapState.marineZoneOverlayVisible;
    }
    if (label) label.textContent = "Show NWS forecast zone " + zone;

    if (mapState.marineZoneOverlayVisible) {
      marineZoneHaloLayer.addTo(map);
      marineZoneLayer.addTo(map);
      marineZoneHaloLayer.bringToFront();
      marineZoneLayer.bringToFront();
    }
  }

  {{if .MarineForecastGeometry}}
  setMarineZoneOverlay({{.MarineForecastZone}}, {{.MarineForecastGeometry}});
  {{end}}

  function smokeStyle(feature) {
    var density = feature && feature.properties
      ? String(feature.properties.density || "").toLowerCase()
      : "";
    var imageryBase = activeMapLayerName === "satellite" || activeMapLayerName === "hybrid";

    if (imageryBase) {
      if (density === "heavy") {
        return {color:"#9b431f",weight:1.0,opacity:.70,fillColor:"#d85c25",fillOpacity:.50};
      }
      if (density === "medium") {
        return {color:"#b96f12",weight:.9,opacity:.66,fillColor:"#f09a24",fillOpacity:.38};
      }
      return {color:"#b99a22",weight:.8,opacity:.60,fillColor:"#f4d35e",fillOpacity:.26};
    }

    if (density === "heavy") {
      return {color:"#8f3f1c",weight:1.0,opacity:.68,fillColor:"#d45a24",fillOpacity:.44};
    }
    if (density === "medium") {
      return {color:"#ad6811",weight:.9,opacity:.64,fillColor:"#ed9620",fillOpacity:.32};
    }
    return {color:"#aa8d1d",weight:.8,opacity:.58,fillColor:"#f2cf57",fillOpacity:.20};
  }

  function smokeOutlineStyle() {
    var imageryBase = activeMapLayerName === "satellite" || activeMapLayerName === "hybrid";
    if (imageryBase) {
      return {
        pane: "smokeOutlinePane",
        color: "#6f4a22",
        weight: 1.6,
        opacity: .68,
        fill: false,
        interactive: false,
        lineJoin: "round"
      };
    }
    return {
      pane: "smokeOutlinePane",
      color: "#6f4a22",
      weight: 1.4,
      opacity: .58,
      fill: false,
      interactive: false,
      lineJoin: "round"
    };
  }

  function updateSmokeLegendForBaseMap() {
    var imageryBase = activeMapLayerName === "satellite" || activeMapLayerName === "hybrid";
    var light = document.querySelector(".smoke-swatch.light");
    var medium = document.querySelector(".smoke-swatch.medium");
    var heavy = document.querySelector(".smoke-swatch.heavy");
    if (!light || !medium || !heavy) return;

    if (imageryBase) {
      light.style.background = "rgba(244,211,94,.46)";
      light.style.borderColor = "rgba(111,74,34,.72)";
      medium.style.background = "rgba(240,154,36,.58)";
      medium.style.borderColor = "rgba(111,74,34,.76)";
      heavy.style.background = "rgba(216,92,37,.70)";
      heavy.style.borderColor = "rgba(111,74,34,.82)";
    } else {
      light.style.background = "rgba(242,207,87,.38)";
      light.style.borderColor = "rgba(111,74,34,.62)";
      medium.style.background = "rgba(237,150,32,.50)";
      medium.style.borderColor = "rgba(111,74,34,.68)";
      heavy.style.background = "rgba(212,90,36,.62)";
      heavy.style.borderColor = "rgba(111,74,34,.76)";
    }
  }

  function setSmokeStatus(message, isError) {
    var status = document.getElementById("map-smoke-status");
    if (!status) return;
    message = String(message || "").trim();
    status.hidden = !message;
    status.textContent = message;
    status.style.color = isError ? "#9a352f" : "";
  }

  function setSmokeLegendVisible(visible) {
    var legend = document.getElementById("map-smoke-legend");
    if (legend) {
      legend.hidden = !visible;
      if (visible) updateSmokeLegendForBaseMap();
    }
  }

  function clearSmokeLayer() {
    if (smokeLayer && map.hasLayer(smokeLayer)) {
      map.removeLayer(smokeLayer);
    }
    if (smokeOutlineLayer && map.hasLayer(smokeOutlineLayer)) {
      map.removeLayer(smokeOutlineLayer);
    }
    smokeLayer = null;
    smokeOutlineLayer = null;
    mapState.smokeOverlayLoaded = false;
    setSmokeLegendVisible(false);
  }


  function loadSmokeOverlay() {
    if (!mapState.smokeOverlayVisible) return;

    if (smokeLayer && mapState.smokeOverlayLoaded) {
      if (!map.hasLayer(smokeLayer)) smokeLayer.addTo(map);
      if (smokeOutlineLayer && !map.hasLayer(smokeOutlineLayer)) {
        smokeOutlineLayer.addTo(map);
        smokeOutlineLayer.bringToFront();
      }
      setSmokeLegendVisible(true);
      return;
    }

    setSmokeStatus("Loading NOAA satellite smoke analysis…", false);

    fetch("/smoke-overlay", {headers: {"Accept":"application/json"}})
      .then(function(response) {
        if (!response.ok) throw new Error("HTTP " + response.status);
        return response.json();
      })
      .then(function(payload) {
        if (!mapState.smokeOverlayVisible) return;

        clearSmokeLayer();

        if (payload.error) {
          setSmokeStatus(payload.error, true);
          return;
        }

        var geojson = payload.geojson;
        if (!geojson || !Array.isArray(geojson.features)) {
          setSmokeStatus(
            "NOAA HMS did not return smoke polygons for the latest available analysis.",
            false
          );
          return;
        }

        smokeLayer = L.geoJSON(geojson, {
          style: smokeStyle,
          interactive: false
        });
        smokeOutlineLayer = L.geoJSON(geojson, {
          style: smokeOutlineStyle,
          interactive: false,
          pane: "smokeOutlinePane"
        });
        mapState.smokeOverlayLoaded = true;

        if (mapState.smokeOverlayVisible) {
          smokeLayer.addTo(map);
          smokeOutlineLayer.addTo(map);
          smokeOutlineLayer.bringToFront();
          setSmokeLegendVisible(true);
        }

        var dateText = payload.analysis_date
          ? " · analysis " + payload.analysis_date
          : "";
        var count = geojson.features.length;
        var visibleCount = 0;
        var viewBounds = map.getBounds();
        smokeLayer.eachLayer(function(layer) {
          if (layer.getBounds && layer.getBounds().intersects(viewBounds)) {
            visibleCount++;
          }
        });
        setSmokeStatus(
          "NOAA HMS satellite smoke" + dateText + " · " + count +
          " analyzed polygon" + (count === 1 ? "" : "s") +
          "; " + visibleCount + " intersect current map view. " +
          "Qualitative smoke density; not AQI.",
          false
        );
      })
      .catch(function(err) {
        clearSmokeLayer();
        setSmokeStatus(
          "NOAA satellite smoke overlay is temporarily unavailable" +
          (err && err.message ? ": " + err.message : "."),
          true
        );
      });
  }

  // Compatibility accessors kept local to this script while the rest of the
  // map rendering code is migrated to mapState.
  function chosenLocation() {
    return mapState.selectedLocation;
  }

  function symbolMarker(lat, lon, symbol, kind, label, options) {
    options = options || {};
    var icon = L.divIcon({
      className: "map-leaflet-symbol",
      html: '<span class="marker-symbol ' + kind + '" aria-hidden="true">' + symbol + '</span>',
      iconSize: [32, 32],
      iconAnchor: [16, 16]
    });
    var marker = L.marker([lat, lon], {
      icon: icon,
      keyboard: false,
      interactive: options.interactive !== false,
      zIndexOffset: options.zIndexOffset || 0,
      title: label || ""
    }).addTo(map);
    marker.on("add", function() {
      var markerEl = marker.getElement();
      if (markerEl) markerEl.style.cursor = "default";
    });
    if (label) marker.bindTooltip(label);
    return marker;
  }

  function circleMarker(lat, lon, color, label) {
    var marker = L.circleMarker([lat, lon], {
      radius: 7,
      color: "#ffffff",
      weight: 2,
      fillColor: color,
      fillOpacity: 1
    }).addTo(map);
    marker.bindPopup(label);
    sourcePoints.push([lat, lon]);
    return marker;
  }

  function reportURLWithMapView(rawURL) {
    var target = new URL(rawURL, window.location.href);
    var center = map.getCenter();

    if (mapState.selectedLocation) {
      target.searchParams.set("lat", Number(mapState.selectedLocation.lat).toFixed(5));
      target.searchParams.set("lon", Number(mapState.selectedLocation.lon).toFixed(5));
    }

    target.searchParams.set("map_center_lat", Number(center.lat).toFixed(5));
    target.searchParams.set("map_center_lon", Number(center.lng).toFixed(5));
    target.searchParams.set("map_zoom", String(map.getZoom()));

    if (preferredMapLayerName === "nautical") {
      target.searchParams.set("map_layer", "nautical");
    } else if (preferredMapLayerName === "satellite") {
      target.searchParams.set("map_layer", "satellite");
    } else if (preferredMapLayerName === "hybrid") {
      target.searchParams.set("map_layer", "hybrid");
    } else {
      target.searchParams.delete("map_layer");
    }

    return target.pathname + "?" + target.searchParams.toString() + target.hash;
  }



  function previewCurrentsForWind(station, currentStation, currentName, currentDistance, currentLat, currentLon) {
    if (!currentStation) return;
    if (!Number.isFinite(Number(currentLat)) || !Number.isFinite(Number(currentLon))) return;

    var label =
      "Currents station " + currentStation +
      " — preview for wind station " + station;
    if (!currentStationMarker) {
      currentStationMarker = symbolMarker(
        Number(currentLat),
        Number(currentLon),
        "◆",
        "current",
        label,
        {interactive:false, zIndexOffset:-500}
      );
    } else {
      currentStationMarker.setLatLng([Number(currentLat), Number(currentLon)]);
    }
    if (currentName) label += " — " + currentName;
    if (currentDistance) label += " (" + currentDistance + " from wind station)";
    if (currentStationMarker.getTooltip()) {
      currentStationMarker.setTooltipContent(label);
    }
    previewingCurrentStation = true;

    if (!map.hasLayer(currentStationMarker)) {
      currentStationMarker.addTo(map);
    }
    updateRecenterControls();
  }

  function restoreSelectedCurrentsStation() {
    if (!currentStationMarker || !previewingCurrentStation) return;
    if (selectedCurrentLatLng) currentStationMarker.setLatLng(selectedCurrentLatLng);
    if (selectedCurrentLabel && currentStationMarker.getTooltip()) {
      currentStationMarker.setTooltipContent(selectedCurrentLabel);
    }
    previewingCurrentStation = false;
    updateRecenterControls();
  }

  function windCandidateMarker(lat, lon, station, name, distance, url, isAuto, isSelected, currentStation, currentName, currentDistance, currentLat, currentLon, currentNote) {
    var fill = "#718794";
    var radius = 7;
    if (isAuto) {
      fill = "#24538a";
      radius = 8;
    }
    if (isSelected) {
      fill = "#16805f";
      radius = 9;
    }

    var icon = L.divIcon({
      className: "map-leaflet-symbol map-wind-candidate",
      html: '<span class="marker-triangle" aria-hidden="true"></span>',
      iconSize: [32, 32],
      iconAnchor: [16, 16]
    });
    var marker = L.marker([lat, lon], {
      icon:icon,
      keyboard:false,
      interactive:true,
      bubblingMouseEvents:false,
      zIndexOffset:500
    }).addTo(candidateLayer);

    marker.bindTooltip(station + " " + name);


    var state = "";
    if (isAuto) state += "<strong>AUTO</strong> ";
    // Selection is represented by the separate filled ▲ wind-source marker.

    marker.on("click", function(e) {
      if (e && e.originalEvent) L.DomEvent.stopPropagation(e.originalEvent);
      showWindInfo(station, name, distance, url, currentStation, currentName, currentDistance, currentNote);
      previewCurrentsForWind(
        station,
        currentStation,
        currentName,
        currentDistance,
        currentLat,
        currentLon
      );
    });

    return marker;
  }

  {{if .MapHasRequest}}
  selectedMarker = symbolMarker(
    {{printf "%.6f" .MapRequestLat}},
    {{printf "%.6f" .MapRequestLon}},
    "★",
    "request",
    "Selected location"
  );
  {{end}}

  mapState.windCandidates = [
    {{range .WindCandidates}}
    {
      station: {{.Station}},
      name: {{.Name}},
      distance: {{.Distance}},
      wind: {{.Wind}},
      observationAge: {{.ObservationAge}},
      lat: {{printf "%.6f" .Lat}},
      lon: {{printf "%.6f" .Lon}},
      url: {{.JSURL}},
      isAuto: {{if .IsAuto}}true{{else}}false{{end}},
      currentStation: {{.CurrentStation}},
      currentName: {{.CurrentName}},
      currentDistance: {{.CurrentDistance}},
      currentLat: {{printf "%.6f" .CurrentLat}},
      currentLon: {{printf "%.6f" .CurrentLon}},
      currentNote: {{.CurrentNote}}
    },
    {{end}}
  ];

  function normalizeWindStationID(value) {
    var id = String(value || "").trim();

    // Be defensive about station IDs that arrive with literal wrapping quotes.
    // This was observed in the live marker trace: selectedID="PCOC1" while
    // candidate IDs were PCOC1.
    while (id.length >= 2) {
      var first = id.charAt(0);
      var last = id.charAt(id.length - 1);
      if ((first === '"' && last === '"') ||
          (first === "'" && last === "'")) {
        id = id.slice(1, -1).trim();
        continue;
      }
      break;
    }

    return id.toUpperCase();
  }

  function visibleWindCandidateCount() {
    var selectedID = normalizeWindStationID(mapState.selectedWindStationID);
    var seen = Object.create(null);
    mapState.windCandidates.forEach(function(c) {
      var id = normalizeWindStationID(c.station);
      if (!id || id === selectedID || seen[id]) return;
      seen[id] = true;
    });
    return Object.keys(seen).length;
  }

  function renderWindMarkers() {
    // The candidate layer is a pure rendering of authoritative candidate state.
    candidateLayer.clearLayers();

    var selectedID = normalizeWindStationID(mapState.selectedWindStationID);
    var seen = Object.create(null);

    mapState.windCandidates.forEach(function(c) {
      var stationID = normalizeWindStationID(c.station);
      if (!stationID) return;

      // Invariant 1: selected wind station is never also a candidate marker.
      if (selectedID && stationID === selectedID) return;

      // Invariant 2: one normalized station ID -> at most one candidate marker.
      if (seen[stationID]) return;
      seen[stationID] = true;

      var lat = Number(c.lat);
      var lon = Number(c.lon);
      if (!Number.isFinite(lat) || !Number.isFinite(lon)) return;

      windCandidateMarker(
        lat,
        lon,
        stationID,
        String(c.name || stationID),
        String(c.distance || ""),
        String(c.url || ""),
        !!c.isAuto,
        false,
        String(c.currentStation || ""),
        String(c.currentName || ""),
        String(c.currentDistance || ""),
        Number(c.currentLat),
        Number(c.currentLon),
        String(c.currentNote || "")
      );
    });

  }

  renderWindMarkers();

  {{if .MapHasWind}}
  selectedWindMarker = symbolMarker(
    {{printf "%.6f" .MapWindLat}},
    {{printf "%.6f" .MapWindLon}},
    "▲",
    "wind",
    "Selected wind station {{.MapWindStation}}",
    {zIndexOffset:250}
  );
  {{end}}

  {{if .MapHasCurrent}}
  currentStationMarker = symbolMarker(
    {{printf "%.6f" .MapCurrentLat}},
    {{printf "%.6f" .MapCurrentLon}},
    "◆",
    "current",
    "Currents station {{.MapCurrentStation}}",
    {interactive:false, zIndexOffset:-500}
  );
  selectedCurrentLatLng = currentStationMarker.getLatLng();
  selectedCurrentLabel = "Currents station {{.MapCurrentStation}}";
  {{end}}

  {{if .MapHasRequest}}
  if (!pageURL.searchParams.has("map_center_lat") ||
      !pageURL.searchParams.has("map_center_lon")) {
    map.setView(
      [{{printf "%.6f" .MapRequestLat}}, {{printf "%.6f" .MapRequestLon}}],
      initialZoom
    );
  }
  {{else}}
  if (sourcePoints.length > 1 &&
      !pageURL.searchParams.has("map_center_lat") &&
      !pageURL.searchParams.has("map_center_lon")) {
    map.fitBounds(sourcePoints, {padding:[35,35], maxZoom:10});
  }
  {{end}}

  wireMapNavigation();


  var marineZoneCheckbox = document.getElementById("map-show-marine-zone");
  if (marineZoneCheckbox) {
    marineZoneCheckbox.checked = !!mapState.marineZoneOverlayVisible;
    marineZoneCheckbox.addEventListener("change", function() {
      mapState.marineZoneOverlayVisible = marineZoneCheckbox.checked;
      if (!marineZoneLayer || !marineZoneHaloLayer) return;
      if (mapState.marineZoneOverlayVisible) {
        if (!map.hasLayer(marineZoneHaloLayer)) marineZoneHaloLayer.addTo(map);
        if (!map.hasLayer(marineZoneLayer)) marineZoneLayer.addTo(map);
        marineZoneHaloLayer.bringToFront();
        marineZoneLayer.bringToFront();
      } else {
        if (map.hasLayer(marineZoneLayer)) map.removeLayer(marineZoneLayer);
        if (map.hasLayer(marineZoneHaloLayer)) map.removeLayer(marineZoneHaloLayer);
      }
    });
  }

  var smokeCheckbox = document.getElementById("map-show-smoke");
  if (smokeCheckbox) {
    smokeCheckbox.checked = !!mapState.smokeOverlayVisible;
    smokeCheckbox.addEventListener("change", function() {
      mapState.smokeOverlayVisible = smokeCheckbox.checked;
      if (mapState.smokeOverlayVisible) {
        loadSmokeOverlay();
      } else {
        clearSmokeLayer();
        setSmokeStatus("", false);
      }
    });
  }

  var mapTypeInputs = document.querySelectorAll('input[name="map-type"]');
  mapTypeInputs.forEach(function(input) {
    input.checked = input.value === preferredMapLayerName;
    input.addEventListener("change", function() {
      if (input.checked) setBaseMapLayer(input.value);
    });
  });

  var sstCheckbox = document.getElementById("map-show-sst");
  if (sstCheckbox) {
    sstCheckbox.checked = !!mapState.sstOverlayVisible;
    sstCheckbox.addEventListener("change", function() {
      setSSTOverlayVisible(!!sstCheckbox.checked);
    });
  }

  var chlorophyllFieldCheckbox = document.getElementById("map-show-chlorophyll-field");
  if (chlorophyllFieldCheckbox) {
    chlorophyllFieldCheckbox.checked = !!mapState.chlorophyllFieldVisible;
    chlorophyllFieldCheckbox.addEventListener("change", function() {
      setChlorophyllFieldVisible(!!chlorophyllFieldCheckbox.checked);
    });
  }

  var chlorophyllCheckbox = document.getElementById("map-show-chlorophyll");
  if (chlorophyllCheckbox) {
    chlorophyllCheckbox.checked = !!mapState.chlorophyllOverlayVisible;
    chlorophyllCheckbox.addEventListener("change", function() {
      setChlorophyllOverlayVisible(!!chlorophyllCheckbox.checked);
    });
  }

  var structureCheckbox = document.getElementById("map-show-structure");
  if (structureCheckbox) {
    structureCheckbox.checked = !!mapState.structureOverlayVisible;
    structureCheckbox.addEventListener("change", function() {
      setStructureOverlayVisible(!!structureCheckbox.checked);
    });
  }

  var fishingReportsCheckbox = document.getElementById("map-show-fishing-reports");
  if (fishingReportsCheckbox) {
    fishingReportsCheckbox.checked = !!mapState.fishingReportsVisible;
    fishingReportsCheckbox.addEventListener("change", function() {
      setFishingReportsVisible(!!fishingReportsCheckbox.checked);
    });
  }

  var cloudCheckbox = document.getElementById("map-show-clouds");
  if (cloudCheckbox) {
    cloudCheckbox.checked = !!mapState.cloudOverlayVisible;
    cloudCheckbox.addEventListener("change", function() {
      setCloudOverlayVisible(!!cloudCheckbox.checked);
    });
  }

  var radarCheckbox = document.getElementById("map-show-radar");
  if (radarCheckbox) {
    radarCheckbox.checked = !!mapState.radarOverlayVisible;
    radarCheckbox.addEventListener("change", function() {
      setRadarOverlayVisible(!!radarCheckbox.checked);
    });
  }

  ["drag", "move", "zoom"].forEach(function(eventName) {
    map.on(eventName, function() {
      syncCoordinateInputsToMapCenter();
      updateMapScaleStatus();
    });
  });

  map.on("moveend", function() {
    syncCoordinateInputsToMapCenter();
    updateMapScaleStatus();
    if (mapState.sstOverlayVisible) scheduleSSTRefresh();
    if (mapState.chlorophyllFieldVisible) scheduleChlorophyllFieldRefresh();
    if (mapState.chlorophyllOverlayVisible) scheduleChlorophyllRefresh();
    if (mapState.structureOverlayVisible) scheduleStructureRefresh();
    if (mapState.cloudOverlayVisible) scheduleCloudRefresh();

    if (!mapState.smokeOverlayVisible || !smokeLayer) return;
    var visibleCount = 0;
    var viewBounds = map.getBounds();
    smokeLayer.eachLayer(function(layer) {
      if (layer.getBounds && layer.getBounds().intersects(viewBounds)) {
        visibleCount++;
      }
    });
    var status = document.getElementById("map-smoke-status");
    if (status && !status.hidden) {
      var text = status.textContent || "";
      text = text.replace(/; \d+ intersect current map view\./,
        "; " + visibleCount + " intersect current map view.");
      status.textContent = text;
    }
  });

  updateRecenterControls();

  var findPoint = document.getElementById("map-find-point");
  var reset = document.getElementById("map-reset");
  var geolocate = document.getElementById("map-geolocate");
  var coordinateError = document.getElementById("map-coordinate-error");

  function syncCoordinateInputsToMapCenter() {
    if (!map) return;
    var center = map.getCenter();
    if (!center) return;
    var latField = document.getElementById("map-lat-input");
    var lonField = document.getElementById("map-lon-input");
    if (latField) latField.value = Number(center.lat).toFixed(5);
    if (lonField) lonField.value = Number(center.lng).toFixed(5);
  }

  map.on("zoomend", function() {
    syncCoordinateInputsToMapCenter();
    updateMapScaleStatus();
    applyPreferredBaseMap();
  });
  syncCoordinateInputsToMapCenter();
  updateMapScaleStatus();

  var searchStatus = document.getElementById("map-search-status");

  function renderSearchControls() {
    if (findPoint) {
      findPoint.hidden = false;
      if (!mapState.selectedLocation) {
        findPoint.textContent = "Select a location to find stations";
        findPoint.setAttribute("aria-disabled", "true");
      } else {
        findPoint.textContent = mapState.stationSearch.busy
          ? "Finding stations..."
          : "Find stations near selected location";
        findPoint.setAttribute(
          "aria-disabled",
          mapState.stationSearch.busy ? "true" : "false"
        );
      }
    }

    if (reset) {
      var hasSelectedLocation = !!mapState.selectedLocation;
      reset.disabled = !hasSelectedLocation;
      reset.setAttribute("aria-disabled", hasSelectedLocation ? "false" : "true");
    }

    updateRecenterControls();

    if (searchStatus) {
      searchStatus.textContent = mapState.stationSearch.message || "";
    }
  }

  function setStationSearchState(busy, mode, message) {
    mapState.stationSearch.busy = !!busy;
    mapState.stationSearch.mode = mode || "";
    mapState.stationSearch.message = message || "";
    renderSearchControls();
  }

  renderSearchControls();

  var marineForecastRequestSerial = 0;
  var offshoreTripRequestSerial = 0;
  var fishingPlanningRequestSerial = 0;

  function renderSelectedLocationWeather(weather) {
    weather = weather || {};

    var box = document.getElementById("selected-location-weather");
    var place = document.getElementById("selected-location-weather-place");
    var content = document.getElementById("selected-location-weather-content");
    var air = document.getElementById("selected-location-weather-air");
    var high = document.getElementById("selected-location-weather-high");
    var low = document.getElementById("selected-location-weather-low");
    var forecast = document.getElementById("selected-location-weather-forecast");
    var updated = document.getElementById("selected-location-weather-updated");
    var errorBox = document.getElementById("selected-location-weather-error");

    if (!box) return;

    var locationText = String(weather.location || "").trim();
    var airText = String(weather.air_temp || "").trim();
    var highText = String(weather.high_temp || "").trim();
    var lowText = String(weather.low_temp || "").trim();
    var forecastText = String(weather.short_forecast || "").trim();
    var updatedText = String(weather.updated || "").trim();
    var errorText = String(weather.error || "").trim();

    if (place) {
      place.textContent = mapState.selectedLocation
        ? (locationText || "Near selected location")
        : "Select a location for local conditions";
    }
    if (content) content.hidden = !mapState.selectedLocation;
    if (air) air.textContent = airText || "—";
    if (high) high.textContent = highText || "—";
    if (low) low.textContent = lowText || "—";
    if (forecast) {
      forecast.hidden = !forecastText;
      forecast.textContent = forecastText;
    }
    if (updated) updated.textContent = updatedText ? "Updated " + updatedText : "";
    if (errorBox) {
      errorBox.hidden = !errorText;
      errorBox.textContent = errorText;
    }

    box.hidden = false;
  }

  function formatTripObservationTime(value) {
    var parsed = new Date(value);
    if (!Number.isFinite(parsed.getTime())) return String(value || "");
    return parsed.toLocaleString([], {
      month: "short",
      day: "numeric",
      hour: "numeric",
      minute: "2-digit",
      timeZoneName: "short"
    });
  }

  function tripValue(value, digits, suffix) {
    var n = Number(value);
    if (!Number.isFinite(n)) return "—";
    return n.toFixed(digits) + suffix;
  }

  function pointInFishingPolygon(lat, lon, polygon) {
    if (!Array.isArray(polygon) || polygon.length < 3) return false;
    var inside = false;
    for (var i = 0, j = polygon.length - 1; i < polygon.length; j = i++) {
      var yi = Number(polygon[i][0]);
      var xi = Number(polygon[i][1]);
      var yj = Number(polygon[j][0]);
      var xj = Number(polygon[j][1]);
      if (![yi, xi, yj, xj].every(Number.isFinite)) continue;
      var intersects =
        ((yi > lat) !== (yj > lat)) &&
        (lon < (xj - xi) * (lat - yi) / ((yj - yi) || 1e-12) + xi);
      if (intersects) inside = !inside;
    }
    return inside;
  }

  function fishingReportAgeDays(dateText) {
    var d = new Date(String(dateText || "") + "T12:00:00Z");
    if (!Number.isFinite(d.getTime())) return NaN;
    return Math.max(0, Math.floor((Date.now() - d.getTime()) / 86400000));
  }

  function nearestFishingStructureForPoint(lat, lon) {
    var halfLat = 1.0;
    var cosLat = Math.max(0.25, Math.cos(lat * Math.PI / 180));
    var halfLon = Math.min(2.0, 1.0 / cosLat);
    var bounds = L.latLngBounds(
      [Math.max(-89.9, lat - halfLat), Math.max(-179.9, lon - halfLon)],
      [Math.min(89.9, lat + halfLat), Math.min(179.9, lon + halfLon)]
    );
    var requestURL = structureFeatureQueryURL(bounds);

    return fetch(requestURL, {headers: {"Accept":"application/geo+json, application/json"}})
      .then(function(response) {
        if (!response.ok) throw new Error("HTTP " + response.status);
        return response.json();
      })
      .then(function(payload) {
        var features = payload && Array.isArray(payload.features) ? payload.features : [];
        var origin = L.latLng(lat, lon);
        var best = null;
        features.forEach(function(feature) {
          var name = String(feature && feature.properties && feature.properties.name || "").trim();
          var coords = feature && feature.geometry && feature.geometry.coordinates;
          if (!name || !isFishingStructureName(name) ||
              !Array.isArray(coords) || coords.length < 2) return;
          var flon = Number(coords[0]);
          var flat = Number(coords[1]);
          if (!Number.isFinite(flat) || !Number.isFinite(flon)) return;
          var nm = map.distance(origin, L.latLng(flat, flon)) / 1852;
          if (!best || nm < best.distance_nm) {
            best = {name:name, distance_nm:nm, lat:flat, lon:flon};
          }
        });
        return best;
      });
  }

  function fishingReportsNearPoint(lat, lon) {
    return new Promise(function(resolve) {
      loadFishingReports(function(ok, payload) {
        if (!ok || !payload || !Array.isArray(payload.reports)) {
          resolve({nearby:[], regional:[]});
          return;
        }

        var origin = L.latLng(lat, lon);
        var nearby = [];
        var regional = [];

        payload.reports.forEach(function(report) {
          var type = String(report.position_type || "").toLowerCase();
          if (type === "exact_point" || type === "derived_point") {
            var rlat = Number(report.lat);
            var rlon = Number(report.lon);
            if (!Number.isFinite(rlat) || !Number.isFinite(rlon)) return;
            var nm = map.distance(origin, L.latLng(rlat, rlon)) / 1852;
            if (nm <= 100) {
              nearby.push({
                species: String(report.species || "Report"),
                date: String(report.date || ""),
                count: String(report.count || ""),
                distance_nm: nm,
                confidence: String(report.confidence || type)
              });
            }
          } else if (type === "region" && pointInFishingPolygon(lat, lon, report.polygon)) {
            regional.push({
              species: String(report.species || "Report"),
              date: String(report.date || ""),
              locationText: String(report.locationText || ""),
              confidence: String(report.confidence || "regional")
            });
          }
        });

        nearby.sort(function(a, b) {
          var ageA = fishingReportAgeDays(a.date);
          var ageB = fishingReportAgeDays(b.date);
          if (Number.isFinite(ageA) && Number.isFinite(ageB) && ageA !== ageB) return ageA - ageB;
          return a.distance_nm - b.distance_nm;
        });

        resolve({nearby:nearby, regional:regional});
      });
    });
  }

  function renderFishingPlanning(water, structure, reports) {
    var status = document.getElementById("fishing-planning-status");
    var summary = document.getElementById("fishing-planning-summary");
    var sst = document.getElementById("fishing-planning-sst");
    var sstDetail = document.getElementById("fishing-planning-sst-detail");
    var chl = document.getElementById("fishing-planning-chl");
    var chlDetail = document.getElementById("fishing-planning-chl-detail");
    var structureValue = document.getElementById("fishing-planning-structure");
    var structureDetail = document.getElementById("fishing-planning-structure-detail");
    var reportValue = document.getElementById("fishing-planning-reports");
    var reportDetail = document.getElementById("fishing-planning-reports-detail");
    var errorBox = document.getElementById("fishing-planning-error");

    var factors = [];
    var caveats = [];

    var sstPointF = Number(water && water.sst && water.sst.point_f);
    var sstSpreadF = Number(water && water.sst && water.sst.spread_f);
    if (sst) sst.textContent = Number.isFinite(sstPointF) ? sstPointF.toFixed(1) + "°F" : "Unavailable";
    if (sstDetail) {
      if (Number.isFinite(sstSpreadF)) {
        var breakText = sstSpreadF >= 2.0
          ? "pronounced local temp-break signal"
          : (sstSpreadF >= 1.0 ? "moderate local temp-break signal" : "weak local temp gradient");
        sstDetail.textContent =
          breakText + " · " + sstSpreadF.toFixed(1) +
          "°F spread in the nearby ~15 nmi sampling window";
        if (sstSpreadF >= 1.0) factors.push("a useful Sea Surface Temp gradient");
      } else {
        sstDetail.textContent = "No local gradient estimate available.";
      }
    }

    var chlPoint = Number(water && water.chlorophyll && water.chlorophyll.point_mg_m3);
    var chlMin = Number(water && water.chlorophyll && water.chlorophyll.min_mg_m3);
    var chlMax = Number(water && water.chlorophyll && water.chlorophyll.max_mg_m3);
    if (chl) {
      chl.textContent = Number.isFinite(chlPoint) ? chlPoint.toFixed(2) + " mg/m³" : "Unavailable";
    }
    if (chlDetail) {
      if (Number.isFinite(chlPoint)) {
        var waterClass = chlPoint <= 0.20 ? "clearer/blue water"
          : (chlPoint <= 0.30 ? "clear-water transition"
          : (chlPoint <= 0.50 ? "moderate chlorophyll" : "greener water"));
        var edge = Number.isFinite(chlMin) && Number.isFinite(chlMax) && chlMin <= 0.30 && chlMax >= 0.30;
        chlDetail.textContent = waterClass + (edge ? " · 0.30 mg/m³ transition crosses nearby window" : "");
        if (chlPoint <= 0.30) factors.push("cleaner water");
        if (edge) factors.push("a nearby chlorophyll edge");
      } else {
        chlDetail.textContent = "No chlorophyll value available.";
      }
    }

    if (structure) {
      if (structureValue) structureValue.textContent = structure.name;
      if (structureDetail) structureDetail.textContent = structure.distance_nm.toFixed(1) + " nmi from selected destination";
      if (structure.distance_nm <= 15) factors.push("nearby named structure");
    } else {
      if (structureValue) structureValue.textContent = "None nearby";
      if (structureDetail) structureDetail.textContent = "No fishing-relevant NOAA named feature found in the search window.";
    }

    reports = reports || {nearby:[], regional:[]};
    var nearby = Array.isArray(reports.nearby) ? reports.nearby : [];
    var regional = Array.isArray(reports.regional) ? reports.regional : [];
    if (nearby.length) {
      var newest = nearby[0];
      var age = fishingReportAgeDays(newest.date);
      if (reportValue) reportValue.textContent = nearby.length + " within 100 nmi";
      if (reportDetail) {
        reportDetail.textContent =
          newest.species + " · " + newest.distance_nm.toFixed(0) + " nmi away" +
          (Number.isFinite(age) ? " · " + age + " day" + (age === 1 ? "" : "s") + " old" : "") +
          (newest.count ? " · " + newest.count : "");
      }
      if (Number.isFinite(age) && age <= 14) factors.push("recent nearby fishing activity");
    } else if (regional.length) {
      var reg = regional[0];
      if (reportValue) reportValue.textContent = "Regional report";
      if (reportDetail) reportDetail.textContent =
        reg.species + (reg.locationText ? " · " + reg.locationText : "") + " · broad location confidence";
      factors.push("regional fishing activity");
    } else {
      if (reportValue) reportValue.textContent = "No nearby reports";
      if (reportDetail) reportDetail.textContent = "No point report within 100 nmi and no regional report covering the selected destination.";
    }

    if (water && water.error) caveats.push(String(water.error));

    if (summary) {
      var unique = [];
      factors.forEach(function(item) {
        if (unique.indexOf(item) === -1) unique.push(item);
      });
      if (unique.length >= 3) {
        summary.innerHTML = "<strong>Fishing setup:</strong> promising alignment of " +
          escapeHTML(unique.slice(0, 4).join(", ")) + ".";
      } else if (unique.length) {
        summary.innerHTML = "<strong>Fishing setup:</strong> some favorable signals — " +
          escapeHTML(unique.join(", ")) +
          " — but the selected point does not yet show a full multi-factor convergence.";
      } else {
        summary.innerHTML = "<strong>Fishing setup:</strong> limited positive convergence detected from the currently available water, structure, and report data.";
      }
    }

    if (status) status.textContent = "Selected-destination fishing context";
    if (errorBox) {
      errorBox.hidden = caveats.length === 0;
      errorBox.textContent = caveats.join(" | ");
    }
  }

  function refreshFishingPlanningForSelectedLocation() {
    if (!mapState.selectedLocation) return;

    var status = document.getElementById("fishing-planning-status");
    var summary = document.getElementById("fishing-planning-summary");
    var errorBox = document.getElementById("fishing-planning-error");
    if (status) status.textContent = "Loading water, structure, and reports…";
    if (summary) summary.textContent = "Evaluating the selected fishing destination…";
    if (errorBox) errorBox.hidden = true;

    var lat = Number(mapState.selectedLocation.lat);
    var lon = Number(mapState.selectedLocation.lon);
    var serial = ++fishingPlanningRequestSerial;

    var waterPromise = fetch(
      "/fishing-water?lat=" + encodeURIComponent(lat.toFixed(5)) +
      "&lon=" + encodeURIComponent(lon.toFixed(5)),
      {headers: {"Accept":"application/json"}, cache:"no-store"}
    ).then(function(response) {
      if (!response.ok) {
        return response.text().then(function(detail) {
          throw new Error(String(detail || "HTTP " + response.status).trim());
        });
      }
      return response.json();
    }).catch(function(err) {
      return {error:"Water data: " + String(err && err.message ? err.message : err || "unavailable")};
    });

    Promise.all([
      waterPromise,
      nearestFishingStructureForPoint(lat, lon).catch(function() { return null; }),
      fishingReportsNearPoint(lat, lon)
    ]).then(function(results) {
      if (serial !== fishingPlanningRequestSerial) return;
      renderFishingPlanning(results[0], results[1], results[2]);
    });
  }

  function renderOffshoreTrip(payload) {
    payload = payload || {};
    var card = document.getElementById("offshore-trip-card");
    if (payload.is_offshore !== true) {
      if (card) card.hidden = true;
      return;
    }
    var loading = document.getElementById("offshore-trip-loading");
    var content = document.getElementById("offshore-trip-content");
    var errorBox = document.getElementById("offshore-trip-error");
    var coords = document.getElementById("offshore-trip-coords");
    var summary = document.getElementById("offshore-trip-summary");
    var buoyLine = document.getElementById("offshore-trip-buoy");
    var wind = document.getElementById("offshore-trip-wind");
    var gust = document.getElementById("offshore-trip-gust");
    var wave = document.getElementById("offshore-trip-wave");
    var period = document.getElementById("offshore-trip-period");
    var direction = document.getElementById("offshore-trip-direction");
    var watchWrap = document.getElementById("offshore-trip-watch-wrap");
    var watch = document.getElementById("offshore-trip-watch");
    var forecast = document.getElementById("offshore-trip-forecast");

    if (!card) return;
    card.hidden = false;
    if (loading) loading.hidden = true;

    var err = String(payload.error || "").trim();
    if (errorBox) {
      errorBox.hidden = !err;
      errorBox.textContent = err;
    }

    if (coords && mapState.selectedLocation) {
      coords.textContent =
        Number(mapState.selectedLocation.lat).toFixed(4) + ", " +
        Number(mapState.selectedLocation.lon).toFixed(4);
    }

    var buoy = payload.buoy || null;
    var periods = Array.isArray(payload.periods) ? payload.periods.slice(0, 4) : [];
    var alerts = Array.isArray(payload.alerts) ? payload.alerts : [];
    var watchItems = [];

    if (buoy) {
      var windKT = Number(buoy.wind_kt);
      var gustKT = Number(buoy.gust_kt);
      var waveFT = Number(buoy.wave_ft);
      var dominantSEC = Number(buoy.dominant_period_sec);

      if (wind) wind.textContent = tripValue(windKT, 1, " kt");
      if (gust) gust.textContent = tripValue(gustKT, 1, " kt");
      if (wave) wave.textContent = tripValue(waveFT, 1, " ft");
      if (period) period.textContent = tripValue(dominantSEC, 0, " s");
      if (direction) {
        direction.textContent = Number.isFinite(Number(buoy.mean_wave_direction_deg))
          ? Math.round(Number(buoy.mean_wave_direction_deg)) + "°"
          : "—";
      }

      if (buoyLine) {
        buoyLine.innerHTML =
          "<strong>Observed buoy:</strong> " +
          escapeHTML(String(buoy.station || "")) +
          (buoy.name ? " — " + escapeHTML(String(buoy.name)) : "") +
          (Number.isFinite(Number(buoy.distance_nm))
            ? " · " + Number(buoy.distance_nm).toFixed(1) + " nmi from destination"
            : "") +
          (buoy.observation_time
            ? " · observation " + escapeHTML(formatTripObservationTime(buoy.observation_time))
            : "");
      }

      if (Number.isFinite(gustKT) && gustKT >= 25) {
        watchItems.push("Observed gusts are at least 25 kt.");
      } else if (Number.isFinite(windKT) && windKT >= 20) {
        watchItems.push("Observed sustained wind is at least 20 kt.");
      } else if (Number.isFinite(windKT) && windKT >= 15) {
        watchItems.push("Observed sustained wind is at least 15 kt.");
      }

      if (Number.isFinite(waveFT) && waveFT >= 8) {
        watchItems.push("Observed significant wave height is at least 8 ft.");
      } else if (Number.isFinite(waveFT) && waveFT >= 6) {
        watchItems.push("Observed significant wave height is at least 6 ft.");
      }

      if (Number.isFinite(waveFT) && Number.isFinite(dominantSEC) &&
          waveFT >= 4 && dominantSEC <= 8) {
        watchItems.push("Observed seas are short-period (8 s or less) with at least 4 ft significant wave height.");
      }
    } else {
      if (buoyLine) buoyLine.innerHTML = "<strong>Observed buoy:</strong> no usable nearby NDBC wave observation found.";
      if (wind) wind.textContent = "—";
      if (gust) gust.textContent = "—";
      if (wave) wave.textContent = "—";
      if (period) period.textContent = "—";
      if (direction) direction.textContent = "—";
    }

    alerts.forEach(function(alert) {
      watchItems.push("NWS alert: " + String(alert));
    });

    if (summary) {
      var zone = String(payload.zone || "").trim();
      var summaryText = watchItems.length
        ? "<strong>Planning snapshot:</strong> review the watch items below before committing to the run."
        : "<strong>Planning snapshot:</strong> no threshold watch items were triggered by the current buoy observation or NWS alerts.";
      if (zone) summaryText += " NWS zone " + escapeHTML(zone) + ".";
      summary.innerHTML = summaryText;
    }

    if (watch && watchWrap) {
      watch.innerHTML = watchItems.map(function(item) {
        return "<li>" + escapeHTML(item) + "</li>";
      }).join("");
      watchWrap.hidden = watchItems.length === 0;
    }

    if (forecast) {
      forecast.innerHTML = periods.map(function(item) {
        var name = escapeHTML(String(item && item.name || ""));
        var text = escapeHTML(String(item && item.forecast || ""));
        return '<div class="offshore-trip-period">' +
          (name ? "<strong>" + name + "</strong>" : "") +
          (text ? "<p>" + text + "</p>" : "") +
          "</div>";
      }).join("");
    }

    if (content) content.hidden = false;
  }

  function refreshOffshoreTripForSelectedLocation() {
    if (!mapState.selectedLocation) return;

    var card = document.getElementById("offshore-trip-card");
    var loading = document.getElementById("offshore-trip-loading");
    var content = document.getElementById("offshore-trip-content");
    var errorBox = document.getElementById("offshore-trip-error");
    var coords = document.getElementById("offshore-trip-coords");

    // Hide by default. The card is only revealed after the server confirms
    // that the selected point belongs to an offshore/coastal-ocean NWS zone.
    if (card) card.hidden = true;
    if (loading) loading.hidden = false;
    if (content) content.hidden = true;
    if (errorBox) errorBox.hidden = true;
    fishingPlanningRequestSerial++;

    if (coords) {
      coords.textContent =
        Number(mapState.selectedLocation.lat).toFixed(4) + ", " +
        Number(mapState.selectedLocation.lon).toFixed(4);
    }

    var serial = ++offshoreTripRequestSerial;
    var requestURL =
      "/offshore-trip?lat=" +
      encodeURIComponent(Number(mapState.selectedLocation.lat).toFixed(5)) +
      "&lon=" +
      encodeURIComponent(Number(mapState.selectedLocation.lon).toFixed(5));

    fetch(requestURL, {
      method: "GET",
      headers: {"Accept": "application/json"},
      cache: "no-store"
    })
      .then(function(response) {
        if (!response.ok) {
          return response.text().then(function(detail) {
            throw new Error(String(detail || "HTTP " + response.status).trim());
          });
        }
        return response.json();
      })
      .then(function(payload) {
        if (serial !== offshoreTripRequestSerial) return;

        if (!payload || payload.is_offshore !== true) {
          if (card) card.hidden = true;
          if (content) content.hidden = true;
          if (loading) loading.hidden = true;
          return;
        }

        if (card) card.hidden = false;
        refreshFishingPlanningForSelectedLocation();
        renderOffshoreTrip(payload);
      })
      .catch(function() {
        if (serial !== offshoreTripRequestSerial) return;
        // Classification failure must not produce an inappropriate offshore
        // card at a land, Delta, or Bay selection.
        if (card) card.hidden = true;
        if (loading) loading.hidden = true;
        if (content) content.hidden = true;
      });
  }

  function renderMarineForecast(payload) {
    payload = payload || {};

    var card = document.getElementById("marine-forecast-card");
    var title = document.getElementById("marine-forecast-title");
    var zoneLine = document.getElementById("marine-forecast-zone");
    var alertsBox = document.getElementById("marine-forecast-alerts");
    var periodsBox = document.getElementById("marine-forecast-periods");
    var note = document.getElementById("marine-forecast-note");
    var errorBox = document.getElementById("marine-forecast-error");

    var zone = String(payload.zone || "").trim().toUpperCase();
    var updated = String(payload.updated || "").trim();
    var periods = Array.isArray(payload.periods) ? payload.periods : [];
    var alerts = Array.isArray(payload.alerts) ? payload.alerts : [];
    var error = String(payload.error || "").trim();

    if (title) title.textContent = "NWS Forecast — selected location";

    if (zoneLine) {
      zoneLine.hidden = !zone;
      zoneLine.textContent = zone
        ? "National Weather Service forecast zone " + zone +
          (updated ? " · Marine forecast updated " + updated : "")
        : "";
    }

    if (alertsBox) {
      alertsBox.hidden = alerts.length === 0;
      alertsBox.innerHTML = alerts.map(function(alert) {
        return '<span class="marine-alert">⚠ NWS alert — ' +
          escapeHTML(alert) + "</span>";
      }).join("");
    }

    if (periodsBox) {
      periodsBox.hidden = periods.length === 0;
      periodsBox.innerHTML = periods.map(function(period) {
        var name = escapeHTML(period && period.name ? period.name : "");
        var forecast = escapeHTML(period && period.forecast ? period.forecast : "");
        return '<div class="marine-period">' +
          (name ? "<strong>" + name + "</strong>" : "") +
          (forecast ? "<p>" + forecast + "</p>" : "") +
          "</div>";
      }).join("");
    }

    if (note) note.hidden = periods.length === 0;

    if (errorBox) {
      errorBox.hidden = !error;
      errorBox.textContent = error;
    }

    if (card) card.hidden = !(periods.length || error);
    setMarineZoneOverlay(zone, payload.geometry || null);
    renderSelectedLocationWeather(payload.weather || {});
  }

  function refreshMarineForecastForSelectedLocation() {
    if (!mapState.selectedLocation) return;

    renderSelectedLocationWeather({short_forecast: "Loading NWS point forecast…"});

    var serial = ++marineForecastRequestSerial;
    var control = document.getElementById("map-marine-zone-control");
    var checkbox = document.getElementById("map-show-marine-zone");
    var label = document.getElementById("map-marine-zone-label");

    if (control) control.hidden = false;
    if (checkbox) checkbox.disabled = true;
    if (label) label.textContent = "Finding NWS forecast zone…";

    var requestURL =
      "/marine-forecast?lat=" +
      encodeURIComponent(Number(mapState.selectedLocation.lat).toFixed(5)) +
      "&lon=" +
      encodeURIComponent(Number(mapState.selectedLocation.lon).toFixed(5));

    fetch(requestURL, {
      method: "GET",
      headers: {"Accept": "application/json"},
      cache: "no-store"
    })
      .then(function(response) {
        if (!response.ok) throw new Error("HTTP " + response.status);
        return response.json();
      })
      .then(function(payload) {
        if (serial !== marineForecastRequestSerial) return;
        renderMarineForecast(payload);
      })
      .catch(function(err) {
        if (serial !== marineForecastRequestSerial) return;
        setMarineZoneOverlay("", null);

        var card = document.getElementById("marine-forecast-card");
        var title = document.getElementById("marine-forecast-title");
        var periodsBox = document.getElementById("marine-forecast-periods");
        var alertsBox = document.getElementById("marine-forecast-alerts");
        var note = document.getElementById("marine-forecast-note");
        var errorBox = document.getElementById("marine-forecast-error");
        var zoneLine = document.getElementById("marine-forecast-zone");

        if (title) title.textContent = "NWS Forecast — selected location";
        if (zoneLine) zoneLine.hidden = true;
        if (periodsBox) periodsBox.hidden = true;
        if (alertsBox) alertsBox.hidden = true;
        if (note) note.hidden = true;
        if (errorBox) {
          errorBox.hidden = false;
          errorBox.textContent =
            "NWS forecast information could not be refreshed: " +
            (err && err.message ? err.message : "unknown error");
        }
        if (card) card.hidden = false;
        renderSelectedLocationWeather({
          error: "NWS point weather could not be refreshed."
        });
      });
  }

  function selectSailingLocation(lat, lon, recenter) {
    var latText = String(lat == null ? "" : lat).trim();
    var lonText = String(lon == null ? "" : lon).trim();
    if (!latText || !lonText) {
      if (coordinateError) {
        coordinateError.textContent =
          "Enter both latitude and longitude.";
      }
      return false;
    }

    lat = Number(latText);
    lon = Number(lonText);
    if (!Number.isFinite(lat) || !Number.isFinite(lon) ||
        lat < -90 || lat > 90 || lon < -180 || lon > 180) {
      if (coordinateError) {
        coordinateError.textContent =
          "Enter a valid latitude (-90 to 90) and longitude (-180 to 180).";
      }
      return false;
    }

    if (coordinateError) coordinateError.textContent = "";

    mapState.selectedLocation = {lat:lat, lon:lon};
    var point = L.latLng(lat, lon);

    if (selectedMarker) {
      selectedMarker.setLatLng(point);
    } else {
      selectedMarker = symbolMarker(
        lat,
        lon,
        "★",
        "request",
        "Selected location"
      );
    }

    if (recenter) {
      map.panTo(point);
    }

    renderSearchControls();
    updateRecenterControls();
    renderWindMarkers();
    refreshMarineForecastForSelectedLocation();
    refreshOffshoreTripForSelectedLocation();
    return true;
  }

  function persistSelectedLocationInURL() {
    if (!mapState.selectedLocation || !window.history || !window.history.replaceState) {
      return;
    }

    var pageURL = new URL(window.location.href);
    pageURL.searchParams.set(
      "lat",
      Number(mapState.selectedLocation.lat).toFixed(5)
    );
    pageURL.searchParams.set(
      "lon",
      Number(mapState.selectedLocation.lon).toFixed(5)
    );
    window.history.replaceState(null, "", pageURL.toString());
  }

  function escapeHTML(value) {
    return String(value)
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;")
      .replace(/'/g, "&#39;");
  }

  var windInfo = document.getElementById("map-wind-info");
  function showWindInfo(station, name, distance, url, currentStation, currentName, currentDistance, currentNote) {
    if (!windInfo) return;
    var action = "";
    if (url) {
      action = '<br><a class="map-station-report-link" href="' +
        escapeHTML(reportURLWithMapView(url)) +
        '" data-base-href="' + escapeHTML(url) +
        '">Use this wind station</a>';
    }
    var currentLine = "";
    if (currentStation) {
      currentLine =
        "<br><strong>◆ Currents preview:</strong> " +
        escapeHTML(currentStation) +
        (currentName ? " — " + escapeHTML(currentName) : "") +
        (currentDistance ? " (" + escapeHTML(currentDistance) + " from wind station)" : "");
    } else {
      currentLine =
        "<br><strong>◆ Currents preview:</strong> " +
        escapeHTML(currentNote || "No nearby currents prediction station available.");
    }
    windInfo.innerHTML =
      '<button type="button" class="map-wind-info-close" aria-label="Close station information">×</button>' +
      "<strong>△ " + escapeHTML(station) + "</strong> — " + escapeHTML(name) +
      (distance ? "<br>" + escapeHTML(distance) + " from selected location" : "") +
      currentLine +
      action;
    windInfo.hidden = false;
  }
  function hideWindInfo() {
    if (windInfo) windInfo.hidden = true;
  }
  if (windInfo) {
    windInfo.addEventListener("click", function(e) {
      if (e.target.closest && e.target.closest(".map-wind-info-close")) {
        e.preventDefault();
        hideWindInfo();
        restoreSelectedCurrentsStation();
      }
    });
  }


  function findStationsWithoutReload() {
    if (mapState.stationSearch.busy) return;

    if (!mapState.selectedLocation) {
      setStationSearchState(
        false,
        "",
        "Select a sailing location before searching for stations."
      );
      return;
    }

    setStationSearchState(
      true,
      "point",
      "Finding stations near the selected location…"
    );

    var requestURL =
      "/wind-stations?lat=" +
        encodeURIComponent(Number(mapState.selectedLocation.lat).toFixed(5)) +
      "&lon=" +
        encodeURIComponent(Number(mapState.selectedLocation.lon).toFixed(5)) +
      "&selected_station=" +
        encodeURIComponent(normalizeWindStationID(mapState.selectedWindStationID));

    var settled = false;
    var timeoutID = window.setTimeout(function() {
      if (settled) return;
      settled = true;
      setStationSearchState(false, "", "Station search timed out. Try again.");
    }, 8000);

    fetch(requestURL, {
      method: "GET",
      headers: {"Accept": "application/json"},
      cache: "no-store"
    })
      .then(function(response) {
        if (!response.ok) throw new Error("HTTP " + response.status);
        return response.json();
      })
      .then(function(payload) {
        if (settled) return;
        settled = true;
        window.clearTimeout(timeoutID);

        var candidates =
          payload && Array.isArray(payload.candidates)
            ? payload.candidates
            : [];

        var selectedID =
          normalizeWindStationID(mapState.selectedWindStationID);
        var seen = Object.create(null);

        mapState.windCandidates = candidates
          .filter(function(c) {
            var id = normalizeWindStationID(c.station);
            if (!id || id === selectedID || seen[id]) return false;
            seen[id] = true;
            return true;
          })
          .map(function(c) {
            return {
              station: normalizeWindStationID(c.station),
              name: String(c.name || c.station || ""),
              distance: String(c.distance || ""),
              wind: String(c.wind || ""),
              observationAge: String(c.observation_age || ""),
              lat: Number(c.lat),
              lon: Number(c.lon),
              url: String(c.url || ""),
              isAuto: false,
              currentStation: String(c.current_station || ""),
              currentName: String(c.current_name || ""),
              currentDistance: String(c.current_distance || ""),
              currentLat: Number(c.current_lat),
              currentLon: Number(c.current_lon),
              currentNote: String(c.current_note || "")
            };
          });

        renderWindMarkers();

        var stationList = document.getElementById("map-station-list");
        if (stationList) {
          if (mapState.windCandidates.length) {
            var rows = mapState.windCandidates.map(function(c) {
              var station = escapeHTML(c.station);
              var name = escapeHTML(c.name || c.station);
              var distance = escapeHTML(c.distance);
              var wind = escapeHTML(c.wind || "—");
              var observationAge = escapeHTML(c.observationAge || "—");
              var reportURL = escapeHTML(c.url || "#");
              return "<tr>" +
                '<td><a class="map-station-report-link" href="' + reportURL +
                '" data-base-href="' + reportURL + '">' + station + "</a></td>" +
                '<td><a class="map-station-report-link" href="' + reportURL +
                '" data-base-href="' + reportURL + '">' + name + "</a></td>" +
                "<td>" + wind + "</td>" +
                "<td>" + observationAge + "</td>" +
                "<td>" + distance + "</td>" +
                "</tr>";
            }).join("");

            stationList.innerHTML =
              '<div class="map-station-list-title">Nearby Wind Stations</div>' +
              '<div class="map-station-table-wrap">' +
              '<table class="map-station-table">' +
              '<thead><tr><th>Station</th><th>Name</th><th>Wind</th><th>Age</th><th>From selected location</th></tr></thead>' +
              "<tbody>" + rows + "</tbody></table></div>";
          } else {
            stationList.innerHTML =
              '<div class="map-station-list-title">Nearby Wind Stations</div>' +
              '<div class="meta">No nearby stations found.</div>';
          }
        }

        var count = mapState.windCandidates.length;
        setStationSearchState(
          false,
          "",
          count
            ? count + " nearby station" + (count === 1 ? "" : "s") + " shown."
            : "No nearby stations found."
        );
      })
      .catch(function(err) {
        if (settled) return;
        settled = true;
        window.clearTimeout(timeoutID);
        console.error("Nearby station lookup failed", err);
        setStationSearchState(
          false,
          "",
          "Station lookup failed: " +
            (err && err.message ? err.message : "unknown error")
        );
      });
  }


  document.addEventListener("click", function(e) {
    var link = e.target.closest && e.target.closest("a.map-station-report-link");
    if (!link) return;

    var baseHref =
      link.getAttribute("data-base-href") ||
      link.getAttribute("href");

    if (!baseHref) return;

    e.preventDefault();
    e.stopPropagation();
    window.location.assign(reportURLWithMapView(baseHref));
  });

  function wireSearchControl(control, handler) {
    if (!control) return;
    L.DomEvent.disableClickPropagation(control);
    L.DomEvent.disableScrollPropagation(control);

    function activate(e) {
      if (e) {
        e.preventDefault();
        e.stopPropagation();
      }
      if (control.getAttribute("aria-disabled") === "true") return;
      handler();
    }

    control.addEventListener("click", activate);
    control.addEventListener("keydown", function(e) {
      if (e.key === "Enter" || e.key === " ") activate(e);
    });
  }

  wireSearchControl(findPoint, function() {
    if (!mapState.selectedLocation) return;
    findStationsWithoutReload();
  });

  function restoreGeolocateControl() {
    if (!geolocate) return;
    geolocate.disabled = false;
  }

  if (geolocate) {
    if (!navigator.geolocation) {
      geolocate.disabled = true;
      geolocate.title = "Location is not available in this browser";
    } else {
      geolocate.addEventListener("click", function() {
        if (geolocate.disabled) return;
        if (coordinateError) coordinateError.textContent = "";
        geolocate.disabled = true;

        var centerMenu = document.getElementById("map-center-menu");
        if (centerMenu) centerMenu.open = false;

        navigator.geolocation.getCurrentPosition(
          function(position) {
            restoreGeolocateControl();
            if (!position || !position.coords) {
              if (coordinateError) {
                coordinateError.textContent =
                  "Your location could not be determined.";
              }
              return;
            }

            map.panTo([
              position.coords.latitude,
              position.coords.longitude
            ]);
          },
          function(error) {
            restoreGeolocateControl();

            var message = "Your location could not be determined.";
            if (error && error.code === 1) {
              message =
                "Location permission was denied. Allow location access and try again.";
            } else if (error && error.code === 2) {
              message =
                "Your device could not determine its current location.";
            } else if (error && error.code === 3) {
              message =
                "Location lookup timed out. Try again.";
            }

            if (coordinateError) coordinateError.textContent = message;
          },
          {
            enableHighAccuracy: true,
            timeout: 10000,
            maximumAge: 60000
          }
        );
      });
    }
  }

  map.on("click", function(e) {
    var originalTarget =
      e && e.originalEvent && e.originalEvent.target
        ? e.originalEvent.target
        : null;

    if (originalTarget && originalTarget.closest &&
        originalTarget.closest(".map-wind-candidate")) {
      // Selecting a wind source must never move the ★ sailing location.
      return;
    }

    if (selectSailingLocation(e.latlng.lat, e.latlng.lng, false)) {
      persistSelectedLocationInURL();
    }
  });

  // A selected location may already be present when the Planning page loads
  // from lat/lon carried in the URL. In that case selectSailingLocation() is
  // not invoked, so explicitly initialize the offshore planning card.
  if (mapState.selectedLocation) {
    refreshOffshoreTripForSelectedLocation();
  }

  if (reset) {
    reset.addEventListener("click", function() {
      if (!mapState.selectedLocation) return;

      mapState.selectedLocation = null;
      mapState.windCandidates = [];
      marineForecastRequestSerial++;
      setMarineZoneOverlay("", null);
      var marineForecastCard = document.getElementById("marine-forecast-card");
      if (marineForecastCard) marineForecastCard.hidden = true;
      offshoreTripRequestSerial++;
      fishingPlanningRequestSerial++;
      var offshoreTripCard = document.getElementById("offshore-trip-card");
      if (offshoreTripCard) offshoreTripCard.hidden = true;
      renderSelectedLocationWeather({});
      setStationSearchState(false, "", "");

      if (selectedMarker) {
        map.removeLayer(selectedMarker);
        selectedMarker = null;
      }

      hideWindInfo();
      restoreSelectedCurrentsStation();

      var distanceWarning = document.querySelector(".wind-distance-warning");
      if (distanceWarning) distanceWarning.remove();

      var stationList = document.getElementById("map-station-list");
      if (stationList) {
        stationList.innerHTML =
          '<div class="meta">Select a location to find nearby wind stations.</div>';
      }

      if (window.history && window.history.replaceState) {
        var resetURL = new URL(window.location.href);
        resetURL.searchParams.delete("lat");
        resetURL.searchParams.delete("lon");
        window.history.replaceState(null, "", resetURL.toString());
      }
      syncCoordinateInputsToMapCenter();
      if (coordinateError) coordinateError.textContent = "";
      renderSearchControls();
      updateRecenterControls();
      renderWindMarkers();
    });
  }

})();

(function(){
  var unitSelect = document.getElementById("wind-unit-select");
  if (!unitSelect) return;
  unitSelect.addEventListener("change", function() {
    var target = new URL(window.location.href);
    target.searchParams.set("wind_unit", unitSelect.value === "mph" ? "mph" : "kts");
    window.location.href = target.toString();
  });
})();

(function(){
  var hoursSelect = document.getElementById("wind-reading-hours");
  var unitSelect = document.getElementById("wind-unit-select");
  var tableBody = document.getElementById("wind-readings-body");
  var chart = document.getElementById("wind-reading-chart");
  var status = document.getElementById("wind-reading-status");
  if (!hoursSelect || !tableBody) return;

  function escapeHTML(value) {
    return String(value)
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;")
      .replace(/'/g, "&#39;");
  }

  function updateHoursInLinks(hours) {
    var pageURL = new URL(window.location.href);
    pageURL.searchParams.set("wind_hours", String(hours));
    pageURL.searchParams.delete("wind_readings");
    window.history.replaceState(null, "", pageURL.toString());

    var detailsLink = document.querySelector("#full-report-card a.details-link");
    if (detailsLink) {
      var detailsURL = new URL(detailsLink.href, window.location.href);
      detailsURL.searchParams.set("wind_hours", String(hours));
      detailsURL.searchParams.delete("wind_readings");
      detailsLink.href = detailsURL.toString();
    }
  }

  function renderReadings(readings) {
    tableBody.innerHTML = readings.map(function(item) {
      return "<tr><td>" + escapeHTML(item.time) +
        "</td><td>" + escapeHTML(item.direction) +
        "</td><td>" + escapeHTML(item.wind) +
        "</td><td>" + escapeHTML(item.gust) +
        "</td><td>" + escapeHTML(item.age) +
        "</td></tr>";
    }).join("");
  }

  function readingNumber(value) {
    var match = String(value || "").match(/-?\d+(?:\.\d+)?/);
    return match ? Number(match[0]) : NaN;
  }

  function renderWindChart(readings) {
    if (!chart) return;

    var chartUnit = unitSelect && unitSelect.value === "mph" ? "mph" : "kt";
    var points = (Array.isArray(readings) ? readings : []).map(function(item) {
      return {
        time: String(item.time || ""),
        wind: readingNumber(item.wind),
        gust: readingNumber(item.gust)
      };
    }).filter(function(item) {
      return Number.isFinite(item.wind) || Number.isFinite(item.gust);
    }).reverse();

    if (!points.length) {
      chart.innerHTML = '<div class="wind-chart-empty">No recent wind readings available.</div>';
      return;
    }

    var width = 760;
    var height = 300;
    var left = 42;
    var right = 14;
    var top = 12;
    var bottom = 42;
    var plotW = width - left - right;
    var plotH = height - top - bottom;

    var maxValue = 0;
    points.forEach(function(item) {
      if (Number.isFinite(item.wind)) maxValue = Math.max(maxValue, item.wind);
      if (Number.isFinite(item.gust)) maxValue = Math.max(maxValue, item.gust);
    });
    var yMax = Math.max(5, Math.ceil(maxValue / 5) * 5);

    function xFor(index) {
      if (points.length === 1) return left + plotW / 2;
      return left + index * plotW / (points.length - 1);
    }

    function yFor(value) {
      return top + (yMax - value) / yMax * plotH;
    }

    function pathFor(key) {
      var path = "";
      var drawing = false;
      points.forEach(function(item, index) {
        var value = item[key];
        if (!Number.isFinite(value)) {
          drawing = false;
          return;
        }
        path += (drawing ? " L " : " M ") +
          xFor(index).toFixed(1) + " " + yFor(value).toFixed(1);
        drawing = true;
      });
      return path.trim();
    }

    var grid = "";
    for (var tick = 0; tick <= 4; tick++) {
      var value = yMax * tick / 4;
      var y = yFor(value);
      grid += '<line class="wind-chart-grid" x1="' + left + '" y1="' + y.toFixed(1) +
        '" x2="' + (width-right) + '" y2="' + y.toFixed(1) + '"></line>';
      grid += '<text class="wind-chart-label" x="' + (left-7) + '" y="' + (y+4).toFixed(1) +
        '" text-anchor="end">' + value.toFixed(value % 1 ? 1 : 0) + '</text>';
    }

    function chooseLabelIndexes(count) {
      if (count <= 1) return [0];
      var desired = Math.min(6, Math.max(4, Math.round(count / 8)));
      desired = Math.min(desired, count);
      var result = [];
      for (var i = 0; i < desired; i++) {
        var idx = Math.round(i * (count - 1) / (desired - 1));
        if (result.indexOf(idx) === -1) result.push(idx);
      }
      if (result[result.length - 1] !== count - 1) result.push(count - 1);
      return result;
    }

    var xLabels = "";
    chooseLabelIndexes(points.length).forEach(function(index) {
      xLabels += '<text class="wind-chart-label" x="' + xFor(index).toFixed(1) +
        '" y="' + (height-10) + '" text-anchor="middle">' +
        escapeHTML(points[index].time) + '</text>';
    });

    chart.innerHTML =
      '<div id="wind-chart-readout" class="wind-chart-readout" aria-live="polite"></div>' +
      '<svg viewBox="0 0 ' + width + ' ' + height +
      '" role="img" aria-label="Recent sustained wind and gust history. Drag or tap to inspect observations." tabindex="0">' +
      grid +
      '<line class="wind-chart-axis" x1="' + left + '" y1="' + top +
      '" x2="' + left + '" y2="' + (height-bottom) + '"></line>' +
      '<line class="wind-chart-axis" x1="' + left + '" y1="' + (height-bottom) +
      '" x2="' + (width-right) + '" y2="' + (height-bottom) + '"></line>' +
      '<text class="wind-chart-label" x="10" y="' + (top+8) + '">' + chartUnit + '</text>' +
      '<path class="wind-chart-wind" d="' + pathFor("wind") + '"></path>' +
      '<path class="wind-chart-gust" d="' + pathFor("gust") + '"></path>' +
      xLabels +
      '<g id="wind-chart-inspector" hidden>' +
      '<line class="wind-chart-cursor" x1="0" y1="' + top + '" x2="0" y2="' + (height-bottom) + '"></line>' +
      '<circle class="wind-chart-cursor-dot wind" cx="0" cy="0" r="4" hidden></circle>' +
      '<circle class="wind-chart-cursor-dot gust" cx="0" cy="0" r="4" hidden></circle>' +
      '</g>' +
      '<rect class="wind-chart-hit" x="' + left + '" y="' + top + '" width="' + plotW +
      '" height="' + plotH + '" aria-hidden="true"></rect>' +
      '</svg>';

    var svg = chart.querySelector("svg");
    var hit = chart.querySelector(".wind-chart-hit");
    var inspector = document.getElementById("wind-chart-inspector");
    var cursorLine = inspector ? inspector.querySelector(".wind-chart-cursor") : null;
    var windDot = inspector ? inspector.querySelector(".wind-chart-cursor-dot.wind") : null;
    var gustDot = inspector ? inspector.querySelector(".wind-chart-cursor-dot.gust") : null;
    var readout = document.getElementById("wind-chart-readout");
    var activeIndex = -1;
    var dragging = false;

    function formatObservation(item) {
      var parts = [item.time];
      if (Number.isFinite(item.wind)) parts.push("Wind " + item.wind.toFixed(1) + " " + chartUnit);
      if (Number.isFinite(item.gust)) parts.push("Gust " + item.gust.toFixed(1) + " " + chartUnit);
      return parts.join(" · ");
    }

    function showIndex(index) {
      index = Math.max(0, Math.min(points.length - 1, index));
      activeIndex = index;
      var item = points[index];
      var x = xFor(index);

      if (inspector) inspector.hidden = false;
      if (cursorLine) {
        cursorLine.setAttribute("x1", x.toFixed(1));
        cursorLine.setAttribute("x2", x.toFixed(1));
      }

      if (windDot) {
        if (Number.isFinite(item.wind)) {
          windDot.hidden = false;
          windDot.setAttribute("cx", x.toFixed(1));
          windDot.setAttribute("cy", yFor(item.wind).toFixed(1));
        } else {
          windDot.hidden = true;
        }
      }

      if (gustDot) {
        if (Number.isFinite(item.gust)) {
          gustDot.hidden = false;
          gustDot.setAttribute("cx", x.toFixed(1));
          gustDot.setAttribute("cy", yFor(item.gust).toFixed(1));
        } else {
          gustDot.hidden = true;
        }
      }

      if (readout) readout.textContent = formatObservation(item);
    }

    function indexForClientX(clientX) {
      var rect = svg.getBoundingClientRect();
      if (!rect.width) return 0;
      var svgX = (clientX - rect.left) * width / rect.width;
      var ratio = (svgX - left) / plotW;
      ratio = Math.max(0, Math.min(1, ratio));
      return Math.round(ratio * (points.length - 1));
    }

    if (hit) {
      hit.addEventListener("pointerdown", function(event) {
        dragging = true;
        if (hit.setPointerCapture) hit.setPointerCapture(event.pointerId);
        showIndex(indexForClientX(event.clientX));
        event.preventDefault();
      });

      hit.addEventListener("pointermove", function(event) {
        if (!dragging) return;
        showIndex(indexForClientX(event.clientX));
        event.preventDefault();
      });

      hit.addEventListener("pointerup", function(event) {
        if (!dragging) return;
        dragging = false;
        showIndex(indexForClientX(event.clientX));
        if (hit.releasePointerCapture) {
          try { hit.releasePointerCapture(event.pointerId); } catch (_) {}
        }
        event.preventDefault();
      });

      hit.addEventListener("pointercancel", function() {
        dragging = false;
      });

      hit.addEventListener("click", function(event) {
        showIndex(indexForClientX(event.clientX));
      });
    }

    if (svg) {
      svg.addEventListener("keydown", function(event) {
        if (event.key !== "ArrowLeft" && event.key !== "ArrowRight" &&
            event.key !== "Home" && event.key !== "End") return;

        if (activeIndex < 0) activeIndex = points.length - 1;
        if (event.key === "ArrowLeft") showIndex(activeIndex - 1);
        else if (event.key === "ArrowRight") showIndex(activeIndex + 1);
        else if (event.key === "Home") showIndex(0);
        else if (event.key === "End") showIndex(points.length - 1);
        event.preventDefault();
      });
    }
  }
  function readingsFromTable() {
    return Array.prototype.map.call(tableBody.querySelectorAll("tr"), function(row) {
      var cells = row.querySelectorAll("td");
      return {
        time: cells[0] ? cells[0].textContent.trim() : "",
        direction: cells[1] ? cells[1].textContent.trim() : "",
        wind: cells[2] ? cells[2].textContent.trim() : "",
        gust: cells[3] ? cells[3].textContent.trim() : "",
        age: cells[4] ? cells[4].textContent.trim() : ""
      };
    });
  }

  renderWindChart(readingsFromTable());

  hoursSelect.addEventListener("change", function() {
    var hours = hoursSelect.value;
    var station = hoursSelect.getAttribute("data-station") || "";
    if (!station) return;

    hoursSelect.disabled = true;
    if (status) status.textContent = "Updating…";

    var target = new URL("/wind-readings", window.location.origin);
    target.searchParams.set("station", station);
    target.searchParams.set("wind_hours", hours);
    target.searchParams.set(
      "wind_unit",
      unitSelect && unitSelect.value === "mph" ? "mph" : "kts"
    );

    fetch(target.toString(), {headers:{"Accept":"application/json"}})
      .then(function(response) {
        if (!response.ok) throw new Error("HTTP " + response.status);
        return response.json();
      })
      .then(function(data) {
        var readings = Array.isArray(data.readings) ? data.readings : [];
        renderReadings(readings);
        renderWindChart(readings);

        var summary = document.querySelector(".wind-card .wind-summary");
        if (summary && typeof data.summary === "string") summary.textContent = data.summary;

        hoursSelect.value = String(data.hours || hours);
        updateHoursInLinks(hoursSelect.value);
        if (status) status.textContent = "";
      })
      .catch(function(err) {
        if (status) status.textContent = "Update failed";
        console.error("Wind history update failed:", err);
      })
      .finally(function() {
        hoursSelect.disabled = false;
      });
  });
})();

(function(){
  var replaceIDs = [
    "bottom-line-card",
    "tide-context-card",
    "current-chart-card",
    "full-report-card"
  ];
  var requestSerial = 0;

  function setRefreshing(on) {
    replaceIDs.forEach(function(id) {
      var el = document.getElementById(id);
      if (el) el.classList.toggle("current-refreshing", on);
    });
  }

  function currentURLForDate(dateValue) {
    var target = new URL(window.location.href);
    target.pathname = "/report";
    target.searchParams.set("format", "html");
    if (dateValue) {
      target.searchParams.set("current_date", dateValue);
    } else {
      target.searchParams.delete("current_date");
    }
    return target;
  }

  function currentURLForPlanning() {
    var target = new URL(window.location.href);
    target.pathname = "/report";
    target.searchParams.set("format", "html");

    var start = document.getElementById("planning-start");
    var end = document.getElementById("planning-end");
    var cautionEbb = document.getElementById("planning-caution-ebb");
    var cautionFlood = document.getElementById("planning-caution-flood");
    var maxEbb = document.getElementById("planning-max-ebb");
    var maxFlood = document.getElementById("planning-max-flood");
    var buffer = document.getElementById("planning-buffer");
    var currentDistanceWarning = document.getElementById("planning-current-distance-warning");

    var startValue = start ? start.value : "12:00";
    var endValue = end ? end.value : "17:00";
    var cautionEbbValue = cautionEbb ? Number(cautionEbb.value) : 2.0;
    var cautionFloodValue = cautionFlood ? Number(cautionFlood.value) : 2.0;
    var maxValue = maxEbb ? Number(maxEbb.value) : 3.0;
    var maxFloodValue = maxFlood ? Number(maxFlood.value) : 3.0;
    var bufferValue = buffer ? Number(buffer.value) : 60;
    var currentDistanceWarningValue = currentDistanceWarning ? Number(currentDistanceWarning.value) : 15.0;

    if (!/^\d{2}:\d{2}$/.test(startValue)) startValue = "12:00";
    if (!/^\d{2}:\d{2}$/.test(endValue)) endValue = "17:00";
    if (!Number.isFinite(cautionEbbValue) || cautionEbbValue <= 0) cautionEbbValue = 2.0;
    if (!Number.isFinite(cautionFloodValue) || cautionFloodValue <= 0) cautionFloodValue = 2.0;
    if (!Number.isFinite(maxValue) || maxValue <= cautionEbbValue) maxValue = 3.0;
    if (!Number.isFinite(maxFloodValue) || maxFloodValue <= cautionFloodValue) maxFloodValue = 3.0;
    if (!Number.isFinite(bufferValue) || bufferValue < 0) bufferValue = 60;
    bufferValue = Math.max(0, Math.min(360, Math.round(bufferValue)));
    if (!Number.isFinite(currentDistanceWarningValue) ||
        currentDistanceWarningValue <= 0 ||
        currentDistanceWarningValue > 30.0) {
      currentDistanceWarningValue = 15.0;
    }
    currentDistanceWarningValue = Math.round(currentDistanceWarningValue * 10) / 10;

    if (startValue === "12:00") target.searchParams.delete("planning_start");
    else target.searchParams.set("planning_start", startValue);

    if (endValue === "17:00") target.searchParams.delete("planning_end");
    else target.searchParams.set("planning_end", endValue);

    if (Math.abs(cautionEbbValue - 2.0) < 0.001) target.searchParams.delete("caution_ebb");
    else target.searchParams.set("caution_ebb", cautionEbbValue.toFixed(1));

    if (Math.abs(cautionFloodValue - 2.0) < 0.001) target.searchParams.delete("caution_flood");
    else target.searchParams.set("caution_flood", cautionFloodValue.toFixed(1));

    if (Math.abs(maxValue - 3.0) < 0.001) target.searchParams.delete("max_ebb");
    else target.searchParams.set("max_ebb", maxValue.toFixed(1));

    if (Math.abs(maxFloodValue - 3.0) < 0.001) target.searchParams.delete("max_flood");
    else target.searchParams.set("max_flood", maxFloodValue.toFixed(1));

    if (bufferValue === 60) target.searchParams.delete("planning_buffer");
    else target.searchParams.set("planning_buffer", String(bufferValue));

    if (Math.abs(currentDistanceWarningValue - 15.0) < 0.001) {
      target.searchParams.delete("current_distance_warning");
    } else {
      target.searchParams.set("current_distance_warning", currentDistanceWarningValue.toFixed(1));
    }

    return target;
  }

  function currentURLForDays(daysValue) {
    var target = new URL(window.location.href);
    target.pathname = "/report";
    target.searchParams.set("format", "html");
    if (daysValue === "3" || daysValue === "7") {
      target.searchParams.set("current_days", daysValue);
    } else {
      target.searchParams.delete("current_days");
    }
    return target;
  }

  async function loadCurrentURL(target, pushHistory) {
    var serial = ++requestSerial;
    setRefreshing(true);
    try {
      var response = await fetch(target.pathname + target.search, {
        headers: {"X-Requested-With": "current-date-controls"}
      });
      if (!response.ok) throw new Error("HTTP " + response.status);
      var html = await response.text();
      if (serial !== requestSerial) return;
      var doc = new DOMParser().parseFromString(html, "text/html");
      replaceIDs.forEach(function(id) {
        var oldEl = document.getElementById(id);
        var newEl = doc.getElementById(id);
        if (oldEl && newEl) oldEl.replaceWith(newEl);
      });
      if (pushHistory) history.pushState({}, "", target.pathname + target.search);
    } catch (err) {
      // Preserve the reliable server-rendered fallback if partial refresh fails.
      window.location.assign(target.pathname + target.search);
    } finally {
      if (serial === requestSerial) setRefreshing(false);
    }
  }

  document.addEventListener("click", function(e) {
    var link = e.target.closest("a.current-date-nav");
    if (!link) return;
    e.preventDefault();
    loadCurrentURL(new URL(link.href, window.location.href), true);
  });

  document.addEventListener("change", function(e) {
    if (e.target.id !== "current-date-picker") return;
    var value = e.target.value;
    if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return;
    e.target.blur();
    loadCurrentURL(currentURLForDate(value), true);
  });

  document.addEventListener("change", function(e) {
    if (e.target.id !== "current-days-picker") return;
    loadCurrentURL(currentURLForDays(e.target.value), true);
  });

  document.addEventListener("change", function(e) {
    if (e.target.id !== "show-tide-range-overlay") return;
    var card = e.target.closest("#current-chart-card");
    if (!card) return;
    card.querySelectorAll(".tide-range-layer").forEach(function(layer) {
      layer.style.display = e.target.checked ? "" : "none";
    });
  });

  document.addEventListener("change", function(e) {
    if (
      e.target.id !== "planning-start" &&
      e.target.id !== "planning-end" &&
      e.target.id !== "planning-caution-ebb" &&
      e.target.id !== "planning-caution-flood" &&
      e.target.id !== "planning-max-ebb" &&
      e.target.id !== "planning-max-flood" &&
      e.target.id !== "planning-buffer" &&
      e.target.id !== "planning-current-distance-warning"
    ) return;
    loadCurrentURL(currentURLForPlanning(), true);
  });

  document.addEventListener("click", function(e) {
  });


  window.addEventListener("popstate", function() {
    loadCurrentURL(new URL(window.location.href), false);
  });
})();

(function(){
  var link = document.getElementById("planning-page-link");
  var overlay = document.getElementById("planning-loading-overlay");
  if (!link || !overlay) return;

  function hidePlanningLoader() {
    overlay.classList.remove("active");
    overlay.setAttribute("aria-hidden", "true");
    link.removeAttribute("aria-disabled");
  }

  link.addEventListener("click", function(event) {
    if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    overlay.classList.add("active");
    overlay.setAttribute("aria-hidden", "false");
    link.setAttribute("aria-disabled", "true");
  });

  window.addEventListener("pageshow", hidePlanningLoader);
})();
</script>
</body></html>`))

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
