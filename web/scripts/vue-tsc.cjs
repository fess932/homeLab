// vue-tsc, который работает и под Bun.
//
// vue-tsc встраивает поддержку .vue в компилятор TypeScript так: подменяет
// fs.readFileSync и загружает tsc через require, рассчитывая, что require прочитает
// файл подменённой функцией. Загрузчик модулей Bun читает файлы сам, мимо JS-функции,
// поэтому под Bun подмена не срабатывает и tsc не видит .vue-файлы.
//
// Здесь та же подмена делается заранее: изменённый tsc записывается в файл, и vue-tsc
// получает путь к нему. Под Node vue-tsc запускается как обычно — иначе изменение
// применилось бы дважды.
const fs = require('node:fs')
const path = require('node:path')
const { run } = require('vue-tsc')

if (!process.versions.bun) {
  run()
  return
}

const runTscPath = require.resolve('@volar/typescript/lib/quickstart/runTsc')
const { transformTscContent } = require(runTscPath)
const tscPath = require.resolve('typescript/lib/tsc')
let source = fs.readFileSync(tscPath, 'utf8')
// С TypeScript 5.7 lib/tsc.js — обёртка над ./_tsc.js.
const shim = /module\.exports\s*=\s*require\((?:"|')(\.\/\w+\.js)(?:"|')\)/.exec(source)
if (shim) source = fs.readFileSync(path.join(path.dirname(tscPath), shim[1]), 'utf8')

// Рядом с исходным tsc: библиотеки lib.*.d.ts компилятор ищет в своём каталоге.
const patched = path.join(path.dirname(tscPath), '_tsc.vue-bun.js')
fs.writeFileSync(patched, transformTscContent(source, require.resolve('@volar/typescript/lib/node/proxyCreateProgram'), ['vue'], [], runTscPath))
run(patched)
