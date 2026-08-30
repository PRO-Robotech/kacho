// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// clienttruth_iam_moduleset_injection_test.go — ДОКАЗАТЕЛЬСТВО того, что анализатор
// способен упасть, называет координату и молчит на законном близнеце.
//
// Стенд синтетический: настоящее дерево нельзя ни сломать, ни вернуть, а вердикт
// о нём (`clienttruth_iam_moduleset_test.go`) о способности падать не говорит
// ничего — зелёный получает и та проверка, что не смотрит никуда.
//
// Инъекции вносятся ПО ОДНОЙ. К каждой приложен законный близнец той же формы,
// обязанный молчать.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// moduleSetStand — синтетическое дерево: объявление набора из четырёх имён и
// клиентские поверхности, называющие его ВЕРНО. Это ЗАКОННОЕ состояние, и на нём
// анализатор обязан молчать.
type moduleSetStand struct{ root string }

func newModuleSetStand(t *testing.T) *moduleSetStand {
	t.Helper()
	s := &moduleSetStand{root: t.TempDir()}

	// Объявление. В КОММЕНТАРИИ рядом стоит неполный перечень тех же имён —
	// анализатор, читающий сырой текст вместо узла-литерала, вывел бы набор из
	// него и разошёлся бы с действительностью, не покраснев ни разу.
	s.write(t, "svc/domain/module_set.go", `package domain

// knownModules — набор. Исторически он был {alpha, beta}; эта строка НЕ объявление.
var knownModules = []string{"alpha", "beta", "gamma", "delta"}

// IsKnownModule …
func IsKnownModule(m string) bool { return false }
`)
	// Полный перечень в документации — законное состояние.
	s.write(t, "docs/role.mdx", `# Роль

<tr><td><code>module</code></td><td>Ровно один: <code>alpha</code> / <code>beta</code> / <code>gamma</code> / <code>delta</code></td></tr>
`)
	// Полный перечень в контракте — законное состояние.
	s.write(t, "proto/role.proto", "// член набора (`alpha`/`beta`/`gamma`/`delta`).\nstring module = 6;\n")
	// ЗАКОННЫЙ БЛИЗНЕЦ: пара имён — не перечень набора, а соседство двух
	// доменов. Гейт обязан молчать на ней при любом составе набора.
	s.write(t, "docs/feed.mdx", "Зеркалом кормятся `alpha`/`beta` — остальные нет.\n")
	return s
}

