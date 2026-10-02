// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"path"
	"sort"
	"strings"

	"github.com/PRO-Robotech/corelib/treecorpus"

	"github.com/PRO-Robotech/kacho/internal/migrationchains"
)

// chainsOfComposition — цепочки миграций состава rels (пути от root) для
// распознавателей, которым состав отдают готовым (индекс git либо обход
// синтетического дерева), — вместо признака «путь несёт /internal/migrations/»
// (kacho#2915, CX1-114).
//
// Состав с точками наката судится migrationchains.FromTree. Синтетический
// состав без единой точки наката — фикстура инъекции, чьи цепочки объявлены
// путём, который она кладёт (`services/<svc>/internal/migrations`): точка
// наката предметом такой пробы не является. Состав настоящего дерева точки
// несёт всегда, и эта ветка на нём не исполняется.
func chainsOfComposition(root string, rels []string) ([]migrationchains.Chain, error) {
	sorted := append([]string(nil), rels...)
	sort.Strings(sorted)
	points := false
	layout := map[string]bool{}
	for _, rel := range sorted {
		if m, _ := path.Match("services/*/cmd/migrator/main.go", rel); m {
			points = true
		}
		if m, _ := path.Match("services/*/internal/migrations/*.sql", rel); m {
			layout[path.Dir(rel)] = true
		}
	}
	if points {
		return migrationchains.FromTree(treecorpus.ParseIndex(root, []byte(strings.Join(sorted, "\x00"))))
	}
	dirs := make([]string, 0, len(layout))
	for d := range layout {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	out := make([]migrationchains.Chain, 0, len(dirs))
	for _, d := range dirs {
		svc := strings.Split(d, "/")[1]
		out = append(out, migrationchains.Chain{Service: svc, Dir: d})
	}
	return out, nil
}
