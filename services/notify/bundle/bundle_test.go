// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// bundle_test.go — отказы New над каталогом сборки (короткие пробы): каждый
// отказ — порча ровно одного факта против встроенного каталога как есть,
// близнец — тот же каталог без порчи.
package bundle

import (
	"io/fs"
	"path"
	"sort"
	"strings"
	"testing"
	"testing/fstest"
)

// embeddedCopy — встроенный каталог сборки копией в памяти; пустой —
// ФИКСТУРА.
func embeddedCopy(t *testing.T) fstest.MapFS {
	t.Helper()
	m := fstest.MapFS{}
	err := fs.WalkDir(catalogFS, catalogRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(catalogFS, p)
		if err != nil {
			return err
		}
		m[p] = &fstest.MapFile{Data: data}
		return nil
	})
	if err != nil {
		t.Fatalf("ФИКСТУРА: обход встроенного каталога: %v", err)
	}
	if len(m) == 0 {
		t.Fatalf("ФИКСТУРА: встроенный каталог сборки пуст")
	}
	return m
}

// fileWithPrefix — путь файла копии, чьё базовое имя начинается с prefix:
// первый по порядку путей, чтобы порча была детерминированной при любом числе
// шаблонов каталога. Порча одного шаблона обязана ронять всю сборку, поэтому
// выбор шаблона предмета не меняет; ни одного файла — ФИКСТУРА.
func fileWithPrefix(t *testing.T, m fstest.MapFS, prefix string) string {
	t.Helper()
	var hit []string
	for p := range m {
		if strings.HasPrefix(path.Base(p), prefix) {
			hit = append(hit, p)
		}
	}
	if len(hit) == 0 {
		t.Fatalf("ФИКСТУРА: файлов с префиксом %q в копии 0 — портить нечего", prefix)
	}
	sort.Strings(hit)
	return hit[0]
}

func TestNew_EmbeddedCatalogServesEveryTemplate(t *testing.T) {
	b, err := New()
	if err != nil {
		t.Fatalf("New на встроенном каталоге: %v", err)
	}
	if len(b.templates) == 0 {
		t.Fatalf("New: шаблонов 0")
	}
	for key, tpl := range b.templates {
		ns, name, _ := strings.Cut(key, "/")
		got, ok := b.Template(ns, name)
		if !ok || got != tpl || !got.HasRevision {
			t.Errorf("Template(%q, %q): найден %v, ревизия %v", ns, name, ok, ok && got.HasRevision)
		}
		if _, ok := b.Template(ns+"-other", name); ok {
			t.Errorf("Template(%q, %q) найден в чужом пространстве", ns+"-other", name)
		}
	}
	t.Logf("встроенная сборка: шаблонов %d", len(b.templates))
}

func TestLoad_RefusesABrokenCatalog(t *testing.T) {
	if _, err := load(embeddedCopy(t), catalogRoot); err != nil {
		t.Fatalf("близнец: копия встроенного каталога без порчи отвергнута: %v", err)
	}
	cases := []struct {
		name   string
		spoil  func(t *testing.T, m fstest.MapFS)
		reason string
	}{
		{"шаблон без ревизии", func(t *testing.T, m fstest.MapFS) {
			delete(m, fileWithPrefix(t, m, "revision."))
		}, "нет ревизии"},
		{"описание не принято валидатором", func(t *testing.T, m fstest.MapFS) {
			p := fileWithPrefix(t, m, "notification.")
			m[p] = &fstest.MapFile{Data: append(append([]byte{}, m[p].Data...), "unknown_key: 1\n"...)}
		}, "пространство"},
		{"файл в корне каталога сборки", func(_ *testing.T, m fstest.MapFS) {
			m[catalogRoot+"/stray"] = &fstest.MapFile{Data: []byte("x")}
		}, "не пространство"},
		{"пространство без шаблонов", func(_ *testing.T, m fstest.MapFS) {
			m[catalogRoot+"/empty-space"] = &fstest.MapFile{Mode: fs.ModeDir}
		}, "без шаблонов"},
		{"каталог сборки пуст", func(_ *testing.T, m fstest.MapFS) {
			for p := range m {
				delete(m, p)
			}
			m[catalogRoot] = &fstest.MapFile{Mode: fs.ModeDir}
		}, "0 шаблонов"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := embeddedCopy(t)
			tc.spoil(t, m)
			b, err := load(m, catalogRoot)
			if err == nil {
				t.Fatalf("сборка принята (шаблонов %d)", len(b.templates))
			}
			if !strings.Contains(err.Error(), tc.reason) {
				t.Errorf("отказ %q не называет причину %q", err, tc.reason)
			}
		})
	}
}
