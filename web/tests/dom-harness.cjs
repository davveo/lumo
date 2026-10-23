'use strict';
// A deliberately small DOM for module-contract and HTTP model tests. It does
// not replace real browser layout/interaction verification.
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const crypto = require('node:crypto').webcrypto;
class Element {
  constructor(id = '') { this.id = id; this.innerHTML = ''; this.textContent = ''; this.value = ''; this.disabled = false; this.checked = false; this.hidden = false; this.dataset = {}; this.style = { setProperty() {} }; this.attributes = new Map(); this.children = []; this.classList = { add() {}, remove() {}, toggle() {}, contains() { return false; } }; }
  setAttribute(name, value) { this.attributes.set(name, String(value)); }
  getAttribute(name) { return this.attributes.get(name) ?? null; }
  removeAttribute(name) { this.attributes.delete(name); }
  matches(selector) { return selector === '[data-backdrop]' && this.dataset.backdrop || false; }
  closest(selector) { const attributes = [...selector.matchAll(/\[data-([a-z-]+)/g)].map(row => row[1].replace(/-([a-z])/g, (_, ch) => ch.toUpperCase())); return attributes.some(name => this.dataset[name] !== undefined) ? this : null; }
  append(...items) { this.children.push(...items); }
  prepend(...items) { this.children.unshift(...items); }
  appendChild(item) { this.children.push(item); return item; }
  insertAdjacentHTML(_, html) { this.innerHTML += html; }
  querySelector(selector) { if (selector === 'button' || selector === '[type=submit],button') return this.testButton || (this.testButton = new Element('submit-button')); return null; }
  reset() { this.fields = {}; }
  querySelectorAll() { return []; }
  remove() {}
  focus() {}
  click() {}
  scrollIntoView() {}
}
function storage() { const data = new Map(); return { getItem: key => data.get(key) ?? null, setItem: (key, value) => data.set(key, String(value)), removeItem: key => data.delete(key), clear: () => data.clear() }; }
function createHarness(directory, baseURL, options = {}) {
  const docListeners = new Map(), winListeners = new Map(), timers = new Set(), failures = [];
  const ids = new Map(['app', 'toast', 'modal-root'].map(id => [id, new Element(id)]));
  const add = (map, type, fn, capture) => { if (!map.has(type)) map.set(type, []); map.get(type).push({ fn, capture: capture === true || capture?.capture === true }); };
  const document = {
    readyState: 'loading', body: new Element('body'), documentElement: new Element('html'), title: '',
    addEventListener(type, fn, capture) { add(docListeners, type, fn, capture); },
    removeEventListener() {},
    createElement: tag => new Element(tag),
    querySelector(selector) { if (options.html === 'admin.html' && /^#(?:admin-error|admin-result|operation-result)$/.test(selector) && !ids.has(selector.slice(1))) ids.set(selector.slice(1), new Element(selector.slice(1))); if (selector.startsWith('#') && !selector.includes(' ')) return ids.get(selector.slice(1)) || null; return null; },
    querySelectorAll() { return []; }, getElementById: id => ids.get(id) || null
  };
  let hash = '#/';
  const location = { origin: new URL(baseURL).origin, href: baseURL, reload() {}, get hash() { return hash; }, set hash(value) { hash = value.startsWith('#') ? value : '#' + value; } };
  const context = {
    document, location, history: { replaceState(_, __, url) { if (url) location.hash = String(url).split('#')[1] ? '#' + String(url).split('#')[1] : '#/'; } },
    navigator: { clipboard: { async writeText() {} }, onLine: true }, URL, URLSearchParams, AbortController, Uint8Array, TextEncoder, TextDecoder,
    console, crypto, localStorage: storage(), sessionStorage: storage(),
    CustomEvent: class { constructor(type, options) { this.type = type; this.detail = options?.detail; } },
    FormData: class { constructor(form) { this.fields = new Map(Object.entries(form?.fields || {})); } get(name) { return this.fields.get(name) ?? null; } entries() { return this.fields.entries(); } [Symbol.iterator]() { return this.entries(); } append(name, value) { this.fields.set(name, value); } },
    setTimeout(fn, ms, ...args) { const timer = setTimeout(() => { timers.delete(timer); fn(...args); }, ms); timer.unref?.(); timers.add(timer); return timer; },
    clearTimeout(timer) { timers.delete(timer); clearTimeout(timer); },
    setInterval(fn, ms, ...args) { const timer = setInterval(fn, ms, ...args); timer.unref?.(); timers.add(timer); return timer; }, clearInterval(timer) { timers.delete(timer); clearInterval(timer); },
    requestAnimationFrame(fn) { fn(); }, cancelAnimationFrame() {}, scrollTo() {}, scrollY: 0,
    matchMedia() { return { matches: false, addEventListener() {}, removeEventListener() {} }; },
    addEventListener(type, fn, capture) { add(winListeners, type, fn, capture); },
    dispatchEvent(event) { for (const { fn } of winListeners.get(event.type) || []) { try { const p = fn(event); p?.catch?.(error => failures.push(error)); } catch (error) { failures.push(error); } } },
    async fetch(input, init) { return fetch(new URL(input, baseURL), init); },
    ...options.globals
  };
  context.window = context;
  const ctx = vm.createContext(context);
  const evaluate = source => vm.runInContext(source, ctx);
  const load = file => { vm.runInContext(fs.readFileSync(path.join(directory, file), 'utf8'), ctx, { filename: file }); };
  const html = fs.readFileSync(path.join(directory, options.html || 'index.html'), 'utf8');
  for (const row of html.matchAll(/\bid="([^"]+)"/g)) if (!ids.has(row[1])) ids.set(row[1], new Element(row[1]));
  const scripts = [...html.matchAll(/<script[^>]*\bsrc="([^"]+)"[^>]*>/g)].map(row => row[1]);
  async function fireDocument(type, target = new Element()) {
    let stopped = false;
    const event = { type, target, defaultPrevented: false, preventDefault() { this.defaultPrevented = true; }, stopImmediatePropagation() { stopped = true; }, stopPropagation() { stopped = true; } };
    const listeners = [...(docListeners.get(type) || [])].sort((a, b) => Number(b.capture) - Number(a.capture));
    for (const { fn } of listeners) { if (stopped) break; await fn(event); }
    return event;
  }
  return { ctx, evaluate, load, scripts, document, failures, ids,
    async loadAll({ bootstrap = true } = {}) {
      for (const file of scripts) { if (!bootstrap && file === 'api-bootstrap.js') continue; load(file); }
      document.readyState = 'interactive'; await fireDocument('DOMContentLoaded'); document.readyState = 'complete';
      if (bootstrap && ctx.LumoAppReady) await ctx.LumoAppReady;
    },
    async click(dataset) { const target = new Element('test-button'); target.dataset = { ...dataset }; await fireDocument('click', target); },
    async submit(id, fields, dataset = {}) { const form = new Element(id); form.fields = fields; form.dataset = dataset; await fireDocument('submit', form); },
    navigate(route) { location.hash = '#/' + route; evaluate('render()'); return ids.get('app').innerHTML; },
    json(source) { return JSON.parse(evaluate('JSON.stringify(' + source + ')')); },
    dispose() { for (const timer of timers) { clearTimeout(timer); clearInterval(timer); } timers.clear(); }
  };
}
module.exports = { createHarness, Element };
