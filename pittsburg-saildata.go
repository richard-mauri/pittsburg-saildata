package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	ndbcBaseURL = "https://www.ndbc.noaa.gov/data/realtime2"
	msToKnots   = 1.94384449
)

type Observation struct {
	Time      time.Time
	Direction float64
	WindMS    float64
	GustMS    float64
	HasDir    bool
	HasWind   bool
	HasGust   bool
}

type SailingReport struct {
	Station     string            `json:"station"`
	ReportTime  time.Time         `json:"report_time"`
	Latest      *WindObservation  `json:"latest,omitempty"`
	Last12Hours *WindStats        `json:"last_12_hours,omitempty"`
	Afternoon   []PeriodReport    `json:"afternoon,omitempty"`
	Historical  *HistoricalReport `json:"historical,omitempty"`
}

type PeriodReport struct {
	Label string     `json:"label"`
	Date  time.Time  `json:"date"`
	Stats *WindStats `json:"stats,omitempty"`
}

type WindObservation struct {
	Time       time.Time `json:"time"`
	AgeMinutes int       `json:"age_minutes"`
	Direction  string    `json:"direction,omitempty"`
	WindKT     float64   `json:"wind_kt,omitempty"`
	GustKT     float64   `json:"gust_kt,omitempty"`
}

type WindStats struct {
	Observations int       `json:"observations"`
	AverageWind  float64   `json:"average_wind_kt,omitempty"`
	MaxWind      float64   `json:"maximum_wind_kt,omitempty"`
	MaxWindTime  time.Time `json:"maximum_wind_time,omitempty"`
	MaxGust      float64   `json:"maximum_gust_kt,omitempty"`
	MaxGustTime  time.Time `json:"maximum_gust_time,omitempty"`
	Trend        string    `json:"trend,omitempty"`
}

type HistoricalReport struct {
	Requested    time.Time         `json:"requested"`
	WindowStart  time.Time         `json:"window_start"`
	WindowEnd    time.Time         `json:"window_end"`
	Closest      *WindObservation  `json:"closest,omitempty"`
	Stats        *WindStats        `json:"stats,omitempty"`
	Observations []WindObservation `json:"observations,omitempty"`
}

func main() {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		fmt.Println("Unable to load Pacific timezone:", err)
		os.Exit(1)
	}

	server := flag.Bool(
		"server",
		false,
		"run as REST API server",
	)

	at := flag.String(
		"at",
		"",
		"historical date/time, e.g. \"2026-08-20 15:00\"",
	)

	port := flag.String(
		"port",
		"8080",
		"HTTP server port",
	)

	station := flag.String(
		"station",
		"PSBC1",
		"NDBC station ID",
	)

	flag.Usage = printUsage

	flag.Parse()

	stationID := strings.ToUpper(strings.TrimSpace(*station))

	if !validStationID(stationID) {
		fmt.Printf("Invalid station ID %q\n", stationID)
		os.Exit(1)
	}

	if *server {
		runServer(*port, stationID, loc)
		return
	}

	observations, err := getStation(stationID)
	if err != nil {
		fmt.Println("Error retrieving station:", err)
		os.Exit(1)
	}

	if len(observations) == 0 {
		fmt.Printf(
			"No usable observations found for %s.\n",
			stationID,
		)
		os.Exit(1)
	}

	var report *SailingReport

	if *at == "" {
		report = buildCurrentReport(
			stationID,
			observations,
			loc,
		)
	} else {
		report, err = buildHistoricalReport(
			stationID,
			observations,
			*at,
			loc,
		)

		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
	}

	writeTextReport(os.Stdout, report, loc)
}

// ---------------------------------------------------------------------
// HELP
// ---------------------------------------------------------------------

