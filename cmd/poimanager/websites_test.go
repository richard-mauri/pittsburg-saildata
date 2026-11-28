package main
import("testing";"strings")
func TestWebsiteExtraction(t *testing.T){
 if !strings.Contains(websiteSQL(37,38),"CAST(websites AS JSON) AS websites FROM read_parquet") {t.Fatal("website query")}
 a:=map[string]bool{"poi-ovt-a":true,"poi-ovt-b":true};s:=map[string]string{}
 raw:="id,websites\na,\"[\"\"https://vendor.com\"\"]\"\nb,\nc,\"[\"\"https://other.com\"\"]\"\n"
 if e:=consumeWebsites(strings.NewReader(raw),a,s);e!=nil||len(s)!=2||s["poi-ovt-a"]!="https://vendor.com"||s["poi-ovt-b"]!=""{t.Fatal(s,e)}
 if e:=consumeWebsites(strings.NewReader("id,websites\na,\n"),a,s);e==nil{t.Fatal("conflict accepted")}
 if e:=consumeWebsites(strings.NewReader("id,name\na,x\n"),a,s);e==nil{t.Fatal("missing field accepted")}
}
