// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// workflowrunsubject_test.go — прогон, запущенный завершением другого прогона,
// обязан работать с ТЕМ коммитом, из-за которого запустился.
//
// ПРЕДМЕТ И ПОЧЕМУ ЭТО ЛОВУШКА. У события `workflow_run` контекст указывает на
// ВЕТКУ ПО УМОЛЧАНИЮ, а не на коммит вышестоящего прогона: `GITHUB_SHA` — последний
// коммит ветки по умолчанию, `GITHUB_REF` — она же. Это документированное поведение
// провайдера, и оно беззвучное: checkout без явного `ref` возьмёт ветку по
// умолчанию, шаги пройдут, теги отрендерятся, артефакт уедет — и будет собран НЕ ИЗ
// ТОГО состояния, ради покрытия которого триггер и добавляли.
//
// То есть дефект «артефакт собирается не из проверяемого состояния» возвращается в
// новой маске И ВЫГЛЯДИТ ИСПРАВЛЕННЫМ: триггер на месте, прогон зелёный, артефакт
// есть. Отличить можно только заглянув, какой коммит выкачан, — чего никто не делает.
//
// Ровно поэтому запрет живёт гейтом, а не комментарием в одном файле: следующий
// workflow, добавленный по образцу соседа, унаследует то же умолчание.
//
// ЧТО ПРОВЕРЯЕТСЯ (обе половины нужны, порознь каждая обходится):
//
//	A. Есть `workflow_run` → файл ОБЯЗАН упоминать `workflow_run.head_sha`. Без него
//	   у прогона вообще нет способа назвать проверяемый коммит.
//	B. Есть `workflow_run` → КАЖДЫЙ шаг `actions/checkout` обязан задавать `ref`.
//	   Упоминание head_sha где-то в файле (в теге, в имени) не спасает: выкачивается
//	   то, что сказано checkout'у, а не то, что напечатано в логе.
//
// Гейт НЕ трогает workflow без `workflow_run`: у push/PR-прогонов контекст и так
// указывает на нужный коммит, и требовать там явный `ref` значило бы краснеть на
// законном — первый же ложный срабат такой гейт и снимет.
package repohygiene

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const workflowsDir = ".github/workflows"

