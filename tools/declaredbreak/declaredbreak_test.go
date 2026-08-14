// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package declaredbreak

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// realFixture — вывод, СНЯТЫЙ С РЕАЛЬНОГО buf 1.72.0 (той же версии, что пинит
// конвейер), а не написанный по догадке о его формате. Догадка о чужом формате даёт
// пробу, которая зеленеет на любом выводе, включая тот, который гейт никогда не получит.
//
// Как снят: копия каталога контрактов вне рабочей копии, в ней сняты один метод
// (`RouteTableService.AddRoutes`) и одно поле (`SecurityGroupRule.predefined_target`),
// затем `buf breaking --against <исходный каталог> --error-format=json`.
const realFixture = "testdata/buf-breaking-real.jsonl"

func loadReal(t *testing.T) []Finding {
	t.Helper()
	f, err := os.Open(filepath.FromSlash(realFixture))
	if err != nil {
		t.Fatalf("фикстура не открыта: %v", err)
	}
	defer f.Close()
	got, err := ParseFindings(f)
	if err != nil {
		t.Fatalf("реальный вывод buf не разобран: %v", err)
	}
	return got
}

// TestParseFindingsReadsRealBufOutput — разбор формата, а не его пересказ.
func TestParseFindingsReadsRealBufOutput(t *testing.T) {
	got := loadReal(t)
	if len(got) != 2 {
		t.Fatalf("находок из фикстуры: %d, ожидалось 2", len(got))
	}
	if got[0].Type != "FIELD_NO_DELETE" || got[0].Path != "kacho/cloud/vpc/v1/security_group.proto" || got[0].StartLine != 63 {
		t.Errorf("первая находка разобрана неверно: %+v", got[0])
	}
	if got[1].Type != "RPC_NO_DELETE" || got[1].StartLine != 19 {
		t.Errorf("вторая находка разобрана неверно: %+v", got[1])
	}
	if got[1].Coordinate() != "kacho/cloud/vpc/v1/route_table_service.proto:19" {
		t.Errorf("координата: %q", got[1].Coordinate())
	}
	t.Logf("осмотрено: находок в фикстуре %d, правил различных %d", len(got), 2)
}

// TestPremiseSymbolIsQuoted — ПРОВЕРКА СОБСТВЕННОЙ ПРЕДПОСЫЛКИ гейта.
//
// Сопоставление опирается на факт о чужом выводе: buf называет символ в кавычках.
// Факт меняется — сопоставление перестаёт работать молча, поэтому он утверждается
// отдельно и на реальной фикстуре. Обратная сторона в TestSymbolMismatchIsItsOwnOutcome:
// отказ предпосылки виден как отдельный исход, а не как ложное «послабление истекло».
func TestPremiseSymbolIsQuoted(t *testing.T) {
	got := loadReal(t)
	want := map[string]string{
		"FIELD_NO_DELETE": "predefined_target",
		"RPC_NO_DELETE":   "AddRoutes",
	}
	for _, f := range got {
		sym, ok := want[f.Type]
		if !ok {
			t.Fatalf("в фикстуре правило %s, для которого предпосылка не заявлена", f.Type)
		}
		if !strings.Contains(f.Message, `"`+sym+`"`) {
			t.Errorf("предпосылка нарушена: сообщение правила %s не называет символ %q в кавычках: %s", f.Type, sym, f.Message)
		}
	}
	t.Logf("осмотрено: правил %d, у каждого символ в кавычках подтверждён на реальном выводе", len(want))
}

// TestUndeclaredBreakIsRed — защита от СЛУЧАЙНОГО разрыва сохранена.
func TestUndeclaredBreakIsRed(t *testing.T) {
	res := Adjudicate(loadReal(t), nil)
	if res.Clean() {
		t.Fatal("два необъявленных разрыва прошли — гейт не защищает ни от чего")
	}
	if len(res.Undeclared) != 2 {
		t.Fatalf("необъявленных: %d, ожидалось 2", len(res.Undeclared))
	}
	rep := res.Report()
	for _, want := range []string{"НЕОБЪЯВЛЕННЫЙ РАЗРЫВ", "route_table_service.proto:19", "AddRoutes", "осмотрено находок buf 2"} {
		if !strings.Contains(rep, want) {
			t.Errorf("в отчёте нет %q:\n%s", want, rep)
		}
	}
}

func decl(rule, path, symbol string) Declaration {
	return Declaration{
		Rule: rule, Path: path, Symbol: symbol,
		Reason: "метод отвечает отказом при любом входе — снят с контракта осознанно",
		Issue:  "kacho#246",
	}
}

// TestDeclaredBreakPasses — ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ. Без него «красное на всём» было бы
// неотличимо от работающего гейта.
func TestDeclaredBreakPasses(t *testing.T) {
	decls := []Declaration{
		decl("RPC_NO_DELETE", "kacho/cloud/vpc/v1/route_table_service.proto", "AddRoutes"),
		decl("FIELD_NO_DELETE", "kacho/cloud/vpc/v1/security_group.proto", "predefined_target"),
	}
	res := Adjudicate(loadReal(t), decls)
	if !res.Clean() {
		t.Fatalf("объявленные разрывы не прошли:\n%s", res.Report())
	}
	if res.Matched != 2 {
		t.Errorf("сопоставлено %d, ожидалось 2", res.Matched)
	}
	if !strings.Contains(res.Report(), "[PASS]") {
		t.Errorf("отчёт не называет исход:\n%s", res.Report())
	}
}

