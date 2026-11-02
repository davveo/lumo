(function () {
  'use strict';
  const pending = new Map();
  function load(file) {
    if (pending.has(file)) return pending.get(file);
    const promise = new Promise((resolve, reject) => {
      const css = file.endsWith('.css'),
        element = document.createElement(css ? 'link' : 'script');
      const url = window.LumoAssets?.[file] || file;
      if (css) {
        element.rel = 'stylesheet';
        element.href = url;
      } else {
        element.src = url;
        element.async = false;
      }
      element.onload = () => resolve();
      element.onerror = () => {
        element.remove();
        pending.delete(file);
        reject(Error('资源加载失败，请重试'));
      };
      document.head.appendChild(element);
    });
    pending.set(file, promise);
    return promise;
  }
  let editor = null;
  async function ensureEditor() {
    if (window.LumoAuth?.user()?.role !== window.LumoConstants.Role.Admin) throw Error('需要管理员账号');
    if (editor) return editor;
    editor = (async () => {
      await Promise.all([load('editor.css'), load('category-editor.css')]);
      await load('category-editor.js');
      await load('editor.js');
    })();
    try {
      await editor;
    } catch (cause) {
      editor = null;
      throw cause;
    }
  }
  window.LumoRouteAssets = { ensureEditor };
})();
