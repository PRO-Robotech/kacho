// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// catalogwriterlock_test.go — держатель Г1: всякий прод-писатель строк
// `kacho_iam.catalog_*` берёт глобальный транзакционный замок каталога
// (приёмка `plan-confirms-what-apply-withdraws.md` §7, объём О11).
//
// Способность гейта упасть и смолчать доказана инъекцией —
// `catalogwriterlock_injection_test.go`.
package repohygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const (
	// catalogWriterScanRoot — прод-дерево, по которому идёт обход.
	//
	// Шире одного сервиса намеренно: предмет запрета — «ВСЯКИЙ прод-писатель», и
	// обход, сужённый на каталог сегодняшнего писателя, молчал бы ровно на том
	// случае, ради которого гейт заводится, — на писателе, заведённом в другом
	// месте.
	catalogWriterScanRoot = "services/"
	// catalogWriterCensusFloor — прод-файлов, ниже которого обход беспредметен.
	// Порог намеренно грубый: он ловит обвал состава, а не колебание дерева.
	catalogWriterCensusFloor = 200
)

// catalogWriteFindings — предикат находки. Тот же зовёт инъекция.
func catalogWriteFindings(sites []CatalogWriteFinding) []string {
	var out []string
	for _, s := range sites {
		out = append(out, fmt.Sprintf("%s:%d  [%s] %s — %s", s.File, s.Line, s.Unit, s.What, s.Why))
	}
	sort.Strings(out)
	return out
}

// TestIAM1034_EveryCatalogRowWriterTakesTheCatalogLock — сам гейт Г1.
func TestIAM1034_EveryCatalogRowWriterTakesTheCatalogLock(t *testing.T) {
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	var rels []string
	for rel := range tt.files {
		if !strings.HasPrefix(rel, catalogWriterScanRoot) {
			continue
		}
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			continue
		}
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	var sources []CatalogSource
	for _, rel := range rels {
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("прочитать %s: %v — непрочитанное есть НАХОДКА, а не пропуск", rel, err)
		}
		sources = append(sources, CatalogSource{Path: rel, Src: src})
	}

	findings, census, err := ScanCatalogWriteLocking(sources)
	if err != nil {
		t.Fatalf("разбор прод-дерева %s: %v", catalogWriterScanRoot, err)
	}

	t.Logf("перепись: прод-файлов под %s подано %d, разобрано %d · функций %d, из них "+
		"исполнителей %d · строковых литералов %d, комментариев %d · ТЕКСТОМ совпало "+
		"операторов записи %d в %d файле(ах) · ИСПОЛНЯЕТСЯ из них %d в %d файле(ах) · "+
		"пишущих единиц %d, из них запирающих %d · мест взятия замка %d · находок %d",
		catalogWriterScanRoot, census.Files, census.Parsed, census.Funcs, census.Executors,
		census.StringLiterals, census.Comments,
		census.TextMatches, census.TextFiles, census.Executed, census.ExecutingFiles,
		census.WriteUnits, census.LockedUnits, census.LockSites, len(findings))

	// ── Предпосылка 1: обход вообще состоялся ────────────────────────────────
	if census.Parsed < catalogWriterCensusFloor {
		t.Fatalf("перепись обвалилась: разобрано %d прод-файлов при пороге %d — обход "+
			"читает не то дерево, и «ноль находок» тут означало бы «ноль прочитанного»",
			census.Parsed, catalogWriterCensusFloor)
	}

	// ── Предпосылка 2 (П43): исполняющий писатель в дереве ЕСТЬ ──────────────
	//
	// Ноль исполняемых операторов означает, что судить нечего: писатель переехал,
	// сменил форму записи запроса либо снят вместе с предметом. Молчание гейта в
	// этом состоянии было бы сказано ни о чём — ровно тот класс, который сам
	// гейт и стережёт.
	if census.Executed == 0 {
		t.Fatalf("в прод-дереве %s НЕ НАЙДЕНО ни одного исполняемого оператора записи "+
			"строк каталога (текстом совпало %d в %d файле(ах), разобрано %d файлов) — "+
			"писатель переехал либо сменил форму запроса, и гейт стережёт предмет, "+
			"которого больше нет. Это отказ, а не молчаливый успех.",
			catalogWriterScanRoot, census.TextMatches, census.TextFiles, census.Parsed)
	}

	// ── Предпосылка 3 (П1): различение «разбор против исполнения» имеет предмет ─
	//
	// Тот же оператор лежит текстом в `internal/check/catalog_seed_parity.go`,
	// который его РАЗБИРАЕТ и не исполняет. Пока текстовых совпадений строго
	// больше исполняемых, различение по узлу разбора что-то различает; сравнялись
	// — значит гейт, судящий подстроку, дал бы сегодня тот же ответ, и об этом
	// надо знать, а не узнать на первом ложном срабатывании.
	if census.TextMatches <= census.Executed {
		t.Logf("ВНИМАНИЕ: текстовых совпадений %d при %d исполняемых — сегодня различение "+
			"«разбор против исполнения» не различает ничего. Гейт от этого не становится "+
			"неверным, но законный близнец (разбор текста миграции) в живом дереве "+
			"исчез, и его держит теперь только инъекция.",
			census.TextMatches, census.Executed)
	}

	// ── Предпосылка 4 (П1): сегодняшний писатель замок БЕРЁТ ─────────────────
	if census.LockSites == 0 {
		t.Fatalf("в прод-дереве %s не найдено НИ ОДНОГО взятия консультативного замка "+
			"при %d исполняемых операторах записи каталога — либо замок снят целиком, "+
			"либо разбор перестал его опознавать; в обоих случаях подтверждение "+
			"применения перестало быть CAS", catalogWriterScanRoot, census.Executed)
	}

	if f := catalogWriteFindings(findings); len(f) > 0 {
		t.Fatalf("строки каталога пишет %d единица(ы), не запирающая каталог:\n  %s\n\n"+
			"Подтверждение применения (отпечаток состояния модуля) есть CAS ТОЛЬКО потому, "+
			"что между чтением отпечатка и записью строк не может встать второй писатель. "+
			"Обеспечивает это `pg_advisory_xact_lock(hashtext(%s))`, взятый ПЕРВЫМ "+
			"оператором транзакции, — а не само сравнение: под конкуренцией сравнение "+
			"сравнивало бы состояние, которое сосед меняет прямо сейчас, и «совпало» "+
			"означало бы «совпало на момент чтения», то есть ничего.\n"+
			"Свойство истекает МОЛЧА: отпечаток продолжит сравниваться, пробы останутся "+
			"зелёными (они гоняют один применитель против одной базы), и «CAS» станет "+
			"формой без содержания.\n"+
			"Приёмка: services/iam/docs/engineering/acceptance/"+
			"plan-confirms-what-apply-withdraws.md §7, держатель Г1 (kacho#1034)",
			len(f), strings.Join(f, "\n  "), catalogLockKeyIdent)
	}
}