func (s *moduleSetStand) write(t *testing.T, rel, body string) {
	t.Helper()
	p := filepath.Join(s.root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (s *moduleSetStand) run(t *testing.T) ([]ClientTruthIAMModuleSetFinding, ClientTruthIAMModuleSetCensus) {
	t.Helper()
	var log strings.Builder
	f, c, err := AuditClientTruthIAMModuleSet(ClientTruthIAMModuleSetOptions{
		Tree:          clientTruthSyntheticTree(t, s.root),
		ModuleSetFile: "svc/domain/module_set.go",
		ModuleSetVar:  "knownModules",
		Surfaces:      []string{"docs", "proto"},
		SurfaceExts:   []string{".mdx", ".proto"},
	}, &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))
	return f, c
}

// TestModuleSetGate_SilentOnCompleteEnumerations — контроль: всё цело, гейт молчит.
// Без него любое «краснеет» ниже доказывало бы лишь то, что он краснеет всегда.
func TestModuleSetGate_SilentOnCompleteEnumerations(t *testing.T) {
	s := newModuleSetStand(t)
	findings, census := s.run(t)
	if len(findings) != 0 {
		t.Fatalf("на законном дереве findings=%d, ожидался 0: %v", len(findings), findings)
	}
	if census.Modules != 4 {
		t.Fatalf("модулей выведено %d, ожидалось 4 — набор берётся из УЗЛА-литерала, "+
			"а не из комментария рядом", census.Modules)
	}
	if census.Enumerations != 2 {
		t.Fatalf("перечней рассужено %d, ожидалось 2 (документ + контракт)", census.Enumerations)
	}
	// Законный близнец учтён как пара и НЕ рассужен — объявленная слепая зона.
	if census.PairSpans != 1 {
		t.Fatalf("спанов из двух имён %d, ожидался 1 (законная пара)", census.PairSpans)
	}
}

// TestModuleSetGate_RedOnIncompleteDocEnumeration — инъекция: перечень документа
// теряет одно имя. Гейт обязан покраснеть и НАЗВАТЬ координату и недостающее имя.
func TestModuleSetGate_RedOnIncompleteDocEnumeration(t *testing.T) {
	s := newModuleSetStand(t)
	s.write(t, "docs/role.mdx", `# Роль

<tr><td><code>module</code></td><td>Ровно один: <code>alpha</code> / <code>beta</code> / <code>gamma</code></td></tr>
`)
	findings, _ := s.run(t)
	if len(findings) != 1 {
		t.Fatalf("findings=%d, ожидался ровно 1 (документ): %v", len(findings), findings)
	}
	got := findings[0].String()
	for _, want := range []string{"docs/role.mdx", ":3", "delta"} {
		if !strings.Contains(got, want) {
			t.Errorf("находка не называет %q: %s", want, got)
		}
	}
}

// TestModuleSetGate_RedOnIncompleteProtoEnumeration — инъекция: то же в контракте.
// Отдельным прогоном, потому что поверхности разные и распознаватель у них разный
// (обратные кавычки против <code>).
func TestModuleSetGate_RedOnIncompleteProtoEnumeration(t *testing.T) {
	s := newModuleSetStand(t)
	s.write(t, "proto/role.proto", "// член набора (`alpha`/`beta`/`gamma`).\nstring module = 6;\n")
	findings, _ := s.run(t)
	if len(findings) != 1 {
		t.Fatalf("findings=%d, ожидался ровно 1 (контракт): %v", len(findings), findings)
	}
	if !strings.Contains(findings[0].String(), "proto/role.proto") ||
		!strings.Contains(findings[0].String(), "delta") {
		t.Errorf("находка не называет координату и недостающее имя: %s", findings[0].String())
	}
}

// TestModuleSetGate_SilentWhenSetGrowsAndEnumerationsFollow — набор вырос, перечни
// названы полностью. Это ровно тот случай, в котором прежняя проба набора молчала
// незаконно; здесь молчание ЗАКОННО, и гейт обязан его сохранить.
func TestModuleSetGate_SilentWhenSetGrowsAndEnumerationsFollow(t *testing.T) {
	s := newModuleSetStand(t)
	s.write(t, "svc/domain/module_set.go",
		"package domain\n\nvar knownModules = []string{\"alpha\", \"beta\", \"gamma\", \"delta\", \"epsilon\"}\n")
	s.write(t, "docs/role.mdx",
		"<code>alpha</code> / <code>beta</code> / <code>gamma</code> / <code>delta</code> / <code>epsilon</code>\n")
	s.write(t, "proto/role.proto", "// (`alpha`/`beta`/`gamma`/`delta`/`epsilon`)\n")
	findings, census := s.run(t)
	if census.Modules != 5 {
		t.Fatalf("модулей выведено %d, ожидалось 5", census.Modules)
	}
	if len(findings) != 0 {
		t.Fatalf("перечни полны, а гейт краснеет: %v", findings)
	}
}

// TestModuleSetGate_RedWhenSetGrowsAndEnumerationsDoNot — та же смена набора, но
// перечни за ней НЕ пошли: ровно дефект #1627. Обязаны покраснеть ОБА места.
func TestModuleSetGate_RedWhenSetGrowsAndEnumerationsDoNot(t *testing.T) {
	s := newModuleSetStand(t)
	s.write(t, "svc/domain/module_set.go",
		"package domain\n\nvar knownModules = []string{\"alpha\", \"beta\", \"gamma\", \"delta\", \"epsilon\"}\n")
	findings, _ := s.run(t)
	if len(findings) != 2 {
		t.Fatalf("findings=%d, ожидалось 2 (документ и контракт): %v", len(findings), findings)
	}
	for _, f := range findings {
		if len(f.Missing) != 1 || f.Missing[0] != "epsilon" {
			t.Errorf("находка называет не то недостающее имя: %v", f.Missing)
		}
	}
}

// TestModuleSetGate_FailsOnEmptyTraversal — «ноль находок» обязано быть отличимо
// от «прочитано ноль»: объявления нет вовсе.
func TestModuleSetGate_FailsOnEmptyTraversal(t *testing.T) {
	s := newModuleSetStand(t)
	if err := os.Remove(filepath.Join(s.root, "svc/domain/module_set.go")); err != nil {
		t.Fatal(err)
	}
	var log strings.Builder
	_, _, err := AuditClientTruthIAMModuleSet(ClientTruthIAMModuleSetOptions{
		Tree:          clientTruthSyntheticTree(t, s.root),
		ModuleSetFile: "svc/domain/module_set.go",
		ModuleSetVar:  "knownModules",
		Surfaces:      []string{"docs", "proto"},
		SurfaceExts:   []string{".mdx", ".proto"},
	}, &log)
	if err == nil {
		t.Fatal("объявления нет, а анализатор вернул успех — пустой обход неотличим от чистого дерева")
	}
}

// TestModuleSetGate_FailsWhenVarNameIsWrong — вторая половина той же премисы:
// объявление есть, но названо неверно. Молчаливый ноль здесь означал бы гейт,
// переживший переименование своего предмета.
func TestModuleSetGate_FailsWhenVarNameIsWrong(t *testing.T) {
	s := newModuleSetStand(t)
	var log strings.Builder
	_, _, err := AuditClientTruthIAMModuleSet(ClientTruthIAMModuleSetOptions{
		Tree:          clientTruthSyntheticTree(t, s.root),
		ModuleSetFile: "svc/domain/module_set.go",
		ModuleSetVar:  "renamedModules",
		Surfaces:      []string{"docs", "proto"},
		SurfaceExts:   []string{".mdx", ".proto"},
	}, &log)
	if err == nil {
		t.Fatal("объявление не найдено по имени, а анализатор вернул успех")
	}
}
