'use strict';
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const root = path.resolve(__dirname, '..');
for (const name of ['quota.html', 'login.html']) {
  const html = fs.readFileSync(path.join(root, name), 'utf8');
  const scripts = [...html.matchAll(/<script\b[^>]*>([\s\S]*?)<\/script>/gi)];
  if (!scripts.length) throw Error(name + ': no embedded script');
  for (const [i, match] of scripts.entries()) new vm.Script(match[1], {filename: name + ':' + i});
  if (/\blocalStorage\b/.test(html) || /<script[^>]+\bsrc\s*=/i.test(html)) {
    throw Error(name + ': persistent secret storage or external script');
  }
  if (!html.includes("connect-src 'self'")) throw Error(name + ': missing same-origin CSP');
  console.log(name + ': JavaScript syntax and static privacy checks PASS');
}
