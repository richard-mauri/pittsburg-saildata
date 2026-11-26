package main

import (
 "crypto/aes"
 "crypto/cipher"
 "crypto/rand"
 "encoding/base64"
 "errors"
 "io"
 "net"
 "net/http"
 "strconv"
 "strings"
 "time"
)

// SailingHTTPAuth protects report, weather data and API endpoints in the
// production listener. A browser receives an encrypted, HttpOnly first-party
// handoff cookie; the bearer inside is reverified with Homepage on every
// protected request, not trusted as a cached identity.
type SailingHTTPAuth struct {
 gate *SailingSuiteGate
 seal cipher.AEAD
}

const sailingSessionCookie = "__Host-sailing-session"
const sailingLocalSessionCookie = "sailing-local-session"
const sailingSessionLifetime = 8*time.Hour

func NewSailingHTTPAuth(gate *SailingSuiteGate) (*SailingHTTPAuth,error) {
 if gate == nil {return nil,errors.New("Sailing suite verifier required")}
 key:=make([]byte,32)
 if _,err:=rand.Read(key);err!=nil{return nil,err}
 block,err:=aes.NewCipher(key)
 if err!=nil{return nil,err}
 seal,err:=cipher.NewGCM(block)
 if err!=nil{return nil,err}
 return &SailingHTTPAuth{gate:gate,seal:seal},nil
}

func sailingLoopbackHost(hostport string) bool {
 host,_,err:=net.SplitHostPort(hostport)
 if err!=nil{host=hostport}
 return host=="localhost"||host=="127.0.0.1"||host=="::1"||host=="[::1]"
}

func sailingCookieName(r *http.Request) string {
 if sailingLoopbackHost(r.Host) {return sailingLocalSessionCookie}
 return sailingSessionCookie
}

func (a *SailingHTTPAuth) issueCookie(w http.ResponseWriter,r *http.Request,bearer string) error {
 if a==nil||a.seal==nil||!sailingSuiteBearer.MatchString("Bearer "+bearer){return errors.New("invalid session")}
 nonce:=make([]byte,a.seal.NonceSize())
 if _,err:=rand.Read(nonce);err!=nil{return err}
 expires:=time.Now().Add(sailingSessionLifetime)
 plaintext:=[]byte(strconv.FormatInt(expires.Unix(),10)+":"+bearer)
 ciphertext:=a.seal.Seal(nonce,nonce,plaintext,nil)
 http.SetCookie(w,&http.Cookie{
  Name:sailingCookieName(r),Value:base64.RawURLEncoding.EncodeToString(ciphertext),
  Path:"/",MaxAge:int(sailingSessionLifetime.Seconds()),Expires:expires,
  HttpOnly:true,Secure:!sailingLoopbackHost(r.Host),SameSite:http.SameSiteLaxMode,
 })
 return nil
}

func (a *SailingHTTPAuth) sessionBearer(r *http.Request) string {
 if a==nil||a.seal==nil{return ""}
 cookie,err:=r.Cookie(sailingCookieName(r))
 if err!=nil||len(cookie.Value)>512{return ""}
 data,err:=base64.RawURLEncoding.DecodeString(cookie.Value)
 if err!=nil||len(data)<=a.seal.NonceSize(){return ""}
 nonce:=data[:a.seal.NonceSize()]
 plain,err:=a.seal.Open(nil,nonce,data[a.seal.NonceSize():],nil)
 if err!=nil{return ""}
 fields:=strings.SplitN(string(plain),":",2)
 if len(fields)!=2{return ""}
 expiry,err:=strconv.ParseInt(fields[0],10,64)
 if err!=nil||time.Now().Unix()>=expiry{return ""}
 if !sailingSuiteBearer.MatchString("Bearer "+fields[1]){return ""}
 return fields[1]
}

func sailingBootstrapRoute(r *http.Request) bool {
 if r.Method!=http.MethodGet||r.Header.Get("X-Requested-With")!=""{return false}
 if dest:=r.Header.Get("Sec-Fetch-Dest");dest!=""&&dest!="document"{return false}
 switch r.URL.Path {
 case "/","/planning":return true
 case "/report":
  format:=strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
  return format==""||format=="html"
 }
 return false
}

func sailingPublicRoute(path string) bool {
 switch path {
 case "/health","/welcome","/activity.js","/assets/hero.jpg","/assets/tanner.jpg","/favicon.ico","/favicon.svg":
  return true
 }
 return false
}

