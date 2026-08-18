// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// authzcheckoutage_injection_test.go — доказательство, что гейт
// `TestAuthzCheckOutageIsNotADenial` СПОСОБЕН упасть и способен смолчать.
//
// Инъекция идёт настоящим входом — файлами на диске, которые разбирает тот же
// `ScanAuthzCheckOutage`, что и на дереве. Второго разбора нет: гейт, у которого
// свой парсер для проб и свой для дерева, проверял бы сам себя.
//
// Обе стороны обязательны. Без положительных близнецов гейт ловил бы ФОРМУ
// («рядом с Check стоит слово err»), а не существо, и первый же законный страж
// его бы покраснил — после чего гейт снимают как шумный.
package repohygiene

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// injectAuthzTree раскладывает синтетическое дерево и разбирает его.
func injectAuthzTree(t *testing.T, files map[string]string) AuthzCheckCensus {
	t.Helper()
	root := t.TempDir()
	// go.mod — чтобы дерево было похоже на настоящее; разбор его не читает, но
	// синтетика, отличимая от предмета формой, доказывает меньше.
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module synthetic\n\ngo 1.24\n"), 0o600); err != nil {
		t.Fatalf("go.mod: %v", err)
	}
	for name, body := range files {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	// Список подаётся ЯВНО — как его подал бы индекс. Синтетика репозиторием не
	// является, спрашивать у неё индекс нечего, и обход диска здесь был бы не
	// откатом, а единственным авторитетом; явный список честнее обоих.
	rels := make([]string, 0, len(files))
	for name := range files {
		rels = append(rels, name)
	}
	sort.Strings(rels)

	c, err := ScanAuthzCheckOutage(root, rels)
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	return c
}

const authzInjPreamble = `package guard

import "context"

type checker interface {
	Check(ctx context.Context, subject, relation, object string) (bool, error)
}

func denied() error { return nil }
func unavailable() error { return nil }
`

// TestAuthzCheckOutage_InjectionRed — КАЖДАЯ форма схлопывания краснеет, и гейт
// называет координату.
func TestAuthzCheckOutage_InjectionRed(t *testing.T) {
	cases := map[string]string{
		// Форма 1 — присваивание в заголовке `if`, ошибка выброшена сравнением.
		"guard/inif.go": authzInjPreamble + `
func Gate(ctx context.Context, c checker, s string) error {
	if allowed, err := c.Check(ctx, s, "admin", "account:a"); err == nil && allowed {
		return nil
	}
	return denied()
}
`,
		// Форма 2 — обычное присваивание, ошибка сравнивается с nil и забывается.
		"guard/plain.go": authzInjPreamble + `
func Gate(ctx context.Context, c checker, s string) error {
	allowed, cerr := c.Check(ctx, s, "admin", "account:a")
	if cerr == nil && allowed {
		return nil
	}
	return denied()
}
`,
		// Форма 3 — ошибка выброшена в `_` открыто.
		"guard/blank.go": authzInjPreamble + `
func Gate(ctx context.Context, c checker, s string) bool {
	allowed, _ := c.Check(ctx, s, "admin", "account:a")
	return allowed
}
`,
		// Форма 4 — возврат выражением, без отдельного `if`.
		"guard/returnexpr.go": authzInjPreamble + `
func Gate(ctx context.Context, c checker, s string) bool {
	allowed, err := c.Check(ctx, s, "admin", "account:a")
	return err == nil && allowed
}
`,
	}

	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			c := injectAuthzTree(t, map[string]string{name: body})
			if len(c.Sites) != 1 {
				t.Fatalf("распознано вызовов %d, ожидался 1 — разбор не увидел предмет", len(c.Sites))
			}
			if len(c.Findings) != 1 {
				t.Fatalf("находок %d, ожидалась 1: гейт НЕ КРАСНЕЕТ на внесённом дефекте", len(c.Findings))
			}
			f := c.Findings[0]
			if f.File != name {
				t.Errorf("находка названа координатой %q, ожидалась %q", f.File, name)
			}
			if f.Line <= 0 {
				t.Errorf("находка без строки — по такой координате искать нечего")
			}
			if strings.TrimSpace(f.Why) == "" {
				t.Errorf("находка без причины: гейт, не называющий предмет, снимут как непонятный")
			}
			t.Logf("краснеет: %s:%d — %s", f.File, f.Line, f.Why)
		})
	}
}

