'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');
const {createHarness, Element} = require('./dom-harness.cjs');
const directory = process.env.FRONTEND_DIR || path.join(__dirname, '../dist');
const copy = value => JSON.parse(JSON.stringify(value));
const record = configuration => ({id:'storefront-categories',kind:'storefront',title:'商城分类导航',body:JSON.stringify(configuration)});
const deferred = () => {let resolve, reject;const promise=new Promise((yes,no)=>{resolve=yes;reject=no;});return {promise,resolve,reject};};
function configuration() {
  return {version:1,groups:[
    {id:'phones',category:'手机数码',label:'手机 / 智能 / 摄影',title:'手机数码',icon:'camera',enabled:true,
      shortcuts:[{label:'全部手机数码',query:''}],
      rows:[{title:'手机通讯',query:'手机',items:[{label:'智能手机',query:'手机',featured:true},{label:'手机壳',query:'保护壳',featured:false}]},
        {title:'影音娱乐',query:'音频',items:[{label:'耳机',query:'耳机'}]}],
      brands:[{label:'苹果',brand:'Apple'},{label:'索尼',brand:'Sony'}]},
    {id:'home',category:'生活电器',label:'生活电器 / 厨房小电',title:'生活电器',icon:'layers',enabled:true,
      shortcuts:[],rows:[{title:'厨房电器',query:'厨房',items:[{label:'电水壶',query:'水壶'}]}],brands:[{label:'美的',brand:'Midea'}]}
  ]};
}

// Reuse the shared module harness. Only add the input events and dialog nodes
// needed here; assertions concern editor contracts, not browser layout.
function setup({initial=configuration(), role='admin'}={}) {
  const calls=[], applied=[], messages=[], listeners=new Map();
  let server=copy(initial), principal={user_id:'admin-a',role}, token='session-a', drawCount=0;
  let read=null, save=null, consumer=null;
  const app=createHarness(directory,'http://category-editor.test',{globals:{
    LumoAuth:{user:()=>principal},
    LumoAPI:{getBase:()=>'/api/v1',getToken:()=>token,async call(service,action,command,options) {
      calls.push(copy({service,action,command,options}));
      assert.equal(service,'content');
      if(action==='get')return read?read(command):record(server);
      assert.equal(action,'upsert');
      if(save)return save(command);
      server=JSON.parse(command.data.body);return record(server);
    }},
    LumoCategories:{get loaded(){return consumer!==null;},get configuration(){return copy(consumer);},
      apply(value){consumer=copy(value);applied.push(copy(value));}}
  }});
  const originalAdd=app.document.addEventListener.bind(app.document);
  app.document.addEventListener=(type,handler,...rest)=>{
    if(!listeners.has(type))listeners.set(type,[]);
    listeners.get(type).push(handler);originalAdd(type,handler,...rest);
  };
  const originalCreate=app.document.createElement.bind(app.document);
  app.document.createElement=tag=>{
    const element=originalCreate(tag), error=new Element('dialog-error');
    const originalQuery=element.querySelector.bind(element);
    element.querySelector=selector=>selector==='.ce-dialog-error'?error:selector==='input,button'?new Element('focus-target'):originalQuery(selector);
    element.remove=()=>{app.document.body.children=app.document.body.children.filter(child=>child!==element);};
    return element;
  };
  app.document.body.style.overflow='auto';
  app.load('category-editor.js');
  const editor=app.ctx.LumoCategoryEditor;
  editor.configure({draw(){drawCount++;},notify(message){messages.push(message);}});
  async function fire(type,target,extra={}) {
    const event={type,target,preventDefault(){},...extra};
    for(const handler of listeners.get(type)||[])await handler(event);
  }
  return {app,editor,calls,applied,messages,
    get drawCount(){return drawCount;},
    get dialog(){return app.document.body.children.find(child=>child.id==='category-editor-dialog');},
    get current(){return copy(editor.configuration);},
    setServer(value){server=copy(value);},setRead(handler){read=handler;},setSave(handler){save=handler;},
    setSession(user,nextToken='session-b'){principal=user;token=nextToken;},
    event(type){app.ctx.dispatchEvent(new app.ctx.CustomEvent(type));},
    click(action,index){return app.click({ceAction:action,...(index===undefined?{}:{ceIndex:String(index)})});},
    async input(key,value,index) {
      const target=new Element();target.dataset={[key]:value.name,...(index===undefined?{}:{ceIndex:String(index)})};
      target.value=value.value;target.checked=Boolean(value.checked);await fire('input',target);
    },
    field(name,value){return this.input('ceField',{name,value,checked:value});},
    rowField(name,value){return this.input('ceRowField',{name,value});},
    linkField(index,name,value){return this.input('ceLinkField',{name,value,checked:value},index);},
    submit(){return app.submit('category-detail-form');},
    key(key){return fire('keydown',new Element(),{key});},
    async load(){assert.equal(await editor.refresh(),true);calls.length=0;applied.length=0;},
    dispose(){app.dispose();}
  };
}

