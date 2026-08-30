// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Способность гейта заглушки перемещения УПАСТЬ и СМОЛЧАТЬ — доказательство.
//
// Гейт рядом (`move-stub-single-predicate.test.ts`) утверждает свойство ДЕРЕВА и
// на исправном дереве зелен. Зелёное на чистом дереве — ровно та половина,
// которая не может обнаружить поломку: распознаватель, потерявший способность
// краснеть, выглядит точно так же. Поэтому здесь — синтетика, и по КАЖДОЙ оси
// обе стороны.
//
// Оси, и почему именно они:
//	1. ПРЕДМЕТ. Производитель подписи, решающий вопрос своим перечнем, —
//	   находка; он же, решающий каноническим предикатом, — молчание. Это то,
//	   ради чего гейт заведён.
//	2. КОД, А НЕ ПРОЗА. Подпись, стоящая в КОММЕНТАРИИ, производителем не
//	   является. Ось не теоретическая: слово «Переместить» стоит в шапке
//	   самого гейта, в объяснении перечня и в комментарии у пункта меню —
//	   гейт по подстроке краснел бы на собственном объяснении.
//	3. ИМЯ СВОЙСТВА. Та же строка под другим именем свойства подписи пункта
//	   не производит.
//	4. ГРАНИЦА, НАЗВАННАЯ ВСЛУХ. Подпись, собранная из переменной, гейтом не
//	   видна. Это его предел, а не проверка: он назван здесь, чтобы следующий
//	   читатель не принял молчание за суждение.

import { CANON_PREDICATE, MOVE_LABEL, parseMoveStubSource } from "./move-stub-single-predicate";

const parse = (src: string) => parseMoveStubSource("synthetic.tsx", src);

describe("гейт заглушки перемещения способен упасть и способен смолчать", () => {
  it("инъекция: производитель со СВОИМ перечнем — находка", () => {
    const facts = parse(`
      const items = [
        !["accounts", "projects"].includes(spec.id)
          ? { key: "move", label: "${MOVE_LABEL}", onClick: () => setMoveOpen(true) }
          : null,
      ];
    `);
    expect(facts.producesMoveStub).toBe(true);
    expect(facts.usesCanonicalPredicate).toBe(false);
    expect(facts.producerLines).toEqual([4]);
  });

  it("законный близнец: тот же производитель через канонический предикат — молчание", () => {
    // Отличается от инъекции выше РОВНО решением вопроса, а не формой пункта:
    // иначе молчание объяснялось бы чем угодно ещё.
    const facts = parse(`
      const items = [
        ${CANON_PREDICATE}(spec)
          ? { key: "move", label: "${MOVE_LABEL}", onClick: () => setMoveOpen(true) }
          : null,
      ];
    `);
    expect(facts.producesMoveStub).toBe(true);
    expect(facts.usesCanonicalPredicate).toBe(true);
  });

  it("проза о подписи подписью не является", () => {
    // Обе формы комментария разом: строчный и блочный.
    const facts = parse(`
      // Пункт «${MOVE_LABEL}» открывает окно-заглушку, и предлагать его нельзя.
      /* label: "${MOVE_LABEL}" — так выглядел производитель до сведения. */
      const items = [];
    `);
    expect(facts.producesMoveStub).toBe(false);
    expect(facts.producerLines).toEqual([]);
  });

  it("та же строка под другим именем свойства подписи не производит", () => {
    const facts = parse(`const t = { title: "${MOVE_LABEL}", tooltip: "${MOVE_LABEL}" };`);
    expect(facts.producesMoveStub).toBe(false);
  });

  it("объявление канонического предиката отличается от его вызова", () => {
    const declares = parse(`export function ${CANON_PREDICATE}(spec) { return true; }`);
    expect(declares.declaresCanonicalPredicate).toBe(true);

    const calls = parse(`const ok = ${CANON_PREDICATE}(spec);`);
    expect(calls.declaresCanonicalPredicate).toBe(false);
    expect(calls.usesCanonicalPredicate).toBe(true);
  });

  it("ГРАНИЦА: подпись из переменной гейту не видна — сказано, а не подразумевается", () => {
    // Не «проверка проходит», а «предмет вне наблюдения». Появится в дереве
    // такая форма — расширять придётся распознаватель, и вот здесь написано,
    // почему сегодняшнее молчание суждением не является.
    const facts = parse(`const L = "${MOVE_LABEL}"; const item = { label: L };`);
    expect(facts.producesMoveStub).toBe(false);
  });
});
