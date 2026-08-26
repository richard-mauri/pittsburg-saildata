package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	defaultWindStation    = "PSBC1"
	windDistanceWarningNM = 10.0
	defaultStartHour      = -1
	defaultEndHour        = -1
	timeZoneName          = "America/Los_Angeles"
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
			report.Current = current
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
			report.Current = current
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
		return 1.5
	}
	var parsed float64
	if _, err := fmt.Sscanf(value, "%f", &parsed); err != nil || parsed <= 0 || parsed > 10 {
		return 1.5
	}
	// Keep UI/classification aligned with the one-decimal display.
	return math.Round(parsed*10) / 10
}

func parsePlanningMaxFlood(q url.Values) float64 {
	value := strings.TrimSpace(q.Get("max_flood"))
	if value == "" {
		return 1.5
	}
	var parsed float64
	if _, err := fmt.Sscanf(value, "%f", &parsed); err != nil || parsed <= 0 || parsed > 10 {
		return 1.5
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
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := welcomeHTMLTemplate.Execute(w, nil); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
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
			Lat             float64 `json:"lat"`
			Lon             float64 `json:"lon"`
			URL             string  `json:"url"`
			CurrentStation  string  `json:"current_station,omitempty"`
			CurrentName     string  `json:"current_name,omitempty"`
			CurrentDistance string  `json:"current_distance,omitempty"`
			CurrentLat      float64 `json:"current_lat,omitempty"`
			CurrentLon      float64 `json:"current_lon,omitempty"`
		}
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
			if currentPreview, err := previewCurrentStationForPoint(c.Station.Lat, c.Station.Lon); err == nil && currentPreview != nil {
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
				report.Current = current
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
				report.Current = current
			}
		}

		report.WindSelection = windSelection
		report.DebugWindSelection = queryBool(r, "debug_wind")
		report.RequestQuery = cloneQuery(r.URL.Query())

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
	return best, nil
}

type htmlWindCandidate struct {
	Rank            int
	Station         string
	Name            string
	Distance        string
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
	HasCurrent      bool
}

type htmlCurrentEvent struct{ Time, Label, Speed, Direction, Class string }

type currentPlanningHint struct {
	Date, Status, Class, Detail string
}

type htmlReportData struct {
	Title, Station, ReportTime                       string
	Historical                                       bool
	RequestedTime                                    string
	WindDirection, WindSpeed, WindGust, WindObserved string
	WindSummary                                      string
	WindSelection                                    string
	WindDistanceWarning                              string
	WindCandidates                                   []htmlWindCandidate
	DebugWind                                        bool
	WindError                                        string
	UseNearestURL                                    string
	MapCenterLat, MapCenterLon                       float64
	MapRequestLat, MapRequestLon                     float64
	MapWindLat, MapWindLon                           float64
	MapCurrentLat, MapCurrentLon                     float64
	MapHasRequest, MapHasWind, MapHasCurrent         bool
	MapWindStation, MapCurrentStation                string
	CurrentStation, CurrentMeta                      string
	CurrentWindow, CurrentWindowMode                 string
	CurrentDateLabel, CurrentDateISO                 string
	CurrentDays                                      int
	CurrentRangeLabel                                string
	FullDetailsURL                                   string
	WindStationsURL                                  string
	CurrentPrevURL, CurrentTodayURL, CurrentNextURL  string
	CurrentIsToday                                   bool
	CurrentOutlook                                   []string
	CurrentEvents                                    []htmlCurrentEvent
	CurrentPlanningHints                             []currentPlanningHint
	PlanningStart, PlanningEnd                       string
	PlanningMaxEbb, PlanningMaxFlood, PlanningBuffer string
	CurrentChart                                     template.HTML
	BottomLine                                       []string
	FullText                                         string
}

