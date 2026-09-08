// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// OperatorFacingScripts — файлы оснастки, КОТОРЫЕ ГОВОРЯТ С ОПЕРАТОРОМ: их текст
// читает человек в момент отказа и идёт по названной координате.
//
// Перечень ВЫПИСАН, а не выведен, и это осознанно: предикат «все скрипты дерева»
// прогонялся и провалил контроль — из 58 «несуществующих» путей подавляющее
// большинство оказалось синтетическими фикстурами самопроверок, которые
// порождаются во временном каталоге и обязаны не существовать. Гейт, у которого
// почти все находки ложные, отключают первым же коммитом.
var OperatorFacingScripts = []string{
	"scripts/hooks/pre-push",
	"scripts/hooks/install.sh",
	"scripts/hooks/prepush-groups.sh",
	"scripts/ci-local.sh",
}

// treeRoots — головы путей, по которым координата дерева отличается от имени файла
// в текущем каталоге. Относительное имя (`gen.py`, `run.sh`) под предикат НЕ
// попадает: судить, из какого каталога его запустят, гейт не вправе.
var treeRoots = []string{
	"proto/", "services/", "scripts/", "internal/", "pkg/",
	"deploy/", "tools/", "gateway/", "ui-future/", "terraform/", ".github/",
}

var treePathRe = regexp.MustCompile(
	`(?:^|[^\w/.$-])((?:` + strings.Join(escapedRoots(), "|") + `)[\w./-]*\.[a-z]{2,5})(?:[^\w/.-]|$)`)

func escapedRoots() []string {
	out := make([]string, 0, len(treeRoots))
	for _, r := range treeRoots {
		out = append(out, regexp.QuoteMeta(r))
	}
	return out
}

// PathFinding — координата, названная оператору, которой в дереве нет.
type PathFinding struct {
	Path string // что названо
	File string // где названо
}

// OperatorMessageCensus — перепись: сколько файлов прочитано, сколько путей
// названо, что из названного не существует.
type OperatorMessageCensus struct {
	FilesRead   int
	FilesAbsent []string
	PathsNamed  int
	Findings    []PathFinding
}

// CollectOperatorMessagePaths обходит перечень выше от корня дерева repoRoot и
// собирает перепись. Отсутствующий файл перечня — сам по себе находка: он
// означает, что предмет гейта переехал, а гейт об этом не знает.
func CollectOperatorMessagePaths(repoRoot string) OperatorMessageCensus {
	c := OperatorMessageCensus{}
	seen := map[string]string{}
	for _, rel := range OperatorFacingScripts {
		abs := filepath.Join(repoRoot, rel)
		body, err := os.ReadFile(filepath.Clean(abs)) // rel из выписанного перечня выше, не из запроса
		if err != nil {
			c.FilesAbsent = append(c.FilesAbsent, rel)
			continue
		}
		c.FilesRead++
		for _, m := range treePathRe.FindAllStringSubmatch(string(body), -1) {
			p := m[1]
			c.PathsNamed++
			if _, dup := seen[p]; dup {
				continue
			}
			seen[p] = rel
			// #nosec G703 -- p взят регулярным выражением из файла ОСНАСТКИ,
			// перечисленного выше, а не из запроса; проверяется существование
			// координаты в собственном дереве, ничего не читается и не пишется.
			if _, err := os.Stat(filepath.Join(repoRoot, p)); err != nil {
				c.Findings = append(c.Findings, PathFinding{Path: p, File: rel})
			}
		}
	}
	sort.Slice(c.Findings, func(i, j int) bool { return c.Findings[i].Path < c.Findings[j].Path })
	return c
}