// TestAuthzCheckOutage_InjectionGreen — ЗАКОННЫЕ формы той же конструкции
// молчат. Без этой половины гейт ловил бы форму, а не существо.
func TestAuthzCheckOutage_InjectionGreen(t *testing.T) {
	cases := map[string]string{
		// Близнец 1 — канонический ответ: отдельная ветка на недоступность.
		"guard/canon.go": authzInjPreamble + `
func Gate(ctx context.Context, c checker, s string) error {
	allowed, err := c.Check(ctx, s, "admin", "account:a")
	if err != nil {
		return unavailable()
	}
	if !allowed {
		return denied()
	}
	return nil
}
`,
		// Близнец 2 — проброс наверх: вызывающий облекает в свой контракт.
		"guard/propagate.go": authzInjPreamble + `
func Gate(ctx context.Context, c checker, s string) (bool, error) {
	allowed, err := c.Check(ctx, s, "admin", "account:a")
	if err != nil {
		return false, err
	}
	return allowed, nil
}
`,
		// Близнец 3 — цикл по отношениям, помнящий неотвеченный вопрос.
		"guard/loop.go": authzInjPreamble + `
func Gate(ctx context.Context, c checker, s string) error {
	var unanswered bool
	for _, rel := range []string{"editor", "admin"} {
		allowed, err := c.Check(ctx, s, rel, "account:a")
		switch {
		case err != nil:
			unanswered = true
		case allowed:
			return nil
		}
	}
	if unanswered {
		return unavailable()
	}
	return denied()
}
`,
		// Близнец 4 — ошибка обёрнута, а не сопоставлена с nil.
		"guard/wrapped.go": `package guard

import (
	"context"
	"fmt"
)

type checker interface {
	Check(ctx context.Context, subject, relation, object string) (bool, error)
}

func Gate(ctx context.Context, c checker, s string) (bool, error) {
	allowed, err := c.Check(ctx, s, "admin", "account:a")
	if err != nil {
		return false, fmt.Errorf("authz check: %w", err)
	}
	return allowed, nil
}
`,
		// Близнец 5 — ДРУГАЯ подпись с тем же именем метода: три результата,
		// ошибка в третьем и употребляется честно. Гейт судит только форму
		// `RelationChecker`, и это его объявленная граница, а не послабление.
		"bench/threeresults.go": `package bench

import "context"

type former interface {
	Check(ctx context.Context, subject, verb, object string) (bool, string, error)
}

func Measure(ctx context.Context, f former, s string) error {
	before, _, err := f.Check(ctx, s, "read", "account:a")
	if err != nil {
		return err
	}
	_ = before
	return nil
}
`,
		// Близнец 6 — иная арность: проверка здоровья, не решение о правах.
		"health/health.go": `package health

import "context"

type prober interface {
	Check(ctx context.Context) error
}

func Probe(ctx context.Context, p prober) error { return p.Check(ctx) }
`,
	}

	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			c := injectAuthzTree(t, map[string]string{name: body})
			if len(c.Findings) != 0 {
				t.Fatalf("гейт покраснел на ЗАКОННОЙ форме: %s:%d — %s",
					c.Findings[0].File, c.Findings[0].Line, c.Findings[0].Why)
			}
			t.Logf("молчит: прочитано файлов %d, распознано вызовов %d", c.FilesRead, len(c.Sites))
		})
	}
}

// TestAuthzCheckOutage_ScopeIsTheBindingsOwnBlock — имя ошибки ищется в ЕЁ
// области, а не по всему телу функции.
//
// Гипотеза «достаточно поискать имя в теле функции» была опровергнута до того,
// как гейт написан: в функции, где `err` встречается часто, соседний законный
// `if err != nil` двадцатью строками ниже зачёлся бы за употребление, и гейт
// молчал бы ровно там, где схлопывание прячется удобнее всего. Опровергнутая
// гипотеза записана здесь, чтобы следующий читатель не повторил её.
func TestAuthzCheckOutage_ScopeIsTheBindingsOwnBlock(t *testing.T) {
	const body = authzInjPreamble + `
func Gate(ctx context.Context, c checker, s string) error {
	if allowed, err := c.Check(ctx, s, "admin", "account:a"); err == nil && allowed {
		return nil
	}
	// Соседний, НИЧЕМ не связанный err — законно употреблённый.
	if err := other(); err != nil {
		return err
	}
	return denied()
}

func other() error { return nil }
`
	c := injectAuthzTree(t, map[string]string{"guard/shadow.go": body})
	if len(c.Findings) != 1 {
		t.Fatalf("находок %d, ожидалась 1: соседний законный `err` погасил разбор — "+
			"значит имя ищется шире своей области, и гейт молчит там, где дефект прячется лучше всего",
			len(c.Findings))
	}
	t.Logf("краснеет несмотря на соседний законный err: %s:%d", c.Findings[0].File, c.Findings[0].Line)
}

// TestAuthzCheckOutage_EmptyTreeIsRefused — беспредметный обход не есть успех.
func TestAuthzCheckOutage_EmptyTreeIsRefused(t *testing.T) {
	c := injectAuthzTree(t, map[string]string{})
	if len(c.Sites) != 0 {
		t.Fatalf("на пустом дереве распознано %d вызовов — разбор выдумывает предмет", len(c.Sites))
	}
	// Сам гейт на этом состоянии обязан упасть предпосылкой; здесь утверждается
	// то, на чём он это решение принимает: перепись отличает «ноль находок» от
	// «ноль прочитанного».
	t.Logf("пустое дерево: прочитано файлов %d, распознано вызовов %d — гейт обязан отвергнуть такой прогон",
		c.FilesRead, len(c.Sites))
}
