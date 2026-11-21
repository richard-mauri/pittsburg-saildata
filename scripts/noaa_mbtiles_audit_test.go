package main

import (
 "testing"
)

func TestXYZtoTMS(t *testing.T){
 x,y,tms,err:=xyzToTMS(0,0,1)
 if err!=nil || x!=1 || y!=1 || tms!=0 {t.Fatalf("unexpected XYZ->TMS result: %d %d %d %v",x,y,tms,err)}
 _,y,tms,err=xyzToTMS(-121.885,38.028,9)
 if err!=nil || y+tms!=511 {t.Fatalf("invalid TMS inversion %d %d: %v",y,tms,err)}
 if _,_,_,err=xyzToTMS(-122,100,3);err==nil{t.Fatal("expected invalid lat")}
 if _,_,_,err=xyzToTMS(0,0,23);err==nil{t.Fatal("expected invalid zoom")}
}

func TestImageFormats(t *testing.T){
 cases:=map[string]string{
  "89504E470D0A1A0A00":"png",
  "FFD8FFAA":"jpeg",
  "524946460100000057454250":"webp",
  "":"none",
  "010203":"unknown",
 }
 for src,want:=range cases{
  if got:=detectTile(src);got!=want{t.Errorf("detectTile(%q)=%q; want %q",src,got,want)}
 }
}