func printUsage() {
	fmt.Fprintf(os.Stderr, `
Pittsburg Delta Sailing Data

Usage:
  pittsburg-saildata [options]

Options:
  -server
        Run as a REST API server.

  -station STATION
        NDBC station ID. Default: PSBC1

  -port PORT
        HTTP server port. Default: 8080

  -at DATETIME
        Generate a historical report instead of a current report.
        Examples:
          2026-08-20T15:00
          2026-08-20 15:00

Examples:

  Start the REST server:

    ./pittsburg-saildata -server

  Start on a different port:

    ./pittsburg-saildata -server -port 9090

  Use a different default station:

    ./pittsburg-saildata -server -station SANF1

  Current report using default PSBC1:

    curl "http://localhost:8080/report"

  Current report for another station:

    curl "http://localhost:8080/report?station=SANF1"

  Historical PSBC1 report:

    curl "http://localhost:8080/report?at=2026-08-20T15:00"

  Historical report for another station:

    curl "http://localhost:8080/report?station=SANF1&at=2026-08-20T15:00"

  Request JSON instead of text:

    curl -H "Accept: application/json" \
      "http://localhost:8080/report"

  Historical JSON report:

    curl -H "Accept: application/json" \
      "http://localhost:8080/report?station=SANF1&at=2026-08-20T15:00"

  Health check:

    curl "http://localhost:8080/health"

Command-line reports:

  Current PSBC1 report:

    ./pittsburg-saildata

  Historical PSBC1 report:

    ./pittsburg-saildata -at "2026-08-20T15:00"

  Historical report for SANF1:

    ./pittsburg-saildata -station SANF1 -at "2026-08-20T15:00"

`)
}

// ---------------------------------------------------------------------
// REST SERVER
// ---------------------------------------------------------------------

func runServer(
	port string,
	defaultStation string,
	loc *time.Location,
) {
	mux := http.NewServeMux()

	mux.HandleFunc("/health", healthHandler)

	mux.HandleFunc(
		"/report",
		func(w http.ResponseWriter, r *http.Request) {

			if r.Method != http.MethodGet {
				http.Error(
					w,
					"method not allowed",
					http.StatusMethodNotAllowed,
				)
				return
			}

			requestStation := r.URL.Query().Get("station")

			if requestStation == "" {
				requestStation = defaultStation
			}

			requestStation = strings.ToUpper(
				strings.TrimSpace(requestStation),
			)

			if !validStationID(requestStation) {
				http.Error(
					w,
					"invalid station ID",
					http.StatusBadRequest,
				)
				return
			}

			at := r.URL.Query().Get("at")

			fmt.Printf(
				"request: station=%q at=%q\n",
				requestStation,
				at,
			)

			observations, err := getStation(requestStation)
			if err != nil {
				http.Error(
					w,
					fmt.Sprintf(
						"station error: %v",
						err,
					),
					http.StatusBadGateway,
				)
				return
			}

			var report *SailingReport

			if at == "" {
				report = buildCurrentReport(
					requestStation,
					observations,
					loc,
				)
			} else {
				report, err = buildHistoricalReport(
					requestStation,
					observations,
					at,
					loc,
				)

				if err != nil {
					http.Error(
						w,
						err.Error(),
						http.StatusBadRequest,
					)
					return
				}
			}

			if wantsJSON(r) {
				writeJSONReport(w, report)
			} else {
				writeTextReport(w, report, loc)
			}
		},
	)

    port = os.Getenv("PORT")
    if port == "" {
        port = "8080"
    }
	addr := ":" + port

	fmt.Println("Pittsburg Delta sailing API")
	fmt.Printf(
		"Default station: %s\n",
		defaultStation,
	)
	fmt.Printf(
		"Listening on http://localhost%s\n",
		addr,
	)
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  /report")
	fmt.Println("  /report?station=SANF1")
	fmt.Println("  /report?at=2026-08-20T15:00")
	fmt.Println(
		"  /report?station=SANF1&at=2026-08-20T15:00",
	)

	err := http.ListenAndServe(addr, mux)
	if err != nil {
		fmt.Println("Server error:", err)
		os.Exit(1)
	}
}

func healthHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	w.Header().Set(
		"Content-Type",
		"text/plain; charset=utf-8",
	)

	fmt.Fprintln(w, "OK")
}

func wantsJSON(r *http.Request) bool {
	return strings.Contains(
		r.Header.Get("Accept"),
		"application/json",
	)
}

func writeJSONReport(
	w http.ResponseWriter,
	report *SailingReport,
) {
	w.Header().Set(
		"Content-Type",
		"application/json; charset=utf-8",
	)

	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(report); err != nil {
		fmt.Println(
			"JSON encoding error:",
			err,
		)
	}
}

// ---------------------------------------------------------------------
// STATION RETRIEVAL
// ---------------------------------------------------------------------

func getStation(
	station string,
) ([]Observation, error) {
	url := fmt.Sprintf(
		"%s/%s.txt",
		ndbcBaseURL,
		station,
	)

	client := &http.Client{
		Timeout: 15 * time.Second,
	}

	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"NDBC returned HTTP %d for station %s",
			resp.StatusCode,
			station,
		)
	}

	return parseStation(resp.Body)
}

