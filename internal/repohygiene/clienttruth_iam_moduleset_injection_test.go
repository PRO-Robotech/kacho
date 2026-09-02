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
	s.write(t, "svc/authzmap/fga_types.go", `package authzmap

// objectTypes — таблица типов. Исторически модулей было {alpha, beta}; эта
// строка НЕ объявление. Ключ "epsilon.ghost" здесь тоже НЕ объявление.
var objectTypes = map[string]string{
	"alpha.one":   "alpha_one",
	"alpha.two":   "alpha_two",
	"beta.one":    "beta_one",
	"gamma.one":   "gamma_one",
	"delta.one":   "delta_one",
	"noDotAtAll":  "malformed",
}
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
		Tree:         clientTruthSyntheticTree(t, s.root),
		ModuleSetPkg: "svc/authzmap",
		ModuleSetVar: "objectTypes",
		Surfaces:     []string{"docs", "proto"},
		SurfaceExts:  []string{".mdx", ".proto"},
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
		t.Fatalf("модулей выведено %d, ожидалось 4 — набор берётся из УЗЛОВ-КЛЮЧЕЙ, "+
			"а не из комментария рядом", census.Modules)
	}
	// Ключ без точки типом не является и модуля НЕ даёт: прочитан он всё равно,
	// и обе величины печатаются, иначе «шесть ключей» и «пять» неразличимы.
	if census.TypeKeys != 6 {
		t.Fatalf("ключей прочитано %d, ожидалось 6 (пять типов плюс один без точки)",
			census.TypeKeys)
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
	s.write(t, "svc/authzmap/fga_types.go", grownTypeTable)
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
	s.write(t, "svc/authzmap/fga_types.go", grownTypeTable)
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

// TestModuleSetGate_SilentWhenTheDeclarationMovesWithinItsPackage — предмет
// задачи #1944 на уровне самого гейта, а не разрешателя.
//
// Объявление уезжает в ДРУГОЙ файл того же пакета — ровно то, что сделал переход
// таблицы типов на порождение (#1092). Прежний гейт на этом отказывал
// «анализатор не отработал», то есть третьей категорией, поданной как красное.
// Здесь он обязан вынести тот же вердикт, что и до переезда.
func TestModuleSetGate_SilentWhenTheDeclarationMovesWithinItsPackage(t *testing.T) {
	s := newModuleSetStand(t)
	// В прежнем файле остаётся ПРОЗА, называющая имя и неполный перечень: гейт,
	// читающий текст, вывел бы набор из неё и разошёлся бы с действительностью.
	s.write(t, "svc/authzmap/fga_types.go", `package authzmap

// objectTypes переехал в порождённый файл. Исторически модулей было
// {alpha, beta}; эта строка НЕ объявление.
func unrelated() {}
`)
	s.write(t, "svc/authzmap/tables_gen.go", `package authzmap

var objectTypes = map[string]string{
	"alpha.one":  "alpha_one",
	"beta.one":   "beta_one",
	"gamma.one":  "gamma_one",
	"delta.one":  "delta_one",
	"noDotAtAll": "malformed",
}
`)
	findings, census := s.run(t)
	if len(findings) != 0 {
		t.Fatalf("объявление лишь переехало внутри пакета, а гейт краснеет: %v", findings)
	}
	if census.DeclFile != "svc/authzmap/tables_gen.go" {
		t.Errorf("объявление найдено в %q — гейт читает прежний файл", census.DeclFile)
	}
	if census.PkgFiles != 2 {
		t.Errorf("файлов пакета осмотрено %d, ожидалось 2", census.PkgFiles)
	}
	if census.Modules != 4 {
		t.Errorf("модулей выведено %d, ожидалось 4 — набор взят из прозы прежнего файла",
			census.Modules)
	}
}

// TestModuleSetGate_RedWhenTheDeclarationMovedAndTheSetGrew — та же раскладка, но
// набор при переезде вырос, а перечни за ним не пошли. Без этой пробы «молчит
// после переезда» доказывало бы лишь то, что гейт после переезда молчит всегда.
func TestModuleSetGate_RedWhenTheDeclarationMovedAndTheSetGrew(t *testing.T) {
	s := newModuleSetStand(t)
	s.write(t, "svc/authzmap/fga_types.go", "package authzmap\n\nfunc unrelated() {}\n")
	s.write(t, "svc/authzmap/tables_gen.go", grownTypeTable)
	findings, census := s.run(t)
	if census.DeclFile != "svc/authzmap/tables_gen.go" {
		t.Fatalf("объявление найдено в %q — гейт читает прежний файл", census.DeclFile)
	}
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
	if err := os.Remove(filepath.Join(s.root, "svc/authzmap/fga_types.go")); err != nil {
		t.Fatal(err)
	}
	var log strings.Builder
	_, _, err := AuditClientTruthIAMModuleSet(ClientTruthIAMModuleSetOptions{
		Tree:         clientTruthSyntheticTree(t, s.root),
		ModuleSetPkg: "svc/authzmap",
		ModuleSetVar: "objectTypes",
		Surfaces:     []string{"docs", "proto"},
		SurfaceExts:  []string{".mdx", ".proto"},
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
		Tree:         clientTruthSyntheticTree(t, s.root),
		ModuleSetPkg: "svc/authzmap",
		ModuleSetVar: "renamedTypes",
		Surfaces:     []string{"docs", "proto"},
		SurfaceExts:  []string{".mdx", ".proto"},
	}, &log)
	if err == nil {
		t.Fatal("объявление не найдено по имени, а анализатор вернул успех")
	}
}

// grownTypeTable — та же таблица, в которой появился ПЯТЫЙ модуль. Вынесена
// значением, потому что её пишут два прогона: один — где перечни за ней пошли,
// другой — где не пошли. Две копии разошлись бы, и «краснеет» с «молчит»
// перестали бы говорить об одном входе.
const grownTypeTable = `package authzmap

var objectTypes = map[string]string{
	"alpha.one":   "alpha_one",
	"beta.one":    "beta_one",
	"gamma.one":   "gamma_one",
	"delta.one":   "delta_one",
	"epsilon.one": "epsilon_one",
}
`
