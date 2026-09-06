// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identitywirekeys_test.go — пространство имён личности объявлено ОДИН раз, и
// обе стороны провода берут имена оттуда (приёмка KAN-WIRE-1, сценарии
// KAN-W2-05 / KAN-W2-06, предмет `ПР-2`).
//
// Разбор, формы записи и границы распознавателя — в шапке
// `identitywirekeys.go`; здесь не пересказываются.
//
// # Гейт держит ДВА утверждения, и второе — несущее
//
//  1. имя объявлено только в пакете-объявлении;
//  2. ключ, помеченный каталогом читаемым у фундамента, у фундамента ЗАВЕДЁН, и
//     наоборот. Именно это утверждение краснеет, когда ключ внесли в одно
//     объявление и не внесли в другое, — и оно называет ОБА файла.
//
// # Почему обход идёт по ВСЕМУ дереву, включая вторую сборку
//
// Служба собирается отдельным модулем и резолвит фундамент опубликованной
// версией — то есть её код МОГ БЫ завести своё написание, ничего не сломав в
// сборке. Разбор читает исходники, а не собирает их, поэтому вторая сборка
// осматривается наравне с первой.
//
// # Почему пробы тоже
//
// Второе объявление, найденное в дереве при заведении этого гейта, лежало
// именно в пробе — и уже разошлось с первым на одном ключе из трёх. Проба,
// ведущая свой перечень имён провода, есть то же второе место об одном
// предмете: она зеленеет на имени, которого не бывает.
package repohygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/principalwire"
)

// identityWireCensusFloor — порог переписи: ниже него «ноль находок» означало
// бы «ноль прочитанного».
const identityWireCensusFloor = 1000

