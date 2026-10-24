'use strict';
const test=require('node:test');
const assert=require('node:assert/strict');
const path=require('node:path');
const {createHarness}=require('./dom-harness.cjs');
const directory=process.env.FRONTEND_DIR || path.join(__dirname,'../dist');
const json=value=>JSON.parse(JSON.stringify(value));
const product=(id,reason='与你偏好的品类相近')=>({id,name:'推荐商品 '+id,price_cents:12900,category:'手机数码',brand:'LUMO',image:'headphones.jpg',active:true,recommendation_reason:reason});
const titles=html=>[...html.matchAll(/class="product-title"[^>]*>([^<]+)/g)].map(row=>row[1]);
function setup(handler=async()=>({items:[]})) {
  let user='buyer',token='session-a',base='http://shop.test',ticket=0;
  const calls=[],primes=[];
  const app=createHarness(directory,base,{globals:{
    LumoAPI:{getBase:()=>base,getToken:()=>token,async call(service,action,command={},options={}) {calls.push({service,action,command:json(command),options:json(options)});return handler(service,action,command,options);}},
    LumoAuth:{user:()=>user?{id:user}:null,isLoggedIn:()=>!!user,gate:()=>true,requireLogin:()=>!!user,afterRender(){}},
    LumoInventory:{stockFor:()=>50,limit:()=>10,badge:()=>'',hydrationTicket:()=>++ticket,prime:(rows,version)=>primes.push({rows:json(rows),version})}
  }});
  app.load('app.js');app.evaluate('products.push(...presentationProducts.slice(0,4))');app.load('recommendations.js');app.ctx.LumoRecommendations.catalogChanged();
  return {app,calls,primes,recommendations:app.ctx.LumoRecommendations,setContext(next){if('user'in next)user=next.user;if('token'in next)token=next.token;if('base'in next)base=next.base;}};
}

test('home follows server rank with real reasons and respects every explicit sort/filter',async()=>{
  const fixture=setup(async()=>({items:[{id:'macbook',recommendation_reason:'更符合你的品牌偏好'},{id:'iphone',recommendation_reason:'与你最近浏览的好物相近'}],personalized:true,personalized_model:false}));
  const {app,recommendations,calls}=fixture;
  try {
    await recommendations.refreshHome();
    let html=app.navigate('');
    assert.match(html,/<h2>为你推荐<\/h2>/);
    assert.match(html,/更符合你的品牌偏好/);
    assert.deepEqual(titles(html).slice(0,2),['MacBook Air 13 英寸','Apple iPhone 15 Pro']);
    assert.deepEqual(calls[0].command,{data:{scene:'home',limit:20}});
    app.evaluate("ui.sort='价格低'");html=app.navigate('');assert.equal(titles(html)[0],'LUMO 简约智能腕表');assert.doesNotMatch(html,/更符合你的品牌偏好/);
    app.evaluate("ui.sort='销量'");assert.equal(titles(app.navigate(''))[0],'Apple iPhone 15 Pro');
    app.evaluate("ui.sort='综合';ui.category='手机数码'");assert.deepEqual(titles(app.navigate('')),['Apple iPhone 15 Pro','索尼 WH-1000XM5 无线降噪耳机']);
    app.evaluate("ui.category='全部好物';ui.query='索尼'");assert.deepEqual(titles(app.navigate('')),['索尼 WH-1000XM5 无线降噪耳机']);
  } finally {app.dispose();}
});

test('cold start is labelled honestly and response text is escaped',async()=>{
  const {app,recommendations}=setup(async()=>({items:[{id:'iphone',recommendation_reason:'<script>fake()</script>'}],personalized:false,personalized_model:true}));
  try {
    await recommendations.refreshHome();const html=app.navigate('');
    assert.match(html,/<h2>精选推荐<\/h2>/);assert.doesNotMatch(html,/<h2>为你推荐<\/h2>/);
    assert.match(html,/&lt;script&gt;fake\(\)&lt;\/script&gt;/);assert.doesNotMatch(html,/<script>fake/);
  } finally {app.dispose();}
});

test('full metadata outside the first catalog page is normalized and fresh stock is primed without another call',async()=>{
  const fixture=setup(async()=>({items:[product('old-but-relevant')],inventory_items:[{sku_id:'old-but-relevant',available:9}],personalized:true}));
  try {
    await fixture.recommendations.refreshHome();
    const p=fixture.app.json("products.find(p=>p.id==='old-but-relevant')");
    assert.equal(p.name,'推荐商品 old-but-relevant');assert.equal(p.price,129);assert.equal(p.skuId,'old-but-relevant');
    assert.match(fixture.app.navigate('product/old-but-relevant'),/推荐商品 old-but-relevant/);
    assert.deepEqual(fixture.primes,[{rows:[{sku_id:'old-but-relevant',available:9}],version:1}]);
    assert.equal(fixture.calls.length,1);
  } finally {fixture.app.dispose();}
});

test('home refresh retains only a bounded current recommendation pool',async()=>{
  let sequence=0;
  const {app,recommendations}=setup(async()=>({items:Array.from({length:20},(_,i)=>product('pool-'+sequence+'-'+i)),personalized:true}));
  try {
    for(sequence=0;sequence<20;sequence++)await recommendations.refreshHome();
    assert.equal(app.json('products').length,24);
    assert.equal(app.json("products.filter(p=>p.id.startsWith('pool-19-'))").length,20);
    assert.equal(app.json("products.filter(p=>p.id.startsWith('pool-0-'))").length,0);
  } finally {app.dispose();}
});

