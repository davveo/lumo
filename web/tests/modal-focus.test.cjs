'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');
const { createHarness, Element } = require('./dom-harness.cjs');
const directory = process.env.FRONTEND_DIR || path.join(__dirname, '../dist');
function setup() {
  const app = createHarness(directory, 'http://modal-focus.test');
  app.load('app.js');
  return app;
}
test('closing or replacing a dialog returns keyboard focus to its original connected trigger', (t) => {
  const app = setup();
  t.after(() => app.dispose());
  const trigger = new Element('original-trigger');
  trigger.isConnected = true;
  let focused = 0;
  trigger.focus = () => focused++;
  app.document.activeElement = trigger;
  app.ctx.modal('地址管理', '<button>保存</button>');
  const internal = new Element('dialog-button');
  internal.isConnected = true;
  internal.focus = () => assert.fail('replacement dialog must preserve the original trigger');
  app.document.activeElement = internal;
  app.ctx.modal('确认地址', '<button>确定</button>');
  app.ctx.closeModal();
  assert.equal(focused, 1);
  assert.equal(app.ids.get('modal-root').innerHTML, '');
  app.ctx.closeModal();
  assert.equal(focused, 1, 'repeated close must not move focus again');
});
test('closing after navigation does not focus a removed trigger', (t) => {
  const app = setup();
  t.after(() => app.dispose());
  const trigger = new Element('removed-trigger');
  trigger.isConnected = true;
  trigger.focus = () => assert.fail('removed controls must not receive focus');
  app.document.activeElement = trigger;
  app.ctx.modal('帮助', '<button>关闭</button>');
  trigger.isConnected = false;
  app.ctx.closeModal();
  assert.equal(app.ids.get('modal-root').innerHTML, '');
});
