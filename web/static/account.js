// Homepage is the only sign-in authority. Preferences sync automatically for a
// verified Homepage bearer; without one, SailingGo uses browser-local settings.
// No approved credentials, cookies or bearer tokens are written to localStorage.
(function () {
  "use strict";
  const home = "https://home.themauris.org";
  const handoffKey = "sailing.homepage.handoff.v1";
  const navKey = "sailing.preferences.last-navigation.v2";
  const draftKey = "sailing.preferences.pending-account.v2";
  const restoreKey = "sailing.preferences.restoring.v2";
  const localKey = "sailing.preferences.browser.v2";
  const params = ["lat","lon","station","current_station","bin","wind_unit","distance_unit","wind_hours","map_center_lat","map_center_lon","map_zoom","map_layer","planning_start","planning_end","caution_ebb","caution_flood","max_ebb","max_flood","planning_buffer","current_distance_warning","current_days"];
  const overlays = ["map-show-marine-zone","map-show-smoke","map-show-fire-perimeters","map-show-fire-detections","map-show-air-temperature","map-show-clouds","map-show-radar","map-show-wind-barbs","map-show-inland-wind-barbs","map-show-pressure","map-show-swell","map-show-structure","map-show-saildrone"];
  const onPublicHost = location.protocol === "https:" && location.hostname === "weather.themauris.org";
  const pageURL = location.pathname + location.search;
  const fragment = new URLSearchParams(location.hash.substring(1));
  const handedOff = fragment.has("suite_token") || fragment.has("suite_mode");
  const explicitlyLocal = fragment.get("suite_mode") === "local";
  let bearer = "", user = "", revision = null, baseline = null;
  let signedIn = false, initializing = true, applying = false;
  let saving = false, dirty = false, pendingSave = null, blocked = false;
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
    const link = document.querySelector(".suite-home-link");
    if (!link) return null;
    const node = document.createElement("div");
    node.id = "sailing-account-identity";
    node.setAttribute("role", "status");
    node.setAttribute("aria-live", "polite");
    node.style.cssText = "margin:3px 0 10px;color:#69cbd5;font:600 13px/1.5 system-ui,-apple-system,BlinkMacSystemFont,sans-serif";
    node.textContent = "Not signed in";
    link.insertAdjacentElement("afterend", node);
    return node;
  }
  const label = makeLabel();
  function status(value) { if (label) label.textContent = value; }
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
  window.addEventListener("sailing-session-invalid",function(){signedIn=false;bearer="";clearSession(handoffKey);status("Session expired or revoked — Sign in again");requireSignIn();});
  function settingsFromPage(fallback) {
    const source = new URL(location.href).searchParams;
    const urlParams = {};
    // Retain preferences not represented on this view (such as map controls).
    if (fallback && fallback.params) Object.assign(urlParams, fallback.params);
    params.forEach(function (key) {
      if (source.has(key)) urlParams[key] = source.get(key).slice(0,256);
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
  function same(a,b) { return JSON.stringify(a) === JSON.stringify(b); }
  function actualParameterChanges(previous) {
    const before = new URL(previous,location.origin).searchParams;
    const after = new URL(location.href).searchParams;
    const changed = {};
    params.forEach(function (key) {
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
  async function applySettings(data, identity) {
    if (!valid(data)) return;
    const previous = new URL(location.href);
    const next = new URL(location.href);
    params.forEach(function (key) { next.searchParams.delete(key); });
    Object.entries(data.params).forEach(function (pair) { next.searchParams.set(pair[0],pair[1]); });
    if (next.pathname + next.search !== previous.pathname + previous.search) {
      const changed = params.filter(function(key) {
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
        location.replace(next.toString());
        return;
      }
    }
    restoreMap(data);
    document.documentElement.classList.remove("sailing-units-pending");
  }
  function keepNavigation(identity) {
    putJSON(navKey,{user:identity,url:pageURL},false);
  }
  function saveLocal(data) {
    if (valid(data)) putJSON(localKey,data,true);
  }
  async function browserOnly(guestName) {
    signedIn = false;
    status(guestName ? "Signed in as "+guestName+" · Guest (browser-only preferences)" : "Not signed in");
    const old = getJSON(localKey,true);
    let data = valid(old) ? old : settingsFromPage(null);
    if (priorNavigation && priorNavigation.user === "local" && priorNavigation.url !== pageURL) {
      data = mergeChangedNavigation(data, priorNavigation);
      saveLocal(data);
      keepNavigation("local");
    } else if (!valid(old)) saveLocal(data);
    await applySettings(data,"local");
    keepNavigation("local");
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
  function stage(data) {
    if (!signedIn || initializing || applying || blocked || !valid(data)) return;
    putJSON(draftKey,{user:user,revision:revision,data:data},false);
    dirty = true;
    if (pendingSave !== null) clearTimeout(pendingSave);
    pendingSave = setTimeout(function () { pendingSave=null;void flush(); },250);
  }
  async function flush() {
    if (!signedIn || blocked || saving || !dirty || revision === null) return;
    saving = true;
    const draft = getJSON(draftKey,false);
    if (!draft || draft.user !== user || !valid(draft.data)) { dirty=false;saving=false;return; }
    const desired = draft.data;
    dirty = false;
    try {
      if (same(desired,baseline)) {clearSession(draftKey);return;}
      const response = await request("/api/storage/sailing",{
        method:"PUT",headers:{"Content-Type":"application/json"},
        body:JSON.stringify({revision:revision,data:desired})
      });
      if (response.status === 409) {
        blocked = true;
        status("Signed in as "+user+" (Homepage) · preference conflict; browser changes retained");
        return;
      }
      if (!response.ok) throw Error("Preference save failed (HTTP "+response.status+")");
      const result = await response.json();
      revision = result.revision;
      baseline = desired;
      const newest = getJSON(draftKey,false);
      if (newest && newest.user === user && same(newest.data,desired)) clearSession(draftKey);
      else dirty = true;
    } catch (_) {
      // Keep the draft for a later validated attempt; never discard changes.
      dirty = true;
      status("Signed in as "+user+" (Homepage) · preferences not yet saved");
    } finally {
      saving = false;
      if (dirty && signedIn && !blocked) {
        if (pendingSave !== null) clearTimeout(pendingSave);
        pendingSave = setTimeout(function(){pendingSave=null;void flush();},1200);
      }
    }
  }
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
        initializing = false;
        await browserOnly(user);
        return;
      }
      status("Signed in as "+user+" (Homepage)");
      const existing = await record();
      if (existing && (!valid(existing.data) || !Number.isSafeInteger(existing.revision) || existing.revision < 0))
        throw Error("Unsupported account record");
      revision = existing ? existing.revision : 0;
      baseline = existing ? existing.data : null;
      signedIn = true;
      let desired = baseline ? JSON.parse(JSON.stringify(baseline)) : settingsFromPage(null);
      const held = getJSON(draftKey,false);
      // Only resume an unsaved draft for the same verified account, based on
      // the exact revision it was created from. Never cross user boundaries.
      if (held && held.user === user && valid(held.data) && held.revision === revision) {
        desired = held.data;
      } else if (held) {
        clearSession(draftKey);
      }
      if (!held || held.user !== user || held.revision !== revision) {
        if (baseline && !handedOff && !restoredNavigation &&
            priorNavigation && priorNavigation.user === user && priorNavigation.url !== pageURL) {
          desired = mergeChangedNavigation(desired,priorNavigation);
        }
      }
      // Initialize the user's account once; thereafter the account, not the
      // launch URL or the previous browser user, is authoritative.
      initializing = false;
      keepNavigation(user);
      if (!same(desired,baseline)) {
        putJSON(draftKey,{user:user,revision:revision,data:desired},false);
        dirty = true;
        await flush();
      }
      await applySettings(desired,user);
    } catch (err) {
      initializing = false;
      // A network fault is not an identity: preserve current browser settings,
      // but do not mutate the account or claim a valid login.
      signedIn = false;
      status("Not signed in · account unavailable");
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
      target.id === "wind-reading-hours" || target.matches("input[name='map-type']") ||
      target.matches(".map-marine-place-toggle") || overlays.includes(target.id);
    if (!preference) return;
    const next = settingsFromPage(baseline);
    if (target.id === "wind-unit-select") next.params.wind_unit = target.value === "mph" ? "mph" : "kts";
    if (target.id === "distance-unit-select") next.params.distance_unit = target.value === "mi" ? "mi" : "nmi";
    if (target.id === "wind-reading-hours") next.params.wind_hours = String(target.value);
    if (signedIn) stage(next);
    else saveLocal(next);
  },true);
  // A page-navigation change is recovered on the next view even if the
  // asynchronous save on the departing page could not complete.
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
