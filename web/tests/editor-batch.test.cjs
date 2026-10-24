'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');
const {createHarness} = require('./dom-harness.cjs');
const directory = process.env.FRONTEND_DIR || path.join(__dirname, '../dist');
const tick = () => new Promise(resolve => setImmediate(resolve));

function setup({missing = [], failure = null, incomplete = false} = {}) {
  const product = {id:'phone',skuId:'sku-a',name:'Variant Phone',category:'手机数码',price:100,old:200,stock:50,image:'phone.jpg',visible:true,colors:['黑色'],options:['128GB','256GB','512GB'],defaultOption:'128GB',variants:[
    {sku_id:'sku-a',color:'黑色',option:'128GB',price_cents:10000},
    {sku_id:'sku-b',color:'黑色',option:'256GB',price_cents:20000},
    {sku_id:'sku-c',color:'黑色',option:'512GB',price_cents:30000}
  ]};
  const calls = [], messages = [];
  let modal = '', principal = {id:'admin',role:'admin'};
  const app = createHarness(directory,'http://editor.test',{globals:{
    products:[product],presentationProducts:[product],categories:['全部好物','手机数码'],
    LumoAuth:{isLoggedIn:()=>!!principal,user:()=>principal,requireLogin:()=>!!principal},
    LumoAPI:{async call(service,action,command={}) {
      calls.push({service,action,command});
      if(service==='inventory'&&action==='batch_get') {
        if(failure)throw failure;
        return {items:command.data.ids.filter(id=>!missing.includes(id)).map(id=>({sku_id:id,on_hand:id==='sku-b'?12:7,available:5})),missing_ids:incomplete?[]:missing};
      }
      if(service==='catalog'&&action==='upsert')return {id:'phone',variants:product.variants};
      if(service==='catalog'&&action==='list')return {items:[product]};
      if(service==='content'&&action==='list')return {items:[]};
      if(service==='inventory'&&action==='get')throw new Error('per-variant inventory requests are forbidden');
      return {};
    }},
    LumoCommerce:{async refreshCatalog(){},normalizeProduct:row=>row},
    LumoInventory:{async refresh(){},stockFor:()=>5},LumoDiscovery:{async refresh(){}},
    esc:value=>String(value??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c])),
    money:String,icon:()=>'',header:()=>'',footer:()=>'',render(){},toast:message=>messages.push(message),
    modal:(_title,body)=>{modal=body;},closeModal(){modal='';}
  }});
  app.load('editor.js');
  return {app,calls,messages,get modal(){return modal;},setUser(value){principal=value;}};
}

test('opening the editor reads every variant stock in one fresh batch',async()=>{
  const fixture=setup();
  try {
    fixture.app.navigate('editor');
    await fixture.app.click({editorAction:'edit-product',id:'phone'});await tick();
    const stock=fixture.calls.filter(c=>c.service==='inventory');
    assert.equal(stock.length,1);assert.equal(stock[0].action,'batch_get');
    assert.deepEqual([...stock[0].command.data.ids],['sku-a','sku-b','sku-c']);
    assert.match(fixture.modal,/name="stock_variant_0"[^>]*value="12"/);
    assert.match(fixture.modal,/name="stock_variant_1"[^>]*value="7"/);
  } finally {fixture.app.dispose();}
});

test('editing fails visibly when the batch is incomplete and does not invent stock',async()=>{
  const fixture=setup({missing:['sku-b']});
  try {
    await fixture.app.click({editorAction:'edit-product',id:'phone'});await tick();
    assert.equal(fixture.modal,'');
    assert.match(fixture.messages.join(' '),/库存缺失/);
    assert.equal(fixture.calls.filter(c=>c.service==='inventory'&&c.action==='adjust').length,0);
  } finally {fixture.app.dispose();}
});

test('publishing changes checks stock once and initializes only missing variants',async()=>{
  const fixture=setup({missing:['sku-b']});
  try {
    fixture.app.navigate('editor');
    await fixture.app.click({editorAction:'toggle-product',id:'phone'});
    const stock=fixture.calls.filter(c=>c.service==='inventory');
    assert.deepEqual(stock.map(c=>c.action),['batch_get','adjust']);
    assert.equal(stock[1].command.id,'sku-b');
    assert.equal(stock[1].command.data.on_hand,50);
    assert.ok(fixture.calls.some(c=>c.service==='content'&&c.action==='upsert'));
    assert.equal(fixture.app.failures.length,0);
  } finally {fixture.app.dispose();}
});

test('failed stock batch never falls back to per-SKU reads or resets existing stock',async()=>{
  const fixture=setup({failure:new Error('inventory offline')});
  try {
    fixture.app.navigate('editor');
    await fixture.app.click({editorAction:'toggle-product',id:'phone'});
    assert.deepEqual(fixture.calls.filter(c=>c.service==='inventory').map(c=>c.action),['batch_get']);
    assert.match(fixture.messages.join(' '),/库存更新未完成/);
    assert.ok(!fixture.calls.some(c=>c.service==='content'&&c.action==='upsert'));
  } finally {fixture.app.dispose();}
});

test('admin homepage applies public theme without loading disabled catalog; editor entry loads it',async()=>{
  const fixture=setup();
  try {
    await fixture.app.ctx.LumoStudio.refresh();
    assert.deepEqual(fixture.calls.map(c=>c.service+'/'+c.action),['content/list']);
    fixture.app.navigate('editor');await fixture.app.ctx.LumoStudio.refresh();
    const catalogs=fixture.calls.filter(c=>c.service==='catalog');
    assert.equal(catalogs.length,1);assert.equal(catalogs[0].command.data.include_disabled,true);
    fixture.setUser({id:'buyer',role:'user'});
    await fixture.app.click({editorAction:'edit-product',id:'phone'});await tick();
    assert.equal(fixture.calls.filter(c=>c.service==='inventory').length,0);
  } finally {fixture.app.dispose();}
});


test('an incomplete stock response cannot initialize existing or unknown stock rows',async()=>{
  const fixture=setup({missing:['sku-b'],incomplete:true});
  try {
    fixture.app.navigate('editor');
    await fixture.app.click({editorAction:'toggle-product',id:'phone'});
    assert.deepEqual(fixture.calls.filter(c=>c.service==='inventory').map(c=>c.action),['batch_get']);
    assert.match(fixture.messages.join(' '),/库存查询结果不完整/);
  } finally {fixture.app.dispose();}
});
