// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// pgoutsideselection_test.go — освобождение от переписи долга ссылкой на цель
// test-pg-outside-selection обязано указывать на пакет, который эта цель
// действительно гонит.
//
// # Предмет
//
// shortgatedselection_test.go освобождает пакет от переписи долга, если тот
// назван исполняемым СВОИМ шагом конвейера, и проверяет это строкой в ci.yaml.
// Строка там ОДНА на весь перечень (`make test-pg-outside-selection`), поэтому
// сама по себе она не отвечает, дошёл ли до прогона КОНКРЕТНЫЙ пакет: перечень
// живёт в корневом Makefile (PG_OUTSIDE_SELECTION_PKGS), а не в ci.yaml.
//
// Без этой сверки освобождение становится способом выйти из-под гейта ОДНОЙ
// строкой в списке — тем самым, от чего гейт и стоит. Ровно этот шов уже
// закрыт у соседней цели (authzfgaproofs_test.go,
// TestAuthzFGAOwnStepDeclarationsPointAtTheList); у Postgres-половины его не
// было, и первая же вторая запись в перечне (задача #495) сделала бы разрыв
// достижимым.
//
// # Почему сверка идёт в ОБЕ стороны
//
// Ссылка без пакета в перечне выдаёт непрогон за прогон. Пакет в перечне без
// ссылки — тише и потому хуже: цель его гоняет, а перепись долга продолжает
// числить его наблюдаемым не тем механизмом, и снятие пакета из перечня не
// краснеет нигде.
//
// # Перепись
//
// «Ноль находок» обязано отличаться от «ноль прочитанного»: гейт печатает,
// сколько освобождений и сколько записей перечня он сверил, и падает, когда
// сверять оказалось нечего.
package repohygiene

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// pgOutsideMakeTarget — команда, которой конвейер зовёт цель. Ровно эта строка
// стоит значением в shortGatedRunByOwnCIStep и в шаге ci.yaml.
const pgOutsideMakeTarget = "make test-pg-outside-selection"

// TestPGOutsideOwnStepDeclarationsPointAtTheList — шов с соседним гейтом.
func TestPGOutsideOwnStepDeclarationsPointAtTheList(t *testing.T) {
	root := repoRoot(t)
	// ПЕРЕЧНЕЙ ДВА, И ЧИТАТЬ НАДО ОБА.
	//
	// Служба iam несёт свой `go.mod`, поэтому её пакеты не резолвятся из корня и
	// вынесены в отдельную величину с путями ОТНОСИТЕЛЬНО каталога службы. Гейт
	// сверяет освобождения по путям ДЕРЕВА, значит вторую половину надо привести
	// к тому же виду. Читать одну половину — значит объявить вторую
	// «отсутствующей в перечне», ничего в ней не изменив: тринадцать ложных
	// находок и ни одной настоящей.
	declared := makefileListedPkgs(t, root, "PG_OUTSIDE_SELECTION_PKGS")
	rootNamed := len(declared)
	iamNamed := 0
	for _, rel := range makefileListedPkgs(t, root, "PG_OUTSIDE_SELECTION_PKGS_IAM") {
		declared = append(declared, strings.TrimSuffix(iamTreePrefix, "/")+"/"+rel)
		iamNamed++
	}
	sort.Strings(declared)
	if rootNamed == 0 || iamNamed == 0 {
		t.Fatalf("перечень пуст с одной из сторон (корень %d, модуль службы iam %d) — "+
			"сверять не с чем, а молчание такого гейта неотличимо от согласия", rootNamed, iamNamed)
	}
	for _, f := range judgePGOutsideSeam(shortGatedRunByOwnCIStep, declared) {
		t.Errorf("%s", f)
	}
	t.Logf("сверено: освобождений со ссылкой на цель — %d, записей перечня — %d "+
		"(корневой модуль %d, модуль службы iam %d)",
		countPGOutsideExemptions(shortGatedRunByOwnCIStep), len(declared), rootNamed, iamNamed)
}

// judgePGOutsideSeam — решающая часть, вынесенная из вердикта, чтобы её можно
// было проверить подставными входами (тест ниже), а не только зелёным деревом.
func judgePGOutsideSeam(exemptions map[string]string, declared []string) []string {
	inList := map[string]bool{}
	for _, p := range declared {
		inList[p] = true
	}
	exempt := map[string]bool{}
	for pkg, inv := range exemptions {
		if inv == pgOutsideMakeTarget {
			exempt[pkg] = true
		}
	}

	var findings []string
	for pkg := range exempt {
		if !inList[pkg] {
			findings = append(findings,
				"shortGatedRunByOwnCIStep освобождает "+pkg+" ссылкой на "+pgOutsideMakeTarget+
					", но в PG_OUTSIDE_SELECTION_PKGS корневого Makefile этого пакета НЕТ — "+
					"цель его не гоняет, и освобождение выдаёт непрогон за прогон")
		}
	}
	for _, pkg := range declared {
		if !exempt[pkg] {
			findings = append(findings,
				"PG_OUTSIDE_SELECTION_PKGS называет "+pkg+", но shortGatedRunByOwnCIStep на "+
					pgOutsideMakeTarget+" по нему не ссылается — пакет гоняет цель, а перепись "+
					"долга об этом не знает, и снятие его из перечня не покраснеет нигде")
		}
	}
	sort.Strings(findings)
	return findings
}

