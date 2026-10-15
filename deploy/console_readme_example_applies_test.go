// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build helmcharts

// console_readme_example_applies_test.go — пример значений из README чарта
// консоли, положенный в зонт, ПРИМЕНЯЕТСЯ к консоли (kacho#3218).
//
// README называл чарт `ui` и давал пример под ключом `ui:`, а зонт знает
// зависимость `uif`. Незнакомый ключ верхнего уровня helm не отвергает: пример
// с настройками консоли молча не применялся, и рендер был зелёным.
//
// Проба рендерит зонт боевым профилем с каждым yaml-примером README поверх и
// требует, чтобы каждая строковая величина примера доехала до документов чарта
// консоли (метка `helm.sh/chart: uif-…`). Положительный близнец — тот же пример
// под верным ключом; инъекция — пример под ключом `ui:`.
package deploy_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const consoleChartReadme = "../ui-future/deploy/README.md"

var readmeYAMLBlock = regexp.MustCompile("(?s)```yaml\n(.*?)```")

// stringLeaves — строковые листья дерева значений.
func readmeStringLeaves(v any, out *[]string) {
	switch x := v.(type) {
	case map[string]any:
		for _, c := range x {
			readmeStringLeaves(c, out)
		}
	case []any:
		for _, c := range x {
			readmeStringLeaves(c, out)
		}
	case string:
		*out = append(*out, x)
	}
}

// consoleDocs — документы рендера, принадлежащие чарту консоли.
func consoleDocs(render string) string {
	var b strings.Builder
	for _, d := range strings.Split(render, "\n---") {
		if strings.Contains(d, "helm.sh/chart: uif-") {
			b.WriteString(d)
		}
	}
	return b.String()
}

// unappliedLeaves — строковые величины примера, не доехавшие до консоли.
func unappliedLeaves(t *testing.T, example string) []string {
	t.Helper()
	var tree map[string]any
	if err := yaml.Unmarshal([]byte(example), &tree); err != nil {
		t.Fatalf("пример README не разбирается как YAML: %v", err)
	}
	f := filepath.Join(t.TempDir(), "readme-example.yaml")
	if err := os.WriteFile(f, []byte(example), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := helmRenderFiles([]string{filepath.Join(umbrellaDir, "values.prod.yaml"), f},
		`uif.enabled=true`)
	if err != nil {
		t.Fatalf("зонт с примером README не рендерится: %v\n%s", err, out)
	}
	docs := consoleDocs(out)
	if docs == "" {
		t.Fatal("в рендере нет ни одного документа чарта консоли — предпосылка исчезла")
	}
	var leaves, missing []string
	readmeStringLeaves(tree, &leaves)
	for _, l := range leaves {
		if !strings.Contains(docs, l) {
			missing = append(missing, l)
		}
	}
	sort.Strings(missing)
	return missing
}

func TestConsoleReadmeExampleAppliesUnderTheUmbrella(t *testing.T) {
	requireHelmForRender(t)
	raw, err := os.ReadFile(consoleChartReadme)
	if err != nil {
		t.Fatalf("README чарта консоли не прочитан: %v", err)
	}
	blocks := readmeYAMLBlock.FindAllStringSubmatch(string(raw), -1)
	t.Logf("перепись: yaml-примеров в README %d", len(blocks))
	if len(blocks) == 0 {
		t.Fatal("в README чарта консоли нет ни одного yaml-примера — обход пуст")
	}
	for i, b := range blocks {
		if m := unappliedLeaves(t, b[1]); len(m) != 0 {
			t.Errorf("пример %d README чарта консоли не применяется к консоли в зонте: величины %q "+
				"в рендер чарта не доехали (ключ верхнего уровня зонта — `uif`)", i+1, m)
		}
	}
}

func TestConsoleReadmeExampleInjection_WrongTopKeyIsFound(t *testing.T) {
	requireHelmForRender(t)
	if m := unappliedLeaves(t, "ui:\n  image: registry.example.test/console-host:probe\n"); len(m) != 1 {
		t.Fatalf("пример под ключом ui: назван применённым: %v", m)
	}
}

func TestConsoleReadmeExampleInjection_RightTopKeyIsSilent(t *testing.T) {
	requireHelmForRender(t)
	if m := unappliedLeaves(t, "uif:\n  image: registry.example.test/console-host:probe\n"); len(m) != 0 {
		t.Fatalf("пример под ключом uif: назван неприменённым: %v", m)
	}
}
