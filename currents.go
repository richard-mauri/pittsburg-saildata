package main

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	ndbcStationsURL = "https://www.ndbc.noaa.gov/activestations.xml"

	currentMetadataURL = "https://api.tidesandcurrents.noaa.gov/mdapi/prod/webapi/stations.json?type=currents"

	currentDataURL = "https://api.tidesandcurrents.noaa.gov/api/prod/datagetter"

	noaaCurrentTimeFormat = "2006-01-02 15:04"
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

type CurrentStation struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Lat        float64 `json:"lat"`
	Lon        float64 `json:"lon"`
	DistanceNM float64 `json:"distance_nm"`
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

type CurrentReport struct {
	WindReference  *NDBCStation    `json:"wind_reference,omitempty"`
	CurrentStation *CurrentStation `json:"current_station,omitempty"`
	Start          time.Time       `json:"window_start,omitempty"`
	End            time.Time       `json:"window_end,omitempty"`
	Outlook        []string        `json:"outlook,omitempty"`
	Events         []CurrentEvent  `json:"events,omitempty"`
	Units          string          `json:"units,omitempty"`
	Depth          string          `json:"depth_ft,omitempty"`
	Bin            string          `json:"bin,omitempty"`
	Error          string          `json:"error,omitempty"`
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

func BuildCurrentReport(
	windStationID string,
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

	currentStations, err := fetchCurrentStations()
	if err != nil {
		return nil, err
	}
	if len(currentStations) == 0 {
		return nil, fmt.Errorf("NOAA metadata returned no current stations")
	}

	for i := range currentStations {
		currentStations[i].DistanceNM = distanceNM(
			windStation.Lat,
			windStation.Lon,
			currentStations[i].Lat,
			currentStations[i].Lon,
		)
	}

	sort.Slice(
		currentStations,
		func(i, j int) bool {
			return currentStations[i].DistanceNM <
				currentStations[j].DistanceNM
		},
	)

	currentStation := currentStations[0]

	dateString := date.In(loc).Format("20060102")

	predictions, units, err := fetchCurrentPredictions(
		currentStation.ID,
		dateString,
	)
	if err != nil {
		return nil, err
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

	events := make([]CurrentEvent, 0, len(relevant))
	for _, event := range relevant {
		p := event.Prediction

		currentEvent := CurrentEvent{
			Type: p.Type,
			Time: event.Time,
		}

		switch p.Type {
		case "flood":
			currentEvent.SpeedKT = absFloat(p.Velocity)
			currentEvent.Direction = p.MeanFloodDir
		case "ebb":
			currentEvent.SpeedKT = absFloat(p.Velocity)
			currentEvent.Direction = p.MeanEbbDir
		}

		events = append(events, currentEvent)
	}

	report := &CurrentReport{
		WindReference:  &windStation,
		CurrentStation: &currentStation,
		Start:          start,
		End:            end,
		Outlook:        outlook,
		Events:         events,
		Units:          units,
	}

	if len(relevant) > 0 {
		report.Depth = relevant[0].Prediction.Depth
		report.Bin = relevant[0].Prediction.Bin
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
		fmt.Fprintf(
			w,
			"Current prediction unavailable: %s\n",
			report.Error,
		)
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

	if report.Depth != "" {
		fmt.Fprintf(
			w,
			"Prediction depth: %s ft.\n",
			report.Depth,
		)
	}

	for _, line := range report.Outlook {
		fmt.Fprintln(w, line)
	}

	if len(report.Events) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "CURRENT EVENTS")
		fmt.Fprintln(w, "--------------------------------")

		for _, event := range report.Events {
			switch event.Type {
			case "flood":
				fmt.Fprintf(
					w,
					"%8s  Max flood  %.2f kt → %03d°\n",
					event.Time.Format("3:04 PM"),
					event.SpeedKT,
					event.Direction,
				)
			case "ebb":
				fmt.Fprintf(
					w,
					"%8s  Max ebb    %.2f kt → %03d°\n",
					event.Time.Format("3:04 PM"),
					event.SpeedKT,
					event.Direction,
				)
			case "slack":
				fmt.Fprintf(
					w,
					"%8s  Slack\n",
					event.Time.Format("3:04 PM"),
				)
			}
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

func fetchCurrentStations() ([]CurrentStation, error) {
	client := &http.Client{Timeout: 15 * time.Second}

	resp, err := client.Get(currentMetadataURL)
	if err != nil {
		return nil, fmt.Errorf(
			"NOAA current metadata request failed: %w",
			err,
		)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"NOAA current metadata returned HTTP %d",
			resp.StatusCode,
		)
	}

	var raw map[string]interface{}

	decoder := json.NewDecoder(resp.Body)
	decoder.UseNumber()

	if err := decoder.Decode(&raw); err != nil {
		return nil, err
	}

	rawStations, ok := raw["stations"].([]interface{})
	if !ok {
		return nil, fmt.Errorf(
			"NOAA metadata response did not contain stations",
		)
	}

	var stations []CurrentStation

	for _, item := range rawStations {
		record, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		id := stringValue(record["id"])
		name := stringValue(record["name"])
		lat, latOK := floatValue(record["lat"])
		lon, lonOK := floatValue(record["lng"])

		if id == "" || !latOK || !lonOK {
			continue
		}

		stations = append(
			stations,
			CurrentStation{
				ID:   id,
				Name: name,
				Lat:  lat,
				Lon:  lon,
			},
		)
	}

	return stations, nil
}

func fetchCurrentPredictions(
	station string,
	date string,
) ([]CurrentPrediction, string, error) {
	params := url.Values{}

	params.Set("product", "currents_predictions")
	params.Set("application", "pittsburg-saildata")
	params.Set("begin_date", date)
	params.Set("end_date", date)
	params.Set("station", station)
	params.Set("time_zone", "lst_ldt")
	params.Set("units", "english")
	params.Set("interval", "max_slack")
	params.Set("format", "json")

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

	return data.CurrentPredictions.CP,
		data.CurrentPredictions.Units,
		nil
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
					"The flood peaks around %s at %.1f kt, which is %s.",
					event.Time.Format("3:04 PM"),
					absFloat(p.Velocity),
					currentStrength(
						absFloat(p.Velocity),
					),
				),
			)

		case "ebb":
			lines = append(
				lines,
				fmt.Sprintf(
					"The ebb peaks around %s at %.1f kt, which is %s.",
					event.Time.Format("3:04 PM"),
					absFloat(p.Velocity),
					currentStrength(
						absFloat(p.Velocity),
					),
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
	maxVelocity := 0.0

	for _, event := range events {
		if event.Time.Before(start) ||
			event.Time.After(end) ||
			event.Prediction.Type == "slack" {
			continue
		}

		v := absFloat(event.Prediction.Velocity)
		if v > maxVelocity {
			maxVelocity = v
		}
	}

	switch {
	case maxVelocity == 0:
		return "Current should be very light during the sailing window."
	case maxVelocity < 0.5:
		return "Overall, currents should be relatively mild during the sailing window."
	case maxVelocity < 1.0:
		return "Overall, expect moderate current during the sailing window."
	case maxVelocity < 1.5:
		return "Overall, expect a fairly strong current during part of the sailing window."
	default:
		return "Overall, expect strong current during part of the sailing window."
	}
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
