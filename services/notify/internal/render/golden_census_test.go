// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// golden_census_test.go — перепись эталонов .eml сборки notify:
// NTF1-G03 (And: в HTML ни одного обращения к внешнему ресурсу — по всем
// эталонам со знаменателем) и NTF1-G18 (отправитель один, Reply-To у
// security нет; перепись печатает число эталонов).
//
// Эталон лежит по пути <пространство>/<шаблон>.<локаль>.eml; класс берётся
// из шаблона, а не из письма. Эталон, чей шаблон не найден, — находка: иначе
// он выпадал бы из суждения о классе молча.
package render

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/notify/spec"

	"github.com/PRO-Robotech/kacho/services/notify/internal/render/emlprobe"
)

// goldenCatalogs — откуда берётся шаблон эталона по пространству:
// фикстурное пространство kaname — замороженный корпус corelib по пину;
// probe — каталог сборки notify.
func goldenCatalogs(t *testing.T) map[string]map[string]spec.Template {
	t.Helper()
	out := map[string]map[string]spec.Template{"kaname": {}, "probe": {}}
	for _, fx := range []string{"invite", "optional-when", "blocks-all"} {
		tpl := corpusTemplate(t, fx)
		out["kaname"][tpl.Name] = tpl
	}
	cat, _, err := spec.Load("../../notifications")
	if err != nil {
		t.Fatalf("условие не создано: каталог сборки notify: %v", err)
	}
	for _, tpl := range cat.Templates {
		out["probe"][tpl.Name] = tpl
	}
	return out
}

type goldenEntry struct {
	path  string
	class spec.Class
	raw   []byte
}

// goldens — все эталоны; пустой обход — красный у вызывающего.
func goldens(t *testing.T) (entries []goldenEntry, findings []string) {
	t.Helper()
	cats := goldenCatalogs(t)
	if _, err := os.Stat(goldenRoot); errors.Is(err, fs.ErrNotExist) {
		return nil, nil // каталога эталонов нет — перепись пуста, вердикт у вызывающего
	}
	err := filepath.WalkDir(goldenRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".eml") {
			return nil
		}
		rel, _ := filepath.Rel(goldenRoot, p)
		ns, file := filepath.Split(rel)
		ns = strings.TrimSuffix(ns, string(filepath.Separator))
		parts := strings.Split(strings.TrimSuffix(file, ".eml"), ".")
		if len(parts) != 2 {
			findings = append(findings, fmt.Sprintf("%s: имя вне формы <шаблон>.<локаль>.eml", rel))
			return nil
		}
		tpl, ok := cats[ns][parts[0]]
		if !ok {
			findings = append(findings, fmt.Sprintf("%s: шаблона %s/%s нет ни в одном каталоге", rel, ns, parts[0]))
			return nil
		}
		raw, err := os.ReadFile(p) // #nosec G304 -- путь из обхода каталога эталонов
		if err != nil {
			return err
		}
		entries = append(entries, goldenEntry{path: rel, class: tpl.Class, raw: raw})
		return nil
	})
	if err != nil {
		t.Fatalf("обход эталонов %s: %v", goldenRoot, err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].path < entries[j].path })
	return entries, findings
}

// TestRender_NTF1G03_GoldenCensusHasNoExternalResource — по всем эталонам:
// обращений HTML не по cid: 0, элементов <style>/<link> 0. Знаменатель —
// эталоны, элементы, обращения; пустой обход — красный.
func TestRender_NTF1G03_GoldenCensusHasNoExternalResource(t *testing.T) {
	entries, findings := goldens(t)
	elements, refs := 0, 0
	for _, e := range entries {
		l, err := emlprobe.Parse(e.raw)
		if err != nil {
			findings = append(findings, fmt.Sprintf("%s: %v", e.path, err))
			continue
		}
		rs, _, styles, c, err := emlprobe.Inspect(l.HTML)
		if err != nil {
			findings = append(findings, fmt.Sprintf("%s: %v", e.path, err))
			continue
		}
		elements += c.Elements
		refs += c.Refs
		for _, x := range emlprobe.External(rs) {
			findings = append(findings, fmt.Sprintf("%s: внешнее обращение <%s %s=%q>", e.path, x.Element, x.Attr, x.Value))
		}
		if styles != 0 {
			findings = append(findings, fmt.Sprintf("%s: элементов <style>/<link> %d", e.path, styles))
		}
	}
	t.Logf("G03 перепись: эталонов %d · элементов HTML %d · обращений к ресурсам %d · находок %d",
		len(entries), elements, refs, len(findings))
	if len(entries) == 0 {
		t.Fatalf("эталонов в %s 0 — пустой обход не вердикт", goldenRoot)
	}
	for _, f := range findings {
		t.Error(f)
	}
}

// TestRender_NTF1G18_GoldenCensusSenderAndReplyTo — у каждого эталона From —
// один адрес, адрес отправителя установки; у security Reply-To нет. Перепись
// печатает число эталонов и число эталонов security (положительный контроль:
// без них «Reply-To нет у security» зеленеет на пустом).
func TestRender_NTF1G18_GoldenCensusSenderAndReplyTo(t *testing.T) {
	entries, findings := goldens(t)
	security := 0
	for _, e := range entries {
		l, err := emlprobe.Parse(e.raw)
		if err != nil {
			findings = append(findings, fmt.Sprintf("%s: %v", e.path, err))
			continue
		}
		from, err := l.Header.AddressList("From")
		switch {
		case err != nil:
			findings = append(findings, fmt.Sprintf("%s: From не разбирается: %v", e.path, err))
		case len(from) != 1 || from[0].Address != testFromAddress:
			findings = append(findings, fmt.Sprintf("%s: From %v, ожидался один адрес %s", e.path, from, testFromAddress))
		case from[0].Name != testFromName:
			findings = append(findings, fmt.Sprintf("%s: имя отправителя %q, ожидалось %q", e.path, from[0].Name, testFromName))
		}
		if len(l.Header["Sender"]) != 0 {
			findings = append(findings, fmt.Sprintf("%s: заголовок Sender — второй отправитель", e.path))
		}
		if e.class == spec.ClassSecurity {
			security++
			if v, ok := l.Header["Reply-To"]; ok {
				findings = append(findings, fmt.Sprintf("%s: у security есть Reply-To %v", e.path, v))
			}
		}
	}
	t.Logf("G18 перепись: эталонов %d · из них security %d · находок %d", len(entries), security, len(findings))
	if len(entries) == 0 || security == 0 {
		t.Fatalf("эталонов %d, security %d — перепись без предмета не вердикт", len(entries), security)
	}
	for _, f := range findings {
		t.Error(f)
	}
}
