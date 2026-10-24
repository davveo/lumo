const fs=require('node:fs'), vm=require('node:vm'), assert=require('node:assert/strict');
const source=require('node:path').resolve(process.env.FRONTEND_DIR || require('node:path').join(__dirname,'../dist')) + require('node:path').sep;
const tick=()=>new Promise(resolve=>setImmediate(resolve));
function setup(file, backend){
 const calls=[],handlers={};let principal={id:'demo',role:'user'};
 const products=[{id:'iphone',skuId:'iphone',name:'Apple Phone',category:'手机数码',brand:'Apple',price:7999,old:8999,image:'phone.png',colors:['黑色'],options:['256GB'],defaultOption:'256GB',variants:[{sku_id:'iphone',color:'黑色',option:'256GB',price_cents:799900}],highlights:[],highSubs:[]},{id:'coffee',name:'咖啡',category:'食品生鲜',brand:'LUMO',price:99,image:'coffee.jpg',colors:['经典'],options:['250g'],defaultOption:'250g',variants:[{sku_id:'coffee',color:'经典',option:'250g',price_cents:9900}],highlights:[],highSubs:[]}];
 const document={readyState:'complete',documentElement:{style:{setProperty(){}}},addEventListener(name,fn){(handlers[name] ||= []).push(fn)},querySelector(){return null},body:{classList:{toggle(){}}}};
 const window={LumoAPI:{async call(service,action,command={},options={}){calls.push({service,action,command,options});return backend(service,action,command,options)}},LumoAuth:{user:()=>principal,isLoggedIn:()=>!!principal,requireLogin:()=>!!principal},LumoMember:{model:()=>({profile:{nickname:'Tester'}})},scrollTo(){}};
 const ctx={window,document,products,presentationProducts:products,state:{},ui:{query:'',category:'全部好物',sort:'综合'},location:{hash:'#/product/iphone',origin:'http://localhost',pathname:'/'},categories:['全部好物','手机数码','食品生鲜'],categoryImages:[],crypto:{randomUUID:()=>String(calls.length)},setTimeout,clearTimeout,Map,Set,console,esc:String,asset:String,money:String,icon:()=>'',price:String,productCard:p=>p.name,save(){},toast(){},header:()=>'',footer:()=>'',go(){},closeModal(){},renderKeepScroll(){},render(){},FormData:global.FormData,Date};
 vm.createContext(ctx);vm.runInContext(fs.readFileSync(source+file,'utf8'),ctx);
 return {ctx,window,document,calls,handlers,setUser(value){principal=value}};
}
(async()=>{
 const remote={compare:[{sku_id:'iphone'}],search:[{id:'search-a',query:'手机'}]};
 const a=setup('discovery.js',async(service,action,c)=>{
   if(service==='engagement'&&action==='list')return {items:remote[c.data.kind]};
   if(service==='search')return {items:[{id:'iphone'}]};
   return {accepted:true};
 });
 await a.window.LumoDiscovery.refresh();
 assert.equal(a.window.LumoDiscovery.filter(a.ctx.products).length,2);
 assert.equal(a.calls.filter(c=>c.service==='search').length,0,'browse filters reuse the already loaded catalog');
 a.ctx.ui.query='Apple';
 await a.window.LumoDiscovery.refreshSearch();
 assert.equal(a.window.LumoDiscovery.filter(a.ctx.products).length,1);
 assert.match(a.window.LumoDiscovery.compareBar(),/Apple Phone/);
 a.window.LumoDiscovery.commitSearch('Apple buyer@example.test 13800138000');await tick();
 const behavior=a.calls.find(c=>c.service==='analytics');
 assert.equal(behavior.command.data.type,'search');
 assert.equal(behavior.command.data.query,'Apple');
 a.setUser(null);
 assert.equal(a.window.LumoDiscovery.compareBar(),'');
 assert.doesNotMatch(a.window.LumoDiscovery.page(),/disc-compare-table-panel/);
 const review={id:'review-1',user_id:'other',status:'PUBLISHED',sku_id:'iphone',rating:5,body:'Actual published review',helpful_count:4,liked:false,created_at:'2026-10-22T00:00:00Z'};
 const b=setup('product-extras.js',async(service,action,c)=>{
   if(service==='recommendation')return {items:[{id:'coffee'}]};
   if(action==='list')return {items:c.data.kind==='review'?[review]:c.data.mine?[{id:'question-1',user_id:'demo',status:'PENDING',sku_id:'iphone',body:'My pending question',created_at:'2026-10-22T00:00:00Z'}]:[]};
   if(action==='like')return {liked:true,helpful_count:5};return {};
 });
 await b.window.LumoProductExtras.refresh('iphone');
 assert.match(b.window.LumoProductExtras.reviews('iphone'),/Actual published review/);
 assert.match(b.window.LumoProductExtras.questions('iphone'),/My pending question/);
 assert.doesNotMatch(b.window.LumoProductExtras.questions('iphone'),/删除提问/);
 const button={disabled:false,dataset:{productExtraAction:'helpful',product:'iphone',id:'review-1'}};
 for(const fn of b.handlers.click)await fn({target:{closest:()=>button}});
 assert.match(b.window.LumoProductExtras.reviews('iphone'),/有帮助 5/);
 assert.equal(b.calls.filter(c=>c.action==='like').length,1);
 b.setUser(null);
 assert.equal(b.window.LumoProductExtras.questionData('iphone').length,0);
 let release;
 const pending=new Promise(resolve=>release=resolve);
 const c=setup('product-extras.js',async(service,action,command)=>{
   if(service==='recommendation')return {items:[]};
   if(command.data.mine) {await pending;return {items:[{id:'private',user_id:'demo',sku_id:'iphone',status:'PENDING',body:'Old account private question'}]};}
   return {items:[]};
 });
 const beforeLogout=c.window.LumoProductExtras.refresh('iphone');await tick();
 c.setUser(null);await c.window.LumoProductExtras.refresh('iphone');release();await beforeLogout;
 assert.equal(c.window.LumoProductExtras.questionData('iphone').length,0,'late authenticated response leaked after logout');
 const d=setup('editor.js',async()=>({items:[]}));
 assert.match(d.window.LumoStudio.renderEditor(),/管理员账号/);
 assert.equal(d.calls.length,0,'ordinary user editor unexpectedly mutated server');
 console.log('PASS discovery server search/compare ownership, behavior redaction, real QA/helpful, late-response account isolation, editor admin guard');
})().catch(error=>{console.error(error);process.exitCode=1});
