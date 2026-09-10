// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// dictionarycitationgate_injection_test.go — доказательство того, что
// распознаватель цитаты словаря УМЕЕТ находить неполную и УМЕЕТ молчать на
// полной.
//
// # Зачем оно заведено ИМЕННО СЕЙЧАС
//
// До выноса службы доступа обе стороны гейт держал НАСТОЯЩИМ деревом: в нём
// были и неполная цитата (годок порта эмиссии), и семь полных. Оба признака
// жили в её файлах и уехали вместе с нею; предпосылки «цитат не ноль» и «полных
// не ноль» стали падать на ДОСТИЖЕНИИ цели гейта.
//
// Снять их вовсе значило бы сделать «ноль находок» неотличимым от «ноль
// прочитанного». Поэтому обе стороны перенесены на СИНТЕТИКУ, которую строит
// сама проба, и зовут они ТУ ЖЕ функцию, что гейт.
//
// # Три случая, а не два
//
// Третий — цитата ОДНОГО значения — обязателен: без него распознаватель,
// отвечающий «перечисление» на любое совпадение, прошёл бы обе первые пробы, а
// порог в два значения (законная фраза «пустое приводится к ENABLED») перестал
// бы что-либо значить.
package repohygiene

import "testing"

// injCitationDict — синтетический словарь ограничения: три значения.
func injCitationDict() []namedDict {
	return []namedDict{{
		table:  "demo:demo_outbox",
		column: "op",
		values: []string{"WRITE", "DELETE", "TOUCH"},
		source: "0001_demo.sql",
	}}
}

// TestDictionaryCitationRecogniserRedensOnAPartialQuote — НАХОДКА: имя названо,
// пересказаны два значения из трёх.
func TestDictionaryCitationRecogniserRedensOnAPartialQuote(t *testing.T) {
	t.Parallel()
	const comment = "op MUST be one of: WRITE, DELETE (DB CHECK demo_outbox_op_chk)"

	dict := injCitationDict()
	best, hit := dictionaryCitationCoverage(comment, dict)
	if best != 0 {
		t.Fatalf("вариант ограничения не выбран (best=%d) — распознаватель не связал "+
			"комментарий со словарём", best)
	}
	if hit != 2 {
		t.Fatalf("совпавших значений %d, ожидалось 2 — распознаватель считает не то", hit)
	}
	if hit >= len(dict[best].values) {
		t.Fatalf("цитата из %d значений признана ПОЛНОЙ при словаре из %d — гейт молчал бы "+
			"на неполном перечислении, ради которого и заведён", hit, len(dict[best].values))
	}
}

// TestDictionaryCitationRecogniserIsSilentOnAWholeQuote — ЗАКОННЫЙ БЛИЗНЕЦ: та
// же фраза, отличается ОДНИМ фактом — перечислены все три значения.
//
// Без этой стороны гейт был бы неотличим от запрета цитировать словарь вообще.
func TestDictionaryCitationRecogniserIsSilentOnAWholeQuote(t *testing.T) {
	t.Parallel()
	const comment = "op MUST be one of: WRITE, DELETE, TOUCH (DB CHECK demo_outbox_op_chk)"

	dict := injCitationDict()
	best, hit := dictionaryCitationCoverage(comment, dict)
	if best != 0 {
		t.Fatalf("вариант ограничения не выбран (best=%d)", best)
	}
	if hit != len(dict[best].values) {
		t.Fatalf("полная цитата признана неполной: совпало %d из %d — гейт краснел бы на "+
			"верном комментарии, и его сняли бы первым", hit, len(dict[best].values))
	}
}

// TestDictionaryCitationRecogniserDoesNotCallOneValueAnEnumeration — ТРЕТЬЯ
// сторона: одно значение перечислением не является.
//
// В дереве такая фраза законна и встречается («пустое значение приводится к
// ENABLED, чтобы ограничение держалось»). Считай распознаватель её цитатой —
// гейт требовал бы дописать остальные значения туда, где их не перечисляют.
func TestDictionaryCitationRecogniserDoesNotCallOneValueAnEnumeration(t *testing.T) {
	t.Parallel()
	const comment = "пустое значение приводится к WRITE, чтобы demo_outbox_op_chk держалось"

	_, hit := dictionaryCitationCoverage(comment, injCitationDict())
	if hit != 1 {
		t.Fatalf("совпавших значений %d, ожидалось 1", hit)
	}
	if hit >= 2 {
		t.Fatalf("одно значение признано перечислением — порог в два значения перестал " +
			"что-либо значить")
	}
}

// TestDictionaryCitationRecogniserIsSilentWhenNoValueIsQuoted — контроль пустого
// входа: комментарий называет ограничение и НЕ пересказывает словарь.
func TestDictionaryCitationRecogniserIsSilentWhenNoValueIsQuoted(t *testing.T) {
	t.Parallel()
	const comment = "набор значений закрыт ограничением demo_outbox_op_chk; см. миграцию"

	best, hit := dictionaryCitationCoverage(comment, injCitationDict())
	if hit != 0 || best != -1 {
		t.Fatalf("ссылка на ограничение по имени принята за цитату словаря (best=%d, hit=%d): "+
			"гейт запрещал бы ссылаться на ограничение, а запрещает он ПЕРЕСКАЗ", best, hit)
	}
}
