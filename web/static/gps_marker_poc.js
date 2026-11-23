// Issue #41: opt-in foreground-only iPhone GPS marker POC.
// Embedded directly into the existing Leaflet map initialization scope.
// Does not change selected sailing/weather coordinates or send GPS to the server.
(function (map, L) {
  "use strict";
  if (!map || !L || !window.matchMedia("(max-width: 767px)").matches) return;
  if (!navigator.geolocation || !window.isSecureContext) return;

  var watchId = null, marker = null, accuracyCircle = null, following = true;
  var lastFixTime = 0;
  var control = L.control({ position: "bottomleft" });
  var button, followButton, status;

  function setStatus(message) {
    if (status) status.textContent = message;
  }
  function stop() {
    if (watchId !== null) {
      navigator.geolocation.clearWatch(watchId);
      watchId = null;
    }
    if (button) button.textContent = "Show My Position";
    if (followButton) followButton.style.display = "none";
    setStatus("GPS off");
  }
  function errorMessage(error) {
    if (error && error.code === 1) return "Location permission denied";
    if (error && error.code === 2) return "GPS position unavailable";
    if (error && error.code === 3) return "GPS timed out; waiting";
    return "GPS unavailable";
  }
  function update(position) {
    if (!position || !position.coords) return;
    var c = position.coords, lat = Number(c.latitude), lon = Number(c.longitude);
    if (!Number.isFinite(lat) || !Number.isFinite(lon) ||
        lat < -90 || lat > 90 || lon < -180 || lon > 180) return;
    var point = [lat, lon], accuracy = Number(c.accuracy);
    if (!marker) {
      marker = L.circleMarker(point, {
        radius: 9, color: "#ffffff", weight: 3, fillColor: "#0068d7",
        fillOpacity: 1, interactive: false, pane: "markerPane"
      }).addTo(map);
    } else marker.setLatLng(point);
    if (Number.isFinite(accuracy) && accuracy >= 0) {
      if (!accuracyCircle) accuracyCircle = L.circle(point, {
        radius: accuracy, color: "#0068d7", weight: 1,
        fillColor: "#0068d7", fillOpacity: 0.08, interactive: false
      }).addTo(map);
      else accuracyCircle.setLatLng(point).setRadius(accuracy);
    }
    lastFixTime = Number(position.timestamp) || Date.now();
    var age = Math.max(0, Math.round((Date.now() - lastFixTime) / 1000));
    setStatus("GPS ±" + (Number.isFinite(accuracy) ? Math.round(accuracy) + " m" : "?") +
      (age > 10 ? " · fix " + age + "s old" : ""));
    if (following) map.panTo(point, { animate: false });
  }
  function start() {
    if (watchId !== null) return;
    setStatus("Requesting GPS permission…");
    following = true;
    followButton.textContent = "Following";
    try {
      watchId = navigator.geolocation.watchPosition(update, function (err) {
        setStatus(errorMessage(err));
        if (err && err.code === 1) { stop(); setStatus(errorMessage(err)); }
      }, { enableHighAccuracy: true, maximumAge: 0, timeout: 15000 });
      button.textContent = "Stop GPS";
      followButton.style.display = "block";
    } catch (err) { setStatus("Unable to start GPS"); stop(); }
  }
  control.onAdd = function () {
    var panel = L.DomUtil.create("div", "sailing-gps-poc");
    panel.style.cssText = "background:#fff;color:#153242;border:1px solid #a4bdc9;border-radius:10px;padding:8px;box-shadow:0 2px 12px #0003;max-width:190px;font:13px system-ui";
    button = L.DomUtil.create("button", "", panel);
    button.type = "button"; button.textContent = "Show My Position";
    button.style.cssText = "display:block;width:100%;padding:9px;border-radius:6px;border:1px solid #147a9b;background:#e7f6fb;color:#153242;font:600 13px system-ui";
    followButton = L.DomUtil.create("button", "", panel);
    followButton.type = "button"; followButton.textContent = "Following";
    followButton.style.cssText = "display:block;width:100%;margin-top:5px;padding:7px;border-radius:6px;border:1px solid #aac0c9;background:#fff;color:#153242;font:13px system-ui";
    // Keep this button hidden until tracking begins.
    followButton.style.display = "none";
    status = L.DomUtil.create("div", "", panel);
    status.setAttribute("role", "status");
    status.setAttribute("aria-live", "polite");
    status.style.cssText = "margin-top:5px;font-size:12px";
    setStatus("GPS off");
    L.DomEvent.disableClickPropagation(panel);
    L.DomEvent.disableScrollPropagation(panel);
    L.DomEvent.on(button, "click", function (e) {
      L.DomEvent.stop(e);
      if (watchId !== null) stop(); else start();
      followButton.style.display = watchId !== null ? "block" : "none";
    });
    L.DomEvent.on(followButton, "click", function (e) {
      L.DomEvent.stop(e);
      following = !following;
      followButton.textContent = following ? "Following" : "Follow Boat";
      if (following && marker) map.panTo(marker.getLatLng(), { animate: false });
    });
    return panel;
  };
  control.addTo(map);
  // A manual pan pauses following, but programmatic panTo must not.
  map.on("dragstart", function () {
    if (watchId !== null) {
      following = false;
      if (followButton) followButton.textContent = "Follow Boat";
    }
  });
  document.addEventListener("visibilitychange", function () {
    if (watchId !== null && document.hidden) setStatus("Safari backgrounded; GPS may pause");
    else if (watchId !== null) setStatus("Waiting for a fresh GPS fix…");
  });
  window.addEventListener("pagehide", stop);
})(map, L);
