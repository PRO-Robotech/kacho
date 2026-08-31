// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Способность гейта «словарь посадок объявлен один раз» УПАСТЬ и СМОЛЧАТЬ —
// доказана инъекцией, а не прочтением.
//
// Одностороннее доказательство негодно в обе стороны: гейт, который краснеет
// всегда, отключат первым же ложным срабатыванием, а гейт, который молчит всегда,
// не заметит возвращённой копии. Поэтому у каждой оси стоит ЗАКОННЫЙ БЛИЗНЕЦ —
// конструкция той же формы, на которой гейт обязан молчать.
//
// Файл дома лежит в КАЖДОМ синтетическом дереве: без него предпосылка гейта не
// выполняется, и красное приходило бы от неё, а не от проверяемой оси
// (`testing.md` §«Гейт на класс», п.2в).
func TestPostureVocabularySingleSourceInjectionCutsBothWays(t *testing.T) {
	const home = `package servicecontract

var modeVocabulary = []struct {
	Name string
}{
	{"dev"},
	{"production"},
	{"production-strict"},
}
`
	cases := []struct {
		name string
		rel  string
		src  string
		find bool
		says []string
	}{
		{
			name: "НАХОДКА: возвращена копия словаря целиком",
			rel:  "services/nlb/internal/apps/kacho/config/validate.go",
			src: `package config

func parseMode(s string) (int, error) {
	switch s {
	case "dev":
		return 1, nil
	case "production":
		return 2, nil
	case "production-strict":
		return 3, nil
	}
	return 0, nil
}
`,
			find: true,
			says: []string{"validate.go", "dev", "production", "production-strict"},
		},
		{
			name: "НАХОДКА: одно однозначное написание — уже объявление",
			rel:  "services/vpc/cmd/vpc/describe.go",
			src: `package main

func strictest(mode string) bool { return mode == "production-strict" }
`,
			find: true,
			says: []string{"describe.go", "production-strict"},
		},
		{
			name: "НАХОДКА: копия УЖЕ общего словаря — однозначных написаний нет вовсе",
			rel:  "services/nlb/internal/apps/kacho/config/validate.go",
			src: `package config

func known(mode string) bool {
	switch mode {
	case "dev", "production":
		return true
	}
	return false
}
`,
			find: true,
			says: []string{"dev", "production"},
		},
		{
			name: "МОЛЧИТ: одно написание как ЗНАЧЕНИЕ умолчания ручки",
			rel:  "services/nlb/internal/apps/kacho/config/defaults.go",
			src: `package config

func RegisterDefaults(set func(key, value string)) { set("mode", "production") }
`,
			find: false,
		},
		{
			name: "МОЛЧИТ: `dev` как метка окружения, а не посадка",
			rel:  "gateway/cmd/api-gateway/authz_validation.go",
			src: `package main

import "strings"

func devClassEnv(env string) bool {
	switch strings.ToLower(strings.TrimSpace(env)) {
	case "dev":
		return true
	}
	return false
}
`,
			find: false,
		},
		{
			name: "МОЛЧИТ: спрашивает разбор дома, написания только в комментарии и в тексте отказа",
			rel:  "services/nlb/internal/apps/kacho/config/validate.go",
			src: `package config

import (
	"fmt"
	"strings"

	"github.com/PRO-Robotech/kacho/pkg/servicecontract"
)

// Допустимы dev, production и production-strict — словарь живёт в
// pkg/servicecontract, здесь только вопрос к нему.
func parseMode(s string) (servicecontract.Mode, error) {
	m, err := servicecontract.ParseMode(s)
	if err != nil {
		return 0, fmt.Errorf("mode %q: допустимы %s", s, strings.Join(servicecontract.Modes(), ", "))
	}
	return m, nil
}
`,
			find: false,
		},
		{
			name: "МОЛЧИТ: перевод общего значения в своё — литералов не содержит",
			rel:  "services/vpc/internal/apps/kacho/config/mode.go",
			src: `package config

import "github.com/PRO-Robotech/kacho/pkg/servicecontract"

type Mode int

func fromHost(m servicecontract.Mode) Mode {
	switch m {
	case servicecontract.ModeProductionStrict:
		return 2
	case servicecontract.ModeProduction:
		return 1
	default:
		return 0
	}
}
`,
			find: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			files := map[string]string{
				filepath.Join(postureHomeDir, "contract.go"): home,
				tc.rel: tc.src,
			}
			var rels []string
			for rel, src := range files {
				abs := filepath.Join(root, rel)
				if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
					t.Fatalf("создание каталога %s: %v", rel, err)
				}
				if err := os.WriteFile(abs, []byte(src), 0o600); err != nil {
					t.Fatalf("запись %s: %v", rel, err)
				}
				rels = append(rels, filepath.ToSlash(rel))
			}
			sort.Strings(rels)

			findings, cen, err := auditPostureVocabularySingleSource(root, rels)
			if err != nil {
				t.Fatalf("обход: %v", err)
			}
			if cen.FilesRead != len(rels) {
				t.Fatalf("разобрано %d файлов из %d — обход неполон, вердикт беспредметен",
					cen.FilesRead, len(rels))
			}
			// Дом обязан быть узнан домом в КАЖДОМ прогоне: иначе красное приходило
			// бы от него, и вакуумность проверяемой оси осталась бы незамеченной.
			if cen.HomeFiles != 1 {
				t.Fatalf("файлов дома узнано %d, ожидался 1 — инъекция роняет не то, что проверяет",
					cen.HomeFiles)
			}

			var got []string
			for _, f := range findings {
				got = append(got, f.Where+": "+strings.Join(f.Values, ", "))
			}
			joined := strings.Join(got, "\n")
			if strings.Contains(joined, postureHomeDir) {
				t.Fatalf("дом объявлен нарушителем — гейт ловит форму, а не существо:\n%s", joined)
			}
			if tc.find && len(findings) == 0 {
				t.Fatalf("гейт смолчал на возвращённой копии — падать он не способен\nперепись: %+v", cen)
			}
			if !tc.find && len(findings) != 0 {
				t.Fatalf("гейт покраснел на законной конструкции:\n%s", joined)
			}
			for _, want := range tc.says {
				if !strings.Contains(joined, want) {
					t.Errorf("находка не называет %q — читатель пойдёт искать не там:\n%s", want, joined)
				}
			}
		})
	}
}
