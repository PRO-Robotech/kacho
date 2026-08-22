// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// carriedcoordinateledger_test.go — ведомость координат, переносимых в F4,
// ИСТЕКАЕТ САМА (приёмка F2, сценарий F2-46, §9.4;
// `testing.md` §«Гейт на класс» п. 5).
//
// # Предмет
//
// Фаза заводит принимающую сторону и ничего не сносит. У каждой координаты,
// дожившей до F4, обязан быть назван исход из закрытого словаря; четвёртого —
// «осталось как есть, потому что не заметили» — не существует. У исхода
// «оставлено» обязан быть ПРЕДИКАТ СНЯТИЯ: послабление, которое не умеет
// истечь, переживает свой предмет, а освободившееся место наследует новый
// дефект с той же координатой.
//
// # Гейт двусторонний, и обе стороны обязательны
//
//   - ПОЛНОТА: координата, живущая в дереве и не названная ведомостью, —
//     находка. Без этой стороны ведомость молчала бы ровно о том, что забыли;
//   - САМОИСТЕЧЕНИЕ: запись, чьей координаты в дереве больше нет, — находка.
//     Без этой стороны ведомость объявляла бы живым закрытый долг.
//
// # На ПУСТОЙ ведомости гейт ПРОХОДИТ
//
// Пустая ведомость есть ЦЕЛЬ, ради которой ведомость заведена. Отказ на ней
// подталкивал бы держать запись ради зелёного — то есть ровно к тому, что гейт
// и ловит. Способность обеих сторон упасть показана инъекцией на синтетике, а
// не тем, что когда-то падало.
//
// # Что здесь считается деревом
//
// Индекс git — то же множество, которое увидит свежий клон и CI.
package repohygiene

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const (
	// carriedLedgerPath — единственный дом ведомости.
	carriedLedgerPath = "services/iam/docs/engineering/architecture/" +
		"client-assertion-carried-over-coordinates.md"
	// carriedMirrorScope — каталог, в котором меряется зеркальная колонка.
	carriedMirrorScope = "services/iam/"
)

// carriedMirrorTokens — чем зеркальная колонка себя называет.
//
// Оба написания обязательны: колонка приходит в дерево именем схемы и именем
// поля, и место, названное только одним из них, осталось бы вне переписи.
var carriedMirrorTokens = []string{"hydra_client_id", "HydraClientID"}

