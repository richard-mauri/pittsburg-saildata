package main

import (
	"embed"
	"html/template"
)

// Browser page templates are embedded at build time so local and deployed
// binaries retain their original self-contained behavior.
//
//go:embed web/templates/*.html
var sailingUIFiles embed.FS

func readSailingUITemplate(path string) string {
	data, err := sailingUIFiles.ReadFile(path)
	if err != nil {
		panic(err)
	}
	return string(data)
}

var welcomeHTMLTemplate = template.Must(template.New("welcome").Parse(readSailingUITemplate("web/templates/welcome.html")))
var sailingStationsHTMLTemplate = template.Must(template.New("stations").Parse(readSailingUITemplate("web/templates/stations.html")))
var sailingDetailsHTMLTemplate = template.Must(template.New("details").Parse(readSailingUITemplate("web/templates/details.html")))
var sailingHTMLTemplate = template.Must(template.New("sailing").Parse(readSailingUITemplate("web/templates/sailing.html")))
