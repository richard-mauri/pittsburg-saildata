// Homepage is the only sign-in authority. Preferences are committed only by
// clicking Save Changes; verified guests save to browser-local storage.
// No approved credentials, cookies or bearer tokens are written to localStorage.
(function () {
  "use strict";
  const home = "https://home.themauris.org";
  const handoffKey = "sailing.homepage.handoff.v1";
  const navKey = "sailing.preferences.last-navigation.v2";
  const draftKey = "sailing.preferences.pending-account.v2";
  const restoreKey = "sailing.preferences.restoring.v2";
  const localKey = "sailing.preferences.browser.v2";
  const guestDraftKey = "sailing.preferences.pending-guest.v1";
  // Distinguish report URL values from UI-only choices: the latter persist in
  // the same schema-1 params record but must not trigger report navigation.
  const urlParams = ["lat","lon","station","current_station","bin","wind_unit","distance_unit","wind_hours","map_center_lat","map_center_lon","map_zoom","map_layer","planning_start","planning_end","caution_ebb","caution_flood","max_ebb","max_flood","planning_buffer","current_distance_warning","current_days"];
  const controlDefaults = {"swell_playback_speed":"1","swell_playback_step":"3","alertcalifornia_mode":"clustered"};
  const controlIDs = {"map-swell-playback-speed":"swell_playback_speed","map-swell-playback-step":"swell_playback_step","map-alertcalifornia-mode":"alertcalifornia_mode"};
  const controlOptions = {"swell_playback_speed":["0.5","1","2"],"swell_playback_step":["1","3","6"],"alertcalifornia_mode":["clustered","all"]};
  const params = urlParams.concat(Object.keys(controlDefaults));
  const overlays = ["map-show-marine-zone","map-show-smoke","map-show-fire-perimeters","map-show-fire-detections","map-show-air-temperature","map-show-clouds","map-show-radar","map-show-wind-barbs","map-show-inland-wind-barbs","map-show-pressure","map-show-swell","map-show-structure","map-show-saildrone","map-show-alertcalifornia","show-tide-range-overlay"];
  const onPublicHost = location.protocol === "https:" && location.hostname === "weather.themauris.org";
  const pageURL = location.pathname + location.search;
  const fragment = new URLSearchParams(location.hash.substring(1));
  const handedOff = fragment.has("suite_token") || fragment.has("suite_mode");
  const explicitlyLocal = fragment.get("suite_mode") === "local";
  let bearer = "", user = "", guestIdentity = "", revision = null, baseline = null;
  let savedSnapshot = null; // Last successfully saved preferences, not the latest edit.
  let signedIn = false, initializing = true, applying = false;
  let saving = false, dirty = false, blocked = false, suppressUnload = false;
  let priorNavigation = null, restoredNavigation = false;
  function getSession(key) { try { return sessionStorage.getItem(key); } catch (_) { return null; } }
  function putSession(key, value) { try { sessionStorage.setItem(key, value); return true; } catch (_) { return false; } }
  function clearSession(key) { try { sessionStorage.removeItem(key); } catch (_) {} }
  function getJSON(key, local) {
    try { return JSON.parse((local ? localStorage : sessionStorage).getItem(key) || "null"); } catch (_) { return null; }
  }
  function putJSON(key, value, local) {
    try { (local ? localStorage : sessionStorage).setItem(key, JSON.stringify(value)); return true; } catch (_) { return false; }
  }
  function makeLabel() {
    const main = document.querySelector("main.suite-content");
    if (!main) return null;
    const node = document.createElement("div");
    node.id = "sailing-account-note";
    node.className = "sailing-account-note";
    node.setAttribute("role", "status");
    node.setAttribute("aria-live", "polite");
    node.hidden = true;
    main.prepend(node);
    return node;
  }
  const label = makeLabel();
  function status(value) { if (label) {label.textContent = value;label.hidden=!value;} }
  function signedInStatus(name, note) {SuiteBannerStatus.signedIn(name);status(note || "");}
  // The same explicit-save control is present on Conditions Now and Planning.
  // It lives in the existing preference toolbar alongside Wind/Distance units.
  const saveRow = document.querySelector(".page-preferences");
  const saveButton = document.createElement("button");
  saveButton.id = "sailing-save-changes";
  saveButton.className = "ui-button";
  saveButton.type = "button";
  saveButton.textContent = "Save Changes";
  saveButton.disabled = true;
  const discardButton = document.createElement("button");
  discardButton.id = "sailing-discard-changes";
  discardButton.className = "ui-button";
  discardButton.type = "button";
  discardButton.textContent = "Discard Changes";
  discardButton.disabled = true;
  const saveNote = document.createElement("span");
  saveNote.id = "sailing-save-status";
  saveNote.setAttribute("role","status");
  saveNote.setAttribute("aria-live","polite");
  saveNote.style.cssText = "font-size:.83rem;color:var(--muted);max-width:25rem";
  if (saveRow) saveRow.append(saveButton,discardButton,saveNote);
  function updateSaveUI(message) {
    saveButton.disabled = saving || blocked || !dirty;
    discardButton.disabled = saving || (!dirty && !blocked);
    saveNote.textContent = message === undefined
      ? (blocked ? "Preference conflict · Changes retained." :
         dirty ? "Unsaved changes · Click Save Changes." : "")
      : message;
  }
  const signInDialog = document.createElement("dialog");
  signInDialog.className = "sailing-suite-signin";
  signInDialog.setAttribute("aria-labelledby","sailing-suite-signin-title");
  const signInTitle = document.createElement("h2");
  signInTitle.id = "sailing-suite-signin-title";
  signInTitle.textContent = "Suite sign-in required";
  const signInInfo = document.createElement("p");
  signInInfo.textContent = "Go to Homepage to sign in with your personal token or obtain a temporary authenticated guest session valid for one hour. Guest preferences remain browser-only.";
  const signInLink = document.createElement("a");
  signInLink.href = home; signInLink.target = "_blank";signInLink.rel = "noopener noreferrer";
  signInLink.textContent = "Go to Homepage";
  signInDialog.addEventListener("cancel",function(event){event.preventDefault();});
  signInDialog.append(signInTitle,signInInfo,signInLink);
  document.body.append(signInDialog);
  function requireSignIn(){if(onPublicHost && !signInDialog.open)signInDialog.showModal();}
  function clearSignIn(){if(signInDialog.open)signInDialog.close();}
  window.addEventListener("sailing-session-invalid",function(){signedIn=false;bearer="";clearSession(handoffKey);SuiteBannerStatus.setText("Browser-only mode");status("Session expired or revoked — Sign in again");requireSignIn();});
  function urlParamsList() { return urlParams; }
  function settingsFromPage(fallback) {
    const source = new URL(location.href).searchParams;
    const urlParams = {};
    // Retain preferences not represented on this view (such as map controls).
    if (fallback && fallback.params) Object.assign(urlParams, fallback.params);
    urlParamsList().forEach(function (key) {
      if (source.has(key)) urlParams[key] = source.get(key).slice(0,256);
    });
    Object.entries(controlIDs).forEach(function (entry) {
      const element = document.getElementById(entry[0]);
      if (element && controlOptions[entry[1]].includes(element.value))
        urlParams[entry[1]] = element.value;
    });
    const mapOverlays = Object.assign({}, fallback ? fallback.overlays : {});
    overlays.forEach(function (id) {
      const el = document.getElementById(id);
      if (el) mapOverlays[id] = !!el.checked;
    });
    const categories = Object.assign({}, fallback ? fallback.marineCategories : {});
    document.querySelectorAll(".map-marine-place-toggle").forEach(function (el) {
      const cat = el.getAttribute("data-marine-category");
      if (cat && /^[a-z_]+$/.test(cat) && cat.length <= 48) categories[cat] = !!el.checked;
    });
    const selected = document.querySelector('input[name="map-type"]:checked');
    return {schema:1,params:urlParams,overlays:mapOverlays,
      mapType:selected ? selected.value : (fallback ? fallback.mapType : ""),
      marineCategories:categories};
  }
  function valid(data) {
    if (!data || data.schema !== 1 || !data.params || typeof data.params !== "object" || Array.isArray(data.params) ||
        !data.overlays || typeof data.overlays !== "object" || Array.isArray(data.overlays) ||
        !data.marineCategories || typeof data.marineCategories !== "object" || Array.isArray(data.marineCategories) ||
        !["","map","nautical","satellite","hybrid"].includes(data.mapType)) return false;
    return Object.entries(data.params).every(function (pair) {
      return params.includes(pair[0]) && typeof pair[1] === "string" && pair[1].length <= 256;
    }) && Object.entries(data.overlays).every(function (pair) {
      return overlays.includes(pair[0]) && typeof pair[1] === "boolean";
    }) && Object.entries(data.marineCategories).every(function (pair) {
      return /^[a-z_]+$/.test(pair[0]) && pair[0].length <= 48 && typeof pair[1] === "boolean";
    });
  }
  // Compare canonical preference state, not raw DOM snapshots. Older saved
  // records omit unchecked overlays/categories, while the live page reports
  // those same values as false. Both describe the same saved state.
  function canonicalState(data) {
    if (!data) return null;
    const state = JSON.parse(JSON.stringify(data));
    overlays.forEach(function (id) {
      state.overlays[id] = id === "show-tide-range-overlay"
        ? state.overlays[id] !== false : !!state.overlays[id];
    });
    Object.keys(controlDefaults).forEach(function (key) {
      const value = state.params[key];
      if (!value || value === controlDefaults[key]) delete state.params[key];
    });
    Object.keys(state.marineCategories).forEach(function (cat) {
      state.marineCategories[cat] = !!state.marineCategories[cat];
    });
    // Empty mapType in older account records means the default Street map,
    // unless the record explicitly selected another layer through its URL.
    if (!state.mapType) {
      state.mapType = ["nautical","satellite","hybrid"].includes(state.params.map_layer)
        ? state.params.map_layer : "map";
    }
    if (state.params.current_days === "1") delete state.params.current_days;
    // A radio selection is authoritative; the map controller updates the URL
    // after our capturing change listener runs.
    if (state.mapType === "map") delete state.params.map_layer;
    else if (state.mapType) state.params.map_layer = state.mapType;
    return state;
  }
  function stable(value) {
    if (Array.isArray(value)) return value.map(stable);
    if (value && typeof value === "object") {
      const result = {};
      Object.keys(value).sort().forEach(function(key){result[key]=stable(value[key]);});
      return result;
    }
    return value;
  }
  function same(a,b) {
    const left = canonicalState(a), right = canonicalState(b);
    if (left && right) {
      // A category missing from an older record is the same as unchecked.
      Object.keys(left.marineCategories).forEach(function(cat) {
        if (!(cat in right.marineCategories)) right.marineCategories[cat] = false;
      });
      Object.keys(right.marineCategories).forEach(function(cat) {
        if (!(cat in left.marineCategories)) left.marineCategories[cat] = false;
      });
    }
    return JSON.stringify(stable(left)) === JSON.stringify(stable(right));
  }
  function refreshDirty(next,guest) {
    const key = guest ? guestDraftKey : draftKey;
    const saved = savedSnapshot;
    if (saved && same(next,saved)) {
      clearSession(key);
      dirty=false;
      blocked=false;
      updateSaveUI();
      return false;
    }
    return true;
  }
  function actualParameterChanges(previous) {
    const before = new URL(previous,location.origin).searchParams;
    const after = new URL(location.href).searchParams;
    const changed = {};
    urlParams.forEach(function (key) {
      // A different view may omit parameters as part of normal navigation.
      // Only treat an omitted parameter as intentional on the same path.
      if (after.has(key) && before.get(key) !== after.get(key)) changed[key] = after.get(key).slice(0,256);
      else if (before.has(key) && !after.has(key) && new URL(previous,location.origin).pathname === location.pathname) changed[key] = null;
    });
    return changed;
  }
  function mergeChangedNavigation(data, prior) {
    const copy = JSON.parse(JSON.stringify(data));
    Object.entries(actualParameterChanges(prior.url)).forEach(function (pair) {
      if (pair[1] === null) delete copy.params[pair[0]];
      else copy.params[pair[0]] = pair[1];
    });
    return copy;
  }
  function restoreMap(data) {
    if (!document.querySelector('input[name="map-type"]')) return;
    applying = true;
    try {
      const type = Array.from(document.querySelectorAll('input[name="map-type"]'))
        .find(function (el) { return el.value === data.mapType; });
      if (type && !type.checked) type.click();
      Object.entries(data.overlays).forEach(function (pair) {
        const el = document.getElementById(pair[0]);
        if (el && el.checked !== pair[1]) el.click();
      });
      Object.entries(data.marineCategories).forEach(function (pair) {
        const el = Array.from(document.querySelectorAll(".map-marine-place-toggle"))
          .find(function (item) { return item.getAttribute("data-marine-category") === pair[0]; });
        if (el && el.checked !== pair[1]) el.click();
      });
    } finally { applying = false; }
  }
  // Restore UI-only selections through their existing controller listeners.
  // No preference staging occurs during restoration because applying is true.
  function restoreControlPreferences(data,chartOnly) {
    if (!valid(data)) return;
    applying = true;
    try {
      if (!chartOnly) Object.entries(controlIDs).forEach(function (entry) {
        const element = document.getElementById(entry[0]);
        if (!element) return;
        const value = controlOptions[entry[1]].includes(data.params[entry[1]])
          ? data.params[entry[1]] : controlDefaults[entry[1]];
        if (element.value !== value) {
          element.value = value;
          element.dispatchEvent(new Event("change",{bubbles:true}));
        }
      });
      // This checkbox sits inside the chart subtree, which is replaced by
      // the chart's in-place AJAX refresh. Reapply after every replacement.
      const tide = document.getElementById("show-tide-range-overlay");
      if (tide) {
        const enabled = data.overlays["show-tide-range-overlay"] !== false;
        if (tide.checked !== enabled) {
          tide.checked = enabled;
          tide.dispatchEvent(new Event("change",{bubbles:true}));
        }
      }
    } finally { applying = false; }
  }
  document.addEventListener("sailing-current-chart-updated",function(){
    const key = signedIn ? draftKey : guestDraftKey;
    const draft = getJSON(key,false);
    const own = draft && valid(draft.data) &&
      (signedIn ? draft.user === user : draft.user === (guestIdentity || "local"));
    restoreControlPreferences(own ? draft.data : savedSnapshot,true);
  });
  async function applySettings(data, identity) {
    if (!valid(data)) return;
    const previous = new URL(location.href);
    const next = new URL(location.href);
    urlParams.forEach(function (key) { next.searchParams.delete(key); });
    Object.entries(data.params).forEach(function (pair) {
      if (urlParams.includes(pair[0])) next.searchParams.set(pair[0],pair[1]);
    });
    if (next.pathname + next.search !== previous.pathname + previous.search) {
      const changed = urlParams.filter(function(key) {
        return previous.searchParams.get(key) !== next.searchParams.get(key);
      });
      if (changed.every(function(key) { return key === "wind_unit" || key === "distance_unit"; }) &&
          typeof window.sailingRefreshUnits === "function") {
        // In-place unit application: do not tear down the map, charts or page.
        const wind = document.getElementById("wind-unit-select");
        const distance = document.getElementById("distance-unit-select");
        if (wind) wind.value = next.searchParams.get("wind_unit") === "mph" ? "mph" : "kts";
        if (distance) distance.value = next.searchParams.get("distance_unit") === "mi" ? "mi" : "nmi";
        history.replaceState(history.state,"",next.pathname + next.search);
        await window.sailingRefreshUnits(next,previous,changed.includes("wind_unit"));
      } else {
        // Location/station/planning changes may require server recomputation;
        // preserve the current route and the user's account on navigation.
        putSession(restoreKey, JSON.stringify({user:identity,url:next.pathname+next.search}));
        suppressUnload=true; // Internal account restoration keeps staged drafts.
        location.replace(next.toString());
        return;
      }
    }
    restoreMap(data);
    restoreControlPreferences(data,false);
    document.documentElement.classList.remove("sailing-units-pending");
  }
  function keepNavigation(identity) {
    putJSON(navKey,{user:identity,url:pageURL},false);
  }
  function saveLocal(data) {
    return valid(data) && putJSON(localKey,data,true);
  }
  // Guest changes remain tab-scoped until the visitor explicitly saves.
  function stageGuest(data) {
    if (initializing || applying || !valid(data)) return;
    if (!refreshDirty(data,true)) return;
    if (!putJSON(guestDraftKey,{user:guestIdentity || "local",data:data},false)) {
      updateSaveUI("Could not stage preferences in this tab.");
      return;
    }
    dirty = true;
    updateSaveUI();
  }
  async function browserOnly(guestName) {
    signedIn = false;
    const localIdentity = guestName || guestIdentity;
    if (localIdentity) signedInStatus(localIdentity,"");
    else {SuiteBannerStatus.setText("Browser-only mode");status("Not signed in");}
    const old = getJSON(localKey,true);
    const saved = valid(old) ? old : settingsFromPage(null);
    savedSnapshot = JSON.parse(JSON.stringify(saved));
    const pending = getJSON(guestDraftKey,false);
    const ownPending = pending && pending.user === (localIdentity || "local") && valid(pending.data);
    let data = ownPending ? pending.data : saved;
    dirty = !!ownPending && !same(data,saved);
    if (ownPending && !dirty) clearSession(guestDraftKey);
    if (priorNavigation && priorNavigation.user === "local" && priorNavigation.url !== pageURL) {
      const changed = mergeChangedNavigation(data,priorNavigation);
      if (!same(changed,data)) {
        data=changed;
        if (same(data,savedSnapshot)) {
          clearSession(guestDraftKey);
          dirty=false;
        } else if (putJSON(guestDraftKey,{user:localIdentity || "local",data:data},false)) dirty=true;
      }
    }
    await applySettings(data,"local");
    keepNavigation("local");
    updateSaveUI();
  }
  // A handoff is transferred only through the fragment. Clear it immediately.
  if (handedOff) {
    const received = fragment.get("suite_token");
    if (explicitlyLocal || !/^[a-f0-9]{64}$/.test(received || "")) clearSession(handoffKey);
    else putSession(handoffKey, received);
    history.replaceState(history.state, "", location.pathname + location.search);
  }
  priorNavigation = getJSON(navKey,false);
  const restoring = getJSON(restoreKey,false);
  if (restoring && restoring.url === pageURL) {
    restoredNavigation = true;
    clearSession(restoreKey);
  }
  bearer = getSession(handoffKey) || "";
  if (!/^[a-f0-9]{64}$/.test(bearer)) bearer = "";
  async function request(path, options) {
    if (!bearer) throw Error("No Homepage handoff");
    const opts = options || {};
    const headers = Object.assign({}, opts.headers || {}, {Authorization:"Bearer "+bearer});
    const response = await fetch(home+path,Object.assign({credentials:"omit",cache:"no-store"},opts,{headers:headers}));
    if (response.status === 401 || response.status === 403) {
      bearer = ""; clearSession(handoffKey); signedIn = false;
      status("Not signed in");
      requireSignIn();
    }
    return response;
  }
  async function record() {
    const response = await request("/api/storage/sailing");
    if (response.status === 404) return null;
    if (!response.ok) throw Error("Preference read failed (HTTP "+response.status+")");
    return response.json();
  }
  // Staging is separate from persistence: no timer or passive account PUT.
  function stage(data) {
    if (!signedIn || initializing || applying || !valid(data)) return;
    if (!refreshDirty(data,false)) return;
    if (!putJSON(draftKey,{user:user,revision:revision,data:data},false)) {
      updateSaveUI("Could not stage preferences in this tab.");
      return;
    }
    dirty = true;
    updateSaveUI();
  }
  // This function is called exclusively by the explicit Save Changes button.
  async function flush() {
    if (saving || !dirty || blocked) return;
    if (!signedIn) {
      const guest = getJSON(guestDraftKey,false);
      if (!guest || guest.user !== (guestIdentity || "local") || !valid(guest.data)) {
        updateSaveUI("Pending Guest preferences unavailable; retry the edit.");
        return;
      }
      saving=true;updateSaveUI("Saving…");
      if (saveLocal(guest.data)) {
        clearSession(guestDraftKey);
        savedSnapshot=JSON.parse(JSON.stringify(guest.data));
        dirty=false;updateSaveUI("Settings saved in this browser.");
      } else updateSaveUI("Save failed · Click Save Changes to retry.");
      saving=false;updateSaveUI(dirty ? undefined : "Settings saved in this browser.");
      return;
    }
    if (revision === null) return;
    const draft = getJSON(draftKey,false);
    if (!draft || draft.user !== user || draft.revision !== revision || !valid(draft.data)) {
      updateSaveUI("Pending account preferences unavailable; retry the edit.");
      return;
    }
    const desired=draft.data;
    if (same(desired,baseline)) {
      clearSession(draftKey);dirty=false;updateSaveUI("");
      return;
    }
    saving=true;updateSaveUI("Saving…");
    try {
      const response = await request("/api/storage/sailing",{
        method:"PUT",headers:{"Content-Type":"application/json"},
        body:JSON.stringify({revision:revision,data:desired})
      });
      if (response.status === 409) {
        blocked=true;
        signedInStatus(user,"Preference conflict; browser changes retained");
        updateSaveUI("Preference conflict · Changes retained.");
        return;
      }
      if (!response.ok) throw Error("Preference save failed (HTTP "+response.status+")");
      const result=await response.json();
      if (!Number.isSafeInteger(result.revision) || result.revision<=revision)
        throw Error("Invalid account save revision");
      revision=result.revision;
      baseline=desired;
      savedSnapshot=JSON.parse(JSON.stringify(desired));
      const newest=getJSON(draftKey,false);
      if (newest && newest.user===user && newest.revision===draft.revision && same(newest.data,desired)){
        clearSession(draftKey);
        dirty=false;
        updateSaveUI("Settings saved.");
      } else {
        // Do not discard edits made during a pending network request.
        if (newest && newest.user===user && valid(newest.data) && !same(newest.data,savedSnapshot)) {
          putJSON(draftKey,{user:user,revision:revision,data:newest.data},false);
          dirty=true;
          updateSaveUI("Saved; newer changes remain unsaved.");
        } else {
          clearSession(draftKey);
          dirty=false;
          updateSaveUI("Settings saved.");
        }
      }
    } catch (_) {
      dirty=true;
      updateSaveUI("Save failed · Click Save Changes to retry.");
    } finally {
      saving=false;
      updateSaveUI();
    }
  }
  saveButton.addEventListener("click",function(){void flush();});
  discardButton.addEventListener("click",function(){
    if (saving || (!dirty && !blocked) || !valid(savedSnapshot)) return;
    // Discard never writes Homepage or the Guest's persisted browser copy.
    // The previously saved account/browser snapshot remains authoritative.
    clearSession(draftKey);
    clearSession(guestDraftKey);
    dirty=false;
    blocked=false;
    updateSaveUI("Changes discarded.");
    void applySettings(JSON.parse(JSON.stringify(savedSnapshot)),signedIn ? user : "local");
  });
  window.addEventListener("beforeunload",function(event){
    if (!suppressUnload && dirty){event.preventDefault();event.returnValue="";}
  });
  async function hydrate() {
    if (!onPublicHost || !bearer || explicitlyLocal) {initializing=false;await browserOnly();return;}
    try {
      const profile = await request("/api/session/profile");
      if (!profile.ok) throw Error("Homepage verification HTTP "+profile.status);
      const info = await profile.json();
      if (typeof info.user !== "string" || !info.user || !(Date.parse(info.expiresAt)>Date.now()))
        throw Error("Invalid session");
      user = info.user;
      clearSignIn();
      if(info.role === "guest") {
        // Identity is already verified. Local preference restoration is
        // independent and must never erase or downgrade that identity.
        guestIdentity = user;
        initializing = false;
        try {
          await browserOnly(user);
        } catch (_) {
          document.documentElement.classList.remove("sailing-units-pending");
          signedInStatus(user,"");
        }
        return;
      }
      guestIdentity = "";
      signedInStatus(user,"");
      const existing = await record();
      if (existing && (!valid(existing.data) || !Number.isSafeInteger(existing.revision) || existing.revision < 0))
        throw Error("Unsupported account record");
      revision = existing ? existing.revision : 0;
      baseline = existing ? existing.data : null;
      signedIn = true;
      let desired = baseline ? JSON.parse(JSON.stringify(baseline)) : settingsFromPage(null);
      savedSnapshot = JSON.parse(JSON.stringify(desired));
      const held = getJSON(draftKey,false);
      // Only resume an unsaved draft for the same verified account, based on
      // the exact revision it was created from. Never cross user boundaries.
      if (held && held.user === user && valid(held.data) && held.revision === revision) {
        desired = held.data;
      } else if (held) {
        clearSession(draftKey);
      }
      // Preserve a pending draft across full-page navigation; only the
      // parameters actually changed by the user on the intervening route
      // are folded into it, never an automatic account save.
      let navigated=false;
      if (!handedOff && !restoredNavigation && priorNavigation &&
          priorNavigation.user === user && priorNavigation.url !== pageURL) {
        const merged=mergeChangedNavigation(desired,priorNavigation);
        if (!same(merged,desired)) {desired=merged;navigated=true;}
      }
      dirty = (navigated || !!(held && held.user === user && held.revision === revision && valid(held.data))) &&
        !same(desired,savedSnapshot);
      if (dirty) putJSON(draftKey,{user:user,revision:revision,data:desired},false);
      else clearSession(draftKey);
      initializing = false;
      keepNavigation(user);
      await applySettings(desired,user);
      updateSaveUI();
    } catch (err) {
      initializing = false;
      // A network fault is not an identity: preserve current browser settings,
      // but do not mutate the account or claim a valid login.
      signedIn = false;
      SuiteBannerStatus.setText("Browser-only mode");
      status(/HTTP (401|403)|Invalid session/.test(String(err.message)) ? "Session expired or revoked — Sign in again" :
        "Not signed in · Homepage verification unavailable");
      if(/HTTP (401|403)|Invalid session/.test(String(err.message))) requireSignIn();
      document.documentElement.classList.remove("sailing-units-pending");
    }
  }
  // Capture change events before existing page handlers navigate/reload.
  document.addEventListener("change",function (event) {
    if (initializing || applying) return;
    const target = event.target;
    if (!target) return;
    const preference = target.id === "wind-unit-select" || target.id === "distance-unit-select" ||
      target.id === "wind-reading-hours" || target.id === "current-days-picker" ||
      Object.prototype.hasOwnProperty.call(controlIDs,target.id) ||
      target.matches("input[name='map-type']") ||
      target.matches(".map-marine-place-toggle") || overlays.includes(target.id);
    if (!preference) return;
    const pending = signedIn ? getJSON(draftKey,false) : getJSON(guestDraftKey,false);
    const previous = pending && valid(pending.data) &&
      (signedIn ? pending.user===user : pending.user===(guestIdentity || "local"))
      ? pending.data : savedSnapshot;
    if (!valid(previous)) return;
    // Change a single field in the current preference state. Reading all
    // controls and URL values on every event pulls in unrelated map defaults,
    // and can make a reversible map-type change look permanently dirty.
    const next = JSON.parse(JSON.stringify(previous));
    if (target.matches("input[name='map-type']")) {
      if (target.value === "map") delete next.params.map_layer;
      else next.params.map_layer = target.value;
      next.mapType = target.value;
    }
    if (target.id === "wind-unit-select") next.params.wind_unit = target.value === "mph" ? "mph" : "kts";
    if (target.id === "distance-unit-select") next.params.distance_unit = target.value === "mi" ? "mi" : "nmi";
    if (target.id === "wind-reading-hours") next.params.wind_hours = String(target.value);
    if (Object.prototype.hasOwnProperty.call(controlIDs,target.id)) {
      const key = controlIDs[target.id];
      if (!controlOptions[key].includes(target.value)) return;
      if (target.value === controlDefaults[key]) delete next.params[key];
      else next.params[key] = target.value;
    }
    if (target.id === "current-days-picker") {
      // The chart refreshes by fetch + history.pushState, without a full
      // navigation. Stage the range now, before its DOM subtree is replaced.
      if (target.value === "3" || target.value === "7") next.params.current_days = target.value;
      else delete next.params.current_days;
    }
    if (overlays.includes(target.id)) next.overlays[target.id] = !!target.checked;
    if (target.matches(".map-marine-place-toggle")) {
      const cat = target.getAttribute("data-marine-category");
      if (cat && /^[a-z_]+$/.test(cat) && cat.length <= 48)
        next.marineCategories[cat] = !!target.checked;
    }
    if (signedIn) stage(next);
    else stageGuest(next);
  },true);
  // A page-navigation change is recovered on the next view as pending,
  // not silently committed to the Homepage account or Guest storage.
  window.addEventListener("pagehide",function () {
    if (!initializing) keepNavigation(signedIn ? user : "local");
  });
  if (onPublicHost && !bearer && !explicitlyLocal) {
    // A fresh Safari tab has no transferable bearer. Recover an existing
    // Homepage login through its allowlisted same-origin bridge. An absent,
    // expired or revoked parent returns once with #suite_mode=local, avoiding
    // loops and preserving browser-only settings.
    location.replace(home + "/launch/sailing");
    return;
  }
  if (!onPublicHost || !bearer || explicitlyLocal) {
    initializing=false;void browserOnly().then(function(){if(explicitlyLocal)requireSignIn();});
  } else {
    void hydrate();
  }
})();