test('draft changes stay local until publication and configuration snapshots cannot mutate the editor',async t=>{
  const fixture=setup();t.after(()=>fixture.dispose());
  assert.equal(await fixture.editor.refresh(),true);
  assert.deepEqual(fixture.calls,[{service:'content',action:'get',command:{id:'storefront-categories'},options:{auth:false}}]);
  assert.deepEqual(fixture.applied,[configuration()]);
  fixture.calls.length=0;fixture.applied.length=0;
  await fixture.field('label','手机 / 平板 / 智能设备');
  await fixture.field('title','数码精选');
  assert.equal(fixture.editor.dirty,true);
  assert.equal(fixture.current.groups[0].title,'数码精选');
  const snapshot=fixture.editor.configuration;snapshot.groups[0].title='outside mutation';
  assert.equal(fixture.current.groups[0].title,'数码精选');
  assert.deepEqual(fixture.calls,[]);assert.deepEqual(fixture.applied,[]);
  await fixture.click('reload');
  assert.deepEqual(fixture.calls,[],'reload cannot silently discard a dirty draft');
});

test('admin publication writes the fixed backend document and applies the returned canonical configuration',async t=>{
  const fixture=setup();t.after(()=>fixture.dispose());await fixture.load();
  await fixture.field('title','数码精选');
  const draft=fixture.current, canonical=copy(draft);canonical.groups[0].title='后端规范化标题';
  fixture.setSave(()=>record(canonical));
  await fixture.click('publish');
  assert.equal(fixture.calls.length,1);
  assert.deepEqual(fixture.calls[0],{service:'content',action:'upsert',command:{id:'storefront-categories',data:{kind:'storefront',title:'商城分类导航',body:JSON.stringify(draft)}},options:{auth:true}});
  assert.deepEqual(fixture.current,canonical);assert.deepEqual(fixture.applied,[canonical]);
  assert.equal(fixture.editor.dirty,false);assert.deepEqual(fixture.messages,['分类导航已保存并发布']);
  assert.match(fixture.editor.render(),/已与后端配置同步/);
});

test('ordinary users cannot edit or publish even when action events are forged',async t=>{
  const fixture=setup({role:'user'});t.after(()=>fixture.dispose());await fixture.load();
  const before=fixture.current;
  await fixture.field('title','forged title');await fixture.click('add-group');
  await fixture.click('edit-brands');await fixture.click('publish');
  assert.deepEqual(fixture.current,before);assert.equal(fixture.editor.dirty,false);
  assert.deepEqual(fixture.calls,[]);assert.deepEqual(fixture.applied,[]);assert.equal(fixture.dialog,undefined);
  assert.match(fixture.editor.render(),/请使用管理员账号登录/);
});

