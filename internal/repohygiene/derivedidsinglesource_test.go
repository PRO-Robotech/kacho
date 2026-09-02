// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// derivedidsinglesource_test.go — деривация детерминированного идентификатора
// объявлена в прод-дереве РОВНО ОДИН РАЗ (держатель Г1 приёмки
// `services/iam/docs/engineering/acceptance/roles-come-as-data-not-migrations.md`
// §3.3; сценарии MOD-RD-10 и MOD-RD-11).
//
// Способность гейта упасть и смолчать доказана инъекцией —
// `derivedidsinglesource_injection_test.go`.
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
	// derivedIDOwner — единственный дом деривации.
	derivedIDOwner = "services/iam/internal/domain/derived_id.go"
	// derivedIDCensusFloor — порог переписи: ниже него «ноль находок» означало бы
	// «ноль прочитанного».
	derivedIDCensusFloor = 1000
)

// derivedIDWalkable — что гейт вообще осматривает. Вынесено функцией, а не
// оставлено в теле обхода: инъекция обязана проверять ТОТ ЖЕ отбор, которым
// судит гейт, — отбор, переписанный на стороне пробы, остаётся зелёным, когда
// гейт отбирает иначе.
func derivedIDWalkable(rel string) bool {
	if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
		return false
	}
	return !skipPath(rel)
}

// derivedIDFindings — находки по осмотренным местам. Тот же предикат зовёт
// инъекция.
func derivedIDFindings(sites []DerivedIDImportSite) []string {
	var out []string
	for _, s := range sites {
		if s.File == derivedIDOwner {
			continue
		}
		out = append(out, fmt.Sprintf("%s:%d  (импорт %s, форма %s)",
			s.File, s.Line, derivedIDPackage, s.Form))
	}
	sort.Strings(out)
	return out
}

// TestDeterministicIDDerivationIsDeclaredOnce — сам гейт.
func TestDeterministicIDDerivationIsDeclaredOnce(t *testing.T) {
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	var rels []string
	for rel := range tt.files {
		if !derivedIDWalkable(rel) {
			continue
		}
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	var (
		parsed, imports int
		sites           []DerivedIDImportSite
	)
	for _, rel := range rels {
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		s, census, err := ScanDerivedIDDeclarations(rel, src)
		if err != nil {
			t.Fatalf("разбор %s: %v", rel, err)
		}
		parsed++
		imports += census.Imports
		sites = append(sites, s...)
	}

	byForm := map[string]int{}
	for _, s := range sites {
		byForm[s.Form]++
	}
	t.Logf("перепись: не-тестовых файлов Go разобрано %d, объявлений импорта прочитано %d, "+
		"импортов %s найдено %d (по формам: %v)",
		parsed, imports, derivedIDPackage, len(sites), byForm)

	if parsed < derivedIDCensusFloor {
		t.Fatalf("перепись обвалилась: разобрано %d файлов при пороге %d — на таком объёме "+
			"«ноль находок» означало бы «ноль прочитанного»", parsed, derivedIDCensusFloor)
	}
	if imports == 0 {
		t.Fatalf("прочитано ноль объявлений импорта на %d файлах — разбор перестал видеть "+
			"предмет, и его молчание сказано ни о чём", parsed)
	}

	// (1) Предпосылка: деривация вообще ЕСТЬ. Ноль означает, что дома у формулы
	// нет — и гейт молчал бы одинаково и тогда, когда предмет исчез, и тогда,
	// когда сломался он сам.
	if len(sites) == 0 {
		t.Fatalf("объявлений деривации (%s) в прод-дереве НОЛЬ — формулы нет ни в одном "+
			"месте, а идентификаторы применённых миграций ею адресованы. Гейт беспредметен.",
			derivedIDPackage)
	}

	// (2) Находка: объявление ВНЕ единственного дома.
	findings := derivedIDFindings(sites)
	if len(findings) > 0 {
		t.Fatalf("деривация детерминированного идентификатора объявлена ВНЕ %s — %d место(а):\n  %s\n\n"+
			"Две копии одной формулы расходятся МОЛЧА: обе отвечают «идентификатор вычислен», "+
			"полученное значение остаётся синтаксически верным и перестаёт находить строку. "+
			"Наблюдаемо это только по отказу в доступе у арендатора, у которого право не отзывали.\n"+
			"Снятие: звать `domain.DerivedIDSuffix`, а не писать формулу своей рукой.",
			derivedIDOwner, len(findings), strings.Join(findings, "\n  "))
	}

	// (3) Дом обязан существовать: перечень выше пуст и тогда, когда дом
	// переименовали, — и тогда гейт молчит, ничего не удержав.
	var owned int
	for _, s := range sites {
		if s.File == derivedIDOwner {
			owned++
		}
	}
	if owned == 0 {
		t.Fatalf("дом деривации %s в прод-дереве не найден, а находок нет — значит формула "+
			"переехала, и гейт стережёт координату, которой больше нет", derivedIDOwner)
	}
	t.Logf("единственное объявление деривации: %s (%d импорт(ов))", derivedIDOwner, owned)
}