func writeHTMLReport(w http.ResponseWriter, report *SailingReport, loc *time.Location) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	data := makeHTMLReportData(report, loc)
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
func makeHTMLReportData(report *SailingReport, loc *time.Location) htmlReportData {
	d := htmlReportData{Station: report.Station, Title: report.Station}
	d.DebugWind = report.DebugWindSelection
	d.WindError = strings.TrimSpace(report.WindError)
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
	if !report.ReportTime.IsZero() {
		d.ReportTime = report.ReportTime.In(loc).Format("Mon Jan 2, 2006 · 3:04 PM MST")
	}
	if report.Historical != nil {
		d.Historical = true
		d.RequestedTime = report.Historical.Requested.In(loc).Format("Mon Jan 2, 2006 · 3:04 PM")
	}
	if report.Current != nil && report.Current.WindReference != nil && strings.TrimSpace(report.Current.WindReference.Name) != "" {
		d.Title = strings.TrimSpace(report.Current.WindReference.Name)
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
			d.WindSpeed = fmt.Sprintf("%.0f kt", latest.WindKT)
		} else {
			d.WindSpeed = "—"
		}

		if latest.GustKT > 0 {
			d.WindGust = fmt.Sprintf("%.0f kt", latest.GustKT)
		} else {
			d.WindGust = "—"
		}

		d.WindObserved = latest.Time.In(loc).Format("3:04 PM")
	} else {
		d.WindDirection = "—"
		d.WindSpeed = "—"
		d.WindGust = "—"
		d.WindObserved = "Wind observation unavailable"
	}
	// Generate the same wind summary used by the text report when wind
	// data are available. Error pages use the explicit WindError instead.
	if d.WindError == "" {
		var windText strings.Builder
		if report.Historical != nil {
			if report.Historical.Closest != nil {
				printWindObservation(
					&windText,
					report.Historical.Closest,
					loc,
					report.Historical.Requested,
				)
			}
		} else {
			writeWindSummaryText(
				&windText,
				report,
				loc,
			)
		}
		d.WindSummary = strings.TrimSpace(windText.String())
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

	if report.WindSelection != nil {
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
			if currentPreview, err := previewCurrentStationForPoint(candidate.Lat, candidate.Lon); err == nil && currentPreview != nil {
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

		if report.Current.CurrentStation != nil {
			s := report.Current.CurrentStation
			d.CurrentStation = s.Name
			d.CurrentMeta = fmt.Sprintf("%s · bin %s · %s ft depth · %.1f nmi away", s.ID, report.Current.Bin, report.Current.Depth, s.DistanceNM)
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
		planningMaxEbb := parsePlanningMaxEbb(report.RequestQuery)
		planningMaxFlood := parsePlanningMaxFlood(report.RequestQuery)
		planningBuffer := parsePlanningBuffer(report.RequestQuery)
		d.PlanningStart = planningStart
		d.PlanningEnd = planningEnd
		d.PlanningMaxEbb = fmt.Sprintf("%.1f", planningMaxEbb)
		d.PlanningMaxFlood = fmt.Sprintf("%.1f", planningMaxFlood)
		d.PlanningBuffer = fmt.Sprintf("%d", planningBuffer)

		d.CurrentPlanningHints = buildCurrentPlanningHints(
			report.Current,
			currentDay,
			d.CurrentDays,
			loc,
			planningMaxEbb,
			planningMaxFlood,
			planningStartMinutes,
			planningEndMinutes,
			planningBuffer,
		)
		d.CurrentChart = buildCurrentChartSVG(
			report.Current,
			report.ReportTime,
			loc,
			d.CurrentDays,
			planningStartMinutes,
			planningEndMinutes,
		)
	}
	if d.WindError != "" {
		d.BottomLine = append(
			d.BottomLine,
			"Wind station selection is unavailable for the requested location.",
			"Try nearby coordinates, an explicit NDBC station ID, or enable debug_wind=1 to inspect nearby candidates.",
		)
		d.FullText = fmt.Sprintf(
			"WIND STATION SELECTION UNAVAILABLE\n--------------------------------\n%s",
			d.WindError,
		)
	} else {
		var b strings.Builder
		writeBottomLineText(&b, report)
		for _, line := range strings.Split(strings.TrimSpace(b.String()), "\n") {
			line = strings.TrimSpace(line)
			if line != "" && line != "BOTTOM LINE" && !strings.HasPrefix(line, "---") {
				d.BottomLine = append(d.BottomLine, line)
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

func buildCurrentPlanningHints(
	report *CurrentReport,
	startDay time.Time,
	days int,
	loc *time.Location,
	maxEbbKT float64,
	maxFloodKT float64,
	planningStartMinutes int,
	planningEndMinutes int,
	planningBufferMinutes int,
) []currentPlanningHint {
	if report == nil || report.CurrentStation == nil || days < 1 {
		return nil
	}
	if maxEbbKT <= 0 {
		maxEbbKT = 1.5
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

		h := currentPlanningHint{Date: day.Format("Mon Jan 2")}
		var redReasons, cautionReasons []string
		if windowMaxEbb >= maxEbbKT {
			redReasons = append(redReasons, fmt.Sprintf("ebb %.1f kt at %s", windowMaxEbb, windowMaxEbbTime.Format("3:04 PM")))
		}
		if windowMaxFlood >= maxFloodKT {
			redReasons = append(redReasons, fmt.Sprintf("flood %.1f kt at %s", windowMaxFlood, windowMaxFloodTime.Format("3:04 PM")))
		}
		if bufferMaxEbb >= maxEbbKT {
			cautionReasons = append(cautionReasons, fmt.Sprintf("ebb %.1f kt at %s", bufferMaxEbb, bufferMaxEbbTime.Format("3:04 PM")))
		}
		if bufferMaxFlood >= maxFloodKT {
			cautionReasons = append(cautionReasons, fmt.Sprintf("flood %.1f kt at %s", bufferMaxFlood, bufferMaxFloodTime.Format("3:04 PM")))
		}

		switch {
		case len(redReasons) > 0:
			h.Status, h.Class = "Red flag", "redflag"
			h.Detail = fmt.Sprintf("%s during %s.", strings.Join(redReasons, "; "), windowLabel)
			if len(cautionReasons) > 0 {
				h.Detail += fmt.Sprintf(" Also near the window: %s.", strings.Join(cautionReasons, "; "))
			}
		case len(cautionReasons) > 0:
			h.Status, h.Class = "Caution", "caution"
			h.Detail = fmt.Sprintf("%s within the %d-minute buffer around %s.",
				strings.Join(cautionReasons, "; "), planningBufferMinutes, windowLabel)
		default:
			h.Status, h.Class = "Preferred", "preferred"
			h.Detail = fmt.Sprintf("During %s, ebb stays below %.1f kt and flood stays below %.1f kt.",
				windowLabel, maxEbbKT, maxFloodKT)
		}

		hints = append(hints, h)
	}

	return hints
}

func buildCurrentChartSVG(
	report *CurrentReport,
	reportTime time.Time,
	loc *time.Location,
	days int,
	planningStartMinutes int,
	planningEndMinutes int,
) template.HTML {
	if report == nil || len(report.Series) < 2 {
		return ""
	}
	if days != 3 && days != 7 {
		days = 1
	}

	series := append([]CurrentSample(nil), report.Series...)
	if days > 1 && report.CurrentStation != nil {
		for offset := 1; offset < days; offset++ {
			day := report.Start.In(loc).AddDate(0, 0, offset)
			dateString := day.Format("20060102")
			if densePredictions, _, err := fetchCurrentDensePredictions(
				report.CurrentStation.ID,
				report.CurrentStation.CurrBin,
				dateString,
			); err == nil {
				if samples, parseErr := currentSamplesFromPredictions(
					densePredictions,
					loc,
				); parseErr == nil {
					series = append(series, samples...)
				}
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
	if days > 1 && report.CurrentStation != nil {
		beginDate := dayStartForCurrentChart(report, loc).Format("20060102")
		endDate := dayStartForCurrentChart(report, loc).AddDate(0, 0, days-1).Format("20060102")
		if predictions, _, err := fetchCurrentPredictionsRange(
			report.CurrentStation.ID,
			report.CurrentStation.CurrBin,
			beginDate,
			endDate,
		); err == nil {
			if timed, parseErr := parseCurrentPredictions(predictions, loc); parseErr == nil {
				chartEvents = currentEventsFromTimed(timed)
			}
		}
	}

	const (
		width  = 860.0
		height = 330.0
		left   = 54.0
		right  = 18.0
		top    = 18.0
		bottom = 42.0
	)

	plotW := width - left - right
	plotH := height - top - bottom

	dayStart := dayStartForCurrentChart(report, loc)
	dayEnd := dayStart.AddDate(0, 0, days)

	maxAbs := 0.0
	for _, sample := range series {
		if v := math.Abs(sample.VelocityKT); v > maxAbs {
			maxAbs = v
		}
	}
	if maxAbs < 0.5 {
		maxAbs = 0.5
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

	// "Now" marker appears when report time falls inside the plotted range.
	nowLocal := reportTime.In(loc)
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
<meta name="description" content="Mauri's Wind & Current Conditions — wind observations and predicted currents for supported waters, presented for sailors, paddlers, and other people on the water.">
<meta property="og:title" content="Mauri's Wind & Current Conditions">
<meta property="og:description" content="Pick where you're going. See nearby wind observations and predicted currents.">
<meta property="og:type" content="website">
<meta property="og:url" content="https://pittsburg-saildata.onrender.com/welcome">
<meta property="og:image" content="https://pittsburg-saildata.onrender.com/assets/hero.jpg">
<meta property="og:image:alt" content="Sailing on the supported coastal and inland waters">
<meta name="twitter:card" content="summary_large_image">
<meta name="twitter:title" content="Mauri's Wind & Current Conditions">
<meta name="twitter:description" content="Pick where you're going. See nearby wind observations and predicted currents.">
<meta name="twitter:image" content="https://pittsburg-saildata.onrender.com/assets/hero.jpg">
<title>Mauri's Wind & Current Conditions — Welcome</title>
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
.footer{margin-top:18px;text-align:center;color:var(--muted);font-size:.88rem}
@media(max-width:680px){.grid{grid-template-columns:1fr}.github-actions{grid-template-columns:1fr}.hero{min-height:340px;padding:26px 22px}}
</style>
</head>
<body>
<main class="shell">
<section class="hero">
<div class="eyebrow">Mauri's Wind & Current Conditions</div>
<h1>Pick where you're going. See the wind & current.</h1>
<p>A free wind and current conditions tool for sailors, paddlers, and other people on the water, using nearby observations and predicted currents where supported.</p>
<div class="cta-row">
<a class="cta primary" href="/report?format=html">See Wind & Current Conditions</a>
<a class="cta secondary" href="https://github.com/richard-mauri/pittsburg-saildata">View on GitHub</a>
</div>
</section>

<div class="grid">
<section class="card quick">
<h2>The 30-second version</h2>
<ol>
<li>Open the Wind & Current Conditions.</li>
<li>Click where you plan to sail on the map.</li>
<li>Click <strong>Show Conditions</strong>.</li>
<li>Read the <strong>Bottom Line</strong> first.</li>
<li>Scroll down for wind-station choices, current timing, and the current graph.</li>
</ol>
<p class="note">You do not need to know buoy IDs, NOAA station numbers, or coordinates.</p>
</section>

<section class="card">
<h2>What it combines</h2>
<p><strong>Wind:</strong> recent NOAA/NDBC observations from a nearby station with usable wind data.</p>
<p><strong>Current:</strong> NOAA CO-OPS current predictions including ebb, flood, slack, maximum speeds, and a smooth current curve.</p>
<p><strong>Context:</strong> nearby station alternatives and relative current comparisons instead of relying only on vague words like "strong."</p>
</section>

<section class="card full qa">
<h2>Questions people on the water will probably ask</h2>
<details open><summary>Is this tide data or current data?</summary><p><strong>Current data.</strong> The report is about the predicted speed and direction of moving water — ebb, flood, maximum current, and slack. Tide height and current are related, but they are not the same thing.</p></details>
<details><summary>How does it choose the wind station?</summary><p>Clicking the map gives the service a latitude/longitude. It looks at nearby NOAA/NDBC meteorological stations, checks them for usable wind observations, and chooses the nearest usable one.</p></details>
<details><summary>Can I choose another wind station?</summary><p>Yes. The Nearby Wind Stations section is clickable. <strong>AUTO</strong> is the service's preferred station; <strong>SELECTED</strong> is the station currently driving the report.</p></details>
<details><summary>What does the current graph show?</summary><p>Predicted current speed through the day. Flood is above zero, ebb is below zero, and crossings indicate slack water.</p></details>
<details><summary>Why compare one ebb or flood with another?</summary><p>A raw knot value can be misleading without context. The report can show that an afternoon ebb, for example, is only about half as strong as the other ebb that day, and can compare it with recent cycles.</p></details>
<details><summary>Is this for navigation or safety decisions?</summary><p>No. It is a conditions-planning and exploration tool. Observations can be delayed or missing, station exposure differs, and current predictions are predictions. Use normal marine forecasts, charts, local knowledge, and seamanship.</p></details>
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
<p>The browser submits the map point as decimal latitude/longitude. The Go service caches active NDBC station metadata, computes geographic distance, probes nearby candidates concurrently for usable wind, then combines that with NOAA CO-OPS current predictions. The same service also exposes text and JSON output for scripts and integrations.</p>
</section>
</div>

<div class="footer">Mauri's Wind & Current Conditions · Conditions-planning utility, not a navigation system.</div>
</main>
</body>
</html>`))

var sailingStationsHTMLTemplate = template.Must(template.New("stations").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Nearby Wind Stations — Mauri's Wind & Current Conditions</title>
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
.actions{display:flex;gap:10px;flex-wrap:wrap;margin-top:14px}.button{display:inline-block;text-decoration:none;border:1px solid var(--line);border-radius:999px;padding:8px 12px;color:var(--blue);font-weight:850;background:#fff}
@media(max-width:650px){.map{height:360px}th,td{padding:9px 6px}}
</style></head><body><main>
<a class="back" href="javascript:history.back()">← Back to conditions</a>
<h1>Nearby Wind Stations</h1>
<p class="intro">These are the candidate observation stations for the selected location. Distances are measured from that selected point. Use the map or table to choose the station you think best represents the water you care about.</p>
<div class="card"><div id="station-map" class="map" aria-label="Nearby wind station candidates"></div></div>
<div class="card">
{{if .UseNearestURL}}<div class="actions"><a class="button" href="{{.UseNearestURL}}">Use nearest usable station</a></div>{{end}}
<div class="scroll"><table><thead><tr><th>#</th><th>Station</th><th>Name</th><th>State</th><th class="num">From Selected Location</th>{{if .DebugWind}}<th>Status</th><th>Reason</th>{{end}}</tr></thead><tbody>
{{range .WindCandidates}}<tr><td>{{.Rank}}</td><td><a class="station" href="{{.URL}}">{{.Station}}</a></td><td><a class="station" href="{{.URL}}">{{.Name}}</a></td><td>{{if .IsAuto}}<span class="badge auto">AUTO</span>{{end}}{{if .IsSelected}}<span class="badge selected">SELECTED</span>{{end}}</td><td class="num">{{.Distance}}</td>{{if $.DebugWind}}<td>{{.Status}}</td><td>{{.Reason}}</td>{{end}}</tr>{{else}}<tr><td colspan="5">No nearby station candidates are available.</td></tr>{{end}}
</tbody></table></div></div>
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
  var mapLayerParam=new URL(window.location.href).searchParams.get("map_layer");
  (mapLayerParam==="nautical"?nauticalLayer:streetLayer).addTo(map);
  L.control.layers({"Map":streetLayer,"Nautical Chart":nauticalLayer},null,{collapsed:true,position:"topright"}).addTo(map);
  map.on("baselayerchange",function(e){
    var target=new URL(window.location.href);
    if(e.layer===nauticalLayer)target.searchParams.set("map_layer","nautical");
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
<title>Full report details — Mauri's Wind & Current Conditions</title>
<style>:root{--navy:#082b45;--blue:#126b91;--ink:#153242;--muted:#607886;--paper:#f5fafc;--card:#fff;--line:#d8e7ed}*{box-sizing:border-box}body{margin:0;background:var(--paper);color:var(--ink);font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}main{max-width:1000px;margin:0 auto;padding:24px 16px 48px}a{color:var(--blue)}.back{display:inline-block;margin-bottom:18px;text-decoration:none;font-weight:800}.card{background:var(--card);border:1px solid var(--line);border-radius:16px;padding:20px}h1{margin:0 0 6px;color:var(--navy);font-size:clamp(1.5rem,4vw,2.2rem)}.meta{color:var(--muted);margin-bottom:18px}pre{white-space:pre-wrap;overflow-wrap:anywhere;margin:0;font:14px/1.55 ui-monospace,SFMono-Regular,Menlo,Monaco,Consolas,monospace}</style>
</head><body><main><a class="back" href="javascript:history.back()">← Back to conditions</a><div class="card"><h1>Full report details</h1><div class="meta">{{.Title}}{{if .ReportTime}} · {{.ReportTime}}{{end}}</div><pre>{{.FullText}}</pre></div></main></body></html>`))

var sailingHTMLTemplate = template.Must(template.New("sailing").Parse(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="description" content="Wind observations and predicted currents for supported waters, presented for people planning time on the water.">
<meta property="og:title" content="Mauri's Wind & Current Conditions">
<meta property="og:description" content="Wind observations and predicted currents for supported waters, presented for people planning time on the water.">
<meta property="og:type" content="website">
<meta property="og:url" content="https://pittsburg-saildata.onrender.com/">
<meta property="og:image" content="https://pittsburg-saildata.onrender.com/assets/hero.jpg">
<meta property="og:image:alt" content="Sailing on the supported coastal and inland waters">
<meta name="twitter:card" content="summary_large_image">
<meta name="twitter:title" content="Mauri's Wind & Current Conditions">
<meta name="twitter:description" content="Wind observations and predicted currents for supported waters, presented for people planning time on the water.">
<meta name="twitter:image" content="https://pittsburg-saildata.onrender.com/assets/hero.jpg">
<title>Mauri's Wind & Current Conditions — {{.Title}}</title>
<link rel="stylesheet" href="https://unpkg.com/leaflet@1.9.4/dist/leaflet.css" crossorigin="">
<script src="https://unpkg.com/leaflet@1.9.4/dist/leaflet.js" crossorigin=""></script>
<style>:root{--navy:#082b45;--blue:#126b91;--sea:#0b8793;--ink:#153242;--muted:#607886;--paper:#f5fafc;--card:#fff;--line:#d8e7ed;--flood:#087f8c;--ebb:#365f91;--slack:#756d64;--shadow:0 12px 34px rgba(8,43,69,.10)}*{box-sizing:border-box}body{margin:0;background:linear-gradient(180deg,#dff3f8,#f7fbfc 32rem);color:var(--ink);font-family:-apple-system,BlinkMacSystemFont,"Segoe UI","Avenir Next",Avenir,Helvetica,Arial,sans-serif;line-height:1.45}.shell{max-width:880px;margin:auto;padding:28px 18px 64px}.hero{color:#fff;padding:34px 30px 30px;border-radius:24px;min-height:360px;display:flex;flex-direction:column;justify-content:flex-end;background:
linear-gradient(180deg,rgba(4,24,38,.06) 12%,rgba(4,24,38,.24) 48%,rgba(4,24,38,.86) 100%),
url('/assets/hero.jpg') center 48%/cover no-repeat;box-shadow:var(--shadow);text-shadow:0 2px 12px rgba(0,0,0,.45)}.eyebrow{text-transform:uppercase;letter-spacing:.14em;font-weight:800;font-size:.76rem;opacity:.8}.photo-tag{margin-top:14px;font-size:.72rem;letter-spacing:.12em;text-transform:uppercase;opacity:.72}h1{font-size:clamp(1.8rem,6vw,3.2rem);line-height:1.05;margin:.4rem 0 .6rem;letter-spacing:-.035em}.sub{opacity:.82}.grid{display:grid;grid-template-columns:1fr 1fr;gap:18px;margin-top:18px}.card{background:var(--card);border:1px solid var(--line);border-radius:20px;padding:22px;box-shadow:var(--shadow)}.full{grid-column:1/-1}h2{font-size:.82rem;letter-spacing:.13em;text-transform:uppercase;color:var(--blue);margin:0 0 16px}.bottom{font-size:1.13rem}.metrics{display:grid;grid-template-columns:repeat(3,1fr);gap:10px}.metric{background:var(--paper);border-radius:15px;padding:14px}.label{font-size:.73rem;text-transform:uppercase;letter-spacing:.08em;color:var(--muted);font-weight:700}.value{font-size:1.55rem;font-weight:800;color:var(--navy)}.meta{color:var(--muted);font-size:.88rem;margin-top:12px}.station{font-weight:800;font-size:1.1rem;color:var(--navy)}.wind-distance-warning{margin-top:10px;padding:10px 12px;border:1px solid #e7c978;border-radius:12px;background:#fff8df;color:#654d08;font-size:.9rem}.wind-distance-warning strong{color:#4e3a00}.wind-summary{white-space:pre-line;margin-top:14px;padding:13px 14px;background:#eef7fa;border-left:4px solid var(--sea);border-radius:10px;color:var(--ink);font-size:.92rem}.event{display:grid;grid-template-columns:88px 12px 1fr;gap:12px;align-items:center;min-height:58px}.time{font-weight:800;color:var(--navy)}.dot{width:12px;height:12px;border-radius:50%;background:var(--slack);box-shadow:0 0 0 5px #edf3f5}.flood .dot{background:var(--flood)}.ebb .dot{background:var(--ebb)}.eventbody{border-left:2px solid var(--line);padding:8px 0 8px 18px}.eventlabel{font-weight:800}.eventdata{color:var(--muted);font-size:.9rem}.badge{display:inline-block;border-radius:999px;padding:5px 10px;background:#e9f6fb;color:var(--blue);font-size:.75rem;font-weight:800;margin-top:12px}.footer{text-align:center;color:var(--muted);font-size:.78rem;margin-top:22px}.full-report{margin:0;white-space:pre-wrap;overflow-wrap:anywhere;font-family:"SFMono-Regular",Consolas,"Liberation Mono",Menlo,monospace;font-size:.88rem;line-height:1.55;background:#071f31;color:#e7f4f8;border-radius:14px;padding:18px;overflow-x:auto}.card-action-row{margin-top:14px;padding-top:12px;border-top:1px solid var(--line)}#current-summary-card,.wind-card{min-width:0}.timeline-scope-note{margin:.15rem 0 1rem;color:var(--muted);font-size:.88rem}.details-link-card{display:flex;align-items:center;justify-content:space-between;gap:18px;flex-wrap:wrap}.details-link-card h2{margin-bottom:.2rem}.details-link{display:inline-block;text-decoration:none;border:1px solid var(--line);border-radius:999px;padding:9px 13px;background:#fff;color:var(--blue);font-weight:850;white-space:nowrap}.details-note{color:var(--muted);font-size:.88rem;margin:-4px 0 14px}.current-chart-header{display:flex;justify-content:space-between;align-items:flex-start;gap:14px;flex-wrap:wrap}.current-chart-header h2{margin-bottom:.15rem}.current-date-label{color:var(--muted);font-weight:750;font-size:.9rem}.current-range-toolbar{display:flex;gap:10px;align-items:flex-end;flex-wrap:wrap;margin:8px 0 10px;padding:12px 14px;border:1px solid var(--line);border-radius:14px;background:var(--paper)}.current-range-toolbar .current-date-nav{display:inline-flex;align-items:center;min-height:44px;text-decoration:none;border:1px solid var(--line);border-radius:999px;padding:8px 12px;background:#fff;color:var(--blue);font-size:.86rem;font-weight:850}.current-range-toolbar .current-date-nav.is-current{background:var(--navy);border-color:var(--navy);color:#fff}.current-control-label{display:flex;flex-direction:column;gap:4px;color:var(--muted);font-size:.75rem;font-weight:850}.current-control-label .current-date-picker{min-height:44px;font-size:1rem}@media(max-width:640px){.current-range-toolbar{align-items:stretch}.current-control-label{flex:1 1 140px}.current-range-toolbar .current-date-nav{justify-content:center;flex:1 1 135px}}.current-date-controls{display:flex;gap:7px;flex-wrap:wrap;align-items:center}.current-date-picker{border:1px solid var(--line);border-radius:999px;padding:6px 10px;background:#fff;color:var(--navy);font:inherit;font-size:.82rem;font-weight:750;min-height:34px}.current-date-picker:focus{outline:2px solid var(--blue);outline-offset:2px}.current-refreshing{opacity:.55;transition:opacity .15s ease}.current-date-controls a{display:inline-block;text-decoration:none;border:1px solid var(--line);border-radius:999px;padding:7px 11px;background:#fff;color:var(--blue);font-size:.82rem;font-weight:850}.current-date-controls a:hover{background:var(--paper)}.current-date-controls a.is-current{background:var(--navy);border-color:var(--navy);color:#fff}.current-planning{margin:16px 0 12px;padding:14px 16px;border:1px solid var(--line);border-radius:16px;background:#fff}.current-planning-head{display:flex;justify-content:space-between;gap:10px;align-items:baseline;flex-wrap:wrap;margin-bottom:10px}.current-planning-head strong{color:var(--navy);font-size:1rem}.current-planning-head span{color:var(--muted);font-size:.82rem}.planning-preferences{display:flex;gap:10px;align-items:flex-end;flex-wrap:wrap;margin:8px 0 12px}.planning-preferences label{display:flex;gap:5px;align-items:center;color:var(--muted);font-size:.78rem;font-weight:850}.planning-preferences input{min-height:40px;border:1px solid var(--line);border-radius:10px;background:#fff;color:var(--navy);font:inherit;font-size:1rem;padding:6px 8px}.planning-preferences input[type="number"]{width:76px}.planning-preferences b{color:var(--muted);font-size:.82rem}@media(max-width:640px){.planning-preferences label{flex:1 1 130px;justify-content:space-between}.planning-preferences input[type="time"]{min-width:110px}}.planning-help{margin:2px 0 12px;padding:10px 12px;border-radius:10px;background:#f7f9fa;color:var(--ink);font-size:.82rem;line-height:1.45}.planning-help strong{color:var(--navy)}.current-planning-days{display:grid;grid-template-columns:repeat(auto-fit,minmax(125px,1fr));gap:8px}.planning-day{border:1px solid var(--line);border-radius:12px;padding:10px;min-width:0}.planning-day.preferred{background:#eef8f3}.planning-day.caution{background:#fff8df;border-color:#e7c978}.planning-day.redflag{background:#fff0ed;border-color:#dfa297}.planning-date{font-size:.78rem;font-weight:850;color:var(--navy)}.planning-status{font-size:.92rem;font-weight:900;margin-top:2px}.preferred .planning-status{color:#176246}.caution .planning-status{color:#775900}.redflag .planning-status{color:#9a3328}.planning-detail{font-size:.78rem;line-height:1.35;color:var(--ink);margin-top:5px}.planning-disclaimer{color:var(--muted);font-size:.75rem;margin-top:9px}@media(max-width:640px){.current-planning-days{grid-template-columns:1fr 1fr}.planning-detail{font-size:.8rem}}.current-event-browser{margin:12px 0 4px;padding:14px 16px;background:var(--paper);border:1px solid var(--line);border-radius:16px}.current-event-readout{text-align:center;min-height:64px}.current-event-time{font-size:clamp(1.15rem,3vw,1.55rem);font-weight:900;color:var(--navy);font-variant-numeric:tabular-nums}.current-event-label{font-size:1rem;font-weight:750;color:var(--muted);margin-top:2px}.current-event-controls{display:grid;grid-template-columns:auto minmax(120px,1fr) auto;gap:10px;align-items:center;margin-top:10px}.current-event-slider{width:100%;min-height:44px;accent-color:var(--blue)}.current-event-button{min-height:44px;border:1px solid var(--line);border-radius:999px;background:#fff;color:var(--blue);font-weight:850;padding:8px 12px;cursor:pointer}.event-point.is-selected{stroke:#c63a2b;stroke-width:3;r:6}.event-cursor-line{stroke:#c63a2b;stroke-width:2;stroke-dasharray:5 4;pointer-events:none}@media(max-width:640px){.current-event-controls{grid-template-columns:1fr 1fr}.current-event-slider{grid-column:1/-1;grid-row:1}.current-event-button{font-size:.82rem}}.current-chart-wrap .event-point,.current-chart-wrap .event-point:hover{cursor:default!important;pointer-events:none}.current-chart-wrap{margin-top:16px}.current-chart-svg{display:block;width:100%;height:auto;background:#f8fbfc;border:1px solid var(--line);border-radius:16px}.grid-line{stroke:#d9e4e8;stroke-width:1}.v-grid-line{stroke:#e6eef1;stroke-width:1}.day-grid-line{stroke:#b7cbd4;stroke-width:1.4}.zero-line{stroke:#17384a;stroke-width:2}.axis-label{fill:#657d89;font-size:11px;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}.y-label{text-anchor:end}.x-label{text-anchor:middle}.axis-title{fill:#657d89;font-size:11px;text-anchor:middle}.night-window{fill:#aebdc4;opacity:.78}.sail-window{fill:#f8fbfc;opacity:.96}.preferred-window{fill:#f0d46d;opacity:.34}.flood-area{fill:#6d8fd0;opacity:.86}.ebb-area{fill:#0b9d83;opacity:.90}.current-line{fill:none;stroke:#214b62;stroke-width:1.5;stroke-linejoin:round;stroke-linecap:round}.event-point{stroke:#fff;stroke-width:1.5}.event-point.flood{fill:#5478bd}.event-point.ebb{fill:#078a75}.event-point.slack{fill:#756d64}.event-time{fill:#17384a;font-size:9px;font-weight:800;text-anchor:middle;paint-order:stroke;stroke:#fff;stroke-width:2.5px;stroke-linejoin:round}.event-time.flood{fill:#294f91}.event-time.ebb{fill:#066c5d}.event-time.slack{fill:#514b46}.now-line{stroke:#c63a2b;stroke-width:2.5}.now-label{fill:#c63a2b;font-size:11px;font-weight:800}.chart-explainer{color:var(--ink);font-size:.94rem;line-height:1.45;margin:2px 0 12px}.chart-note{color:var(--muted);font-size:.82rem;margin-top:9px}.candidate-table{width:100%;border-collapse:collapse;font-size:.86rem}.candidate-table th,.candidate-table td{padding:10px 8px;border-bottom:1px solid var(--line);text-align:left;vertical-align:top}.candidate-table th{font-size:.72rem;text-transform:uppercase;letter-spacing:.06em;color:var(--muted)}.candidate-table td.num,.candidate-table th.num{text-align:right;white-space:nowrap}.candidate-good td.status{font-weight:800}.candidate-bad{opacity:.82}.candidate-selected{background:rgba(20,120,100,.08)}.candidate-selected td:first-child{font-weight:800}.candidate-note{color:var(--muted);font-size:.82rem;margin:0 0 12px}.candidate-scroll{overflow-x:auto}.candidate-link{color:var(--blue);text-decoration:none;font-weight:800}.candidate-link:hover{text-decoration:underline}.candidate-actions{display:flex;gap:12px;align-items:center;flex-wrap:wrap;margin:0 0 12px}.nearest-link{display:inline-block;border:1px solid var(--line);border-radius:999px;padding:7px 12px;color:var(--blue);font-weight:800;text-decoration:none;background:#fff}.nearest-link:hover{background:var(--paper)}.map-card{overflow:hidden}.map-intro{display:flex;justify-content:space-between;gap:14px;align-items:flex-start;flex-wrap:wrap;margin-bottom:12px}.map-help{color:var(--muted);font-size:.9rem;max-width:600px}.map-wrap{border:1px solid var(--line);border-radius:16px;overflow:hidden;background:#dfecef}.location-map{height:390px;width:100%}.map-controls{display:flex;gap:10px;align-items:center;flex-wrap:wrap;margin-top:12px}.map-coordinate{font-variant-numeric:tabular-nums;color:var(--muted);font-size:.9rem}.map-go{display:inline-block;border:0;border-radius:999px;padding:10px 16px;background:var(--blue);color:#fff;font-weight:850;text-decoration:none;cursor:pointer}.map-go[aria-disabled="true"]{opacity:.45;pointer-events:none}.map-search-area{border:0;cursor:pointer}.map-search-area[aria-disabled="true"]{opacity:.55;pointer-events:none}.map-search-area[hidden]{display:none}.map-search-status{color:var(--muted);font-size:.82rem}.map-reset{border:1px solid var(--line);border-radius:999px;padding:9px 13px;background:#fff;color:var(--blue);font-weight:800;cursor:pointer}.map-layer-note{margin-top:8px;color:var(--muted);font-size:.8rem;line-height:1.4}.map-symbol{display:inline-flex;align-items:center;justify-content:center;width:32px;height:32px;margin-right:4px;font-size:27px;font-weight:950;line-height:1;vertical-align:-6px;text-shadow:-1px -1px 0 #fff,1px -1px 0 #fff,-1px 1px 0 #fff,1px 1px 0 #fff,0 2px 3px rgba(0,0,0,.35)}
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
.map-recenter-control{
  background:#fff;
  border-radius:8px;
  box-shadow:0 1px 6px rgba(0,0,0,.28);
  overflow:hidden;
}
.map-recenter-control button{
  display:block;
  width:100%;
  border:0;
  border-bottom:1px solid #d8e1e6;
  background:#fff;
  color:#12394f;
  padding:7px 9px;
  font:inherit;
  font-size:.78rem;
  font-weight:850;
  text-align:left;
  cursor:pointer;
}
.map-recenter-control button:last-child{border-bottom:0}
.map-overlay-control{background:rgba(255,255,255,.96);border:1px solid #9aa8ae;border-radius:6px;box-shadow:0 1px 4px rgba(0,0,0,.28);padding:7px 9px;color:#17394b;font-size:.82rem;font-weight:750}
.map-overlay-control label{display:flex;align-items:center;gap:6px;cursor:pointer;white-space:nowrap}
.map-overlay-control input{margin:0}

@media(max-width:700px){.map-recenter-control button{font-size:.72rem;padding:6px 8px}}
.map-recenter-control button:hover{background:#eef5f8}
.map-recenter-control button[hidden]{display:none}

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
.map-station-list{margin-top:12px}.map-station-list-title{font-weight:850;color:var(--navy);margin:0 0 8px}.map-station-table-wrap{overflow-x:auto}.map-station-table{width:100%;border-collapse:collapse;font-size:.86rem}.map-station-table th,.map-station-table td{padding:8px 10px;border-top:1px solid var(--line);text-align:left;vertical-align:top}.map-station-table th{color:var(--muted);font-size:.72rem;text-transform:uppercase;letter-spacing:.06em}.map-station-table a{color:var(--blue);font-weight:800;text-decoration:none}.map-station-table a:hover{text-decoration:underline}.map-legend{display:flex;gap:12px;flex-wrap:wrap;margin-top:10px;color:var(--muted);font-size:.78rem}.map-key{display:inline-flex;align-items:center;gap:5px}.map-dot{width:10px;height:10px;border-radius:50%;display:inline-block}.map-dot.request{background:#126b91}.map-dot.wind{background:#2f855a}.map-dot.current{background:#7d55a6}@media(max-width:600px){.location-map{height:330px}}.candidate-state{display:flex;gap:5px;flex-wrap:wrap}.candidate-badge{display:inline-block;border-radius:999px;padding:3px 7px;font-size:.68rem;font-weight:900;letter-spacing:.04em}.badge-auto{background:#e8f0fb;color:#24538a}.badge-selected{background:#e8f5ef;color:#176246}.candidate-auto td:first-child{font-weight:800}.error-card{border-left:5px solid #b64735;background:#fff7f4}.error-card h2{color:#8f3025}.error-message{font-weight:650;line-height:1.5}.error-help{color:var(--muted);font-size:.9rem}@media(max-width:640px){.shell{padding:14px 12px 40px}.hero{padding:24px 20px;min-height:430px;background-position:center 42%}.grid{grid-template-columns:1fr}.full{grid-column:auto}.metrics{grid-template-columns:1fr 1fr}.metric:first-child{grid-column:1/-1}.card{padding:18px}}</style></head><body><main class="shell">
<section class="hero"><div class="eyebrow">Mauri's Wind & Current Conditions</div><h1>{{.Title}}</h1><div class="sub">{{.ReportTime}} · {{.Station}}</div>{{if .Historical}}<span class="badge">Historical · {{.RequestedTime}}</span>{{end}}<div class="photo-tag">Bay sailing</div></section><div class="grid">
<section id="bottom-line-card" class="card full bottom"><h2>Bottom line</h2>{{range .BottomLine}}<p>{{.}}</p>{{else}}<p>Summary unavailable.</p>{{end}}</section>
<section class="card full map-card"><div class="map-intro"><div><h2>Choose Location</h2><div class="map-help">Click the map to choose a sailing location, then use Find stations near this point. Panning only changes the view; to search somewhere else, click the map to move the selected ★ location first. Click a nearby wind station to pin its details and preview the associated currents station, then use the selection link in the map panel to commit the wind-station choice. Candidate stations and distances always refer to the selected location.</div></div></div><div class="location-map-wrap"><div id="sailing-location-map" class="location-map" aria-label="Interactive supported coastal and inland waters conditions map"></div><div id="map-wind-info" class="map-wind-info" hidden aria-live="polite"></div></div><div class="map-controls"><span id="map-coordinate" class="map-coordinate">{{if .MapHasRequest}}Selected: {{printf "%.5f" .MapRequestLat}}, {{printf "%.5f" .MapRequestLon}}{{else}}Click the map to choose a location.{{end}}</span><a id="map-go" class="map-go" href="#" aria-disabled="{{if .MapHasRequest}}false{{else}}true{{end}}">Show Conditions</a><span id="map-find-point" class="map-go map-search-area" role="button" tabindex="0" hidden>Find stations near this point</span><span id="map-search-status" class="map-search-status" aria-live="polite"></span>{{if .MapHasRequest}}<button id="map-reset" class="map-reset" type="button">Clear selected location point</button>{{end}}</div><div class="map-layer-note">Layer control: <strong>Map</strong> uses OpenStreetMap; <strong>Nautical Chart</strong> uses NOAA's ENC-based Chart Display Service. Chart layer is for planning/reference and does not replace official navigation products.</div><div class="map-legend">{{if .MapHasRequest}}<span class="map-key"><span class="map-symbol request" aria-hidden="true">★</span>Selected location</span>{{end}}{{if .MapHasWind}}<span class="map-key"><span class="map-symbol wind" aria-hidden="true">▲</span>Selected wind station {{.MapWindStation}}</span>{{end}}{{if .WindCandidates}}<span class="map-key"><span class="map-symbol wind-candidate legend-triangle" aria-hidden="true"><span></span></span>Nearby wind stations</span>{{end}}{{if .MapHasCurrent}}<span class="map-key"><span class="map-symbol current" aria-hidden="true">◆</span>Currents station {{.MapCurrentStation}}</span>{{end}}</div><div id="map-station-list" class="map-station-list" aria-live="polite">{{if .MapHasWind}}<div class="meta"><strong>Selected wind source:</strong> {{.MapWindStation}}</div>{{end}}{{if .WindCandidates}}<div class="map-station-list-title">Nearby Wind Stations</div><div class="map-station-table-wrap"><table class="map-station-table"><thead><tr><th>Station</th><th>Name</th><th>From selected location</th></tr></thead><tbody>{{range .WindCandidates}}<tr><td><a class="map-station-report-link" href="{{.URL}}" data-base-href="{{.URL}}">{{.Station}}</a></td><td><a class="map-station-report-link" href="{{.URL}}" data-base-href="{{.URL}}">{{.Name}}</a></td><td>{{.Distance}}</td></tr>{{end}}</tbody></table></div>{{end}}</div></section>
{{if .WindError}}<section class="card full error-card"><h2>Wind station selection unavailable</h2><p class="error-message">{{.WindError}}</p><p class="error-help">The page is still available so you can inspect the request and nearby station diagnostics. Try nearby coordinates or an explicit NDBC station ID.</p></section>{{end}}
<section class="card wind-card"><h2>Wind</h2>
<div class="metrics">
<div class="metric"><div class="label">Direction</div><div class="value">{{if .WindDirection}}{{.WindDirection}}{{else}}—{{end}}</div></div>
<div class="metric"><div class="label">Wind</div><div class="value">{{if .WindSpeed}}{{.WindSpeed}}{{else}}—{{end}}</div></div>
<div class="metric"><div class="label">Gust</div><div class="value">{{if .WindGust}}{{.WindGust}}{{else}}—{{end}}</div></div>
</div>
<div class="meta"><strong>Observed:</strong> {{if .WindObserved}}{{.WindObserved}}{{else}}unavailable{{end}}</div>
{{if .WindSelection}}<div class="meta"><strong>Wind station:</strong> {{.WindSelection}}</div>{{end}}
{{if .WindDistanceWarning}}<div class="wind-distance-warning"><strong>Distance warning:</strong> {{.WindDistanceWarning}}</div>{{end}}
{{if .WindSummary}}<div class="wind-summary">{{.WindSummary}}</div>{{end}}
{{if .WindCandidates}}<div class="card-action-row"><a class="details-link" href="{{.WindStationsURL}}">Browse nearby wind stations →</a></div>{{end}}
</section>

<section id="current-summary-card" class="card"><h2>Current{{if .CurrentDateLabel}} — {{.CurrentDateLabel}}{{end}}</h2>{{if .CurrentStation}}<div class="station">{{.CurrentStation}}</div><div class="meta">{{.CurrentMeta}}</div>{{if .CurrentWindowMode}}<p><strong>{{.CurrentWindowMode}}</strong><br>{{.CurrentWindow}}</p>{{else if .CurrentWindow}}<p><strong>Conditions window</strong><br>{{.CurrentWindow}}</p>{{end}}{{range .CurrentOutlook}}<p>{{.}}</p>{{end}}{{else}}<p>Current prediction unavailable.</p>{{end}}</section>
{{if .CurrentChart}}<section id="current-chart-card" class="card full"><div class="current-chart-header"><div><h2>Tidal Current</h2>{{if .CurrentRangeLabel}}<div class="current-date-label">{{.CurrentRangeLabel}}</div>{{end}}</div></div><div class="current-range-toolbar" aria-label="Current graph date controls"><a class="current-date-nav" href="{{.CurrentPrevURL}}" aria-label="Previous date range">← Previous range</a><label class="current-control-label"><span>Start date</span><input id="current-date-picker" class="current-date-picker" type="date" value="{{.CurrentDateISO}}" aria-label="Choose starting date"></label><label class="current-control-label"><span>Range</span><select id="current-days-picker" class="current-date-picker" aria-label="Number of days"><option value="1" {{if eq .CurrentDays 1}}selected{{end}}>1 day</option><option value="3" {{if eq .CurrentDays 3}}selected{{end}}>3 days</option><option value="7" {{if eq .CurrentDays 7}}selected{{end}}>7 days</option></select></label><a class="current-date-nav {{if .CurrentIsToday}}is-current{{end}}" href="{{.CurrentTodayURL}}">Today</a><a class="current-date-nav" href="{{.CurrentNextURL}}" aria-label="Next date range">Next range →</a></div><div class="chart-explainer"><strong>This is current, not tide height.</strong> Above zero = flood; below zero = ebb; crossings = slack water.</div><div class="current-chart-wrap">{{.CurrentChart}}</div><div class="chart-note">NOAA 6-minute harmonic current predictions. Darker bands are night; light areas are daylight; warm bands mark the configured preferred planning period. {{if eq .CurrentDays 1}}Max flood, max ebb, and slack events are labeled with their times.{{else}}Small dots mark max flood, max ebb, and slack. Use the event navigator below for large, readable times.{{end}} {{if .CurrentIsToday}}Red line marks report time when it falls inside the displayed range.{{end}}{{if gt .CurrentDays 1}} Day boundaries are emphasized for multi-day planning.{{end}}</div>{{if gt .CurrentDays 1}}<div class="current-event-browser" data-current-event-browser><div class="current-event-readout" aria-live="polite"><div class="current-event-time">Select an event</div><div class="current-event-label">Use the slider or Previous / Next event</div></div><div class="current-event-controls"><button type="button" class="current-event-button" data-event-prev>← Previous event</button><input class="current-event-slider" data-event-slider type="range" min="0" max="0" value="0" step="1" aria-label="Current event"><button type="button" class="current-event-button" data-event-next>Next event →</button></div></div>{{end}}{{if .CurrentPlanningHints}}<div class="current-planning"><div class="current-planning-head"><strong>Preferred-period planning hint{{if eq .CurrentDays 1}} — today / selected day{{end}}</strong><span>Red flag when ebb or flood reaches its configured limit during the preferred window; caution when it does so within the buffer.</span></div><div class="planning-preferences"><label><span>Start</span><input id="planning-start" type="time" value="{{.PlanningStart}}" aria-label="Preferred period start"></label><label><span>End</span><input id="planning-end" type="time" value="{{.PlanningEnd}}" aria-label="Preferred period end"></label><label><span>Max ebb</span><input id="planning-max-ebb" type="number" min="0.1" max="10" step="0.1" value="{{.PlanningMaxEbb}}" aria-label="Maximum preferred ebb in knots"><b>kt</b></label><label><span>Max flood</span><input id="planning-max-flood" type="number" min="0.1" max="10" step="0.1" value="{{.PlanningMaxFlood}}" aria-label="Maximum preferred flood in knots"><b>kt</b></label><label><span>Buffer</span><input id="planning-buffer" type="number" min="0" max="360" step="15" value="{{.PlanningBuffer}}" aria-label="Preferred-period buffer in minutes"><b>min</b></label></div><div class="planning-help"><strong>How these settings work:</strong> Max ebb is the strongest ebb you want during your preferred period; strong ebb can become rough when wind opposes the current. Max flood is the strongest flood you want during that period; a large flood can make sailing against the current difficult. Buffer checks the same limits before and after the preferred period, catching strong current close enough to affect the start or end of your outing. <strong>Red flag</strong> means an ebb or flood limit is reached during the preferred period. <strong>Caution</strong> means a limit is reached only within the buffer. <strong>Preferred</strong> means both stay below their limits.</div><div class="current-planning-days">{{range .CurrentPlanningHints}}<div class="planning-day {{.Class}}"><div class="planning-date">{{.Date}}</div><div class="planning-status">{{if eq .Class "preferred"}}✓{{else if eq .Class "redflag"}}⚠{{else}}△{{end}} {{.Status}}</div><div class="planning-detail">{{.Detail}}</div></div>{{end}}</div><div class="planning-disclaimer">Current-based planning hint only; wind, swell, weather, traffic, and local effects still matter.</div></div>{{end}}</section>{{end}}
<section id="current-timeline-card" class="card full"><h2>Current timeline{{if .CurrentDateLabel}} — {{.CurrentDateLabel}}{{end}}</h2>{{if gt .CurrentDays 1}}<p class="timeline-scope-note">Events below are for the selected start date only. The graph above shows the full {{.CurrentDays}}-day range.</p>{{end}}{{range .CurrentEvents}}<div class="event {{.Class}}"><div class="time">{{.Time}}</div><div class="dot"></div><div class="eventbody"><div class="eventlabel">{{.Label}}</div>{{if .Speed}}<div class="eventdata">{{.Speed}} · {{.Direction}}</div>{{end}}</div></div>{{else}}<p>No current events in the conditions window.</p>{{end}}</section>
<section id="full-report-card" class="card full details-link-card"><div><h2>Need the details?</h2><p class="details-note">Open the complete text-style report, including diagnostic and supporting information.</p></div><a class="details-link" href="{{.FullDetailsURL}}">View full report details →</a></section></div>
<div class="footer"><strong>Mauri's Wind & Current Conditions</strong><br>NOAA/NDBC observations + NOAA CO-OPS current predictions · Conditions-planning aid, not a navigation system</div></main><script>
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

  var mapLayerParam = new URL(window.location.href).searchParams.get("map_layer");
  var activeBaseLayer = mapLayerParam === "nautical" ? nauticalLayer : streetLayer;
  activeBaseLayer.addTo(map);

  L.control.layers(
    {
      "Map": streetLayer,
      "Nautical Chart": nauticalLayer
    },
    null,
    {
      collapsed: false,
      position: "topright"
    }
  ).addTo(map);

  var activeMapLayerName =
    mapLayerParam === "nautical" ? "nautical" : "map";

  map.on("baselayerchange", function(e) {
    activeMapLayerName = e.layer === nauticalLayer ? "nautical" : "map";
    updateGo();
  });

  var RecenterControl = L.Control.extend({
    options: {position: "topleft"},
    onAdd: function() {
      var container = L.DomUtil.create("div", "map-recenter-control leaflet-bar");

      var selectedButton = L.DomUtil.create("button", "", container);
      selectedButton.type = "button";
      selectedButton.innerHTML = "★ Selected location";
      selectedButton.title = "Recenter on selected sailing location";

      var windButton = L.DomUtil.create("button", "", container);
      windButton.type = "button";
      windButton.innerHTML = "▲ Selected wind station";
      windButton.title = "Recenter on selected wind station";

      var currentsButton = L.DomUtil.create("button", "", container);
      currentsButton.type = "button";
      currentsButton.innerHTML = "◆ Currents station";
      currentsButton.title = "Recenter on tidal currents station";

      L.DomEvent.disableClickPropagation(container);
      L.DomEvent.disableScrollPropagation(container);

      selectedButton.addEventListener("click", function(e) {
        e.preventDefault();
        e.stopPropagation();
        if (selectedMarker) {
          map.setView(selectedMarker.getLatLng(), map.getZoom());
        }
      });

      windButton.addEventListener("click", function(e) {
        e.preventDefault();
        e.stopPropagation();
        if (selectedWindMarker) {
          map.setView(selectedWindMarker.getLatLng(), map.getZoom());
        }
      });

      currentsButton.addEventListener("click", function(e) {
        e.preventDefault();
        e.stopPropagation();
        if (currentStationMarker) {
          map.setView(currentStationMarker.getLatLng(), map.getZoom());
        }
      });

      container._selectedButton = selectedButton;
      container._windButton = windButton;
      container._currentsButton = currentsButton;
      return container;
    }
  });

  var recenterControl = new RecenterControl();
  map.addControl(recenterControl);

  function updateRecenterControls() {
    if (!recenterControl || !recenterControl.getContainer()) return;

    var container = recenterControl.getContainer();
    var bounds = map.getBounds();

    if (container._selectedButton) {
      container._selectedButton.hidden =
        !selectedMarker || bounds.contains(selectedMarker.getLatLng());
    }

    if (container._windButton) {
      container._windButton.hidden =
        !selectedWindMarker || bounds.contains(selectedWindMarker.getLatLng());
    }

    if (container._currentsButton) {
      container._currentsButton.hidden =
        !currentStationMarker ||
        !map.hasLayer(currentStationMarker) ||
        bounds.contains(currentStationMarker.getLatLng());
    }

    var anyVisible =
      (container._selectedButton && !container._selectedButton.hidden) ||
      (container._windButton && !container._windButton.hidden) ||
      (container._currentsButton && !container._currentsButton.hidden);

    container.style.display = anyVisible ? "" : "none";
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
    currentsOverlayVisible: true
  };

  var sourcePoints = [];
  var candidateLayer = L.layerGroup().addTo(map);

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

    if (activeMapLayerName === "nautical") {
      target.searchParams.set("map_layer", "nautical");
    } else {
      target.searchParams.delete("map_layer");
    }

    return target.pathname + "?" + target.searchParams.toString() + target.hash;
  }


  function currentsOverlayEnabled() {
    return !!mapState.currentsOverlayVisible;
  }

  function syncCurrentsOverlayControl() {
    var checkbox = document.getElementById("map-show-currents");
    if (!checkbox) return;
    checkbox.checked = !!mapState.currentsOverlayVisible;
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
      if (!mapState.currentsOverlayVisible && map.hasLayer(currentStationMarker)) {
        map.removeLayer(currentStationMarker);
      }
    } else {
      currentStationMarker.setLatLng([Number(currentLat), Number(currentLon)]);
    }
    if (currentName) label += " — " + currentName;
    if (currentDistance) label += " (" + currentDistance + " from wind station)";
    if (currentStationMarker.getTooltip()) {
      currentStationMarker.setTooltipContent(label);
    }
    previewingCurrentStation = true;

    if (currentsOverlayEnabled() && !map.hasLayer(currentStationMarker)) {
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

  function windCandidateMarker(lat, lon, station, name, distance, url, isAuto, isSelected, currentStation, currentName, currentDistance, currentLat, currentLon) {
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
      zIndexOffset:500,
      title:station + " " + name
    }).addTo(candidateLayer);


    var state = "";
    if (isAuto) state += "<strong>AUTO</strong> ";
    // Selection is represented by the separate filled ▲ wind-source marker.

    marker.on("click", function(e) {
      if (e && e.originalEvent) L.DomEvent.stopPropagation(e.originalEvent);
      showWindInfo(station, name, distance, url, currentStation, currentName, currentDistance);
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
    "Selected sailing location"
  );
  {{end}}

  mapState.windCandidates = [
    {{range .WindCandidates}}
    {
      station: {{.Station}},
      name: {{.Name}},
      distance: {{.Distance}},
      lat: {{printf "%.6f" .Lat}},
      lon: {{printf "%.6f" .Lon}},
      url: {{.JSURL}},
      isAuto: {{if .IsAuto}}true{{else}}false{{end}},
      currentStation: {{.CurrentStation}},
      currentName: {{.CurrentName}},
      currentDistance: {{.CurrentDistance}},
      currentLat: {{printf "%.6f" .CurrentLat}},
      currentLon: {{printf "%.6f" .CurrentLon}}
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
        Number(c.currentLon)
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

  var CurrentsOverlayControl = L.Control.extend({
    options: {position:"topright"},
    onAdd: function() {
      var container = L.DomUtil.create("div", "map-overlay-control leaflet-control");
      container.innerHTML =
        '<label><input type="checkbox" id="map-show-currents" checked> Currents station</label>';
      L.DomEvent.disableClickPropagation(container);
      L.DomEvent.disableScrollPropagation(container);

      var checkbox = container.querySelector("#map-show-currents");
      checkbox.checked = !!mapState.currentsOverlayVisible;
      checkbox.addEventListener("change", function() {
        mapState.currentsOverlayVisible = checkbox.checked;
        if (currentStationMarker) {
          if (mapState.currentsOverlayVisible) {
            if (!map.hasLayer(currentStationMarker)) currentStationMarker.addTo(map);
          } else {
            if (map.hasLayer(currentStationMarker)) map.removeLayer(currentStationMarker);
          }
        }
        updateRecenterControls();
      });
      return container;
    }
  });
  new CurrentsOverlayControl().addTo(map);

  updateRecenterControls();

  map.on("moveend zoomend", function() {
    updateRecenterControls();
  });

  var coord = document.getElementById("map-coordinate");
  var go = document.getElementById("map-go");
  var findPoint = document.getElementById("map-find-point");
  var reset = document.getElementById("map-reset");

  var searchStatus = document.getElementById("map-search-status");

  function renderSearchControls() {
    if (findPoint) {
      findPoint.hidden = !mapState.selectedLocation;
      findPoint.textContent = "Find stations near this point";
      findPoint.setAttribute(
        "aria-disabled",
        mapState.stationSearch.busy ? "true" : "false"
      );
    }

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

  function updateGo() {
    if (!mapState.selectedLocation) {
      go.setAttribute("aria-disabled","true");
      go.setAttribute("href","#");
      return;
    }

    var target = new URL(window.location.href);
    target.pathname = "/report";
    target.searchParams.set("lat", mapState.selectedLocation.lat.toFixed(5));
    target.searchParams.set("lon", mapState.selectedLocation.lon.toFixed(5));
    target.searchParams.set("format","html");
    var liveCenter = map.getCenter();
    target.searchParams.set("map_center_lat", liveCenter.lat.toFixed(5));
    target.searchParams.set("map_center_lon", liveCenter.lng.toFixed(5));
    target.searchParams.set("map_zoom", String(map.getZoom()));
    if (activeMapLayerName === "nautical") {
      target.searchParams.set("map_layer", "nautical");
    } else {
      target.searchParams.delete("map_layer");
    }

    // A new sailing location should drive fresh automatic station selection.
    target.searchParams.delete("station");
    target.searchParams.delete("current_station");
    target.searchParams.delete("bin");

    go.href = target.pathname + "?" + target.searchParams.toString();
    go.setAttribute("aria-disabled","false");
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
  function showWindInfo(station, name, distance, url, currentStation, currentName, currentDistance) {
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
              lat: Number(c.lat),
              lon: Number(c.lon),
              url: String(c.url || ""),
              isAuto: false,
              currentStation: String(c.current_station || ""),
              currentName: String(c.current_name || ""),
              currentDistance: String(c.current_distance || ""),
              currentLat: Number(c.current_lat),
              currentLon: Number(c.current_lon)
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
              var reportURL = escapeHTML(c.url || "#");
              return "<tr>" +
                '<td><a class="map-station-report-link" href="' + reportURL +
                '" data-base-href="' + reportURL + '">' + station + "</a></td>" +
                '<td><a class="map-station-report-link" href="' + reportURL +
                '" data-base-href="' + reportURL + '">' + name + "</a></td>" +
                "<td>" + distance + "</td>" +
                "</tr>";
            }).join("");

            stationList.innerHTML =
              '<div class="map-station-list-title">Nearby Wind Stations</div>' +
              '<div class="map-station-table-wrap">' +
              '<table class="map-station-table">' +
              '<thead><tr><th>Station</th><th>Name</th><th>From selected location</th></tr></thead>' +
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

    mapState.selectedLocation = {lat:e.latlng.lat, lon:e.latlng.lng};

    if (selectedMarker) {
      selectedMarker.setLatLng(e.latlng);
    } else {
      selectedMarker = circleMarker(
        e.latlng.lat,
        e.latlng.lng,
        "#126b91",
        "Selected location"
      );
    }

    coord.textContent =
      "Selected: " +
      mapState.selectedLocation.lat.toFixed(5) +
      ", " +
      mapState.selectedLocation.lon.toFixed(5);

    renderSearchControls();
    updateGo();
    updateRecenterControls();
    renderWindMarkers();
  });

  if (reset) {
    reset.addEventListener("click", function() {
      mapState.selectedLocation = null;
      if (selectedMarker) {
        map.removeLayer(selectedMarker);
        selectedMarker = null;
      }
      coord.textContent = "Click the map to choose a location.";
      renderSearchControls();
      updateGo();
      updateRecenterControls();
      renderWindMarkers();
    });
  }

  updateGo();
})();

(function(){
  var replaceIDs = [
    "bottom-line-card",
    "current-summary-card",
    "current-chart-card",
    "current-timeline-card",
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
    var maxEbb = document.getElementById("planning-max-ebb");
    var maxFlood = document.getElementById("planning-max-flood");
    var buffer = document.getElementById("planning-buffer");

    var startValue = start ? start.value : "12:00";
    var endValue = end ? end.value : "17:00";
    var maxValue = maxEbb ? Number(maxEbb.value) : 1.5;
    var maxFloodValue = maxFlood ? Number(maxFlood.value) : 1.5;
    var bufferValue = buffer ? Number(buffer.value) : 60;

    if (!/^\d{2}:\d{2}$/.test(startValue)) startValue = "12:00";
    if (!/^\d{2}:\d{2}$/.test(endValue)) endValue = "17:00";
    if (!Number.isFinite(maxValue) || maxValue <= 0) maxValue = 1.5;
    if (!Number.isFinite(maxFloodValue) || maxFloodValue <= 0) maxFloodValue = 1.5;
    if (!Number.isFinite(bufferValue) || bufferValue < 0) bufferValue = 60;
    bufferValue = Math.max(0, Math.min(360, Math.round(bufferValue)));

    if (startValue === "12:00") target.searchParams.delete("planning_start");
    else target.searchParams.set("planning_start", startValue);

    if (endValue === "17:00") target.searchParams.delete("planning_end");
    else target.searchParams.set("planning_end", endValue);

    if (Math.abs(maxValue - 1.5) < 0.001) target.searchParams.delete("max_ebb");
    else target.searchParams.set("max_ebb", maxValue.toFixed(1));

    if (Math.abs(maxFloodValue - 1.5) < 0.001) target.searchParams.delete("max_flood");
    else target.searchParams.set("max_flood", maxFloodValue.toFixed(1));

    if (bufferValue === 60) target.searchParams.delete("planning_buffer");
    else target.searchParams.set("planning_buffer", String(bufferValue));

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
      initCurrentEventBrowser();
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
    if (
      e.target.id !== "planning-start" &&
      e.target.id !== "planning-end" &&
      e.target.id !== "planning-max-ebb" &&
      e.target.id !== "planning-max-flood" &&
      e.target.id !== "planning-buffer"
    ) return;
    loadCurrentURL(currentURLForPlanning(), true);
  });

  function initCurrentEventBrowser() {
    var browser = document.querySelector("[data-current-event-browser]");
    var svg = document.querySelector("#current-chart-card .current-chart-svg");
    if (!browser || !svg || browser.dataset.ready === "1") return;
    var events = Array.prototype.slice.call(svg.querySelectorAll("circle.event-point[data-event-time]"));
    if (!events.length) return;
    browser.dataset.ready = "1";
    var slider = browser.querySelector("[data-event-slider]");
    var timeEl = browser.querySelector(".current-event-time");
    var labelEl = browser.querySelector(".current-event-label");
    var prev = browser.querySelector("[data-event-prev]");
    var next = browser.querySelector("[data-event-next]");
    slider.max = String(events.length - 1);
    var cursor = document.createElementNS("http://www.w3.org/2000/svg", "line");
    cursor.setAttribute("class", "event-cursor-line");
    cursor.setAttribute("y1", "18");
    cursor.setAttribute("y2", "288");
    svg.appendChild(cursor);
    function select(index) {
      index = Math.max(0, Math.min(events.length - 1, index));
      slider.value = String(index);
      events.forEach(function(el){ el.classList.remove("is-selected"); });
      var event = events[index];
      event.classList.add("is-selected");
      var x = event.getAttribute("cx");
      cursor.setAttribute("x1", x);
      cursor.setAttribute("x2", x);
      timeEl.textContent = event.dataset.eventTime || "Current event";
      labelEl.textContent = event.dataset.eventLabel || "";
      prev.disabled = index === 0;
      next.disabled = index === events.length - 1;
    }
    slider.addEventListener("input", function(){ select(Number(slider.value)); });
    prev.addEventListener("click", function(){ select(Number(slider.value) - 1); });
    next.addEventListener("click", function(){ select(Number(slider.value) + 1); });
    events.forEach(function(event, index){
      event.style.cursor = "pointer";
      event.addEventListener("click", function(){ select(index); });
    });
    select(0);
  }

  document.addEventListener("click", function(e) {
    if (e.target.closest("[data-current-event-browser]")) initCurrentEventBrowser();
  });

  initCurrentEventBrowser();

  window.addEventListener("popstate", function() {
    loadCurrentURL(new URL(window.location.href), false);
  });
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
		BottomLine: bottomLineLines(report),
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
	writeBottomLineText(w, report)

	fmt.Fprintln(w)
	fmt.Fprintln(w, "WIND")
	fmt.Fprintln(w, "--------------------------------")
	writeWindSummaryText(w, report, loc)

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

func bottomLineLines(report *SailingReport) []string {
	var b strings.Builder
	writeBottomLineText(&b, report)

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
		writeHistoricalWindText(w, report, loc)

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
	writeBottomLineText(w, report)

	fmt.Fprintln(w)
	fmt.Fprintln(w, "WIND")
	fmt.Fprintln(w, "--------------------------------")
	writeWindSummaryText(w, report, loc)

	fmt.Fprintln(w)
	fmt.Fprintln(w, "CURRENT")
	fmt.Fprintln(w, "--------------------------------")
	writeCurrentText(w, report.Current)

	fmt.Fprintln(w)
	fmt.Fprintln(w, "WIND DETAILS")
	fmt.Fprintln(w, "--------------------------------")
	writeWindDetailsText(w, report, loc)
}

func writeBottomLineText(
	w io.Writer,
	report *SailingReport,
) {
	if report == nil || report.Latest == nil {
		fmt.Fprintln(w, "Insufficient data for a combined conditions summary.")
		return
	}

	latest := report.Latest
	if latest.GustKT > 0 {
		fmt.Fprintf(w, "Latest wind at %s: %s %.0f kt, gusting %.0f kt.\n",
			latest.Time.Format("3:04 PM"), latest.Direction, latest.WindKT, latest.GustKT)
	} else {
		fmt.Fprintf(w, "Latest wind at %s: %s %.0f kt.\n",
			latest.Time.Format("3:04 PM"), latest.Direction, latest.WindKT)
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
