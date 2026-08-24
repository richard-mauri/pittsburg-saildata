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
	"strings"
	"time"
)

const (
	defaultWindStation = "PSBC1"
	defaultStartHour   = -1
	defaultEndHour     = -1
	timeZoneName       = "America/Los_Angeles"
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

			current, currentErr := BuildCurrentReport(
				stationID,
				currentStation,
				currentBin,
				report.Historical.Requested,
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

			current, currentErr := BuildCurrentReport(
				stationID,
				currentStation,
				currentBin,
				report.ReportTime,
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

type htmlWindCandidate struct {
	Rank       int
	Station    string
	Name       string
	Distance   string
	Met        string
	Status     string
	Reason     string
	Class      string
	URL        string
	IsAuto     bool
	IsSelected bool
}

type htmlCurrentEvent struct{ Time, Label, Speed, Direction, Class string }
type htmlReportData struct {
	Title, Station, ReportTime                       string
	Historical                                       bool
	RequestedTime                                    string
	WindDirection, WindSpeed, WindGust, WindObserved string
	WindSummary                                      string
	WindSelection                                    string
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
	CurrentOutlook                                   []string
	CurrentEvents                                    []htmlCurrentEvent
	CurrentChart                                     template.HTML
	BottomLine                                       []string
	FullText                                         string
}

func writeHTMLReport(w http.ResponseWriter, report *SailingReport, loc *time.Location) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := sailingHTMLTemplate.Execute(w, makeHTMLReportData(report, loc)); err != nil {
		http.Error(w, err.Error(), 500)
	}
}
func makeHTMLReportData(report *SailingReport, loc *time.Location) htmlReportData {
	d := htmlReportData{Station: report.Station, Title: report.Station}
	d.DebugWind = report.DebugWindSelection
	d.WindError = strings.TrimSpace(report.WindError)
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
			if report.DebugWindSelection {
				linkQuery.Set("debug_wind", "1")
			} else {
				linkQuery.Del("debug_wind")
			}

			d.WindCandidates = append(
				d.WindCandidates,
				htmlWindCandidate{
					Rank:       i + 1,
					Station:    strings.ToUpper(candidate.StationID),
					Name:       candidate.StationName,
					Distance:   fmt.Sprintf("%.1f nmi", candidate.DistanceNM),
					Met:        candidate.Met,
					Status:     strings.ToUpper(candidate.WindStatus),
					Reason:     reason,
					Class:      className,
					URL:        "/report?" + linkQuery.Encode(),
					IsAuto:     isAuto,
					IsSelected: isSelected,
				},
			)
		}
	}

	if report.WindSelection != nil {
		_, _, hasUserLocation, _ :=
			parseOptionalLatLon(report.RequestQuery)
		if hasUserLocation {
			nearestQuery := cloneQuery(report.RequestQuery)
			nearestQuery.Del("station")
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
		d.CurrentChart = buildCurrentChartSVG(report.Current, report.ReportTime, loc)
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

func buildCurrentChartSVG(
	report *CurrentReport,
	reportTime time.Time,
	loc *time.Location,
) template.HTML {
	if report == nil || len(report.Series) < 2 {
		return ""
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

	dayStart := time.Date(
		report.Series[0].Time.In(loc).Year(),
		report.Series[0].Time.In(loc).Month(),
		report.Series[0].Time.In(loc).Day(),
		0, 0, 0, 0,
		loc,
	)
	dayEnd := dayStart.Add(24 * time.Hour)

	maxAbs := 0.0
	for _, sample := range report.Series {
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
	for i, sample := range report.Series {
		x := xFor(sample.Time.In(loc))
		y := yFor(sample.VelocityKT)
		if i == 0 {
			fmt.Fprintf(&path, "M %.2f %.2f", x, y)
		} else {
			fmt.Fprintf(&path, " L %.2f %.2f", x, y)
		}
	}

	firstX := xFor(report.Series[0].Time.In(loc))
	lastX := xFor(report.Series[len(report.Series)-1].Time.In(loc))
	areaPath := fmt.Sprintf(
		"M %.2f %.2f L %.2f %.2f %s L %.2f %.2f Z",
		firstX, zeroY,
		firstX, yFor(report.Series[0].VelocityKT),
		strings.TrimPrefix(path.String(), fmt.Sprintf("M %.2f %.2f", firstX, yFor(report.Series[0].VelocityKT))),
		lastX, zeroY,
	)

	var svg strings.Builder
	fmt.Fprintf(&svg, `<svg class="current-chart-svg" viewBox="0 0 %.0f %.0f" role="img" aria-label="Predicted tidal current speed and direction through the day">`, width, height)
	fmt.Fprintf(&svg, `<defs><clipPath id="floodClip"><rect x="%.2f" y="%.2f" width="%.2f" height="%.2f"/></clipPath><clipPath id="ebbClip"><rect x="%.2f" y="%.2f" width="%.2f" height="%.2f"/></clipPath></defs>`,
		left, top, plotW, zeroY-top,
		left, zeroY, plotW, top+plotH-zeroY,
	)

	// Sailing window highlight.
	if !report.Start.IsZero() && !report.End.IsZero() {
		x1 := xFor(report.Start.In(loc))
		x2 := xFor(report.End.In(loc))
		fmt.Fprintf(&svg, `<rect class="sail-window" x="%.2f" y="%.2f" width="%.2f" height="%.2f"/>`,
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

	// Vertical time grid and labels every 3 hours.
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

	// Filled areas from the same NOAA 6-minute curve.
	fmt.Fprintf(&svg, `<path class="flood-area" d="%s" clip-path="url(#floodClip)"/>`, areaPath)
	fmt.Fprintf(&svg, `<path class="ebb-area" d="%s" clip-path="url(#ebbClip)"/>`, areaPath)
	fmt.Fprintf(&svg, `<path class="current-line" d="%s"/>`, path.String())

	// Mark maxima/slacks.
	for _, event := range report.Events {
		x := xFor(event.Time.In(loc))
		y := zeroY
		if event.Type == "flood" {
			y = yFor(event.SpeedKT)
		} else if event.Type == "ebb" {
			y = yFor(-event.SpeedKT)
		}
		fmt.Fprintf(&svg, `<circle class="event-point %s" cx="%.2f" cy="%.2f" r="4"/>`,
			event.Type, x, y)
	}

	// "Now" marker only when the report time falls on the plotted date.
	nowLocal := reportTime.In(loc)
	if nowLocal.Year() == dayStart.Year() &&
		nowLocal.YearDay() == dayStart.YearDay() {
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
<meta name="description" content="Mauri’s Bay & Delta Conditions — a Bay & Delta conditions tool using NOAA wind observations and current predictions for sailors, paddlers, and other people on the water.">
<meta property="og:title" content="Mauri’s Bay & Delta Conditions">
<meta property="og:description" content="Pick where you're sailing. Get nearby wind observations and predicted currents.">
<meta property="og:type" content="website">
<meta property="og:url" content="https://pittsburg-saildata.onrender.com/welcome">
<meta property="og:image" content="https://pittsburg-saildata.onrender.com/assets/hero.jpg">
<meta property="og:image:alt" content="Sailing on the San Francisco Bay and Delta">
<meta name="twitter:card" content="summary_large_image">
<meta name="twitter:title" content="Mauri’s Bay & Delta Conditions">
<meta name="twitter:description" content="Pick where you're sailing. Get nearby wind observations and predicted currents.">
<meta name="twitter:image" content="https://pittsburg-saildata.onrender.com/assets/hero.jpg">
<title>Mauri’s Bay & Delta Conditions — Welcome</title>
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
<div class="eyebrow">Mauri’s Bay & Delta Conditions</div>
<h1>Pick where you’re going. See the wind & current.</h1>
<p>A free Bay & Delta conditions tool for sailors, paddlers, and other people on the water, using nearby NOAA wind observations and predicted currents.</p>
<div class="cta-row">
<a class="cta primary" href="/report?format=html">See Bay & Delta Conditions</a>
<a class="cta secondary" href="https://github.com/richard-mauri/pittsburg-saildata">View on GitHub</a>
</div>
</section>

<div class="grid">
<section class="card quick">
<h2>The 30-second version</h2>
<ol>
<li>Open the Bay & Delta Conditions.</li>
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
<p><strong>Context:</strong> nearby station alternatives and relative current comparisons instead of relying only on vague words like “strong.”</p>
</section>

<section class="card full qa">
<h2>Questions sailors will probably ask</h2>
<details open><summary>Is this tide data or current data?</summary><p><strong>Current data.</strong> The report is about the predicted speed and direction of moving water — ebb, flood, maximum current, and slack. Tide height and current are related, but they are not the same thing.</p></details>
<details><summary>How does it choose the wind station?</summary><p>Clicking the map gives the service a latitude/longitude. It looks at nearby NOAA/NDBC meteorological stations, checks them for usable wind observations, and chooses the nearest usable one.</p></details>
<details><summary>Can I choose another wind station?</summary><p>Yes. The Nearby Wind Stations section is clickable. <strong>AUTO</strong> is the service's preferred station; <strong>SELECTED</strong> is the station currently driving the report.</p></details>
<details><summary>What does the current graph show?</summary><p>Predicted current speed through the day. Flood is above zero, ebb is below zero, and crossings indicate slack water.</p></details>
<details><summary>Why compare one ebb or flood with another?</summary><p>A raw knot value can be misleading without context. The report can show that an afternoon ebb, for example, is only about half as strong as the other ebb that day, and can compare it with recent cycles.</p></details>
<details><summary>Is this for navigation or safety decisions?</summary><p>No. It is a sailing-planning and conditions-exploration tool. Observations can be delayed or missing, station exposure differs, and current predictions are predictions. Use normal marine forecasts, charts, local knowledge, and seamanship.</p></details>
</section>

<section class="card full">
<h2>Know these waters? Your feedback is useful.</h2>
<p>If something looks questionable, that's worth reporting. Examples: a wind station that doesn't represent Alameda well, confusing current wording, an unexpectedly weak or strong ebb, or a feature that would make the report more useful.</p>
<p>You do not need to be a programmer to contribute useful sailing knowledge.</p>
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
<p class="note">Issues are not just for software bugs. Sailing terminology, station-selection concerns, and feature ideas are all useful.</p>
</section>

<section class="card full">
<h2>Want the geeky version?</h2>
<p>The browser submits the map point as decimal latitude/longitude. The Go service caches active NDBC station metadata, computes geographic distance, probes nearby candidates concurrently for usable wind, then combines that with NOAA CO-OPS current predictions. The same service also exposes text and JSON output for scripts and integrations.</p>
</section>
</div>

<div class="footer">Mauri’s Bay & Delta Conditions · San Francisco Bay & Delta · Sailing-planning utility, not a navigation system.</div>
</main>
</body>
</html>`))

var sailingHTMLTemplate = template.Must(template.New("sailing").Parse(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="description" content="Live sailing-oriented wind and current outlook using NOAA/NDBC observations and NOAA CO-OPS predictions.">
<meta property="og:title" content="Mauri’s Bay & Delta Conditions">
<meta property="og:description" content="Live sailing-oriented wind and current outlook using NOAA/NDBC observations and NOAA CO-OPS predictions.">
<meta property="og:type" content="website">
<meta property="og:url" content="https://pittsburg-saildata.onrender.com/">
<meta property="og:image" content="https://pittsburg-saildata.onrender.com/assets/hero.jpg">
<meta property="og:image:alt" content="Sailing on the San Francisco Bay and Delta">
<meta name="twitter:card" content="summary_large_image">
<meta name="twitter:title" content="Mauri’s Bay & Delta Conditions">
<meta name="twitter:description" content="Live sailing-oriented wind and current outlook using NOAA/NDBC observations and NOAA CO-OPS predictions.">
<meta name="twitter:image" content="https://pittsburg-saildata.onrender.com/assets/hero.jpg">
<title>Mauri’s Bay & Delta Conditions — {{.Title}}</title>
<link rel="stylesheet" href="https://unpkg.com/leaflet@1.9.4/dist/leaflet.css" crossorigin="">
<script src="https://unpkg.com/leaflet@1.9.4/dist/leaflet.js" crossorigin=""></script>
<style>:root{--navy:#082b45;--blue:#126b91;--sea:#0b8793;--ink:#153242;--muted:#607886;--paper:#f5fafc;--card:#fff;--line:#d8e7ed;--flood:#087f8c;--ebb:#365f91;--slack:#756d64;--shadow:0 12px 34px rgba(8,43,69,.10)}*{box-sizing:border-box}body{margin:0;background:linear-gradient(180deg,#dff3f8,#f7fbfc 32rem);color:var(--ink);font-family:-apple-system,BlinkMacSystemFont,"Segoe UI","Avenir Next",Avenir,Helvetica,Arial,sans-serif;line-height:1.45}.shell{max-width:880px;margin:auto;padding:28px 18px 64px}.hero{color:#fff;padding:34px 30px 30px;border-radius:24px;min-height:360px;display:flex;flex-direction:column;justify-content:flex-end;background:
linear-gradient(180deg,rgba(4,24,38,.06) 12%,rgba(4,24,38,.24) 48%,rgba(4,24,38,.86) 100%),
url('/assets/hero.jpg') center 48%/cover no-repeat;box-shadow:var(--shadow);text-shadow:0 2px 12px rgba(0,0,0,.45)}.eyebrow{text-transform:uppercase;letter-spacing:.14em;font-weight:800;font-size:.76rem;opacity:.8}.photo-tag{margin-top:14px;font-size:.72rem;letter-spacing:.12em;text-transform:uppercase;opacity:.72}h1{font-size:clamp(1.8rem,6vw,3.2rem);line-height:1.05;margin:.4rem 0 .6rem;letter-spacing:-.035em}.sub{opacity:.82}.grid{display:grid;grid-template-columns:1fr 1fr;gap:18px;margin-top:18px}.card{background:var(--card);border:1px solid var(--line);border-radius:20px;padding:22px;box-shadow:var(--shadow)}.full{grid-column:1/-1}h2{font-size:.82rem;letter-spacing:.13em;text-transform:uppercase;color:var(--blue);margin:0 0 16px}.bottom{font-size:1.13rem}.metrics{display:grid;grid-template-columns:repeat(3,1fr);gap:10px}.metric{background:var(--paper);border-radius:15px;padding:14px}.label{font-size:.73rem;text-transform:uppercase;letter-spacing:.08em;color:var(--muted);font-weight:700}.value{font-size:1.55rem;font-weight:800;color:var(--navy)}.meta{color:var(--muted);font-size:.88rem;margin-top:12px}.station{font-weight:800;font-size:1.1rem;color:var(--navy)}.wind-summary{white-space:pre-line;margin-top:14px;padding:13px 14px;background:#eef7fa;border-left:4px solid var(--sea);border-radius:10px;color:var(--ink);font-size:.92rem}.event{display:grid;grid-template-columns:88px 12px 1fr;gap:12px;align-items:center;min-height:58px}.time{font-weight:800;color:var(--navy)}.dot{width:12px;height:12px;border-radius:50%;background:var(--slack);box-shadow:0 0 0 5px #edf3f5}.flood .dot{background:var(--flood)}.ebb .dot{background:var(--ebb)}.eventbody{border-left:2px solid var(--line);padding:8px 0 8px 18px}.eventlabel{font-weight:800}.eventdata{color:var(--muted);font-size:.9rem}.badge{display:inline-block;border-radius:999px;padding:5px 10px;background:#e9f6fb;color:var(--blue);font-size:.75rem;font-weight:800;margin-top:12px}.footer{text-align:center;color:var(--muted);font-size:.78rem;margin-top:22px}.full-report{margin:0;white-space:pre-wrap;overflow-wrap:anywhere;font-family:"SFMono-Regular",Consolas,"Liberation Mono",Menlo,monospace;font-size:.88rem;line-height:1.55;background:#071f31;color:#e7f4f8;border-radius:14px;padding:18px;overflow-x:auto}.details-note{color:var(--muted);font-size:.88rem;margin:-4px 0 14px}.current-chart-wrap{margin-top:16px}.current-chart-svg{display:block;width:100%;height:auto;background:#f8fbfc;border:1px solid var(--line);border-radius:16px}.grid-line{stroke:#d9e4e8;stroke-width:1}.v-grid-line{stroke:#e6eef1;stroke-width:1}.zero-line{stroke:#17384a;stroke-width:2}.axis-label{fill:#657d89;font-size:11px;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}.y-label{text-anchor:end}.x-label{text-anchor:middle}.axis-title{fill:#657d89;font-size:11px;text-anchor:middle}.sail-window{fill:#dcebf0;opacity:.55}.flood-area{fill:#6d8fd0;opacity:.86}.ebb-area{fill:#0b9d83;opacity:.90}.current-line{fill:none;stroke:#214b62;stroke-width:1.5;stroke-linejoin:round;stroke-linecap:round}.event-point{stroke:#fff;stroke-width:1.5}.event-point.flood{fill:#5478bd}.event-point.ebb{fill:#078a75}.event-point.slack{fill:#756d64}.now-line{stroke:#c63a2b;stroke-width:2.5}.now-label{fill:#c63a2b;font-size:11px;font-weight:800}.chart-explainer{color:var(--ink);font-size:.94rem;line-height:1.45;margin:2px 0 12px}.chart-note{color:var(--muted);font-size:.82rem;margin-top:9px}.candidate-table{width:100%;border-collapse:collapse;font-size:.86rem}.candidate-table th,.candidate-table td{padding:10px 8px;border-bottom:1px solid var(--line);text-align:left;vertical-align:top}.candidate-table th{font-size:.72rem;text-transform:uppercase;letter-spacing:.06em;color:var(--muted)}.candidate-table td.num,.candidate-table th.num{text-align:right;white-space:nowrap}.candidate-good td.status{font-weight:800}.candidate-bad{opacity:.82}.candidate-selected{background:rgba(20,120,100,.08)}.candidate-selected td:first-child{font-weight:800}.candidate-note{color:var(--muted);font-size:.82rem;margin:0 0 12px}.candidate-scroll{overflow-x:auto}.candidate-link{color:var(--blue);text-decoration:none;font-weight:800}.candidate-link:hover{text-decoration:underline}.candidate-actions{display:flex;gap:12px;align-items:center;flex-wrap:wrap;margin:0 0 12px}.nearest-link{display:inline-block;border:1px solid var(--line);border-radius:999px;padding:7px 12px;color:var(--blue);font-weight:800;text-decoration:none;background:#fff}.nearest-link:hover{background:var(--paper)}.map-card{overflow:hidden}.map-intro{display:flex;justify-content:space-between;gap:14px;align-items:flex-start;flex-wrap:wrap;margin-bottom:12px}.map-help{color:var(--muted);font-size:.9rem;max-width:600px}.map-wrap{border:1px solid var(--line);border-radius:16px;overflow:hidden;background:#dfecef}.location-map{height:390px;width:100%}.map-controls{display:flex;gap:10px;align-items:center;flex-wrap:wrap;margin-top:12px}.map-coordinate{font-variant-numeric:tabular-nums;color:var(--muted);font-size:.9rem}.map-go{display:inline-block;border:0;border-radius:999px;padding:10px 16px;background:var(--blue);color:#fff;font-weight:850;text-decoration:none;cursor:pointer}.map-go[aria-disabled="true"]{opacity:.45;pointer-events:none}.map-reset{border:1px solid var(--line);border-radius:999px;padding:9px 13px;background:#fff;color:var(--blue);font-weight:800;cursor:pointer}.map-legend{display:flex;gap:12px;flex-wrap:wrap;margin-top:10px;color:var(--muted);font-size:.78rem}.map-key{display:inline-flex;align-items:center;gap:5px}.map-dot{width:10px;height:10px;border-radius:50%;display:inline-block}.map-dot.request{background:#126b91}.map-dot.wind{background:#db7b20}.map-dot.current{background:#7d55a6}@media(max-width:600px){.location-map{height:330px}}.candidate-state{display:flex;gap:5px;flex-wrap:wrap}.candidate-badge{display:inline-block;border-radius:999px;padding:3px 7px;font-size:.68rem;font-weight:900;letter-spacing:.04em}.badge-auto{background:#e8f0fb;color:#24538a}.badge-selected{background:#e8f5ef;color:#176246}.candidate-auto td:first-child{font-weight:800}.error-card{border-left:5px solid #b64735;background:#fff7f4}.error-card h2{color:#8f3025}.error-message{font-weight:650;line-height:1.5}.error-help{color:var(--muted);font-size:.9rem}@media(max-width:640px){.shell{padding:14px 12px 40px}.hero{padding:24px 20px;min-height:430px;background-position:center 42%}.grid{grid-template-columns:1fr}.full{grid-column:auto}.metrics{grid-template-columns:1fr 1fr}.metric:first-child{grid-column:1/-1}.card{padding:18px}}</style></head><body><main class="shell">
<section class="hero"><div class="eyebrow">Mauri’s Bay & Delta Conditions</div><h1>{{.Title}}</h1><div class="sub">{{.ReportTime}} · {{.Station}}</div>{{if .Historical}}<span class="badge">Historical · {{.RequestedTime}}</span>{{end}}<div class="photo-tag">Bay sailing</div></section><div class="grid">
<section class="card full bottom"><h2>Bottom line</h2>{{range .BottomLine}}<p>{{.}}</p>{{else}}<p>Summary unavailable.</p>{{end}}</section>
<section class="card full map-card"><div class="map-intro"><div><h2>Choose Location</h2><div class="map-help">Click anywhere on the map to choose where you’ll be on the water. Mauri’s Bay & Delta Conditions will use that latitude/longitude to find nearby wind observations and current predictions.</div></div></div><div id="sailing-location-map" class="location-map" aria-label="Interactive San Francisco Bay and Delta conditions map"></div><div class="map-controls"><span id="map-coordinate" class="map-coordinate">{{if .MapHasRequest}}Selected: {{printf "%.5f" .MapRequestLat}}, {{printf "%.5f" .MapRequestLon}}{{else}}Click the map to choose a location.{{end}}</span><a id="map-go" class="map-go" href="#" aria-disabled="{{if .MapHasRequest}}false{{else}}true{{end}}">Show Conditions</a>{{if .MapHasRequest}}<button id="map-reset" class="map-reset" type="button">Clear chosen point</button>{{end}}</div><div class="map-legend">{{if .MapHasRequest}}<span class="map-key"><span class="map-dot request"></span>Selected location</span>{{end}}{{if .MapHasWind}}<span class="map-key"><span class="map-dot wind"></span>Wind station {{.MapWindStation}}</span>{{end}}{{if .MapHasCurrent}}<span class="map-key"><span class="map-dot current"></span>Current station {{.MapCurrentStation}}</span>{{end}}</div></section>
{{if .WindError}}<section class="card full error-card"><h2>Wind station selection unavailable</h2><p class="error-message">{{.WindError}}</p><p class="error-help">The page is still available so you can inspect the request and nearby station diagnostics. Try nearby coordinates or an explicit NDBC station ID.</p></section>{{end}}
<section class="card"><h2>Wind</h2>
<div class="metrics">
<div class="metric"><div class="label">Direction</div><div class="value">{{if .WindDirection}}{{.WindDirection}}{{else}}—{{end}}</div></div>
<div class="metric"><div class="label">Wind</div><div class="value">{{if .WindSpeed}}{{.WindSpeed}}{{else}}—{{end}}</div></div>
<div class="metric"><div class="label">Gust</div><div class="value">{{if .WindGust}}{{.WindGust}}{{else}}—{{end}}</div></div>
</div>
<div class="meta"><strong>Observed:</strong> {{if .WindObserved}}{{.WindObserved}}{{else}}unavailable{{end}}</div>
{{if .WindSelection}}<div class="meta"><strong>Wind station:</strong> {{.WindSelection}}</div>{{end}}
{{if .WindSummary}}<div class="wind-summary">{{.WindSummary}}</div>{{end}}
</section>
{{if .WindCandidates}}<section class="card full"><h2>Nearby Wind Stations</h2><p class="candidate-note">{{if .DebugWind}}Nearby stations are probed concurrently and sorted by distance. Click any station to reload the outlook using that station.{{else}}Click a station to explore its wind and sailing outlook. Stations are ordered by distance from the current browsing location.{{end}}</p>{{if .UseNearestURL}}<div class="candidate-actions"><a class="nearest-link" href="{{.UseNearestURL}}">Use nearest usable station</a></div>{{end}}<div class="candidate-scroll"><table class="candidate-table"><thead><tr><th>#</th><th>Station</th><th>Name</th><th>State</th><th class="num">Distance</th>{{if .DebugWind}}<th>Met</th><th>Status</th><th>Reason</th>{{end}}</tr></thead><tbody>{{range .WindCandidates}}<tr class="{{.Class}}"><td>{{.Rank}}</td><td><a class="candidate-link" href="{{.URL}}">{{.Station}}</a></td><td><a class="candidate-link" href="{{.URL}}">{{.Name}}</a></td><td><div class="candidate-state">{{if .IsAuto}}<span class="candidate-badge badge-auto">AUTO</span>{{end}}{{if .IsSelected}}<span class="candidate-badge badge-selected">SELECTED</span>{{end}}</div></td><td class="num">{{.Distance}}</td>{{if $.DebugWind}}<td>{{.Met}}</td><td class="status">{{.Status}}</td><td>{{.Reason}}</td>{{end}}</tr>{{end}}</tbody></table></div></section>{{end}}

<section class="card"><h2>Current</h2>{{if .CurrentStation}}<div class="station">{{.CurrentStation}}</div><div class="meta">{{.CurrentMeta}}</div>{{if .CurrentWindowMode}}<p><strong>{{.CurrentWindowMode}}</strong><br>{{.CurrentWindow}}</p>{{else if .CurrentWindow}}<p><strong>Conditions window</strong><br>{{.CurrentWindow}}</p>{{end}}{{range .CurrentOutlook}}<p>{{.}}</p>{{end}}{{else}}<p>Current prediction unavailable.</p>{{end}}</section>
{{if .CurrentChart}}<section class="card full"><h2>Tidal Current Through the Day</h2><div class="chart-explainer"><strong>This is current, not tide height.</strong> Above zero = flood; below zero = ebb; crossings = slack water.</div><div class="current-chart-wrap">{{.CurrentChart}}</div><div class="chart-note">NOAA 6-minute harmonic current predictions. Shaded band marks the conditions window; red line marks report time.</div></section>{{end}}
<section class="card full"><h2>Current timeline</h2>{{range .CurrentEvents}}<div class="event {{.Class}}"><div class="time">{{.Time}}</div><div class="dot"></div><div class="eventbody"><div class="eventlabel">{{.Label}}</div>{{if .Speed}}<div class="eventdata">{{.Speed}} · {{.Direction}}</div>{{end}}</div></div>{{else}}<p>No current events in the conditions window.</p>{{end}}</section>
<section class="card full"><h2>Full report details</h2><p class="details-note">Complete CLI/text report. This section includes every detail available from the text endpoint.</p><pre class="full-report">{{.FullText}}</pre></section></div>
<div class="footer"><strong>Mauri’s Bay & Delta Conditions</strong><br>NOAA/NDBC observations + NOAA CO-OPS current predictions · Conditions-planning aid, not a navigation system</div></main><script>
(function(){
  var el = document.getElementById("sailing-location-map");
  if (!el || typeof L === "undefined") return;

  var centerLat = {{printf "%.6f" .MapCenterLat}};
  var centerLon = {{printf "%.6f" .MapCenterLon}};
  var map = L.map(el, {scrollWheelZoom:true}).setView([centerLat, centerLon], 9);

  L.tileLayer("https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png", {
    maxZoom: 18,
    attribution: '&copy; OpenStreetMap contributors'
  }).addTo(map);

  var selectedMarker = null;
  var sourcePoints = [];

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

  {{if .MapHasRequest}}
  selectedMarker = circleMarker(
    {{printf "%.6f" .MapRequestLat}},
    {{printf "%.6f" .MapRequestLon}},
    "#126b91",
    "Selected location"
  );
  {{end}}

  {{if .MapHasWind}}
  circleMarker(
    {{printf "%.6f" .MapWindLat}},
    {{printf "%.6f" .MapWindLon}},
    "#db7b20",
    "Wind station {{.MapWindStation}}"
  );
  {{end}}

  {{if .MapHasCurrent}}
  circleMarker(
    {{printf "%.6f" .MapCurrentLat}},
    {{printf "%.6f" .MapCurrentLon}},
    "#7d55a6",
    "Current station {{.MapCurrentStation}}"
  );
  {{end}}

  if (sourcePoints.length > 1) {
    map.fitBounds(sourcePoints, {padding:[35,35], maxZoom:10});
  }

  var coord = document.getElementById("map-coordinate");
  var go = document.getElementById("map-go");
  var reset = document.getElementById("map-reset");
  var chosen = {{if .MapHasRequest}}{
    lat: {{printf "%.6f" .MapRequestLat}},
    lon: {{printf "%.6f" .MapRequestLon}}
  }{{else}}null{{end}};

  function updateGo() {
    if (!chosen) {
      go.setAttribute("aria-disabled","true");
      go.setAttribute("href","#");
      return;
    }

    var target = new URL(window.location.href);
    target.pathname = "/report";
    target.searchParams.set("lat", chosen.lat.toFixed(5));
    target.searchParams.set("lon", chosen.lon.toFixed(5));
    target.searchParams.set("format","html");

    // A new sailing location should drive fresh automatic station selection.
    target.searchParams.delete("station");
    target.searchParams.delete("current_station");
    target.searchParams.delete("bin");

    go.href = target.pathname + "?" + target.searchParams.toString();
    go.setAttribute("aria-disabled","false");
  }

  map.on("click", function(e) {
    chosen = {lat:e.latlng.lat, lon:e.latlng.lng};

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
      chosen.lat.toFixed(5) +
      ", " +
      chosen.lon.toFixed(5);

    updateGo();
  });

  if (reset) {
    reset.addEventListener("click", function() {
      chosen = null;
      if (selectedMarker) {
        map.removeLayer(selectedMarker);
        selectedMarker = null;
      }
      coord.textContent = "Click the map to choose a location.";
      updateGo();
    });
  }

  updateGo();
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

	fmt.Fprintf(w, "BAY & DELTA CONDITIONS — %s (%s)\n", headingName, report.Station)
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
		"BAY & DELTA CONDITIONS — %s (%s)\n",
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
