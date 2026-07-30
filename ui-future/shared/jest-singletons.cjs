// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1
//
// Отображение синглтонов для jest — те же пакеты, что `resolve.dedupe` в
// vite.config.ts.
//
// ЗАЧЕМ ВООБЩЕ ОТОБРАЖЕНИЕ. Файлы `@shared` лежат ВНЕ пакета remote'а, поэтому их
// импорты jest резолвил бы из `ui-future/node_modules` — то есть подставил бы
// ВТОРУЮ копию react. Вторая копия роняет любой хук в общем компоненте, причём
// сообщением про «invalid hook call», из которого причина не читается. Отображение
// прибивает каждый такой импорт к копии самого remote'а.
//
// ПОЧЕМУ НЕ ОТОБРАЖЕНИЕ НА КАТАЛОГ. Так было, и это работало ровно пока каждый
// пакет объявлял вход по-старому. `react-router@8` объявляет вход ТОЛЬКО через
// `exports`: ни `main`, ни `index.js` у него нет. Отображение на каталог пакета
// jest резолвить не умеет и падает
//
//   Configuration error: Could not locate module react-router mapped as:
//   <remote>/node_modules/$1
//
// причём падает НЕ один тест, а ВСЯ суита целиком («Test suite failed to run»), и
// следом сыпется каскад «require after the Jest environment has been torn down»,
// в котором исходной строчки уже не видно.
//
// Это стоило двух пакетов сразу (nlb — 13 суит, storage — 13), и ещё два (compute,
// registry) несли тот же дефект СКРЫТО: их суиты react-router просто не
// затрагивали, поэтому отображение было сломано и там, но молчало. То есть предмет
// — класс, а не два падения.
//
// ПРИЗНАК — ОТСУТСТВИЕ `main`, А НЕ ИМЯ ПАКЕТА. Прибивать здесь список
// «react-router и остальные» значило бы оставить ту же ловушку следующему пакету,
// который перейдёт на exports-only: он снова унёс бы суиту целиком, и снова
// сообщением не про себя. Поэтому файл входа подставляется ВСЕМ, у кого каталог не
// резолвится, — а у кого резолвится, отображение остаётся байт-в-байт прежним
// (менять разрешение работающих пакетов заодно с починкой значило бы смешать
// починку с изменением поведения).
"use strict";

const fs = require("fs");
const path = require("path");

// Те же пакеты, что `resolve.dedupe` в vite.config.ts каждого remote'а.
const SINGLETONS = ["react", "react-dom", "react-router", "@tanstack/react-query", "antd"];

const escapeRe = (s) => s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

/**
 * moduleNameMapper для точных имён пакетов-синглтонов.
 *
 * @param {string} remoteDir каталог пакета remote'а (обычно `__dirname` его jest.config.cjs)
 * @returns {Record<string,string>} шаблон → путь, куда прибит пакет
 */
function singletonMappings(remoteDir) {
  const local = (...p) => path.join(remoteDir, "node_modules", ...p);
  const out = {};

  for (const name of SINGLETONS) {
    let main;
    try {
      main = JSON.parse(fs.readFileSync(local(name, "package.json"), "utf8")).main;
    } catch {
      // Пакет не установлен. Молча пропустить нельзя: отображения не будет, импорт
      // уедет в ../node_modules и вернётся второй копией react — то есть дефект
      // сменит форму, а не исчезнет. Оставляем отображение на каталог: тогда jest
      // назовёт ненайденный пакет сам, вместо того чтобы тихо подставить чужой.
      out[`^${escapeRe(name)}$`] = local(name);
      continue;
    }

    const dirResolves = Boolean(main) || fs.existsSync(local(name, "index.js"));
    // require.resolve читает `exports`, поэтому находит вход при любой форме объявления.
    out[`^${escapeRe(name)}$`] = dirResolves
      ? local(name)
      : require.resolve(name, { paths: [remoteDir] });
  }

  return out;
}

module.exports = { SINGLETONS, singletonMappings };
