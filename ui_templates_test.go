package main

import (
	"strings"
	"testing"

	suiteui "github.com/richard-mauri/homepage/shared/ui"
)

func TestEmbeddedPageTemplates(t *testing.T) {
	tests := []struct {
		name string
		file string
	}{
		{"welcome", "web/templates/welcome.html"},
		{"stations", "web/templates/stations.html"},
		{"details", "web/templates/details.html"},
		{"sailing", "web/templates/sailing.html"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			markup := readSailingUITemplate(tc.file)
			if !strings.Contains(markup, string(suiteui.ButtonCSS)) {
				t.Fatalf("%s does not embed the pinned Homepage canonical button stylesheet", tc.file)
			}
			if !strings.Contains(markup, "--panel:") || !strings.Contains(markup, "--accent:") {
				t.Fatalf("%s missing shared theme", tc.file)
			}
			if !strings.Contains(strings.ToLower(markup), "<html") {
				t.Fatalf("%s does not contain an HTML page", tc.file)
			}
		})
	}
}

func TestSailingStandardButtonsUseHomepageCSS(t *testing.T) {
	data, err := sailingUIFiles.ReadFile("web/templates/sailing.html")
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, expected := range []string{
		`class="map-reset ui-button"`,
		`class="map-overlay-clear ui-button"`,
		`class="info-popup-open ui-button"`,
		`class="tide-context-open ui-button"`,
	} {
		if !strings.Contains(source, expected) {
			t.Errorf("missing canonical style on ordinary control: %s", expected)
		}
	}
	for _, special := range []string{
		`class="map-center-action"`,
		`class="map-swell-cache-reload"`,
		`class="info-popup-close"`,
	} {
		if !strings.Contains(source, special) {
			t.Errorf("specialized map control changed: %s", special)
		}
	}
	for _, obsolete := range []string{
		".info-popup-open:hover{background:",
		".tide-context-open:hover{background:",
		".map-overlay-clear:hover{background:",
		".map-reset:disabled{opacity:",
	} {
		if strings.Contains(source, obsolete) {
			t.Errorf("obsolete local ordinary-button appearance retained: %s", obsolete)
		}
	}
}

func TestMapPlaybackTransportsUseSharedThemeTokens(t *testing.T) {
	data, err := sailingUIFiles.ReadFile("web/static/theme.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(data)
	for _, expected := range []string{
		".map-air-temperature-transport,.map-swell-transport,",
		"background:var(--panel);color:var(--text);border-color:var(--line);",
		".map-air-temperature-transport button,.map-air-temperature-transport select,",
		"background:var(--soft);color:var(--text);border-color:var(--line);",
		".map-air-temperature-footer-status,.map-swell-footer-status",
		".map-swell-transport input[type=range] { accent-color:var(--accent); }",
	} {
		if !strings.Contains(css, expected) {
			t.Errorf("playback transport missing shared theme rule: %s", expected)
		}
	}
}

func TestMapResizeHandleUsesSharedTheme(t *testing.T) {
	data, err := sailingUIFiles.ReadFile("web/static/theme.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(data)
	for _, expected := range []string{
		".map-resize-handle { background:var(--panel);color:var(--muted);border-color:var(--line); }",
		"background:var(--muted);box-shadow:0 -6px 0 -1px var(--border);",
		`.map-resize-handle[aria-grabbed="true"] { background:var(--soft); }`,
	} {
		if !strings.Contains(css, expected) {
			t.Errorf("map resize strip missing shared theme rule: %s", expected)
		}
	}
}

func TestMapOverlayHelpIsConcise(t *testing.T) {
	data, err := sailingUIFiles.ReadFile("web/templates/sailing.html")
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, obsolete := range []string{
		`class="map-air-temperature-overlay-note"`,
		`<summary>Map overlay details</summary>`,
	} {
		if strings.Contains(source, obsolete) {
			t.Errorf("obsolete map overlay explanatory text retained: %s", obsolete)
		}
	}
	if !strings.Contains(source, `id="map-sources-modal"`) {
		t.Fatal("map and data source information dialog must remain available")
	}
}

func TestAirTemperatureManualThreeHourFrames(t *testing.T) {
	data, err := sailingUIFiles.ReadFile("web/templates/sailing.html")
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, expected := range []string{
		`id="map-air-temperature-map-hours" type="range" min="0" max="24" step="3"`,
		`aria-label="Previous 3 hours"`,
		`aria-label="Next 3 hours"`,
		"commitAirTemperatureHours(Math.max(0, current - 3))",
		"commitAirTemperatureHours(Math.min(airTemperaturePlaybackRangeHours, current + 3))",
		"fetchAirTemperatureForecastGrid(spec,requested,false)",
		"Air Temperature · Observed · current heat map restored.",
		`if (!validCells) throw new Error("NDFD forecast grid has no usable temperature cells")`,
	} {
		if !strings.Contains(source, expected) {
			t.Errorf("manual 3-hour temperature control missing: %s", expected)
		}
	}
	for _, obsolete := range []string{
		`id="map-air-temperature-map-play"`,
		`id="map-air-temperature-speed"`,
		"airTemperaturePlaybackActive",
		"scheduleAirTemperaturePlaybackAdvance",
		"animateAirTemperatureForecastStep",
		"stopAirTemperaturePlayback",
		"airTemperatureForecastGridBlendAlpha",
	} {
		if strings.Contains(source, obsolete) {
			t.Errorf("obsolete temperature playback logic retained: %s", obsolete)
		}
	}
}