// countPGOutsideExemptions — сколько освобождений ссылается на эту цель.
func countPGOutsideExemptions(exemptions map[string]string) int {
	n := 0
	for _, inv := range exemptions {
		if inv == pgOutsideMakeTarget {
			n++
		}
	}
	return n
}

// TestPGOutsideSeamJudgeFiresAndStaysSilent — инъекция в обе стороны.
//
// Гейт выше на дереве зелёный, и зелёный сам по себе не значит ничего: ровно так
// же он выглядел бы, не умей вердикт краснеть.
func TestPGOutsideSeamJudgeFiresAndStaysSilent(t *testing.T) {
	const a = "services/nlb/internal/apps/kacho/jobs"
	const b = "services/iam/internal/apps/kaname/api/bootstrap_token"

	t.Run("согласованная пара — молчит", func(t *testing.T) {
		got := judgePGOutsideSeam(
			map[string]string{a: pgOutsideMakeTarget, b: pgOutsideMakeTarget},
			[]string{a, b})
		if len(got) != 0 {
			t.Fatalf("законный вход помечен: %v", got)
		}
	})

	t.Run("чужая цель в освобождении не считается ссылкой сюда", func(t *testing.T) {
		got := judgePGOutsideSeam(
			map[string]string{a: pgOutsideMakeTarget, "x/y": "make test-authz-fga"},
			[]string{a})
		if len(got) != 0 {
			t.Fatalf("запись чужой цели принята за свою: %v", got)
		}
	})

	t.Run("освобождение без записи в перечне — находка", func(t *testing.T) {
		got := judgePGOutsideSeam(map[string]string{a: pgOutsideMakeTarget}, []string{b})
		if len(got) != 2 { // a освобождён без перечня, b в перечне без освобождения
			t.Fatalf("ожидались обе находки, получено %d: %v", len(got), got)
		}
		if !strings.Contains(strings.Join(got, "\n"), a) {
			t.Fatalf("находка не называет виновника: %v", got)
		}
	})

	t.Run("запись перечня без освобождения — находка", func(t *testing.T) {
		got := judgePGOutsideSeam(map[string]string{a: pgOutsideMakeTarget}, []string{a, b})
		if len(got) != 1 {
			t.Fatalf("ожидалась ровно одна находка, получено %d: %v", len(got), got)
		}
		if !strings.Contains(got[0], b) {
			t.Fatalf("находка не называет виновника: %v", got[0])
		}
	})
}

// makefileListedPkgs читает перечень пакетов из присваивания <name> в корневом
// Makefile. Пути там записаны как аргументы `go test` (`./services/...`) —
// приводятся к той же форме, что у обхода дерева, иначе сверялись бы разные вещи.
//
// Общий разбор для обоих швов (этого и authzfgaproofs_test.go): у двух целей
// одна форма списка, и второй разбор разошёлся бы с первым молча.
func makefileListedPkgs(t *testing.T, root, name string) []string {
	t.Helper()
	var out []string
	for _, f := range strings.Fields(makefileAssignment(t, root, name)) {
		if p, ok := strings.CutPrefix(f, "./"); ok {
			out = append(out, strings.TrimSuffix(p, "/"))
		}
	}
	sort.Strings(out)
	return out
}

// makefileAssignment — правая часть присваивания <name>, склеенная по
// продолжениям строк. Совпадение ищется в НАЧАЛЕ строки, поэтому упоминания
// имени в комментариях (те начинаются с решётки) и в рецептах (те с табуляции)
// за присваивание не принимаются.
func makefileAssignment(t *testing.T, root, name string) string {
	t.Helper()
	var b strings.Builder
	inside := false
	for _, line := range strings.Split(rootMakefile(t, root), "\n") {
		if !inside {
			rest, ok := strings.CutPrefix(line, name)
			if !ok {
				continue
			}
			if _, after, found := strings.Cut(rest, "="); found {
				b.WriteString(after + " ")
			}
			inside = true
		} else {
			b.WriteString(line + " ")
		}
		if !strings.HasSuffix(strings.TrimRight(line, " \t"), `\`) {
			break
		}
	}
	return strings.ReplaceAll(b.String(), `\`, " ")
}

// rootMakefile — корневой Makefile этого дерева.
//
// Жил в пробе, снятой вместе со своим предметом (харнесс проб против внешнего
// движка прав, стадия S6 эпика #747). Переехал сюда, к оставшемуся читателю:
// вспомогательная функция без читателя — мёртвый код, а вспомогательная функция,
// живущая в чужом файле, переживает его снятие только случайно.
func rootMakefile(t *testing.T, root string) string {
	t.Helper()
	// #nosec G304 -- читается корневой Makefile этого же репозитория.
	raw, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatalf("не прочитан корневой Makefile: %v", err)
	}
	return string(raw)
}
