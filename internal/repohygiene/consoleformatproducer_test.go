// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// consoleformatproducer_test.go — объявленная пакетом консоли проверка формата
// обязана иметь производителя.
//
// # Предмет
//
// Девять пакетов консоли объявляют скрипт `format:check` (`prettier . --check`),
// у всех девяти одинаковая настройка `.prettierrc`, и у всех она работает. Не
// работало ОДНО: этот скрипт не звал никто (задача #1653).
//
//	производителей `format:check` в дереве .......... 0
//	контроль: производителей `typecheck` ............ 7
//
// Следствие названо и измерено, а не предположено: на стволе расхождение формата
// накопилось в 22 файлах СЕМИ пакетов из девяти, и ни один вердикт этого не
// говорил. Обнаружил бы его тот, кто позвал команду руками, — то есть никто.
//
// # Почему это гейт, а не «починили файлы и разошлись»
//
// Правка 22 файлов закрывает экземпляр; класс закрывает производитель. Но и
// производитель сам по себе стареет: он перечисляет пакеты ПОИМЁННО (иначе
// `npm run` их не обойдёт), а десятый пакет консоли, заведённый завтра со своим
// `format:check`, в этот перечень не попадёт — и снова окажется вне наблюдения,
// ничем не покраснев. Ровно так сегодня устроен соседний корневой скрипт
// `typecheck`: одиннадцать `--prefix` подряд, и никто не сверяет их с деревом.
//
// Поэтому гейт сверяет ДВЕ стороны шва:
//
//	пакет объявил format:check   →   корневой скрипт его зовёт   →   конвейер зовёт корневой скрипт
//
// Перечень пакетов ВЫВОДИТСЯ обходом `ui-future/*/package.json`, а не
// выписывается: выписанный разошёлся бы с деревом молча — тот самый класс, ради
// которого гейт стоит.
//
// # Обратная сторона шва
//
// Сверка идёт в обе стороны намеренно. Пакет, объявивший проверку и не названный
// производителем, выдаёт непроверку за проверку. Обратное — корневой скрипт
// зовёт пакет, у которого объявления нет — тише и потому хуже: `npm run` на
// несуществующем скрипте отвечает отказом, и падать это будет не там, где
// сломано.
//
// # Чего гейт НЕ проверяет — названо, чтобы его не читали шире
//
// Он судит НАЛИЧИЕ производителя, а не СОГЛАСИЕ судей: перечень вызовов и версия
// форматтера — разные свойства одного скрипта. Согласие держит соседний
// consoleformatterversion_test.go (#1674) — он требует, чтобы версия была
// объявлена ТОЧНО и одинаково всеми и чтобы замок разрешал ровно объявленное.
// До его заведения судей в дереве было три (3.8.3 · 3.9.4 · 3.9.6), и «формат
// зелёный» означало «каждый пакет согласен со СВОИМ судьёй», а не «в дереве один
// формат».
//
// # Перепись
//
// «Ноль находок» обязано отличаться от «ноль прочитанного»: гейт печатает,
// сколько пакетов осмотрел, сколько из них объявляют проверку и сколько названо
// производителем. Пустой обход — провал.
package repohygiene

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// consoleFormatScript — имя объявляемого пакетом скрипта проверки формата.
const consoleFormatScript = "format:check"

// consoleFormatCIInvocation — строка, которой конвейер зовёт корневой скрипт.
const consoleFormatCIInvocation = "npm run format:check"

// consolePrefixCallRe — вызов `npm run <скрипт> --prefix <пакет>` в корневом
// скрипте. Читается ИМЯ ПАКЕТА, а не порядок слов: цепочку пишут вручную, и
// перестановка аргументов не должна выводить пакет из-под наблюдения.
//
// Общий на все гейты консоли, читающие корневые цепочки (`format:check`,
// `typecheck`): вторая копия того же предиката разошлась бы с первой молча.
var consolePrefixCallRe = regexp.MustCompile(`--prefix\s+([A-Za-z0-9._-]+)`)