// This page deliberately contains no report, station, token or user data. It
// handles Homepage's fragment handoff before any protected report is served.
// Existing account.js subsequently reads the same tab-scoped bearer for the
// personal/Guest preference flow. No token is placed in a URL or log.
const sailingEntryHTML = `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="color-scheme" content="light dark"><title>SailingGo · Sign in</title><style>:root{color-scheme:light dark;--bg:light-dark(#f5f5f0,#0e141a);--panel:light-dark(#ffffff,#19232b);--ink:light-dark(#182631,#eef2f4);--muted:light-dark(#60707b,#a5b3bb);--accent:light-dark(#126877,#69c6d3)}html{min-height:100%;background:var(--bg)}body{box-sizing:border-box;min-height:100vh;margin:0;padding:clamp(28px,9vh,96px) 20px;background:var(--bg);color:var(--ink);font:16px system-ui,sans-serif}.entry{max-width:38rem;margin:0 auto;padding:28px;border:1px solid var(--muted);border-radius:16px;background:var(--panel)}h1{font-size:1.5rem;line-height:1.3}p{line-height:1.55}a{color:var(--accent)}a:focus-visible{outline:2px solid var(--accent);outline-offset:3px}</style></head><body><main class="entry"><h1>Mauri's Weather &amp; Water Conditions</h1><p id="message" role="status">Checking Homepage access…</p><p><a href="https://home.themauris.org/">Sign in or continue as Guest at Homepage</a></p><script>
(async function(){
 "use strict";
 const handoffKey="sailing.homepage.handoff.v1";
 const fragment=new URLSearchParams(location.hash.slice(1));
 const local=fragment.get("suite_mode")==="local";
 const received=fragment.get("suite_token");
 if(location.hash) history.replaceState(history.state,"",location.pathname+location.search);
 const message=document.getElementById("message");
 if(local){message.textContent="Homepage sign-in or Guest access required.";return;}
 let token=/^[a-f0-9]{64}$/.test(received||"")?received:"";
 if(token){try{sessionStorage.setItem(handoffKey,token);}catch(_){}}
 if(!token){try{token=sessionStorage.getItem(handoffKey)||"";}catch(_){}}
 if(!/^[a-f0-9]{64}$/.test(token)){
  message.textContent="Open SailingGo from Homepage after signing in or selecting Guest.";
  return;
 }
 try{
  const result=await fetch("/suite/session",{
   method:"POST",credentials:"same-origin",cache:"no-store",
   headers:{"Authorization":"Bearer "+token}
  });
  if(!result.ok)throw Error("verification failed");
  location.replace(location.pathname+location.search);
 }catch(_){
  try{sessionStorage.removeItem(handoffKey);}catch(_){}
  message.textContent="Homepage access was not verified. Return to Homepage to sign in again.";
 }
})();
</script></main></body></html>`

func sailingEntry(w http.ResponseWriter) {
 w.Header().Set("Cache-Control","no-store")
 w.Header().Set("Content-Type","text/html; charset=utf-8")
 w.Header().Set("X-Content-Type-Options","nosniff")
 w.Header().Set("Referrer-Policy","no-referrer")
 w.Header().Set("Content-Security-Policy","default-src 'none'; script-src 'unsafe-inline'; connect-src 'self'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
 _,_=io.WriteString(w,sailingEntryHTML)
}

func sailingSameOrigin(r *http.Request) bool {
 origin:=r.Header.Get("Origin")
 if origin==""||origin=="null"{return false}
 if sailingLoopbackHost(r.Host){return origin=="http://"+r.Host||origin=="https://"+r.Host}
 return r.Host=="weather.themauris.org" && origin==sailingSuiteOrigin
}

// ProtectWeather enforces the server boundary on every nonpublic route.
// Unknown routes (including new /api/* paths) are protected by default.
func (a *SailingHTTPAuth) ProtectWeather(next http.Handler) http.Handler {
 return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if a==nil||a.gate==nil||next==nil{
   http.Error(w,"Sailing authentication unavailable",http.StatusServiceUnavailable)
   return
  }
  if r.URL.Path=="/suite/session" {
   w.Header().Set("Cache-Control","no-store")
   if r.Method!=http.MethodPost{w.Header().Set("Allow","POST");http.Error(w,"method not allowed",405);return}
   if !sailingSameOrigin(r){http.Error(w,"invalid session origin",http.StatusForbidden);return}
   if !sailingSuiteBearer.MatchString(r.Header.Get("Authorization")){http.Error(w,"Homepage sign-in required",http.StatusUnauthorized);return}
   identity,err:=a.gate.verify(r.Context(),r)
   if err!=nil{http.Error(w,"Homepage verification unavailable",http.StatusServiceUnavailable);return}
   if !identity.Verified{http.Error(w,"Homepage sign-in required",http.StatusUnauthorized);return}
   if err:=a.issueCookie(w,r,strings.TrimPrefix(r.Header.Get("Authorization"),"Bearer "));err!=nil{
    http.Error(w,"session unavailable",http.StatusServiceUnavailable);return
   }
   w.WriteHeader(http.StatusNoContent)
   return
  }
  if sailingPublicRoute(r.URL.Path) || (r.URL.EscapedPath() == r.URL.Path && streetTileRoute(r.URL.Path)) {
   next.ServeHTTP(w,r)
   return
  }
  // Explicit Authorization is useful for CLI requests, while browser
  // navigations and same-origin fetches use the sealed HttpOnly cookie.
  credential:=r.Header.Get("Authorization")
  fromCookie:=false
  if credential=="" {
   if bearer:=a.sessionBearer(r);bearer!=""{
    credential="Bearer "+bearer
    fromCookie=true
   }
  }
  // Cookie authentication is only for same-origin browser traffic. A bearer
  // supplied explicitly by a CLI client is separately verified by Homepage.
  if fromCookie && r.Method!=http.MethodGet && r.Method!=http.MethodHead && !sailingSameOrigin(r) {
   http.Error(w,"invalid write origin",http.StatusForbidden)
   return
  }
  if credential=="" && sailingBootstrapRoute(r){
   sailingEntry(w)
   return
  }
  w.Header().Set("Cache-Control","no-store")
  // No role/user header, browser label or raw cookie is trusted as identity.
  verified:=r.Clone(r.Context())
  verified.Header=r.Header.Clone()
  verified.Header.Set("Authorization",credential)
  identity,err:=a.gate.verify(verified.Context(),verified)
  if err!=nil{http.Error(w,"Homepage verification unavailable",http.StatusServiceUnavailable);return}
  if !identity.Verified{
   if sailingBootstrapRoute(r) && r.Header.Get("Authorization")==""{
    sailingEntry(w)
    return
   }
   http.Error(w,"Homepage sign-in or Guest access required",http.StatusUnauthorized)
   return
  }
  next.ServeHTTP(w,r)
 })
}

