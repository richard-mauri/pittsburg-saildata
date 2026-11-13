// SailingGo browser presence, no personal tokens or article URLs are transmitted.
// A random per-tab identifier is ephemeral and origin-scoped.
(() => {
  let id='';
  const key='sailing-activity-tab-v1';
  try {
    id=sessionStorage.getItem(key)||'';
    if (!/^[a-f0-9]{32}$/.test(id)) {
      id=Array.from(crypto.getRandomValues(new Uint8Array(16)),b=>b.toString(16).padStart(2,'0')).join('');
      sessionStorage.setItem(key,id);
    }
  } catch (_) {
    id=Array.from(crypto.getRandomValues(new Uint8Array(16)),b=>b.toString(16).padStart(2,'0')).join('');
  }
  let interacted=false;
  let sentAt=0;
  function send() {
    if (document.hidden)return;
    const now=Date.now();
    if (now-sentAt<25000)return;
    sentAt=now;
    const body=JSON.stringify({id,visible:true,interacted});
    interacted=false;
    try {
      fetch('/api/activity/ping',{method:'POST',cache:'no-store',credentials:'same-origin',
        headers:{'Content-Type':'application/json'},body,keepalive:true}).catch(()=>{});
    }catch(_){}
  }
  function mark() {interacted=true;send()}
  document.addEventListener('pointerdown',mark,{passive:true});
  document.addEventListener('keydown',mark,{passive:true});
  document.addEventListener('visibilitychange',()=>{if(!document.hidden)send()});
  window.addEventListener('pageshow',send);
  window.addEventListener('focus',send);
  send();
  setInterval(send,60000);
})();
