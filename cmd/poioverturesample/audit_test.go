package main

import (
 "bytes"
 "encoding/csv"
 "strings"
 "testing"
)
func TestAuditRows(t *testing.T){
 var b bytes.Buffer;w:=csv.NewWriter(&b)
 _=w.Write(strings.Split("id,name,basic_category,primary_category,hierarchy,operating_status,confidence,wkb,sources",","))
 for _,r:=range [][]string{
 {"a","Coffee A","cafe","cafe","NULL","open","0.9",wkb(38.02,-121.9),"NULL"},
 {"b","  coffee a ","coffee_shop","coffee_shop","NULL","NULL","0.3",wkb(38.0201,-121.9),"NULL"},
 {"c","Coffee A","cafe","cafe","NULL","open","0.7",wkb(38.04,-121.9),"NULL"},
 {"d","Shop","convenience_store","convenience_store","NULL","open","0.9",wkb(38.02,-121.9),"NULL"},
 {"e","Coffee B","cafe","cafe","NULL","permanently_closed","0.9",wkb(38.02,-121.9),"NULL"},
 {"f","Coffee C","cafe","cafe","NULL","NULL","NULL",wkb(38.02,-121.9),"NULL"},
 }{_=w.Write(r)};w.Flush()
 rows,_,e:=normalize(bytes.NewReader(b.Bytes()),"delta");if e!=nil{t.Fatal(e)}
 got,e:=auditRows(bytes.NewReader(b.Bytes()),rows,"delta",false);if e!=nil{t.Fatal(e)}
 a:=got.Categories["coffee"]
 if got.Accepted!=4||a.Rows!=4||a.StatusMissing!=2||a.ConfidenceMissing!=1||a.ConfidenceBelow05!=1||a.RetainedAt05!=2||a.RetainedAt08!=1||got.NearbySameNamePairs!=1||got.NearbySameNameRecords!=2||got.ExcludedReviewTokens["convenience_store"]!=1||len(got.Review)!=0{t.Fatalf("%+v %+v",got,a)}
 local,e:=auditRows(bytes.NewReader(b.Bytes()),rows,"delta",true);if e!=nil||len(local.Review)!=4{t.Fatalf("%+v %v",local,e)}
}
