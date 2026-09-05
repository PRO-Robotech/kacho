// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// authzwrapperoutcomelanes_injection_test.go — ИНЪЕКЦИЯ: обе стороны гоняют ТУ
// ЖЕ функцию, которой судит гейт по дереву, включая ВЫВЕДЕНИЕ пар из дерева.
//
// Перечень пар не подставляется. Подставив его, инъекция перестала бы проверять
// ровно то, чем гейт снимается дешевле всего: сузить вывод пар.
//
// Входы взяты ДОСЛОВНО с площадок задачи #1045: дефектный — тем, чем был
// `requireGrantAuthority`; законный — тем, чем стал `fgaHoldsAdminE`.

import (
	"strings"
	"testing"
)

// scanWrapperSynth — обход синтетического дерева ТЕМ ЖЕ кодом, что и гейт.
func scanWrapperSynth(t *testing.T, root string) wrapperScanReport {
	t.Helper()
	roots, err := prodGoRoots(root)
	if err != nil {
		t.Fatalf("вывод каталогов: %v", err)
	}
	rep, serr := scanBoolWrapperCalls(root, roots)
	if serr != nil {
		t.Fatalf("обход синтетического дерева: %v", serr)
	}
	return rep
}

// synthGoMod — синтетическое дерево обязано нести go.mod: путь модуля читается
// из него, а не выписывается.
const synthGoMod = "module example.test/synth\n\ngo 1.25\n"

// guardPkgSrc — пакет-владелец, объявивший ОБЕ формы вопроса. Один в один
// authzguard: булева половина зовёт форму с исходом и роняет ошибку.
const guardPkgSrc = `package guard

import "context"

type RelationChecker interface {
	Check(ctx context.Context, subject, relation, object string) (bool, error)
}

func IsClusterAdmin(ctx context.Context, c RelationChecker) bool {
	admin, _ := IsClusterAdminE(ctx, c)
	return admin
}

func IsClusterAdminE(ctx context.Context, c RelationChecker) (bool, error) {
	if c == nil {
		return false, nil
	}
	return c.Check(ctx, "user:x", "system_admin", "cluster:root")
}
`

// callerBoolSrc — ДЕФЕКТ: чужой пакет зовёт булеву половину.
const callerBoolSrc = `package ab

import (
	"context"

	"example.test/synth/services/iam/internal/guard"
)

func requireGrantAuthority(ctx context.Context, c guard.RelationChecker) error {
	if guard.IsClusterAdmin(ctx, c) {
		return nil
	}
	return errDenied
}

var errDenied = context.Canceled
`

// callerOutcomeSrc — ЗАКОННЫЙ БЛИЗНЕЦ: тот же пакет, тот же вопрос, тот же
// порт — выбрана форма с исходом. Без этой стороны гейт ловил бы всякое
// обращение к пакету-владельцу, а не выбор формы.
const callerOutcomeSrc = `package ab

import (
	"context"

	"example.test/synth/services/iam/internal/guard"
)

func requireGrantAuthority(ctx context.Context, c guard.RelationChecker) error {
	admin, err := guard.IsClusterAdminE(ctx, c)
	if err != nil {
		return errUnavailable
	}
	if admin {
		return nil
	}
	return errDeniedB
}

var errDeniedB = context.Canceled
var errUnavailable = context.DeadlineExceeded
`

// callerAliasedSrc — ДЕФЕКТ под алиасом импорта. Узнавание идёт по РАЗРЕШЁННОМУ
// импорту, а не по написанию имени пакета, иначе гейт снимался бы алиасом.
const callerAliasedSrc = `package acct

import (
	"context"

	az "example.test/synth/services/iam/internal/guard"
)

func requireAccountViewAuthority(ctx context.Context, c az.RelationChecker) error {
	if az.IsClusterAdmin(ctx, c) {
		return nil
	}
	return errDeniedC
}

var errDeniedC = context.Canceled
`

// unpairedBoolPkgSrc — ОТРИЦАТЕЛЬНЫЙ КОНТРОЛЬ ВЫВОДА: булева функция БЕЗ парной
// формы. Автор третьего исхода не объявлял, значит выбора у вызывающего нет и
// обвинять его не в чем. Без этой стороны гейт краснел бы на всякой булевой
// функции дерева — то есть ловил бы форму, а не выбор.
const unpairedBoolPkgSrc = `package plainguard

import "context"

func IsSelf(ctx context.Context, id string) bool { return ctx != nil && id != "" }
`

const callerUnpairedSrc = `package svc

import (
	"context"

	"example.test/synth/services/iam/internal/plainguard"
)

func gate(ctx context.Context, id string) bool { return plainguard.IsSelf(ctx, id) }
`

