package main

import (
	"strings"
	"testing"
)

func TestSailingAccountAutoSyncEmbeddedOnlyOnDashboard(t *testing.T) {
	dashboard := readSailingUITemplate("web/templates/sailing.html")
	required := []string{
		"sailing.homepage.handoff.v1",
		"/api/session/profile",
		"/api/storage/sailing",
		"suite_mode",
		"suite_token",
		"Signed in as ",
		" (Homepage)",
		"document.addEventListener(\"change\"",
		"const existing = await record()",
		"applySettings(desired,user)",
		"response.status === 409",
		"revision:revision",
		"sailing.preferences.pending-account.v2",
		"sailing.preferences.browser.v2",
	}
	for _, needle := range required {
		if !strings.Contains(dashboard, needle) {
			t.Errorf("SailingGo auto-sync client missing %q", needle)
		}
	}
	for _, forbidden := range []string{"Save to account", "Load from account", "sailing.homepage.autosave.v1", "window.confirm("} {
		if strings.Contains(dashboard, forbidden) {
			t.Errorf("obsolete manual account sync remains: %q", forbidden)
		}
	}
	for _, path := range []string{"web/templates/welcome.html", "web/templates/details.html", "web/templates/stations.html"} {
		if strings.Contains(readSailingUITemplate(path), "sailing.homepage.handoff.v1") {
			t.Fatalf("Account script should not be injected into %s", path)
		}
	}
}

func TestUnitChangesUseInPlaceRefresh(t *testing.T) {
	page := readSailingUITemplate("web/templates/sailing.html")
	for _, required := range []string{
		"sailing:unit-change",
		"sailingRefreshUnits",
		"X-Requested-With",
		"new DOMParser()",
		"sailing-units-pending",
		"sailing:units-refreshed",
		"windBarbObservationCache = Object.create(null)",
	} {
		if !strings.Contains(page, required) {
			t.Errorf("unit refresh contract missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"window.location.href = target.toString();",
	} {
		if strings.Contains(page, forbidden) {
			t.Errorf("unit selector must not navigate: %q", forbidden)
		}
	}
	client := readSailingUITemplate("web/templates/sailing.html")
	if !strings.Contains(client, "await window.sailingRefreshUnits(next,previous") {
		t.Fatal("account restore must refresh units in place")
	}
}
