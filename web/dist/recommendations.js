(function () {
  'use strict';
  const cache = new Map();
  let page = 0, currentKey = '';
  const keyFor = order => String(window.LumoAuth?.user()?.id || window.LumoAuth?.user()?.user_id || 'guest') + ':' + String(order?.id || 'catalog');
  const productFor = raw => products.find(p => p.id === (raw.product_id || raw.id) || (p.skuId || p.sku_id) === raw.id) ||
    (window.LumoCommerce?.normalizeProduct ? window.LumoCommerce.normalizeProduct(raw) : null);
  function rank(order) { return cache.get(keyFor(order))?.rows || []; }
  async function refresh(order = (state.orders || [])[0]) {
    const key = keyFor(order), prior = cache.get(key);
    if(prior?.promise)return prior.promise;
    const entry = {rows:prior?.rows || [],loading:true,error:'',strategy:''};
    cache.set(key,entry);
    entry.promise = window.LumoAPI.call('recommendation','list',{data:{...(order?.id?{order_id:order.id}:{}),limit:20}},{auth:!!order?.id}).then(data => {
      const own = new Set((order?.items || []).map(i=>i.id));
      const seen = new Set();
      entry.rows=(data.items || []).map(raw=>({product:productFor(raw),reason:data.personalized_model?'根据购买偏好推荐':'根据本单商品排除与可售目录推荐'}))
        .filter(row=>row.product&&row.product.visible!==false&&!own.has(row.product.id)&&!seen.has(row.product.id)&&seen.add(row.product.id));
      entry.strategy=data.strategy || '';entry.error='';return entry.rows;
    }).catch(error=>{entry.error=error.message || '推荐暂不可用';entry.traceID=error.traceID;throw error;}).finally(()=>{entry.loading=false;entry.promise=null;});
    return entry.promise;
  }
  function renderRecommendations(order) {
    if(!order)return '';
    const key=keyFor(order);
    if(currentKey!==key){currentKey=key;page=0;}
    let entry=cache.get(key);
    if(!entry){entry={rows:[],loading:true};cache.set(key,entry);refresh(order).then(()=>typeof renderKeepScroll==='function'&&renderKeepScroll()).catch(()=>typeof renderKeepScroll==='function'&&renderKeepScroll());}
    const rows=rank(order),pages=Math.ceil(rows.length/4);
    page=pages?page%pages:0;
    const visible=rows.slice(page*4,page*4+4);
    const controls=pages>1?`<button class="purchase-recommendations-refresh" data-recommendation-action="next" aria-label="换一组推荐商品">${icon('return')}换一组<span>${page+1} / ${pages}</span></button>`:'';
    return `<section class="purchase-recommendations" aria-labelledby="purchase-recommendations-title"><div class="purchase-recommendations-head"><div><span class="purchase-recommendations-eyebrow">CURATED FOR YOU</span><h2 id="purchase-recommendations-title">为你推荐</h2><p>根据本次订单与当前可售商品挑选</p></div>${controls}</div>${entry.loading&&!visible.length?'<div class="purchase-recommendations-empty" role="status">正在获取推荐商品…</div>':entry.error?`<div class="purchase-recommendations-empty"><p>${esc(entry.error)}</p><button class="outline" data-recommendation-action="retry" data-order="${esc(order.id)}">重新加载</button></div>`:visible.length?`<div class="purchase-recommendations-grid">${visible.map(row=>`<div class="purchase-recommendation-card">${productCard(row.product)}<div class="purchase-recommendation-reason">${icon('sparkles')}<span>${esc(row.reason)}</span></div></div>`).join('')}</div>`:`<div class="purchase-recommendations-empty">${icon('bag')}<h3>当前暂无更多可售推荐</h3><p>继续逛逛，发现更多好物。</p><a class="outline" href="#/">浏览全部商品 ${icon('arrow')}</a></div>`}</section>`;
  }
  document.addEventListener('click',event=>{
    const control=event.target.closest('[data-recommendation-action]');if(!control)return;
    if(control.dataset.recommendationAction==='next'){page++;renderKeepScroll();return;}
    const order=(state.orders||[]).find(o=>o.id===control.dataset.order);
    if(order){control.disabled=true;refresh(order).catch(()=>{}).finally(()=>renderKeepScroll());}
  });
  window.addEventListener('lumo:logout',()=>{cache.clear();page=0;currentKey='';});
  window.LumoRecommendations={render:renderRecommendations,rank,refresh};
})();
