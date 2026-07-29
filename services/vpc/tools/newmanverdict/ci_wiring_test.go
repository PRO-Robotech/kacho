// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Проверка честности прогонщика newman существует — и её кто-то запускает.
//
// Половину вопроса «настоящий ли это гейт?» гейт про себя ответить не может.
// У vpc самопроверка агрегатора вердикта лежала в дереве и не вызывалась НИ ОДНИМ
// workflow, то есть ни разу не выполнялась; ровно тогда же сам вердикт читал одно
// измерение из четырёх и засчитывал зелёной коллекцию, не выполнившую ни одного
// запроса. Оба дефекта прикрывали друг друга: починка любого поодиночке оставила
// бы инвариант без энфорсмента.
//
// Поэтому проводка проверяется чтением workflow, а не доверием к комментарию,
// который утверждает, что CI это гоняет. Форма — та же, что у registry
// (services/registry/tools/auditlistfilter/ci_wiring_test.go).
package newmanverdict

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// selftestScript — путь самопроверки относительно корня репозитория.
const selftestScript = "services/vpc/tests/newman/scripts/run_selftest.sh"

// selftestInvocation — то, чем шаг её запускает.
const selftestInvocation = "scripts/run_selftest.sh"

// selftestWorkdir — рабочая директория шага (скрипт вызывается коротким путём,
// поэтому директория — часть утверждения, иначе совпадение по подстроке прошло бы
// на шаге из другого места дерева).
const selftestWorkdir = "services/vpc/tests/newman"

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

// workflowFile — тот кусок схемы GitHub Actions, который нужен этой проверке.
type workflowFile struct {
	Jobs map[string]struct {
		Steps []struct {
			Run string `yaml:"run"`
			Dir string `yaml:"working-directory"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

// TestSelftestScriptExists — у проверки проводки есть предмет. Без этого
// переименование скрипта превратило бы её в утверждение о пустоте.
func TestSelftestScriptExists(t *testing.T) {
	path := filepath.Join(repoRoot(t), selftestScript)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("%s must exist — the wiring check would otherwise assert about nothing: %v", selftestScript, err)
	}
}

// TestCIRunsTheNewmanVerdictSelftest — какой-то workflow действительно её зовёт.
func TestCICallsTheNewmanVerdictSelftest(t *testing.T) {
	dir := filepath.Join(repoRoot(t), ".github", "workflows")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read workflows: %v", err)
	}

	var (
		parsed int
		found  []string
	)
	for _, e := range entries {
		if e.IsDir() || (!strings.HasSuffix(e.Name(), ".yml") && !strings.HasSuffix(e.Name(), ".yaml")) {
			continue
		}
		body, rerr := os.ReadFile(filepath.Join(dir, e.Name()))
		if rerr != nil {
			t.Fatalf("read %s: %v", e.Name(), rerr)
		}
		var wf workflowFile
		if uerr := yaml.Unmarshal(body, &wf); uerr != nil {
			// Не всякий workflow разбирается в эту усечённую схему; пропускаем,
			// но такой файл не засчитывается осмотренным.
			continue
		}
		parsed++
		for jobName, job := range wf.Jobs {
			for _, step := range job.Steps {
				if strings.Contains(step.Run, selftestInvocation) &&
					strings.TrimSuffix(step.Dir, "/") == selftestWorkdir {
					found = append(found, e.Name()+":"+jobName)
				}
			}
		}
	}

	// «Ноль находок» обязано быть отличимо от «ноль прочитанного».
	if parsed == 0 {
		t.Fatal("no workflow parsed — the wiring check inspected nothing")
	}
	t.Logf("parsed %d workflow file(s)", parsed)
	if len(found) == 0 {
		t.Fatalf("no workflow step runs %s from %s — a runner-honesty check nothing invokes enforces nothing",
			selftestInvocation, selftestWorkdir)
	}
}
