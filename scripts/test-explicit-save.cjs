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
async function run(){
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
