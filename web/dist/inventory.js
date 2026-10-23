(function () {
  'use strict';
  const snapshots = new Map();
  const errors = new Map();
  let refreshPromise;
  const LIMIT = 10;
  const find = value => { const id = typeof value === 'string' ? value : value?.id; return products.find(p => p.id === id || (p.skuId || p.sku_id) === id); };
  const sku = value => {
    if(value&&typeof value==='object'){
      if(value.sku_id||value.skuId)return String(value.sku_id||value.skuId);
      if(window.LumoCommerce?.skuForItem){try{return window.LumoCommerce.skuForItem(value);}catch(_){return '';}}
    }
    const p=find(value);return p?.skuId||p?.sku_id||p?.id||(typeof value==='string'?value:'');
  };
  async function refreshOne(id) {
    const key=sku(id);
    if(!key)return;
    try { const row=await window.LumoAPI.call('inventory','get',{id:key},{auth:false}); snapshots.set(key,row); errors.delete(key); return row; }
    catch(e){errors.set(key,e);throw e;}
  }
  async function refresh(ids) {
    const keys=Array.from(new Set((Array.isArray(ids)?ids:products).map(sku).filter(Boolean)));
    if(!ids&&refreshPromise)return refreshPromise;
    const work=Promise.allSettled(keys.map(refreshOne)).then(results=>{
      const failures=results.filter(r=>r.status==='rejected');
      if(failures.length===results.length&&failures.length)throw failures[0].reason;
      return {loaded:results.length-failures.length,failed:failures.length};
    });
    if(ids)return work;
    refreshPromise=work.finally(()=>{refreshPromise=null;});
    return refreshPromise;
  }
  function snapshot(value) { return snapshots.get(sku(value)); }
  function stockFor(value) {
    const p=find(value);
    if(p?.visible===false)return 0;
    const row=snapshot(value);
    return row ? Math.max(0,Number(row.available)||0) : 0;
  }
  function validate(items) {
    if(!Array.isArray(items)||!items.length)return '请先选择商品';
    const totals=new Map();
    for(const item of items){
      const p=find(item),key=sku(item),q=Number(item.qty);
      if(!p||p.visible===false)return '商品不存在或已下架';
      if(!key)return '所选规格已变化，请重新选择';
      if(!Number.isInteger(q)||q<1||q>LIMIT)return '每种商品数量应为1至10件';
      if(!snapshot(item))return errors.has(key)?'库存服务暂不可用，请刷新后重试':'正在获取库存，请稍后重试';
      totals.set(key,(totals.get(key)||0)+q);
    }
    for(const [key,q]of totals){if(q>LIMIT)return '每种商品每单限购10件';const remaining=stockFor(key);if(q>remaining)return '商品库存不足，当前可售'+remaining+'件';}
    return null;
  }
  function validateAdd(item) {
    const related=(state.cart||[]).filter(row=>sku(row)===sku(item));
    const q=related.reduce((sum,row)=>sum+(Number(row.qty)||0),0)+Number(item.qty);
    return validate([{...item,qty:q}]);
  }
  function limit(item,wanted) {
    const key=sku(item),other=(state.cart||[]).includes(item)?(state.cart||[]).filter(row=>row!==item&&sku(row)===key).reduce((sum,row)=>sum+(Number(row.qty)||0),0):0;
    return Math.min(LIMIT,Math.max(0,stockFor(item)-other),Math.max(1,Math.floor(Number(wanted)||1)));
  }
  function badge(value) {
    const p=find(value),row=snapshot(value),key=sku(value);
    const label=p?.visible===false?'暂时下架':!row?(errors.has(key)?'库存暂不可用':'正在查询库存'):stockFor(value)>0?'可售 '+stockFor(value).toLocaleString('zh-CN')+' 件 · 每单限购10件':'暂时售罄';
    return '<span class="stock inventory-stock'+(row&&stockFor(value)<=0?' sold-out':'')+'">'+label+'</span>';
  }
  window.LumoInventory={stockFor,validate,validateAdd,limit,badge,refresh,refreshOne,snapshot};
})();
