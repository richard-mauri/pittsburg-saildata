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
   if !strings.Contains(markup,`class="suite-home-link" href="https://home.themauris.org/"`){
    t.Fatalf("%s missing Homepage return",path)
   }
   if strings.Contains(markup,`class="suite-home-link" target="_blank"`){
    t.Fatalf("%s Homepage return opens a new tab",path)
   }
  })
 }
}
