// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import path from "node:path";
import { fileURLToPath } from "node:url";
import { codeSources } from "./provider-address-census";
import { returnToReaders } from "./return-to-readers";

/**
 * Гейт: адрес возврата читает ОДИН узел во всём прод-дереве каркаса и общей
 * библиотеки — `useReturnTo` (приёмка F6b, Р8; условие ревью безопасности о
 * четырёх выходах экрана подтверждения). Каждый выход с экрана подтверждения
 * берёт адрес возврата у него, и суждение о происхождении у всех одно.
 *
 * Перепись печатает знаменатель — число прочитанных файлов — и краснеет на
 * пустом обходе. Способность упасть держит инъекция ниже.
 */

const uiRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");
const THE_READER = "shared/src/pages/auth/use-return-to.ts";

function isProductFile(rel: string): boolean {
  return (
    /^(shared|host)\/src\//.test(rel) && /\.(ts|tsx)$/.test(rel) && !/\.test\./.test(rel) && !/(^|\/)test\//.test(rel)
  );
}

const product = codeSources(uiRoot, isProductFile);
const readers = returnToReaders(product);
process.stdout.write(
  `\n[адрес возврата] прод-файлов shared/host прочитано ${product.length} · читателей параметра ${readers.length}: ` +
    `${readers.map((r) => `${r.file}:${r.line}`).join(", ")}\n`,
);

describe("адрес возврата — один читатель", () => {
  it("знаменатель обхода — прод-дерево shared и host прочитано", () => {
    expect(product.length).toBeGreaterThan(200);
    expect(product.some((s) => s.file === THE_READER)).toBe(true);
  });

  it("читатель параметра адреса возврата ровно один — useReturnTo", () => {
    expect(readers.map((r) => r.file)).toEqual([THE_READER]);
  });

  it("инъекция: второй читатель находится с координатой; комментарий и чужой ключ — нет", () => {
    const found = returnToReaders([
      { file: "host/src/planted.ts", text: "const to = new URLSearchParams(search).get(RETURN_TO_PARAM);" },
      { file: "host/src/planted2.tsx", text: 'const to = params.get("returnTo");' },
      { file: "host/src/planted3.ts", text: "const to = q.get(addresses.RETURN_TO_PARAM);" },
      { file: "host/src/twin.ts", text: '// .get(RETURN_TO_PARAM)\nconst p = params.get("form");' },
    ]);
    expect(found.map((r) => `${r.file}:${r.line}`)).toEqual([
      "host/src/planted.ts:1",
      "host/src/planted2.tsx:1",
      "host/src/planted3.ts:1",
    ]);
  });
});
