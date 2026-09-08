// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// securityscanmodulereach_test.go — гейт безопасности обязан читать КАЖДЫЙ
// модуль Go этого дерева, а не тот один, из корня которого его позвали.
//
// ПРЕДМЕТ, И ОН НЕ ГИПОТЕТИЧЕСКИЙ. `gosec ./...` — множество пакетов ТЕКУЩЕГО
// модуля: во вложенный модуль (свой go.mod) сборщик не спускается by
// construction. Пока модуль в дереве был один, прямой вызов из корня был верен.
// Со вторым (2026-09-05) половина дерева вышла из-под гейта — и гейт продолжал
// светиться зелёным, потому что «ноль находок» и «ноль прочитанного» на выходе
// выглядят одинаково. Форма без содержания: проверка присутствует, зелена и не
// осматривает предмета.
//
// ПОЧЕМУ ГЕЙТ, А НЕ КОММЕНТАРИЙ. Отсутствующая половина дерева ничем себя не
// проявляет: прогон быстрее, лог короче, вердикт тот же. Заметить это можно
// только сверив перечень осмотренного с деревом — то есть механизмом, а не
// внимательностью. Прежняя редакция объявления прямо утверждала обратное
// («сканеры читают дерево целиком»), и это утверждение пережило свой предмет.
//
// ЧТО ПРОВЕРЯЕТСЯ (две половины, порознь каждая обходится):
//
//	A. ПЕРЕЧЕНЬ ВЫВОДИТСЯ ИЗ ДЕРЕВА. Скрипт скана, спрошенный о своём перечне
//	   модулей, обязан назвать ровно те каталоги, где индекс git видит go.mod.
//	   Это поведенческая проверка, а не текстовая: выписанный внутри скрипта
//	   список сегодня совпал бы с деревом, но разошёлся бы в тот день, когда
//	   заводится следующий модуль, — и покраснеет ровно тогда.
//	B. ПЕРЕЧЕНЬ КТО-ТО ЗОВЁТ. Скрипт может быть безупречен и не вызван — тогда
//	   свойство есть у файла, а не у гейта. Поэтому объявление процесса обязано
//	   звать и перепись, и вердикт, и не вправе звать сканер напрямую по
//	   множеству пакетов: такой вызов вернул бы слепую зону в обход переписи.
package repohygiene

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/PRO-Robotech/kacho/pkg/gitenv"
)

// securityScanWorkflow — объявление, которое судит половина B.
const securityScanWorkflow = ".github/workflows/security-scan.yml"

// censusScript, verdictScript — единственное определение того, чем сканируется
// дерево. Гейт требует ИМЕННО их, а не «какого-нибудь скрипта»: перечень
// модулей выводится из индекса git, и это свойство конкретного скрипта, а не
// всякого.
const (
	censusScript  = "scripts/gosec-scan-modules.sh"
	verdictScript = "scripts/gosec-verdict.sh"
)

// securityScanWiring — то, ЧЕМ процесс сканирует Go-дерево.
//
// Перепись здесь не украшение: «ни одного прямого вызова» обязано быть отличимо
// от «ни одного блока run не прочитано». Поэтому объём осмотренного — часть
// результата, а не строка в логе.
type securityScanWiring struct {
	Jobs      int // заданий прочитано
	RunBlocks int // блоков `run:` прочитано

	CensusCalls  []string // шаги, зовущие перепись модулей
	VerdictCalls []string // шаги, зовущие вердикт
	DirectGosec  []string // шаги, зовущие сам сканер по множеству пакетов
}