test('failed publication preserves the draft and permits retry without exposing unsafe backend text',async t=>{
  const fixture=setup();t.after(()=>fixture.dispose());await fixture.load();
  await fixture.field('title','保留我的修改');const draft=fixture.current;
  fixture.setSave(()=>{throw Error('<img src=x onerror=alert(1)> 保存失败');});
  await fixture.click('publish');
  assert.deepEqual(fixture.current,draft);assert.equal(fixture.editor.dirty,true);
  assert.deepEqual(fixture.applied,[]);assert.deepEqual(fixture.messages,[]);
  assert.match(fixture.editor.render(),/&lt;img src=x onerror=alert\(1\)&gt; 保存失败/);
  assert.doesNotMatch(fixture.editor.render(),/<img src=x/);
  fixture.setSave(null);await fixture.click('publish');
  assert.equal(fixture.calls.length,2);assert.deepEqual(fixture.calls[0].command,fixture.calls[1].command);
  assert.equal(fixture.editor.dirty,false);assert.deepEqual(fixture.applied,[draft]);
});

test('a late publish from an old login cannot replace the new session configuration',async t=>{
  const fixture=setup();t.after(()=>fixture.dispose());await fixture.load();
  await fixture.field('title','旧会话草稿');const oldDraft=fixture.current;
  const pending=deferred();fixture.setSave(()=>pending.promise);
  const publication=fixture.click('publish');
  assert.equal(fixture.calls.length,1);
  fixture.setSession({user_id:'admin-b',role:'admin'});fixture.event('lumo:login');
  const next=configuration();next.groups[0].title='新会话配置';fixture.setServer(next);
  assert.equal(await fixture.editor.refresh(),true);
  pending.resolve(record(oldDraft));await publication;
  assert.deepEqual(fixture.current,next);assert.deepEqual(fixture.applied,[next]);
  assert.equal(fixture.editor.dirty,false);assert.deepEqual(fixture.messages,[]);
});

test('late refresh results after logout cannot repopulate private editor state or apply stale navigation',async t=>{
  const fixture=setup();t.after(()=>fixture.dispose());
  const pending=deferred();fixture.setRead(()=>pending.promise);
  const refresh=fixture.editor.refresh();
  fixture.setSession(null,'');fixture.event('lumo:logout');
  pending.resolve(record(configuration()));
  assert.equal(await refresh,false);assert.equal(fixture.current,null);
  assert.deepEqual(fixture.applied,[]);assert.deepEqual(fixture.messages,[]);
  assert.match(fixture.editor.render(),/请使用管理员账号登录/);
});

test('ordering and disabling groups remain draft operations and revert restores the backend order',async t=>{
  const fixture=setup();t.after(()=>fixture.dispose());await fixture.load();
  await fixture.click('group-down',0);await fixture.field('enabled',false);
  assert.deepEqual(fixture.current.groups.map(group=>[group.id,group.enabled]),[['home',true],['phones',false]]);
  assert.match(fixture.editor.render(),/已隐藏/);
  await fixture.click('publish');
  const saved=JSON.parse(fixture.calls[0].command.data.body);
  assert.deepEqual(saved.groups.map(group=>[group.id,group.enabled]),[['home',true],['phones',false]]);
  await fixture.click('group-up',1);await fixture.field('enabled',true);
  await fixture.click('revert');
  assert.deepEqual(fixture.current,saved);assert.equal(fixture.editor.dirty,false);
  assert.equal(fixture.calls.length,1);assert.equal(fixture.applied.length,1);
});

