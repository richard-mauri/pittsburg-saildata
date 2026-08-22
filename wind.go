package main

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"sort"
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

func getWindStation(station string) ([]Observation, error) {
	url := fmt.Sprintf("%s/%s.txt", ndbcBaseURL, station)

	client := &http.Client{Timeout: 15 * time.Second}
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

	return parseWindStation(resp.Body)
}

func parseWindStation(r io.Reader) ([]Observation, error) {
	var observations []Observation

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4096), 1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 8 {
			continue
		}

		year, e1 := strconv.Atoi(fields[0])
		month, e2 := strconv.Atoi(fields[1])
		day, e3 := strconv.Atoi(fields[2])
		hour, e4 := strconv.Atoi(fields[3])
		minute, e5 := strconv.Atoi(fields[4])

		if e1 != nil || e2 != nil || e3 != nil || e4 != nil || e5 != nil {
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

		wdir, dirOK := parseOptionalFloat(fields[5])
		windMS, windOK := parseOptionalFloat(fields[6])
		gustMS, gustOK := parseOptionalFloat(fields[7])

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

	return observations, scanner.Err()
}

func buildCurrentWindReport(
	station string,
	observations []Observation,
	loc *time.Location,
) *SailingReport {
	now := time.Now().In(loc)
	latest := findLatest(observations)
	latest10 := findLatestN(observations, 10)

	start12 := now.Add(-12 * time.Hour)

	last12 := filterObservations(
		observations,
		func(o Observation) bool {
			t := o.Time.In(loc)
			return !t.Before(start12) && !t.After(now)
		},
	)

	report := &SailingReport{
		Station:    station,
		ReportTime: now,
		Latest:     makeWindObservation(latest, now, loc),
		Latest10:   makeWindObservationList(latest10, now, loc),
		Last12Hours: calculateWindStats(
			last12,
			loc,
		),
	}

	for daysAgo := 1; daysAgo <= 2; daysAgo++ {
		d := now.AddDate(0, 0, -daysAgo)

		start := time.Date(
			d.Year(),
			d.Month(),
			d.Day(),
			12, 0, 0, 0,
			loc,
		)

		end := time.Date(
			d.Year(),
			d.Month(),
			d.Day(),
			17, 0, 0, 0,
			loc,
		)

		period := filterObservations(
			observations,
			func(o Observation) bool {
				t := o.Time.In(loc)
				return !t.Before(start) && t.Before(end)
			},
		)

		report.Afternoon = append(
			report.Afternoon,
			PeriodReport{
				Label: fmt.Sprintf("%d days ago", daysAgo),
				Date:  d,
				Stats: calculateWindStats(period, loc),
			},
		)
	}

	return report
}

func buildHistoricalWindReport(
	station string,
	observations []Observation,
	value string,
	loc *time.Location,
) (*SailingReport, error) {
	target, err := parseHistoricalTime(value, loc)
	if err != nil {
		return nil, fmt.Errorf(
			"invalid date/time %q; use YYYY-MM-DD HH:MM or YYYY-MM-DDTHH:MM",
			value,
		)
	}

	start := target.Add(-30 * time.Minute)
	end := target.Add(30 * time.Minute)

	window := filterObservations(
		observations,
		func(o Observation) bool {
			t := o.Time.In(loc)
			return !t.Before(start) && !t.After(end)
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

	closest := findClosest(window, target)

	historical := &HistoricalReport{
		Requested:   target,
		WindowStart: start,
		WindowEnd:   end,
		Closest:     makeWindObservation(closest, target, loc),
		Stats:       calculateWindStats(window, loc),
	}

	for _, o := range window {
		historical.Observations = append(
			historical.Observations,
			*makeWindObservation(o, target, loc),
		)
	}

	return &SailingReport{
		Station:    station,
		ReportTime: time.Now().In(loc),
		Historical: historical,
	}, nil
}

func writeWindSummaryText(
	w io.Writer,
	report *SailingReport,
	loc *time.Location,
) {
	if report.Latest == nil {
		fmt.Fprintln(w, "No current wind observation is available.")
		return
	}

	fmt.Printf("")

	latest := report.Latest

	fmt.Fprintf(
		w,
		"Currently %s %.1f kt, gusting %.1f kt.\n",
		latest.Direction,
		latest.WindKT,
		latest.GustKT,
	)

	if len(report.Latest10) > 1 {
		minWind, maxWind := report.Latest10[0].WindKT, report.Latest10[0].WindKT
		minGust, maxGust := report.Latest10[0].GustKT, report.Latest10[0].GustKT

		for _, o := range report.Latest10[1:] {
			if o.WindKT > 0 && o.WindKT < minWind {
				minWind = o.WindKT
			}
			if o.WindKT > maxWind {
				maxWind = o.WindKT
			}
			if o.GustKT > 0 && o.GustKT < minGust {
				minGust = o.GustKT
			}
			if o.GustKT > maxGust {
				maxGust = o.GustKT
			}
		}

		fmt.Fprintf(
			w,
			"Recent observations have been %.0f–%.0f kt, with gusts around %.0f–%.0f kt.\n",
			minWind,
			maxWind,
			minGust,
			maxGust,
		)
	}

	if report.Last12Hours != nil && report.Last12Hours.Trend != "" {
		fmt.Fprintf(
			w,
			"Longer trend: %s.\n",
			report.Last12Hours.Trend,
		)
	}

	age := report.ReportTime.Sub(latest.Time)
	if age >= 60*time.Minute {
		fmt.Fprintf(
			w,
			"Warning: the latest wind observation is %s old.\n",
			formatAge(age),
		)
	}
}

func writeWindDetailsText(
	w io.Writer,
	report *SailingReport,
	loc *time.Location,
) {
	if report.Latest != nil {
		fmt.Fprintf(w, "LATEST %s OBSERVATION\n", report.Station)
		fmt.Fprintln(w, "--------------------------------")
		printWindObservation(w, report.Latest, loc, report.ReportTime)
	}

	if len(report.Latest10) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "LATEST 10 OBSERVATIONS")
		fmt.Fprintln(w, "--------------------------------")
		fmt.Fprintf(w, "%-9s %-3s %6s  %s\n", "Time", "Dir", "Wind", "Gust")

		for _, o := range report.Latest10 {
			fmt.Fprintf(
				w,
				"%-9s %-3s %5.1f kt  %5.1f kt\n",
				o.Time.In(loc).Format("3:04 PM"),
				o.Direction,
				o.WindKT,
				o.GustKT,
			)
		}
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "LAST 12 HOURS")
	fmt.Fprintln(w, "--------------------------------")
	printWindStatsText(w, report.Last12Hours, loc)

	for _, period := range report.Afternoon {
		fmt.Fprintln(w)
		fmt.Fprintf(
			w,
			"%s — %s — 12 PM–5 PM\n",
			strings.ToUpper(period.Label),
			period.Date.In(loc).Format("Mon Jan 2, 2006"),
		)
		fmt.Fprintln(w, "--------------------------------")
		printWindStatsText(w, period.Stats, loc)
	}
}

