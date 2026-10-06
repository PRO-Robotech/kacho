// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

/**
 * САМОПРОВЕРКА ДОВЕРИЯ ЛИСТУ СТЕНДА (kacho#3025, `stand-tls-trust.ts`).
 *
 * Отпечаток SPKI сверяется с НЕЗАВИСИМЫМ производителем — `openssl`, той
 * формулой, которую документирует Chromium для
 * `--ignore-certificate-errors-spki-list`
 * (`x509 -pubkey | pkey -pubin -outform der | dgst -sha256 -binary | base64`).
 * Совпадение двух реализаций — свойство, а не повтор: ошибка формы (DER листа
 * вместо DER ключа, hex вместо base64) дала бы отпечаток, который Chromium
 * молча не узнаёт, и пробы упали бы на сертификате.
 *
 * Исходы: предмет — два ключа дают два разных отпечатка, каждый равен
 * отпечатку openssl; контроль — переменная не задана ⇒ флагов нет; отказ —
 * нечитаемый файл и не-PEM ⇒ исключение с именем переменной.
 */
import { execFileSync } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { spkiFingerprint, standTlsTrustArgs } from "../stand-tls-trust.ts";

let failed = 0;
let checks = 0;
function check(ok: boolean, what: string): void {
  checks++;
  console.log(`  ${ok ? "ОК " : "ПРОВАЛ"} ${what}`);
  if (!ok) failed++;
}

const dir = mkdtempSync(join(tmpdir(), "stand-tls-trust-"));
try {
  const fingerprints: string[] = [];
  for (const name of ["a", "b"]) {
    const key = join(dir, `${name}.key`);
    const crt = join(dir, `${name}.crt`);
    execFileSync("openssl", ["req", "-x509", "-nodes", "-newkey", "ec", "-pkeyopt", "ec_paramgen_curve:P-256",
      "-keyout", key, "-out", crt, "-days", "1", "-subj", "/CN=console.kacho.local",
      "-addext", "subjectAltName=DNS:console.kacho.local"], { stdio: "ignore" });
    const pub = execFileSync("openssl", ["x509", "-pubkey", "-noout", "-in", crt]);
    const der = execFileSync("openssl", ["pkey", "-pubin", "-outform", "der"], { input: pub });
    const digest = execFileSync("openssl", ["dgst", "-sha256", "-binary"], { input: der });
    const want = Buffer.from(digest).toString("base64");
    const args = standTlsTrustArgs(crt);
    check(args.length === 1 && args[0] === `--ignore-certificate-errors-spki-list=${want}`,
      `лист ${name}: флаг браузера несёт отпечаток SPKI, равный отпечатку openssl`);
    fingerprints.push(want);
  }
  check(fingerprints[0] !== fingerprints[1], "два ключа — два отпечатка: доверие не шире одного листа");

  check(standTlsTrustArgs(undefined).length === 0 && standTlsTrustArgs("").length === 0,
    "контроль: KACHO_CONSOLE_CA не задан — флагов нет");

  let threw = "";
  try { standTlsTrustArgs(join(dir, "absent.crt")); } catch (e) { threw = (e as Error).message; }
  check(threw.includes("KACHO_CONSOLE_CA") && threw.includes("не читается"), "нечитаемый файл — отказ с именем переменной");

  threw = "";
  try { spkiFingerprint("это не сертификат\n"); } catch (e) { threw = (e as Error).message; }
  check(threw.includes("PEM"), "не-PEM — отказ, а не пустой отпечаток");
} finally {
  rmSync(dir, { recursive: true, force: true });
}

console.log(`\nпроверок ${checks}, провалов ${failed}`);
if (checks === 0 || failed > 0) {
  console.log("FAIL: stand-tls-trust-selftest");
  process.exit(1);
}
console.log("PASS: stand-tls-trust-selftest");
