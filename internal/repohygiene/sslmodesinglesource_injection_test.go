// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Способность гейта «перечень sslmode объявлен один раз» УПАСТЬ и СМОЛЧАТЬ —
// доказана инъекцией, а не прочтением.
//
// Одностороннее доказательство негодно в обе стороны: гейт, который краснеет
// всегда, отключат первым же ложным срабатыванием, а гейт, который молчит
// всегда, не заметит возвращённой копии. Поэтому у каждой оси стоит ЗАКОННЫЙ
// БЛИЗНЕЦ — конструкция той же формы, на которой гейт обязан молчать.
func TestSSLModeSingleSourceInjectionCutsBothWays(t *testing.T) {
	cases := []struct {
		name string
		rel  string
		src  string
		find bool
		says []string
	}{
		{
			name: "НАХОДКА: возвращена копия боевого перечня",
			rel:  "services/vpc/internal/apps/kacho/config/validate.go",
			src: `package config

func productionSSLModeOK(mode string) bool {
	switch mode {
	case "require", "verify-ca", "verify-full":
		return true
	}
	return false
}
`,
			find: true,
			says: []string{"validate.go", "require", "verify-ca", "verify-full"},
		},
		{
			name: "НАХОДКА: один однозначный литерал — уже перечисление",
			rel:  "services/nlb/internal/apps/kacho/config/validate.go",
			src: `package config

func strictest(mode string) bool { return mode == "verify-full" }
`,
			find: true,
			says: []string{"verify-full"},
		},
		{
			name: "НАХОДКА: копия УЖЕ общего перечня — однозначных слов нет вовсе",
			rel:  "services/geo/internal/apps/kacho/config/validate.go",
			src: `package config

func knownMode(mode string) bool {
	switch mode {
	case "disable", "require":
		return true
	}
	return false
}
`,
			find: true,
			says: []string{"disable", "require"},
		},
		{
			name: "МОЛЧИТ: спрашивает предикат дома, режимы только в комментарии",
			rel:  "services/vpc/internal/apps/kacho/config/validate.go",
			src: `package config

import coredb "github.com/PRO-Robotech/kacho/pkg/db"

// Боевая посадка допускает require|verify-ca|verify-full — перечень живёт в
// pkg/db, здесь только вопрос к нему.
func productionSSLModeOK(mode string) bool { return coredb.SSLModeSecure(mode) }
`,
			find: false,
		},
		{
			name: "МОЛЧИТ: одиночное значение без дефиса — обычное слово, а не перечень",
			rel:  "services/compute/internal/config/config.go",
			src: `package config

// Умолчание ручки: одно значение, а не перечисление режимов.
const defaultSSLMode = "require"
`,
			find: false,
		},
		{
			name: "МОЛЧИТ: дом объявляет перечень — это ЦЕЛЬ свойства, а не его нарушение",
			rel:  "pkg/db/sslmode.go",
			src: `package db

var modes = []string{"disable", "allow", "prefer", "require", "verify-ca", "verify-full"}
`,
			find: false,
		},
		{
			name: "МОЛЧИТ: сгенерённые стабы руками не правят",
			rel:  "pkg/api/kacho/cloud/vpc/v1/x.pb.go",
			src: `package v1

var modes = []string{"require", "verify-ca", "verify-full"}
`,
			find: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(c.rel)), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, c.rel), []byte(c.src), 0o600); err != nil {
				t.Fatal(err)
			}
			findings, cen, err := auditSSLModeSingleSource(dir, []string{c.rel})
			if err != nil {
				t.Fatalf("обход: %v", err)
			}
			if !c.find {
				if len(findings) != 0 {
					t.Fatalf("ложная находка на законной форме: %+v", findings)
				}
				return
			}
			if len(findings) == 0 {
				t.Fatalf("гейт НЕ УВИДЕЛ возвращённой копии — перепись: %+v", cen)
			}
			// Находка обязана НАЗЫВАТЬ координату и значения: находка, называющая
			// симптом, посылает читателя искать не там, и её снимают как непонятную.
			text := findings[0].Where + " " + strings.Join(findings[0].Values, " ")
			for _, want := range c.says {
				if !strings.Contains(text, want) {
					t.Fatalf("находка не называет %q — сказано только %q", want, text)
				}
			}
		})
	}
}

// Пустой обход — ОТКАЗ, а не молчаливый успех: «ноль находок» обязано быть
// отличимо от «ноль прочитанного».
func TestSSLModeSingleSourceEmptyWalkIsNotSuccess(t *testing.T) {
	dir := t.TempDir()
	findings, cen, err := auditSSLModeSingleSource(dir, nil)
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("пустой обход дал находки: %+v", findings)
	}
	if cen.FilesRead != 0 || cen.HomeFiles != 0 {
		t.Fatalf("пустой обход отчитался о прочитанном: %+v", cen)
	}
	// Гейт (не судящая функция) на такой переписи обязан упасть — он требует
	// непустого обхода и непустого дома. Здесь утверждается ровно то, что делает
	// это требование выразимым: перепись НУЛЕВАЯ, а не «похожа на чистое дерево».
}
