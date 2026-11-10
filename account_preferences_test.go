package main

import (
    "strings"
    "testing"
)

func TestSailingAccountScriptOnlyOnDashboard(t *testing.T) {
    dashboard := readSailingUITemplate("web/templates/sailing.html")
    if !strings.Contains(dashboard, "sailing.homepage.handoff.v1") {
        t.Fatal("SailingGo dashboard is missing Homepage handoff client")
    }
    if !strings.Contains(dashboard, "/api/storage/sailing") {
        t.Fatal("SailingGo dashboard is missing the account storage endpoint")
    }
    if !strings.Contains(dashboard, "suite_mode") || !strings.Contains(dashboard, "suite_token") {
        t.Fatal("SailingGo must support signed-in and browser-only launch modes")
    }
    if !strings.Contains(dashboard, "revision:revision") || !strings.Contains(dashboard, "response.status === 409") {
        t.Fatal("Account settings must use revisioned writes and conflict detection")
    }
    if !strings.Contains(dashboard, "Save to account") || !strings.Contains(dashboard, "Load from account") {
        t.Fatal("Explicit account sync controls must be available")
    }
    for _, path := range []string{"web/templates/welcome.html", "web/templates/details.html", "web/templates/stations.html"} {
        if strings.Contains(readSailingUITemplate(path), "sailing.homepage.handoff.v1") {
            t.Fatalf("Account script should not be injected into %s", path)
        }
    }
}