// TestEveryConsoleFormatCheckHasAProducer — вердикт на настоящем дереве.
func TestEveryConsoleFormatCheckHasAProducer(t *testing.T) {
	root := repoRoot(t)
	uiRoot := filepath.Join(root, "ui-future")

	declaring, scanned := consolePkgsDeclaringFormatCheck(t, uiRoot)
	if scanned == 0 {
		t.Fatal("обход ui-future не прочитал НИ ОДНОГО package.json — перепись " +
			"беспредметна, а молчание такого гейта неотличимо от согласия")
	}
	if len(declaring) == 0 {
		t.Fatal("ни один пакет консоли не объявляет " + consoleFormatScript + " — " +
			"либо обход сломан, либо проверку сняли, не сняв этот гейт")
	}

	rootScript := consoleRootScript(t, uiRoot, consoleFormatScript)
	if rootScript == "" {
		t.Errorf("корневой ui-future/package.json не объявляет скрипт %q — у проверки "+
			"формата нет производителя вовсе: пакеты её объявляют, а зовёт её никто",
			consoleFormatScript)
	}

	var called []string
	for _, m := range consolePrefixCallRe.FindAllStringSubmatch(rootScript, -1) {
		called = append(called, m[1])
	}
	for _, f := range judgeConsoleFormatProducers(declaring, called) {
		t.Errorf("%s", f)
	}

	if wf := consoleUIWorkflow(t, root); !strings.Contains(wf, consoleFormatCIInvocation) {
		t.Errorf("ui.yml не содержит %q — корневой скрипт есть, а конвейер его не зовёт: "+
			"проверка объявлена дважды и не исполняется ни разу", consoleFormatCIInvocation)
	}

	t.Logf("перепись: пакетов осмотрено — %d, объявляют %s — %d, названо производителем — %d",
		scanned, consoleFormatScript, len(declaring), len(called))
}

// judgeConsoleFormatProducers — решающая часть, вынесенная из вердикта, чтобы её
// можно было проверить подставными входами, а не только зелёным деревом.
func judgeConsoleFormatProducers(declaring, called []string) []string {
	isCalled := map[string]bool{}
	for _, p := range called {
		isCalled[p] = true
	}
	declares := map[string]bool{}
	for _, p := range declaring {
		declares[p] = true
	}

	var findings []string
	for _, p := range declaring {
		if !isCalled[p] {
			findings = append(findings,
				"пакет ui-future/"+p+" объявляет "+consoleFormatScript+", но корневой скрипт "+
					"его не зовёт — проверка объявлена и не исполняется, а её краснота "+
					"обнаружится только тем, кто позовёт команду руками")
		}
	}
	for _, p := range called {
		if !declares[p] {
			findings = append(findings,
				"корневой скрипт зовёт "+consoleFormatScript+" у ui-future/"+p+
					", но такого скрипта пакет не объявляет — вызов отвалится отказом npm, "+
					"и падать это будет не там, где сломано")
		}
	}
	sort.Strings(findings)
	return findings
}

// consolePkgsDeclaringFormatCheck — обход дерева: кто объявляет проверку.
// Возвращает и число ОСМОТРЕННЫХ пакетов, чтобы «ноль объявивших» было отличимо
// от «ноль прочитанных».
func consolePkgsDeclaringFormatCheck(t *testing.T, uiRoot string) (declaring []string, scanned int) {
	t.Helper()
	entries, err := os.ReadDir(uiRoot)
	if err != nil {
		t.Fatalf("не прочитан каталог консоли: %v", err)
	}
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "node_modules" {
			continue
		}
		manifest := filepath.Join(uiRoot, e.Name(), "package.json")
		// #nosec G304 -- читается манифест пакета этого же репозитория.
		raw, readErr := os.ReadFile(manifest)
		if readErr != nil {
			continue
		}
		scanned++
		var pkg struct {
			Scripts map[string]string `json:"scripts"`
		}
		if json.Unmarshal(raw, &pkg) != nil {
			t.Fatalf("не разобран %s", manifest)
		}
		if _, ok := pkg.Scripts[consoleFormatScript]; ok {
			declaring = append(declaring, e.Name())
		}
	}
	sort.Strings(declaring)
	return declaring, scanned
}

// consoleRootScript — тело названного скрипта корневого манифеста консоли.
func consoleRootScript(t *testing.T, uiRoot, name string) string {
	t.Helper()
	// #nosec G304 -- читается корневой манифест консоли этого же репозитория.
	raw, err := os.ReadFile(filepath.Join(uiRoot, "package.json"))
	if err != nil {
		t.Fatalf("не прочитан корневой манифест консоли: %v", err)
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if json.Unmarshal(raw, &pkg) != nil {
		t.Fatal("не разобран корневой манифест консоли")
	}
	return pkg.Scripts[name]
}

// consoleUIWorkflow — процесс конвейера, отвечающий за консоль.
func consoleUIWorkflow(t *testing.T, root string) string {
	t.Helper()
	// #nosec G304 -- читается объявление процесса этого же репозитория.
	raw, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "ui.yml"))
	if err != nil {
		t.Fatalf("не прочитан ui.yml: %v", err)
	}
	return string(raw)
}
