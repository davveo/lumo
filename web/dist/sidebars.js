(function () {
  'use strict';
  const LIMIT = 6, PREF_KEY = 'lumo-sidebar-ui-v1';
  const scope = () => JSON.stringify([window.LumoAuth?.user()?.id || window.LumoAuth?.user()?.user_id || '',window.LumoAPI?.getToken?.() || '',window.LumoAPI?.getBase?.() || location.origin]);
  const loggedIn = () => window.LumoAuth?.isLoggedIn() === true;
  const route = () => (location.hash || '#/').slice(2).split('/')[0];
  const allowed = () => !['login','editor','payment','checkout'].includes(route());
  let owner = scope(), revision = 0, rows = [], pending = null, loading = false, failed = false;
  let preferences = {interestCollapsed:false,quickCollapsed:false};
  try {const saved=JSON.parse(localStorage.getItem(PREF_KEY) || '{}');preferences={interestCollapsed:saved.interestCollapsed===true,quickCollapsed:saved.quickCollapsed===true};} catch(_) {}
  const known = id => products.find(p=>p.id===id || p.skuId===id || p.variants?.some(v=>v.sku_id===id));
  function reset() {revision++;owner=scope();rows=[];pending=null;loading=false;failed=false;window.LumoRecommendations?.restoreItems?.('recent',[]);}
  function ensureScope() {if(owner!==scope())reset();}
  function sourceIDs() {
    if(!loggedIn())return [];
    const history=window.LumoMember?.model()?.history || [], seen=new Set();
    return [...history.map(row=>row.sku_id || row.id),...(state.favoriteSKUs?.length ? state.favoriteSKUs : state.favorites || [])].filter(id=>typeof id==='string' && id && !seen.has(id) && seen.add(id)).slice(0,LIMIT);
  }
  function visibleRows() {
    const seen=new Set();
    return sourceIDs().map(id=>rows.find(row=>row.source===id)?.product || known(id)).filter(p=>p && p.visible!==false && p.name && !seen.has(p.id) && seen.add(p.id)).slice(0,LIMIT);
  }
  async function refresh() {
    ensureScope();
    const context=owner, ids=sourceIDs(), key=JSON.stringify([context,ids]);
    if(pending?.key===key)return pending.promise;
    const version=++revision, active=()=>context===scope() && version===revision;
    const ticket=window.LumoInventory?.hydrationTicket?.();
    loading=ids.length>0;failed=false;
    const entry={key};pending=entry;
    entry.promise=(async()=>{
      const resolved=await Promise.all(ids.map(async id=>{
        try {
          let product=rows.find(row=>row.source===id)?.product || known(id);
          if(!product) {
            let raw=await window.LumoAPI.call('catalog','get',{id},{auth:false});
            if(!active())return null;
            const parent=raw.product_id || raw.parent_id || raw.attributes?.product_id || raw.attributes?.parent_id;
            if(parent && parent!==raw.id)raw=await window.LumoAPI.call('catalog','get',{id:parent},{auth:false});
            if(!active())return null;
            if(raw.active===false || raw.status==='DISABLED' || raw.catalog_visible===false)return null;
            product=window.LumoCommerce?.normalizeProduct?.(raw);
          }
          if(!product || product.visible===false || !active())return null;
          let stock=window.LumoInventory?.snapshot?.(product);
          if(!stock)stock=await window.LumoAPI.call('inventory','get',{id:product.skuId || product.sku_id || product.id},{auth:false});
          return active()?{source:id,product,stock}:null;
        } catch(_) {if(active())failed=true;return null;}
      }));
      if(!active())return [];
      rows=resolved.filter(Boolean);
      const items=rows.map(row=>row.product), inventory=rows.map(row=>row.stock).filter(Boolean);
      if(window.LumoRecommendations?.mapItems)window.LumoRecommendations.mapItems({items,inventory_items:inventory},'recent',[],ticket);
      else window.LumoInventory?.prime?.(inventory,ticket);
      loading=false;update();return items;
    })().finally(()=>{if(pending===entry){pending=null;loading=false;}});
    return entry.promise;
  }
  function interest() {
    if(preferences.interestCollapsed)return `<button type="button" class="interest-rail-reopen" data-sidebar-action="toggle-interest" aria-label="展开感兴趣商品">${icon('heart')}<span>最近感兴趣</span>${icon('chevron')}</button>`;
    const items=visibleRows();
    const content=items.length?items.map(p=>`<a class="interest-item" href="#/product/${encodeURIComponent(p.id)}" aria-label="查看${esc(p.name)}"><img src="${esc(asset(p.image))}" alt="${esc(p.name)}" loading="lazy"><span class="interest-item-name">${esc(p.name)}</span><strong>¥${money(p.price)}</strong></a>`).join(''):`<div class="interest-empty"><span>${icon('heart')}</span><p>${!loggedIn()?'登录查看你的兴趣好物':loading?'正在整理最近的好物…':failed?'部分商品暂时无法展示':'浏览或收藏商品后，在这里轻松找回'}</p>${!loggedIn()?'<a href="#/login">去登录</a>':failed?'<button type="button" data-sidebar-action="retry-interest">重试</button>':'<a href="#/member/history">查看足迹</a>'}</div>`;
    return `<aside class="interest-rail" aria-label="最近感兴趣"><div class="interest-rail-head"><h2>最近感兴趣</h2></div><div class="interest-rail-list">${content}</div><button type="button" class="interest-rail-toggle" data-sidebar-action="toggle-interest" aria-label="收起感兴趣商品">${icon('back')}</button></aside>`;
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
    if(action==='retry-interest'){void refresh();return;}
    if(action==='home'){window.LumoDiscovery?.resetFilters();Object.assign(ui,{category:'全部好物',categoryLabel:'',query:'',onlyFavorites:false,min:'',max:'',sort:'综合'});go('');return;}
    if(action==='top'){window.scrollTo({top:0,behavior:window.matchMedia?.('(prefers-reduced-motion: reduce)').matches?'auto':'smooth'});return;}
    if(action==='feedback')window.LumoServices?.openFeedback?.();
  });
  for(const event of ['lumo:login','lumo:logout','lumo:session-expired'])window.addEventListener?.(event,()=>{reset();update();});
  window.LumoSidebars={render,refresh,afterRender,reset};
})();
