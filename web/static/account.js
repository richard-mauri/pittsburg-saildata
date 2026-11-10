// SailingGo's optional Homepage account settings. No personal credential is stored here.
// Browser-only dashboard navigation works without Homepage or this script.
(function () {
  "use strict";
  const base = "https://home.themauris.org";
  const handoffKey = "sailing.homepage.handoff.v1";
  const optInKey = "sailing.homepage.autosave.v1";
  const optInUserKey = "sailing.homepage.autosave-user.v1";
  const pendingKey = "sailing.homepage.restore.v1";
  const navigationKey = "sailing.homepage.last-url.v1";
  let navigationChanged = false;
  try {
    const currentURL = location.pathname + location.search;
    const previousURL = sessionStorage.getItem(navigationKey);
    navigationChanged = previousURL !== null && previousURL !== currentURL;
    sessionStorage.setItem(navigationKey, currentURL);
  } catch (_) {}
  const allowedParams = ["lat","lon","station","current_station","bin","wind_unit","distance_unit","wind_hours","map_center_lat","map_center_lon","map_zoom","map_layer","planning_start","planning_end","caution_ebb","caution_flood","max_ebb","max_flood","planning_buffer","current_distance_warning","current_days"];
  const overlayIds = ["map-show-marine-zone","map-show-smoke","map-show-fire-perimeters","map-show-fire-detections","map-show-air-temperature","map-show-clouds","map-show-radar","map-show-wind-barbs","map-show-inland-wind-barbs","map-show-pressure","map-show-swell","map-show-structure","map-show-saildrone"];
  const hostEnabled = location.protocol === "https:" && location.hostname === "weather.themauris.org";
  let bearer = "", account = "", revision = null, authenticated = false, saving = false, queued = false, timer = null;
  let autosave = false;
  try { autosave = sessionStorage.getItem(optInKey) === "yes"; } catch (_) {}
  const fragment = new URLSearchParams(location.hash.substring(1));
  if (fragment.has("suite_token") || fragment.get("suite_mode") === "local") {
    const issued = fragment.get("suite_token");
    try {
      if (fragment.get("suite_mode") === "local" || !/^[a-f0-9]{64}$/.test(issued || "")) {
        sessionStorage.removeItem(handoffKey);
        sessionStorage.removeItem(optInKey);
        sessionStorage.removeItem(optInUserKey);
        autosave = false;
      } else {
        sessionStorage.setItem(handoffKey, issued);
      }
    } catch (_) {}
    history.replaceState(history.state, "", location.pathname + location.search);
  }
  try { bearer = sessionStorage.getItem(handoffKey) || ""; } catch (_) {}
  if (!/^[a-f0-9]{64}$/.test(bearer)) bearer = "";
  const panel = document.querySelector(".page-preferences");
  if (!panel) return;
  const group = document.createElement("div");
  group.className = "sailing-account-controls";
  group.style.cssText = "display:flex;flex-wrap:wrap;align-items:center;gap:8px;margin:8px 0;font:500 12px/1.5 system-ui,-apple-system,sans-serif";
  const status = document.createElement("span");
  status.setAttribute("role", "status");
  status.setAttribute("aria-live", "polite");
  status.textContent = "Browser-only preferences";
  const saveButton = document.createElement("button");
  saveButton.type = "button"; saveButton.textContent = "Save to account"; saveButton.disabled = true;
  const loadButton = document.createElement("button");
  loadButton.type = "button"; loadButton.textContent = "Load from account"; loadButton.disabled = true;
  [saveButton, loadButton].forEach(function (b) {
    b.style.cssText = "border:1px solid #9ab9c7;border-radius:8px;background:#f7fbfc;color:#153f52;padding:6px 9px;cursor:pointer;font:inherit";
  });
  group.append(status, saveButton, loadButton);
  panel.appendChild(group);
  function message(value) { status.textContent = value; }
  function disableAccount(value) {
    authenticated = false; saveButton.disabled = true; loadButton.disabled = true;
    if (value) message(value);
  }
  async function request(path, options) {
    if (!bearer) throw Error("No Homepage handoff; open SailingGo from Homepage.");
    const opts = options || {};
    const headers = Object.assign({}, opts.headers || {}, {Authorization: "Bearer " + bearer});
    const result = await fetch(base + path, Object.assign({credentials:"omit",cache:"no-store"}, opts, {headers:headers}));
    if (result.status === 401 || result.status === 403) {
      bearer = ""; authenticated = false;
      try { sessionStorage.removeItem(handoffKey); sessionStorage.removeItem(optInKey); sessionStorage.removeItem(optInUserKey); } catch (_) {}
      autosave = false;
      disableAccount("Account session expired; browser preferences preserved.");
    }
    return result;
  }
  function snapshot() {
    const params = {};
    const query = new URL(location.href).searchParams;
    allowedParams.forEach(function (key) {
      if (query.has(key)) params[key] = query.get(key).slice(0, 256);
    });
    const overlays = {};
    overlayIds.forEach(function (id) {
      const el = document.getElementById(id);
      if (el) overlays[id] = !!el.checked;
    });
    const type = document.querySelector('input[name="map-type"]:checked');
    const categories = {};
    document.querySelectorAll(".map-marine-place-toggle").forEach(function (el) {
      const category = el.getAttribute("data-marine-category");
      if (category && /^[a-z_]+$/.test(category)) categories[category] = !!el.checked;
    });
    return {schema:1,params:params,overlays:overlays,mapType:type ? type.value : "",marineCategories:categories};
  }
  function valid(data) {
    if (!data || data.schema !== 1 || typeof data.params !== "object" || !data.params ||
        Array.isArray(data.params) || typeof data.overlays !== "object" || !data.overlays ||
        Array.isArray(data.overlays) || typeof data.marineCategories !== "object" || !data.marineCategories ||
        Array.isArray(data.marineCategories) || !["","map","nautical","satellite","hybrid"].includes(data.mapType)) return false;
    return Object.entries(data.params).every(function (pair) {
      return allowedParams.includes(pair[0]) && typeof pair[1] === "string" && pair[1].length <= 256;
    }) && Object.entries(data.overlays).every(function (pair) {
      return overlayIds.includes(pair[0]) && typeof pair[1] === "boolean";
    }) && Object.entries(data.marineCategories).every(function (pair) {
      return /^[a-z_]+$/.test(pair[0]) && pair[0].length <= 48 && typeof pair[1] === "boolean";
    });
  }
  async function accountRecord() {
    const response = await request("/api/storage/sailing");
    if (response.status === 404) return null;
    if (!response.ok) throw Error("Account read failed (HTTP " + response.status + ").");
    return response.json();
  }
  async function save() {
    if (!authenticated || saving) return;
    saving = true; saveButton.disabled = true;
    try {
      if (revision === null) {
        const existing = await accountRecord();
        if (existing && !valid(existing.data)) throw Error("Saved account format is unsupported; nothing overwritten.");
        if (existing && !autosave && !window.confirm("Replace existing SailingGo account preferences with this browser's settings?")) {
          message("Account unchanged; browser settings preserved."); return;
        }
        revision = existing ? existing.revision : 0;
      }
      const response = await request("/api/storage/sailing", {
        method:"PUT", headers:{"Content-Type":"application/json"},
        body:JSON.stringify({revision:revision,data:snapshot()})
      });
      if (response.status === 409) {
        revision = null; autosave = false;
        try {sessionStorage.removeItem(optInKey);} catch (_) {}
        throw Error("Account preferences changed elsewhere. Load them before saving again.");
      }
      if (!response.ok) throw Error("Account save failed (HTTP " + response.status + "); browser settings retained.");
      const result = await response.json();
      revision = result.revision;
      autosave = true;
      try { sessionStorage.setItem(optInKey, "yes"); sessionStorage.setItem(optInUserKey, account); } catch (_) {}
      message("Saved for " + account + " · automatic saving enabled.");
    } catch (err) { message(err.message); }
    finally { saving = false; saveButton.disabled = !authenticated; if (queued) {queued=false; queueSave();} }
  }
  function queueSave() {
    if (!authenticated || !autosave) return;
    if (saving) {queued=true;return;}
    if (timer) clearTimeout(timer);
    timer = setTimeout(function () {timer=null;void save();}, 350);
  }
  saveButton.addEventListener("click", function () {void save();});
  loadButton.addEventListener("click", async function () {
    if (!authenticated) return;
    loadButton.disabled = true;
    try {
      const record = await accountRecord();
      if (!record) { message("No SailingGo preferences saved in this account."); return; }
      if (!valid(record.data)) throw Error("Saved account format unsupported; browser unchanged.");
      if (!window.confirm("Load your saved SailingGo settings? Current browser settings and selected map location will be replaced.")) return;
      const url = new URL(location.href);
      allowedParams.forEach(function (key) {url.searchParams.delete(key);});
      Object.entries(record.data.params).forEach(function (pair) {url.searchParams.set(pair[0],pair[1]);});
      try {sessionStorage.setItem(pendingKey, JSON.stringify(record.data));} catch (_) {throw Error("Browser session storage unavailable; settings not loaded.");}
      revision = record.revision;
      autosave = true;
      try {sessionStorage.setItem(optInKey, "yes"); sessionStorage.setItem(optInUserKey, account);} catch (_) {}
      location.assign(url.toString());
    } catch (err) {message(err.message);} finally {loadButton.disabled = !authenticated;}
  });
  function restoreMapChoices() {
    let saved;
    try {saved=JSON.parse(sessionStorage.getItem(pendingKey)||"null");sessionStorage.removeItem(pendingKey);} catch (_) {return;}
    if (!valid(saved)) return;
    if (saved.mapType) {
      const selected = Array.from(document.querySelectorAll('input[name="map-type"]')).find(function(el){return el.value===saved.mapType;});
      if (selected && !selected.checked) selected.click();
    }
    Object.entries(saved.overlays).forEach(function (pair) {
      const input=document.getElementById(pair[0]);
      if (input && input.checked!==pair[1]) input.click();
    });
    Object.entries(saved.marineCategories).forEach(function (pair) {
      const input=Array.from(document.querySelectorAll(".map-marine-place-toggle")).find(function(el){return el.getAttribute("data-marine-category")===pair[0];});
      if (input && input.checked!==pair[1]) input.click();
    });
  }
  // The dashboard registers map overlay handlers before this script executes.
  restoreMapChoices();
  panel.addEventListener("change", queueSave);
  document.addEventListener("change", function(event) {
    if (event.target.matches && (event.target.matches("input[name='map-type']") ||
       event.target.matches(".map-marine-place-toggle") ||
       overlayIds.includes(event.target.id))) queueSave();
  });
  async function validateSession() {
    if (!hostEnabled || !bearer) {disableAccount("Not signed in · browser-only settings (open from Homepage).");return;}
    try {
      const response = await request("/api/session/profile");
      if (!response.ok) throw Error("Authentication check failed (HTTP "+response.status+").");
      const profile = await response.json();
      if (typeof profile.user !== "string" || !profile.user || !(Date.parse(profile.expiresAt)>Date.now())) throw Error("Homepage session unavailable.");
      let allowedUser = "";
      try { allowedUser = sessionStorage.getItem(optInUserKey) || ""; } catch (_) {}
      if (allowedUser !== profile.user) {revision=null;autosave=false;try{sessionStorage.removeItem(optInKey);sessionStorage.removeItem(optInUserKey);}catch(_){}}
      account = profile.user;
      authenticated = true;
      saveButton.disabled = false;loadButton.disabled = false;
      message("Signed in as " + account + " · account preferences available.");
      if (autosave) {
        const existing=await accountRecord();
        if (existing && !valid(existing.data)) {autosave=false;message("Unsupported account settings; automatic saving disabled.");return;}
        revision=existing ? existing.revision : 0;
        if (navigationChanged) queueSave(); // Save changes that navigated/reloaded before the prior request finished.
      }
    } catch (err) {disableAccount("Account unavailable · browser settings retained.");}
  }
  void validateSession();
  window.addEventListener("focus", function(){void validateSession();});
})();
