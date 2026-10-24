(function () {
  'use strict';
  const esc = value => String(value ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
  const clone = value => JSON.parse(JSON.stringify(value));
  const icons = [['layers','家居与家电'],['chip','电脑与工具'],['camera','数码'],['sparkles','精选'],['bag','购物'],['heart','生活'],['shield','健康'],['edit','图书'],['clock','钟表'],['truck','汽车']];
  let adapter = {}, scope = '', revision = 0, published = null, draft = null, selected = 0, dirty = false, busy = false, warning = '', editing = null, dialog = null, previousOverflow = '';
  const API = () => window.LumoAPI;
  const authorized = () => adapter.isAdmin ? adapter.isAdmin() : window.LumoAuth?.user?.()?.role === 'admin';
  const currentScope = () => adapter.scope ? adapter.scope() : [API()?.getBase?.() || '', API()?.getToken?.() || '', window.LumoAuth?.user?.()?.user_id || ''].join('|');
  const call = (...args) => adapter.call ? adapter.call(...args) : API().call(...args);
  const notify = text => adapter.notify ? adapter.notify(text) : typeof toast === 'function' ? toast(text) : undefined;
  const draw = () => adapter.draw ? adapter.draw() : typeof renderKeepScroll === 'function' ? renderKeepScroll() : undefined;
  function reset() { revision++;closeDialog();published=null;draft=null;selected=0;dirty=false;busy=false;warning='';scope=currentScope(); }
  function checkScope() { if (scope !== currentScope()) reset(); }
  function adopt(value) {
    const raw = value?.body !== undefined ? JSON.parse(value.body) : value;
    if (raw?.version !== 1 || !Array.isArray(raw.groups)) throw Error('分类配置不完整，请重新加载');
    published=clone(raw);draft=clone(raw);selected=Math.min(selected,Math.max(0,draft.groups.length-1));dirty=false;warning='';
  }
  function synchronize() {
    checkScope();
    if (!adapter.call && window.LumoCategories?.loaded && !dirty && !busy) adopt(window.LumoCategories.configuration);
  }
  async function refresh() {
    checkScope();const ticket=++revision, origin=scope;
    busy=true;warning='';
    try {
      const result=await call('content','get',{id:'storefront-categories'},{auth:false});
      if(ticket!==revision||origin!==currentScope())return false;
      adopt(result);
      if(!adapter.call)window.LumoCategories?.apply?.(published);
      return true;
    } catch(error) { if(ticket===revision&&origin===currentScope())warning=error.message||'分类配置读取失败';return false; }
    finally {if(ticket===revision&&origin===currentScope())busy=false;}
  }
  function button(action,label,extra='',primary=false,disabled=false) {
    return `<button type="button" class="ce-button${primary?' ce-primary':''}" data-ce-action="${action}" ${extra}${disabled||busy?' disabled':''}>${label}</button>`;
  }
  function move(list,index,offset) {const next=index+offset;if(next<0||next>=list.length)return index;[list[index],list[next]]=[list[next],list[index]];return next;}
  function mark() {dirty=true;warning='';}
  function render() {
    synchronize();
    if(!authorized())return '<section class="ce-card"><h2>分类导航</h2><p>请使用管理员账号登录后配置分类导航。</p></section>';
    if(!draft)return `<section class="ce-card"><h2>分类导航</h2><p>${esc(warning || '正在加载后端分类配置…')}</p>${button('reload','重新加载')}</section>`;
    const groups=draft.groups, group=groups[selected], enabled=groups.filter(value=>value.enabled!==false).length;
    const rows=groups.reduce((sum,value)=>sum+(value.rows?.length||0),0), leaves=groups.reduce((sum,value)=>sum+(value.rows||[]).reduce((n,row)=>n+(row.items?.length||0),0),0);
    return `<section class="ce-editor" aria-label="分类导航配置">
      <header class="ce-heading"><div><span class="ce-eyebrow">CATEGORY NAVIGATION</span><h1>分类导航</h1><p>设置首页分类、精选词与相关品牌，保存后在商城展示。</p></div><div class="ce-heading-actions">${button('revert','撤销修改','',false,!dirty)}${button('reload','重新加载','',false,dirty)}${button('publish',busy?'正在保存…':'保存并发布','',true,!dirty)}</div></header>
      <div class="ce-summary"><span><strong>${enabled}</strong> 个展示大类</span><span>${rows} 个分组 · ${leaves} 个细分类</span><span class="${dirty?'ce-unsaved':'ce-saved'}" data-ce-save-status>${dirty?'有未保存的修改':'已与后端配置同步'}</span></div>
      ${warning?`<p class="ce-error" role="alert">${esc(warning)}</p>`:''}
      <div class="ce-layout"><aside class="ce-card ce-groups"><div class="ce-card-heading"><h2>全部大类</h2>${button('add-group','＋ 添加','',false,groups.length>=40)}</div><div class="ce-group-list">${groups.map((value,index)=>`<button type="button" class="ce-group${selected===index?' is-active':''}" data-ce-action="select-group" data-ce-index="${index}"><span><strong>${esc(value.title)}</strong><small>${esc(value.label)}</small></span><i>${value.enabled===false?'已隐藏':String(index+1).padStart(2,'0')}</i></button>`).join('')||'<p class="ce-empty">还没有分类，点击“添加”开始配置。</p>'}</div></aside>
      <div class="ce-details">${group?groupView(group,selected):'<section class="ce-card ce-empty">添加一个大类，再配置分组和细分类。</section>'}</div></div>
    </section>`;
  }
  function field(label,name,value,limit=80,help='') {
    return `<label class="ce-field">${label}<input data-ce-field="${name}" value="${esc(value||'')}" maxlength="${limit}"${busy?' disabled':''}>${help?`<small>${help}</small>`:''}</label>`;
  }
  function groupView(group,index) {
    return `<section class="ce-card"><div class="ce-card-heading"><h2>大类信息</h2><div class="ce-actions">${button('group-up','↑ 上移',`data-ce-index="${index}"`,false,index===0)}${button('group-down','↓ 下移',`data-ce-index="${index}"`,false,index===draft.groups.length-1)}${button('delete-group','删除大类',`data-ce-index="${index}"`)}</div></div>
      <div class="ce-fields">${field('商品分类名称','category',group.category,80,'与商品的分类字段保持一致；更名不会移动已有商品。')}${field('左侧展示文字','label',group.label)}${field('面板标题','title',group.title)}<label class="ce-field">分类图标<select data-ce-field="icon"${busy?' disabled':''}>${icons.map(([value,label])=>`<option value="${value}"${group.icon===value?' selected':''}>${label}</option>`).join('')}</select></label></div><label class="ce-toggle"><input type="checkbox" data-ce-field="enabled"${group.enabled!==false?' checked':''}${busy?' disabled':''}>在首页展示此大类</label></section>
      <section class="ce-card"><div class="ce-card-heading"><h2>快捷入口 <small>${group.shortcuts?.length||0}</small></h2>${button('edit-shortcuts','配置入口')}</div><div class="ce-chips">${(group.shortcuts||[]).map(value=>`<span>${esc(value.label)}</span>`).join('')||'<p class="ce-empty">尚未配置快捷入口</p>'}</div></section>
      <section class="ce-card"><div class="ce-card-heading"><h2>分组与细分类 <small>${group.rows?.length||0}</small></h2>${button('add-row','＋ 添加分组','',false,(group.rows?.length||0)>=20)}</div><div class="ce-rows">${(group.rows||[]).map((row,i)=>`<article class="ce-row"><div class="ce-row-heading"><strong>${esc(row.title)}</strong><div class="ce-actions">${button('row-up','↑',`data-ce-index="${i}" aria-label="上移${esc(row.title)}"`,false,i===0)}${button('row-down','↓',`data-ce-index="${i}" aria-label="下移${esc(row.title)}"`,false,i===group.rows.length-1)}${button('edit-row','编辑',`data-ce-index="${i}"`)}${button('delete-row','删除',`data-ce-index="${i}"`)}</div></div><div class="ce-chips">${(row.items||[]).slice(0,12).map(value=>`<span${value.featured?' class="is-featured"':''}>${esc(value.label)}</span>`).join('')}${row.items?.length>12?`<small>共 ${row.items.length} 项</small>`:''}</div></article>`).join('')||'<p class="ce-empty">添加分组，配置对应的细分类和搜索词。</p>'}</div></section>
      <section class="ce-card"><div class="ce-card-heading"><h2>相关品牌 <small>${group.brands?.length||0}</small></h2>${button('edit-brands','配置品牌')}</div><div class="ce-chips">${(group.brands||[]).map(value=>`<span>${esc(value.label)}</span>`).join('')||'<p class="ce-empty">未配置品牌时，首页隐藏品牌区域。</p>'}</div></section>`;
  }
  function openDialog() {
    if(!dialog){dialog=document.createElement('div');dialog.id='category-editor-dialog';dialog.className='ce-backdrop';document.body.appendChild(dialog);previousOverflow=document.body.style.overflow;document.body.style.overflow='hidden';}
    const isRow=editing.kind==='row', list=isRow?editing.value.items:editing.value, fieldName=editing.kind==='brands'?'brand':'query';
    dialog.innerHTML=`<section class="ce-dialog" role="dialog" aria-modal="true" aria-labelledby="ce-dialog-title"><header><h2 id="ce-dialog-title">${isRow?'编辑分组':editing.kind==='brands'?'配置相关品牌':'配置快捷入口'}</h2>${button('close-dialog','×','aria-label="关闭配置窗口"')}</header><form id="category-detail-form"><div class="ce-dialog-content">${isRow?`<div class="ce-fields"><label class="ce-field">分组名称<input data-ce-row-field="title" value="${esc(editing.value.title)}" maxlength="80" required></label><label class="ce-field">分组搜索词<input data-ce-row-field="query" value="${esc(editing.value.query)}" maxlength="120" placeholder="留空时使用分组名称"></label></div>`:''}<p class="ce-help">${isRow?'配置细分类名称、搜索词和推荐标记。搜索词留空时使用名称。':editing.kind==='brands'?'填写展示名称和商品实际品牌值；点击品牌后按此值筛选。':'搜索词留空时，显示该大类的全部商品。'}</p><div class="ce-link-head"><span>展示名称</span><span>${editing.kind==='brands'?'商品品牌值':'搜索词'}</span><span>${isRow?'推荐 / 排序':'排序'}</span></div><div class="ce-link-list">${list.map((item,i)=>`<div class="ce-link-row"><input aria-label="第${i+1}项名称" data-ce-link-field="label" data-ce-index="${i}" value="${esc(item.label)}" maxlength="80" required><input aria-label="第${i+1}项${fieldName==='brand'?'品牌值':'搜索词'}" data-ce-link-field="${fieldName}" data-ce-index="${i}" value="${esc(item[fieldName]||'')}" maxlength="${fieldName==='brand'?80:120}"${fieldName==='brand'?' required':''}><div class="ce-actions">${isRow?`<label class="ce-feature"><input type="checkbox" aria-label="推荐第${i+1}项" data-ce-link-field="featured" data-ce-index="${i}"${item.featured?' checked':''}>推荐</label>`:''}${button('link-up','↑',`data-ce-index="${i}" aria-label="上移第${i+1}项"`,false,i===0)}${button('link-down','↓',`data-ce-index="${i}" aria-label="下移第${i+1}项"`,false,i===list.length-1)}${button('delete-link','×',`data-ce-index="${i}" aria-label="删除第${i+1}项"`)}</div></div>`).join('')||'<p class="ce-empty">暂未添加内容</p>'}</div>${button('add-link','＋ 添加一项','',false,list.length>=(isRow?40:editing.kind==='brands'?16:8))}<p class="ce-dialog-error" role="alert"></p></div><footer><span>修改将在“保存并发布”后生效</span>${button('close-dialog','取消')}<button class="ce-button ce-primary" type="submit">保存配置</button></footer></form></section>`;
    dialog.querySelector('input,button')?.focus();
  }
  function closeDialog() {if(dialog){dialog.remove();dialog=null;document.body.style.overflow=previousOverflow;}editing=null;}
  function edit(kind,index=-1) {
    const group=draft.groups[selected];
    editing={kind,index,group:selected,value:kind==='row'?clone(index>=0?group.rows[index]:{title:'',query:'',items:[]}):clone(group[kind]||[])};
    openDialog();
  }
  function editingList() {return editing?.kind==='row'?editing.value.items:editing?.value;}
  async function publish() {
    checkScope();if(!authorized()||!draft||busy)return;
    const ticket=++revision, origin=scope, configuration=clone(draft);busy=true;warning='';draw();
    try {
      const result=await call('content','upsert',{id:'storefront-categories',data:{kind:'storefront',title:'商城分类导航',body:JSON.stringify(configuration)}},{auth:true});
      if(ticket!==revision||origin!==currentScope()||!authorized())return;
      adopt(result);window.LumoCategories?.apply?.(published);notify('分类导航已保存并发布');
    } catch(cause) {if(ticket===revision&&origin===currentScope())warning=cause.message||'分类保存失败';}
    finally {if(ticket===revision&&origin===currentScope()){busy=false;draw();}}
  }
  document.addEventListener('input', event=>{
    checkScope();if(!authorized()||busy)return;const target=event.target;
    if(target.dataset?.ceField&&draft?.groups[selected]){const key=target.dataset.ceField;if(!['category','label','title','icon','enabled'].includes(key))return;draft.groups[selected][key]=key==='enabled'?target.checked:target.value;mark();const status=document.querySelector('[data-ce-save-status]');if(status){status.textContent='有未保存的修改';status.className='ce-unsaved';}document.querySelectorAll('[data-ce-action="publish"], [data-ce-action="revert"]').forEach(button=>button.disabled=false);document.querySelectorAll('[data-ce-action="reload"]').forEach(button=>button.disabled=true);}
    if(editing&&target.dataset?.ceRowField&&['title','query'].includes(target.dataset.ceRowField))editing.value[target.dataset.ceRowField]=target.value;
    if(editing&&target.dataset?.ceLinkField){const key=target.dataset.ceLinkField,index=Number(target.dataset.ceIndex);if(['label','query','brand','featured'].includes(key)&&editingList()?.[index])editingList()[index][key]=key==='featured'?target.checked:target.value;}
  });
  document.addEventListener('change', event=>{if(event.target.matches?.('[data-ce-field="enabled"], [data-ce-field="icon"], [data-ce-link-field="featured"]'))event.target.dispatchEvent(new Event('input',{bubbles:true}));});
  document.addEventListener('click',async event=>{
    const target=event.target.closest?.('[data-ce-action]');if(!target)return;event.preventDefault();checkScope();if(!authorized()||busy)return;
    const action=target.dataset.ceAction,index=Number(target.dataset.ceIndex),group=draft?.groups[selected];
    if(action==='close-dialog'){closeDialog();return;}
    if(action==='reload'){if(dirty)return;await refresh();draw();return;}
    if(action==='publish'){await publish();return;}
    if(action==='revert'){if(published)adopt(published);draw();return;}
    if(!draft)return;
    if(action==='select-group'){selected=Math.max(0,Math.min(index,draft.groups.length-1));draw();return;}
    if(action==='add-group'){
      if(draft.groups.length>=40)return;
      draft.groups.push({id:'category-'+(window.crypto?.randomUUID?.()||Date.now().toString(36)).toLowerCase(),category:'新分类',label:'新分类',title:'新分类',icon:'layers',enabled:true,shortcuts:[],rows:[],brands:[]});selected=draft.groups.length-1;mark();draw();return;
    }
    if(!group)return;
    if(action==='group-up'||action==='group-down'){selected=move(draft.groups,index,action==='group-up'?-1:1);mark();draw();return;}
    if(action==='delete-group'){draft.groups.splice(index,1);selected=Math.min(selected,Math.max(0,draft.groups.length-1));mark();draw();return;}
    if(action==='add-row'){if(group.rows.length<20)edit('row');return;}
    if(action==='edit-row'){edit('row',index);return;}
    if(action==='edit-shortcuts'){edit('shortcuts');return;}
    if(action==='edit-brands'){edit('brands');return;}
    if(action==='delete-row'){group.rows.splice(index,1);mark();draw();return;}
    if(action==='row-up'||action==='row-down'){move(group.rows,index,action==='row-up'?-1:1);mark();draw();return;}
    if(editing){
      const list=editingList();
      if(action==='add-link'){const max=editing.kind==='row'?40:editing.kind==='brands'?16:8;if(list.length<max)list.push(editing.kind==='brands'?{label:'',brand:''}:{label:'',query:'',...(editing.kind==='row'?{featured:false}:{})});}
      if(action==='delete-link')list.splice(index,1);
      if(action==='link-up'||action==='link-down')move(list,index,action==='link-up'?-1:1);
      openDialog();
    }
  });
  document.addEventListener('submit', event=>{
    if(event.target.id!=='category-detail-form')return;event.preventDefault();checkScope();if(!editing||!authorized()||busy)return;
    try {
      const group=draft.groups[editing.group], list=editingList();
      if(!group)throw Error('当前大类已改变，请重新编辑');
      for(let i=0;i<list.length;i++){list[i].label=String(list[i].label||'').trim();if(!list[i].label)throw Error(`请填写第${i+1}项的展示名称`);if(editing.kind==='brands'){list[i].brand=String(list[i].brand||'').trim();if(!list[i].brand)throw Error(`请填写第${i+1}项的商品品牌值`);}else{list[i].query=String(list[i].query||'').trim();if(editing.kind==='row'&&!list[i].query)list[i].query=list[i].label;}}
      if(editing.kind==='row'){editing.value.title=editing.value.title.trim();if(!editing.value.title)throw Error('请填写分组名称');editing.value.query=editing.value.query.trim()||editing.value.title;if(editing.index>=0)group.rows[editing.index]=editing.value;else group.rows.push(editing.value);}else group[editing.kind]=list;
      mark();closeDialog();draw();
    }catch(cause){const target=dialog?.querySelector('.ce-dialog-error');if(target)target.textContent=cause.message;}
  });
  document.addEventListener('keydown',event=>{if(event.key==='Escape'&&dialog){event.preventDefault();closeDialog();}});
  for(const name of ['lumo:logout','lumo:session-expired','lumo:login'])window.addEventListener(name,reset);
  window.LumoCategoryEditor={render,refresh,configure(options){adapter=options||{};reset();},reset,get dirty(){return dirty;},get configuration(){synchronize();return draft?clone(draft):null;}};
})();
