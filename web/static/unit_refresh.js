// Refresh server-rendered unit-dependent values without reloading the dashboard.
// Preserve Leaflet maps, event listeners, chart controllers and scroll position.
(function () {
  "use strict";
  let serial = 0;
  const skip = ".wind-readings, .location-map, .map-card, .offshore-trip-card, svg, canvas, script, style, input, select, button";
  function patch(current, incoming) {
    if (!current || !incoming || current.nodeType !== incoming.nodeType) return;
    if (current.nodeType === Node.TEXT_NODE) {
      if (current.nodeValue !== incoming.nodeValue) current.nodeValue = incoming.nodeValue;
      return;
    }
    if (current.nodeType !== Node.ELEMENT_NODE || current.tagName !== incoming.tagName ||
        current.matches(skip)) return;
    if (current.tagName === "A" && incoming.hasAttribute("href")) current.setAttribute("href", incoming.getAttribute("href"));
    const before = current.childNodes;
    const after = incoming.childNodes;
    if (before.length !== after.length) return; // Dynamic widgets are never replaced.
    for (let i = 0; i < before.length; i++) patch(before[i], after[i]);
  }
  function markSettled() {
    document.documentElement.classList.remove("sailing-units-pending");
  }
  async function refresh(nextURL, previousURL, windChanged) {
    const requestID = ++serial;
    try {
      const response = await fetch(nextURL.pathname + nextURL.search, {
        cache:"no-store", headers:{"X-Requested-With":"sailing-unit-refresh"}
      });
      if (!response.ok) throw Error("HTTP " + response.status);
      const doc = new DOMParser().parseFromString(await response.text(),"text/html");
      if (requestID !== serial) return;
      const oldCards = document.querySelectorAll(".grid > .card");
      const newCards = doc.querySelectorAll(".grid > .card");
      // Different card structures mean a data change requires a deliberate
      // later navigation; never destroy an interactive dashboard controller.
      if (oldCards.length !== newCards.length) throw Error("Dashboard structure changed");
      for (let i=0; i<oldCards.length; i++) {
        if (oldCards[i].classList.contains("map-card") ||
            oldCards[i].classList.contains("offshore-trip-card")) continue;
        patch(oldCards[i],newCards[i]);
      }
      // Live data can change the number of server-rendered child nodes.
      // Refresh the stable wind metrics even when the general tree differs.
      for (const selector of ["#bottom-line-card .metric .value", ".wind-card .metric .value"]) {
        const current = document.querySelectorAll(selector);
        const updated = doc.querySelectorAll(selector);
        if (current.length !== updated.length) continue;
        for (let i=0; i<current.length; i++) current[i].textContent = updated[i].textContent;
      }
      // Existing wind-history code owns its table/chart and already refreshes
      // from the JSON endpoint in the selected unit, preserving handlers.
      if (windChanged) {
        const hours = document.getElementById("wind-reading-hours");
        if (hours) hours.dispatchEvent(new Event("change",{bubbles:true}));
      }
    } catch (error) {
      if (requestID !== serial) return;
      // Never leave newly selected units beside stale values.
      const wind = document.getElementById("wind-unit-select");
      const distance = document.getElementById("distance-unit-select");
      if (wind) wind.value = previousURL.searchParams.get("wind_unit") === "mph" ? "mph" : "kts";
      if (distance) distance.value = previousURL.searchParams.get("distance_unit") === "mi" ? "mi" : "nmi";
      history.replaceState(history.state,"",previousURL.pathname+previousURL.search);
      let note = document.getElementById("sailing-unit-error");
      if (!note) {
        note=document.createElement("span");note.id="sailing-unit-error";
        note.setAttribute("role","status");note.style.cssText="color:#f4aa86;font-size:12px";
        document.querySelector(".page-preferences")?.append(note);
      }
      note.textContent = "Could not refresh units; try again.";
      console.warn("SailingGo unit update failed",error);
    } finally {
      if (requestID === serial) markSettled();
    }
  }
  window.sailingRefreshUnits = function (nextURL, previousURL, windChanged) {
    return refresh(new URL(nextURL,location.href),new URL(previousURL,location.href),!!windChanged);
  };
  window.addEventListener("sailing:unit-change",function (event) {
    const next = new URL(event.detail.url,location.href);
    const previous = new URL(event.detail.previous,location.href);
    void refresh(next,previous,event.detail.key==="wind_unit");
  });
  // A network outage must never leave the dashboard permanently hidden.
  setTimeout(markSettled,4000);
})();
