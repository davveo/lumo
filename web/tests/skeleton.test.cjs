'use strict';
const test=require('node:test');
const assert=require('node:assert/strict');
const path=require('node:path');
const fs=require('node:fs');
const {createHarness}=require('./dom-harness.cjs');
const directory=process.env.FRONTEND_DIR||path.join(__dirname,'../dist');
const deferred=()=>{let resolve,reject;const promise=new Promise((yes,no)=>{resolve=yes;reject=no;});return {resolve,reject,promise};};
const item={id:'sku-phone',name:'真实商品',price_cents:10000,attributes:{product_id:'iphone',name:'真实商品',category:'手机数码',image:'iphone-black.png'}};
function setup(){
  const calls=[];let token='a',user={id:'demo',role:'user'},handler=()=>({items:[]});
  const app=createHarness(directory,'http://skeleton.test',{globals:{LumoAPI:{getToken:()=>token,getBase:()=>'',key:()=>'',async call(service,action,command){calls.push({service,action,command});return handler(service,action,command);}},LumoAuth:{user:()=>user,isLoggedIn:()=>!!user,gate:()=>true,afterRender(){},requireLogin:()=>!!user}}});
  app.load('skeleton.js');app.load('app.js');
  return {app,calls,commerce:app.ctx.LumoCommerce,setHandler(fn){handler=fn;},setUser(value){user=value;token=value?'b':'';app.ctx.dispatchEvent(new app.ctx.CustomEvent(value?'lumo:login':'lumo:logout'));},render(){app.evaluate('render()');return app.ids.get('app').innerHTML;},html(){return app.ids.get('app').innerHTML;},dispose(){app.dispose();}};
}
test('all shared shapes are passive accessible regions with bounded escaped markup',t=>{
  const f=setup();t.after(()=>f.dispose());for(const type of ['products','rows','cards','coupons','detail','profile','form','reviews','navigation','summary','table']){const html=f.app.ctx.LumoSkeleton.render(type,{count:999,label:'<img src=x>',className:'ok "bad'});assert.match(html,/role="status"/);assert.match(html,/aria-busy="true"/);assert.match(html,/aria-hidden="true"/);assert.match(html,/&lt;img src=x&gt;/);assert.doesNotMatch(html,/<(?:button|a|input|img)\b/);assert.doesNotMatch(html,/"bad/);if(type==='products')assert.equal((html.match(/class="sk-product"/g)||[]).length,12);}
  assert.match(fs.readFileSync(path.join(directory,'skeleton.css'),'utf8'),/prefers-reduced-motion:reduce/);
});
test('cold catalog renders six product placeholders without fake count, empty CTA or business requests',t=>{
  const f=setup();t.after(()=>f.dispose());const html=f.html();assert.equal((html.match(/class="sk-product"/g)||[]).length,6);assert.match(html,/正在加载商品/);assert.doesNotMatch(html,/0 件好物|没有找到合适的好物|浏览全部好物/);assert.deepEqual(f.calls,[]);
});
test('pending catalog is coalesced, success replaces skeleton, successful empty then shows real empty',async t=>{
  const f=setup();t.after(()=>f.dispose());const pending=deferred();f.setHandler(()=>pending.promise);const first=f.commerce.refreshCatalog(),second=f.commerce.refreshCatalog();assert.equal(first,second);assert.equal(f.calls.length,1);assert.match(f.render(),/sk-loading-products/);pending.resolve({items:[item]});await first;const html=f.render();assert.doesNotMatch(html,/sk-loading-products/);assert.match(html,/真实商品/);
  f.setHandler(()=>({items:[]}));await f.commerce.refreshCatalog();assert.match(f.render(),/没有找到合适的好物/);assert.match(f.html(),/试试其他关键词，或看看全部商品。/);assert.doesNotMatch(f.html(),/sk-loading-products|>false</);
});
test('catalog error stops loading and preserves a retry, cached data survives failed background read',async t=>{
  const f=setup();t.after(()=>f.dispose());f.setHandler(()=>{throw Error('服务暂不可用');});await assert.rejects(f.commerce.refreshCatalog());assert.match(f.render(),/商品加载失败/);assert.match(f.html(),/重新加载/);assert.doesNotMatch(f.html(),/sk-loading-products/);
  f.setHandler(()=>({items:[item]}));await f.commerce.refreshCatalog();f.setHandler(()=>{throw Error('后台更新失败');});await assert.rejects(f.commerce.refreshCatalog());assert.match(f.render(),/真实商品/);assert.doesNotMatch(f.html(),/sk-loading-products/);
});
test('initial cart and orders use rows, successful empty is distinct and previous account cannot finish a new read',async t=>{
  const f=setup();t.after(()=>f.dispose());assert.match(f.app.navigate('cart'),/sk-loading-rows/);assert.doesNotMatch(f.html(),/购物车还是空的/);const pending=deferred();f.setHandler(()=>pending.promise);const old=f.commerce.refreshCart();f.setUser({id:'other',role:'user'});pending.resolve({items:[{sku_id:'sku-phone',quantity:1}]});await old;assert.equal(f.commerce.readState('cart').loaded,false);assert.equal(f.app.json('state.cart').length,0);
  f.setHandler(()=>({items:[]}));await f.commerce.refreshCart();assert.match(f.app.navigate('cart'),/购物车还是空的/);assert.doesNotMatch(f.html(),/sk-loading-rows/);
  assert.match(f.app.navigate('payment'),/sk-loading-summary/);await f.commerce.refreshOrders();assert.match(f.app.navigate('payment'),/暂无待支付的订单/);
});
test('new search shows placeholders until its own response and a failed query is never a successful empty',async t=>{
  const f=setup();t.after(()=>f.dispose());f.setHandler(()=>({items:[item]}));await f.commerce.refreshCatalog();f.app.load('discovery.js');await f.app.ctx.LumoDiscovery.refreshSearch();const pending=deferred();f.setHandler(()=>pending.promise);f.app.evaluate("ui.query='新关键词'");const request=f.app.ctx.LumoDiscovery.refreshSearch();assert.match(f.render(),/sk-loading-products/);assert.doesNotMatch(f.html(),/0 件好物|没有找到合适/);pending.reject(Error('搜索服务失败'));await assert.rejects(request);assert.match(f.render(),/搜索服务失败/);assert.match(f.html(),/data-action="commerce-search-retry"/);assert.doesNotMatch(f.html(),/sk-loading-products/);
});
test('cold detail has no invented zero price or buy button, missing product is fetched only once after list settles',async t=>{
  const f=setup();t.after(()=>f.dispose());assert.match(f.app.navigate('product/outside-catalog'),/sk-loading-detail/);assert.doesNotMatch(f.html(),/立即购买|加入购物车|¥0/);f.setHandler(()=>({items:[]}));await f.commerce.refreshCatalog();const pending=deferred();f.setHandler((service,action)=>action==='get'?pending.promise:{items:[]});f.render();f.render();assert.equal(f.calls.filter(row=>row.action==='get').length,1);pending.reject(Error('商品已下架'));await new Promise(resolve=>setImmediate(resolve));assert.match(f.render(),/商品已下架/);assert.doesNotMatch(f.html(),/sk-loading-detail/);
});

test('same-filter account switch still resolves public search when personal history fails, without duplicate renders requesting again',async t=>{
  const f=setup();t.after(()=>f.dispose());f.setHandler(()=>({items:[item]}));await f.commerce.refreshCatalog();f.app.load('discovery.js');f.app.evaluate("ui.query='手机'");await f.app.ctx.LumoDiscovery.refresh();
  assert.equal(f.app.ctx.LumoDiscovery.readState().loaded,true);
  f.setUser({id:'next-member',role:'user'});const search=deferred(),privateRead=deferred();
  const before=f.calls.filter(row=>row.service==='search').length;
  f.setHandler((service,_action,command)=>service==='search'?search.promise:command.data.kind==='compare'?privateRead.promise:{items:[{query:'当前会员的历史'}]});
  const refreshed=f.app.ctx.LumoDiscovery.refresh(),failed=assert.rejects(refreshed,/个人历史暂不可用/);
  assert.match(f.render(),/sk-loading-products/);f.render();assert.equal(f.calls.filter(row=>row.service==='search').length-before,1);
  privateRead.reject(Error('个人历史暂不可用'));search.resolve({items:[{...item,id:'iphone',product_id:'iphone'}]});await failed;
  const read=f.app.ctx.LumoDiscovery.readState();assert.equal(read.loaded,true);assert.equal(read.loading,false);assert.equal(read.error,'');
  assert.match(f.render(),/真实商品/);assert.doesNotMatch(f.html(),/sk-loading-products|没有找到合适的好物/);f.render();
  assert.equal(f.calls.filter(row=>row.service==='search').length-before,1,'private failure and repeated renders cannot resubmit the public search');
});

test('pending public searches coalesce for the current owner while delayed old-account responses cannot replace them',async t=>{
  const f=setup();t.after(()=>f.dispose());f.setHandler(()=>({items:[item]}));await f.commerce.refreshCatalog();f.app.load('discovery.js');f.app.evaluate("ui.query='手机'");
  const old=deferred();f.setHandler(()=>old.promise);const previous=f.app.ctx.LumoDiscovery.refreshSearch();
  f.setUser({id:'fresh-member',role:'user'});const current=deferred();f.setHandler(()=>current.promise);
  const first=f.app.ctx.LumoDiscovery.refreshSearch(),second=f.app.ctx.LumoDiscovery.refreshSearch();assert.equal(first,second);
  assert.equal(f.calls.filter(row=>row.service==='search').length,2,'one request per distinct account, including the pending current query');
  old.resolve({items:[]});await previous;assert.equal(f.app.ctx.LumoDiscovery.readState().loaded,false);assert.equal(f.app.ctx.LumoDiscovery.readState().loading,true);
  current.resolve({items:[{...item,id:'iphone',product_id:'iphone'}]});await first;assert.equal(f.app.ctx.LumoDiscovery.readState().loaded,true);assert.match(f.render(),/真实商品/);assert.doesNotMatch(f.html(),/sk-loading-products/);
});
