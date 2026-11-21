'use strict';
// Node.js is available in GitHub CI only; Mac build remains Go-only.
const assert=require('node:assert/strict');
const fs=require('node:fs');
const vm=require('node:vm');
const source=fs.readFileSync('web/static/account.js','utf8');
new vm.Script(source,{filename:'sailing-account.js'});
const guestStart=source.indexOf('  function saveLocal(data) {');
const guestEnd=source.indexOf('  // A handoff is transferred only through the fragment.',guestStart);
const stageStart=source.indexOf('  // Staging is separate from persistence:');
const stageEnd=source.indexOf('  async function hydrate() {',stageStart);
assert.ok(guestStart>=0&&guestEnd>guestStart&&stageStart>=0&&stageEnd>stageStart);
const comparisonStart=source.indexOf('  // Compare canonical preference state,');
const comparisonEnd=source.indexOf('  function actualParameterChanges(',comparisonStart);
assert.ok(comparisonStart>=0 && comparisonEnd>comparisonStart);
const settingsStart=source.indexOf('  function settingsFromPage(fallback) {');
const settingsEnd=source.indexOf('  function valid(data) {',settingsStart);
const eventStart=source.indexOf('  document.addEventListener("change",function (event) {');
const eventEnd=source.indexOf('  // A page-navigation change is recovered',eventStart);
assert.ok(settingsStart>=0&&settingsEnd>settingsStart&&eventStart>=0&&eventEnd>eventStart);
const headerStart=source.indexOf('  const urlParams = [');
const headerEnd=source.indexOf('  const onPublicHost =',headerStart);
assert.ok(headerStart>=0 && headerEnd>headerStart);
const controlHeader=source.slice(headerStart,headerEnd);
const operations=controlHeader+'\n'+source.slice(comparisonStart,comparisonEnd)+'\n'+
  source.slice(guestStart,guestEnd)+'\n'+source.slice(stageStart,stageEnd);
const restoreStart=source.indexOf('  // Restore UI-only selections through');
const restoreEnd=source.indexOf('  async function applySettings(',restoreStart);
assert.ok(restoreStart>=0 && restoreEnd>restoreStart);
const restoreOperations=source.slice(restoreStart,restoreEnd);
const realEventOperations=source.slice(settingsStart,settingsEnd)+'\n'+
  source.slice(eventStart,eventEnd);
assert.ok(!source.includes('setTimeout(function(){pendingSave=null;void flush()'));
assert.ok(!source.includes('setTimeout(function () { pendingSave=null;void flush()'));
assert.ok(!source.includes('await flush();'), 'account hydration must not save automatically');

// Inventory every actual dropdown/checkbox rendered in the complete report
// template, not just the controls manually represented by this test fixture.
const report=fs.readFileSync('web/templates/sailing.html','utf8');
const selects=[...report.matchAll(/<select\b[^>]*id="([^"]+)"[^>]*>/g)].map(x=>x[1]);
const checks=[...report.matchAll(/<input\b[^>]*type="checkbox"[^>]*>/g)];
const boxIDs=checks.map(x=>x[0].match(/\bid="([^"]+)"/)?.[1]||x[0].match(/data-marine-category="([^"]+)"/)?.[1]);
const expectedSelects=[
 'wind-unit-select','distance-unit-select','map-swell-playback-speed',
 'map-swell-playback-step','map-alertcalifornia-mode','wind-reading-hours',
 'current-days-picker'];
const expectedChecks=[
 'map-show-marine-zone','map-show-smoke','map-show-fire-perimeters','map-show-fire-detections',
 'map-show-air-temperature','map-show-clouds','map-show-radar','map-show-wind-barbs',
 'map-show-inland-wind-barbs','map-show-pressure','map-show-swell','map-show-structure',
 'marinas','boatyards','fuel_docks','launch_ramps','ferry_terminals',
 'marine_supply','yacht_clubs','waterfront_restaurants','map-show-saildrone',
 'map-show-alertcalifornia','show-tide-range-overlay'];
assert.deepEqual(selects.sort(),expectedSelects.sort(),'all report dropdowns must have explicit preference coverage');
assert.deepEqual(boxIDs.sort(),expectedChecks.sort(),'all report checkboxes must have explicit preference coverage');
assert.ok(report.includes('document.dispatchEvent(new Event("sailing-current-chart-updated"))'),
 'AJAX chart replacement must restore saved/pending checkbox state');

