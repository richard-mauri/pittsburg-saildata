package main

import (
 "strings"
 "testing"
)

func TestMobileGPSPOCEmbeddedOnlyInMapPage(t *testing.T) {
 page:=readSailingUITemplate("web/templates/sailing.html")
 if !strings.Contains(page, "Show My Position") ||
  !strings.Contains(page, "navigator.geolocation.watchPosition") ||
  !strings.Contains(page, "map.panTo(point") {
  t.Fatal("mobile GPS marker POC not embedded into Leaflet map page")
 }
 if strings.Count(page, "Issue #41: opt-in foreground-only iPhone GPS marker POC.") != 1 {
  t.Fatal("GPS POC injected wrong number of times")
 }
 if strings.Contains(readSailingUITemplate("web/templates/welcome.html"), "Show My Position") {
  t.Fatal("GPS POC must not appear on welcome page")
 }
}
