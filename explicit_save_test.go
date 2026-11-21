package main

import (
 "strings"
 "testing"
)

func TestSailingExplicitSaveReplacesAutosync(t *testing.T){
 script,err:=sailingUIFiles.ReadFile("web/static/account.js")
 if err!=nil{t.Fatal(err)}
 s:=string(script)
 for _,fragment:=range []string{
  `saveButton.id = "sailing-save-changes"`,
  `saveButton.textContent = "Save Changes"`,
  `saveRow.append(saveButton,discardButton,saveNote)`,
  `saveButton.addEventListener("click",function(){void flush();})`,
  `discardButton.id = "sailing-discard-changes"`,
  `discardButton.textContent = "Discard Changes"`,
  `discardButton.addEventListener("click"`,
  `function refreshDirty(next,guest)`,
  `if (saved && same(next,saved))`,
  `savedSnapshot=JSON.parse(JSON.stringify(saved))`,
  `window.addEventListener("beforeunload"`,
  `if (!suppressUnload && dirty)`,
  `const guestDraftKey = "sailing.preferences.pending-guest.v1"`,
  `function stageGuest(data)`,
  `function stage(data)`,
  `async function flush()`,
  `method:"PUT"`,
  `response.status === 409`,
  `draft.user !== user || draft.revision !== revision`,
  `getJSON(guestDraftKey,false)`,
  `putJSON(draftKey,{user:user,revision:revision,data:data},false)`,
  `if (signedIn) stage(next)`,
  `else stageGuest(next)`,
 }{
  if !strings.Contains(s,fragment){t.Errorf("explicit-save contract missing %q",fragment)}
 }
 for _,obsolete:=range []string{
  `setTimeout(function () { pendingSave=null;void flush(); },250)`,
  `setTimeout(function(){pendingSave=null;void flush();},1200)`,
  `await flush();`,
  `else saveLocal(next)`,
 }{
  if strings.Contains(s,obsolete){t.Errorf("automatic preference commit remains %q",obsolete)}
 }
 for _,path:=range []string{"web/templates/sailing.html"}{
  markup:=readSailingUITemplate(path)
  for _,fragment:=range []string{`sailing-save-changes`,`sailing-discard-changes`,`sailing-save-status`,`Save Changes`,`Discard Changes`}{
   if !strings.Contains(markup,fragment){t.Errorf("%s missing save control %q",path,fragment)}
  }
 }
 for _,path:=range []string{"web/templates/welcome.html","web/templates/details.html","web/templates/stations.html"}{
  if strings.Contains(readSailingUITemplate(path),`sailing-save-changes`){t.Errorf("unexpected Save Changes control on %s",path)}
 }
}
