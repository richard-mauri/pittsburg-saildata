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
const operations=source.slice(guestStart,guestEnd)+'\n'+source.slice(stageStart,stageEnd);
assert.ok(!source.includes('setTimeout(function(){pendingSave=null;void flush()'));
assert.ok(!source.includes('setTimeout(function () { pendingSave=null;void flush()'));
assert.ok(!source.includes('await flush();'), 'account hydration must not save automatically');
const makeFixture=({guest=false,failSave=false,conflict=false}={})=>{
 const store=new Map(),requests=[],notices=[];
 const saveButton={disabled:true,addEventListener(name,fn){this.click=fn}};
 const ctx={
  saveButton,
  window:{addEventListener(name,fn){if(name==='beforeunload')ctx.beforeunload=fn}},
  document:{documentElement:{classList:{remove(){}}}},
  guestIdentity:guest?'guest-001':'',
  signedIn:!guest,user:guest?'':'reader',initializing:false,applying:false,
  blocked:false,saving:false,dirty:false,suppressUnload:false,
  revision:3,baseline:{schema:1,params:{wind_unit:'kts'},overlays:{},mapType:'',marineCategories:{}},
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
  same(a,b){return JSON.stringify(a)===JSON.stringify(b)},
  updateSaveUI(message){notices.push(message);saveButton.disabled=!ctx.dirty||ctx.blocked},
  signedInStatus(){},
  async request(path,options){requests.push({path,options});return conflict
   ? {status:409,ok:false}
   : {status:200,ok:true,json:async()=>({revision:4})}},
  accountStatus(){},
  Promise,JSON,Error,String,Number,
 };
 vm.createContext(ctx);
 vm.runInContext(operations,ctx);
 return {ctx,store,requests,notices,saveButton};
};
async function run(){
 const personal=makeFixture();
 const next={schema:1,params:{wind_unit:'mph'},overlays:{},mapType:'',marineCategories:{}};
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
 console.log('SailingGo explicit Save Changes: staging, personal, Guest, 409, failure and navigation checks passed.');
}
run().catch(err=>{console.error(err);process.exitCode=1});
