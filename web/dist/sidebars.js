(function () {
  'use strict';
  const C = window.LumoConstants;
  const LIMIT = 6, PREF_KEY = 'lumo-sidebar-ui-v1';
  const scope = () => JSON.stringify([window.LumoAuth?.user()?.id || window.LumoAuth?.user()?.user_id || '',window.LumoAPI?.getToken?.() || '',window.LumoAPI?.getBase?.() || location.origin]);
  const loggedIn = () => window.LumoAuth?.isLoggedIn() === true;
  const route = () => (location.hash || '#/').slice(2).split('/')[0];
  const allowed = () => !['login','editor','payment','checkout'].includes(route());
  let owner=scope(),revision=0,rows=[],pending=null,loading=false,loaded=false,error='',personalized=false,degraded=false;
  let preferences = {interestCollapsed:false,quickCollapsed:false};
  try {const saved=JSON.parse(localStorage.getItem(PREF_KEY) || '{}');preferences={interestCollapsed:saved.interestCollapsed===true,quickCollapsed:saved.quickCollapsed===true};} catch(_) {}
  function reset(){revision++;owner=scope();rows=[];pending=null;loading=false;loaded=false;error='';personalized=false;degraded=false;window.LumoRecommendations?.restoreItems?.(C.ProductPool.Recent,[]);}
  function ensureScope(){if(owner!==scope())reset();}
  function visibleRows(){return rows.map(row=>row.product);}
  function normalizeRows(data){
    if(!data||!Array.isArray(data.items)||!Array.isArray(data.inventory_items))throw Error('兴趣好物返回格式不完整，请重试');
    const seen=new Set(),result=[];
    for(const raw of data.items.slice(0,24)){
      if(!raw||raw.active===false||raw.status===C.CatalogStatus.Disabled||raw.catalog_visible===false)continue;
      const fields={...(raw.attributes||raw.presentation||{}),...raw};
      if(!String(fields.name||'').trim()||fields.price_cents==null||!Number.isFinite(Number(fields.price_cents))||Number(fields.price_cents)<0)continue;
      const product=window.LumoCommerce?.normalizeProduct?.(raw),id=product?.id;
      if(!product||!id||seen.has(id)||product.visible===false||!product.name||!Number.isFinite(product.price)||product.price<0)continue;
      seen.add(id);result.push({product,source:[C.RecommendationSource.History,C.RecommendationSource.Favorite,C.RecommendationSource.Recommendation,C.RecommendationSource.Featured].includes(raw.recent_source)?raw.recent_source:C.RecommendationSource.Recommendation,reason:typeof raw.recommendation_reason==='string'?raw.recommendation_reason.slice(0,120):''});
      if(result.length===LIMIT)break;
    }
    return result;
  }
  function refresh(force=false){
    ensureScope();if(pending&&!force)return pending.promise;
    const context=owner,version=++revision,active=()=>context===scope()&&version===revision,ticket=window.LumoInventory?.hydrationTicket?.();
    loading=true;error='';update();
    const entry={};pending=entry;
    entry.promise=(async()=>{
      try{
        const data=await window.LumoAPI.call(C.Service.Storefront,C.Action.Recent,{}, {auth:loggedIn()});
        if(!active())return [];
        const next=normalizeRows(data);
        if(!active())return [];
        rows=next;loaded=true;personalized=data.personalized===true;degraded=data.degraded===true;
        window.LumoInventory?.prime?.(data.inventory_items,ticket);
        window.LumoRecommendations?.restoreItems?.(C.ProductPool.Recent,rows);
        return visibleRows();
      }catch(cause){if(active())error=cause?.message||'暂时无法加载兴趣好物，请重试';return [];}
      finally{if(active()){loading=false;update();}if(pending===entry)pending=null;}
    })();
    return entry.promise;
  }
  function interest(){
    const title=loaded?(personalized?'最近感兴趣':'精选好物'):loggedIn()?'最近感兴趣':'精选好物';
    if(preferences.interestCollapsed)return `<button type="button" class="interest-rail-reopen" data-sidebar-action="toggle-interest" aria-label="展开感兴趣商品">${icon('heart')}<span>${title}</span>${icon('chevron')}</button>`;
    const busy=!rows.length&&!loaded&&!error,warning=error?`<div class="interest-empty" role="alert"><p>${esc(error)}</p><button type="button" data-sidebar-action="retry-interest">重试</button></div>`:'';
    const content=busy?(window.LumoSkeleton?.render('products',{count:6,label:'正在加载'+title,className:'sk-interest'})||''):rows.length?rows.map(row=>{const p=row.product;return `<a class="interest-item" href="#/product/${encodeURIComponent(p.id)}" aria-label="查看${esc(p.name)}"${row.reason?` title="${esc(row.reason)}"`:''}><img src="${esc(asset(p.image))}" alt="${esc(p.name)}" loading="lazy"><span class="interest-item-name">${esc(p.name)}</span><strong>¥${money(p.price)}</strong></a>`;}).join(''):error?'':`<div class="interest-empty"><span>${icon('heart')}</span><p>暂时没有可推荐的在售好物</p><button type="button" data-sidebar-action="retry-interest">重新加载</button></div>`;
    return `<aside class="interest-rail" aria-label="${title}"><div class="interest-rail-head"><h2>${title}</h2></div><div class="interest-rail-list"${loading?' aria-busy="true"':''}>${warning}${content}</div><button type="button" class="interest-rail-toggle" data-sidebar-action="toggle-interest" aria-label="收起感兴趣商品">${icon('back')}</button></aside>`;
  }
  function quick() {
    if(preferences.quickCollapsed)return `<button type="button" class="quick-rail-reopen" data-sidebar-action="toggle-quick" aria-label="展开快捷工具">${icon('layers')}<span>工具</span>${icon('back')}</button>`;
    const current=route(), count=typeof cartCount==='function'?cartCount():0;
    const item=(label,glyph,href,active=false,optional=false,badge='')=>`<a class="quick-rail-item${active?' active':''}${optional?' quick-rail-optional':''}" href="${href}" aria-label="${label}"${active?' aria-current="page"':''}>${icon(glyph)}<span>${label}</span>${badge?`<i class="quick-rail-badge">${badge}</i>`:''}</a>`;
    return `<nav class="quick-rail" aria-label="商城快捷工具"><button type="button" class="quick-rail-item quick-rail-optional${current===''?' active':''}" data-sidebar-action="home" aria-label="返回商城首页">${icon('home')}<span>首页</span></button>${item('购物车','cart','#/cart',current==='cart',false,count>0?(count>99?'99+':String(count)):'')}${item('我的','user','#/member',current==='member' && !/^#\/member\/(?:support|history)(?:\/|$)/.test(location.hash),true)}${item('客服','support','#/member/support',location.hash.startsWith('#/member/support'))}${item('领券','ticket','#/coupons',current==='coupons',true)}${item('足迹','clock','#/member/history',location.hash==='#/member/history',true)}<button type="button" class="quick-rail-item quick-rail-optional" data-sidebar-action="feedback" aria-label="意见反馈">${icon('edit')}<span>反馈</span></button><button type="button" class="quick-rail-item" data-sidebar-action="top" aria-label="回到顶部">${icon('arrow','quick-rail-up')}<span>回顶部</span></button><button type="button" class="quick-rail-item quick-rail-collapse" data-sidebar-action="toggle-quick" aria-label="收起快捷工具">${icon('chevron')}<span>收起</span></button></nav>`;
  }
  function render() {ensureScope();return allowed()?`<div id="lumo-sidebars" class="lumo-sidebars">${interest()}${quick()}</div>`:'';}
  function afterRender() {
    const active=allowed() && !!document.querySelector('#lumo-sidebars');
    document.body?.classList.toggle('has-lumo-sidebars',active);
    document.body?.classList.toggle('has-interest-rail',active && !preferences.interestCollapsed);
    document.body?.classList.toggle('quick-rail-collapsed',active && preferences.quickCollapsed);
  }
  function update() {const root=document.querySelector('#lumo-sidebars');if(root)root.innerHTML=allowed()?interest()+quick():'';afterRender();}
  document.addEventListener('click',event=>{
    const button=event.target.closest?.('[data-sidebar-action]');if(!button)return;
    const action=button.dataset.sidebarAction;
    if(action==='toggle-interest'||action==='toggle-quick'){
      const key=action==='toggle-interest'?'interestCollapsed':'quickCollapsed';preferences[key]=!preferences[key];
      try{localStorage.setItem(PREF_KEY,JSON.stringify(preferences));}catch(_){}update();return;
    }
    if(action==='retry-interest'){void refresh(true);return;}
    if(action==='home'){window.LumoDiscovery?.resetFilters();Object.assign(ui,{category:'全部好物',categoryLabel:'',query:'',onlyFavorites:false,min:'',max:'',sort:'综合'});go('');return;}
    if(action==='top'){window.scrollTo({top:0,behavior:window.matchMedia?.('(prefers-reduced-motion: reduce)').matches?'auto':'smooth'});return;}
    if(action==='feedback')window.LumoServices?.openFeedback?.();
  });
  // A behavior write invalidates a pending result; its existing caller then
  // explicitly refreshes this aggregate. Pure render/collapse never fetches.
  window.addEventListener?.('lumo:api-request',event=>{const row=event.detail;if(row?.ok&&row.service===C.Service.Engagement&&[C.Action.Put,C.Action.Delete].includes(row.action)&&pending){revision++;pending=null;loading=false;}});
  for(const event of ['lumo:login','lumo:logout','lumo:session-expired','lumo:api-scope-changed'])window.addEventListener?.(event,()=>{reset();update();});
  window.LumoSidebars={render,refresh,afterRender,reset,model(){ensureScope();return {loading,loaded,error,personalized,degraded,count:rows.length,items:rows.map(row=>({...row.product,recent_source:row.source,recommendation_reason:row.reason}))};}};
})();