const makeFixture=({guest=false,failSave=false,conflict=false}={})=>{
 const store=new Map(),requests=[],notices=[];
 const saveButton={disabled:true,addEventListener(name,fn){this.click=fn}};
 const discardButton={disabled:true,addEventListener(name,fn){this.click=fn}};
 const saved={schema:1,params:{wind_unit:'kts'},overlays:{},mapType:'map',marineCategories:{}};
 const restored=[];
 const ctx={
  saveButton,discardButton,savedSnapshot:JSON.parse(JSON.stringify(saved)),
  window:{addEventListener(name,fn){if(name==='beforeunload')ctx.beforeunload=fn}},
  document:{documentElement:{classList:{remove(){}}},addEventListener(name,handler){this['on_'+name]=handler}},
  guestIdentity:guest?'guest-001':'',
  overlays:['map-show-radar','map-show-smoke'],
  signedIn:!guest,user:guest?'':'reader',initializing:false,applying:false,
  blocked:false,saving:false,dirty:false,suppressUnload:false,
  revision:3,baseline:JSON.parse(JSON.stringify(saved)),
  draftKey:'sailing.preferences.pending-account.v2',
  guestDraftKey:'sailing.preferences.pending-guest.v1',
  localKey:'sailing.preferences.browser.v2',
  localStorage:{
   setItem(key,value){if(failSave)throw Error('quota exceeded');store.set('local:'+key,value)},
  },
  getJSON(key,local){try{return JSON.parse(store.get((local?'local:':'session:')+key)||'null')}catch(_){return null}},
  putJSON(key,data,local){if(local&&failSave)return false;store.set((local?'local:':'session:')+key,JSON.stringify(data));return true},
  clearSession(key){store.delete('session:'+key)},
  valid(value){return !!value&&value.schema===1},
  updateSaveUI(message){
    notices.push(message);
    saveButton.disabled=ctx.saving||ctx.blocked||!ctx.dirty;
    discardButton.disabled=ctx.saving||(!ctx.dirty&&!ctx.blocked);
  },
  signedInStatus(){},
  applySettings(data,identity){restored.push({data,identity});return Promise.resolve()},
  async request(path,options){requests.push({path,options});return conflict
   ? {status:409,ok:false}
   : {status:200,ok:true,json:async()=>({revision:4})}},
  accountStatus(){},
  Promise,JSON,Error,String,Number,Event:class{constructor(type,options){this.type=type;this.bubbles=!!options?.bubbles}},
 };
 vm.createContext(ctx);
 vm.runInContext(operations+'\n'+restoreOperations,ctx);
 return {ctx,store,requests,notices,saveButton,discardButton,restored,saved};
};
// A current-chart preference edit is an AJAX update, never a full-page
// navigation on network or authentication failure. Exercise the actual refresh
// function with a failing fetch and a navigation spy.
const chartSource=fs.readFileSync('web/templates/sailing.html','utf8');
const chartStart=chartSource.indexOf('  async function loadCurrentURL(target, pushHistory) {');
const chartEnd=chartSource.indexOf('  document.addEventListener("click", function(e) {',chartStart);
assert.ok(chartStart>=0 && chartEnd>chartStart,'chart fetch controller must exist');
assert.ok(!chartSource.slice(chartStart,chartEnd).includes('location.assign('),
 'AJAX chart update must not cause a full-page reload on failure');
assert.ok(!chartSource.includes('.current-refreshing{opacity:.55'),
 'changing chart preferences must not fade all report cards');

