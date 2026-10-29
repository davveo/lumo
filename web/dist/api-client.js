(function () {
  'use strict';
  const C = window.LumoConstants;

  let bearer = '',
    base = '',
    readEpoch = 0;
  let bootstrapActive = false,
    bootstrapRevision = 0;
  const requests = [],
    pending = new Map(),
    sectionFlights = new Map(),
    snapshots = new Map(),
    snapshotFailures = new Map();
  const emptySummary = () => ({
    loading: false,
    loaded: false,
    error: '',
    profile: null,
    coupon_count: null,
    points: null,
    favorite_count: null,
    cart_count: null,
    unread_count: null,
  });
  let accountSummary = emptySummary(),
    accountFlight = null,
    summaryRevision = 0,
    summaryAt = 0;
  const summaryRefreshes = new Map();
  const emit = (name, detail) => window.dispatchEvent(new CustomEvent(name, { detail }));
  const own = (object, key) => Object.prototype.hasOwnProperty.call(object, key);
  const clone = (value) => JSON.parse(JSON.stringify(value));
  const stable = (value) =>
    JSON.stringify(value, (_, row) =>
      row && typeof row === 'object' && !Array.isArray(row)
        ? Object.keys(row)
            .sort()
            .reduce((result, key) => {
              if (row[key] !== undefined) result[key] = row[key];
              return result;
            }, Object.create(null))
        : row,
    );
  const cleanBase = (value) => {
    if (!value) return '';
    const url = new URL(String(value), location.origin);
    if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password || url.search || url.hash)
      throw new Error('后端地址格式不正确');
    return url.href.replace(/\/$/, '').replace(/\/api\/v1$/, '');
  };
  try {
    base = cleanBase(window.LUMO_API_BASE || '');
  } catch (error) {
    console.error(error.message);
  }

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
  const readActions = {
    [C.Service.Storefront]: [
      C.Action.Recent,
      C.Action.Shell,
      C.Action.Products,
      C.Action.Activities,
      C.Action.AccountSummary,
    ],
    [C.Service.Catalog]: [C.Action.List, C.Action.Get, C.Action.BatchGet, C.Action.AdminList],
    [C.Service.Inventory]: [C.Action.Get, C.Action.BatchGet, C.Action.AdminList, C.Action.Adjustments, C.Action.Ledger],
    [C.Service.Search]: [C.Action.Query, C.Action.PolicyGet],
    [C.Service.Recommendation]: [C.Action.List, C.Action.PolicyGet],
    [C.Service.Content]: [C.Action.List, C.Action.Get, C.Action.AdminList, C.Action.PopupList],
    [C.Service.Campaign]: [C.Action.List, C.Action.Evaluate, C.Action.AdminList],
    [C.Service.Group]: [C.Action.List, C.Action.Get, C.Action.AdminList, C.Action.Rules, C.Action.RulesAdmin],
    [C.Service.Flash]: [C.Action.List, C.Action.AdminList, C.Action.SessionGet, C.Action.RequestsAdmin],
    [C.Service.Interaction]: [C.Action.List],
    [C.Service.Logistics]: [C.Action.Get, C.Action.PolicyGet],
    [C.Service.Helpdesk]: [C.Action.Faq, C.Action.List, C.Action.Get],
    [C.Service.Member]: [C.Action.Get, C.Action.PolicyGet],
    [C.Service.Address]: [C.Action.List, C.Action.Get],
    [C.Service.Cart]: [C.Action.List],
    [C.Service.Engagement]: [C.Action.List, C.Action.Subscriptions, C.Action.PolicyGet, C.Action.AdminList],
    [C.Service.Coupon]: [C.Action.List, C.Action.Templates, C.Action.TemplatesSnapshot, C.Action.TemplatesAdmin],
    [C.Service.Points]: [C.Action.Get, C.Action.AdminGet, C.Action.Ledger, C.Action.PolicyGet],
    [C.Service.Order]: [C.Action.List, C.Action.AdminList, C.Action.AdminSummary],
    [C.Service.Aftersale]: [C.Action.List, C.Action.Get, C.Action.AdminList, C.Action.AdminSummary],
    [C.Service.Fulfillment]: [C.Action.List, C.Action.Get, C.Action.AdminList, C.Action.AdminSummary],
    [C.Service.Invoice]: [C.Action.List, C.Action.Get, C.Action.Reversals, C.Action.AdminList, C.Action.AdminSummary],
    [C.Service.Notification]: [C.Action.List, C.Action.TemplatesAdmin],
    [C.Service.Payment]: [
      C.Action.List,
      C.Action.AdminList,
      C.Action.AdminSummary,
      C.Action.Methods,
      C.Action.PolicyGet,
    ],
    [C.Service.Refund]: [C.Action.List, C.Action.AdminList, C.Action.AdminSummary],
    [C.Service.Reconciliation]: [C.Action.List, C.Action.AdminList, C.Action.Get],
    [C.Service.Analytics]: [C.Action.List],
    [C.Service.IAM]: [
      C.Action.AdminList,
      C.Action.AdminGet,
      C.Action.AdminSummary,
      C.Action.AuditList,
      C.Action.LoginMethods,
      C.Action.PolicyGet,
    ],
    [C.Service.Management]: [C.Action.Dashboard, C.Action.Tasks],
  };
  const isRead = (service, action) => (readActions[service] || []).includes(action);
  // These operations must observe each call's current state and stay outside the
  // read batch. Quotes may reserve/write state even though their names look like reads.
  const directReads = new Set([
    C.Service.IAM + '/' + C.Action.Current,
    C.Service.IAM + '/' + C.Action.Validate,
    C.Service.Pricing + '/' + C.Action.Get,
    C.Service.Payment + '/' + C.Action.Get,
    C.Service.Order + '/' + C.Action.Get,
    C.Service.Flash + '/' + C.Action.Get,
    C.Service.Refund + '/' + C.Action.Get,
    C.Service.Logistics + '/' + C.Action.Quote,
    C.Service.Media + '/' + C.Action.Get,
    C.Service.Media + '/' + C.Action.Download,
  ]);
  const contextFor = (options) => {
    const explicit = own(options, 'token');
    const token = String(explicit ? options.token || '' : options.auth === false ? '' : bearer);
    return { base, token, mode: token ? (explicit ? 'explicit' : 'session') : 'public' };
  };
  const current = (context) => context.base === base && (context.mode !== 'session' || context.token === bearer);
  const requestKey = (service, action, command, context) =>
    stable([context.base, context.mode, context.token, service, action, command]);
  const changed = (service, action) =>
    new APIError('登录账号、接口或数据已变化，请重新加载当前页面', {
      status: 409,
      code: C.ApiErrorCode.SessionChanged,
      service,
      action,
    });
  function invalidate(resetAccount = false) {
    readEpoch++;
    snapshots.clear();
    snapshotFailures.clear();
    pending.clear();
    sectionFlights.clear();
    if (resetAccount) {
      summaryRevision++;
      accountSummary = emptySummary();
      summaryAt = 0;
      accountFlight = null;
      summaryRefreshes.clear();
    }
  }
  function expire(context, traceId) {
    if (context.mode === 'session' && context.token && context.token === bearer) {
      bearer = '';
      invalidate(true);
      emit('lumo:session-expired', { traceId });
    }
  }
  function commandFor(command) {
    const payload = { data: command.data || {} };
    if (command.id !== undefined) payload.id = command.id;
    if (command.idempotency_key) payload.idempotency_key = command.idempotency_key;
    return clone(payload);
  }
  const newTraceParent = () => {
    if (!window.crypto?.getRandomValues) return '';
    const bytes = new Uint8Array(24);
    window.crypto.getRandomValues(bytes);
    const hex = [...bytes].map((value) => value.toString(16).padStart(2, '0')).join('');
    return '00-' + hex.slice(0, 32) + '-' + hex.slice(32) + '-01';
  };
  function record(entry) {
    requests.unshift(entry);
    if (requests.length > 200) requests.length = 200;
    emit('lumo:api-request', { ...entry });
    return entry;
  }
  async function transport(service, action, payload, options, context) {
    const controller = new AbortController(),
      started = Date.now(),
      epoch = readEpoch;
    const abort = () => controller.abort();
    if (options.signal?.aborted) abort();
    else options.signal?.addEventListener('abort', abort, { once: true });
    const timeout = setTimeout(() => controller.abort(), options.timeout || 30000);
    const headers = { 'Content-Type': 'application/json', Accept: 'application/json' };
    if (context.token) headers.Authorization = 'Bearer ' + context.token;
    if (payload.idempotency_key) headers['Idempotency-Key'] = payload.idempotency_key;
    const traceparent = newTraceParent();
    if (traceparent) headers.traceparent = traceparent;
    let traceId = '',
      httpStatus = 0;
    try {
      const response = await fetch(context.base + '/api/v1/' + service + '/' + action, {
        method: 'POST',
        headers,
        body: JSON.stringify(payload),
        signal: controller.signal,
        credentials: 'same-origin',
        cache: 'no-store',
        redirect: 'error',
      });
      httpStatus = response.status;
      traceId = response.headers.get('X-Trace-ID') || '';
      let body;
      try {
        body = await response.json();
      } catch (_) {
        throw new APIError('后端地址未提供有效接口，请检查网关及前端接口地址配置', {
          status: httpStatus,
          traceId,
          service,
          action,
        });
      }
      if (!response.ok || body.error) {
        const error = body.error || {};
        if (httpStatus === 401) expire(context, traceId);
        throw new APIError(translated(error.code, error.message, httpStatus), {
          status: httpStatus,
          code: error.code,
          traceId,
          service,
          action,
        });
      }
      if (!current(context)) throw changed(service, action);
      if (!body || typeof body.data !== 'object' || body.data === null)
        throw new APIError('后端响应缺少数据字段', { status: httpStatus, traceId, service, action });
      // Domain reads must not update header projections before their caller
      // rejects data invalidated by a newer mutation or session scope.
      if (epoch === readEpoch) primeSummaryRead(service, action, payload, body.data, context);
      if (!isRead(service, action) && !directReads.has(service + '/' + action))
        refreshSummaryAfterWrite(service, payload, context);
      const entry = record({
        service,
        action,
        status: httpStatus,
        traceId,
        duration: Date.now() - started,
        at: started,
        ok: true,
      });
      return { data: body.data, traceId, status: httpStatus, entry };
    } catch (error) {
      const apiError =
        error instanceof APIError
          ? error
          : new APIError(
              error.name === 'AbortError'
                ? '请求超时，请查询操作结果后使用原请求编号重试'
                : '无法连接后端服务，请确认服务已启动且接口地址正确',
              { status: httpStatus, traceId, service, action, cause: error },
            );
      record({
        service,
        action,
        status: apiError.status || 0,
        traceId: apiError.traceId || traceId,
        duration: Date.now() - started,
        at: started,
        ok: false,
      });
      emit('lumo:api-error', { service, action, error: apiError });
      throw apiError;
    } finally {
      clearTimeout(timeout);
      options.signal?.removeEventListener('abort', abort);
    }
  }
  function projectedCount(value, fallback) {
    return value != null && Number.isFinite(Number(value)) && Number(value) >= 0 ? Math.floor(Number(value)) : fallback;
  }
  function primeSummaryRead(service, action, command, data, context) {
    if (context.mode !== 'session' || !context.token || !current(context)) return;
    let fields = null;
    if (service === C.Service.Member && action === C.Action.Get)
      fields = { profile: { display_name: data.display_name, level: data.level, avatar: data.avatar } };
    if (service === C.Service.Points && action === C.Action.Get) fields = { points: data.available ?? null };
    if (service === C.Service.Coupon && action === C.Action.List)
      fields = {
        coupon_count: (data.items || []).filter(
          (row) => row.status === C.CouponStatus.Available && Date.parse(row.expires_at) > Date.now(),
        ).length,
      };
    if (
      service === C.Service.Cart &&
      action === C.Action.List &&
      !command.data.kind &&
      !command.data.status &&
      (data.quantity_total != null || !Number(command.data.offset))
    )
      fields = {
        cart_count: projectedCount(
          data.quantity_total,
          (data.items || []).reduce((sum, row) => sum + Math.max(0, Number(row.quantity) || 0), 0),
        ),
      };
    if (service === C.Service.Notification && action === C.Action.List && !command.data.kind && !command.data.status)
      fields = {
        unread_count: projectedCount(
          data.unread_total,
          (data.items || []).filter((row) => row.status !== C.NotificationStatus.Read).length,
        ),
      };
    if (
      service === C.Service.Engagement &&
      action === C.Action.List &&
      command.data.kind === C.EngagementKind.Favorite &&
      !command.data.status
    ) {
      const ids = (data.items || []).map((row) => row.sku_id).filter(Boolean);
      fields = { favorite_count: projectedCount(data.total, ids.length) };
      if (!Number(command.data.offset)) fields.favorite_ids = ids;
    }
    if (fields) {
      accountSummary = { ...accountSummary, ...fields, loading: false, loaded: true };
      const key = requestKey(service, action, command, context),
        timer = summaryRefreshes.get(key);
      if (timer !== undefined) {
        clearTimeout(timer);
        summaryRefreshes.delete(key);
      }
    }
  }
  function refreshSummaryAfterWrite(service, command, context) {
    if (context.mode !== 'session' || !context.token || !current(context)) return;
    const target = {
      [C.Service.Member]: [C.Action.Get, {}],
      [C.Service.Points]: [C.Action.Get, {}],
      [C.Service.Coupon]: [C.Action.List, {}],
      [C.Service.Cart]: [C.Action.List, {}],
      [C.Service.Notification]: [C.Action.List, { data: { limit: 100 } }],
      [C.Service.Engagement]:
        !command.data.kind || command.data.kind === C.EngagementKind.Favorite
          ? [C.Action.List, { data: { kind: C.EngagementKind.Favorite, limit: 100 } }]
          : null,
    }[service];
    if (!target) return; // Analytics/history/search writes do not change header metrics.
    const key = requestKey(service, target[0], commandFor(target[1]), context);
    if (summaryRefreshes.has(key)) return;
    summaryRevision++;
    const timer = setTimeout(() => {
      summaryRefreshes.delete(key);
      if (!current(context)) return;
      void call(service, target[0], target[1]).catch(() => {});
    }, 0);
    summaryRefreshes.set(key, timer);
  }
  function getSnapshot(service, action, command, context) {
    if (!bootstrapActive) return null;
    const key = requestKey(service, action, command, context);
    if (snapshotFailures.has(key)) {
      const failure = snapshotFailures.get(key);
      throw new APIError(translated(failure.code, failure.message, failure.status), {
        status: failure.status,
        code: failure.code,
        traceId: failure.traceId,
        service,
        action,
      });
    }
    if (snapshots.has(key)) return clone(snapshots.get(key));
    if (
      action === C.Action.BatchGet &&
      [C.Service.Inventory, C.Service.Catalog].includes(service) &&
      Array.isArray(command.data.ids)
    ) {
      const items = [];
      for (const id of [...new Set(command.data.ids)]) {
        const row = snapshots.get(requestKey(service, C.Action.Get, { id, data: {} }, context));
        if (!row) return null;
        items.push(clone(row));
      }
      return { items, missing_ids: [] };
    }
    return null;
  }
  function putSnapshot(service, action, command, context, data) {
    if (!isRead(service, action) || snapshots.size >= 10000) return;
    snapshots.set(requestKey(service, action, command, context), clone(data));
    if (action === C.Action.BatchGet && [C.Service.Inventory, C.Service.Catalog].includes(service)) {
      for (const row of data.items || []) {
        const id = service === C.Service.Inventory ? row.sku_id : row.id;
        if (id) putSnapshot(service, C.Action.Get, { id, data: {} }, context, row);
      }
    }
  }
  function childError(row, response, context) {
    const details = row.error || {},
      code = Number(details.status) || 500;
    if (code === 401) expire(context, response.traceId);
    const error = new APIError(translated(details.code, details.message, code), {
      status: code,
      code: details.code,
      traceId: response.traceId,
      service: row.service,
      action: row.action,
    });
    emit('lumo:api-error', { service: row.service, action: row.action, error });
    return error;
  }
  // A storefront read is already a BFF aggregate. Reuse its bootstrap
  // snapshot and in-flight work, but never nest it inside storefront/batch.
  function aggregateRead(service, action, command, options, context) {
    let snapshot;
    try {
      snapshot = getSnapshot(service, action, command, context);
    } catch (error) {
      return Promise.reject(error);
    }
    if (snapshot) return Promise.resolve(snapshot);
    const epoch = readEpoch,
      timeout = Math.max(1, Number(options.timeout) || 30000),
      dedupKey = requestKey(service, action, command, context) + ':' + timeout + ':' + epoch;
    if (pending.has(dedupKey)) return pending.get(dedupKey);
    const promise = transport(service, action, command, options, context).then((result) => {
      if (epoch !== readEpoch || !current(context)) throw changed(service, action);
      return result.data;
    });
    const tracked = promise.finally(() => {
      if (pending.get(dedupKey) === tracked) pending.delete(dedupKey);
    });
    pending.set(dedupKey, tracked);
    return tracked;
  }
  function call(service, action, command = {}, options = {}) {
    if (!/^[a-z][a-z_]*$/.test(service) || !/^[a-z][a-z_]*$/.test(action))
      return Promise.reject(new APIError('接口名称格式不正确'));
    let payload;
    try {
      payload = commandFor(command);
    } catch (_) {
      return Promise.reject(new APIError('接口参数必须是有效 JSON'));
    }
    const context = contextFor(options),
      safeRead = isRead(service, action) && !payload.idempotency_key;
    // Coalesce identical work only. Unrelated reads retain their semantic URL;
    // no client-generated service/action list crosses an HTTP boundary.
    if (safeRead) return aggregateRead(service, action, payload, options, context);
    if (!safeRead && !directReads.has(service + '/' + action) && service !== C.Service.Storefront) invalidate();
    return transport(service, action, payload, options, context).then((result) => result.data);
  }
  function beginSections() {
    snapshots.clear();
    snapshotFailures.clear();
    sectionFlights.clear();
    bootstrapActive = true;
    bootstrapRevision++;
  }
  function prefetchSection(section, command = {}) {
    if (![C.Action.Shell, C.Action.Products, C.Action.Activities].includes(section))
      return Promise.reject(new APIError('未知页面分区'));
    const revision = bootstrapRevision,
      epoch = readEpoch,
      context = contextFor(section === C.Action.Activities ? {} : { auth: false });
    const sectionKey = requestKey(C.Service.Storefront, section, commandFor(command), context);
    if (sectionFlights.has(sectionKey)) return sectionFlights.get(sectionKey);
    const promise = transport(C.Service.Storefront, section, commandFor(command), {}, context).then((response) => {
      if (revision !== bootstrapRevision || (context.mode !== 'public' && epoch !== readEpoch) || !current(context))
        throw changed(C.Service.Storefront, section);
      if (!Array.isArray(response.data.results))
        throw new APIError('页面分区数据不完整，请刷新重试', { status: 502, traceId: response.traceId });
      if (section === C.Action.Products) {
        const catalog = response.data.results.find(
          (row) => row.service === C.Service.Catalog && row.action === C.Action.List,
        );
        if (!catalog || (!catalog.error && (!catalog.data || typeof catalog.data !== 'object')))
          throw new APIError('商品分区缺少目录，请刷新重试', { status: 502, traceId: response.traceId });
        if (catalog.error) throw childError(catalog, response, context);
      }
      for (const row of response.data.results) {
        if (
          section === C.Action.Shell &&
          row.optional === true &&
          row.error?.code === 'NotFound' &&
          row.service === C.Service.Content &&
          row.action === C.Action.Get &&
          row.command?.id === 'storefront-main'
        ) {
          const scope = { base: context.base, token: '', mode: 'public' };
          snapshotFailures.set(requestKey(row.service, row.action, commandFor(row.command), scope), {
            ...row.error,
            traceId: response.traceId,
          });
        }
        if (row.error || !row.data || typeof row.data !== 'object' || !row.command) continue;
        if (row.auth && !context.token) continue;
        const scope = row.auth ? context : { base: context.base, token: '', mode: 'public' };
        putSnapshot(row.service, row.action, commandFor(row.command), scope, row.data);
      }
      return response.data;
    });
    const tracked = promise.finally(() => {
      if (sectionFlights.get(sectionKey) === tracked) sectionFlights.delete(sectionKey);
    });
    sectionFlights.set(sectionKey, tracked);
    return tracked;
  }
  // Compatibility for embedded clients. The old bootstrap URL is no longer
  // requested; even its server alias now contains the small public shell only.
  function prefetchBootstrap(command = {}) {
    beginSections();
    return prefetchSection(C.Action.Shell, command);
  }
  function refreshAccountSummary(force = false) {
    if (!bearer) {
      accountSummary = emptySummary();
      return Promise.resolve(accountSummary);
    }
    if (accountFlight) return accountFlight;
    if (!force && summaryAt && Date.now() - summaryAt < 30000) return Promise.resolve(clone(accountSummary));
    const context = contextFor({}),
      epoch = summaryRevision;
    accountSummary = { ...accountSummary, loading: true, error: '' };
    const promise = transport(C.Service.Storefront, C.Action.AccountSummary, { data: {} }, {}, context)
      .then(({ data }) => {
        if (!current(context) || epoch !== summaryRevision)
          throw changed(C.Service.Storefront, C.Action.AccountSummary);
        accountSummary = { ...emptySummary(), ...data.summary, loaded: true, errors: data.errors || {} };
        summaryAt = Date.now();
        return clone(accountSummary);
      })
      .catch((error) => {
        if (current(context) && epoch === summaryRevision)
          accountSummary = { ...accountSummary, loading: false, error: error.message };
        throw error;
      });
    const tracked = promise.finally(() => {
      if (accountFlight === tracked) {
        accountFlight = null;
        if (current(context)) accountSummary = { ...accountSummary, loading: false };
      }
    });
    accountFlight = tracked;
    return tracked;
  }
  function endBootstrap() {
    bootstrapActive = false;
    bootstrapRevision++;
    snapshots.clear();
    snapshotFailures.clear();
  }
  const key = (prefix) =>
    String(prefix || 'request') +
    '-' +
    (window.crypto?.randomUUID
      ? window.crypto.randomUUID()
      : Date.now().toString(36) + '-' + Math.random().toString(36).slice(2));
  window.LumoAccountSummary = { model: () => clone(accountSummary), refresh: refreshAccountSummary };
  window.LumoAPI = {
    call,
    key,
    APIError,
    prefetchBootstrap,
    prefetchSection,
    beginSections,
    endBootstrap,
    isBootstrapActive: () => bootstrapActive,
    // Kept for older modules; read coalescing no longer creates generic batches.
    enableReadBatching: () => {},
    getToken: () => bearer,
    setToken: (value) => {
      const next = String(value || '');
      if (next !== bearer) {
        bearer = next;
        invalidate(true);
        emit('lumo:api-scope-changed', {});
      }
    },
    getBase: () => base,
    base: () => base,
    setBase: (value) => {
      const next = cleanBase(value);
      if (next !== base) {
        base = next;
        bearer = '';
        invalidate(true);
        emit('lumo:api-scope-changed', {});
        emit('lumo:session-expired', { reason: '接口地址已更改，请重新登录' });
      }
    },
    // These are physical HTTP requests. Cache hits and shared in-flight readers
    // do not inflate the diagnostics with calls which never crossed the network.
    requests: () =>
      requests.map((row) => ({
        ...row,
        ...(row.failedReads ? { failedReads: row.failedReads.map((value) => ({ ...value })) } : {}),
      })),
  };
})();
