// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// sources_test.go — таблица источников сборки против индекса дерева: каждый
// каталог шаблонов службы (`services/<служба>/notifications`) назван строкой
// [Sources], и каждая строка называет каталог, у которого в индексе есть
// файлы. Шаг сборки дерево не перечисляет — он читает названное; поэтому
// «шаблон есть, а в сборку не попал» держит эта проба.
//
// Состав — индекс git (treecorpus), а не диск: вердикт — свойство коммита.
package assemble

import (
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/treecorpus"
)

// serviceTemplateFiles — образец файлов шаблонов служб от корня: каталог
// шаблонов службы, в нём каталог шаблона, в нём файл.
const serviceTemplateFiles = "services/*/notifications/*/*"

// sourceFindings — суд таблицы над множеством файлов шаблонов служб (пути от
// корня, через «/»): каталог вне таблицы и строка без файлов.
func sourceFindings(files []string, sources []Source) (catalogs []string, findings []string) {
	seen := map[string]bool{}
	for _, f := range files {
		parts := strings.Split(f, "/")
		if len(parts) != 5 {
			continue
		}
		seen[path.Join(parts[0], parts[1], parts[2])] = true
	}
	listed := map[string]bool{}
	for _, s := range sources {
		listed[path.Clean(s.Dir)] = true
	}
	for c := range seen {
		catalogs = append(catalogs, c)
		if !listed[c] {
			findings = append(findings, fmt.Sprintf("каталог шаблонов %s не назван таблицей источников сборки (assemble.Sources)", c))
		}
	}
	for _, s := range sources {
		if !seen[path.Clean(s.Dir)] {
			findings = append(findings, fmt.Sprintf("строка таблицы %s → %s: в индексе нет ни одного файла шаблона", s.Namespace, s.Dir))
		}
	}
	sort.Strings(catalogs)
	sort.Strings(findings)
	return catalogs, findings
}

func TestSources_EveryTemplateCatalogOfTheIndexIsBuilt(t *testing.T) {
	root := repoRoot(t)
	abs, err := treecorpus.Glob(filepath.Join(root, filepath.FromSlash(serviceTemplateFiles)))
	if err != nil {
		t.Fatalf("состав %s по индексу: %v", serviceTemplateFiles, err)
	}
	rel := make([]string, 0, len(abs))
	for _, p := range abs {
		r, err := filepath.Rel(root, p)
		if err != nil {
			t.Fatalf("%v", err)
		}
		rel = append(rel, filepath.ToSlash(r))
	}
	catalogs, findings := sourceFindings(rel, Sources())
	t.Logf("перепись: файлов шаблонов служб в индексе %d · каталогов шаблонов %d %v · строк таблицы %d · находок %d",
		len(rel), len(catalogs), catalogs, len(Sources()), len(findings))
	if len(catalogs) == 0 {
		t.Fatalf("в индексе 0 каталогов шаблонов служб — пустой обход не вердикт (образец %s)", serviceTemplateFiles)
	}
	for _, f := range findings {
		t.Error(f)
	}
}

// TestSources_JudgeCutsBothWays — суд таблицы на синтетике: каждая порча
// меняет один факт против близнеца.
func TestSources_JudgeCutsBothWays(t *testing.T) {
	probe := Source{Namespace: "notify-probe", Dir: "services/notify/notifications"}
	twin := []string{"services/notify/notifications/probe-hello/a.yaml"}
	if _, f := sourceFindings(twin, []Source{probe}); len(f) != 0 {
		t.Fatalf("близнец: находки %v", f)
	}
	cases := []struct {
		name    string
		files   []string
		sources []Source
		want    string
	}{
		{"каталог службы вне таблицы", append(twin, "services/vpc/notifications/x/a.yaml"), []Source{probe}, "services/vpc/notifications не назван"},
		{"строка без файлов", twin, []Source{probe, {Namespace: "vpc", Dir: "services/vpc/notifications"}}, "vpc → services/vpc/notifications"},
	}
	for _, tc := range cases {
		_, f := sourceFindings(tc.files, tc.sources)
		if len(f) != 1 || !strings.Contains(f[0], tc.want) {
			t.Errorf("%s: находки %v, ждали одну с %q", tc.name, f, tc.want)
		}
	}
}