for(const change of [{user:'other',token:'session-b'},{token:'new-session-same-user'},{base:'http://other.test'}]) {
  test('late home response cannot change a new account/session/gateway '+JSON.stringify(change),async()=>{
    let release;
    const fixture=setup(()=>new Promise(resolve=>release=resolve));
    try {
      const pending=fixture.recommendations.refreshHome();fixture.setContext(change);
      release({items:[product('private-old')],personalized:true,inventory_items:[{sku_id:'private-old',available:8}]});await pending;
      assert.equal(fixture.app.json("products.some(p=>p.id==='private-old')"),false);
      assert.equal(fixture.recommendations.homeModel().count,0);assert.equal(fixture.primes.length,0);
    } finally {fixture.app.dispose();}
  });
}

test('post-purchase sends its scene, excludes this order, and uses the server explanation',async()=>{
  const fixture=setup(async()=>({items:[{id:'iphone',recommendation_reason:'should exclude'},{id:'macbook',recommendation_reason:'与你购买的品牌相近'}],personalized:true}));
  try {
    const order={id:'paid-order',items:[{id:'iphone'}]};await fixture.recommendations.refresh(order);
    assert.deepEqual(fixture.calls[0].command,{data:{scene:'post_purchase',order_id:'paid-order',limit:20}});
    assert.deepEqual(json(fixture.recommendations.rank(order).map(row=>row.product.id)),['macbook']);
    assert.match(fixture.recommendations.render(order),/与你购买的品牌相近/);assert.doesNotMatch(fixture.recommendations.render(order),/should exclude/);
    fixture.setContext({token:'new-session'});assert.equal(fixture.recommendations.rank(order).length,0);
  } finally {fixture.app.dispose();}
});

test('similar results use the current product and keep the correct cached explanation per product',async()=>{
  const fixture=setup(async(service,action,c)=>service==='interaction'?{items:[]}:{items:c.data.product_id==='iphone'?[{id:'macbook',recommendation_reason:'与手机品牌相近'}]:[{id:'watch',recommendation_reason:'与耳机价格相近'}]});
  try {
    fixture.app.load('product-extras.js');const extras=fixture.app.ctx.LumoProductExtras;
    await extras.refresh('iphone');await extras.refresh('headphones');
    const calls=fixture.calls.filter(c=>c.service==='recommendation');
    assert.deepEqual(calls.map(c=>c.command.data),[{scene:'similar',product_id:'iphone',limit:8},{scene:'similar',product_id:'headphones',limit:8}]);
    assert.match(extras.related(fixture.app.json("products.find(p=>p.id==='iphone')")),/与手机品牌相近/);
    assert.doesNotMatch(extras.related(fixture.app.json("products.find(p=>p.id==='iphone')")),/与耳机价格相近/);
    fixture.setContext({base:'http://other.test'});assert.equal(extras.related(fixture.app.json("products.find(p=>p.id==='iphone')")),'');
  } finally {fixture.app.dispose();}
});

test('recommendation outage does not erase successful product reviews',async()=>{
  const fixture=setup(async(service,action,c)=>{
    if(service==='recommendation')throw Error('recommendation offline');
    return {items:c.data.kind==='review'&&!c.data.mine?[{id:'review-real',sku_id:'iphone',user_id:'other',status:'PUBLISHED',rating:5,body:'真实评价',created_at:new Date().toISOString()}]:[]};
  });
  try {
    fixture.app.load('product-extras.js');await fixture.app.ctx.LumoProductExtras.refresh('iphone');
    assert.match(fixture.app.ctx.LumoProductExtras.reviews('iphone'),/真实评价/);
  } finally {fixture.app.dispose();}
});

test('home hydration consumes the bootstrap fragment without an extra HTTP request',async()=>{
  const network=[];
  const row=product('home-choice');
  const app=createHarness(directory,'http://shop.test',{globals:{
    LumoAuth:{user:()=>({id:'buyer'}),isLoggedIn:()=>true,gate:()=>true,afterRender(){}},
    async fetch(input,options){
      network.push(input);assert.ok(input.endsWith('/storefront/bootstrap'));
      return {status:200,ok:true,headers:{get:()=>''},json:async()=>({data:{results:[
        {service:'catalog',action:'list',command:{data:{limit:100}},auth:false,data:{items:[row]}},
        {service:'recommendation',action:'list',command:{data:{scene:'home',limit:20}},auth:true,data:{items:[row],personalized:true}}
      ]}})};
    }
  }});
  try {
    app.load('api-client.js');app.load('app.js');app.load('recommendations.js');app.ctx.LumoAPI.setToken('a'.repeat(64));
    await app.ctx.LumoAPI.prefetchBootstrap();await app.ctx.LumoCommerce.refreshCatalog();await app.ctx.LumoRecommendations.refreshHome();
    assert.equal(network.length,1);assert.equal(app.ctx.LumoRecommendations.homeModel().count,1);
  } finally {app.ctx.LumoAPI.endBootstrap();app.dispose();}
});