// TestExpiredDeclarationIsRed — послабление истекает САМО. Это то, чем адъюдикация
// отличается от исключения по пути в конфигурации buf.
func TestExpiredDeclarationIsRed(t *testing.T) {
	decls := []Declaration{decl("RPC_NO_DELETE", "kacho/cloud/vpc/v1/gateway_service.proto", "Detach")}
	res := Adjudicate(nil, decls)
	if res.Clean() {
		t.Fatal("запись, которой больше нечего исключать, прошла — послабление переживёт свой предмет")
	}
	if len(res.Expired) != 1 {
		t.Fatalf("истёкших: %d, ожидалось 1", len(res.Expired))
	}
	if !strings.Contains(res.Report(), "ПОСЛАБЛЕНИЕ ИСТЕКЛО") {
		t.Errorf("исход не назван:\n%s", res.Report())
	}
}

// TestSymbolMismatchIsItsOwnOutcome — отказ предпосылки НЕ маскируется под истечение.
func TestSymbolMismatchIsItsOwnOutcome(t *testing.T) {
	decls := []Declaration{decl("RPC_NO_DELETE", "kacho/cloud/vpc/v1/route_table_service.proto", "RemoveRoutes")}
	res := Adjudicate(loadReal(t), decls)
	if len(res.SymbolMismatch) != 1 {
		t.Fatalf("несовпадений символа: %d, ожидалось 1\n%s", len(res.SymbolMismatch), res.Report())
	}
	if len(res.Expired) != 0 {
		t.Errorf("несовпадение символа отнесено к истечению — исходы слиты: %+v", res.Expired)
	}
	rep := res.Report()
	if !strings.Contains(rep, "СИМВОЛ НЕ СОВПАЛ") || !strings.Contains(rep, "AddRoutes") {
		t.Errorf("отчёт не называет ни исход, ни фактический символ:\n%s", rep)
	}
}

// TestInvalidDeclarationIsRed — перечень, принимающий мусор, послабляет молча.
func TestInvalidDeclarationIsRed(t *testing.T) {
	cases := []struct {
		name string
		d    Declaration
		want string
	}{
		{"ссылка неразрешима", Declaration{Rule: "RPC_NO_DELETE", Path: "a/b.proto", Symbol: "X",
			Reason: "метод отвечает отказом при любом входе", Issue: "#71"}, "issue"},
		{"причина не причина", Declaration{Rule: "RPC_NO_DELETE", Path: "a/b.proto", Symbol: "X",
			Reason: "нужно", Issue: "kacho#246"}, "reason"},
		{"правило не похоже на правило", Declaration{Rule: "rpc-no-delete", Path: "a/b.proto", Symbol: "X",
			Reason: "метод отвечает отказом при любом входе", Issue: "kacho#246"}, "rule"},
		{"путь не контракт", Declaration{Rule: "RPC_NO_DELETE", Path: "a/b.go", Symbol: "X",
			Reason: "метод отвечает отказом при любом входе", Issue: "kacho#246"}, "path"},
		{"символ пуст", Declaration{Rule: "RPC_NO_DELETE", Path: "a/b.proto", Symbol: "",
			Reason: "метод отвечает отказом при любом входе", Issue: "kacho#246"}, "symbol"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := Adjudicate(nil, []Declaration{c.d})
			if len(res.Invalid) != 1 {
				t.Fatalf("негодная запись принята:\n%s", res.Report())
			}
			if !strings.Contains(strings.Join(res.Invalid[0].Problems, " "), c.want) {
				t.Errorf("проблема не названа по полю %q: %v", c.want, res.Invalid[0].Problems)
			}
		})
	}
	// Положительный контроль: годная запись негодной не считается.
	ok := decl("RPC_NO_DELETE", "kacho/cloud/vpc/v1/route_table_service.proto", "AddRoutes")
	if problems := ok.Validate(); len(problems) != 0 {
		t.Errorf("годная запись объявлена негодной: %v", problems)
	}
}

// TestNonJSONOutputIsNotZeroFindings — «проверка не выполнялась» не читается как «находок
// нет». Прецедент записан в самом конвейере: при исчерпании квоты шаги получали skipped,
// а вердикт о них выдавался.
func TestNonJSONOutputIsNotZeroFindings(t *testing.T) {
	_, err := ParseFindings(strings.NewReader("Failure: too many requests\n"))
	if err == nil {
		t.Fatal("непонятный вывод разобран как ноль находок — вердикт был бы выдан о непроверенном")
	}
	if !strings.Contains(err.Error(), "не является объектом JSON") {
		t.Errorf("причина не названа: %v", err)
	}
	// Положительный контроль: пустой ввод — законный исход (разрывов нет).
	got, err := ParseFindings(strings.NewReader("\n  \n"))
	if err != nil || len(got) != 0 {
		t.Errorf("пустой вывод обязан читаться как ноль находок без ошибки: %v %v", got, err)
	}
}

// TestDeclaredListLoadsFromTree — перечень в дереве существует и разбирается. Отсутствие
// файла — НЕ законный исход: тогда «объявлений нет» неотличимо от «перечень не прочитан».
func TestDeclaredListLoadsFromTree(t *testing.T) {
	path := filepath.Join("..", "..", "proto", "declared-breaks.yaml")
	got, err := LoadDeclarations(path)
	if err != nil {
		t.Fatalf("перечень дерева не прочитан: %v", err)
	}
	for i, d := range got {
		if problems := d.Validate(); len(problems) > 0 {
			t.Errorf("запись %d перечня дерева негодна: %v", i+1, problems)
		}
	}
	t.Logf("осмотрено: записей в перечне дерева %d", len(got))

	if _, err := LoadDeclarations(filepath.Join("testdata", "нет-такого-файла.yaml")); err == nil {
		t.Error("отсутствующий перечень прочитан как пустой — «объявлений нет» слилось с «не прочитано»")
	}
}
