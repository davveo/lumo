'use strict';
const fs = require('node:fs');
const path = require('node:path');
const crypto = require('node:crypto');
const zlib = require('node:zlib');

function build(source = path.join(__dirname, 'dist'), output = path.join(__dirname, 'build')) {
  source = path.resolve(source);
  output = path.resolve(output);
  if (source === output || output.startsWith(source + path.sep))
    throw Error('Build output must be outside the source directory');
  fs.mkdirSync(output, { recursive: true });
  const manifest = {},
    files = [];
  function walk(directory) {
    for (const entry of fs
      .readdirSync(directory, { withFileTypes: true })
      .sort((a, b) => a.name.localeCompare(b.name))) {
      const full = path.join(directory, entry.name);
      if (entry.isSymbolicLink()) throw Error('Symbolic assets are not supported');
      if (entry.isDirectory()) walk(full);
      else files.push(path.relative(source, full));
    }
  }
  walk(source);
  const hash = (body) => crypto.createHash('sha256').update(body).digest('hex').slice(0, 12);
  for (const file of files) {
    const body = fs.readFileSync(path.join(source, file));
    manifest[file] = /\.(?:js|css)$/.test(file) ? file.replace(/(\.[^.]+)$/, '.' + hash(body) + '$1') : file;
  }
  function write(file, body) {
    const target = path.join(output, file);
    fs.mkdirSync(path.dirname(target), { recursive: true });
    fs.writeFileSync(target, body);
    if (/\.(?:js|css|html|svg|json)$/.test(file)) fs.writeFileSync(target + '.gz', zlib.gzipSync(body, { level: 9 }));
  }
  const assetMap = 'window.LumoAssets=Object.freeze(' + JSON.stringify(manifest) + ');\n',
    mapFile = 'asset-manifest.' + hash(assetMap) + '.js';
  write(mapFile, assetMap);
  for (const file of files) {
    let body = fs.readFileSync(path.join(source, file));
    if (file.endsWith('.html')) {
      body = body.toString();
      const scripts = [...body.matchAll(/<script\b[^>]*src="([^"]+)"[^>]*><\/script>/g)],
        styles = [...body.matchAll(/<link\b[^>]*rel="stylesheet"[^>]*href="([^"]+)"[^>]*>/g)];
      const stem = path.basename(file, '.html');
      if (scripts.length) {
        const bundle =
            assetMap + scripts.map((row) => fs.readFileSync(path.join(source, row[1]), 'utf8')).join('\n;\n'),
          name = stem + '.' + hash(bundle) + '.js';
        write(name, bundle);
        scripts.forEach((row, index) => {
          body = body.replace(row[0], index ? '' : '<script src="' + name + '" defer></script>');
        });
      }
      if (styles.length) {
        const bundle = styles.map((row) => fs.readFileSync(path.join(source, row[1]), 'utf8')).join('\n'),
          name = stem + '.' + hash(bundle) + '.css';
        write(name, bundle);
        styles.forEach((row, index) => {
          body = body.replace(row[0], index ? '' : '<link rel="stylesheet" href="' + name + '" />');
        });
      }
      body = body.replace(/\b(src|href)="([^"]+)"/g, (match, attribute, url) =>
        manifest[url] ? attribute + '="' + manifest[url] + '"' : match,
      );
    }
    write(manifest[file], body);
  }
  return manifest;
}
if (require.main === module) build(process.argv[2], process.argv[3]);
module.exports = { build };
