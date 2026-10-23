const fs=require('node:fs');
const vm=require('node:vm');
const assert=require('node:assert/strict');
const path=require('node:path');
const dir=process.env.FRONTEND_DIR || path.resolve(__dirname,'../dist');
const listeners={};const calls=[];let checkout;
const catalog=[
{id:'headphones',skuId:'headphones',price:2299,visible:true,name:'Headphones',colors:['Black'],options:['Standard'],defaultOption:'Standard',image:'headphones.jpg'},
{id:'coffee',skuId:'coffee',price:99,visible:true,name:'Coffee',colors:['Blend'],options:['250g'],defaultOption:'250g',image:'coffee.jpg'},
{id:'iphone',skuId:'iphone',price:7999,visible:true,name:'Phone',colors:['Black'],options:['256GB'],defaultOption:'256GB',image:'iphone-black.png'}];
let groups=[{id:'team-remote',sku_id:'headphones',creator_id:'other',required:2,paid:1,occupied:1,status:'OPEN',expires_at:new Date(Date.now()+1800000).toISOString()}];
const window={LumoAuth:{user:()=>({id:'demo'}),requireLogin:()=>true},addEventListener:(type,fn)=>(listeners[type]??=[]).push(fn),dispatchEvent:()=>{}};
window.LumoAPI={call:async(service,action,command={},options={})=>{
 calls.push({service,action,command,options});
 if(service==='inventory')return {sku_id:command.id,available:command.id==='sku-headphones'?7:50};
 if(service==='catalog')return {id:'sku-headphones',product_id:'headphones',price_cents:129900,active:true};
 if(service==='campaign'&&action==='evaluate')return {eligible:true,discount_cents:6000};
 if(service==='campaign')return {items:[{id:'flash-demo',kind:'flash',quota:20,held:2,used:1,active:true,discount_cents:0},{id:'group-demo',kind:'group',quota:30,held:0,used:0,active:true,discount_cents:0},{id:'addon-coffee',kind:'addon',quota:50,held:0,used:0,active:true,threshold_cents:100000,discount_cents:6000}]};
 if(service==='flash')return {items:[{id:'flash-demo',sku_id:'sku-headphones',activity_id:'flash-demo',active:true}]};
 if(service==='group'&&action==='list')return {items:groups};
 if(service==='group'&&action==='get')return groups.find(g=>g.id===command.id);
 if(service==='group'&&action==='create'){let row={id:'team-created',sku_id:command.data.sku_id,creator_id:'demo',required:2,paid:0,occupied:0,status:'OPEN',expires_at:new Date(Date.now()+1800000).toISOString()};groups.push(row);return row;}
 if(service==='recommendation')return {items:[{id:'coffee',product_id:'coffee',recommendation_reason:'available_related_catalog'}],strategy:'catalog_available_v1',personalized_model:false};
 throw new Error('Unexpected '+service+'/'+action);
}};
const document={addEventListener:(type,fn)=>(listeners[type]??=[]).push(fn),querySelectorAll:()=>[],querySelector:()=>null};
const context=vm.createContext({window,document,products:catalog,state:{cart:[],orders:[]},location:{hash:'#/checkout',origin:'http://localhost',pathname:'/'},checkoutMode:'cart',directItem:null,detail:{id:'headphones'},crypto:require('node:crypto').webcrypto,CustomEvent:class{},navigator:{clipboard:{writeText:async()=>{}}},setInterval,clearInterval,Date,console,toast:()=>{},renderKeepScroll:()=>{},setCheckout:(mode,item)=>{checkout={mode,item}},go:()=>{},modal:()=>{},money:String,esc:String,icon:()=>'',asset:s=>s,productCard:p=>p.name});
for(const f of ['inventory.js','promotions.js','recommendations.js'])vm.runInContext(fs.readFileSync(path.join(dir,f),'utf8'),context,{filename:f});
(async()=>{
 await window.LumoInventory.refresh();await window.LumoPromotions.refresh();
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
 assert(calls.some(x=>x.service==='recommendation'&&x.command.data.order_id==='order-real'));
 console.log('PASS promotions API mapping, flash exact SKU/price/stock, no local stock arithmetic, add-on quote metadata, real group create, backend recommendation.');
})().catch(e=>{console.error(e);process.exitCode=1});
