// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package migratorapply_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/migrationchains"
)

// Д91 — форма чарта notify (dbname kacho_notify), пока строки kacho_notify в
// таблице цепочек нет, — не третья категория: исход утверждается РЕНДЕРОМ.
// Чарт не рендерит ни одного объекта — форма вне опроса, печатается отдельной
// строкой и в знаменатель не входит; это зелёный по названному объёму.
//
// Инъекция — копия осмотра: чарт notify во временном каталоге, в таблице
// источников которого одна строка (форма таблицы N02, как у проб чарта D1), →
// носитель формы рендерится (либо рендер отказывает) → красный с каталогом
// чарта. Близнец — чарт дерева → строка «вне опроса».
func TestNotifyFormIsNotAThirdCategory(t *testing.T) {
	root := repoRoot(t)
	forms, _ := manifestForms(t, root)
	notifyForms := uniqueForms(forms["notify"])
	if len(notifyForms) == 0 {
		t.Fatal("НЕ ВЫПОЛНИЛОСЬ: формы вызова точки notify в манифестах нет")
	}
	var chains []migrationchains.Chain
	for _, c := range treeChains(t, root) {
		if c.Service == "notify" {
			chains = append(chains, c)
		}
	}
	for _, f := range notifyForms {
		p := formRows(root, f, chains)
		if len(p.findings) != 0 || !strings.Contains(p.outOfPoll, "его не рендерит") ||
			!strings.Contains(p.outOfPoll, "вне опроса") {
			t.Errorf("форма %s (%s): ожидалась строка «вне опроса», утверждённая рендером; получено %+v",
				f, f.origin, p)
			continue
		}
		t.Logf("  форма %s · %s · %s", f, f.origin, p.outOfPoll)

		copyRoot := t.TempDir()
		chart := chartOfForm("notify", f)
		copyChart(t, filepath.Join(root, filepath.FromSlash(chart.dir)),
			filepath.Join(copyRoot, filepath.FromSlash(chart.dir)))
		srcTpl := filepath.Join(copyRoot, filepath.FromSlash(chart.dir), "templates", "_sources.tpl")
		body, err := os.ReadFile(srcTpl) // #nosec G304 -- копия чарта во временном каталоге
		if err != nil {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: таблица источников копии не прочитана: %v", err)
		}
		const empty = `{{- list | toJson -}}`
		if strings.Count(string(body), empty) != 1 {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: тело пустой таблицы %q в копии не единственное — инъекции не на чем стоять", empty)
		}
		fixture := `{{- list (dict "module" "probe-b" "feedAddr" "probe-b:9091" ` +
			`"san" "spiffe://kacho.cloud/ns/kacho/sa/probe-b" "classes" (list "notice") ` +
			`"recipientForms" (list) "authorization" "resolveSend") | toJson -}}`
		if err := os.WriteFile(srcTpl, []byte(strings.Replace(string(body), empty, fixture, 1)), 0o600); err != nil {
			t.Fatal(err)
		}
		inj := formRowsRendered(copyRoot, f, chains, func(dir string) (int, error) {
			return helmRender(dir)
		})
		// dbname выводится из values.yaml копии — тот же, что у дерева.
		if inj.outOfPoll != "" || len(inj.findings) != 1 || !strings.Contains(inj.findings[0], chart.dir) {
			t.Fatalf("инъекция «таблица источников непуста»: ожидался один красный с каталогом чарта; получено %+v", inj)
		}
		t.Logf("инъекция «таблица источников непуста» → красный: %s", inj.findings[0])
	}
}

// copyChart копирует каталог чарта src в dst.
func copyChart(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d os.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o750)
		}
		body, rerr := os.ReadFile(p) // #nosec G304 -- чарт дерева
		if rerr != nil {
			return rerr
		}
		return os.WriteFile(target, body, 0o600)
	})
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: копия чарта %s не снята: %v", src, err)
	}
}
