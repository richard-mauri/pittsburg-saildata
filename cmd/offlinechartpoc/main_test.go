package main

import (
 "os"
 "path/filepath"
 "testing"
)

func TestPlanValidAndMissing(t *testing.T) {
 root:=t.TempDir()
 b:=[4]float64{-121.991,38.021,-121.989,38.023}
 m,err:=plan(root,14,b)
 if err!=nil {t.Fatal(err)}
 if m.Missing<1||m.Complete||len(m.Tiles)!=0 {t.Fatalf("expected missing tiles: %+v",m)}
 p:=filepath.Join(root,"14")
 if _,err:=os.Stat(p);!os.IsNotExist(err){t.Fatal("planner must not write into cache")}
}
func TestRejectOversizedAndInvalid(t *testing.T) {
 root:=t.TempDir()
 for _,tc:=range []struct{z int;b [4]float64}{
  {12,[4]float64{-122,38,-121.9,38.1}},
  {14,[4]float64{-180,-80,180,80}},
  {14,[4]float64{-121,38,-122,39}},
 } {
  if _,err:=plan(root,tc.z,tc.b);err==nil{t.Fatalf("expected reject %+v",tc)}
 }
}
