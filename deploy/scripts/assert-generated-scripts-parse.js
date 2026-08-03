#!/usr/bin/env node
// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1
//
// assert-generated-scripts-parse.js — every script inside every generated newman
// collection must PARSE. A step whose script does not parse is not a failing step:
// it is a step that never ran, and newman reports it in a channel separate from
// `assertions.failed`, so a suite summary can read "0 failed" while its checks were
// never executed.
//
// Why a parser and not a pattern. The defect was found by eye (a title carrying an
// apostrophe pasted into a single-quoted JS literal), and the first census of it was
// taken with a regular expression over the emitted lines. That regex reported ~100
// affected collections across seven services. The real number, from this parser, was
// 10 — all in one service. The regex could not tell a broken literal from a correctly
// escaped one, and had no control in either direction. So the gate parses.
//
// What it is allowed to conclude. `new Function(src)` accepts exactly what newman's
// sandbox will later accept for a script body, so a pass here means "newman will not
// reject this script for syntax". It says nothing about the script being correct.
//
// The premise this gate depends on, stated so it can be checked: it asserts over the
// files it actually read, and prints that count. Zero findings across zero files is a
// silence, not a pass — so an empty file set is itself a failure.
//
// Usage:  node deploy/scripts/assert-generated-scripts-parse.js <glob-expanded files...>
//         node deploy/scripts/assert-generated-scripts-parse.js --self-test
//         make -C deploy generated-scripts-parse

'use strict';
const fs = require('fs');
const os = require('os');
const path = require('path');

// scanCollections — the whole measurement, factored out so the self-test drives the
// SAME code the pipeline drives. A self-test that re-implements the check proves
// something about the copy, not about the gate.
function scanCollections(files) {
  let scanned = 0;   // scripts actually parsed
  let filesRead = 0; // collections actually opened
  const findings = [];

  for (const file of files) {
    let doc;
    try {
      doc = JSON.parse(fs.readFileSync(file, 'utf8'));
    } catch (e) {
      findings.push(`${file}: collection is not readable JSON — ${e.message}`);
      continue;
    }
    filesRead++;

    const walk = (node, owner) => {
      if (Array.isArray(node)) {
        for (const v of node) walk(v, owner);
        return;
      }
      if (!node || typeof node !== 'object') return;

      const name = node.name || owner;
      for (const ev of node.event || []) {
        let src = ev.script && ev.script.exec;
        if (Array.isArray(src)) src = src.join('\n');
        if (typeof src !== 'string' || src.trim() === '') continue;
        scanned++;
        try {
          // eslint-disable-next-line no-new-func
          new Function(src);
        } catch (e) {
          findings.push(`${file}\n    step "${name}" [${ev.listen}] — ${e.message}`);
        }
      }
      for (const key of Object.keys(node)) {
        if (key !== 'event') walk(node[key], name);
      }
    };
    walk(doc, 'root');
  }
  return { scanned, filesRead, findings };
}