// wrongShapeEPkgSrc — ОТРИЦАТЕЛЬНЫЙ КОНТРОЛЬ ФОРМЫ: соседка называется на `E`,
// но возвращает НЕ `(bool, error)`. Парой она не является, и совпадение имён
// парой её делать не должно.
const wrongShapeEPkgSrc = `package shapeguard

import "context"

func IsAdmin(ctx context.Context) bool           { return ctx != nil }
func IsAdminE(ctx context.Context) (string, bool) { return "", ctx != nil }
`

const callerWrongShapeSrc = `package svc2

import (
	"context"

	"example.test/synth/services/iam/internal/shapeguard"
)

func gate(ctx context.Context) bool { return shapeguard.IsAdmin(ctx) }
`

// mentionOnlySrc — ОТРИЦАТЕЛЬНЫЙ КОНТРОЛЬ ТЕКСТА: дефектная форма в комментарии
// и в строке. Поиск по образцу принял бы объяснение за исполнение.
const mentionOnlySrc = `package doc

// Так писать нельзя:
//
//	if guard.IsClusterAdmin(ctx, c) { return nil }
const why = "guard.IsClusterAdmin(ctx, c)"
`

// guardTree — пакет-владелец плюс произвольные файлы вызывающих.
func guardTree(t *testing.T, extra map[string]string) string {
	t.Helper()
	files := map[string]string{
		"go.mod":                               synthGoMod,
		"services/iam/internal/guard/guard.go": guardPkgSrc,
	}
	for k, v := range extra {
		files[k] = v
	}
	return synthCarrierTree(t, files)
}

// TestWrapperGateRedOnABoolCallFromAnotherPackage — сторона дефекта: краснеет и
// называет координату.
func TestWrapperGateRedOnABoolCallFromAnotherPackage(t *testing.T) {
	rep := scanWrapperSynth(t, guardTree(t, map[string]string{
		"services/iam/internal/apps/kaname/api/access_binding/helpers.go": callerBoolSrc,
	}))
	if rep.files == 0 {
		t.Fatal("синтетическое дерево не прочитано")
	}
	if len(rep.pairs) != 1 {
		t.Fatalf("пар выведено %d, ожидалась одна: %+v", len(rep.pairs), rep.pairs)
	}
	if len(rep.found) != 1 {
		t.Fatalf("вызов булевой половины из чужого пакета не найден: %+v", rep.found)
	}
	if !strings.Contains(rep.found[0].file, "helpers.go") || rep.found[0].line == 0 {
		t.Fatalf("координата не названа: %+v", rep.found[0])
	}
	if rep.found[0].pair.outcomeForms() != "IsClusterAdminE" {
		t.Fatalf("парная форма не названа: %q", rep.found[0].pair.outcomeForms())
	}
}

// TestWrapperGateSilentWhenTheOutcomeFormIsChosen — законный близнец: тот же
// пакет, тот же порт, выбрана форма с исходом.
func TestWrapperGateSilentWhenTheOutcomeFormIsChosen(t *testing.T) {
	rep := scanWrapperSynth(t, guardTree(t, map[string]string{
		"services/iam/internal/apps/kaname/api/access_binding/helpers.go": callerOutcomeSrc,
	}))
	if len(rep.pairs) != 1 {
		t.Fatalf("пара не выведена: %+v", rep.pairs)
	}
	if len(rep.found) != 0 {
		t.Fatalf("гейт краснеет на форме С ИСХОДОМ: %+v — он ловит обращение к пакету, "+
			"а не выбор формы", rep.found)
	}
}

// TestWrapperGateSurvivesAnImportAlias — снятие алиасом не проходит.
func TestWrapperGateSurvivesAnImportAlias(t *testing.T) {
	rep := scanWrapperSynth(t, guardTree(t, map[string]string{
		"services/iam/internal/apps/kaname/api/account/list_all_operations.go": callerAliasedSrc,
	}))
	if len(rep.found) != 1 {
		t.Fatalf("вызов под алиасом импорта уехал из наблюдения: %+v — узнавание привязано "+
			"к написанию имени пакета, а не к разрешённому импорту", rep.found)
	}
}

// TestWrapperGateIgnoresTheOwningPackageItself — вызов ВНУТРИ пакета-владельца
// под гейт не подпадает: там булева половина и есть тело обёртки.
func TestWrapperGateIgnoresTheOwningPackageItself(t *testing.T) {
	rep := scanWrapperSynth(t, guardTree(t, nil))
	if len(rep.pairs) != 1 {
		t.Fatalf("пара не выведена: %+v", rep.pairs)
	}
	if len(rep.found) != 0 {
		t.Fatalf("пакет-владелец обвинён в собственной реализации: %+v", rep.found)
	}
}

