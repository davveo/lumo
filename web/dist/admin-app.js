/* Administrative actions always use a separately authenticated server session. */
(function () {
  'use strict';
  const C = window.LumoConstants;
  const API = window.LumoAPI, $ = s => document.querySelector(s);
  API.enableReadBatching?.();
  const esc = value => String(value ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
  const tokenKey = 'lumo-admin-session-v1';
  let token = sessionStorage.getItem(tokenKey) || '', tab = 'overview', rows = [], lastTrace = '', busy = false, epoch = 0;
  const snapshots=new Map();
  const scope=()=>API.getBase()+'|'+token;
  const placeholder=(type,count,label)=>window.LumoSkeleton?.render(type,{count,label})||'';
  function loadingPage(name){return title(name)+(tab==='overview'?placeholder('summary',3,'正在读取工作台统计')+placeholder('rows',3,'正在读取业务概览'):['categories','popups'].includes(tab)?placeholder('form',6,'正在读取管理配置'):placeholder('table',6,'正在读取'+name));}

  const menus = [['overview','工作台'],['payments','支付确认'],['fulfillment','仓库发货'],['aftersales','售后处理'],['refunds','退款确认'],['invoices','发票管理'],['interactions','评价与问答'],['tickets','客服工单'],['reconciliation','对账记录'],['inventory','库存管理'],['categories','分类导航'],['popups','营销弹窗'],['content','内容管理'],['analytics','行为记录']];
  const specs = { payments:[C.Service.Payment,C.Action.List],fulfillment:[C.Service.Fulfillment,C.Action.List],aftersales:[C.Service.Aftersale,C.Action.List],refunds:[C.Service.Refund,C.Action.List],invoices:[C.Service.Invoice,C.Action.List],interactions:[C.Service.Interaction,C.Action.List],tickets:[C.Service.Helpdesk,C.Action.List],reconciliation:[C.Service.Reconciliation,C.Action.List],analytics:[C.Service.Analytics,C.Action.List] };
  const statusNames = { [C.PaymentStatus.Pending]:'待确认', [C.PaymentStatus.Succeeded]:'已成功', [C.FulfillmentStatus.Ready]:'待发货', [C.FulfillmentStatus.Dispatched]:'已发货', [C.AftersaleStatus.Requested]:'待处理', [C.AftersaleStatus.Approved]:'已批准', [C.AftersaleStatus.Returning]:'退货中', [C.FulfillmentStatus.Inspected]:'已验收', [C.AftersaleStatus.Refunding]:'退款中',[C.OrderStatus.Refunded]:'已退款',[C.AftersaleStatus.Exchanged]:'已换货', [C.InvoiceStatus.Issued]:'已开票', [C.HelpdeskStatus.Open]:'待回复', [C.HelpdeskStatus.Replied]:'已回复', [C.PaymentStatus.Closed]:'已关闭', [C.ReconciliationStatus.Matched]:'一致', [C.ReconciliationStatus.Differences]:'有差异', [C.InteractionStatus.Published]:'已公开', [C.AftersaleStatus.Rejected]:'已驳回', [C.FulfillmentStatus.Cancelled]:'已取消'};
  const currency = n => '¥' + (Number(n || 0) / 100).toLocaleString('zh-CN',{minimumFractionDigits:2});
  const idKey = () => 'admin-' + (crypto.randomUUID ? crypto.randomUUID() : Date.now() + '-' + Math.random().toString(16).slice(2));
  const message = text => { const el=$('#admin-toast');el.textContent=text;el.style.display='block';setTimeout(()=>{el.style.display='none';},3500); };
  async function call(service,action,command={}) { const session=token;try{const result=await API.call(service,action,command,{token:session,auth:true});if(session===token)lastTrace=API.requests()[0]?.traceId||'';return result;}catch(error){if(error.status===401&&session===token){token='';sessionStorage.removeItem(tokenKey);epoch++;login();}throw error;} }
  window.LumoCategoryEditor?.configure({isAdmin:()=>!!token,scope:()=>API.getBase()+'|'+token,call,draw:()=>{if(tab==='categories'&&token)$('#admin-main').innerHTML=window.LumoCategoryEditor.render();},notify:message});
  window.LumoPopupAdmin?.configure({isAdmin:()=>!!token,scope:()=>API.getBase()+'|'+token,call,draw:()=>{if(tab==='popups'&&token)$('#admin-main').innerHTML=window.LumoPopupAdmin.render();},notify:message});
  function errorHTML(error){ return `<div class="admin-error">${esc(error.message || '请求失败')}${error.traceID || error.traceId ? `<div class="admin-trace">Trace ID: ${esc(error.traceID || error.traceId)}</div>`:''}</div>`; }
  function nav(){ $('#admin-nav').innerHTML=token?menus.map(([key,title])=>`<button class="admin-nav-btn ${tab===key?'active':''}" data-tab="${key}">${title}</button>`).join('')+'<a class="admin-nav-btn" href="index.html#/editor">商品与页面编辑 ↗</a><button class="admin-nav-btn" data-action="logout">退出管理账号</button>':''; }
  function login(){snapshots.clear();$('#admin-main').removeAttribute('aria-busy');window.LumoCategoryEditor?.reset();window.LumoPopupAdmin?.reset();nav();$('#admin-main').innerHTML=`<div class="admin-login"><h1>登录商城管理</h1><p class="admin-note">使用管理员账号处理支付、履约和售后。管理会话与商城顾客会话分别保存。</p><form id="admin-login"><label class="field">账号<input name="username" autocomplete="username" required></label><label class="field">密码<input type="password" name="password" autocomplete="current-password" required></label><button class="primary" type="submit">登录</button></form><div id="admin-error"></div></div>`; }
  function title(text,extra=''){ return `<div class="admin-title"><h1>${esc(text)}</h1>${extra}<button class="outline" data-action="reload">刷新</button></div>`; }
  const button=(action,id,label)=>`<button data-operation="${action}" data-id="${esc(id)}">${label}</button>`;
  function rowActions(row){const id=row.id;switch(tab){
    case 'payments':return row.status===C.PaymentStatus.Pending?button((C.Service.Payment+'.'+C.Action.ConfirmDemo),id,'确认演示收款'):'';
    case 'fulfillment':return (row.status===C.FulfillmentStatus.Ready?button((C.Service.Fulfillment+'.'+C.Action.Dispatch),id,'确认出库')+button((C.Service.Fulfillment+'.'+C.Action.Cancel),id,'拦截取消'):'')+(row.status===C.FulfillmentStatus.Dispatched?button((C.Service.Logistics+'.'+C.Action.Deliver),id,'确认演示签收'):'');
    case 'aftersales':return (row.status===C.AftersaleStatus.Requested?button((C.Service.Aftersale+'.'+C.Action.Approve),id,'批准')+button((C.Service.Aftersale+'.'+C.Action.Reject),id,'驳回'):'')+(row.status===C.AftersaleStatus.Returning?button((C.Service.Aftersale+'.'+C.Action.Inspect),id,'验收退货'):'')+((row.type===C.AftersaleType.Refund&&row.status===C.AftersaleStatus.Approved)||(row.type===C.AftersaleType.Return&&row.status===C.AftersaleStatus.Inspected)?button((C.Service.Aftersale+'.'+C.Action.Refund),id,'发起退款'):'')+(row.type===C.AftersaleType.Exchange&&row.status===C.AftersaleStatus.Inspected?button((C.Service.Aftersale+'.'+C.Action.Exchange),id,'创建换货履约'):'');
    case 'refunds':return [C.RefundStatus.Pending,C.RefundStatus.Processing].includes(row.status)?button((C.Service.Refund+'.'+C.Action.ConfirmDemo),id,'确认演示退款'):'';
    case 'invoices':return (row.status===C.InvoiceStatus.Requested?button((C.Service.Invoice+'.'+C.Action.IssueDemo),id,'演示开票'):'')+button((C.Service.Invoice+'.'+C.Action.AdjustDemo),id,'处理红冲')+button((C.Service.Invoice+'.'+C.Action.Reversals),id,'查看红冲记录');
    case 'interactions':return button((C.Service.Interaction+'.'+C.Action.Moderate),id,'审核')+(row.kind===C.InteractionKind.Question?button((C.Service.Interaction+'.'+C.Action.Answer),id,'回复问题'):'');
    case 'tickets':return button((C.Service.Helpdesk+'.'+C.Action.Get),id,'查看会话')+(row.status!==C.HelpdeskStatus.Closed?button((C.Service.Helpdesk+'.'+C.Action.Reply),id,'客服回复')+button((C.Service.Helpdesk+'.'+C.Action.Close),id,'关闭工单'):'');
    default:return '';
  }}
  function table(){ if(!rows.length)return '<div class="admin-empty">暂无记录</div>';return `<div class="admin-table-wrap"><table class="admin-table"><thead><tr><th>业务对象</th><th>内容 / 金额</th><th>状态</th><th>操作</th></tr></thead><tbody>${rows.map(row=>`<tr><td><strong>${esc(row.title || row.name || row.kind || row.type || '业务记录')}</strong><small>${esc(row.id)}</small><small>${row.order_id?'订单 '+esc(row.order_id):esc(row.user_id || '')}</small></td><td>${esc(row.body || row.reason || row.tracking_id || row.number || '')}${row.amount_cents!=null?currency(row.amount_cents):''}${row.details?`<small>${esc(JSON.stringify(row.details))}</small>`:''}${row.answer?`<small>${esc(row.answer)}</small>`:''}</td><td><span class="admin-state">${esc(statusNames[row.status]||row.status)}</span></td><td><div class="admin-actions">${rowActions(row)}</div></td></tr>`).join('')}</tbody></table></div>`; }
  function field(label,name,type='text',value='',extra=''){return `<label class="field ${extra}">${label}<input name="${name}" type="${type}" value="${esc(value)}" required></label>`;}
  function forms(){if(tab==='inventory')return `<section class="admin-section"><h2>查询与调整库存</h2><p class="admin-note">设置在库总量；库存服务校验已预占和已分配数量，禁止设置为更低的库存。</p><form id="inventory-query" class="admin-form">${field('SKU 编号','sku_id')}<div><button class="outline">查询库存</button></div></form><form id="inventory-adjust" class="admin-form" style="margin-top:24px">${field('SKU 编号','sku_id')}${field('在库总量（非负整数）','on_hand','number')}${field('调整原因','reason')}<div><button class="primary">提交调整</button></div></form><h2 style="margin-top:32px">仓库退货入库</h2><p class="admin-note">售后退货请优先在售后处理页验收。此处登记独立的仓库收货凭证，同一凭证重复提交不会重复入库。</p><form id="return-inspect" class="admin-form">${field('唯一验收凭证号','inspection_id')}${field('SKU 编号','sku_id')}${field('实收数量','quantity','number')}<label class="field">品质<select name="condition"><option value="sellable">可售</option><option value="damaged">破损</option><option value="unsellable">不可售</option></select></label><button class="primary">登记入库</button></form><div id="admin-result"></div></section>`;
    return `<section class="admin-section"><h2>商城内容</h2><p class="admin-note">发布帮助、公告及页面配置。商品与视觉布局可进入“商品与页面编辑”。</p><form id="content-save" class="admin-form">${field('内容编号','id')}${field('类型','kind','text',C.ContentKind.Notice)}${field('标题','title','text','','wide')}<label class="field wide">正文<textarea name="body" required></textarea></label><button class="primary">发布内容</button></form><div id="admin-result"></div></section>`;
  }
  async function load(){
    const revision=++epoch;nav();if(!token){login();return;}
    const name=menus.find(x=>x[0]===tab)?.[1]||'',origin=scope(),key=origin+'|'+tab,main=$('#admin-main'),cached=snapshots.get(key);
    main.innerHTML=cached||loadingPage(name);main.setAttribute('aria-busy','true');
    const current=()=>revision===epoch&&origin===scope()&&!!token;
    const show=html=>{if(!current())return;main.innerHTML=html;snapshots.set(key,html);};
    try{
      if(tab==='categories'||tab==='popups'){
        const editor=tab==='categories'?window.LumoCategoryEditor:window.LumoPopupAdmin,pending=editor.refresh();
        if(current())main.innerHTML=editor.render();await pending;show(editor.render());return;
      }
      if(tab==='overview'){
        const all=await Promise.all([call(C.Service.Order,C.Action.List),call(C.Service.Aftersale,C.Action.List),call(C.Service.Helpdesk,C.Action.List)]);if(!current())return;
        show(title(name)+`<div class="admin-cards">${[['订单',all[0].items?.length||0],['待处理售后',(all[1].items||[]).filter(x=>x.status===C.AftersaleStatus.Requested).length],['未关闭工单',(all[2].items||[]).filter(x=>x.status!==C.HelpdeskStatus.Closed).length]].map(([name,n])=>`<div class="admin-card"><span>${name}</span><strong>${n}</strong><span>服务端实时记录</span></div>`).join('')}</div><section class="admin-section"><h2>交易处理</h2><p class="admin-note">顾客提交订单并创建支付单后，在“支付确认”中确认演示收款。订单资源确认完成后会进入仓库待发货；确认出库和演示签收后，顾客可确认收货、评价或申请售后。</p><p class="admin-note">支付、退款、物流和发票使用演示适配器；每一步均调用后端并保留状态记录。</p><a class="outline" href="index.html">打开商城</a></section>`);return;
      }
      if(tab==='inventory'||tab==='content'){show(title(name)+forms());return;}
      let fetched=[];
      if(tab==='interactions'){const [reviews,questions]=await Promise.all([call(C.Service.Interaction,C.Action.List,{data:{kind:C.InteractionKind.Review,include_pending:true}}),call(C.Service.Interaction,C.Action.List,{data:{kind:C.InteractionKind.Question,include_pending:true}})]);fetched=[...(reviews.items||[]),...(questions.items||[])];}
      else{const [service,action]=specs[tab];const result=await call(service,action);fetched=result.items||[];}
      if(!current())return;rows=fetched;
      show(title(name,tab==='reconciliation'?'<button class="primary" data-operation="reconciliation.run">执行内部对账</button>':'')+(tab==='payments'||tab==='refunds'?'<p class="admin-note">仅确认演示渠道回执。确认后请刷新查看下游订单或售后状态。</p>':'')+table()+`<div class="admin-trace">最近调用链路：${esc(lastTrace)}</div><section id="operation-result"></section>`);
    }catch(error){if(!current())return;main.innerHTML=cached?cached+errorHTML(error):title(name)+errorHTML(error);}
    finally{if(current())main.removeAttribute('aria-busy');}
  }
  function showResult(result){const target=$('#operation-result')||$('#admin-result');if(target)target.innerHTML=`<section class="admin-section"><h2>处理结果</h2><pre>${esc(JSON.stringify(result,null,2))}</pre><div class="admin-trace">链路编号：${esc(lastTrace)}</div></section>`;}
  document.addEventListener('click',async event=>{const target=event.target.closest('[data-tab],[data-action],[data-operation]');if(!target||busy)return;if(target.dataset.tab){tab=target.dataset.tab;load();return;}if(target.dataset.action==='reload'){load();return;}if(target.dataset.action===C.Action.Logout){try{await call(C.Service.IAM,C.Action.Logout);token='';sessionStorage.removeItem(tokenKey);login();}catch(error){message(error.message);}return;}
    if(!target.dataset.operation)return;const [service,action]=target.dataset.operation.split('.');const command={id:target.dataset.id||'',data:{},idempotency_key:idKey()};
    if(action===C.Action.Inspect){const condition=prompt('退货品质：sellable / damaged / unsellable',C.StockCondition.Sellable);if(condition===null)return;if(![C.StockCondition.Sellable,C.StockCondition.Damaged,C.StockCondition.Unsellable].includes(condition)){message('请选择有效品质');return;}command.data.condition=condition;}
    if(service===C.Service.Interaction&&action===C.Action.Moderate){const approved=confirm('确定公开此内容？点击取消可选择驳回。');if(!approved&&!confirm('是否驳回此内容？'))return;command.data.approved=approved;}
    if(action===C.Action.Answer||action===C.Action.Reply){const body=prompt('输入回复内容');if(!body?.trim())return;command.data[action===C.Action.Answer?'answer':'body']=body.trim();}
    busy=true;target.disabled=true;if(action===C.Action.Get||action===C.Action.Reversals){const resultPanel=$('#operation-result')||$('#admin-result');if(resultPanel)resultPanel.innerHTML=placeholder('rows',3,'正在读取业务详情');}try{const result=await call(service,action,command);if(action===C.Action.Get||action===C.Action.Reversals)showResult(result);else{message('后端已受理');await load();showResult(result);}}catch(error){const el=$('#operation-result');if(el)el.innerHTML=errorHTML(error);else message(error.message);}finally{busy=false;target.disabled=false;}
  });
  document.addEventListener('submit',async event=>{const form=event.target;if(!['admin-login','inventory-query','inventory-adjust','return-inspect','content-save'].includes(form.id))return;event.preventDefault();if(busy)return;busy=true;const button=form.querySelector('button');button.disabled=true;const data=Object.fromEntries(new FormData(form));try{
      if(form.id==='admin-login'){const session=await API.call(C.Service.IAM,C.Action.Login,{data},{auth:false,token:''});if(session.role!==C.Role.Admin){await API.call(C.Service.IAM,C.Action.Logout,{}, {token:session.token});throw Error('此账号没有管理员权限');}token=session.token;sessionStorage.setItem(tokenKey,token);form.reset();await load();}
      if(form.id==='inventory-query'){$('#admin-result').innerHTML=placeholder('rows',3,'正在读取库存快照');showResult(await call(C.Service.Inventory,C.Action.Get,{id:data.sku_id}));}
      if(form.id==='inventory-adjust')showResult(await call(C.Service.Inventory,C.Action.Adjust,{id:data.sku_id,idempotency_key:idKey(),data:{on_hand:Number(data.on_hand),reason:data.reason}}));
      if(form.id==='return-inspect')showResult(await call(C.Service.Fulfillment,C.Action.InspectReturn,{id:data.inspection_id,data:{sku_id:data.sku_id,quantity:Number(data.quantity),condition:data.condition}}));
      if(form.id==='content-save'){const id=data.id;delete data.id;data.active=true;showResult(await call(C.Service.Content,C.Action.Upsert,{id,data}));}
    }catch(error){const el=$('#admin-error')||$('#admin-result');if(el)el.innerHTML=errorHTML(error);else message(error.message);}finally{busy=false;button.disabled=false;}
  });
  window.LumoAdminReady=(async()=>{if(token){$('#admin-main').innerHTML=placeholder('profile',1,'正在验证管理员会话');try{const principal=await call(C.Service.IAM,C.Action.Current);if(principal.role!==C.Role.Admin)throw Error('管理员身份无效');}catch(_){token='';sessionStorage.removeItem(tokenKey);}}await load();})();
})();
