(function () {
  'use strict';
  let controller = null,
    timer = null,
    scope = '',
    last = '',
    attempt = 0,
    seen = new Set(),
    initialized = false;
  function stop() {
    controller?.abort();
    controller = null;
    clearTimeout(timer);
    timer = null;
  }
  function reset() {
    stop();
    scope = '';
    last = '';
    seen = new Set();
    initialized = false;
    attempt = 0;
    connect();
  }
  function connect() {
    if (document.hidden || !window.LumoAuth?.isLoggedIn()) return;
    const next = window.LumoAPI.getBase() + '|' + window.LumoAPI.getToken();
    if (next !== scope) {
      stop();
      scope = next;
      last = '';
      seen = new Set();
      initialized = false;
    }
    if (controller) return;
    const current = scope,
      request = new AbortController();
    controller = request;
    const active = () =>
      controller === request &&
      !request.signal.aborted &&
      current === scope &&
      current === window.LumoAPI.getBase() + '|' + window.LumoAPI.getToken() &&
      window.LumoAuth?.isLoggedIn();
    void (async () => {
      try {
        const response = await fetch(window.LumoAPI.getBase() + '/api/v1/notification/stream', {
          headers: {
            Accept: 'text/event-stream',
            Authorization: 'Bearer ' + window.LumoAPI.getToken(),
            ...(last ? { 'Last-Event-ID': last } : {}),
          },
          signal: request.signal,
          cache: 'no-store',
          credentials: 'same-origin',
        });
        if (!active()) return;
        if (response.status === 401) {
          window.dispatchEvent(new CustomEvent('lumo:session-expired'));
          return;
        }
        if (!response.ok || !response.body) throw Error('消息连接暂不可用');
        attempt = 0;
        const reader = response.body.getReader(),
          decoder = new TextDecoder();
        let buffer = '';
        while (true) {
          const { value, done } = await reader.read();
          if (done) break;
          if (!active()) return;
          buffer += decoder.decode(value, { stream: true });
          if (buffer.length > 1024 * 1024) throw Error('消息过大');
          let boundary;
          while ((boundary = buffer.indexOf('\n\n')) >= 0) {
            if (!active()) return;
            const frame = buffer.slice(0, boundary);
            buffer = buffer.slice(boundary + 2);
            const fields = {};
            for (const line of frame.split('\n')) {
              const index = line.indexOf(':');
              if (index > 0) fields[line.slice(0, index)] = line.slice(index + 1).trimStart();
            }
            if (fields.event === 'auth_required') {
              window.dispatchEvent(new CustomEvent('lumo:session-expired'));
              return;
            }
            if (fields.event !== 'notifications' || !fields.data) continue;
            const data = JSON.parse(fields.data);
            last = fields.id || last;
            window.LumoServices?.applyPush?.(data);
            for (const row of data.items || []) {
              if (initialized && !seen.has(row.id) && !row.read) toast(row.title || '你有一条新的商城消息');
              seen.add(row.id);
            }
            if (seen.size > 500) seen = new Set((data.items || []).map((r) => r.id));
            initialized = true;
            window.dispatchEvent(
              new CustomEvent('lumo:notifications-updated', { detail: { unread: Number(data.unread_total) || 0 } }),
            );
          }
        }
      } catch (error) {
        if (active() && error.name !== 'AbortError') attempt++;
      } finally {
        const reconnect = active() && !document.hidden;
        if (controller === request) controller = null;
        if (reconnect) timer = setTimeout(connect, Math.min(30000, 2000 * 2 ** Math.min(attempt, 4)));
      }
    })();
  }
  window.addEventListener('lumo:login', reset);
  window.addEventListener('lumo:logout', reset);
  window.addEventListener('lumo:api-scope-changed', reset);
  window.addEventListener('lumo:ready', connect);
  document.addEventListener('visibilitychange', () => (document.hidden ? stop() : connect()));
  window.LumoRealtime = { connect, stop };
})();