// TestWrapperGateIgnoresABoolWithoutAPair — отрицательный контроль ВЫВОДА.
func TestWrapperGateIgnoresABoolWithoutAPair(t *testing.T) {
	rep := scanWrapperSynth(t, synthCarrierTree(t, map[string]string{
		"go.mod": synthGoMod,
		"services/iam/internal/plainguard/plain.go":  unpairedBoolPkgSrc,
		"services/iam/internal/apps/kaname/svc/g.go": callerUnpairedSrc,
	}))
	if len(rep.pairs) != 0 {
		t.Fatalf("парой объявлена булева функция без формы с исходом: %+v", rep.pairs)
	}
	if len(rep.found) != 0 {
		t.Fatalf("обвинён вызов булевой функции, у которой ВЫБОРА НЕТ: %+v", rep.found)
	}
}

// TestWrapperGateIgnoresAnEShapedNeighbourOfTheWrongType — отрицательный
// контроль ФОРМЫ: имя на `E` парой само по себе не делает.
func TestWrapperGateIgnoresAnEShapedNeighbourOfTheWrongType(t *testing.T) {
	rep := scanWrapperSynth(t, synthCarrierTree(t, map[string]string{
		"go.mod":                                synthGoMod,
		"services/iam/internal/shapeguard/s.go": wrongShapeEPkgSrc,
		"services/iam/internal/apps/kaname/svc2/g.go": callerWrongShapeSrc,
	}))
	if len(rep.pairs) != 0 {
		t.Fatalf("парой объявлена соседка, возвращающая не (bool, error): %+v", rep.pairs)
	}
	if len(rep.found) != 0 {
		t.Fatalf("находка при отсутствии пары: %+v", rep.found)
	}
}

// TestWrapperGateIgnoresTheFormInAComment — отрицательный контроль ТЕКСТА.
func TestWrapperGateIgnoresTheFormInAComment(t *testing.T) {
	rep := scanWrapperSynth(t, guardTree(t, map[string]string{
		"services/iam/internal/apps/kaname/api/doc/doc.go": mentionOnlySrc,
	}))
	if rep.files == 0 {
		t.Fatal("синтетическое дерево не прочитано")
	}
	if len(rep.found) != 0 {
		t.Fatalf("гейт прочитал УПОМИНАНИЕ формы как её исполнение: %+v", rep.found)
	}
}

// TestWrapperGateIgnoresProbesAndGeneratedCode — пробы и порождённое не
// осматриваются: первые цитируют дефектную форму намеренно (этот файл — тоже),
// у второго нет автора, которому адресован упрёк.
func TestWrapperGateIgnoresProbesAndGeneratedCode(t *testing.T) {
	generated := "// Code generated by protoc-gen-go. DO NOT EDIT.\n\n" + callerBoolSrc
	rep := scanWrapperSynth(t, guardTree(t, map[string]string{
		"services/iam/internal/apps/kaname/api/access_binding/helpers_test.go": callerBoolSrc,
		"pkg/api/iam/v1/service.pb.go":                                         generated,
	}))
	if len(rep.found) != 0 {
		t.Fatalf("проба либо порождённый файл осмотрены как прод-код: %+v", rep.found)
	}
	if rep.generated != 1 {
		t.Fatalf("порождённых пропущено %d, ожидался один", rep.generated)
	}
}

// TestWrapperGateWatchesEveryProdRootOfTheTree — область не сужается: перечень
// каталогов ВЫВОДИТСЯ из дерева, поэтому дефект в каталоге, о котором никто не
// думал, всё равно находится.
func TestWrapperGateWatchesEveryProdRootOfTheTree(t *testing.T) {
	rep := scanWrapperSynth(t, guardTree(t, map[string]string{
		"tools/audit/gate.go": callerBoolSrc,
		"terraform/x/gate.go": callerAliasedSrc,
	}))
	if len(rep.found) != 2 {
		t.Fatalf("каталог вне привычного набора остался без наблюдения: %+v", rep.found)
	}
}

// TestWrapperGatePremiseFailsWhenNoPairExists — предпосылка гейта: пары в
// дереве есть. Дерево без пар судить нечем, и гейт обязан это ЗАЯВИТЬ, а не
// молча зеленеть.
//
// Здесь проверяется сам ФАКТ, на который опирается `t.Fatalf` в гейте: на
// дереве без пар перепись даёт ноль, то есть вердикт был бы вынесен о пустоте.
func TestWrapperGatePremiseFailsWhenNoPairExists(t *testing.T) {
	rep := scanWrapperSynth(t, synthCarrierTree(t, map[string]string{
		"go.mod": synthGoMod,
		"services/iam/internal/plainguard/plain.go": unpairedBoolPkgSrc,
	}))
	if rep.files == 0 {
		t.Fatal("синтетическое дерево не прочитано — предпосылка проверялась бы на пустоте")
	}
	if len(rep.pairs) != 0 {
		t.Fatalf("пары выведены там, где их нет: %+v", rep.pairs)
	}
}