// wiringDoc — то немногое из объявления процесса, что нужно этому гейту.
type wiringDoc struct {
	Jobs map[string]struct {
		Steps []struct {
			Name string `yaml:"name"`
			Run  string `yaml:"run"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

// auditSecurityScanWiring — разбирает объявление процесса и отвечает, чем он
// сканирует Go-дерево.
//
// Судит ИСПОЛНЯЕМУЮ часть: разбор про то, почему прямой вызов запрещён, сам
// содержит слова этого вызова, и проверка по сырому тексту краснела бы на
// собственном объяснении. Комментарии оболочки снимаются общим для дерева
// `stripShellComments`.
func auditSecurityScanWiring(doc []byte) (securityScanWiring, error) {
	var parsed wiringDoc
	if err := yaml.Unmarshal(doc, &parsed); err != nil {
		return securityScanWiring{}, fmt.Errorf("объявление не разбирается: %w", err)
	}

	w := securityScanWiring{}
	names := make([]string, 0, len(parsed.Jobs))
	for jobName := range parsed.Jobs {
		names = append(names, jobName)
	}
	sort.Strings(names) // порядок находок не должен зависеть от обхода карты

	for _, jobName := range names {
		job := parsed.Jobs[jobName]
		w.Jobs++
		for i, step := range job.Steps {
			if strings.TrimSpace(step.Run) == "" {
				continue
			}
			w.RunBlocks++

			where := step.Name
			if where == "" {
				where = fmt.Sprintf("шаг #%d", i+1)
			}
			where = jobName + " / " + where

			code := stripShellComments(step.Run)
			// Проверки независимы: один шаг вправе звать оба скрипта, и
			// взаимоисключающая развилка не заметила бы второго.
			if strings.Contains(code, censusScript) {
				w.CensusCalls = append(w.CensusCalls, where)
			}
			if strings.Contains(code, verdictScript) {
				w.VerdictCalls = append(w.VerdictCalls, where)
			}
			// Прямой вызов сканера по множеству пакетов. Установка пина
			// (`go install …/gosec@vX`) под это НЕ подпадает: она не несёт
			// `./...` и ничего не сканирует.
			if strings.Contains(code, "gosec") && strings.Contains(code, "./...") {
				w.DirectGosec = append(w.DirectGosec, where)
			}
		}
	}
	return w, nil
}

// treeGoModules — каталоги модулей по ИНДЕКСУ git.
//
// Индекс, а не диск: тогда объявление, .gitignore и поведение не могут
// разъехаться молча, а порождённые в рабочем каталоге чужие go.mod (кеши,
// распакованные зависимости) в счёт не идут.
func treeGoModules(t *testing.T, root string) []string {
	t.Helper()
	// Через помощник, а не напрямую: `cmd.Dir` НЕ выбирает репозиторий, когда в
	// окружении есть GIT_DIR — переменная сильнее рабочего каталога. Прямой
	// вызов читал бы состав ЧУЖОГО дерева, и перепись молча стала бы о нём.
	out, err := gitenv.Command(root, "ls-files", "*go.mod").Output()
	if err != nil {
		t.Fatalf("перечень модулей по индексу git не снят: %v", err)
	}
	seen := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		seen[filepath.Dir(line)] = true
	}
	dirs := make([]string, 0, len(seen))
	for d := range seen {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	return dirs
}

// TestGosecScanListsEveryGoModuleOfTheTree — половина A: перечень выводится.
func TestGosecScanListsEveryGoModuleOfTheTree(t *testing.T) {
	root := repoRoot(t)

	want := treeGoModules(t, root)
	// «Ноль расхождений» обязано быть отличимо от «ноль прочитанного»: на пустом
	// дереве сравнение двух пустых множеств прошло бы, ничего не проверив.
	t.Logf("перепись: модулей Go по индексу git — %d (%s)", len(want), strings.Join(want, " "))
	if len(want) == 0 {
		t.Fatal("индекс git не дал ни одного go.mod — обход пуст, судить не о чем")
	}

	script := filepath.Join(root, censusScript)
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("скрипта скана нет (%s): %v — перечень модулей выводить нечем", censusScript, err)
	}

	cmd := exec.Command("bash", script, "--list-modules")
	cmd.Dir = root
	// Скрипт внутри спрашивает git о корне дерева, а GIT_DIR в окружении сильнее
	// рабочего каталога: без снятия этих переменных перечень пришёл бы из ЧУЖОГО
	// репозитория, и проба молча стала бы утверждать о нём.
	cmd.Env = gitenv.Env()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s --list-modules завершился с ошибкой: %v\n%s", censusScript, err, out)
	}
	var got []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			got = append(got, line)
		}
	}
	sort.Strings(got)
	t.Logf("перепись: модулей назвал скрипт скана — %d (%s)", len(got), strings.Join(got, " "))

	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("перечень модулей скана разошёлся с деревом.\n"+
			"  дерево : %s\n  скан   : %s\n\n"+
			"Модуль, не попавший под скан, гейтом не осматривается вовсе, а зелёное по "+
			"нему означает «не смотрели», а не «чисто». Перечень обязан ВЫВОДИТЬСЯ из "+
			"индекса git (git ls-files '*go.mod'), а не выписываться: выписанный "+
			"расходится с деревом молча.",
			strings.Join(want, " "), strings.Join(got, " "))
	}
}

// TestSecurityScanCallsTheModuleCensus — половина B: перечень кто-то зовёт.
func TestSecurityScanCallsTheModuleCensus(t *testing.T) {
	root := repoRoot(t)

	body, err := os.ReadFile(filepath.Join(root, securityScanWorkflow))
	if err != nil {
		t.Fatalf("не прочитано объявление процесса %s: %v", securityScanWorkflow, err)
	}
	w, err := auditSecurityScanWiring(body)
	if err != nil {
		t.Fatalf("%s: %v — файл НЕ проверен, и это не чистота", securityScanWorkflow, err)
	}

	t.Logf("перепись: заданий %d, блоков run %d; зовут перепись модулей %d, "+
		"зовут вердикт %d, зовут сканер напрямую %d",
		w.Jobs, w.RunBlocks, len(w.CensusCalls), len(w.VerdictCalls), len(w.DirectGosec))

	if w.RunBlocks == 0 {
		t.Fatal("в объявлении не прочитано ни одного блока run — гейту нечего было " +
			"судить, и это отказ, а не чистота")
	}

	if len(w.CensusCalls) == 0 {
		t.Errorf("ни один шаг не зовёт %s. Скан, идущий мимо переписи модулей, "+
			"осматривает один модуль из %d — и молчит про остальные.",
			censusScript, len(treeGoModules(t, root)))
	}
	if len(w.VerdictCalls) == 0 {
		t.Errorf("ни один шаг не зовёт %s. Без него перепись снимается и никем не "+
			"читается: непрочитанный модуль не роняет прогон, а «ноль находок» "+
			"остаётся неотличимым от «ноль прочитанного».", verdictScript)
	}
	if len(w.DirectGosec) > 0 {
		t.Errorf("сканер зовут напрямую по множеству пакетов: %s\n\n"+
			"`./...` — пакеты ТЕКУЩЕГО модуля; во вложенный модуль сборщик не "+
			"спускается by construction, поэтому такой вызов возвращает слепую зону "+
			"в обход переписи. Зови %s.",
			strings.Join(w.DirectGosec, ", "), censusScript)
	}
}
