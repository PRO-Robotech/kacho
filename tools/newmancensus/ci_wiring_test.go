// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package newmancensus держит половину вопроса «настоящий ли этот гейт?», на которую
// сам гейт ответить не может: зовёт ли его кто-нибудь.
//
// Сверщики переписи кейсов (`services/*/tests/newman/scripts/validate-cases.py`) лежали
// у ШЕСТИ сюит и не запускались ни одним шагом конвейера: у одной была цель в Makefile,
// которую никто не звал, у остальных — только файл. У compute сверщика не было вовсе, и
// именно там выжили раздел документа про УДАЛЁННЫЙ файл кейсов и перепись, разошедшаяся
// с деревом (111 против 117). Гейт, которого никто не исполняет, стоит ровно столько же,
// сколько гейт, который ничего не проверяет.
//
// Поэтому проводка проверяется ЧТЕНИЕМ workflow, а не доверием к комментарию, который
// утверждает, что CI это гоняет.
package newmancensus

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
	"gopkg.in/yaml.v3"
)

// suiteGlob — образец, которым шаг конвейера находит сверщиков. Он же проверяется на
// покрытие: каждый существующий в дереве сверщик обязан этому образцу соответствовать,
// иначе шаг «обходит дерево», а на деле кого-то не видит.
const suiteGlob = "services/*/tests/newman/scripts/validate-cases.py"

func repoRoot(t *testing.T) string {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(self)
	for range 12 {
		if fi, err := os.Stat(filepath.Join(dir, ".github", "workflows")); err == nil && fi.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("could not find .github/workflows above this test file")
	return ""
}

type workflowFile struct {
	Jobs map[string]struct {
		Steps []struct {
			Name string `yaml:"name"`
			Run  string `yaml:"run"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

// censusSteps — шаги всех workflow, которые зовут сверщика переписи.
func censusSteps(t *testing.T, root string) []string {
	t.Helper()
	wfDir := filepath.Join(root, ".github", "workflows")
	entries, err := os.ReadDir(wfDir)
	if err != nil {
		t.Fatalf("read %s: %v", wfDir, err)
	}
	var out []string
	files := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || (!strings.HasSuffix(name, ".yml") && !strings.HasSuffix(name, ".yaml")) {
			continue
		}
		files++
		raw, rerr := os.ReadFile(filepath.Join(wfDir, name))
		if rerr != nil {
			t.Fatalf("read %s: %v", name, rerr)
		}
		var wf workflowFile
		if uerr := yaml.Unmarshal(raw, &wf); uerr != nil {
			t.Fatalf("parse %s: %v", name, uerr)
		}
		for _, job := range wf.Jobs {
			for _, step := range job.Steps {
				if strings.Contains(step.Run, "validate-cases.py") {
					out = append(out, step.Run)
				}
			}
		}
	}
	if files == 0 {
		t.Fatal("ни одного файла workflow не прочитано — обход ничего не нашёл, " +
			"значит и утверждать ему нечего")
	}
	t.Logf("прочитано файлов workflow: %d; шагов, зовущих сверщика: %d", files, len(out))
	return out
}

// TestCIRunsEveryCaseCensusValidator — конвейер зовёт сверщиков, находит их ОБХОДОМ и
// доносит их отказ до своего кода выхода.
func TestCIRunsEveryCaseCensusValidator(t *testing.T) {
	root := repoRoot(t)
	steps := censusSteps(t, root)
	if len(steps) == 0 {
		t.Fatal("ни один шаг конвейера не зовёт validate-cases.py: сверщики переписи " +
			"есть у сюит и не исполняются — гейт без исполнителя равен отсутствию гейта")
	}

	var wired string
	for _, s := range steps {
		if strings.Contains(s, suiteGlob) {
			wired = s
			break
		}
	}
	if wired == "" {
		t.Fatalf("шаг зовёт validate-cases.py, но не обходом %q — значит сюиты перечислены "+
			"рукой, и следующая появится вне гейта молча", suiteGlob)
	}

	// Отказ сверщика обязан доезжать до вердикта шага. Без этого шаг «выполняется» и
	// всегда зелёный — тот же класс, что прогонщик, печатающий GREEN при красном.
	if !strings.Contains(wired, "fail=1") {
		t.Error("шаг не накапливает отказ (`|| fail=1`): падение сверщика не дойдёт до вердикта")
	}
	if !strings.Contains(wired, `exit "$fail"`) && !strings.Contains(wired, "exit $fail") {
		t.Error("шаг не выходит накопленным кодом: отказ сверщика останется без последствий")
	}
	// «Ноль найденных» обязано быть отличимо от «всё чисто».
	if !strings.Contains(wired, "found") {
		t.Error("шаг не проверяет, что сверщики вообще нашлись: пустой обход отчитался бы успехом")
	}
}

// TestEverySuiteValidatorIsCoveredByTheGlob — образец шага покрывает КАЖДЫЙ сверщик,
// который есть в дереве. Иначе «обход» — форма без содержания: он существует, а кого-то
// не видит, и невидимая сюита выглядит проверенной.
func TestEverySuiteValidatorIsCoveredByTheGlob(t *testing.T) {
	root := repoRoot(t)
	// ОБА состава — из индекса git, и это условие сопоставимости, а не стиль.
	// Взяв один с диска, а другой из индекса, сверка расходилась бы на всяком
	// неотслеживаемом файле: сверщик, положенный в рабочую копию и не
	// закоммиченный, читался бы как «есть в дереве, но не покрыт образцом» —
	// находка о рабочем каталоге, а не о коммите.
	found, err := treecorpus.Glob(filepath.Join(root, "services", "*", "tests", "newman",
		"scripts", "validate-cases.py"))
	if err != nil {
		t.Fatalf("перечень по образцу шага: %v", err)
	}
	// Перечисление НЕЗАВИСИМО от образца: если сверщик лежит не там, где его ищет шаг,
	// это находка, а не «его нет».
	tracked, walkErr := treecorpus.Under(filepath.Join(root, "services"))
	if walkErr != nil {
		t.Fatalf("состав services/: %v", walkErr)
	}
	var all []string
	for _, p := range tracked {
		if filepath.Base(p) == "validate-cases.py" {
			all = append(all, p)
		}
	}
	if len(all) == 0 {
		t.Fatal("в дереве не найдено ни одного validate-cases.py — тест ничего не утверждает")
	}
	t.Logf("сверщиков в дереве: %d; покрыто образцом шага: %d", len(all), len(found))

	covered := map[string]bool{}
	for _, f := range found {
		covered[f] = true
	}
	for _, f := range all {
		if !covered[f] {
			rel, _ := filepath.Rel(root, f)
			t.Errorf("сверщик %s лежит вне образца %q — шаг конвейера его не увидит, "+
				"а сюита будет выглядеть проверенной", rel, suiteGlob)
		}
	}
}
