// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// shellcheck_directives_parse_injection_test.go — доказательство того, что гейт
// разбора директив СПОСОБЕН упасть и падает ТОЛЬКО на своём предмете.
//
// ПРОГОНОВ ТРИ, И ТРЕТИЙ НЕ ФАКУЛЬТАТИВЕН:
//
//  1. распознаватель — обе стороны по каждой законной форме, снятой ЗАПУСКОМ
//     настоящего shellcheck 0.11.0 (сочинённый образец доказывал бы, что
//     предикат ловит сочинителя);
//  2. ГЕЙТ ЦЕЛИКОМ на копии дерева — инъекция роняет его и НАЗЫВАЕТ координату;
//  3. ГЕЙТ ЦЕЛИКОМ на копии с законным близнецом — молчит.
//
// Второй и третий прогоны нужны потому, что проба, зовущая распознаватель
// напрямую, закрепляет ОТВЕТ, а не МЕСТО: она осталась бы зелёной, если бы обход
// перестал доходить до файлов вовсе (`testing.md` §«Гейт на класс», п.6).
//
// ИНЪЕКЦИЯ РОНЯЕТ ТОЛЬКО ПРОВЕРЯЕМОЕ (п.2в): дефект вносится в файл, у которого
// всё остальное на месте, а не заведением нового объекта — новый объект нарушал
// бы заодно всё, что требуется от объектов вообще.
package deploy_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// scDirectiveCases — таблица форм. Колонка `fault` снята запуском shellcheck,
// а не выведена из документации.
var scDirectiveCases = []struct {
	name  string
	rest  string // всё, что стоит после слова shellcheck
	fault bool   // shellcheck отвечает SC1072/SC1073?
}{
	// ── законные близнецы: гейт обязан МОЛЧАТЬ ──────────────────────────────
	{"одно правило", "disable=SC2086", false},
	{"список правил", "disable=SC2086,SC2034", false},
	{"два ключа", "shell=bash disable=SC2086", false},
	{"значение all", "disable=all", false},
	{"источник файлом", "source=lib/deps-failure-class.sh", false},
	{"проза после решётки", "disable=SC2086 # проза", false},
	{"проза после решётки без пробела", "disable=SC2086  #проза", false},
	{"хвостовой пробел", "disable=SC2086 ", false},

	// ── инъекции: гейт обязан НАЙТИ ─────────────────────────────────────────
	{"проза после двойного тире (дефект #1770)", "disable=SC2086 -- ARGS это цепочка -f", true},
	{"проза голым словом", "disable=SC2086 проза", true},
	{"проза с двоеточием", "disable=SC2086 источник: файл", true},
	{"ключ без значения", "disable=", true},
}

func TestShellcheckDirectiveFaultKnowsEveryLegalForm(t *testing.T) {
	var twins, injections int
	for _, c := range scDirectiveCases {
		if c.fault {
			injections++
		} else {
			twins++
		}
		if got := scDirectiveFault(c.rest) != ""; got != c.fault {
			t.Errorf("%s: `# shellcheck %s` → находка=%v, ожидалось %v",
				c.name, c.rest, got, c.fault)
		}
	}
	t.Logf("перепись: входов %d (законных близнецов %d, инъекций %d)",
		len(scDirectiveCases), twins, injections)
	if twins == 0 || injections == 0 {
		t.Fatal("таблица односторонняя — доказательство ловило бы форму, а не существо")
	}
}

// scFixture — каталог под инъекцию. Правится КОПИЯ: писать в дерево, из которого
// запущен прогон, запрещено (`multi-agent-flow.md` §13).
func scFixture(t *testing.T, body string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "probe.sh"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestShellcheckDirectivesGateFailsOnTheInjectedDefect(t *testing.T) {
	const legal = "#!/usr/bin/env bash\nA=\"a b\"\n# ARGS — намеренно разбиваемая цепочка\n# shellcheck disable=SC2086\necho $A\n"
	const broken = "#!/usr/bin/env bash\nA=\"a b\"\n# shellcheck disable=SC2086 -- ARGS это намеренно разбиваемая цепочка -f\necho $A\n"

	// (1) ЗАКОННЫЙ БЛИЗНЕЦ — гейт обязан молчать, и обязан при этом ПРОЧИТАТЬ
	//     директиву: молчание на нуле прочитанного доказательством не является.
	files, directives, findings := scScan(t, scFixture(t, legal))
	if files != 1 || directives != 1 {
		t.Fatalf("законный близнец: прочитано файлов %d, директив %d — обход не дошёл до предмета", files, directives)
	}
	if len(findings) != 0 {
		t.Errorf("законный близнец объявлен находкой: %v", findings)
	}

	// (2) ИНЪЕКЦИЯ — гейт обязан найти И НАЗВАТЬ КООРДИНАТУ. Находка, называющая
	//     симптом вместо места, посылает читателя искать не там (`testing.md`
	//     §«Гейт на класс», п.8).
	files, directives, findings = scScan(t, scFixture(t, broken))
	if files != 1 || directives != 1 {
		t.Fatalf("инъекция: прочитано файлов %d, директив %d — дефект внесён не туда", files, directives)
	}
	if len(findings) != 1 {
		t.Fatalf("инъекция дала находок %d, ожидалась 1: %v", len(findings), findings)
	}
	if !strings.Contains(findings[0], "probe.sh:3") {
		t.Errorf("находка не называет координату: %s", findings[0])
	}
}
