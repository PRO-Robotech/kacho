// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

/**
 * ДОВЕРИЕ ЛИСТУ КОНСОЛИ СВОЕГО СТЕНДА — РОВНО ЕГО КЛЮЧУ (kacho#3025).
 *
 * Стенд kind отдаёт консоль по TLS (`https://console.kacho.local:28443`), лист
 * самоподписан и не знаком ни браузеру, ни Node. Отключить проверку целиком
 * (`ignoreHTTPSErrors`) значило бы принять ЛЮБОЙ лист — и подменённый тоже.
 * Вместо этого доверяется ровно ключ листа, который стенд выпустил:
 *
 *   - браузеру — флагом `--ignore-certificate-errors-spki-list` с отпечатком
 *     SPKI листа (SHA-256 от DER открытого ключа, base64): Chromium пропускает
 *     ошибку цепочки только у листа с этим ключом;
 *   - пути запроса Node — переменной `NODE_EXTRA_CA_CERTS` с тем же файлом;
 *     Node читает её при старте процесса, поэтому ставит её запускающий
 *     (конвейер, `.github/workflows/console-e2e.yml`), а не этот модуль.
 *
 * Файл листа называет `KACHO_CONSOLE_CA` (PEM; на стенде — `ca.crt` секрета
 * `uif-console-tls`). Не задан — доверие не расширяется: внешний стенд с
 * настоящим сертификатом в нём не нуждается. Задан и не читается или не PEM —
 * отказ конфигурации, а не молчаливое «без доверия»: иначе каждая проба упала бы
 * на ошибке сертификата, и «условие не создано» выглядело бы дефектом продукта.
 */
import { X509Certificate, createHash } from "node:crypto";
import { readFileSync } from "node:fs";

/** Отпечаток SPKI листа в форме, которую принимает Chromium. */
export function spkiFingerprint(pem: string): string {
  let cert: X509Certificate;
  try {
    cert = new X509Certificate(pem);
  } catch (e) {
    throw new Error(`KACHO_CONSOLE_CA: файл не разбирается как сертификат PEM (${(e as Error).message})`);
  }
  const der = cert.publicKey.export({ type: "spki", format: "der" });
  return createHash("sha256").update(der).digest("base64");
}

/** Аргументы браузера, доверяющие ровно листу стенда; пусто — без расширения. */
export function standTlsTrustArgs(caPath: string | undefined): string[] {
  if (!caPath) return [];
  let pem: string;
  try {
    pem = readFileSync(caPath, "utf8");
  } catch (e) {
    throw new Error(`KACHO_CONSOLE_CA=${caPath}: файл листа не читается (${(e as Error).message})`);
  }
  return [`--ignore-certificate-errors-spki-list=${spkiFingerprint(pem)}`];
}