// ---------------------------------------------------------------------
// NDBC PARSER
// ---------------------------------------------------------------------

func parseStation(
	r io.Reader,
) ([]Observation, error) {
	var observations []Observation

	scanner := bufio.NewScanner(r)

	scanner.Buffer(
		make([]byte, 4096),
		1024*1024,
	)

	for scanner.Scan() {
		line := strings.TrimSpace(
			scanner.Text(),
		)

		if line == "" ||
			strings.HasPrefix(line, "#") {
			continue
		}

		fields := strings.Fields(line)

		if len(fields) < 8 {
			continue
		}

		year, err1 := strconv.Atoi(fields[0])
		month, err2 := strconv.Atoi(fields[1])
		day, err3 := strconv.Atoi(fields[2])
		hour, err4 := strconv.Atoi(fields[3])
		minute, err5 := strconv.Atoi(fields[4])

		if err1 != nil ||
			err2 != nil ||
			err3 != nil ||
			err4 != nil ||
			err5 != nil {
			continue
		}

		t := time.Date(
			year,
			time.Month(month),
			day,
			hour,
			minute,
			0,
			0,
			time.UTC,
		)

		wdir, dirOK :=
			parseOptionalFloat(fields[5])

		windMS, windOK :=
			parseOptionalFloat(fields[6])

		gustMS, gustOK :=
			parseOptionalFloat(fields[7])

		observations = append(
			observations,
			Observation{
				Time:      t,
				Direction: wdir,
				WindMS:    windMS,
				GustMS:    gustMS,
				HasDir:    dirOK,
				HasWind:   windOK,
				HasGust:   gustOK,
			},
		)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return observations, nil
}

// ---------------------------------------------------------------------
// CURRENT REPORT
// ---------------------------------------------------------------------

func buildCurrentReport(
	station string,
	observations []Observation,
	loc *time.Location,
) *SailingReport {
	now := time.Now().In(loc)

	latest := findLatest(observations)

	start12 := now.Add(
		-12 * time.Hour,
	)

	last12 := filter(
		observations,
		func(o Observation) bool {
			t := o.Time.In(loc)

			return !t.Before(start12) &&
				!t.After(now)
		},
	)

	report := &SailingReport{
		Station:    station,
		ReportTime: now,
		Latest: makeWindObservation(
			latest,
			now,
			loc,
		),
		Last12Hours: calculateStats(
			last12,
			loc,
		),
	}

	for daysAgo := 1; daysAgo <= 2; daysAgo++ {

		d := now.AddDate(
			0,
			0,
			-daysAgo,
		)

		start := time.Date(
			d.Year(),
			d.Month(),
			d.Day(),
			12,
			0,
			0,
			0,
			loc,
		)

		end := time.Date(
			d.Year(),
			d.Month(),
			d.Day(),
			17,
			0,
			0,
			0,
			loc,
		)

		period := filter(
			observations,
			func(o Observation) bool {
				t := o.Time.In(loc)

				return !t.Before(start) &&
					t.Before(end)
			},
		)

		report.Afternoon = append(
			report.Afternoon,
			PeriodReport{
				Label: fmt.Sprintf(
					"%d days ago",
					daysAgo,
				),
				Date: d,
				Stats: calculateStats(
					period,
					loc,
				),
			},
		)
	}

	return report
}

// ---------------------------------------------------------------------
// HISTORICAL REPORT
// ---------------------------------------------------------------------

func buildHistoricalReport(
	station string,
	observations []Observation,
	value string,
	loc *time.Location,
) (*SailingReport, error) {

	target, err := parseHistoricalTime(
		value,
		loc,
	)

	if err != nil {
		return nil, fmt.Errorf(
			"invalid date/time %q; use YYYY-MM-DD HH:MM",
			value,
		)
	}

	start := target.Add(
		-30 * time.Minute,
	)

	end := target.Add(
		30 * time.Minute,
	)

	window := filter(
		observations,
		func(o Observation) bool {
			t := o.Time.In(loc)

			return !t.Before(start) &&
				!t.After(end)
		},
	)

	if len(window) == 0 {
		return nil, fmt.Errorf(
			"no %s observations found from %s through %s",
			station,
			start.Format(time.RFC3339),
			end.Format(time.RFC3339),
		)
	}

	closest := findClosest(
		window,
		target,
	)

	historical := &HistoricalReport{
		Requested:   target,
		WindowStart: start,
		WindowEnd:   end,
		Closest: makeWindObservation(
			closest,
			target,
			loc,
		),
		Stats: calculateStats(
			window,
			loc,
		),
	}

	for _, o := range window {
		historical.Observations =
			append(
				historical.Observations,
				*makeWindObservation(
					o,
					target,
					loc,
				),
			)
	}

	return &SailingReport{
		Station:    station,
		ReportTime: time.Now().In(loc),
		Historical: historical,
	}, nil
}

// ---------------------------------------------------------------------
// STATISTICS
// ---------------------------------------------------------------------

func calculateStats(
	observations []Observation,
	loc *time.Location,
) *WindStats {

	if len(observations) == 0 {
		return nil
	}

	var (
		windSum     float64
		windCount   int
		windMax     float64
		gustMax     float64
		windMaxTime time.Time
		gustMaxTime time.Time
		haveWindMax bool
		haveGustMax bool
	)

	for _, o := range observations {

		if o.HasWind {
			windKT :=
				o.WindMS * msToKnots

			windSum += windKT
			windCount++

			if !haveWindMax ||
				windKT > windMax {

				windMax = windKT
				windMaxTime = o.Time
				haveWindMax = true
			}
		}

		if o.HasGust {
			gustKT :=
				o.GustMS * msToKnots

			if !haveGustMax ||
				gustKT > gustMax {

				gustMax = gustKT
				gustMaxTime = o.Time
				haveGustMax = true
			}
		}
	}

	stats := &WindStats{
		Observations: len(observations),
	}

	if windCount > 0 {
		stats.AverageWind =
			round1(
				windSum /
					float64(windCount),
			)

		stats.MaxWind =
			round1(windMax)

		stats.MaxWindTime =
			windMaxTime.In(loc)
	}

	if haveGustMax {
		stats.MaxGust =
			round1(gustMax)

		stats.MaxGustTime =
			gustMaxTime.In(loc)
	}

	stats.Trend =
		calculateTrend(observations)

	return stats
}

func calculateTrend(
	observations []Observation,
) string {

	var first *Observation
	var last *Observation

	for i := range observations {

		o := observations[i]

		if !o.HasWind {
			continue
		}

		if first == nil ||
			o.Time.Before(first.Time) {

			copy := o
			first = &copy
		}

		if last == nil ||
			o.Time.After(last.Time) {

			copy := o
			last = &copy
		}
	}

	if first == nil ||
		last == nil ||
		first.Time.Equal(last.Time) {
		return ""
	}

	change :=
		(last.WindMS -
			first.WindMS) *
			msToKnots

	switch {
	case change > 2:
		return fmt.Sprintf(
			"increasing (+%.1f kt)",
			change,
		)

	case change < -2:
		return fmt.Sprintf(
			"decreasing (%.1f kt)",
			change,
		)

	default:
		return fmt.Sprintf(
			"roughly steady (%+.1f kt)",
			change,
		)
	}
}

// ---------------------------------------------------------------------
// REPORT OUTPUT
// ---------------------------------------------------------------------

func writeTextReport(
	w io.Writer,
	report *SailingReport,
	loc *time.Location,
) {
	if report.Historical != nil {
		writeHistoricalText(
			w,
			report,
			loc,
		)
		return
	}

	writeCurrentText(
		w,
		report,
		loc,
	)
}

func writeCurrentText(
	w io.Writer,
	report *SailingReport,
	loc *time.Location,
) {
	fmt.Fprintf(
		w,
		"DELTA SAILING — %s WIND\n",
		report.Station,
	)

	fmt.Fprintln(
		w,
		"================================",
	)

	fmt.Fprintf(
		w,
		"Report time: %s\n\n",
		report.ReportTime.In(loc).
			Format(
				"Mon Jan 2, 2006 3:04:05 PM MST",
			),
	)

	fmt.Fprintf(
		w,
		"LATEST %s OBSERVATION\n",
		report.Station,
	)

	fmt.Fprintln(
		w,
		"--------------------------------",
	)

	printWindObservation(
		w,
		report.Latest,
		loc,
		report.ReportTime,
	)

	fmt.Fprintln(w)

	fmt.Fprintln(
		w,
		"LAST 12 HOURS",
	)

	fmt.Fprintln(
		w,
		"--------------------------------",
	)

	printStatsText(
		w,
		report.Last12Hours,
		loc,
	)

	for _, period := range report.Afternoon {

		fmt.Fprintln(w)

		fmt.Fprintf(
			w,
			"%s — %s — 12 PM–5 PM\n",
			strings.ToUpper(period.Label),
			period.Date.In(loc).
				Format("Mon Jan 2, 2006"),
		)

		fmt.Fprintln(
			w,
			"--------------------------------",
		)

		printStatsText(
			w,
			period.Stats,
			loc,
		)
	}
}

func writeHistoricalText(
	w io.Writer,
	report *SailingReport,
	loc *time.Location,
) {
	h := report.Historical

	fmt.Fprintf(
		w,
		"DELTA SAILING — HISTORICAL %s\n",
		report.Station,
	)

	fmt.Fprintln(
		w,
		"=================================",
	)

	fmt.Fprintf(
		w,
		"Requested:   %s\n",
		h.Requested.In(loc).
			Format(
				"Mon Jan 2, 2006 3:04 PM MST",
			),
	)

	fmt.Fprintf(
		w,
		"Window:      %s – %s\n\n",
		h.WindowStart.In(loc).
			Format("3:04 PM"),
		h.WindowEnd.In(loc).
			Format("3:04 PM"),
	)

	fmt.Fprintln(
		w,
		"CLOSEST OBSERVATION",
	)

	fmt.Fprintln(
		w,
		"--------------------------------",
	)

	printWindObservation(
		w,
		h.Closest,
		loc,
		h.Requested,
	)

	fmt.Fprintln(w)

	fmt.Fprintln(
		w,
		"±30 MINUTE WINDOW",
	)

	fmt.Fprintln(
		w,
		"--------------------------------",
	)

	printStatsText(
		w,
		h.Stats,
		loc,
	)

	fmt.Fprintln(w)

	fmt.Fprintln(
		w,
		"OBSERVATIONS",
	)

	fmt.Fprintln(
		w,
		"--------------------------------",
	)

	for _, o := range h.Observations {

		fmt.Fprintf(
			w,
			"%s  ",
			o.Time.In(loc).
				Format("3:04 PM"),
		)

		fmt.Fprintf(
			w,
			"%-3s ",
			o.Direction,
		)

		if o.WindKT != 0 {
			fmt.Fprintf(
				w,
				"%4.1f kt ",
				o.WindKT,
			)
		} else {
			fmt.Fprint(
				w,
				"  --   ",
			)
		}

		if o.GustKT != 0 {
			fmt.Fprintf(
				w,
				"gust %4.1f kt",
				o.GustKT,
			)
		} else {
			fmt.Fprint(
				w,
				"gust   --",
			)
		}

		fmt.Fprintln(w)
	}
}

func printWindObservation(
	w io.Writer,
	o *WindObservation,
	loc *time.Location,
	reference time.Time,
) {
	fmt.Fprintf(
		w,
		"Local:       %s\n",
		o.Time.In(loc).
			Format("3:04:05 PM MST"),
	)

	fmt.Fprintf(
		w,
		"UTC:         %s\n",
		o.Time.UTC().
			Format("2006-01-02 15:04:05 UTC"),
	)

	fmt.Fprintf(
		w,
		"Age:         %s\n",
		formatAge(
			absDuration(
				reference.Sub(o.Time),
			),
		),
	)

	if o.Direction != "" {
		fmt.Fprintf(
			w,
			"Wind:        %s %.1f kt\n",
			o.Direction,
			o.WindKT,
		)
	} else {
		fmt.Fprintln(
			w,
			"Wind:        missing",
		)
	}

	if o.GustKT != 0 {
		fmt.Fprintf(
			w,
			"Gust:        %.1f kt\n",
			o.GustKT,
		)
	} else {
		fmt.Fprintln(
			w,
			"Gust:        missing",
		)
	}
}

func printStatsText(
	w io.Writer,
	stats *WindStats,
	loc *time.Location,
) {
	if stats == nil {
		fmt.Fprintln(
			w,
			"No observations.",
		)
		return
	}

	fmt.Fprintf(
		w,
		"Observations: %d\n",
		stats.Observations,
	)

	if stats.AverageWind != 0 {
		fmt.Fprintf(
			w,
			"Average wind: %.1f kt\n",
			stats.AverageWind,
		)
	}

	if !stats.MaxWindTime.IsZero() {
		fmt.Fprintf(
			w,
			"Maximum wind: %.1f kt at %s\n",
			stats.MaxWind,
			stats.MaxWindTime.In(loc).
				Format("3:04 PM"),
		)
	}

	if !stats.MaxGustTime.IsZero() {
		fmt.Fprintf(
			w,
			"Maximum gust: %.1f kt at %s\n",
			stats.MaxGust,
			stats.MaxGustTime.In(loc).
				Format("3:04 PM"),
		)
	}

	if stats.Trend != "" {
		fmt.Fprintf(
			w,
			"Trend:         %s\n",
			stats.Trend,
		)
	}
}

// ---------------------------------------------------------------------
// OBSERVATION HELPERS
// ---------------------------------------------------------------------

func makeWindObservation(
	o Observation,
	reference time.Time,
	loc *time.Location,
) *WindObservation {

	result := &WindObservation{
		Time: o.Time.In(loc),
		AgeMinutes: int(
			absDuration(
				reference.Sub(o.Time),
			).Minutes(),
		),
	}

	if o.HasDir {
		result.Direction =
			compassDirection(o.Direction)
	}

	if o.HasWind {
		result.WindKT =
			round1(
				o.WindMS *
					msToKnots,
			)
	}

	if o.HasGust {
		result.GustKT =
			round1(
				o.GustMS *
					msToKnots,
			)
	}

	return result
}

func findLatest(
	observations []Observation,
) Observation {
	latest := observations[0]

	for _, o := range observations[1:] {
		if o.Time.After(latest.Time) {
			latest = o
		}
	}

	return latest
}

func findClosest(
	observations []Observation,
	target time.Time,
) Observation {
	closest := observations[0]

	for _, o := range observations[1:] {
		if absDuration(
			o.Time.Sub(target),
		) <
			absDuration(
				closest.Time.Sub(target),
			) {

			closest = o
		}
	}

	return closest
}

func filter(
	observations []Observation,
	predicate func(Observation) bool,
) []Observation {

	var result []Observation

	for _, o := range observations {
		if predicate(o) {
			result = append(
				result,
				o,
			)
		}
	}

	return result
}

// ---------------------------------------------------------------------
// TIME
// ---------------------------------------------------------------------

func parseHistoricalTime(
	value string,
	loc *time.Location,
) (time.Time, error) {

	formats := []string{
		"2006-01-02T15:04",
		"2006-01-02 15:04",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
	}

	for _, format := range formats {
		if t, err := time.ParseInLocation(
			format,
			value,
			loc,
		); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf(
		"invalid date/time",
	)
}

// ---------------------------------------------------------------------
// STATION VALIDATION
// ---------------------------------------------------------------------

func validStationID(
	station string,
) bool {
	if len(station) < 1 ||
		len(station) > 8 {
		return false
	}

	for _, r := range station {
		if (r < 'A' || r > 'Z') &&
			(r < '0' || r > '9') {
			return false
		}
	}

	return true
}

// ---------------------------------------------------------------------
// MISC
// ---------------------------------------------------------------------

func parseOptionalFloat(
	s string,
) (float64, bool) {
	if strings.EqualFold(
		s,
		"MM",
	) {
		return 0, false
	}

	value, err :=
		strconv.ParseFloat(
			s,
			64,
		)

	if err != nil {
		return 0, false
	}

	return value, true
}

func compassDirection(
	degrees float64,
) string {
	directions := []string{
		"N",
		"NNE",
		"NE",
		"ENE",
		"E",
		"ESE",
		"SE",
		"SSE",
		"S",
		"SSW",
		"SW",
		"WSW",
		"W",
		"WNW",
		"NW",
		"NNW",
	}

	degrees =
		float64(
			int(degrees) % 360,
		)

	if degrees < 0 {
		degrees += 360
	}

	index :=
		int(
			(degrees+11.25)/22.5,
		) % 16

	return directions[index]
}

func absDuration(
	d time.Duration,
) time.Duration {
	if d < 0 {
		return -d
	}

	return d
}

func formatAge(
	d time.Duration,
) string {
	if d < 0 {
		return "future timestamp"
	}

	seconds := int(
		d.Seconds(),
	)

	hours := seconds / 3600

	seconds %= 3600

	minutes := seconds / 60

	if hours > 0 {
		return fmt.Sprintf(
			"%dh %dm",
			hours,
			minutes,
		)
	}

	return fmt.Sprintf(
		"%dm",
		minutes,
	)
}

func round1(
	value float64,
) float64 {
	return float64(
		int(value*10+0.5),
	) / 10
}