async function assertChartFailureKeepsPage(){
 const logs=[],original='https://weather.themauris.org/report?format=html&planning=1';
 const ctx={
  URL,Error,Promise,requestSerial:0,
  fetch:async()=>{throw Error('simulated server failure')},
  setRefreshing:on=>logs.push('busy:'+on),
  currentRefreshStatus:message=>logs.push('status:'+message),
  window:{location:{href:original,assign(){throw Error('navigation prohibited')}}},
  history:{pushState(){throw Error('history should not change on failure')}},
  document:{},DOMParser:class{},
 };
 vm.createContext(ctx);
 vm.runInContext('var requestSerial=0;'+chartSource.slice(chartStart,chartEnd),ctx);
 await ctx.loadCurrentURL(new URL(original),true);
 assert.equal(ctx.window.location.href,original,'failed refresh must preserve page URL');
 assert.ok(logs.some(s=>s.includes('Previous results remain visible')),
   'failed AJAX should report a non-destructive inline error');
 assert.ok(logs.includes('busy:false'),'chart status must reset after failure');
}
// Exercise the real Leaflet base-map switch logic with fake tile layers.
// Nautical must keep Street below the NOAA chart and revert to Street if
// its tiles fail; radar must use a transparent PNG cache (not opaque WMS).
function testMapTileFallback(){
 const source=fs.readFileSync('web/templates/sailing.html','utf8');
 const a=source.indexOf('  function applyPreferredBaseMap() {');
 const b=source.indexOf('  function setBaseMapLayer(name) {',a);
 const c=source.indexOf('\n  applyPreferredBaseMap();\n',b);
 assert.ok(a>=0&&b>a&&c>b,'actual basemap implementation must be available');
 const makeLayer=name=>({
  name,addTo(map){map.layers.add(this);map.order.push(this.name);return this},
  bringToFront(){map.order.push(this.name)},setStyle(){}
 });
 const map={layers:new Set(),order:[],hasLayer(x){return this.layers.has(x)},removeLayer(x){this.layers.delete(x)},getZoom(){return 10}};
 const streetLayer=makeLayer('street');
 const nauticalLayer=makeLayer('nautical');
 const satelliteLayer=makeLayer('satellite');
 const hybridLayer=makeLayer('hybrid');
 const radio=[{value:'map'},{value:'nautical'},{value:'satellite'}];
 let timeoutStarts=0;
 const context={
  map,streetLayer,nauticalLayer,satelliteLayer,hybridLayer,
  preferredMapLayerName:'nautical',activeMapLayerName:'map',
  nauticalUnavailable:false,nauticalCheckTimer:null,nauticalAdvisory:false,nauticalTileLoads:0,
  nauticalAutomaticFallback:false,nauticalFailureNotice:false,nauticalStatusMessage:'',minimumNauticalZoom:9,
  nauticalMockInspection:false,
  mapState:{cloudOverlayVisible:false,radarOverlayVisible:false},
  cloudLayer:null,radarLayer:null,marineZoneHaloLayer:null,
  marineZoneLayer:null,smokeLayer:null,smokeOutlineLayer:null,
  updateNauticalZoomUI(){},updateSmokeLegendForBaseMap(){},
  startNauticalResponseCheck(){timeoutStarts++;context.nauticalCheckTimer=1;},
  stopNauticalWaiting(){context.nauticalCheckTimer=null;},
  dismissNauticalFailureNotice(){context.nauticalFailureNotice=false;context.nauticalStatusMessage='';},
  clearTimeout(){context.nauticalCheckTimer=null;},
  persistPreferredMapLayer(){},
  document:{querySelectorAll(){return radio}},
  effectiveMapLayerName(){return context.preferredMapLayerName},
  baseLayerForName(name){return {map:streetLayer,nautical:nauticalLayer,satellite:satelliteLayer,hybrid:hybridLayer}[name]},
 };
 vm.createContext(context);
 vm.runInContext(source.slice(a,b),context);
 vm.runInContext(source.slice(b,c),context);
 context.applyPreferredBaseMap();
 assert.equal(timeoutStarts,1,'initial saved Nautical must arm timeout without Leaflet loading');
 context.applyPreferredBaseMap();
 assert.equal(timeoutStarts,1,'reapplying Nautical must not restart the same timeout');
 assert.ok(map.hasLayer(streetLayer),'nautical must retain street underlay');
 assert.ok(map.hasLayer(nauticalLayer),'nautical layer should render over street');
 assert.equal(context.activeMapLayerName,'nautical');
 assert.ok(map.order.indexOf('street')<map.order.indexOf('nautical'),
  'Street must load before chart tiles');
 context.nauticalUnavailable=true;
 context.applyPreferredBaseMap();
 assert.ok(map.hasLayer(streetLayer),'NOAA failure must leave Street visible');
 assert.ok(!map.hasLayer(nauticalLayer),'failed NOAA chart must be removed');
 assert.equal(context.preferredMapLayerName,'nautical',
  'fallback must not silently change saved preference');
 assert.equal(context.activeMapLayerName,'map');
 assert.equal(context.nauticalCheckTimer,null,'fallback cancels outstanding timeout');
 context.preferredMapLayerName='satellite';
 context.applyPreferredBaseMap();
 assert.ok(map.hasLayer(satelliteLayer),'satellite selection remains intact');
 assert.ok(!map.hasLayer(streetLayer),'other basemaps should not keep Street');
 context.setBaseMapLayer('nautical');
 assert.equal(timeoutStarts,2,'manual Nautical selection must arm timeout without loading event');
 assert.ok(map.hasLayer(nauticalLayer),'manual Nautical retry should attempt chart display');
 assert.equal(context.preferredMapLayerName,'nautical','retry retains Nautical preference');
 assert.ok(source.includes('showNauticalFailureNotice("NOAA nautical charts are temporarily unavailable. Showing Street Map instead; please try again later.");'),
  'a complete NOAA nautical tile failure must show a dismissible retry message');
 assert.ok(source.includes('nauticalStatusText.textContent = nauticalFailureNotice'),
  'the failure notice must be displayed through the existing nautical status UI');
 assert.ok(source.includes('L.tileLayer("/api/charts/tiles/{z}/{x}/{y}.png"'),
  'NOAA tiles must always be fetched through the local SailingGo Go endpoint');
 assert.ok(!source.includes('gis.charttools.noaa.gov'),
  'browser must not call NOAA chart WMS directly');
 assert.ok(source.includes('ridge::USCOMP-N0Q-0/{z}/{x}/{y}.png'),
  'radar must use IEM cached NEXRAD transparency-preserving tiles');
 assert.ok(!source.includes('L.tileLayer.wms(\n    "https://mesonet.agron.iastate.edu/cgi-bin/wms/nexrad/n0q.cgi?'),
  'old problematic WMS radar tiles must not return');
 assert.ok(source.includes('radarTileErrors > 0'),
  'partial radar tile failures must be visible to the user');
}
// Exercise actual NOAA tile-event logic, including HTTP-successful images
// which may be visually blank, and requests that never complete.
function testNauticalResponseDiagnostics(){
 const source=fs.readFileSync('web/templates/sailing.html','utf8');
 const a=source.indexOf('  var nauticalTileLoads = 0;');
 const b=source.indexOf('  function persistPreferredMapLayer() {',a);
 assert.ok(a>=0&&b>a,'real NOAA tile and notice code must be present');

 const callbacks={};
 const nauticalLayer={on(name,fn){callbacks[name]=fn;}};
 const note={hidden:true}, statusText={textContent:''}, progress={hidden:true};
 const cancelButton={hidden:true,addEventListener(name,fn){this[name]=fn;}};
 const closeButton={hidden:true,addEventListener(name,fn){this[name]=fn;}};
 const typeInput={disabled:false,setAttribute(){},title:''};
 const label={classList:{toggle(){}}};
 let timer=null, updates=0, fallbackEvents=0, stagedMapType='nautical';
 const timers=new Map();
 let timerId=0;
 const streetInput={
  checked:false,
  dispatchEvent(ev){
   assert.equal(ev.type,'change');
   assert.equal(ev.bubbles,true);
   stagedMapType='map'; // the real capture listener stages Street, does not Save.
   fallbackEvents++;
   context.preferredMapLayerName='map';
   context.updateNauticalZoomUI();
   return true;
  },
 };
 const context={
  nauticalLayer, preferredMapLayerName:'nautical',
  nauticalZoomNote:note,nauticalStatusText:statusText,
  nauticalCancelButton:cancelButton,nauticalCloseButton:closeButton,
  nauticalProgress:progress,nauticalTypeInput:typeInput,
  nauticalTypeLabel:label,minimumNauticalZoom:9,
  nauticalMockInspection:false,
  map:{hasLayer(x){return x===nauticalLayer},getZoom(){return 10}},
  document:{querySelector(sel){
   if(sel==='input[name="map-type"][value="map"]')return streetInput;
   throw new Error('unexpected selector: '+sel);
  }},
  Event:class Event{constructor(type,opts){this.type=type;this.bubbles=opts.bubbles}},
  setTimeout(fn,ms){timer=fn;const id=++timerId;timers.set(id,{fn,ms});return id;},
  clearTimeout(id){timers.delete(id);timer=null;},
  updateNauticalZoomUI(){updates++;return context.realUpdateNauticalZoomUI();},
  setBaseMapLayer(name){context.preferredMapLayerName=name;},
 };
 vm.createContext(context);
 vm.runInContext(source.slice(a,b),context);
 context.realUpdateNauticalZoomUI=()=>vm.runInContext('updateNauticalZoomUI()',context);
 // The real timeout code must show immediate feedback before NOAA answers.
 assert.equal(typeof callbacks.loading,'function');
 callbacks.loading();
 assert.equal(typeof timer,'function','tile loading should arm bounded response check');
 assert.equal(note.hidden,false,'progress must appear as soon as loading begins');
 assert.ok(statusText.textContent.includes('Loading NOAA nautical chart'));
 assert.equal(cancelButton.hidden,false,'loading must offer Cancel');
 assert.equal(progress.hidden,false,'indeterminate progress bar appears while waiting');
 timer();
 assert.equal(context.nauticalFailureNotice,true,'stalled WMS must record visible warning');
 assert.equal(fallbackEvents,1,'timeout must issue one staged Street radio event');
 assert.equal(stagedMapType,'map','Street is a pending preference, not an automatic Save');
 assert.equal(context.preferredMapLayerName,'map','timeout must reset Street radio choice');
 assert.equal(context.nauticalCheckTimer,null,'timeout must not leave a running timer');
 assert.equal(note.hidden,false,'warning must remain visible after switching radio to Street');
 assert.equal(cancelButton.hidden,true,'Cancel hidden after timeout');
 assert.equal(progress.hidden,true,'progress bar hidden after timeout');
 assert.equal(closeButton.hidden,false,'same status panel offers Close after failure');
 assert.ok(statusText.textContent.includes('not responding'));
 assert.ok([...timers.values()].some(t=>t.ms===8000),
  'failure status should be automatically dismissed');
 closeButton.click();
 assert.equal(note.hidden,true,'Close dismisses failure in the same panel');
 assert.equal(context.nauticalFailureNotice,false,'Close clears failure state');
 assert.equal(closeButton.hidden,true,'Close hides itself');
 assert.ok(![...timers.values()].some(t=>t.ms===8000),
  'manual Close cancels automatic dismissal timer');

 // The same error panel automatically dismisses without requiring Close.
 context.preferredMapLayerName='nautical';
 callbacks.loading();
 timer();
 const autoDismiss=[...timers.values()].find(t=>t.ms===8000);
 assert.ok(autoDismiss,'timeout should schedule automatic dismissal');
 autoDismiss.fn();
 assert.equal(note.hidden,true,'final failure popup must not stay forever');
 assert.equal(context.nauticalFailureNotice,false);
 // Manual Cancel must also reset Street, without displaying the outage warning.
 context.preferredMapLayerName='nautical';
 context.nauticalFailureNotice=false;
 context.nauticalStatusMessage='';
 callbacks.loading();
 assert.equal(note.hidden,false);
 cancelButton.click();
 assert.equal(fallbackEvents,3,'Cancel must reset radio through staged change');
 assert.equal(context.nauticalFailureNotice,false);
 assert.equal(note.hidden,true,'Cancel removes loading message');
 assert.equal(progress.hidden,true,'Cancel removes busy progress bar');

 // A tile that arrived must not be labeled as a hard outage.
 context.preferredMapLayerName='nautical';
 callbacks.loading();
 callbacks.tileload();
 timer();
 assert.equal(context.nauticalFailureNotice,false,'successful image is not proof of outage');
 assert.equal(context.nauticalAdvisory,false,'successful tile must not leave an advisory');
 assert.equal(note.hidden,true,'a successful tile must clear the status panel');
 assert.equal(progress.hidden,true,'successful tile must clear the progress bar');

 // A separate, explicitly opted-in loopback mock inspector must not trigger
 // NOAA failure-to-Street events when only some fixture tiles exist.
 context.nauticalMockInspection=true;
 context.preferredMapLayerName='nautical';
 const fallbackBeforeMock=fallbackEvents;
 callbacks.loading();
 assert.equal(note.hidden,true,'mock mode suppresses misleading NOAA progress');
 assert.equal(context.nauticalCheckTimer,null,'mock mode never starts the 12s timer');
 callbacks.tileerror();
 callbacks.load();
 assert.equal(fallbackEvents,fallbackBeforeMock,'missing mock tile must not force Street');
 assert.equal(context.preferredMapLayerName,'nautical','mock inspection retains Nautical choice');
 assert.equal(note.hidden,true,'mock inspection does not obstruct map');
 context.showNauticalFailureNotice('simulated miss');
 assert.equal(fallbackEvents,fallbackBeforeMock,'mock mode suppresses generic fallback');
 assert.equal(context.nauticalFailureNotice,false,'mock mode suppresses failure popup');
 context.nauticalMockInspection=false;
 assert.ok(source.includes('window.location.hostname === "localhost"') &&
  source.includes('window.location.hostname === "127.0.0.1"') &&
  source.includes('get("nautical_mock_test") === "1"'),
  'mock inspection must require both explicit URL opt-in and loopback host');
 assert.ok(source.includes('if (showNautical && !nauticalMockInspection'),
  'mock mode must not arm a timeout during map reapplication');
 assert.ok(source.includes('white-space:normal;max-width:min(520px'),
  'long notice must wrap rather than be clipped');
}
async function run(){
 testMapTileFallback();
 testNauticalResponseDiagnostics();
 await assertChartFailureKeepsPage();
 const personal=makeFixture();
 const next={schema:1,params:{wind_unit:'mph'},overlays:{},mapType:'map',marineCategories:{}};
 personal.ctx.stage(next);
 assert.equal(personal.ctx.dirty,true);
 assert.equal(personal.requests.length,0,'staging must not persist');
 assert.equal(personal.store.has('session:'+personal.ctx.draftKey),true);
 const leave={preventDefault(){this.prevented=true},returnValue:null};
 personal.ctx.beforeunload(leave);
 assert.equal(leave.prevented,true,'pending preferences must warn on leave');
 personal.saveButton.click();
 // Wait for the explicit save promise chain through the bounded test microtasks.
 await new Promise(resolve=>setImmediate(resolve));
 assert.equal(personal.requests.length,1,'exactly one explicit Homepage PUT');
 assert.equal(personal.requests[0].options.method,'PUT');
 assert.equal(personal.ctx.dirty,false,'saved personal preferences clear dirty');
 assert.equal(personal.store.has('session:'+personal.ctx.draftKey),false);
 const again={preventDefault(){this.prevented=true}};
 personal.ctx.beforeunload(again);
 assert.equal(again.prevented,undefined,'no warning after successful save');
 const guest=makeFixture({guest:true});
 guest.ctx.stageGuest(next);
 assert.equal(guest.requests.length,0);
 assert.equal(guest.store.has('local:'+guest.ctx.localKey),false,'Guest local settings must not auto-commit');
 guest.saveButton.click();
 await new Promise(resolve=>setImmediate(resolve));
 assert.equal(guest.requests.length,0,'Guest must not PUT to Homepage');
 assert.equal(guest.ctx.dirty,false);
 assert.ok(guest.store.has('local:'+guest.ctx.localKey));
 const conflict=makeFixture({conflict:true});
 conflict.ctx.stage(next);conflict.saveButton.click();
 await new Promise(resolve=>setImmediate(resolve));
 assert.equal(conflict.ctx.blocked,true,'409 must block overwriting account');
 assert.equal(conflict.ctx.dirty,true,'409 retains staged changes');
 assert.ok(conflict.store.has('session:'+conflict.ctx.draftKey));
 const failed=makeFixture({guest:true,failSave:true});
 failed.ctx.stageGuest(next);failed.saveButton.click();
 await new Promise(resolve=>setImmediate(resolve));
 assert.equal(failed.ctx.dirty,true,'browser quota failure must retain pending Guest');
 assert.ok(failed.store.has('session:'+failed.ctx.guestDraftKey));
 assert.ok(failed.notices.some(x=>typeof x==='string'&&/Save failed/.test(x)));
 // A net-zero change must clear the draft, disable both controls, and
 // suppress Safari's leave warning without writing preferences anywhere.
 const reversed=makeFixture();
 const nautical={...reversed.saved,mapType:'nautical'};
 reversed.ctx.stage(nautical);
 assert.equal(reversed.ctx.dirty,true,'Street -> Nautical should be pending');
 assert.equal(reversed.saveButton.disabled,false,'save enabled for a real edit');
 reversed.ctx.stage({...reversed.saved,mapType:'map'});
 assert.equal(reversed.ctx.dirty,false,'Street -> Nautical -> Street is not a change');
 assert.equal(reversed.saveButton.disabled,true,'save disabled after undo');
 assert.equal(reversed.discardButton.disabled,true,'discard disabled after undo');
 assert.equal(reversed.store.has('session:'+reversed.ctx.draftKey),false);
 const noWarning={preventDefault(){this.prevented=true}};
 reversed.ctx.beforeunload(noWarning);
 assert.equal(noWarning.prevented,undefined,'no leave warning for net-zero changes');
 assert.equal(reversed.requests.length,0,'no PUT on canceling a preference change');

 const guestReversed=makeFixture({guest:true});
 guestReversed.ctx.stageGuest(nautical);
 guestReversed.ctx.stageGuest({...guestReversed.saved,mapType:'map'});
 assert.equal(guestReversed.ctx.dirty,false,'Guest should detect net-zero map type');
 assert.equal(guestReversed.store.has('session:'+guestReversed.ctx.guestDraftKey),false);
 assert.equal(guestReversed.store.has('local:'+guestReversed.ctx.localKey),false,'undo must not commit Guest settings');

 // Discard restores saved settings in the active view and clears tab drafts.
 const discard=makeFixture();
 discard.ctx.stage(nautical);
 discard.discardButton.click();
 assert.equal(discard.ctx.dirty,false);
 assert.equal(discard.saveButton.disabled,true);
 assert.equal(discard.discardButton.disabled,true);
 assert.equal(discard.store.has('session:'+discard.ctx.draftKey),false);
 assert.equal(discard.restored.length,1,'discard must restore the saved view');
 assert.equal(discard.restored[0].data.mapType,'map');
 assert.equal(discard.requests.length,0,'discard must not call Homepage');

 const discardGuest=makeFixture({guest:true});
 discardGuest.ctx.stageGuest(nautical);
 discardGuest.discardButton.click();
 assert.equal(discardGuest.ctx.dirty,false);
 assert.equal(discardGuest.store.has('session:'+discardGuest.ctx.guestDraftKey),false);
 assert.equal(discardGuest.store.has('local:'+discardGuest.ctx.localKey),false,'discard must not save Guest changes');
 assert.equal(discardGuest.restored[0].data.mapType,'map');

 // A 409 leaves the user a safe way out, instead of forcing an overwrite.
 conflict.discardButton.click();
 assert.equal(conflict.ctx.blocked,false);
 assert.equal(conflict.ctx.dirty,false);
 assert.equal(conflict.store.has('session:'+conflict.ctx.draftKey),false);
 assert.equal(conflict.requests.length,1,'discard after 409 never retries PUT');

 // Exercise the ACTUAL capturing event handler, not only manually created
 // stage() payloads. The map controller writes the map_layer URL AFTER this
 // capture handler. On the return to Street the URL still says nautical.
 for(const guestMode of [false,true]){
 for(const legacyMapType of ['map','']){
  const ui=makeFixture({guest:guestMode});
  // Older records saved before a map control was visited may use an empty
  // mapType, despite the displayed default radio being Street.
  ui.ctx.savedSnapshot.mapType=legacyMapType;
  ui.ctx.baseline.mapType=legacyMapType;
  let radio={value:'map',matches(selector){return selector==="input[name='map-type']";}};
  // Runtime uses the full selector/checkbox registry from the production script.
  // Saved records may predate an overlay/category and omit their false keys.
  // The real page still supplies those unchecked DOM controls on every edit.
  ui.saved.overlays['map-show-radar']=true;
  ui.ctx.savedSnapshot.overlays['map-show-radar']=true;
  ui.ctx.baseline.overlays['map-show-radar']=true;
  const controls={
   'map-show-radar':{checked:true},
   'map-show-smoke':{checked:false},
  };
  const category={checked:false,getAttribute(name){return name==='data-marine-category'?'harbors':null}};
  // Map viewport URL state is present but was not changed by the radio event.
  ui.ctx.location={href:'https://weather.themauris.org/planning?wind_unit=kts&map_center_lat=38.01'};
  ui.ctx.URL=URL;
  const redrawHandler=ui.ctx.document['on_sailing-current-chart-updated'];
  ui.ctx.document={
   querySelector(selector){return selector==='input[name="map-type"]:checked'?radio:null},
   querySelectorAll(selector){return selector==='.map-marine-place-toggle'?[category]:[]},
   getElementById(id){return controls[id]||null},
   addEventListener(name,handler){this['on_'+name]=handler;if(name==='change')this.changeHandler=handler},
  };
  vm.runInContext(realEventOperations,ui.ctx);
  assert.equal(typeof ui.ctx.document.changeHandler,'function');
  radio.value='nautical';
  ui.ctx.document.changeHandler({target:radio});
  assert.equal(ui.ctx.dirty,true,'real DOM Street -> Nautical must be dirty');
  // Emulate the later map-controller history.replaceState.
  ui.ctx.location.href='https://weather.themauris.org/planning?wind_unit=kts&map_center_lat=38.01&map_layer=nautical';
  radio.value='map';
  ui.ctx.document.changeHandler({target:radio});
  assert.equal(ui.ctx.dirty,false,'real DOM Nautical -> Street must clear dirty');
  assert.equal(ui.saveButton.disabled,true,'real map undo must disable Save');
  assert.equal(ui.discardButton.disabled,true,'real map undo must disable Discard');
  assert.equal(ui.store.has('session:'+(guestMode?ui.ctx.guestDraftKey:ui.ctx.draftKey)),false,'real map undo clears draft');
  assert.equal(ui.requests.length,0,'real map undo must not save');
  // An actual overlay edit must still be detected, then clear when undone.
  controls['map-show-smoke'].checked=true;
  const smoke={id:'map-show-smoke',get checked(){return controls['map-show-smoke'].checked},matches(){return false}};
  ui.ctx.document.changeHandler({target:smoke});
  assert.equal(ui.ctx.dirty,true,'enabling an overlay must remain a real change');
  controls['map-show-smoke'].checked=false;
  ui.ctx.document.changeHandler({target:smoke});
  assert.equal(ui.ctx.dirty,false,'overlay reversal must return to saved state');
  category.checked=true;
  category.matches=selector=>selector==='.map-marine-place-toggle';
  ui.ctx.document.changeHandler({target:category});
  assert.equal(ui.ctx.dirty,true,'marine category toggle must stage its own value');
  category.checked=false;
  ui.ctx.document.changeHandler({target:category});
  assert.equal(ui.ctx.dirty,false,'marine category reversal must clear dirty');
  // The tidal-current Range select performs an AJAX chart refresh rather
  // than full-page navigation. Its change must be staged before that refresh.
  const range={id:'current-days-picker',value:'3',matches(){return false}};
  ui.ctx.document.changeHandler({target:range});
  assert.equal(ui.ctx.dirty,true,'1 day -> 3 days must enable Save');
  assert.equal(ui.saveButton.disabled,false,'tidal current Range enables Save');
  range.value='7';
  ui.ctx.document.changeHandler({target:range});
  assert.equal(ui.ctx.getJSON(guestMode?ui.ctx.guestDraftKey:ui.ctx.draftKey,false).data.params.current_days,'7');
  range.value='1';
  ui.ctx.document.changeHandler({target:range});
  assert.equal(ui.ctx.dirty,false,'returning to default 1 day clears dirty');
  assert.equal(ui.saveButton.disabled,true,'restored tidal range disables Save');
  assert.equal(ui.requests.length,0,'tidal range changes cannot auto-save');
  ui.ctx.savedSnapshot.params.current_days='3';
  range.value='7';
  ui.ctx.document.changeHandler({target:range});
  assert.equal(ui.ctx.dirty,true,'saved 3 days -> 7 days must enable Save');
  range.value='3';
  ui.ctx.document.changeHandler({target:range});
  assert.equal(ui.ctx.dirty,false,'restoring saved 3-day range clears dirty');
  // Wind History defaults to 4 hours; sparse legacy records and an explicit
  // wind_hours=4 must compare equal even after 4 -> 8 -> 4 transitions.
  const history={id:'wind-reading-hours',value:'8',matches(){return false}};
  ui.ctx.document.changeHandler({target:history});
  assert.equal(ui.ctx.dirty,true,'History 4h -> 8h enables Save');
  history.value='4';
  ui.ctx.document.changeHandler({target:history});
  assert.equal(ui.ctx.dirty,false,'History 8h -> 4h clears Save');
  assert.equal(ui.saveButton.disabled,true);
  ui.ctx.savedSnapshot.params.wind_hours='4'; // Historical explicit default.
  history.value='12';
  ui.ctx.document.changeHandler({target:history});
  assert.equal(ui.ctx.dirty,true,'explicit default to 12h marks dirty');
  history.value='4';
  ui.ctx.document.changeHandler({target:history});
  assert.equal(ui.ctx.dirty,false,'explicit 4h default restores saved state');
  delete ui.ctx.savedSnapshot.params.wind_hours;

  // All eight planning inputs use one-field state deltas. Their live chart
  // controller refreshes via AJAX after this capture handler runs.
  const planningCases=[
   ['planning-start','12:00','13:30','planning_start'],
   ['planning-end','17:00','18:15','planning_end'],
   ['planning-caution-ebb','2.0','2.5','caution_ebb'],
   ['planning-caution-flood','2.0','1.9','caution_flood'],
   ['planning-max-ebb','3.0','3.5','max_ebb'],
   ['planning-max-flood','3.0','3.7','max_flood'],
   ['planning-buffer','60','75','planning_buffer'],
   ['planning-current-distance-warning','15.0','12.5','current_distance_warning'],
  ];
  for(const [id,original,changed,key] of planningCases){
   const input={id,value:changed,matches(){return false}};
   ui.ctx.document.changeHandler({target:input});
   assert.equal(ui.ctx.dirty,true,id+' must enable Save');
   const staged=ui.ctx.getJSON(guestMode?ui.ctx.guestDraftKey:ui.ctx.draftKey,false);
   assert.equal(staged.data.params[key],changed,id+' stages normalized selected value');
   assert.equal(ui.requests.length,0,id+' must never auto-save');
   input.value=original;
   ui.ctx.document.changeHandler({target:input});
   assert.equal(ui.ctx.dirty,false,id+' reversal clears pending changes');
   assert.equal(ui.saveButton.disabled,true,id+' reversal disables Save');
   assert.equal(ui.discardButton.disabled,true,id+' reversal disables Discard');
  }
  // A preexisting saved threshold must be restored, not the global default.
  ui.ctx.savedSnapshot.params.caution_flood='1.9';
  const flood={id:'planning-caution-flood',value:'2.5',matches(){return false}};
  ui.ctx.document.changeHandler({target:flood});
  assert.equal(ui.ctx.dirty,true);
  flood.value='1.9';
  ui.ctx.document.changeHandler({target:flood});
  assert.equal(ui.ctx.dirty,false,'saved nondefault planning threshold restores clean state');
  delete ui.ctx.savedSnapshot.params.caution_flood;

  // Exercise the remaining three report dropdowns using the real handler.
  for(const [id,nonDefault,baselineKey] of [
    ['map-swell-playback-speed','2','swell_playback_speed'],
    ['map-swell-playback-step','6','swell_playback_step'],
    ['map-alertcalifornia-mode','all','alertcalifornia_mode'],
  ]){
    const control={id,value:nonDefault,matches(){return false}};
    ui.ctx.document.changeHandler({target:control});
    assert.equal(ui.ctx.dirty,true,id+' enables Save');
    const draft=ui.ctx.getJSON(guestMode?ui.ctx.guestDraftKey:ui.ctx.draftKey,false);
    assert.equal(draft.data.params[baselineKey],nonDefault,id+' stages exact value');
    assert.equal(ui.requests.length,0,id+' must not auto-save');
    control.value={swell_playback_speed:'1',swell_playback_step:'3',alertcalifornia_mode:'clustered'}[baselineKey];
    ui.ctx.document.changeHandler({target:control});
    assert.equal(ui.ctx.dirty,false,id+' returning to default clears dirty');
  }
  // The ALERTCalifornia overlay and tidal-range checkbox also stage a net
  // value and clear it when restored to their previous checked state.
  for(const [id,original] of [['map-show-alertcalifornia',false],['show-tide-range-overlay',true]]){
    const toggle={id,checked:!original,matches(){return false}};
    ui.ctx.document.changeHandler({target:toggle});
    assert.equal(ui.ctx.dirty,true,id+' toggled activates Save');
    toggle.checked=original;
    ui.ctx.document.changeHandler({target:toggle});
    assert.equal(ui.ctx.dirty,false,id+' reversed disables Save');
  }
  // Restoration must trigger the existing map/swell controller listeners,
  // and a redrawn chart must receive the saved tide checkbox preference.
  const restoreEvents=[];
  const controlValues={
    'map-swell-playback-speed':{value:'1'},
    'map-swell-playback-step':{value:'3'},
    'map-alertcalifornia-mode':{value:'clustered'},
    'show-tide-range-overlay':{checked:true},
  };
  Object.entries(controlValues).forEach(([id,el])=>el.dispatchEvent=e=>restoreEvents.push(id+':'+e.type));
  ui.ctx.document.getElementById=id=>controlValues[id]||controls[id]||null;
  const savedForRestore=JSON.parse(JSON.stringify(ui.ctx.savedSnapshot));
  savedForRestore.params.swell_playback_speed='2';
  savedForRestore.params.swell_playback_step='6';
  savedForRestore.params.alertcalifornia_mode='all';
  savedForRestore.overlays['show-tide-range-overlay']=false;
  ui.ctx.restoreControlPreferences(savedForRestore,false);
  assert.equal(controlValues['map-swell-playback-speed'].value,'2');
  assert.equal(controlValues['map-swell-playback-step'].value,'6');
  assert.equal(controlValues['map-alertcalifornia-mode'].value,'all');
  assert.equal(controlValues['show-tide-range-overlay'].checked,false);
  assert.equal(restoreEvents.length,4,'restoration dispatches controller change events');
  // AJAX redraw reintroduces server-rendered planning defaults. Restore
  // every saved/pending planning field without dispatching a second fetch.
  const planningEls=Object.fromEntries(planningCases.map(([id])=>[id,{value:''}]));
  ui.ctx.document.getElementById=id=>planningEls[id]||controlValues[id]||controls[id]||null;
  const savedWithPlanning=JSON.parse(JSON.stringify(ui.ctx.savedSnapshot));
  savedWithPlanning.params.planning_start='13:30';
  savedWithPlanning.params.caution_flood='1.9';
  savedWithPlanning.overlays['show-tide-range-overlay']=false;
  ui.ctx.restoreControlPreferences(savedWithPlanning,true);
  assert.equal(planningEls['planning-start'].value,'13:30');
  assert.equal(planningEls['planning-caution-flood'].value,'1.9');
  assert.equal(planningEls['planning-buffer'].value,'60');
  assert.equal(restoreEvents.length,4,'planning reapply must not dispatch new requests');

  // The chart redraw does not reset the saved/pending selection.
  const changedTide={id:'show-tide-range-overlay',checked:false,matches(){return false}};
  ui.ctx.document.changeHandler({target:changedTide});
  controlValues['show-tide-range-overlay'].checked=true; // server-rendered replacement
  redrawHandler();
  assert.equal(controlValues['show-tide-range-overlay'].checked,false,'AJAX chart keeps pending checkbox selection');

 }
 }
 console.log('SailingGo explicit Save/Discard: personal, Guest, net-zero edits, 409, failure and unload checks passed.');
}
run().catch(err=>{console.error(err);process.exitCode=1});