// TestCarriedCoordinateLedgerExpiresOnItsOwn — сам гейт.
func TestCarriedCoordinateLedgerExpiresOnItsOwn(t *testing.T) {
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	// (1) Предпосылка: ведомость существует. Её отсутствие НЕ является пустой
	// ведомостью: пустая ведомость говорит «переносить нечего», а отсутствующая
	// не говорит ничего — и молчит она обо всех координатах сразу.
	if !tt.hasFile(carriedLedgerPath) {
		t.Fatalf("ведомости переносимых координат (%s) в составе дерева НЕТ.\n\n"+
			"Отсутствие ведомости — не пустая ведомость: пустая говорит «переносить "+
			"нечего», отсутствующая не говорит ничего, и молчит она обо всех координатах "+
			"сразу. Приёмка F2 §9.4 требует, чтобы каждая координата, дожившая до F4, "+
			"несла один исход из закрытого словаря, а исход «оставлено» — предикат снятия.",
			carriedLedgerPath)
	}
	body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(carriedLedgerPath)))
	if err != nil {
		t.Fatalf("чтение ведомости %s: %v", carriedLedgerPath, err)
	}

	rows, sections, census := ParseCarriedCoordinateLedger(string(body))

	var kept, removed, rewritten, notSubject, unknown int
	for _, r := range rows {
		switch r.Outcome {
		case CarriedOutcomeKept:
			kept++
		case CarriedOutcomeRemoved:
			removed++
		case CarriedOutcomeRewritten:
			rewritten++
		case CarriedOutcomeNotSubject:
			notSubject++
		default:
			unknown++
		}
	}

	// (2) Перепись дерева по предмету ведомости.
	mirrorFiles := carriedMirrorReaders(t, root, tt)

	t.Logf("перепись ведомости: строк документа %d, разделов %d, таблиц %d, строк таблиц %d "+
		"(без координаты %d); исходы — оставлено %d, снято %d, переписано %d, не предмет %d, "+
		"не опознано %d",
		census.Lines, census.Sections, census.Tables, census.Rows, census.RowsWithoutCoordinate,
		kept, removed, rewritten, notSubject, unknown)
	t.Logf("перепись дерева: файлов состава %d, из них читающих зеркальную колонку (%s) в %s — %d",
		tt.count(), strings.Join(carriedMirrorTokens, "|"), carriedMirrorScope, len(mirrorFiles))

	// (3) Предпосылка разбора: ведомость выражена таблицей. Документ без таблиц
	// прочитан целиком и не сказал ничего — молчание гейта было бы о разборе, а
	// не о дереве.
	if census.Tables == 0 {
		t.Fatalf("в %s (%d строк, %d разделов) не найдено НИ ОДНОЙ таблицы с колонками "+
			"координаты и исхода. Ведомость перестала быть выражена: разбор читает колонки "+
			"ПО ИМЕНИ заголовка, и документ, назвавший их иначе, для гейта пуст.",
			carriedLedgerPath, census.Lines, census.Sections)
	}

	findings := AdjudicateCarriedLedger(carriedLedgerPath, rows, sections, tt.hasFile, mirrorFiles)

	if len(findings) > 0 {
		t.Fatalf("ведомость переносимых координат разошлась с деревом — %d находка(и):\n  %s\n\n"+
			"Ведомость обязана истекать сама: запись без предмета объявляет живым закрытый "+
			"долг, а координата без записи остаётся тем самым «не заметили».",
			len(findings), strings.Join(findings, "\n  "))
	}

	// (8) Пустая ведомость — ЦЕЛЬ, и гейт на ней проходит. Сказать об этом
	// вслух обязательно: иначе «ноль находок» неотличимо от «ноль прочитанного».
	if kept == 0 {
		t.Logf("оставленных координат НОЛЬ: ведомость пуста, и это её ЦЕЛЬ. "+
			"Прочитано строк таблиц %d, из них снято %d, переписано %d, не предмет %d",
			census.Rows, removed, rewritten, notSubject)
		return
	}
	t.Logf("оставлено %d координат(ы), у каждой назван раздел с предикатом снятия; "+
		"координат зеркальной колонки в дереве %d, и все названы", kept, len(mirrorFiles))
}

// carriedMirrorReaders — файлы состава, читающие зеркальную колонку.
//
// Единица счёта — ФАЙЛ ИНДЕКСА git в дереве сервиса, без проб и без самой
// ведомости: то же множество, которым приёмка (§1.3) намеряла двенадцать. Пробы
// исключены намеренно — упоминание в пробе не является местом, которое
// переносится; ведомость — потому что прибор не считает свой выход предметом.
func carriedMirrorReaders(t *testing.T, root string, tt *trackedTree) []string {
	t.Helper()
	var out []string
	for rel := range tt.files {
		if !strings.HasPrefix(rel, carriedMirrorScope) || strings.HasSuffix(rel, "_test.go") {
			continue
		}
		if rel == carriedLedgerPath {
			// Ведомость называет зеркальную колонку в собственном предикате
			// переписи — и потому попадает в свою же перепись читателей.
			// Прибор, считающий СВОЙ ВЫХОД предметом, требует записи о самом
			// себе: замерено, а не предположено — без этой строки читателей
			// насчитывается 13 при двенадцати, и тринадцатый есть сам документ.
			continue
		}
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		text := string(body)
		for _, tok := range carriedMirrorTokens {
			if strings.Contains(text, tok) {
				out = append(out, rel)
				break
			}
		}
	}
	sort.Strings(out)
	return out
}
