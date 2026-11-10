package main

import (
	"embed"
	"html/template"
	"strings"
)

// Browser page templates are embedded at build time so local and deployed
// binaries retain their original self-contained behavior.
//
//go:embed web/templates/*.html web/static/theme.css web/static/account.js
var sailingUIFiles embed.FS

func readSailingUITemplate(path string) string {
	data, err := sailingUIFiles.ReadFile(path)
	if err != nil {
		panic(err)
	}
	theme, err := sailingUIFiles.ReadFile("web/static/theme.css")
	if err != nil {
		panic(err)
	}
	// Each page retains its own structural styles; the shared theme is the
	// final stylesheet and is embedded directly into the rendered HTML.
	page := strings.Replace(string(data), "</style>", "</style><style>"+string(theme)+"</style>", 1)
	// Account behavior is opt-in and applies only to the two report views.
	if path == "web/templates/sailing.html" {
		account, err := sailingUIFiles.ReadFile("web/static/account.js")
		if err != nil {
			panic(err)
		}
		page = strings.Replace(page, "</body>", "<script>"+string(account)+"</script></body>", 1)
	}
	return page
}

var welcomeHTMLTemplate = template.Must(template.New("welcome").Parse(readSailingUITemplate("web/templates/welcome.html")))
var sailingStationsHTMLTemplate = template.Must(template.New("stations").Parse(readSailingUITemplate("web/templates/stations.html")))
var sailingDetailsHTMLTemplate = template.Must(template.New("details").Parse(readSailingUITemplate("web/templates/details.html")))
var sailingHTMLTemplate = template.Must(template.New("sailing").Parse(readSailingUITemplate("web/templates/sailing.html")))
