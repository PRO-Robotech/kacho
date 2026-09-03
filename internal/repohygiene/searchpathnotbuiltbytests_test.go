// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// searchpathnotbuiltbytests_test.go — держатель: проба не собирает клаузу
// приведения схемы сама.
//
// Предмет, цена пропуска и разбор «находка против законного упоминания» — в
// шапке `searchpathnotbuiltbytests.go`; здесь они не пересказываются.
//
// Способность гейта упасть и смолчать доказана инъекцией —
// `searchpathnotbuiltbytests_injection_test.go`.
package repohygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestTestsDoNotBuildTheSearchPathClause(t *testing.T) {
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	var findings []SearchPathBuildSite
	files, withMarker, unparsed, mentions := 0, 0, 0, 0
	for rel := range tt.files {
		if !strings.HasSuffix(rel, "_test.go") {
			continue
		}
		files++
		body, err := os.ReadFile(filepath.Join(root, rel)) // #nosec G304 -- путь из индекса СВОЕГО репозитория
		if err != nil {
			t.Fatalf("чтение %s: %v", rel, err)
		}
		src := string(body)
		if !strings.Contains(src, searchPathMarker) {
			continue
		}
		withMarker++
		mentions += strings.Count(src, searchPathMarker)
		sites, ok := SearchPathBuildSitesIn(rel, src)
		if !ok {
			// Файл не разобрался: это НЕ чистота. Считается отдельно, иначе
			// «ноль находок» стало бы неотличимо от «ноль прочитанного».
			unparsed++
			continue
		}
		findings = append(findings, sites...)
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].Line < findings[j].Line
	})

	t.Logf("файлов проб осмотрено %d; упоминают клаузу %d файл(ов), вхождений %d; "+
		"не разобрано %d; собирают клаузу склейкой %d",
		files, withMarker, mentions, unparsed, len(findings))

	if files == 0 {
		t.Fatal("файлов проб не найдено: обход пуст, и вердикт беспредметен")
	}
	if withMarker == 0 {
		t.Fatal("клауза приведения схемы не встречается в дереве ни разу — предпосылка гейта " +
			"исчезла, и его молчание перестало что-либо означать: снимите утверждение вместе " +
			"с предметом либо переведите на признак, который дерево производит")
	}
	if unparsed > 0 {
		t.Fatalf("%d файл(ов) не разобрались: вердикт по ним не вынесен", unparsed)
	}
	if len(findings) > 0 {
		var b strings.Builder
		fmt.Fprintf(&b, "%d мест(а) собирают клаузу приведения схемы в пробе — "+
			"забывший её получает `relation … does not exist`, то есть дефект ПРОБЫ "+
			"с сообщением о дефекте ПРОДУКТА:\n", len(findings))
		for _, s := range findings {
			via := ""
			if s.Via != "" {
				via = fmt.Sprintf(" (через %s)", s.Via)
			}
			fmt.Fprintf(&b, "  %s:%d склейка со строкой соединения%s\n", s.File, s.Line, via)
		}
		b.WriteString("\nПриведение объявляется ОДИН раз у того, кто выдаёт базу:\n" +
			"  pgtest.Config{… SearchPath: \"kacho_<домен>,public\"}  — пакет берёт базу у pgtest;\n" +
			"  pgtest.WithSearchPath(dsn, \"kacho_<домен>,public\")   — пакет собирает DSN сам.\n")
		t.Fatal(b.String())
	}
}
