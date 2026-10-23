(function () {
  'use strict';
  const API=window.LumoAPI;
  let running=null, queued=false, ready=false, lastError=null, statusEl=null, routeTimer, routeEpoch=0;
  const escaped=value=>String(value??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
  function status(message,error=false){
    if(!statusEl){statusEl=document.createElement('div');statusEl.className='api-status';statusEl.setAttribute('role','status');document.body.prepend(statusEl);}
    statusEl.hidden=!message;
    statusEl.innerHTML=message?`${escaped(message)}${error?'<button data-api-action="retry">重新连接</button><button data-api-action="settings">接口设置</button>':''}`:'';
  }
  function showConfig(){
    modal('后端连接',`<p class="api-config-help">完整工程由网关同时提供商城和接口，本地启动后访问 http://localhost:8080，无需填写地址。若前端单独部署，请填写允许该前端来源访问的 HTTPS 网关地址。</p><form id="api-config-form"><label class="api-config-field">网关地址<input name="base" type="url" placeholder="留空使用当前站点" value="${escaped(API.getBase())}"></label><p class="api-config-help">更换地址后需重新登录。</p><button class="primary">保存并连接</button><div id="api-config-error" class="api-config-error"></div></form><div class="api-trace-details">${API.requests().slice(0,4).map(r=>`${escaped(r.service+'/'+r.action)} · ${r.status} · ${escaped(r.traceId||'无链路编号')}`).join('<br>')}</div>`);
  }
  async function invoke(object,method,...args){if(typeof object?.[method]==='function')return object[method](...args);}
  async function stage(tasks,errors){const results=await Promise.allSettled(tasks.map(fn=>fn()));for(const result of results)if(result.status==='rejected')errors.push(result.reason);}
  async function refresh(){
    if(running){queued=true;return running;}
    running=(async()=>{
      const errors=[];status(ready?'':'正在连接商城服务…');
      try{await invoke(window.LumoAuth,'refresh');}catch(error){errors.push(error);}
      let catalogOK=true;
      try{await invoke(window.LumoCommerce,'refreshCatalog');}catch(error){catalogOK=false;errors.push(error);}
      await stage([()=>invoke(window.LumoInventory,'refresh'),()=>invoke(window.LumoDiscovery,'refresh')],errors);
      await stage([()=>invoke(window.LumoPromotions,'refresh')],errors);
      await stage([()=>invoke(window.LumoCommerce,'refreshCart'),()=>invoke(window.LumoCommerce,'refreshOrders'),()=>invoke(window.LumoCommerce,'refreshFavorites')],errors);
      await stage([()=>invoke(window.LumoMember,'refresh'),()=>invoke(window.LumoServices,'refresh'),()=>invoke(window.LumoStudio,'refresh')],errors);
      await stage([()=>invoke(window.LumoProductExtras,'refresh'),()=>invoke(window.LumoRecommendations,'refresh')],errors);
      ready=catalogOK;lastError=errors[0]||null;
      if(typeof render==='function')render();
      if(errors.length)status(errors[0].message||'部分服务未能连接，请重试',true);else status('');
      window.dispatchEvent(new CustomEvent('lumo:ready',{detail:{ready,errors:errors.map(e=>e.message)}}));
      return {ready,errors};
    })().finally(()=>{running=null;if(queued){queued=false;refresh();}});
    return running;
  }
  document.addEventListener('click',event=>{const button=event.target.closest('[data-api-action]');if(!button)return;if(button.dataset.apiAction==='settings')showConfig();if(button.dataset.apiAction==='retry')refresh();});
  document.addEventListener('submit',async event=>{if(event.target.id!=='api-config-form')return;event.preventDefault();const button=event.target.querySelector('button');button.disabled=true;try{const base=String(new FormData(event.target).get('base')||'').trim();API.setBase(base);try{localStorage.setItem('lumo-api-base-v1',API.getBase());}catch(_){}closeModal();await refresh();}catch(error){const el=document.getElementById('api-config-error');if(el)el.textContent=error.message;}finally{button.disabled=false;}});
  window.addEventListener('lumo:login',()=>refresh());
  window.addEventListener('lumo:logout',()=>refresh());
  window.addEventListener('lumo:catalog-changed',()=>refresh());
  window.addEventListener('hashchange',()=>{
    clearTimeout(routeTimer);
    const revision=++routeEpoch;
    routeTimer=setTimeout(async()=>{
      if(!ready||running)return;
      const route=(location.hash||'#/').slice(2).split('/'), tasks=[], errors=[];
      if(route[0]==='member'||route[0]==='orders'){
        tasks.push(()=>invoke(window.LumoCommerce,'refreshOrders'),()=>invoke(window.LumoMember,'refresh'));
        if(['messages','invoices','support'].includes(route[1]))tasks.push(()=>invoke(window.LumoServices,'refresh'));
      }
      if(route[0]==='cart')tasks.push(()=>invoke(window.LumoCommerce,'refreshCart'));
      if(route[0]==='checkout')tasks.push(async()=>{await invoke(window.LumoMember,'refresh');await invoke(window.LumoCommerce,'invalidateQuote');});
      if(route[0]==='promotions')tasks.push(()=>invoke(window.LumoPromotions,'refresh'));
      if(!tasks.length)return;
      await stage(tasks,errors);
      if(revision!==routeEpoch)return;
      if(typeof renderKeepScroll==='function')renderKeepScroll();
      if(errors.length&&typeof toast==='function')toast(errors[0].message||'数据刷新失败');
    },60);
  });
  window.LumoConnection={refresh,showConfig,get ready(){return ready;},get error(){return lastError;}};
  window.LumoAppReady=refresh();
})();
