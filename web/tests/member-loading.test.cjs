'use strict';
const test=require('node:test');
const assert=require('node:assert/strict');
const path=require('node:path');
const {createHarness}=require('./dom-harness.cjs');
const directory=process.env.FRONTEND_DIR||path.join(__dirname,'../dist');
const clone=value=>JSON.parse(JSON.stringify(value));
function deferred(){let resolve,reject;const promise=new Promise((yes,no)=>{resolve=yes;reject=no;});return {promise,resolve,reject};}
const tick=()=>new Promise(resolve=>setImmediate(resolve));
const esc=value=>String(value??'').replace(/[&<>"']/g,char=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[char]));
function fixture(){
  let user='buyer',token='token-a',base='http://member-loading.test',handler=async(service,action)=>service==='member'?{display_name:'真实会员',growth:300,level:'member'}:service==='points'?{available:120,ledger:[]}:{items:[]},orders={loading:false,loaded:false,error:''},redraws=0;
  const calls=[],shapes=[],modals=[];
  const app=createHarness(directory,base,{globals:{products:[],state:{cart:[],orders:[],pending:null,favorites:[],favoriteSKUs:[],address:null,points:false,coupon:0},header:()=>'',footer:()=>'',icon:()=>'',money:value=>String(value),price:value=>'¥'+value,itemPrice:()=>0,esc,toast(){},renderKeepScroll(){redraws++;},selectedItems:()=>[],go(route){app.ctx.location.hash='#/'+route;},modal(title,html){modals.push({title,html});app.ids.get('modal-root').innerHTML=html;},closeModal(){app.ids.get('modal-root').innerHTML='';},LumoAPI:{getBase:()=>base,getToken:()=>token,async call(service,action,command={},options={}){calls.push({service,action,command:clone(command),options:clone(options)});return handler(service,action,command,options);}},LumoAuth:{isLoggedIn:()=>!!user,user:()=>user?{id:user}:null,requireLogin:()=>!!user},LumoCommerce:{allOrders:()=>[],readState:()=>clone(orders),async refreshOrders(){orders={loading:false,loaded:true,error:''};}},LumoSkeleton:{render(type,options={}){shapes.push({type,...options});return `<div class="sk-loading" data-skeleton="${type}" role="status" aria-busy="true"><span>${esc(options.label||'加载中')}</span></div>`;}}}});
  for(const file of ['member.js','member-views.js','services.js'])app.load(file);
  return {app,calls,shapes,modals,member:app.ctx.LumoMember,services:app.ctx.LumoServices,view:(tab,filter)=>app.ctx.LumoMemberView.page(tab,filter),dispose(){app.dispose();},setHandler(next){handler=next;},setOrders(next){orders=next;},setSession(nextUser,nextToken='token-b',nextBase=base){user=nextUser;token=nextToken;base=nextBase;},event(name){app.ctx.dispatchEvent(new app.ctx.CustomEvent(name));},get redraws(){return redraws;}};
}

test('member pages render their content shapes before account data and never present fake empty states',async t=>{
  const f=fixture();t.after(()=>f.dispose());
  for(const [tab,shape] of [['overview','profile'],['addresses','cards'],['coupons','coupons'],['points','summary'],['favorites','products'],['history','products'],['orders','rows'],['aftersales','rows'],['settings','form']]){
    const html=f.view(tab);assert.match(html,new RegExp(`data-skeleton="${shape}"`),tab);
    assert.doesNotMatch(html,/还没有收货地址|暂时没有售后申请|收藏夹等你填满|还没有浏览足迹|积分明细会记录在这里|LUMO 生活家/);
  }
  assert.equal(f.calls.length,0,'rendering skeletons must not start requests');
});

test('a real successful empty response replaces the skeleton; all member reads are still issued once',async t=>{
  const f=fixture();t.after(()=>f.dispose());const gate=deferred();f.setHandler(()=>gate.promise);
  const loading=f.member.refresh();assert.equal(f.member.readState().loading,true);assert.match(f.view('addresses'),/data-skeleton="cards"/);assert.equal(f.calls.length,9);
  gate.resolve({items:[]});await loading;
  assert.equal(f.member.readState().loaded,true);assert.equal(f.member.readState().loading,false);assert.doesNotMatch(f.view('addresses'),/sk-loading/);assert.match(f.view('addresses'),/添加你的第一个地址/);
  assert.equal(f.calls.length,9);
});

test('one failed account resource does not discard successfully loaded data or pretend the failure is empty',async t=>{
  const f=fixture();t.after(()=>f.dispose());f.setHandler(async(service)=>{if(service==='points')throw Error('积分服务读取失败');if(service==='member')return {display_name:'已加载资料'};if(service==='address')return {items:[{id:'addr-a',recipient:'测试收货人',phone:'13800000000',province:'上海',address:'测试路'}]};return {items:[]};});
  await assert.rejects(f.member.refresh(),/积分服务读取失败/);
  assert.equal(f.member.readState('profile').loaded,true);assert.equal(f.member.readState('addresses').loaded,true);assert.equal(f.member.readState('points').loaded,false);assert.equal(f.member.model().profile.nickname,'已加载资料');
  const addresses=f.view('addresses');assert.match(addresses,/测试收货人/);assert.doesNotMatch(addresses,/sk-loading/);
  const points=f.view('points');assert.match(points,/积分服务读取失败/);assert.match(points,/data-member-action="retry-read"/);assert.doesNotMatch(points,/当前可用积分|积分明细会记录在这里/);
});

test('background refresh preserves real account data while showing a retryable error',async t=>{
  const f=fixture();t.after(()=>f.dispose());f.setHandler(async service=>service==='address'?{items:[{id:'a',recipient:'真实地址',phone:'13800000000',address:'已保存地址'}]}:{items:[]});await f.member.refresh();
  const gate=deferred();f.setHandler((service)=>service==='address'?gate.promise:Promise.resolve({items:[]}));const pending=f.member.refresh();
  assert.match(f.view('addresses'),/真实地址/);assert.doesNotMatch(f.view('addresses'),/data-skeleton="cards"/);gate.reject(Error('地址暂时不可用'));await assert.rejects(pending,/地址暂时不可用/);
  assert.equal(f.member.readState('addresses').loaded,true);assert.match(f.view('addresses'),/真实地址/);assert.match(f.view('addresses'),/地址暂时不可用/);
});

test('member retry uses the same existing refresh and can recover an initial failure',async t=>{
  const f=fixture();t.after(()=>f.dispose());f.setHandler(()=>Promise.reject(Error('临时失败')));await assert.rejects(f.member.refresh());
  f.setHandler(()=>Promise.resolve({items:[]}));await f.app.click({memberAction:'retry-read',value:'member'});
  assert.equal(f.member.readState('addresses').loaded,true);assert.equal(f.member.readState('addresses').error,'');assert.equal(f.calls.length,18);
  assert.doesNotMatch(f.view('addresses'),/sk-loading|临时失败/);
});

test('order skeleton waits for the commerce orders read instead of interpreting an empty local array as success',async t=>{
  const f=fixture();t.after(()=>f.dispose());await f.member.refresh();assert.match(f.view('orders'),/data-skeleton="rows"/);
  f.setOrders({loading:false,loaded:false,error:'订单读取失败'});assert.match(f.view('orders'),/订单读取失败/);assert.doesNotMatch(f.view('orders'),/data-skeleton="rows"/);
  await f.app.click({memberAction:'retry-read',value:'orders'});assert.doesNotMatch(f.view('orders'),/sk-loading/);
});

test('old member reads cannot mark the next user or gateway ready after a scope change',async t=>{
  for(const change of ['user','token','base']){
    const f=fixture();t.after(()=>f.dispose());const old=deferred();f.setHandler(()=>old.promise);const pending=f.member.refresh();
    f.setSession(change==='user'?'other':'buyer',change==='token'?'new-token':'token-a',change==='base'?'http://other.test':'http://member-loading.test');assert.equal(f.member.readState().loaded,false);assert.equal(f.member.readState().loading,false);
    f.setHandler(()=>Promise.resolve({items:[]}));await f.member.refresh();old.resolve({display_name:'旧用户',available:9999,items:[{id:'old',recipient:'旧地址'}]});await pending;
    assert.notEqual(f.member.model().profile.nickname,'旧用户');assert.equal(f.member.model().addresses.length,0);assert.equal(f.member.readState().loaded,true);
  }
});

test('message, invoice, ticket and FAQ first loads use unified skeletons without zero summaries',async t=>{
  const f=fixture();t.after(()=>f.dispose());for(const [tab,filter]of [['messages','all'],['invoices','all'],['support','tickets'],['support','help']]){
    const html=f.services.content(tab,filter);assert.match(html,/sk-loading/);assert.doesNotMatch(html,/暂时没有|暂无|0 项购物帮助|暂无消息/);
  }
  const gate=deferred();f.setHandler(()=>gate.promise);const loading=f.services.refresh();assert.equal(f.services.readState().loading,true);assert.equal(f.calls.length,4);gate.resolve({items:[]});await loading;
  assert.equal(f.services.readState().loaded,true);assert.doesNotMatch(f.services.content('messages'),/sk-loading/);assert.doesNotMatch(f.services.content('invoices'),/sk-loading/);
});

test('public FAQ failure does not discard private service results, and private failure is not an empty inbox',async t=>{
  const f=fixture();t.after(()=>f.dispose());f.setHandler(async(service)=>{if(service==='notification')throw Error('<img src=x> 消息暂不可用');if(service==='content')return {items:[{id:'faq',title:'真实帮助',body:'实际说明'}]};if(service==='helpdesk')return {items:[{id:'ticket',title:'真实工单',body:'问题',status:'OPEN'}]};return {items:[]};});await assert.rejects(f.services.refresh());
  assert.match(f.services.content('messages'),/&lt;img src=x&gt; 消息暂不可用/);assert.doesNotMatch(f.services.content('messages'),/<img|暂无消息|sk-loading/);
  assert.match(f.services.content('support','tickets'),/真实工单/);assert.match(f.services.content('support','help'),/真实帮助/);
  assert.equal(f.services.readState('tickets').loaded,true);assert.equal(f.services.readState('messages').loaded,false);
});

test('service models and unread counts reset on token/base changes even without a logout event',async t=>{
  const f=fixture();t.after(()=>f.dispose());f.setHandler(async service=>service==='notification'?{items:[{id:'private',event_type:'OrderCompleted',status:'UNREAD'}]}:{items:[]});await f.services.refresh();assert.equal(f.services.unreadCount(),1);
  f.setSession('buyer','new-token');assert.equal(f.services.unreadCount(),0);assert.equal(f.services.model().messages.length,0);assert.equal(f.services.readState('messages').loaded,false);assert.match(f.services.content('messages'),/sk-loading/);
  const old=deferred();f.setHandler(()=>old.promise);const loading=f.services.refresh();f.setSession('other','next','http://other.test');f.services.readState();old.resolve({items:[{id:'old',title:'旧消息'}]});await loading;assert.equal(f.services.model().messages.length,0);assert.equal(f.services.readState('messages').loaded,false);
});

test('service retry retains cached information while background reads run and recover',async t=>{
  const f=fixture();t.after(()=>f.dispose());f.setHandler(async service=>service==='helpdesk'?{items:[{id:'ticket',title:'已有工单',body:'内容',status:'OPEN'}]}:{items:[]});await f.services.refresh();
  const gate=deferred();f.setHandler(service=>service==='helpdesk'?gate.promise:Promise.resolve({items:[]}));const loading=f.services.refresh();assert.match(f.services.content('support','tickets'),/已有工单/);assert.doesNotMatch(f.services.content('support','tickets'),/sk-loading/);gate.reject(Error('工单暂不可用'));await assert.rejects(loading);assert.match(f.services.content('support','tickets'),/已有工单/);assert.match(f.services.content('support','tickets'),/工单暂不可用/);
  f.setHandler(()=>Promise.resolve({items:[]}));await f.app.click({serviceAction:'retry-read'});assert.equal(f.services.readState('tickets').error,'');
});

test('asynchronous address and ticket dialogs show shapes immediately then replace them with authoritative details',async t=>{
  const f=fixture();t.after(()=>f.dispose());const gate=deferred();f.setHandler(()=>gate.promise);const address=f.member.openAddress('a');assert.match(f.modals.at(-1).html,/data-skeleton="form"/);gate.resolve({id:'a',recipient:'实际收货人',phone:'13800000000',address:'真实详细地址'});await address;assert.match(f.modals.at(-1).html,/实际收货人/);assert.doesNotMatch(f.modals.at(-1).html,/sk-loading/);
  const ticket=deferred();f.setHandler(()=>ticket.promise);const viewing=f.app.click({serviceAction:'view-ticket',id:'t'});await tick();assert.match(f.modals.at(-1).html,/data-skeleton="rows"/);ticket.resolve({id:'t',title:'真实会话',body:'实际描述',status:'OPEN'});await viewing;assert.match(f.modals.at(-1).html,/真实会话/);
});

test('failed detail dialogs expose retry instead of a permanently busy skeleton, and closed dialogs never reopen',async t=>{
  const f=fixture();t.after(()=>f.dispose());f.setHandler(()=>Promise.reject(Error('收货地址读取失败')));await f.member.openAddress('a');assert.match(f.modals.at(-1).html,/收货地址读取失败/);assert.match(f.modals.at(-1).html,/data-member-action="edit-address"/);assert.doesNotMatch(f.modals.at(-1).html,/sk-loading/);
  const gate=deferred();f.setHandler(()=>gate.promise);const viewing=f.app.click({serviceAction:'view-ticket',id:'t'});await tick();const count=f.modals.length;f.app.ctx.closeModal();gate.resolve({id:'t',title:'已关闭的旧会话',body:'不应重新打开'});await viewing;assert.equal(f.modals.length,count);assert.equal(f.app.ids.get('modal-root').innerHTML,'');
});

test('checkout pickers reuse account hydration and show address/coupon shapes instead of false empty collections',async t=>{
  const address=fixture();t.after(()=>address.dispose());const gate=deferred();address.setHandler(service=>service==='address'?gate.promise:Promise.resolve({items:[]}));
  const loading=address.member.refresh(),picker=address.member.chooseAddress();assert.match(address.modals.at(-1).html,/data-skeleton="cards"/);assert.doesNotMatch(address.modals.at(-1).html,/还没有收货地址/);assert.equal(address.calls.length,9,'picker must join existing hydration');
  gate.resolve({items:[{id:'actual-address',recipient:'实际地址收件人',phone:'13800000000',address:'真实收货街道'}]});await Promise.all([loading,picker]);assert.match(address.modals.at(-1).html,/实际地址收件人/);assert.doesNotMatch(address.modals.at(-1).html,/sk-loading/);assert.equal(address.calls.length,9);
  const coupon=fixture();t.after(()=>coupon.dispose());coupon.app.ctx.location.hash='#/cart';const couponGate=deferred();coupon.setHandler((service,action)=>service==='coupon'&&action==='list'?couponGate.promise:Promise.resolve({items:[]}));
  const hydrating=coupon.member.refresh(),choosing=coupon.member.showCoupons();assert.match(coupon.modals.at(-1).html,/data-skeleton="coupons"/);assert.doesNotMatch(coupon.modals.at(-1).html,/本单参与门槛金额|本单不使用优惠券/);assert.equal(coupon.calls.length,9);
  couponGate.resolve({items:[{id:'actual-coupon',amount_cents:1000,min_cents:10000,status:'AVAILABLE',expires_at:new Date(Date.now()+86400000).toISOString()}]});await Promise.all([hydrating,choosing]);assert.match(coupon.modals.at(-1).html,/actual-coupon/);assert.equal(coupon.calls.length,9);
});

test('pending checkout picker closed or moved to another account cannot reopen after account data arrives',async t=>{
  for(const change of ['close','account']){
    const f=fixture();t.after(()=>f.dispose());const gate=deferred();f.setHandler(()=>gate.promise);const loading=f.member.refresh(),picker=f.member.chooseAddress();const count=f.modals.length;
    if(change==='close')f.app.ctx.closeModal();else f.setSession('other','new-token');
    gate.resolve({items:[{id:'old-address',recipient:'上个账户地址'}]});await Promise.all([loading,picker]);assert.equal(f.modals.length,count);assert.doesNotMatch(f.modals.at(-1).html,/上个账户地址/);
  }
});

test('invoice order picker joins service hydration and waits for authoritative orders before its empty state',async t=>{
  const f=fixture();t.after(()=>f.dispose());const gate=deferred();f.setHandler(service=>service==='invoice'?gate.promise:Promise.resolve({items:[]}));const loading=f.services.refresh();const picker=f.app.click({serviceAction:'invoice-picker'});await tick();
  assert.match(f.modals.at(-1).html,/data-skeleton="rows"/);assert.doesNotMatch(f.modals.at(-1).html,/暂无可开票订单/);assert.equal(f.calls.length,4,'invoice picker must join existing service request');
  gate.resolve({items:[]});await Promise.all([loading,picker]);assert.match(f.modals.at(-1).html,/暂无可开票订单/);assert.doesNotMatch(f.modals.at(-1).html,/sk-loading/);assert.equal(f.calls.length,4);
});