test('row dialogs stage item order, recommended flags, and search defaults without saving to the backend',async t=>{
  const fixture=setup();t.after(()=>fixture.dispose());await fixture.load();
  await fixture.click('edit-row',0);
  assert.equal(fixture.app.document.body.style.overflow,'hidden');
  await fixture.rowField('title','  智能通讯  ');await fixture.rowField('query','');
  await fixture.linkField(1,'label','  保护套  ');await fixture.linkField(1,'query','');
  await fixture.linkField(1,'featured',true);await fixture.click('link-up',1);
  await fixture.click('add-link');await fixture.linkField(2,'label','临时词');
  await fixture.click('delete-link',2);
  assert.equal(fixture.current.groups[0].rows[0].title,'手机通讯','dialog edits do not leak before its save');
  await fixture.submit();
  assert.deepEqual(fixture.current.groups[0].rows[0],{title:'智能通讯',query:'智能通讯',items:[{label:'保护套',query:'保护套',featured:true},{label:'智能手机',query:'手机',featured:true}]});
  assert.equal(fixture.dialog,undefined);assert.equal(fixture.app.document.body.style.overflow,'auto');
  assert.deepEqual(fixture.calls,[]);assert.deepEqual(fixture.applied,[]);
  const saved=fixture.current;
  await fixture.click('edit-row',0);await fixture.rowField('title','取消的修改');await fixture.key('Escape');
  assert.deepEqual(fixture.current,saved);assert.equal(fixture.dialog,undefined);
});

test('new rows, shortcuts, and brand filters preserve their distinct query semantics',async t=>{
  const fixture=setup();t.after(()=>fixture.dispose());await fixture.load();
  await fixture.click('add-row');await fixture.rowField('title',' 平板电脑 ');
  await fixture.click('add-link');await fixture.linkField(0,'label',' 学习平板 ');await fixture.submit();
  assert.deepEqual(fixture.current.groups[0].rows[2],{title:'平板电脑',query:'平板电脑',items:[{label:'学习平板',query:'学习平板',featured:false}]});
  await fixture.click('row-up',2);
  assert.deepEqual(fixture.current.groups[0].rows.map(row=>row.title),['手机通讯','平板电脑','影音娱乐']);
  await fixture.click('edit-shortcuts');await fixture.linkField(0,'label',' 全部数码 ');await fixture.submit();
  assert.deepEqual(fixture.current.groups[0].shortcuts,[{label:'全部数码',query:''}]);
  await fixture.click('edit-brands');await fixture.click('add-link');
  await fixture.linkField(2,'label','华为');await fixture.submit();
  assert.match(fixture.dialog.querySelector('.ce-dialog-error').textContent,/请填写第3项的商品品牌值/);
  assert.equal(fixture.current.groups[0].brands.length,2,'invalid dialog values do not partially update the draft');
  await fixture.linkField(2,'brand',' HUAWEI ');await fixture.click('link-up',2);await fixture.submit();
  assert.deepEqual(fixture.current.groups[0].brands,[{label:'苹果',brand:'Apple'},{label:'华为',brand:'HUAWEI'},{label:'索尼',brand:'Sony'}]);
  assert.deepEqual(fixture.calls,[]);assert.equal(fixture.editor.dirty,true);
});

test('backend text is escaped in both the main editor and editable dialog attributes',async t=>{
  const initial=configuration(), attack='"><img src=x onerror="alert(1)"><script>alert(2)</script>&\'';
  const group=initial.groups[0];
  group.title=group.label=group.category=attack;group.shortcuts[0].label=group.shortcuts[0].query=attack;
  group.rows[0].title=group.rows[0].query=attack;group.rows[0].items[0].label=group.rows[0].items[0].query=attack;
  group.brands[0].label=group.brands[0].brand=attack;
  const fixture=setup({initial});t.after(()=>fixture.dispose());await fixture.load();
  const safe=html=>{assert.doesNotMatch(html,/<(?:img|script)\b/);assert.match(html,/&quot;&gt;&lt;img src=x onerror=&quot;alert\(1\)&quot;&gt;/);assert.match(html,/&amp;&#39;/);};
  safe(fixture.editor.render());
  for(const action of ['edit-row','edit-shortcuts','edit-brands']){
    await fixture.click(action,0);safe(fixture.dialog.innerHTML);await fixture.click('close-dialog');
  }
  assert.deepEqual(fixture.calls,[]);assert.equal(fixture.editor.dirty,false);
});
