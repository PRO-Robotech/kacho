// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package migratorapply_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/treecorpus"

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
	judged := 0
	for _, f := range notifyForms {
		p := formRows(root, f, chains)
		if len(p.findings) == 0 && len(p.rows) > 0 {
			// Форма в паре со строкой своей базы (проба, D3): её исход — накат,
			// а не «вне опроса»; судит его доказательство в манифестной форме.
			t.Logf("  форма %s · %s · в паре со строкой %s — не предмет этой пробы", f, f.origin, p.rows[0].Database)
			continue
		}
		judged++
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
	if judged == 0 {
		t.Fatal("НЕ ВЫПОЛНИЛОСЬ: форм notify вне пары 0 — предмета «вне опроса» в дереве нет, запись " +
			"outOfPollUntilRendered пережила его")
	}
}

// copyChart копирует каталог чарта src в dst. Состав — у индекса git
// (treecorpus.Under), а не с диска: распаковки зависимостей и прочее
// игнорируемое в копию не попадают.
func copyChart(t *testing.T, src, dst string) {
	t.Helper()
	files, err := treecorpus.Under(src)
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: состав чарта %s не взят у индекса: %v", src, err)
	}
	for _, abs := range files {
		rel, err := filepath.Rel(src, abs)
		if err != nil {
			t.Fatal(err)
		}
		body, err := os.ReadFile(abs) // #nosec G304 -- файл индекса дерева
		if err != nil {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: %s не прочитан: %v", abs, err)
		}
		target := filepath.Join(dst, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
