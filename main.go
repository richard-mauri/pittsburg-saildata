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

	fmt.Fprintln(w, "DELTA SAILING OUTLOOK")
	fmt.Fprintln(w, "================================")
	fmt.Fprintf(
		w,
		"Report time: %s\n\n",
		report.ReportTime.In(loc).Format("Mon Jan 2, 2006 3:04:05 PM MST"),
	)

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