// workflowDoc — то немногое из workflow, что нужно этому гейту.
type workflowDoc struct {
	On    map[string]any `yaml:"on"`
	OnAlt map[string]any `yaml:"true"` // YAML 1.1 читает `on:` как булево
	Jobs  map[string]struct {
		Steps []struct {
			Uses string         `yaml:"uses"`
			With map[string]any `yaml:"with"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

func (d workflowDoc) triggers() map[string]any {
	if len(d.On) > 0 {
		return d.On
	}
	return d.OnAlt
}

// listWorkflows — все файлы workflow дерева.
func listWorkflows(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	dir := filepath.Join(root, workflowsDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("не прочитан %s: %v — предпосылка гейта не выполняется", workflowsDir, err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if ext := filepath.Ext(e.Name()); ext != ".yml" && ext != ".yaml" {
			continue
		}
		out = append(out, filepath.ToSlash(filepath.Join(workflowsDir, e.Name())))
	}
	sort.Strings(out)
	return out
}

// checkWorkflowRunSubject — находки одного файла. Вынесено отдельно, чтобы обход
// можно было доказать инъекцией на синтетическом содержимом, не трогая дерево.
func checkWorkflowRunSubject(path, raw string) []string {
	var doc workflowDoc
	if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
		return []string{path + ": не разобран YAML: " + err.Error() + " — файл НЕ проверен"}
	}
	if _, ok := doc.triggers()["workflow_run"]; !ok {
		return nil // не наш класс: контекст push/PR и так про нужный коммит
	}

	var findings []string

	// (A) Нечем назвать проверяемый коммит.
	if !strings.Contains(raw, "workflow_run.head_sha") {
		findings = append(findings, path+": есть триггер workflow_run, но нигде не упомянут "+
			"`github.event.workflow_run.head_sha` — у прогона нет способа назвать коммит, из-за "+
			"которого он запустился, и он будет работать с веткой по умолчанию")
	}

	// (B) checkout без явного ref → выкачается ветка по умолчанию.
	for name, job := range doc.Jobs {
		for i, st := range job.Steps {
			if !strings.HasPrefix(st.Uses, "actions/checkout") {
				continue
			}
			ref, _ := st.With["ref"].(string)
			if strings.TrimSpace(ref) == "" {
				findings = append(findings, path+": job "+name+", шаг #"+itoa(i+1)+
					" — `actions/checkout` без `with.ref`. У прогона по workflow_run это выкачает "+
					"ВЕТКУ ПО УМОЛЧАНИЮ, а не проверяемый коммит: прогон будет зелёным, артефакт "+
					"соберётся, и он будет собран не из того состояния. Задай "+
					"`ref: ${{ github.event.workflow_run.head_sha }}` (или вывод шага, который его резолвит)")
			}
		}
	}
	sort.Strings(findings)
	return findings
}

// TestWorkflowRunUsesTheSubjectCommit — по дереву.
func TestWorkflowRunUsesTheSubjectCommit(t *testing.T) {
	root := repoRoot(t)
	files := listWorkflows(t, root)

	// Перепись — ОТДЕЛЬНОЕ утверждение: обход, переставший находить workflow,
	// выходит зелёным на пустом множестве.
	if len(files) == 0 {
		t.Fatalf("в %s не найдено ни одного workflow — обход сломан, а не дерево чисто", workflowsDir)
	}

	withTrigger := 0
	for _, f := range files {
		raw, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			t.Errorf("%s не прочитан: %v — файл НЕ проверен", f, err)
			continue
		}
		var doc workflowDoc
		if err := yaml.Unmarshal(raw, &doc); err == nil {
			if _, ok := doc.triggers()["workflow_run"]; ok {
				withTrigger++
			}
		}
		for _, msg := range checkWorkflowRunSubject(f, string(raw)) {
			t.Error(msg)
		}
	}
	t.Logf("осмотрено workflow: %d, из них с триггером workflow_run: %d", len(files), withTrigger)
}

// TestWorkflowRunSubjectDetectorSeesBothForms — инъекция в обе стороны.
func TestWorkflowRunSubjectDetectorSeesBothForms(t *testing.T) {
	const okRef = "${{ github.event.workflow_run.head_sha }}"

	cases := []struct {
		name    string
		yaml    string
		wantHit bool
	}{
		{
			name: "workflow_run + checkout без ref — находка",
			yaml: "on:\n  workflow_run:\n    workflows: [x]\njobs:\n  b:\n    steps:\n" +
				"      - uses: actions/checkout@v4\n",
			wantHit: true,
		},
		{
			name: "workflow_run + checkout с ref из head_sha — молчит",
			yaml: "on:\n  workflow_run:\n    workflows: [x]\njobs:\n  b:\n    steps:\n" +
				"      - uses: actions/checkout@v4\n        with:\n          ref: '" + okRef + "'\n",
			wantHit: false,
		},
		{
			name: "workflow_run, head_sha упомянут, но checkout без ref — всё равно находка",
			yaml: "on:\n  workflow_run:\n    workflows: [x]\njobs:\n  b:\n    steps:\n" +
				"      - run: echo '" + okRef + "'\n      - uses: actions/checkout@v4\n",
			wantHit: true,
		},
		{
			name: "БЕЗ workflow_run и без ref — законно, молчит",
			yaml: "on:\n  push:\n    branches: [main]\njobs:\n  b:\n    steps:\n" +
				"      - uses: actions/checkout@v4\n",
			wantHit: false,
		},
		{
			name: "workflow_run без head_sha вовсе — находка про отсутствие предмета",
			yaml: "on:\n  workflow_run:\n    workflows: [x]\njobs:\n  b:\n    steps:\n" +
				"      - uses: actions/checkout@v4\n        with:\n          ref: main\n",
			wantHit: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := checkWorkflowRunSubject("probe.yml", c.yaml)
			if c.wantHit && len(got) == 0 {
				t.Fatal("ожидалась находка, предикат промолчал")
			}
			if !c.wantHit && len(got) != 0 {
				t.Fatalf("ожидалось молчание, предикат нашёл: %v", got)
			}
		})
	}
}
