package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
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

		compact := queryBool(r, "compact")
		format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
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

type htmlCurrentEvent struct{ Time, Label, Speed, Direction, Class string }
type htmlReportData struct {
	Title, Station, ReportTime                       string
	Historical                                       bool
	RequestedTime                                    string
	WindDirection, WindSpeed, WindGust, WindObserved string
	WindSummary                                      string
	CurrentStation, CurrentMeta                      string
	CurrentOutlook                                   []string
	CurrentEvents                                    []htmlCurrentEvent
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
	// Generate the same wind summary used by the text report so the HTML
	// card always has an authoritative fallback.
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

	if report.Current != nil && report.Current.Error == "" {
		if report.Current.CurrentStation != nil {
			s := report.Current.CurrentStation
			d.CurrentStation = s.Name
			d.CurrentMeta = fmt.Sprintf("%s · bin %s · %s ft depth · %.1f nmi away", s.ID, report.Current.Bin, report.Current.Depth, s.DistanceNM)
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
	}
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

	return d
}

var sailingHTMLTemplate = template.Must(template.New("sailing").Parse(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="description" content="Live sailing-oriented wind and current outlook using NOAA/NDBC observations and NOAA CO-OPS predictions.">
<meta property="og:title" content="Mauri’s Sailing Outlook">
<meta property="og:description" content="Live sailing-oriented wind and current outlook using NOAA/NDBC observations and NOAA CO-OPS predictions.">
<meta property="og:type" content="website">
<meta property="og:url" content="https://pittsburg-saildata.onrender.com/">
<meta property="og:image" content="https://pittsburg-saildata.onrender.com/assets/hero.jpg">
<meta property="og:image:alt" content="Sailing on the San Francisco Bay and Delta">
<meta name="twitter:card" content="summary_large_image">
<meta name="twitter:title" content="Mauri’s Sailing Outlook">
<meta name="twitter:description" content="Live sailing-oriented wind and current outlook using NOAA/NDBC observations and NOAA CO-OPS predictions.">
<meta name="twitter:image" content="https://pittsburg-saildata.onrender.com/assets/hero.jpg">
<title>Mauri’s Sailing Outlook — {{.Title}}</title>
<style>:root{--navy:#082b45;--blue:#126b91;--sea:#0b8793;--ink:#153242;--muted:#607886;--paper:#f5fafc;--card:#fff;--line:#d8e7ed;--flood:#087f8c;--ebb:#365f91;--slack:#756d64;--shadow:0 12px 34px rgba(8,43,69,.10)}*{box-sizing:border-box}body{margin:0;background:linear-gradient(180deg,#dff3f8,#f7fbfc 32rem);color:var(--ink);font-family:-apple-system,BlinkMacSystemFont,"Segoe UI","Avenir Next",Avenir,Helvetica,Arial,sans-serif;line-height:1.45}.shell{max-width:880px;margin:auto;padding:28px 18px 64px}.hero{color:#fff;padding:34px 30px 30px;border-radius:24px;min-height:360px;display:flex;flex-direction:column;justify-content:flex-end;background:
linear-gradient(180deg,rgba(4,24,38,.06) 12%,rgba(4,24,38,.24) 48%,rgba(4,24,38,.86) 100%),
url('/assets/hero.jpg') center 48%/cover no-repeat;box-shadow:var(--shadow);text-shadow:0 2px 12px rgba(0,0,0,.45)}.eyebrow{text-transform:uppercase;letter-spacing:.14em;font-weight:800;font-size:.76rem;opacity:.8}.photo-tag{margin-top:14px;font-size:.72rem;letter-spacing:.12em;text-transform:uppercase;opacity:.72}h1{font-size:clamp(1.8rem,6vw,3.2rem);line-height:1.05;margin:.4rem 0 .6rem;letter-spacing:-.035em}.sub{opacity:.82}.grid{display:grid;grid-template-columns:1fr 1fr;gap:18px;margin-top:18px}.card{background:var(--card);border:1px solid var(--line);border-radius:20px;padding:22px;box-shadow:var(--shadow)}.full{grid-column:1/-1}h2{font-size:.82rem;letter-spacing:.13em;text-transform:uppercase;color:var(--blue);margin:0 0 16px}.bottom{font-size:1.13rem}.metrics{display:grid;grid-template-columns:repeat(3,1fr);gap:10px}.metric{background:var(--paper);border-radius:15px;padding:14px}.label{font-size:.73rem;text-transform:uppercase;letter-spacing:.08em;color:var(--muted);font-weight:700}.value{font-size:1.55rem;font-weight:800;color:var(--navy)}.meta{color:var(--muted);font-size:.88rem;margin-top:12px}.station{font-weight:800;font-size:1.1rem;color:var(--navy)}.wind-summary{white-space:pre-line;margin-top:14px;padding:13px 14px;background:#eef7fa;border-left:4px solid var(--sea);border-radius:10px;color:var(--ink);font-size:.92rem}.event{display:grid;grid-template-columns:88px 12px 1fr;gap:12px;align-items:center;min-height:58px}.time{font-weight:800;color:var(--navy)}.dot{width:12px;height:12px;border-radius:50%;background:var(--slack);box-shadow:0 0 0 5px #edf3f5}.flood .dot{background:var(--flood)}.ebb .dot{background:var(--ebb)}.eventbody{border-left:2px solid var(--line);padding:8px 0 8px 18px}.eventlabel{font-weight:800}.eventdata{color:var(--muted);font-size:.9rem}.badge{display:inline-block;border-radius:999px;padding:5px 10px;background:#e9f6fb;color:var(--blue);font-size:.75rem;font-weight:800;margin-top:12px}.footer{text-align:center;color:var(--muted);font-size:.78rem;margin-top:22px}.full-report{margin:0;white-space:pre-wrap;overflow-wrap:anywhere;font-family:"SFMono-Regular",Consolas,"Liberation Mono",Menlo,monospace;font-size:.88rem;line-height:1.55;background:#071f31;color:#e7f4f8;border-radius:14px;padding:18px;overflow-x:auto}.details-note{color:var(--muted);font-size:.88rem;margin:-4px 0 14px}@media(max-width:640px){.shell{padding:14px 12px 40px}.hero{padding:24px 20px;min-height:430px;background-position:center 42%}.grid{grid-template-columns:1fr}.full{grid-column:auto}.metrics{grid-template-columns:1fr 1fr}.metric:first-child{grid-column:1/-1}.card{padding:18px}}</style></head><body><main class="shell">
<section class="hero"><div class="eyebrow">Mauri’s Sailing Outlook</div><h1>{{.Title}}</h1><div class="sub">{{.ReportTime}} · {{.Station}}</div>{{if .Historical}}<span class="badge">Historical · {{.RequestedTime}}</span>{{end}}<div class="photo-tag">Bay sailing</div></section><div class="grid">
<section class="card full bottom"><h2>Bottom line</h2>{{range .BottomLine}}<p>{{.}}</p>{{else}}<p>Summary unavailable.</p>{{end}}</section>
<section class="card"><h2>Wind</h2>
<div class="metrics">
<div class="metric"><div class="label">Direction</div><div class="value">{{if .WindDirection}}{{.WindDirection}}{{else}}—{{end}}</div></div>
<div class="metric"><div class="label">Wind</div><div class="value">{{if .WindSpeed}}{{.WindSpeed}}{{else}}—{{end}}</div></div>
<div class="metric"><div class="label">Gust</div><div class="value">{{if .WindGust}}{{.WindGust}}{{else}}—{{end}}</div></div>
</div>
<div class="meta"><strong>Observed:</strong> {{if .WindObserved}}{{.WindObserved}}{{else}}unavailable{{end}}</div>
{{if .WindSummary}}<div class="wind-summary">{{.WindSummary}}</div>{{end}}
</section>
<section class="card"><h2>Current</h2>{{if .CurrentStation}}<div class="station">{{.CurrentStation}}</div><div class="meta">{{.CurrentMeta}}</div>{{range .CurrentOutlook}}<p>{{.}}</p>{{end}}{{else}}<p>Current prediction unavailable.</p>{{end}}</section>
<section class="card full"><h2>Current timeline</h2>{{range .CurrentEvents}}<div class="event {{.Class}}"><div class="time">{{.Time}}</div><div class="dot"></div><div class="eventbody"><div class="eventlabel">{{.Label}}</div>{{if .Speed}}<div class="eventdata">{{.Speed}} · {{.Direction}}</div>{{end}}</div></div>{{else}}<p>No current events in the selected sailing window.</p>{{end}}</section>
<section class="card full"><h2>Full report details</h2><p class="details-note">Complete CLI/text report. This section includes every detail available from the text endpoint.</p><pre class="full-report">{{.FullText}}</pre></section></div>
<div class="footer"><strong>Mauri’s Sailing Outlook</strong><br>NOAA/NDBC observations + NOAA CO-OPS current predictions · Sailing aid, not a navigation system</div></main></body></html>`))

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

	fmt.Fprintf(w, "SAILING OUTLOOK — %s (%s)\n", headingName, report.Station)
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

  Historical wind + current report:
    ./sailing-go -at "2026-08-20T15:00"

  Start REST server:
    ./sailing-go -server

  Local current combined report:
    curl -sS "http://localhost:8080/report"

  Richmond report:
    curl -sS "http://localhost:8080/report?station=RCMC1"

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
