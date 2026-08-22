package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	defaultWindStation = "PSBC1"
	defaultStartHour   = 12
	defaultEndHour     = 17
	timeZoneName       = "America/Los_Angeles"
)

type SailingReport struct {
	Station     string            `json:"station"`
	ReportTime  time.Time         `json:"report_time"`
	Latest      *WindObservation  `json:"latest,omitempty"`
	Latest10    []WindObservation `json:"latest_10,omitempty"`
	Last12Hours *WindStats        `json:"last_12_hours,omitempty"`
	Afternoon   []PeriodReport    `json:"afternoon,omitempty"`
	Current     *CurrentReport    `json:"current,omitempty"`
	Historical  *HistoricalReport `json:"historical,omitempty"`
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
	startHour := flag.Int("start", defaultStartHour, "current-report sailing window start hour")
	endHour := flag.Int("end", defaultEndHour, "current-report sailing window end hour")

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
	} else {
		report = buildCurrentWindReport(stationID, observations, loc)

		current, currentErr := BuildCurrentReport(
			stationID,
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

	mux.HandleFunc("/report", func(w http.ResponseWriter, r *http.Request) {
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

		at := r.URL.Query().Get("at")
		var report *SailingReport

		if at != "" {
			report, err = buildHistoricalWindReport(stationID, observations, at, loc)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		} else {
			report = buildCurrentWindReport(stationID, observations, loc)

			startHour := queryInt(r, "start", defaultStart)
			endHour := queryInt(r, "end", defaultEnd)

			current, currentErr := BuildCurrentReport(
				stationID,
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

		if wantsJSON(r) {
			writeJSONReport(w, report)
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

func writeTextReport(
	w io.Writer,
	report *SailingReport,
	loc *time.Location,
) {
	if report.Historical != nil {
		writeHistoricalWindText(w, report, loc)
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
		"SAILING OUTLOOK — %s (%s)\n",
		headingName,
		report.Station,
	)
	fmt.Fprintln(w, "================================")
	fmt.Fprintf(
		w,
		"Report time: %s\n\n",
		report.ReportTime.In(loc).Format("Mon Jan 2, 2006 3:04:05 PM MST"),
	)

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
		fmt.Fprintln(w, "Insufficient data for a combined sailing summary.")
		return
	}

	latest := report.Latest

	if latest.GustKT > 0 {
		fmt.Fprintf(
			w,
			"Latest wind at %s: %s %.0f kt, gusting %.0f kt.\n",
			latest.Time.Format("3:04 PM"),
			latest.Direction,
			latest.WindKT,
			latest.GustKT,
		)
	} else {
		fmt.Fprintf(
			w,
			"Latest wind at %s: %s %.0f kt.\n",
			latest.Time.Format("3:04 PM"),
			latest.Direction,
			latest.WindKT,
		)
	}

	if report.Current == nil || report.Current.Error != "" {
		fmt.Fprintln(w, "Current prediction is unavailable.")
		return
	}

	// Determine the predicted current phase at the beginning of the
	// configured sailing window from the event sequence.
	startPhase := ""
	for _, line := range report.Current.Outlook {
		switch {
		case strings.Contains(line, "starts on a flood"):
			startPhase = "flooding"
		case strings.Contains(line, "starts on an ebb"):
			startPhase = "ebbing"
		case strings.Contains(line, "begins close to slack"):
			startPhase = "near slack"
		}
		if startPhase != "" {
			break
		}
	}

	if startPhase != "" {
		fmt.Fprintf(
			w,
			"At %s, current is predicted to be %s.\n",
			report.Current.Start.Format("3:04 PM"),
			startPhase,
		)
	}

	// Find the first slack within the sailing window and the following phase.
	for i, event := range report.Current.Events {
		if event.Type != "slack" ||
			event.Time.Before(report.Current.Start) ||
			event.Time.After(report.Current.End) {
			continue
		}

		var next *CurrentEvent
		for j := i + 1; j < len(report.Current.Events); j++ {
			if report.Current.Events[j].Type == "flood" ||
				report.Current.Events[j].Type == "ebb" {
				copy := report.Current.Events[j]
				next = &copy
				break
			}
		}

		if next != nil {
			fmt.Fprintf(
				w,
				"Slack is around %s, then the current turns to a %s %s.\n",
				event.Time.Format("3:04 PM"),
				currentStrength(next.SpeedKT),
				next.Type,
			)
		} else {
			fmt.Fprintf(
				w,
				"Slack is around %s.\n",
				event.Time.Format("3:04 PM"),
			)
		}
		break
	}

	// End with a compact strength assessment.
	if len(report.Current.Outlook) > 0 {
		assessment := report.Current.Outlook[len(report.Current.Outlook)-1]

		switch {
		case strings.Contains(assessment, "relatively mild"):
			fmt.Fprintln(w, "Overall, current should be relatively mild during the sailing window.")
		case strings.Contains(assessment, "very light"):
			fmt.Fprintln(w, "Overall, current should be very light during the sailing window.")
		case strings.Contains(assessment, "moderate"):
			fmt.Fprintln(w, "Overall, expect moderate current during the sailing window.")
		case strings.Contains(assessment, "fairly strong"):
			fmt.Fprintln(w, "Overall, expect fairly strong current during part of the sailing window.")
		case strings.Contains(assessment, "strong"):
			fmt.Fprintln(w, "Overall, expect strong current during part of the sailing window.")
		}
	}
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

  -port PORT
        Local HTTP server port. Default: 8080
        Render's PORT environment variable takes precedence.

  -at DATETIME
        Historical wind report, e.g.:
          2026-08-20T15:00
          2026-08-20 15:00

  -start HOUR
        Current-prediction sailing window start.
        Default: 12

  -end HOUR
        Current-prediction sailing window end.
        Default: 17

Examples:

  Current combined report:
    ./sailing-go

  Richmond-area combined report:
    ./sailing-go -station RCMC1

  Historical wind report:
    ./sailing-go -at "2026-08-20T15:00"

  Start REST server:
    ./sailing-go -server

  Local current combined report:
    curl -sS "http://localhost:8080/report"

  Richmond report:
    curl -sS "http://localhost:8080/report?station=RCMC1"

  Change current window:
    curl -sS "http://localhost:8080/report?station=PSBC1&start=11&end=18"

  JSON:
    curl -sS \
      -H "Accept: application/json" \
      "http://localhost:8080/report?station=PSBC1"

`)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
