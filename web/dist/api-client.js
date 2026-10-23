(function () {
  'use strict';

  let bearer = '';
  const requests = [];
  const emit = (name, detail) => window.dispatchEvent(new CustomEvent(name, { detail }));
  const cleanBase = value => {
    if (!value) return '';
    const url = new URL(String(value), location.origin);
    if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password || url.search || url.hash) throw new Error('后端地址格式不正确');
    return url.href.replace(/\/$/, '').replace(/\/api\/v1$/, '');
  };
  let base = '';
  try { base = cleanBase(window.LUMO_API_BASE || ''); } catch (error) { console.error(error.message); }

  class APIError extends Error {
    constructor(message, details = {}) {
      super(message + (details.traceId ? '（链路 ' + details.traceId + '）' : ''));
      this.name = 'APIError';
      Object.assign(this, details);
    }
  }
  const translated = (code, message, httpStatus) => {
    if (/invalid credentials/i.test(message || '')) return '账号或密码不正确';
    if (httpStatus === 401) return '登录状态已失效，请重新登录';
    if (httpStatus === 403 && /administrator/i.test(message || '')) return '此操作需要管理员账号';
    if (httpStatus === 403) return '当前账号没有此操作权限：' + (message || code);
    if (httpStatus >= 500) return '后端服务暂时不可用：' + (message || code);
    return message || code || '请求失败';
  };
  const newTraceParent = () => {
    if (!window.crypto?.getRandomValues) return '';
    const bytes = new Uint8Array(24);
    window.crypto.getRandomValues(bytes);
    const hex = [...bytes].map(v => v.toString(16).padStart(2, '0')).join('');
    return '00-' + hex.slice(0, 32) + '-' + hex.slice(32) + '-01';
  };
  function record(entry) {
    requests.unshift(entry);
    if (requests.length > 200) requests.length = 200;
    emit('lumo:api-request', { ...entry });
  }
  async function call(service, action, command = {}, options = {}) {
    if (!/^[a-z][a-z_]*$/.test(service) || !/^[a-z][a-z_]*$/.test(action)) throw new APIError('接口名称格式不正确');
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), options.timeout || 30000);
    const started = Date.now();
    const headers = { 'Content-Type': 'application/json', Accept: 'application/json' };
    const token = Object.prototype.hasOwnProperty.call(options, 'token') ? options.token : options.auth === false ? '' : bearer;
    if (token) headers.Authorization = 'Bearer ' + token;
    if (command.idempotency_key) headers['Idempotency-Key'] = command.idempotency_key;
    const traceparent = newTraceParent();
    if (traceparent) headers.traceparent = traceparent;
    const payload = {};
    if (command.id !== undefined) payload.id = command.id;
    if (command.idempotency_key) payload.idempotency_key = command.idempotency_key;
    payload.data = command.data || {};
    let traceId = '';
    let httpStatus = 0;
    try {
      const response = await fetch(base + '/api/v1/' + service + '/' + action, {
        method: 'POST', headers, body: JSON.stringify(payload), signal: controller.signal,
        credentials: 'omit', cache: 'no-store', redirect: 'error'
      });
      httpStatus = response.status;
      traceId = response.headers.get('X-Trace-ID') || '';
      let body;
      try { body = await response.json(); }
      catch (_) { throw new APIError('后端地址未提供有效接口，请检查网关及前端接口地址配置', { status: response.status, traceId, service, action }); }
      traceId = body.trace_id || traceId;
      if (!response.ok || body.error) {
        const err = body.error || {};
        const error = new APIError(translated(err.code, err.message, response.status), { status: response.status, code: err.code, traceId, service, action });
        if (response.status === 401 && token && token === bearer && !Object.prototype.hasOwnProperty.call(options, 'token')) {
          bearer = '';
          emit('lumo:session-expired', { traceId });
        }
        throw error;
      }
      if (token && options.auth !== false && !Object.prototype.hasOwnProperty.call(options, 'token') && token !== bearer) {
        throw new APIError('登录账号已变化，请重新加载当前页面', { status: 409, code: 'SessionChanged', traceId, service, action });
      }
      if (!body || typeof body.data !== 'object' || body.data === null) throw new APIError('后端响应缺少数据字段', { status: response.status, traceId, service, action });
      record({ service, action, status: response.status, traceId, duration: Date.now() - started, at: started, ok: true });
      return body.data;
    } catch (error) {
      const apiError = error instanceof APIError ? error : new APIError(error.name === 'AbortError' ? '请求超时，请查询操作结果后使用原请求编号重试' : '无法连接后端服务，请确认服务已启动且接口地址正确', { status: httpStatus, traceId, service, action, cause: error });
      record({ service, action, status: apiError.status || 0, traceId: apiError.traceId || traceId, duration: Date.now() - started, at: started, ok: false });
      emit('lumo:api-error', { service, action, error: apiError });
      throw apiError;
    } finally { clearTimeout(timeout); }
  }
  const key = prefix => String(prefix || 'request') + '-' + (window.crypto?.randomUUID ? window.crypto.randomUUID() : Date.now().toString(36) + '-' + Math.random().toString(36).slice(2));
  window.LumoAPI = {
    call, key, APIError,
    getToken: () => bearer,
    setToken: value => { bearer = String(value || ''); },
    getBase: () => base,
    base: () => base,
    setBase: value => {
      const next = cleanBase(value);
      if (next !== base) { base = next; bearer = ''; emit('lumo:session-expired', { reason: '接口地址已更改，请重新登录' }); }
    },
    requests: () => requests.map(row => ({ ...row }))
  };
})();