func writeHistoricalWindText(
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
	fmt.Fprintln(w, "=================================")

	fmt.Fprintf(
		w,
		"Requested:   %s\n",
		h.Requested.In(loc).Format("Mon Jan 2, 2006 3:04 PM MST"),
	)
	fmt.Fprintf(
		w,
		"Window:      %s – %s\n\n",
		h.WindowStart.In(loc).Format("3:04 PM"),
		h.WindowEnd.In(loc).Format("3:04 PM"),
	)

	fmt.Fprintln(w, "CLOSEST OBSERVATION")
	fmt.Fprintln(w, "--------------------------------")
	printWindObservation(w, h.Closest, loc, h.Requested)

	fmt.Fprintln(w)
	fmt.Fprintln(w, "±30 MINUTE WINDOW")
	fmt.Fprintln(w, "--------------------------------")
	printWindStatsText(w, h.Stats, loc)

	fmt.Fprintln(w)
	fmt.Fprintln(w, "OBSERVATIONS")
	fmt.Fprintln(w, "--------------------------------")

	for _, o := range h.Observations {
		fmt.Fprintf(
			w,
			"%s  %-3s %4.1f kt  gust %4.1f kt\n",
			o.Time.In(loc).Format("3:04 PM"),
			o.Direction,
			o.WindKT,
			o.GustKT,
		)
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
		o.Time.In(loc).Format("3:04:05 PM MST"),
	)
	fmt.Fprintf(
		w,
		"UTC:         %s\n",
		o.Time.UTC().Format("2006-01-02 15:04:05 UTC"),
	)
	fmt.Fprintf(
		w,
		"Age:         %s\n",
		formatAge(absDuration(reference.Sub(o.Time))),
	)

	if o.Direction != "" {
		fmt.Fprintf(
			w,
			"Wind:        %s %.1f kt\n",
			o.Direction,
			o.WindKT,
		)
	} else {
		fmt.Fprintln(w, "Wind:        missing")
	}

	if o.GustKT != 0 {
		fmt.Fprintf(w, "Gust:        %.1f kt\n", o.GustKT)
	} else {
		fmt.Fprintln(w, "Gust:        missing")
	}
}

func printWindStatsText(
	w io.Writer,
	stats *WindStats,
	loc *time.Location,
) {
	if stats == nil {
		fmt.Fprintln(w, "No observations.")
		return
	}

	fmt.Fprintf(w, "Observations: %d\n", stats.Observations)

	if stats.AverageWind != 0 {
		fmt.Fprintf(w, "Average wind: %.1f kt\n", stats.AverageWind)
	}
	if !stats.MaxWindTime.IsZero() {
		fmt.Fprintf(
			w,
			"Maximum wind: %.1f kt at %s\n",
			stats.MaxWind,
			stats.MaxWindTime.In(loc).Format("3:04 PM"),
		)
	}
	if !stats.MaxGustTime.IsZero() {
		fmt.Fprintf(
			w,
			"Maximum gust: %.1f kt at %s\n",
			stats.MaxGust,
			stats.MaxGustTime.In(loc).Format("3:04 PM"),
		)
	}
	if stats.Trend != "" {
		fmt.Fprintf(w, "Trend:         %s\n", stats.Trend)
	}
}

