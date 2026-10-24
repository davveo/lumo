'use strict';
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
module.exports = function loadConstants(
  context,
  directory = process.env.FRONTEND_DIR || path.join(__dirname, '../dist'),
) {
  if (context.window?.LumoConstants) return;
  const source = fs.readFileSync(path.join(directory, 'constants.js'), 'utf8');
  vm.runInContext(source, vm.isContext(context) ? context : vm.createContext(context), { filename: 'constants.js' });
};