// TestIdentityWireNamespaceIsDeclaredOnce — сам гейт.
func TestIdentityWireNamespaceIsDeclaredOnce(t *testing.T) {
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	var rels []string
	for rel := range tt.files {
		if !strings.HasSuffix(rel, ".go") || skipPath(rel) {
			continue
		}
		// Сгенерённые стабы контрактов руками не правятся и объявлением имени
		// провода стать не могут.
		if strings.HasPrefix(rel, "pkg/api/") {
			continue
		}
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	var (
		parsed    int
		census    IdentityWireCensus
		owner     []IdentityWireDeclaration
		elsewhere []string
		bound     = map[string][]IdentityWireBinding{}
	)
	for _, rel := range rels {
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		decls, bindings, c, perr := ScanIdentityWireDeclarations(rel, src)
		if perr != nil {
			// Несобирающийся исходник — предмет сборки, а не этот.
			continue
		}
		parsed++
		census.Specs += c.Specs
		census.Literals += c.Literals
		census.Selectors += c.Selectors
		census.Imports += c.Imports
		census.DotImports += c.DotImports

		inOwner := strings.HasPrefix(rel, IdentityWireOwnerDir)
		for _, d := range decls {
			if inOwner {
				owner = append(owner, d)
				continue
			}
			elsewhere = append(elsewhere, fmt.Sprintf("%s:%d  %s = %q  (%s)",
				d.File, d.Line, orAnonymous(d.Const), d.Value, wireKindName(d.Kind)))
		}
		if strings.HasPrefix(rel, IdentityWireFundamentDir) {
			for _, b := range bindings {
				bound[b.Ident] = append(bound[b.Ident], b)
			}
		}
	}

	t.Logf("перепись: файлов Go разобрано %d, объявлений осмотрено %d, строковых литералов в них %d, "+
		"обращений к пакету-объявлению %d, импортов %d, точечных импортов владельца %d; "+
		"объявлений у владельца (%s) %d, вне его %d; ключей каталога связано фундаментом (%s) %d",
		parsed, census.Specs, census.Literals, census.Selectors, census.Imports, census.DotImports,
		IdentityWireOwnerDir, len(owner), len(elsewhere), IdentityWireFundamentDir, len(bound))

	if parsed < identityWireCensusFloor {
		t.Fatalf("перепись обвалилась: разобрано %d файлов при пороге %d — на таком объёме "+
			"«ноль находок» означало бы «ноль прочитанного»", parsed, identityWireCensusFloor)
	}
	if census.Literals == 0 {
		t.Fatalf("осмотрено ноль строковых литералов в объявлениях на %d файлах — разбор "+
			"перестал видеть предмет, и его молчание сказано ни о чём", parsed)
	}

	// (1) Имя объявлено только у владельца.
	sort.Strings(elsewhere)
	if len(elsewhere) > 0 {
		t.Errorf("имя из пространства личности объявлено ВНЕ единственного объявления — "+
			"%d место(а):\n  %s\n\n"+
			"Второе объявление одного имени расходится с первым МОЛЧА: переименование одной "+
			"стороны собирается чисто, а приёмник, не найдя своих ключей, читает это как "+
			"«личности нет» и идёт дальше — то есть рассинхрон даёт не отказ, а ПОТЕРЮ "+
			"личности.\nСнятие: брать имя у %s, а не писать его своей рукой.",
			len(elsewhere), strings.Join(elsewhere, "\n  "), IdentityWireOwnerDir)
	}

	// (2) Каталог и фундамент согласны — в ОБЕ стороны.
	var missing, extra []string
	catalogue := map[string]principalwire.Key{}
	for _, k := range principalwire.Keys() {
		if k.Ident != "" {
			catalogue[k.Ident] = k
		}
		if !k.Fundament {
			continue
		}
		if k.Ident == "" {
			t.Errorf("каталог: ключ %q помечен читаемым у фундамента, а имени константы не "+
				"назвал — привязку не с чем сверить", k.Name)
			continue
		}
		if len(bound[k.Ident]) == 0 {
			missing = append(missing, fmt.Sprintf("%q (%skeys.go %s)", k.Name, IdentityWireOwnerDir, k.Ident))
		}
	}
	for ident, bs := range bound {
		k, known := catalogue[ident]
		if !known {
			// Обращение не к ключу каталога (приставка, пространство, предикат) —
			// не предмет этого утверждения.
			continue
		}
		if !k.Fundament {
			for _, b := range bs {
				extra = append(extra, fmt.Sprintf("%s:%d %s → %q", b.File, b.Line, b.Const, k.Name))
			}
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)

	if len(missing) > 0 {
		t.Errorf("каталог (%skeys.go) объявил ключ читаемым у фундамента, а у фундамента "+
			"(%s) он НЕ заведён — %d: %s\n\n"+
			"Это и есть рассинхрон двух объявлений, только на шаг раньше провода: ключ внесён в "+
			"одно место и не внесён в другое, сборка чиста, а личность под этим ключом до "+
			"обработчика не доедет.\nСнятие: завести константу у фундамента ЛИБО снять пометку "+
			"Fundament, если ключ остаётся делом края.",
			IdentityWireOwnerDir, IdentityWireFundamentDir, len(missing), strings.Join(missing, ", "))
	}
	if len(extra) > 0 {
		t.Errorf("фундамент (%s) завёл ключ, который каталог читаемым у себя НЕ объявлял — "+
			"%d место(а):\n  %s\n\nКаталог перестал описывать провод: снятие — пометить ключ "+
			"Fundament в %skeys.go либо убрать константу.",
			IdentityWireFundamentDir, len(extra), strings.Join(extra, "\n  "), IdentityWireOwnerDir)
	}

	// Предпосылка — ПОСЛЕ находок и ровно затем, зачем заведена: отличить
	// «прочитал и не нашёл» от «искать стало нечего».
	if len(owner) == 0 {
		t.Fatalf("у владельца %s не осталось ни одного объявления имени — гейт беспредметен: "+
			"он молчит и тогда, когда пространство имён личности перестало существовать",
			IdentityWireOwnerDir)
	}
	if len(bound) == 0 {
		t.Fatalf("фундамент %s не связан с каталогом ни одним ключом — второе утверждение "+
			"гейта беспредметно: расхождение сверять не с чем", IdentityWireFundamentDir)
	}
}

// wireKindName — имя вида для отказа. Число вида в тексте отказа ничего не
// говорит читателю.
func wireKindName(k principalwire.WireNameKind) string {
	switch k {
	case principalwire.WireNameNamespace:
		return "пространство имён"
	case principalwire.WireNameFamily:
		return "приставка подсемейства"
	case principalwire.WireNameKey:
		return "ключ личности"
	default:
		return "не относится к пространству"
	}
}

// orAnonymous — имя объявления либо пометка о безымянном элементе составного
// значения: координата без имени читается хуже, но врать про имя нельзя.
func orAnonymous(name string) string {
	if name == "" {
		return "<элемент составного значения>"
	}
	return name
}
