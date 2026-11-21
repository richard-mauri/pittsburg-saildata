package main

import (
 "context"
 "encoding/json"
 "io"
 "net/http"
 "regexp"
 "strings"
 "time"

 suiteauth "github.com/richard-mauri/homepage/shared/auth"
)

const sailingHomepageProfileURL = "https://home.themauris.org/api/session/profile"
const sailingSuiteOrigin = "https://weather.themauris.org"

var sailingSuiteBearer = regexp.MustCompile(`^Bearer [a-f0-9]{64}$`)

// SailingSuiteGate is the server-side verifier for the forthcoming authenticated
// weather/report route migration. Creating it does not install middleware in
// the production listener: routes and browser fetches must migrate together.
type SailingSuiteGate struct {
 ProfileURL string
 Client *http.Client
}

func NewSailingSuiteGate() *SailingSuiteGate {
 return &SailingSuiteGate{
  ProfileURL:sailingHomepageProfileURL,
  Client:&http.Client{
   Timeout:4*time.Second,
   CheckRedirect:func(*http.Request,[]*http.Request)error{return http.ErrUseLastResponse},
  },
 }
}

func (g *SailingSuiteGate) verify(ctx context.Context,r *http.Request)(suiteauth.Identity,error){
 header:=r.Header.Get("Authorization")
 if !sailingSuiteBearer.MatchString(header){return suiteauth.Identity{},nil}
 if g==nil||g.Client==nil||g.ProfileURL==""{return suiteauth.Identity{},suiteauth.ErrVerificationUnavailable}
 req,err:=http.NewRequestWithContext(ctx,http.MethodGet,g.ProfileURL,nil)
 if err!=nil{return suiteauth.Identity{},err}
 req.Header.Set("Authorization",header)
 req.Header.Set("Origin",sailingSuiteOrigin)
 req.Header.Set("Accept","application/json")
 resp,err:=g.Client.Do(req)
 if err!=nil{return suiteauth.Identity{},err}
 defer resp.Body.Close()
 if resp.StatusCode==http.StatusUnauthorized||resp.StatusCode==http.StatusForbidden{return suiteauth.Identity{},nil}
 if resp.StatusCode!=http.StatusOK{return suiteauth.Identity{},suiteauth.ErrVerificationUnavailable}
 var profile struct{
  User string `json:"user"`
  Role suiteauth.Role `json:"role"`
  ExpiresAt string `json:"expiresAt"`
 }
 decoder:=json.NewDecoder(io.LimitReader(resp.Body,8192))
 if err:=decoder.Decode(&profile);err!=nil{return suiteauth.Identity{},err}
 expiry,err:=time.Parse(time.RFC3339,profile.ExpiresAt)
 if err!=nil||!time.Now().Before(expiry){return suiteauth.Identity{},nil}
 identity:=suiteauth.Identity{User:strings.TrimSpace(profile.User),Role:profile.Role,Verified:true}
 if !suiteauth.Allowed(identity,suiteauth.UseApplication){return suiteauth.Identity{},nil}
 return identity,nil
}

// ProtectAPI is ready for the follow-on coordinated browser/API migration.
// Once installed, every API request (including future unknown /api/ routes)
// must be independently verified by Homepage, without a local-mode bypass.
func (g *SailingSuiteGate) ProtectAPI(next http.Handler)http.Handler {
 guarded:=suiteauth.Guard(g.verify,suiteauth.UseApplication,next)
 return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if r.URL.Path=="/api"||strings.HasPrefix(r.URL.Path,"/api/"){
   guarded.ServeHTTP(w,r)
   return
  }
  next.ServeHTTP(w,r)
 })
}
