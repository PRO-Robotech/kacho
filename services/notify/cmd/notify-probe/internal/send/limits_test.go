// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package send_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/notify/spec"
)

// sentTemplate — шаблон, который ставит Send (notify.SendProbeHello).
const sentTemplate = "probe-hello"

// catalogDir — каталог шаблонов владельца от каталога этого пакета.
const catalogDir = "../../../../notifications"

// limitedSentTemplates — шаблоны из sent, у которых в каталоге dir объявлены
// limits, и число прочитанных шаблонов каталога (знаменатель). Шаблона из
// sent в каталоге нет — тоже находка: судить было бы нечего.
func limitedSentTemplates(t *testing.T, dir string, sent ...string) ([]string, int) {
	t.Helper()
	cat, census, err := spec.Load(dir)
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: каталог шаблонов %s не прочитан: %v", dir, err)
	}
	byName := map[string]spec.Template{}
	for _, tm := range cat.Templates {
		byName[tm.Name] = tm
	}
	var out []string
	for _, name := range sent {
		tm, ok := byName[name]
		switch {
		case !ok:
			out = append(out, "шаблона "+name+" в каталоге нет — Send ставит то, чего каталог не объявляет")
		case len(tm.Limits) > 0:
			out = append(out, "шаблон "+name+" объявляет limits: постановка с лимитами обновляет счётчик "+
				"окна (INSERT … ON CONFLICT DO UPDATE), и под REPEATABLE READ параллельные Send "+
				"получили бы 40001 (serialization failure) — повтора транзакции у Send нет")
		}
	}
	return out, census.Templates
}

// RR-SERIALIZATION-LATENT — транзакция Send открывается уровнем REPEATABLE
// READ (см. шапку send.go), и это безопасно, пока постановка ничего не читает
// для записи: у шаблона без limits feed.Put только вставляет строку ленты и
// сигнал. Limits у шаблона пробы сделали бы 40001 возможным, а повтора у Send
// нет — проба держит предпосылку. Инъекция — копия каталога, где у шаблона
// пробы объявлен limit, → красный с именем шаблона. Близнец — каталог дерева.
func TestSentTemplateDeclaresNoLimits(t *testing.T) {
	findings, read := limitedSentTemplates(t, catalogDir, sentTemplate)
	t.Logf("шаблонов каталога прочитано %d; судимых (ставит Send) 1: %s", read, sentTemplate)
	if read == 0 {
		t.Fatal("НЕ ВЫПОЛНИЛОСЬ: в каталоге шаблонов ноль шаблонов — судить нечего")
	}
	for _, f := range findings {
		t.Errorf("%s", f)
	}

	copyDir := filepath.Join(t.TempDir(), "notifications")
	src := filepath.Join(catalogDir, sentTemplate)
	dst := filepath.Join(copyDir, sentTemplate)
	if err := os.MkdirAll(dst, 0o750); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: каталог шаблона %s не прочитан: %v", src, err)
	}
	edited := false
	for _, e := range entries {
		body, err := os.ReadFile(filepath.Join(src, e.Name())) // #nosec G304 -- каталог шаблонов дерева
		if err != nil {
			t.Fatal(err)
		}
		if e.Name() == "notification.yaml" {
			const anchor = "\nttl: 1h\n"
			if strings.Count(string(body), anchor) != 1 {
				t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: якорь инъекции %q в notification.yaml не единственный", anchor)
			}
			body = []byte(strings.Replace(string(body), anchor,
				anchor+"limits:\n  - {scope: recipient, window: 1h, max: 3}\n", 1))
			edited = true
		}
		if err := os.WriteFile(filepath.Join(dst, e.Name()), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if !edited {
		t.Fatal("НЕ ВЫПОЛНИЛОСЬ: notification.yaml шаблона пробы не найден — инъекции не на чем стоять")
	}
	inj, _ := limitedSentTemplates(t, copyDir, sentTemplate)
	if len(inj) != 1 || !strings.Contains(inj[0], sentTemplate) || !strings.Contains(inj[0], "40001") {
		t.Fatalf("инъекция «limit у шаблона пробы»: ожидалась одна находка с именем шаблона, получено %v", inj)
	}
	t.Logf("инъекция «limit у шаблона пробы» → красный: %s", inj[0])
}
