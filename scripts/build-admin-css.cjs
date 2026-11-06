'use strict';
const { spawnSync } = require('node:child_process'),
  fs = require('node:fs'),
  path = require('node:path'),
  os = require('node:os');
const root = path.join(__dirname, '..'),
  target = path.join(root, 'web/dist/admin-tailwind.css'),
  check = process.argv.includes('--check');
const directory = check ? fs.mkdtempSync(path.join(os.tmpdir(), 'lumo-tailwind-')) : null,
  output = check ? path.join(directory, 'admin.css') : target;
try {
  const cli = path.join(root, 'node_modules/@tailwindcss/cli/dist/index.mjs');
  const result = spawnSync(
    process.execPath,
    [cli, '-i', path.join(root, 'web/styles/admin.css'), '-o', output, '--minify'],
    { cwd: root, stdio: 'inherit' },
  );
  if (result.status !== 0) process.exitCode = result.status || 1;
  else if (check && fs.readFileSync(output, 'utf8') !== fs.readFileSync(target, 'utf8')) {
    process.stderr.write('Tailwind output is stale. Run npm run build:admin-css.\n');
    process.exitCode = 1;
  } else process.stdout.write(check ? 'Tailwind output is current.\n' : 'Admin Tailwind stylesheet generated.\n');
} finally {
  if (directory) fs.rmSync(directory, { recursive: true, force: true });
}