func calculateWindStats(
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
			windKT := o.WindMS * msToKnots
			windSum += windKT
			windCount++

			if !haveWindMax || windKT > windMax {
				windMax = windKT
				windMaxTime = o.Time
				haveWindMax = true
			}
		}

		if o.HasGust {
			gustKT := o.GustMS * msToKnots
			if !haveGustMax || gustKT > gustMax {
				gustMax = gustKT
				gustMaxTime = o.Time
				haveGustMax = true
			}
		}
	}

	stats := &WindStats{Observations: len(observations)}

	if windCount > 0 {
		stats.AverageWind = round1(
			windSum / float64(windCount),
		)
		stats.MaxWind = round1(windMax)
		stats.MaxWindTime = windMaxTime.In(loc)
	}

	if haveGustMax {
		stats.MaxGust = round1(gustMax)
		stats.MaxGustTime = gustMaxTime.In(loc)
	}

	stats.Trend = calculateWindTrend(observations)
	return stats
}

func calculateWindTrend(observations []Observation) string {
	var first *Observation
	var last *Observation

	for i := range observations {
		o := observations[i]

		if !o.HasWind {
			continue
		}

		if first == nil || o.Time.Before(first.Time) {
			copy := o
			first = &copy
		}
		if last == nil || o.Time.After(last.Time) {
			copy := o
			last = &copy
		}
	}

	if first == nil || last == nil || first.Time.Equal(last.Time) {
		return ""
	}

	change := (last.WindMS - first.WindMS) * msToKnots

	switch {
	case change > 2:
		return fmt.Sprintf("increasing (+%.1f kt)", change)
	case change < -2:
		return fmt.Sprintf("decreasing (%.1f kt)", change)
	default:
		return fmt.Sprintf("roughly steady (%+.1f kt)", change)
	}
}

func makeWindObservation(
	o Observation,
	reference time.Time,
	loc *time.Location,
) *WindObservation {
	result := &WindObservation{
		Time:       o.Time.In(loc),
		AgeMinutes: int(absDuration(reference.Sub(o.Time)).Minutes()),
	}

	if o.HasDir {
		result.Direction = compassDirection(o.Direction)
	}
	if o.HasWind {
		result.WindKT = round1(o.WindMS * msToKnots)
	}
	if o.HasGust {
		result.GustKT = round1(o.GustMS * msToKnots)
	}

	return result
}

func makeWindObservationList(
	observations []Observation,
	reference time.Time,
	loc *time.Location,
) []WindObservation {
	result := make([]WindObservation, 0, len(observations))

	for _, o := range observations {
		result = append(
			result,
			*makeWindObservation(o, reference, loc),
		)
	}

	return result
}

func findLatestN(
	observations []Observation,
	n int,
) []Observation {
	if n <= 0 || len(observations) == 0 {
		return nil
	}

	sorted := append([]Observation(nil), observations...)

	sort.Slice(
		sorted,
		func(i, j int) bool {
			return sorted[i].Time.After(sorted[j].Time)
		},
	)

	if n > len(sorted) {
		n = len(sorted)
	}

	return sorted[:n]
}

func findLatest(observations []Observation) Observation {
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
		if absDuration(o.Time.Sub(target)) <
			absDuration(closest.Time.Sub(target)) {
			closest = o
		}
	}

	return closest
}

func filterObservations(
	observations []Observation,
	predicate func(Observation) bool,
) []Observation {
	var result []Observation

	for _, o := range observations {
		if predicate(o) {
			result = append(result, o)
		}
	}

	return result
}

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
		if t, err := time.ParseInLocation(format, value, loc); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("invalid date/time")
}

func validStationID(station string) bool {
	if len(station) < 1 || len(station) > 8 {
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

func parseOptionalFloat(s string) (float64, bool) {
	if strings.EqualFold(s, "MM") {
		return 0, false
	}

	value, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}

	return value, true
}

func compassDirection(degrees float64) string {
	directions := []string{
		"N", "NNE", "NE", "ENE",
		"E", "ESE", "SE", "SSE",
		"S", "SSW", "SW", "WSW",
		"W", "WNW", "NW", "NNW",
	}

	degrees = float64(int(degrees) % 360)
	if degrees < 0 {
		degrees += 360
	}

	index := int((degrees+11.25)/22.5) % 16
	return directions[index]
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

func formatAge(d time.Duration) string {
	if d < 0 {
		return "future timestamp"
	}

	minutes := int(d.Minutes())

	if minutes >= 60 {
		return fmt.Sprintf(
			"%dh %dm",
			minutes/60,
			minutes%60,
		)
	}

	return fmt.Sprintf("%dm", minutes)
}

func round1(value float64) float64 {
	return float64(int(value*10+0.5)) / 10
}
