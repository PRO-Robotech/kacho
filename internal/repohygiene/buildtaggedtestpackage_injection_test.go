// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// buildtaggedtestpackage_injection_test.go — доказательство, что гейт
// «пакет собирается под своим признаком сборки» СПОСОБЕН упасть и способен
// смолчать.
//
// # Почему инъекция настоящим входом, а не подставным судьёй
//
// Предикат гейта — вызов настоящего компилятора. Проба, подменившая этот вызов
// заглушкой, доказала бы, что заглушка возвращает то, что в неё положили. Здесь
// поэтому строится НАСТОЯЩИЙ модуль Go: с `go.mod`, с индексом git (судья
// спрашивает индекс, а не диск) и с файлом проб под признаком сборки. Внешних
// зависимостей у него нет — значит сборка идёт без сети.
//
// # Обе стороны и почему одной мало
//
// Дефект и его ЗАКОННЫЙ БЛИЗНЕЦ отличаются одним: близнец обращается к тому, что
// в пакете есть. Всё остальное — признак сборки, имя файла, форма пробы —
// совпадает. Без близнеца гейт ловил бы саму форму «файл под признаком» и
// краснел бы на каждом законном теге; первый же ложный срабат такой гейт
// выключает.
package repohygiene

import (
	"go/build/constraint"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// synthTaggedModule — минимальный модуль: пакет, его прод-файл и файл проб под
// признаком сборки `synthtag`. Вызывающий задаёт ТЕЛО файла проб — единственное,
// чем дефект отличается от близнеца.
//
// Признак назван `synthtag`, а не `integration`: настоящий тег дерева здесь
// означал бы, что проба зависит от того, что этот тег в дереве ещё жив. Класс
// же — про ЛЮБОЙ признак сборки.
func synthTaggedModule(t *testing.T, taggedBody string) string {
	t.Helper()
	root := t.TempDir()

	files := map[string]string{
		"go.mod": "module synthtagged\n\ngo 1.24\n",
		"pkg/thing.go": "" +
			"package pkg\n\n" +
			"type Thing struct{}\n\n" +
			"func (Thing) Existing() int { return 1 }\n",
		"pkg/thing_tagged_test.go": taggedBody,
	}
	for rel, body := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	synthTrack(t, root)
	return root
}

// taggedProbe — файл проб под признаком сборки. `call` — единственная строка,
// которой дефект отличается от законного близнеца.
func taggedProbe(call string) string {
	return "//go:build synthtag\n\n" +
		"package pkg\n\n" +
		"import \"testing\"\n\n" +
		"func TestThing(t *testing.T) {\n" +
		"\tvar x Thing\n" +
		"\t_ = " + call + "\n" +
		"}\n"
}

// auditSynthTagged — ТОТ ЖЕ судья, что судит дерево, плюс проверка предпосылки
// самой инъекции: синтетика обязана быть ПРОЧИТАНА и её признак — РАСПОЗНАН.
// Иначе «ноль находок» на ней не значит ничего.
func auditSynthTagged(t *testing.T, root string) ([]buildTagFinding, buildTagCensus) {
	t.Helper()
	findings, census, err := auditBuildTaggedTestPackages(root)
	if err != nil {
		t.Fatalf("судья сорвался на синтетическом дереве: %v", err)
	}
	if census.TestFilesScanned == 0 || census.FilesWithBuildTag == 0 || census.VetRuns == 0 {
		t.Fatalf("синтетическое дерево не осмотрено — вердикт на нём недействителен: %s", census)
	}
	return findings, census
}

// TestBuildTagGateRedOnInjectedDefect — направление (а): под признаком сборки
// стоит обращение к тому, чего в пакете нет, — гейт КРАСНЕЕТ и НАЗЫВАЕТ
// координату. Находка без координаты не есть действие.
func TestBuildTagGateRedOnInjectedDefect(t *testing.T) {
	root := synthTaggedModule(t, taggedProbe("x.Missing()"))

	findings, census := auditSynthTagged(t, root)
	t.Log(census.String())

	if len(findings) == 0 {
		t.Fatalf("пакет под признаком сборки не собирается, а гейт молчит — "+
			"он не способен упасть.\n%s", census)
	}
	joined := joinBuildTagFindings(findings)
	if !strings.Contains(joined, "pkg") {
		t.Fatalf("находка не называет пакет — по ней нечего чинить:\n%s", joined)
	}
	if !strings.Contains(joined, "synthtag") {
		t.Fatalf("находка не называет признак сборки — непонятно, какой вызов её воспроизводит:\n%s", joined)
	}
	// Текст компилятора обязан доехать до читателя: без него находка сообщает
	// «не собирается» и не сообщает ЧЕМ, а диагноз ставится по тексту отказа.
	if !strings.Contains(joined, "Missing") {
		t.Fatalf("находка не несёт текста компилятора — диагноз по ней не поставить:\n%s", joined)
	}
}

// TestBuildTagGateSilentOnLegitimateTwin — направление (б): та же форма, тот же
// признак сборки, обращение к существующему — гейт МОЛЧИТ.
//
// Без этой стороны гейт ловил бы форму «в дереве есть признак сборки», то есть
// краснел бы на каждом законном теге.
func TestBuildTagGateSilentOnLegitimateTwin(t *testing.T) {
	root := synthTaggedModule(t, taggedProbe("x.Existing()"))

	findings, census := auditSynthTagged(t, root)
	t.Log(census.String())

	if len(findings) != 0 {
		t.Fatalf("законный пакет под признаком сборки помечен находкой — гейт ловит форму, "+
			"а не существо:\n%s", joinBuildTagFindings(findings))
	}
}

// TestBuildTagGateReadsOnlyTheHeader — предпосылка судьи: `//go:build` НИЖЕ
// объявления пакета инструментом не читается, и судья обязан читать его так же.
//
// Проба стоит здесь, а не в основном файле, потому что она про поведение судьи
// на входе, а не про свойство дерева. Не будь её, судья мог бы «найти» признак
// в комментарии посреди файла и звать сборку с тегом, которого нет, — вызов
// прошёл бы молча, и гейт стал бы проверкой с формой, но без содержания.
func TestBuildTagGateReadsOnlyTheHeader(t *testing.T) {
	body := "package pkg\n\n" +
		"import \"testing\"\n\n" +
		"// Ниже — не признак сборки, а разговор о нём:\n" +
		"//go:build synthtag\n\n" +
		"func TestPlain(t *testing.T) { _ = Thing{} }\n"
	root := synthTaggedModule(t, body)

	findings, census, err := auditBuildTaggedTestPackages(root)
	if err != nil {
		t.Fatalf("судья сорвался: %v", err)
	}
	t.Log(census.String())

	if census.TestFilesScanned == 0 {
		t.Fatalf("синтетическое дерево не прочитано: %s", census)
	}
	if census.FilesWithBuildTag != 0 {
		t.Fatalf("строка ниже объявления пакета принята за признак сборки — судья читает "+
			"не то, что читает инструмент: %s", census)
	}
	if len(findings) != 0 {
		t.Fatalf("находка на файле без признака сборки:\n%s", joinBuildTagFindings(findings))
	}
}

// TestBuildTagSelectionTakesOnlyEnablingTags — контроль ОТБОРА признаков, в обе
// стороны, на формах, которые реально стоят в дереве.
//
// Отбор — самое хрупкое место гейта, и первая его редакция была неверна: она
// брала ВСЕ упомянутые идентификаторы и потому требовала сборки под `-tags=short`
// от `services/vpc/internal/repo`, где стоит `//go:build integration || !short`.
// Пакет под этим тегом действительно не собирается — фикстура уходит из сборки, а
// её вызов остаётся, — но вызова с таким тегом не делает никто: `short` здесь
// ИСКЛЮЧАЮЩИЙ признак. Находка была бы на законном коде.
//
// Проба закрепляет разницу дословно, чтобы следующая правка отбора не вернула
// прежнее поведение молча.
func TestBuildTagSelectionTakesOnlyEnablingTags(t *testing.T) {
	cases := []struct {
		line string
		want []string
		why  string
	}{
		{
			line: "//go:build integration",
			want: []string{"integration"},
			why:  "простой признак включает файл — берётся (compute, deploy)",
		},
		{
			line: "//go:build integration || !short",
			want: []string{"integration"},
			why:  "`short` файл ИСКЛЮЧАЕТ, а не включает — не берётся (vpc)",
		},
		{
			line: "//go:build !short",
			want: nil,
			why:  "чистое отрицание не включает ничего: файл и так в обычной сборке",
		},
		{
			line: "//go:build integration && docker",
			want: nil,
			why:  "ни один тег ПООДИНОЧКЕ файл не включает — сборка под ним ничего не докажет",
		},
		{
			line: "//go:build linux && integration",
			want: nil,
			why:  "признак инструмента отбрасывается, а один `integration` конъюнкции не хватает",
		},
	}

	for _, c := range cases {
		expr, err := constraint.Parse(c.line)
		if err != nil {
			t.Fatalf("разбор %q: %v", c.line, err)
		}
		got := enablingTags(expr)
		if strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Errorf("%q: отбор дал %v, ожидалось %v — %s", c.line, got, c.want, c.why)
			continue
		}
		t.Logf("%q → %v (%s)", c.line, got, c.why)
	}
}

func joinBuildTagFindings(findings []buildTagFinding) string {
	parts := make([]string, 0, len(findings))
	for _, f := range findings {
		parts = append(parts, f.String())
	}
	return strings.Join(parts, "\n")
}
