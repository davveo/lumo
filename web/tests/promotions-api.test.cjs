const fs=require('node:fs');
const vm=require('node:vm');
const assert=require('node:assert/strict');
const path=require('node:path');
const dir=process.env.FRONTEND_DIR || path.resolve(__dirname,'../dist');
const listeners={};const calls=[];let checkout, currentUser='demo', gateway='', evaluateOverride;
const campaignFixtures=[
 {id:'reduction-demo',kind:'reduction',name:'满额立减',active:true,quota:100,held:0,used:0,threshold_cents:30000,discount_cents:3000,min_quantity:1,pay_rate_bps:0},
 {id:'discount-demo',kind:'discount',name:'折扣特惠',active:true,quota:100,held:0,used:0,threshold_cents:0,discount_cents:0,min_quantity:1,pay_rate_bps:9000},
 {id:'quantity-discount-demo',kind:'quantity_discount',name:'多件优惠',active:true,quota:100,held:0,used:0,threshold_cents:0,discount_cents:0,min_quantity:2,pay_rate_bps:8500},
 {id:'direct-demo',kind:'direct',name:'限量直降',active:true,quota:100,held:0,used:0,threshold_cents:0,discount_cents:2000,min_quantity:1,pay_rate_bps:0}
];
const catalog=[
{id:'headphones',skuId:'headphones',price:2299,visible:true,name:'Headphones',colors:['Black'],options:['Standard'],defaultOption:'Standard',image:'headphones.jpg'},
{id:'coffee',skuId:'coffee',price:99,visible:true,name:'Coffee',colors:['Blend'],options:['250g'],defaultOption:'250g',image:'coffee.jpg'},
{id:'iphone',skuId:'iphone',price:7999,visible:true,name:'Phone',colors:['Black'],options:['256GB'],defaultOption:'256GB',image:'iphone-black.png'}];
let groups=[{id:'team-remote',sku_id:'headphones',creator_id:'other',required:2,paid:1,occupied:1,status:'OPEN',expires_at:new Date(Date.now()+1800000).toISOString()}];
const window={LumoAuth:{user:()=>({id:currentUser}),requireLogin:()=>!!currentUser},addEventListener:(type,fn)=>(listeners[type]??=[]).push(fn),dispatchEvent:()=>{}};
window.LumoAPI={getBase:()=>gateway,call:async(service,action,command={},options={})=>{
 calls.push({service,action,command,options});
 if(service==='inventory'&&action==='batch_get')return {items:command.data.ids.map(id=>({sku_id:id,available:id==='sku-headphones'?7:50})),missing_ids:[]};
 if(service==='inventory'&&action==='get')return {sku_id:command.id,available:command.id==='sku-headphones'?7:50};
 if(service==='catalog')return {id:'sku-headphones',product_id:'headphones',price_cents:129900,active:true};
 if(service==='campaign'&&action==='evaluate')return evaluateOverride ? evaluateOverride(command) : {eligible:true,discount_cents:6000};
 if(service==='campaign')return {items:[{id:'flash-demo',kind:'flash',quota:20,held:2,used:1,active:true,discount_cents:0},{id:'group-demo',kind:'group',quota:30,held:0,used:0,active:true,discount_cents:0},{id:'addon-coffee',kind:'addon',quota:50,held:0,used:0,active:true,threshold_cents:100000,discount_cents:6000},...campaignFixtures]};
 if(service==='flash')return {items:[{id:'flash-demo',sku_id:'sku-headphones',activity_id:'flash-demo',active:true}]};
 if(service==='group'&&action==='list')return {items:groups};
 if(service==='group'&&action==='get')return groups.find(g=>g.id===command.id);
 if(service==='group'&&action==='create'){let row={id:'team-created',sku_id:command.data.sku_id,creator_id:'demo',required:2,paid:0,occupied:0,status:'OPEN',expires_at:new Date(Date.now()+1800000).toISOString()};groups.push(row);return row;}
 if(service==='recommendation')return {items:[{id:'coffee',product_id:'coffee',recommendation_reason:'与你购买的商品品牌相近'}],strategy:'hybrid_content_v2',personalized:true,personalized_model:false};
 throw new Error('Unexpected '+service+'/'+action);
}};
const document={addEventListener:(type,fn)=>(listeners[type]??=[]).push(fn),querySelectorAll:()=>[],querySelector:()=>null};
const context=vm.createContext({window,document,products:catalog,state:{cart:[],orders:[]},location:{hash:'#/checkout',origin:'http://localhost',pathname:'/'},checkoutMode:'cart',directItem:null,detail:{id:'headphones'},crypto:require('node:crypto').webcrypto,CustomEvent:class{},navigator:{clipboard:{writeText:async()=>{}}},setInterval,clearInterval,Date,console,toast:()=>{},renderKeepScroll:()=>{},setCheckout:(mode,item)=>{checkout={mode,item};context.checkoutMode=mode;context.directItem=item},go:()=>{},modal:()=>{},money:String,esc:String,icon:()=>'',asset:s=>s,productCard:p=>p.name,header:()=>'',footer:()=>''});
for(const f of ['inventory.js','promotions.js','promo-views.js','recommendations.js'])vm.runInContext(fs.readFileSync(path.join(dir,f),'utf8'),context,{filename:f});
async function checkReadAggregation() {
 for(const failedService of ['', 'catalog', 'inventory']) {
  const network=[];
  const fixture={products:catalog,state:{cart:[],orders:[]},URL,AbortController,Uint8Array,setTimeout,clearTimeout,
   location:{hash:'#/promotions/flash',origin:'http://shop.test'},CustomEvent:class{},dispatchEvent(){},addEventListener(){},
   document:{addEventListener(){},querySelector:()=>null,querySelectorAll:()=>[]},
   async fetch(url,options){
    assert.ok(url.endsWith('/storefront/batch'),'all promotion reads must use the gateway read batch');
    const requests=JSON.parse(options.body).data.requests;network.push(requests);
    const results=requests.map(read=>{
     if(read.service===failedService)return {...read,error:{code:'Unavailable',message:'read unavailable',status:503}};
     const data=read.service==='campaign'?{items:[{id:'flash-demo',kind:'flash',quota:20,held:2,used:1,active:true,discount_cents:0}]}:
      read.service==='flash'?{items:[{id:'flash-demo',sku_id:'sku-headphones',activity_id:'flash-demo',active:true}]}:
      read.service==='group'?{items:[]}:
      read.service==='catalog'?{id:'sku-headphones',product_id:'headphones',price_cents:129900,active:true}:
      {sku_id:'sku-headphones',available:7};
     return {...read,auth:false,data};
    });
    return {status:200,ok:true,headers:{get:()=>''},json:async()=>({data:{results}})};
   }
  };
  fixture.window=fixture;vm.createContext(fixture);
  for(const f of ['api-client.js','inventory.js','promotions.js'])vm.runInContext(fs.readFileSync(path.join(dir,f),'utf8'),fixture,{filename:f});
  fixture.LumoAPI.enableReadBatching();
  if(failedService)await assert.rejects(fixture.LumoPromotions.refresh(),error=>error.code==='Unavailable');
  else await fixture.LumoPromotions.refresh();
  assert.equal(network.length,2,'promotion lists and the dependent SKU wave each need one HTTP');
  assert.deepEqual(network[0].map(read=>read.service+'/'+read.action).sort(),['campaign/list','flash/list','group/list']);
  assert.deepEqual(network[1].map(read=>read.service+'/'+read.action).sort(),['catalog/get','inventory/get'],'catalog and stock must start in the same wave even when either fails');
  assert.equal(fixture.LumoPromotions.status().loaded,!failedService);
  assert.equal(fixture.LumoInventory.stockFor('sku-headphones'),failedService==='inventory'?0:7,'a catalog failure must not prevent the independent stock read');
  if(!failedService)assert.equal(fixture.LumoPromotions.flashOffers()[0].remaining,7);
 }
}
(async()=>{
 await window.LumoInventory.refresh();
 assert.equal(calls.length,1);assert.equal(calls[0].action,'batch_get');assert.equal(calls[0].command.data.ids.length,3);
 await window.LumoPromotions.refresh();
 const flash=window.LumoPromotions.flashOffers()[0];assert.equal(flash.price,1299);assert.equal(flash.discount,0);assert.equal(flash.skuId,'sku-headphones');assert.equal(flash.remaining,7);assert.equal(window.LumoPromotions.flashOffers('tomorrow').length,0);
 // Paid orders and refund flags never change the authoritative available amount.
 context.state.orders=[{id:'paid',items:[{id:'headphones',qty:50}]}];assert.equal(window.LumoInventory.stockFor('headphones'),50);
 const team=window.LumoPromotions.teams('group-demo:headphones')[0];assert.equal(team.joined,1);assert.equal(team.occupied,1);assert.equal(team.status,'waiting');assert.equal(team.mine,false);
 const cart=[{id:'iphone',qty:1,color:'Black',option:'256GB',selected:true}];context.state.cart=cart;
 const addon=window.LumoPromotions.exchangeOffers(cart)[0];assert.equal(addon.price,39);assert.equal(addon.eligible,true);
 const click=(data)=>{for(const callback of listeners.click||[])callback({target:{closest:selector=>selector==='[data-promo-action]'?{dataset:data,disabled:false}:null}})};
 click({promoAction:'select-exchange',offer:addon.id});await new Promise(resolve=>setTimeout(resolve,0));
 const augmented=window.LumoPromotions.augmentItems(cart);assert.equal(augmented.length,2);assert.equal(augmented[1].id,'coffee');assert.deepEqual({...window.LumoPromotions.quoteData(augmented)},{addon_sku_id:'coffee'});
 const groupOffer=window.LumoPromotions.groupOffers().find(x=>x.productId==='headphones');click({promoAction:'create-group',offer:groupOffer.id});
 await new Promise(resolve=>setTimeout(resolve,0));assert.equal(checkout.item.promotion.teamId,'team-created');assert.equal(checkout.item.sku_id,'headphones');assert.equal(window.LumoPromotions.myGroups()[0].joined,0);
 assert(calls.some(x=>x.service==='group'&&x.action==='create'));assert(!calls.some(x=>['paid','reserve','confirm'].includes(x.action)));
 click({promoAction:'buy-flash',offer:flash.id});await new Promise(resolve=>setTimeout(resolve,0));assert.equal(checkout.item.sku_id,'sku-headphones');assert.equal(checkout.item.unitPrice,1299);
 assert.deepEqual({...window.LumoPromotions.quoteData([checkout.item])},{activity_id:'flash-demo'});
 const order={id:'order-real',items:[{id:'headphones'}]};await window.LumoRecommendations.refresh(order);assert.equal(window.LumoRecommendations.rank(order)[0].product.id,'coffee');
 assert(calls.some(x=>x.service==='recommendation'&&x.command.data.order_id==='order-real'&&x.command.data.scene==='post_purchase'));assert.match(window.LumoRecommendations.render(order),/与你购买的商品品牌相近/);
 // General campaign selection carries policy IDs, never a client-calculated final price.
 context.checkoutMode='cart';context.directItem=null;context.state.cart=cart;
 const P=window.LumoPromotions;
 for(const fixture of campaignFixtures){
   context.state.cart=[{...cart[0],qty:2}];
   assert.equal(await P.selectCampaign(fixture.id,context.state.cart),true);
   assert.deepEqual({...P.quoteData(context.state.cart)},{activity_id:fixture.id});
   const evaluation=calls.filter(c=>c.service==='campaign'&&c.action==='evaluate').at(-1);
   assert.equal(evaluation.command.data.quantity,2);
   assert.equal(evaluation.command.data.subtotal_cents,1599800);
   assert.equal(P.campaignState(context.state.cart).activity.name,fixture.name);
   assert(window.LumoPromoView.page(fixture.kind).includes(fixture.name));
   assert(window.LumoPromoView.home().includes('#/promotions/'+fixture.kind));
 }
 assert(window.LumoPromoView.campaigns(context.state.cart,'checkout').includes('aria-pressed="true"'));
 context.state.cart[0].qty=1;
 assert.deepEqual({...P.quoteData(context.state.cart)},{} ,'quantity changes invalidate the campaign quote context');
 assert.equal(await P.selectCampaign('quantity-discount-demo',context.state.cart),false);
 assert.equal(await P.selectCampaign('unknown-campaign',context.state.cart),false);
 assert.equal(await P.selectCampaign('direct-demo',context.state.cart),true);
 currentUser='another-buyer';assert.deepEqual({...P.quoteData(context.state.cart)},{});
 currentUser='demo';gateway='http://another-gateway';assert.deepEqual({...P.quoteData(context.state.cart)},{});
 gateway='';
 for(const listener of listeners['lumo:login']||[])listener();
 assert.deepEqual({...P.quoteData(context.state.cart)},{} ,'a new session for the same buyer drops the old choice');
 let release;
 evaluateOverride=()=>new Promise(resolve=>release=resolve);
 const stale=P.selectCampaign('direct-demo',context.state.cart);
 currentUser='new-user';release({eligible:true,discount_cents:2000});
 assert.equal(await stale,false);assert.deepEqual({...P.quoteData(context.state.cart)},{});
 evaluateOverride=undefined;currentUser='demo';
 assert.equal(await P.selectCampaign('direct-demo',context.state.cart),true);
 click({promoAction:'select-exchange',offer:addon.id});await new Promise(resolve=>setTimeout(resolve,0));
 assert.equal(P.campaignState(context.state.cart).selectedId,null,'add-on selection clears a general campaign');
 assert.equal(P.augmentItems(context.state.cart).length,2);
 assert.equal(await P.selectCampaign('direct-demo',context.state.cart),true);
 assert.equal(P.augmentItems(context.state.cart).length,1,'a general campaign removes the selected add-on');
 // The newest valid choice wins even if evaluation responses arrive out of order.
 const responses=new Map();
 evaluateOverride=command=>new Promise(resolve=>responses.set(command.data.activity_id || 'addon',resolve));
 const oldChoice=P.selectCampaign('direct-demo',context.state.cart);
 const newChoice=P.selectCampaign('discount-demo',context.state.cart);
 responses.get('discount-demo')({eligible:true});assert.equal(await newChoice,true);
 responses.get('direct-demo')({eligible:true});assert.equal(await oldChoice,false);
 assert.deepEqual({...P.quoteData(context.state.cart)},{activity_id:'discount-demo'});
 const beforeAddon=P.selectCampaign('direct-demo',context.state.cart);
 click({promoAction:'select-exchange',offer:addon.id});
 responses.get('addon')({eligible:true});await new Promise(resolve=>setTimeout(resolve,0));
 responses.get('direct-demo')({eligible:true});assert.equal(await beforeAddon,false);
 assert.deepEqual({...P.quoteData(P.augmentItems(context.state.cart))},{addon_sku_id:'coffee'});
 P.clearExchange();
 const clearedChoice=P.selectCampaign('direct-demo',context.state.cart);
 P.clearCampaign();responses.get('direct-demo')({eligible:true});
 assert.equal(await clearedChoice,false);assert.deepEqual({...P.quoteData(context.state.cart)},{});
 evaluateOverride=undefined;
 // The selected variant price, rather than the product default, determines the preview threshold.
 context.itemPrice=item=>item.option==='500g'?179:catalog.find(p=>p.id===item.id).price;
 context.detail={id:'coffee',color:'Blend',option:'500g',qty:2};
 await P.startCampaignPurchase('reduction-demo','coffee',true);
 assert.equal(checkout.item.option,'500g');assert.equal(checkout.item.qty,2);
 assert.equal(Object.hasOwn(checkout.item,'unitPrice'),false,'campaign selection must not override backend SKU pricing');
 assert.equal(calls.filter(c=>c.service==='campaign'&&c.action==='evaluate').at(-1).command.data.subtotal_cents,35800);
 assert.deepEqual({...P.quoteData([checkout.item])},{activity_id:'reduction-demo'});
 await checkReadAggregation();
 console.log('PASS promotions API mapping, campaign eligibility, variant pricing, selection isolation, bulk inventory, and parallel catalog/stock HTTP batches including partial failures.');
})().catch(e=>{console.error(e);process.exitCode=1});
