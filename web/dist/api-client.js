(function () {
  'use strict';
  const C = window.LumoConstants;

  let bearer = '', base = '', batching = false, readEpoch = 0, flushTimer = null, sequence = 0;
  let bootstrapActive = false, bootstrapRevision = 0;
  const requests = [], pending = new Map(), queued = [], snapshots = new Map();
  const emit = (name, detail) => window.dispatchEvent(new CustomEvent(name, {detail}));
  const own = (object, key) => Object.prototype.hasOwnProperty.call(object, key);
  const clone = value => JSON.parse(JSON.stringify(value));
  const stable = value => JSON.stringify(value, (_, row) => row && typeof row === 'object' && !Array.isArray(row) ? Object.keys(row).sort().reduce((result, key) => { if (row[key] !== undefined) result[key] = row[key]; return result; }, Object.create(null)) : row);
  const cleanBase = value => {
    if (!value) return '';
    const url = new URL(String(value), location.origin);
    if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password || url.search || url.hash) throw new Error('后端地址格式不正确');
    return url.href.replace(/\/$/, '').replace(/\/api\/v1$/, '');
  };
  try { base = cleanBase(window.LUMO_API_BASE || ''); } catch (error) { console.error(error.message); }

  class APIError extends Error {
    constructor(message, details = {}) { super(message + (details.traceId ? '（链路 ' + details.traceId + '）' : '')); this.name = 'APIError'; Object.assign(this, details); }
  }
  const translated = (code, message, httpStatus) => {
    if (/invalid credentials/i.test(message || '')) return '账号或密码不正确';
    if (httpStatus === 401) return '登录状态已失效，请重新登录';
    if (httpStatus === 403 && /administrator/i.test(message || '')) return '此操作需要管理员账号';
    if (httpStatus === 403) return '当前账号没有此操作权限：' + (message || code);
    if (httpStatus >= 500) return '后端服务暂时不可用：' + (message || code);
    return message || code || '请求失败';
  };
  const readActions = {
    [C.Service.Storefront]: [C.Action.Recent], [C.Service.Catalog]: [C.Action.List, C.Action.Get, C.Action.BatchGet], [C.Service.Inventory]: [C.Action.Get, C.Action.BatchGet], [C.Service.Search]: [C.Action.Query], [C.Service.Recommendation]: [C.Action.List], [C.Service.Content]: [C.Action.List, C.Action.Get],
    [C.Service.Campaign]: [C.Action.List, C.Action.Evaluate], [C.Service.Group]: [C.Action.List, C.Action.Get], [C.Service.Flash]: [C.Action.List], [C.Service.Interaction]: [C.Action.List], [C.Service.Logistics]: [C.Action.Get], [C.Service.Helpdesk]: [C.Action.Faq, C.Action.List, C.Action.Get],
    [C.Service.Member]: [C.Action.Get], [C.Service.Address]: [C.Action.List, C.Action.Get], [C.Service.Cart]: [C.Action.List], [C.Service.Engagement]: [C.Action.List], [C.Service.Coupon]: [C.Action.List, C.Action.Templates, C.Action.TemplatesSnapshot], [C.Service.Points]: [C.Action.Get],
    [C.Service.Order]: [C.Action.List], [C.Service.Aftersale]: [C.Action.List, C.Action.Get], [C.Service.Fulfillment]: [C.Action.List, C.Action.Get], [C.Service.Invoice]: [C.Action.List, C.Action.Get, C.Action.Reversals], [C.Service.Notification]: [C.Action.List],
    [C.Service.Payment]: [C.Action.List], [C.Service.Refund]: [C.Action.List], [C.Service.Reconciliation]: [C.Action.List], [C.Service.Analytics]: [C.Action.List]
  };
  const isRead = (service, action) => (readActions[service] || []).includes(action);
  // These operations must observe each call's current state and stay outside the
  // read batch. Quotes may reserve/write state even though their names look like reads.
  const directReads = new Set([(C.Service.IAM+'/'+C.Action.Current), (C.Service.IAM+'/'+C.Action.Validate), (C.Service.Pricing+'/'+C.Action.Get), (C.Service.Payment+'/'+C.Action.Get), (C.Service.Order+'/'+C.Action.Get), (C.Service.Flash+'/'+C.Action.Get), (C.Service.Refund+'/'+C.Action.Get), (C.Service.Logistics+'/'+C.Action.Quote), (C.Service.Media+'/'+C.Action.Get), (C.Service.Media+'/'+C.Action.Download)]);
  const contextFor = options => {
    const explicit = own(options, 'token');
    const token = String(explicit ? options.token || '' : options.auth === false ? '' : bearer);
    return {base, token, mode: token ? explicit ? 'explicit' : 'session' : 'public'};
  };
  const current = context => context.base === base && (context.mode !== 'session' || context.token === bearer);
  const requestKey = (service, action, command, context) => stable([context.base, context.mode, context.token, service, action, command]);
  const changed = (service, action) => new APIError('登录账号、接口或数据已变化，请重新加载当前页面', {status: 409, code: C.ApiErrorCode.SessionChanged, service, action});
  function invalidate() { readEpoch++; snapshots.clear(); pending.clear(); }
  function expire(context, traceId) {
    if (context.mode === 'session' && context.token && context.token === bearer) { bearer = ''; invalidate(); emit('lumo:session-expired', {traceId}); }
  }
  function commandFor(command) {
    const payload = {data: command.data || {}};
    if (command.id !== undefined) payload.id = command.id;
    if (command.idempotency_key) payload.idempotency_key = command.idempotency_key;
    return clone(payload);
  }
  const newTraceParent = () => {
    if (!window.crypto?.getRandomValues) return '';
    const bytes = new Uint8Array(24); window.crypto.getRandomValues(bytes);
    const hex = [...bytes].map(value => value.toString(16).padStart(2, '0')).join('');
    return '00-' + hex.slice(0, 32) + '-' + hex.slice(32) + '-01';
  };
  function record(entry) {
    requests.unshift(entry); if (requests.length > 200) requests.length = 200;
    emit('lumo:api-request', {...entry}); return entry;
  }
  async function transport(service, action, payload, options, context) {
    const controller = new AbortController(), started = Date.now();
    const abort = () => controller.abort();
    if (options.signal?.aborted) abort();
    else options.signal?.addEventListener('abort', abort, {once: true});
    const timeout = setTimeout(() => controller.abort(), options.timeout || 30000);
    const headers = {'Content-Type': 'application/json', Accept: 'application/json'};
    if (context.token) headers.Authorization = 'Bearer ' + context.token;
    if (payload.idempotency_key) headers['Idempotency-Key'] = payload.idempotency_key;
    const traceparent = newTraceParent(); if (traceparent) headers.traceparent = traceparent;
    let traceId = '', httpStatus = 0;
    try {
      const response = await fetch(context.base + '/api/v1/' + service + '/' + action, {method: 'POST', headers, body: JSON.stringify(payload), signal: controller.signal, credentials: 'same-origin', cache: 'no-store', redirect: 'error'});
      httpStatus = response.status; traceId = response.headers.get('X-Trace-ID') || '';
      let body;
      try { body = await response.json(); } catch (_) { throw new APIError('后端地址未提供有效接口，请检查网关及前端接口地址配置', {status: httpStatus, traceId, service, action}); }
      traceId = body.trace_id || traceId;
      if (!response.ok || body.error) {
        const error = body.error || {};
        if (httpStatus === 401) expire(context, traceId);
        throw new APIError(translated(error.code, error.message, httpStatus), {status: httpStatus, code: error.code, traceId, service, action});
      }
      if (!current(context)) throw changed(service, action);
      if (!body || typeof body.data !== 'object' || body.data === null) throw new APIError('后端响应缺少数据字段', {status: httpStatus, traceId, service, action});
      const entry = record({service, action, status: httpStatus, traceId, duration: Date.now() - started, at: started, ok: true, ...(action === C.Action.Batch ? {batchSize: payload.data.requests.length} : {})});
      return {data: body.data, traceId, status: httpStatus, entry};
    } catch (error) {
      const apiError = error instanceof APIError ? error : new APIError(error.name === 'AbortError' ? '请求超时，请查询操作结果后使用原请求编号重试' : '无法连接后端服务，请确认服务已启动且接口地址正确', {status: httpStatus, traceId, service, action, cause: error});
      record({service, action, status: apiError.status || 0, traceId: apiError.traceId || traceId, duration: Date.now() - started, at: started, ok: false});
      emit('lumo:api-error', {service, action, error: apiError}); throw apiError;
    } finally { clearTimeout(timeout); options.signal?.removeEventListener('abort', abort); }
  }
  function getSnapshot(service, action, command, context) {
    if (!bootstrapActive) return null;
    const key = requestKey(service, action, command, context);
    if (snapshots.has(key)) return clone(snapshots.get(key));
    if (action === C.Action.BatchGet && [C.Service.Inventory, C.Service.Catalog].includes(service) && Array.isArray(command.data.ids)) {
      const items = [];
      for (const id of [...new Set(command.data.ids)]) {
        const row = snapshots.get(requestKey(service, C.Action.Get, {id, data: {}}, context));
        if (!row) return null;
        items.push(clone(row));
      }
      return {items, missing_ids: []};
    }
    return null;
  }
  function putSnapshot(service, action, command, context, data) {
    if (!isRead(service, action) || snapshots.size >= 10000) return;
    snapshots.set(requestKey(service, action, command, context), clone(data));
    if (action === C.Action.BatchGet && [C.Service.Inventory, C.Service.Catalog].includes(service)) {
      for (const row of data.items || []) { const id = service === C.Service.Inventory ? row.sku_id : row.id; if (id) putSnapshot(service, C.Action.Get, {id, data: {}}, context, row); }
    }
  }
  function childError(row, response, context) {
    const details = row.error || {}, code = Number(details.status) || 500;
    if (code === 401) expire(context, response.traceId);
    const error = new APIError(translated(details.code, details.message, code), {status: code, code: details.code, traceId: response.traceId, service: row.service, action: row.action});
    emit('lumo:api-error', {service: row.service, action: row.action, error}); return error;
  }
  async function dispatch(entries) {
    const context = entries[0].context, epoch = entries[0].epoch;
    try {
      if (!current(context) || epoch !== readEpoch) throw changed(C.Service.Storefront, C.Action.Batch);
      const response = await transport(C.Service.Storefront, C.Action.Batch, {data: {requests: entries.map(entry => ({key: entry.key, service: entry.service, action: entry.action, command: entry.command}))}}, {timeout: entries[0].timeout}, context);
      if (epoch !== readEpoch) throw changed(C.Service.Storefront, C.Action.Batch);
      if (!Array.isArray(response.data.results)) throw new APIError('聚合读取响应缺少结果，请刷新重试', {status: 502, traceId: response.traceId});
      const results = new Map(response.data.results.map(row => [row.key, row]));
      const failedReads = [];
      for (const entry of entries) {
        const row = results.get(entry.key);
        if (!row || row.service !== entry.service || row.action !== entry.action) entry.reject(new APIError('聚合读取结果不完整，请刷新重试', {status: 502, traceId: response.traceId, service: entry.service, action: entry.action}));
        else if (row.error) { failedReads.push({service: entry.service, action: entry.action, code: row.error.code}); entry.reject(childError(row, response, context)); }
        else if (!row.data || typeof row.data !== 'object') entry.reject(new APIError('后端响应缺少数据字段', {status: 502, traceId: response.traceId, service: entry.service, action: entry.action}));
        else entry.resolve(clone(row.data));
      }
      if (failedReads.length) { response.entry.ok = false; response.entry.failedReads = failedReads; }
    } catch (error) { for (const entry of entries) entry.reject(error); }
  }
  function flush() {
    flushTimer = null;
    const entries = queued.splice(0), groups = new Map();
    for (const entry of entries) { const key = stable([entry.context, entry.timeout, entry.epoch]); if (!groups.has(key)) groups.set(key, []); groups.get(key).push(entry); }
    for (const group of groups.values()) for (let index = 0; index < group.length; index += 64) void dispatch(group.slice(index, index + 64));
  }
  function read(service, action, command, options, context) {
    const snapshot = getSnapshot(service, action, command, context); if (snapshot) return Promise.resolve(snapshot);
    const timeout = Math.max(1, Number(options.timeout) || 30000), dedupKey = requestKey(service, action, command, context) + ':' + timeout + ':' + readEpoch;
    if (pending.has(dedupKey)) return pending.get(dedupKey);
    const entry = {key: 'read-' + (++sequence), service, action, command, context, timeout, epoch: readEpoch};
    const promise = new Promise((resolve, reject) => { entry.resolve = resolve; entry.reject = reject; });
    const tracked = promise.finally(() => { if (pending.get(dedupKey) === tracked) pending.delete(dedupKey); });
    pending.set(dedupKey, tracked); queued.push(entry);
    if (flushTimer === null) flushTimer = setTimeout(flush, 0);
    return tracked;
  }
  // A storefront read is already a BFF aggregate. Reuse its bootstrap
  // snapshot and in-flight work, but never nest it inside storefront/batch.
  function aggregateRead(service,action,command,options,context){
    const snapshot=getSnapshot(service,action,command,context);if(snapshot)return Promise.resolve(snapshot);
    const epoch=readEpoch,timeout=Math.max(1,Number(options.timeout)||30000),dedupKey=requestKey(service,action,command,context)+':'+timeout+':'+epoch;
    if(pending.has(dedupKey))return pending.get(dedupKey);
    const promise=transport(service,action,command,options,context).then(result=>{if(epoch!==readEpoch||!current(context))throw changed(service,action);return result.data;});
    const tracked=promise.finally(()=>{if(pending.get(dedupKey)===tracked)pending.delete(dedupKey);});pending.set(dedupKey,tracked);return tracked;
  }
  function call(service, action, command = {}, options = {}) {
    if (!/^[a-z][a-z_]*$/.test(service) || !/^[a-z][a-z_]*$/.test(action)) return Promise.reject(new APIError('接口名称格式不正确'));
    let payload; try { payload = commandFor(command); } catch (_) { return Promise.reject(new APIError('接口参数必须是有效 JSON')); }
    const context = contextFor(options), safeRead = isRead(service, action) && !payload.idempotency_key;
    if(service===C.Service.Storefront&&action===C.Action.Recent&&safeRead)return aggregateRead(service,action,payload,options,context);
    if (batching && safeRead) return read(service, action, payload, options, context);
    if (!safeRead && !directReads.has(service + '/' + action) && service !== C.Service.Storefront) invalidate();
    return transport(service, action, payload, options, context).then(result => result.data);
  }
  async function prefetchBootstrap(command = {}) {
    batching = true; snapshots.clear(); bootstrapActive = true;
    const revision = ++bootstrapRevision, epoch = readEpoch, context = contextFor({});
    try {
      const response = await transport(C.Service.Storefront, C.Action.Bootstrap, commandFor(command), {}, context);
      if (revision !== bootstrapRevision || epoch !== readEpoch || !current(context)) throw changed(C.Service.Storefront, C.Action.Bootstrap);
      if (!Array.isArray(response.data.results)) throw new APIError('商城聚合数据不完整，请刷新重试', {status: 502, traceId: response.traceId});
      const catalog = response.data.results.find(row => row.service === C.Service.Catalog && row.action === C.Action.List);
      if (!catalog || (!catalog.error && (!catalog.data || typeof catalog.data !== 'object'))) throw new APIError('商城聚合数据缺少商品目录，请刷新重试', {status: 502, traceId: response.traceId});
      if (catalog.error) throw childError(catalog, response, context);
      for (const row of response.data.results) {
        if (row.error || !row.data || typeof row.data !== 'object' || !row.command) continue;
        if (row.auth && !context.token) continue;
        const scope = row.auth ? context : {base: context.base, token: '', mode: 'public'};
        putSnapshot(row.service, row.action, commandFor(row.command), scope, row.data);
      }
      return response.data;
    } catch (error) { if (revision === bootstrapRevision) { snapshots.clear(); bootstrapActive = false; } throw error; }
  }
  function endBootstrap() { bootstrapActive = false; bootstrapRevision++; snapshots.clear(); }
  const key = prefix => String(prefix || 'request') + '-' + (window.crypto?.randomUUID ? window.crypto.randomUUID() : Date.now().toString(36) + '-' + Math.random().toString(36).slice(2));
  window.LumoAPI = {
    call, key, APIError, prefetchBootstrap, endBootstrap,
    isBootstrapActive: () => bootstrapActive,
    enableReadBatching: (enabled = true) => { batching = !!enabled; },
    getToken: () => bearer,
    setToken: value => { const next = String(value || ''); if (next !== bearer) { bearer = next; invalidate(); emit('lumo:api-scope-changed', {}); } },
    getBase: () => base, base: () => base,
    setBase: value => { const next = cleanBase(value); if (next !== base) { base = next; bearer = ''; invalidate(); emit('lumo:api-scope-changed', {}); emit('lumo:session-expired', {reason: '接口地址已更改，请重新登录'}); } },
    // These are physical HTTP requests. Cache hits and shared in-flight readers
    // do not inflate the diagnostics with calls which never crossed the network.
    requests: () => requests.map(row => ({...row, ...(row.failedReads ? {failedReads: row.failedReads.map(value => ({...value}))} : {})}))
  };
})();
