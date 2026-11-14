// SailingGo optional Homepage-verified activity. Never export browser identity
// or bearer to the private aggregate collector.
(() => {
  let id='';
  const key='sailing-activity-tab-v1';
  const handoffKey='sailing.homepage.handoff.v1';
  try {id=sessionStorage.getItem(key)||''}catch(_){}
  if(!/^[a-f0-9]{32}$/.test(id)){
    id=Array.from(crypto.getRandomValues(new Uint8Array(16)),b=>b.toString(16).padStart(2,'0')).join('');
    try{sessionStorage.setItem(key,id)}catch(_){}
  }
  let bearer='';
  let initialIdentityChecked=false,checking=false,interacted=false,sentAt=0;
  function currentBearer(){
    try {const value=sessionStorage.getItem(handoffKey)||'';return /^[a-f0-9]{64}$/.test(value)?value:''}
    catch(_){return ''}
  }
  function invalidate(stale){
    if(bearer!==stale)return;
    try{if(sessionStorage.getItem(handoffKey)===stale)sessionStorage.removeItem(handoffKey)}catch(_){}
    bearer='';
    window.dispatchEvent(new Event('sailing-session-invalid'));
  }
  async function verify(){
    if(checking)return;
    checking=true;
    try{
      bearer=currentBearer();
      if(!bearer)return;
      const checked=bearer;
      const response=await fetch('/api/activity/identity',{
        method:'GET',cache:'no-store',credentials:'same-origin',headers:{Authorization:'Bearer '+checked}
      });
      if(response.status===503)return;
      const result=response.ok?await response.json():{state:'guest'};
      if(result.state!=='signed-in')invalidate(checked);
    }catch(_){}
    finally{checking=false}
  }
  function send(){
    if(!initialIdentityChecked || document.hidden)return;
    const now=Date.now();if(now-sentAt<25000)return;
    sentAt=now;
    const headers={'Content-Type':'application/json'};
    if(bearer)headers.Authorization='Bearer '+bearer;
    const used=bearer;
    const body=JSON.stringify({id,visible:true,interacted});interacted=false;
    fetch('/api/activity/ping',{method:'POST',cache:'no-store',credentials:'same-origin',headers,body,keepalive:true})
      .then(response=>{if(response.status===401 && used)invalidate(used)}).catch(()=>{});
  }
  function mark(){interacted=true;send()}
  document.addEventListener('pointerdown',mark,{passive:true});
  document.addEventListener('keydown',mark,{passive:true});
  document.addEventListener('visibilitychange',()=>{if(!document.hidden){void verify();send()}});
  window.addEventListener('pageshow',send);
  window.addEventListener('focus',()=>{void verify();send()});
  void verify().finally(()=>{initialIdentityChecked=true;send()});
  setInterval(()=>{void verify();send()},60000);
})();
