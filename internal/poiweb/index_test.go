package poiweb
import("testing";"encoding/json";"crypto/sha256";"encoding/hex")
func TestValid(t *testing.T){
 for _,v:=range []string{"https://www.vendor.com/path?q=1","http://vendor.org","https://example.com/a%20b"}{if !Valid(v){t.Fatal(v)}}
 for _,v:=range []string{"javascript:alert(1)","//example.com","https://a@vendor.com","https://127.0.0.1","https://[::1]","https://127.0.0.1.nip.io:8080","https://localhost","https://host.local","https://vendor.com\\evil.org","https://vendor.com/%0a","https://vendor.com/a b"}{if Valid(v){t.Fatal(v)}}
 if Select([]string{"http://vendor.org","https://vendor.org"})!="https://vendor.org"{t.Fatal("HTTPS preference")}
}
func TestDecode(t *testing.T){
 x:=Index{1,BasePayload,BaseRows,[]Entry{{"poi-ovt-a","https://vendor.com"}}}
 raw,_:=json.Marshal(x);raw=append(raw,'\n');sum:=sha256.Sum256(raw);h:=hex.EncodeToString(sum[:])
 m,e:=Decode(raw,h);if e!=nil||m["poi-ovt-a"]!="https://vendor.com"{t.Fatal(m,e)}
 if _,e=Decode(raw,BasePayload);e==nil{t.Fatal("digest accepted")}
 x.Entries=append(x.Entries,x.Entries[0]);raw,_=json.Marshal(x);raw=append(raw,'\n');sum=sha256.Sum256(raw)
 if _,e=Decode(raw,hex.EncodeToString(sum[:]));e==nil{t.Fatal("duplicate accepted")}
}