// ── SELF-TEST: proof by injection, in both directions ────────────────────────
//
// Proves not "the gate runs" but "the gate goes red on an injected defect and stays
// silent on a LEGITIMATE construct of the same shape". Without the second half a gate
// catches shape rather than substance, and the first false positive retires it.
//
// The injected defect is the real historical one: a step title carrying an apostrophe
// pasted into a single-quoted JS literal. The legitimate twin is the same title with
// the apostrophe correctly escaped — identical in every respect a pattern could see,
// which is exactly why this gate parses instead of matching.
function selfTest() {
  let rc = 0;
  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'gsp-selftest-'));
  const write = (name, doc) => {
    const p = path.join(tmp, name);
    fs.writeFileSync(p, JSON.stringify(doc, null, 2));
    return p;
  };
  const collection = (stepName, source) => ({
    info: { name: 'synthetic' },
    item: [{ name: stepName, event: [{ listen: 'test', script: { exec: source.split('\n') } }] }],
  });

  console.log('=== assert-generated-scripts-parse --self-test: инъекции ===');

  const broken = write('broken.json',
    collection("delete a user's key", "pm.test('delete a user's key ok', () => {});"));
  const legit = write('legit.json',
    collection("delete a user's key", "pm.test('delete a user\\'s key ok', () => {});"));
  const notJson = path.join(tmp, 'notjson.json');
  fs.writeFileSync(notJson, '{ this is not json');
  const noScripts = write('noscripts.json', { info: { name: 'empty' }, item: [{ name: 'x' }] });

  // 1. DEFECT — must be found, and must name the step, not just the file.
  {
    const r = scanCollections([broken]);
    const named = r.findings.length === 1 && /step "delete a user's key"/.test(r.findings[0]);
    if (named) {
      console.log('  ОК  сломанный скрипт найден, и находка называет ШАГ');
    } else {
      console.error(`  ПРОВАЛ сломанный скрипт не найден или без координаты: ${JSON.stringify(r.findings)}`);
      rc = 1;
    }
  }

  // 2. LEGITIMATE TWIN — same title, correctly escaped. Must stay silent.
  {
    const r = scanCollections([legit]);
    if (r.findings.length === 0 && r.scanned === 1) {
      console.log('  ОК  корректно экранированный близнец промолчал (разобран, но не находка)');
    } else {
      console.error(`  ПРОВАЛ законная конструкция той же формы принята за дефект: ${JSON.stringify(r.findings)}`);
      rc = 1;
    }
  }

  // 3. UNREADABLE collection is a finding, not a silent skip.
  {
    const r = scanCollections([notJson]);
    if (r.findings.length === 1 && r.filesRead === 0) {
      console.log('  ОК  нечитаемая коллекция — находка, а не тихий пропуск');
    } else {
      console.error('  ПРОВАЛ нечитаемая коллекция не стала находкой');
      rc = 1;
    }
  }

  // 4. The census must count what was READ, so "0 findings" differs from "0 read".
  {
    const r = scanCollections([broken, legit, noScripts]);
    if (r.filesRead === 3 && r.scanned === 2) {
      console.log('  ОК  перепись считает прочитанное отдельно от находок (3 файла, 2 скрипта)');
    } else {
      console.error(`  ПРОВАЛ перепись врёт: прочитано ${r.filesRead}, разобрано ${r.scanned}`);
      rc = 1;
    }
  }

  // 5. A generator emitting no scripts at all must NOT pass silently — the premise
  //    check that keeps this gate from being green over nothing.
  {
    const r = scanCollections([noScripts]);
    if (r.findings.length === 0 && r.scanned === 0) {
      console.log('  ОК  коллекция без скриптов даёт scanned=0 — основной проход это проваливает');
    } else {
      console.error('  ПРОВАЛ предпосылка «ноль скриптов» перестала быть различимой');
      rc = 1;
    }
  }

  fs.rmSync(tmp, { recursive: true, force: true });
  console.log('');
  console.log(rc === 0 ? 'PASS: самопроверка assert-generated-scripts-parse'
    : 'FAIL: самопроверка assert-generated-scripts-parse');
  return rc;
}

if (process.argv.includes('--self-test')) {
  process.exit(selfTest());
}

const files = process.argv.slice(2);
if (files.length === 0) {
  console.error('assert-generated-scripts-parse: no collection files given.');
  console.error('  "nothing to check" is not "everything is fine" — the caller must');
  console.error('  pass the generated collections. Refusing to report a pass.');
  process.exit(2);
}

const { scanned, filesRead, findings } = scanCollections(files);

// The census is a separate assertion from the verdict: "0 findings" must be
// distinguishable from "0 scripts looked at".
console.log(`assert-generated-scripts-parse: ${filesRead} collection(s) read, ${scanned} script(s) parsed`);

// Findings are printed BEFORE the census check. Ordering these the other way round
// cost a real diagnosis during this gate's own bring-up: an unreadable collection
// produced a finding, then `scanned === 0` exited first with "contains no scripts",
// so the actual reason never reached the screen. A gate that hides its own evidence
// is the class it exists to catch.
if (findings.length > 0) {
  console.error(`\nFAIL: ${findings.length} problem(s) — steps that DO NOT RUN.`);
  console.error('Newman counts unparseable scripts apart from failed assertions, so a suite');
  console.error('summary can read "0 failed" while the checks below were never executed.\n');
  for (const f of findings) console.error(`  ${f}`);
  process.exit(1);
}

if (scanned === 0) {
  console.error('FAIL: collections were read but they contain no scripts at all.');
  console.error('  A generator that emits no scripts would otherwise pass this gate silently.');
  process.exit(1);
}

console.log('OK: every generated script parses.');
