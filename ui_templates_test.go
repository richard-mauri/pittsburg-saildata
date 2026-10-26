package main

import (
	"strings"
	"testing"
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
			if !strings.Contains(markup, "--panel:") || !strings.Contains(markup, "--accent:") {
\t\t\t\tt.Fatalf("%s missing shared theme", tc.file)
\t\t\t}
\t\t\tif !strings.Contains(strings.ToLower(markup), "<html") {
				t.Fatalf("%s does not contain an HTML page", tc.file)
			}
		})
	}
}
