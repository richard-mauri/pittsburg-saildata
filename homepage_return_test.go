package main

import (
 "strings"
 "testing"
)

func TestAllSailingViewsLinkBackToHomepage(t *testing.T) {
 for _,path:=range []string{
  "web/templates/welcome.html",
  "web/templates/sailing.html",
  "web/templates/stations.html",
  "web/templates/details.html",
 }{
  t.Run(path,func(t *testing.T){
   markup:=readSailingUITemplate(path)
   if !strings.Contains(markup,`class="suite-banner__home suite-type-control" href="https://home.themauris.org/"`){
    t.Fatalf("%s missing shared Homepage return",path)
   }
   if !strings.Contains(markup,".suite-banner__home:hover{text-decoration:underline}") {t.Fatalf("%s shared link hover style missing",path)}
   if strings.Contains(markup,`class="suite-home-link"`) || strings.Contains(markup,`class="suite-banner__home suite-type-control" target="_blank"`){
    t.Fatalf("%s Homepage return opens a new tab",path)
   }
   if strings.Count(markup,`id="suite-auth-status"`)!=1 {t.Fatalf("%s must mount exactly one shared banner",path)}
   if !strings.Contains(markup,`suite-content`) || !strings.Contains(markup,`.suite-banner,.suite-content{box-sizing:border-box;width:`) {t.Fatalf("%s missing shared content alignment",path)}
  })
 }
}
