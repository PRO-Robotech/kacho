// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// authzcheckoutcomelanes_injection_test.go — ИНЪЕКЦИЯ: обе стороны гоняют ТУ ЖЕ
// функцию, которой судит гейт по дереву.
//
// Дефектная форма и законная отличаются одним: доступна ли ошибка ниже по
// функции. Без стороны «краснеет» гейт утверждал бы свойство, которого не
// проверяет; без стороны «молчит» он ловил бы форму (всякий вопрос к модели), а
// не существо, и первый же ложный срабат его отключил бы.
//
// Входы взяты ДОСЛОВНО из дерева: дефектный — тем, чем был `requireQuotaReader`
// до починки; законные — тем, чем стали он и `authorizeCaller`.

import (
	"strings"
	"testing"
)

// scanSynth — обход синтетического дерева ТЕМ ЖЕ кодом, которым судит гейт,
// включая ВЫВЕДЕНИЕ каталогов из дерева. Перечень не подставляется: подставив
// его, инъекция перестала бы проверять именно то, чем гейт снимался прежде.
func scanSynth(root string) (found []collapsedCheck, questions, files int, err error) {
	roots, rerr := prodGoRoots(root)
	if rerr != nil {
		return nil, 0, 0, rerr
	}
	rep, serr := scanCollapsedRelationChecks(root, roots)
	if serr != nil {
		return nil, 0, 0, serr
	}
	return rep.found, rep.questions, rep.files, nil
}

// collapsedSrc — форма, в которой ошибка НЕВЫРАЗИМА: связана в `Init` условного
// оператора и поглощена конъюнкцией. Так выглядел гейт читателя пределов.
const collapsedSrc = `package limit

import "context"

type checker interface {
	Check(ctx context.Context, subject, relation, object string) (bool, error)
}

func gate(ctx context.Context, c checker, subject string) error {
	if allowed, err := c.Check(ctx, subject, "quota_reader", "cluster:root"); err == nil && allowed {
		return nil
	}
	return errDenied
}

var errDenied = context.Canceled
`

// separatedSrc — та же проверка, тот же порт, тот же вопрос: ошибка связана
// обычным присваиванием, поэтому исходы разведены.
const separatedSrc = `package limit

import "context"

type checker interface {
	Check(ctx context.Context, subject, relation, object string) (bool, error)
}

func gate(ctx context.Context, c checker, subject string) error {
	allowed, err := c.Check(ctx, subject, "quota_reader", "cluster:root")
	if err != nil {
		return errUnavailable
	}
	if !allowed {
		return errDenied
	}
	return nil
}

var errDenied = context.Canceled
var errUnavailable = context.DeadlineExceeded
`

// branchOnErrorSrc — законный близнец ФОРМЫ: ошибка тоже связана в `Init`, но
// условие называет ЕЁ ОДНУ, без конъюнкции, то есть ветвь по ошибке и есть её
// исход. Гейт обязан молчать — иначе он мерил бы место объявления, а не
// выразимость.
const branchOnErrorSrc = `package limit

import "context"

type checker interface {
	Check(ctx context.Context, subject, relation, object string) (bool, error)
}

func gate(ctx context.Context, c checker, subject string) (bool, error) {
	if allowed, err := c.Check(ctx, subject, "quota_reader", "cluster:root"); err != nil {
		return false, err
	} else {
		return allowed, nil
	}
}
`

// discardedSrc — ошибку выбросили явно. Это то же схлопывание: исход один при
// обоих ответах хранилища, только сказано короче.
const discardedSrc = `package limit

import "context"

type checker interface {
	Check(ctx context.Context, subject, relation, object string) (bool, error)
}

func gate(ctx context.Context, c checker, subject string) error {
	if allowed, _ := c.Check(ctx, subject, "quota_reader", "cluster:root"); allowed {
		return nil
	}
	return errDenied
}

var errDenied = context.Canceled
`

// unrelatedCheckSrc — отрицательный контроль РАСПОЗНАВАНИЯ: метод с тем же
// именем, но не вопрос к модели прав (другая арность, первый довод не
// контекст). Гейт обязан не считать его вопросом — иначе перепись раздувается
// и предпосылка «вопросы в дереве есть» перестаёт что-либо значить.
const unrelatedCheckSrc = `package limit

type health struct{}

func (health) Check(name string) (bool, error) { return name != "", nil }

func probe(h health) bool {
	if ok, err := h.Check("x"); err == nil && ok {
		return true
	}
	return false
}
`

// commentOnlySrc — отрицательный контроль ТЕКСТА: дефектная форма, приведённая
// в комментарии и в строковом литерале. Поиск по образцу принял бы объяснение за
// исполнение — тот самый класс, ради которого гейт разбирает дерево.
const commentOnlySrc = `package limit

// Так писать нельзя:
//
//	if allowed, err := c.Check(ctx, subject, rel, obj); err == nil && allowed {
//	    return nil
//	}
const why = "if allowed, err := c.Check(ctx, s, r, o); err == nil && allowed { return nil }"
`

// TestCheckLaneGateRedOnACollapsedOutcome — сторона дефекта: гейт краснеет и
// называет координату.
func TestCheckLaneGateRedOnACollapsedOutcome(t *testing.T) {
	root := synthCarrierTree(t, map[string]string{
		"services/x/internal/apps/kaname/api/limit/gate.go": collapsedSrc,
	})
	found, questions, files, err := scanSynth(root)
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	if files == 0 {
		t.Fatal("синтетическое дерево не прочитано")
	}
	if questions != 1 {
		t.Fatalf("вопросов к модели прав насчитано %d — ожидался один", questions)
	}
	if len(found) != 1 {
		t.Fatalf("схлопывание не найдено: %v", found)
	}
	if !strings.Contains(found[0].file, "gate.go") || found[0].line == 0 {
		t.Fatalf("координата не названа: %+v", found[0])
	}
	if found[0].errName != "err" {
		t.Fatalf("имя ошибки не названо: %q", found[0].errName)
	}
}

// TestCheckLaneGateRedOnADiscardedError — второй вид того же схлопывания: ошибку
// выбросили явно. Без этой стороны гейт чинился бы заменой имени на `_`.
func TestCheckLaneGateRedOnADiscardedError(t *testing.T) {
	root := synthCarrierTree(t, map[string]string{
		"services/x/internal/gate.go": discardedSrc,
	})
	found, _, _, err := scanSynth(root)
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("выброшенная ошибка схлопыванием не засчитана: %v", found)
	}
}

// TestCheckLaneGateSilentWhenTheOutcomesAreSeparated — законный близнец: тот же
// вопрос, ошибка доступна ниже по функции.
func TestCheckLaneGateSilentWhenTheOutcomesAreSeparated(t *testing.T) {
	root := synthCarrierTree(t, map[string]string{
		"services/x/internal/apps/kaname/api/limit/gate.go": separatedSrc,
	})
	found, questions, files, err := scanSynth(root)
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	if files == 0 {
		t.Fatal("синтетическое дерево не прочитано")
	}
	if questions != 1 {
		t.Fatalf("вопрос к модели прав не распознан: насчитано %d", questions)
	}
	if len(found) != 0 {
		t.Fatalf("гейт нашёл дефект в законном дереве: %v", found)
	}
}

// TestCheckLaneGateSilentOnABranchOverTheError — законный близнец ФОРМЫ: ошибка
// связана в `Init`, но условие называет её одну. Ветвь по ошибке и есть исход.
func TestCheckLaneGateSilentOnABranchOverTheError(t *testing.T) {
	root := synthCarrierTree(t, map[string]string{
		"services/x/internal/gate.go": branchOnErrorSrc,
	})
	found, questions, _, err := scanSynth(root)
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	if questions != 1 {
		t.Fatalf("вопрос к модели прав не распознан: насчитано %d", questions)
	}
	if len(found) != 0 {
		t.Fatalf("ветвь ПО ошибке засчитана схлопыванием: %v — гейт мерит место объявления, "+
			"а не выразимость исхода", found)
	}
}

// TestCheckLaneGateIgnoresAnUnrelatedCheck — отрицательный контроль
// распознавания: одноимённый метод другой формы вопросом о правах не является.
func TestCheckLaneGateIgnoresAnUnrelatedCheck(t *testing.T) {
	root := synthCarrierTree(t, map[string]string{
		"services/x/internal/health.go": unrelatedCheckSrc,
	})
	found, questions, _, err := scanSynth(root)
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	if questions != 0 {
		t.Fatalf("вопросом о правах засчитан посторонний метод Check: %d", questions)
	}
	if len(found) != 0 {
		t.Fatalf("посторонний метод объявлен схлопыванием: %v", found)
	}
}

// TestCheckLaneGateIgnoresTheFormInAComment — отрицательный контроль ТЕКСТА.
func TestCheckLaneGateIgnoresTheFormInAComment(t *testing.T) {
	root := synthCarrierTree(t, map[string]string{
		"services/x/internal/doc.go": commentOnlySrc,
	})
	found, questions, files, err := scanSynth(root)
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	if files == 0 {
		t.Fatal("синтетическое дерево не прочитано")
	}
	if questions != 0 || len(found) != 0 {
		t.Fatalf("гейт прочитал УПОМИНАНИЕ формы как её исполнение: вопросов=%d, находок=%v",
			questions, found)
	}
}

// TestCheckLaneGateIgnoresProbes — пробы под гейт не подпадают: дефектную форму
// они цитируют намеренно (см. соседний файл инъекции), и обвинение по ним
// сделало бы гейт неспособным доказать самого себя.
func TestCheckLaneGateIgnoresProbes(t *testing.T) {
	root := synthCarrierTree(t, map[string]string{
		"services/x/internal/gate_test.go": collapsedSrc,
	})
	found, questions, _, err := scanSynth(root)
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	if questions != 0 || len(found) != 0 {
		t.Fatalf("проба осмотрена как прод-код: вопросов=%d, находок=%v", questions, found)
	}
}

// ── формы, которыми гейт снимался у рецензента ──────────────────────────────

// separateAssignCollapsedSrc — ФОРМА B: раздельное присваивание, самая
// идиоматичная запись того же дефекта. Ошибка доступна ниже по функции, но
// НИ ОДНО её употребление ветвью по ней не является.
//
// Взято дословно из дерева: так выглядел `fgaHoldsScopeAdmin`, питавший шесть
// списочных путей. Прежняя редакция гейта считала это ВОПРОСОМ и молчала.
const separateAssignCollapsedSrc = `package ab

import "context"

type checker interface {
	Check(ctx context.Context, subject, relation, object string) (bool, error)
}

func holds(ctx context.Context, c checker, subject, object string) bool {
	allowed, err := c.Check(ctx, subject, "admin", object)
	return err == nil && allowed
}
`

// separateAssignNestedSrc — та же форма B, но схлопывание спрятано в условии
// глубже по телу: `if cerr == nil && allowed { return nil }`, а ниже безусловный
// отказ. Второй живой экземпляр из дерева (`list_all_operations`).
const separateAssignNestedSrc = `package acct

import "context"

type checker interface {
	Check(ctx context.Context, subject, relation, object string) (bool, error)
}

func authorize(ctx context.Context, c checker, subject, object string) error {
	allowed, cerr := c.Check(ctx, subject, "admin", object)
	if cerr == nil && allowed {
		return nil
	}
	return errDeniedB
}

var errDeniedB = context.Canceled
`

// separateAssignLegitSrc — законный близнец ФОРМЫ B: то же раздельное
// присваивание, но ошибка уходит ЗНАЧЕНИЕМ. Без этой стороны гейт краснел бы на
// всяком раздельном присваивании, то есть ловил бы запись, а не свойство.
const separateAssignLegitSrc = `package ab

import "context"

type checker interface {
	Check(ctx context.Context, subject, relation, object string) (bool, error)
}

func holds(ctx context.Context, c checker, subject, object string) (bool, error) {
	allowed, err := c.Check(ctx, subject, "admin", object)
	if err != nil {
		return false, err
	}
	return allowed, nil
}
`

// renamedCtxSrc — ТО ЖЕ схлопывание, контекст назван `cctx`. Именно этим
// написанием рецензент снял прежний гейт: перепись просела с 24 до 23, площадка
// уехала из наблюдения, вердикт стал зелёным. Написание в дереве живое —
// `pkg/authz/interceptor.go` пользуется им сегодня.
const renamedCtxSrc = `package authz

import "context"

type checker interface {
	Check(ctx context.Context, subject, relation, object string) (bool, error)
}

func gate(cctx context.Context, c checker, subject string) error {
	if allowed, err := c.Check(cctx, subject, "viewer", "cluster:root"); err == nil && allowed {
		return nil
	}
	return errDeniedC
}

var errDeniedC = context.Canceled
`

// generatedSrc — порождённый файл с вызовом той же формы. Вопросом о правах он
// не является: у него нет автора, которому адресован упрёк, а `Check` в нём —
// заглушка транспорта. Прежняя перепись включала два таких файла и завышала
// число вопросов.
const generatedSrc = `// Code generated by protoc-gen-grpc-gateway. DO NOT EDIT.

package genpb

import "context"

type checker interface {
	Check(ctx context.Context, subject, relation, object string) (bool, error)
}

func request(ctx context.Context, c checker, subject string) bool {
	allowed, _ := c.Check(ctx, subject, "viewer", "cluster:root")
	return allowed
}
`

// TestCheckLaneGateRedOnASeparateAssignment — Б1: форма B краснеет.
func TestCheckLaneGateRedOnASeparateAssignment(t *testing.T) {
	root := synthCarrierTree(t, map[string]string{
		"services/x/internal/apps/kaname/api/access_binding/helpers.go": separateAssignCollapsedSrc,
		"services/x/internal/apps/kaname/api/account/list_all.go":       separateAssignNestedSrc,
	})
	found, questions, _, err := scanSynth(root)
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	if questions != 2 {
		t.Fatalf("вопросов насчитано %d — ожидалось два", questions)
	}
	if len(found) != 2 {
		t.Fatalf("раздельное присваивание схлопыванием не засчитано: %v — гейт держит подформу "+
			"класса, а сообщение коммита обещает класс", found)
	}
	for _, c := range found {
		if c.line == 0 || c.file == "" {
			t.Fatalf("координата не названа: %+v", c)
		}
	}
}

// TestCheckLaneGateSilentWhenTheErrorLeavesAsAValue — законный близнец формы B.
func TestCheckLaneGateSilentWhenTheErrorLeavesAsAValue(t *testing.T) {
	root := synthCarrierTree(t, map[string]string{
		"services/x/internal/ab/holds.go": separateAssignLegitSrc,
	})
	found, questions, _, err := scanSynth(root)
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	if questions != 1 {
		t.Fatalf("вопрос не распознан: %d", questions)
	}
	if len(found) != 0 {
		t.Fatalf("гейт краснеет на законном раздельном присваивании: %v — он ловит запись, "+
			"а не выразимость исхода", found)
	}
}

// TestCheckLaneGateSurvivesRenamingTheContext — Б2: переименование переменной
// гейт не снимает.
func TestCheckLaneGateSurvivesRenamingTheContext(t *testing.T) {
	root := synthCarrierTree(t, map[string]string{
		"services/x/internal/authz/gate.go": renamedCtxSrc,
	})
	found, questions, _, err := scanSynth(root)
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	if questions != 1 {
		t.Fatalf("вопрос с контекстом по имени cctx не распознан: насчитано %d — "+
			"узнавание привязано к имени переменной, а не к вызываемому", questions)
	}
	if len(found) != 1 {
		t.Fatalf("схлопывание уехало из наблюдения переименованием переменной: %v", found)
	}
}

// TestCheckLaneGateWatchesEveryProdRootOfTheTree — сужение области невозможно:
// перечень каталогов ВЫВОДИТСЯ из дерева.
//
// Прежняя редакция держала литерал из трёх имён и проверяла лишь, что
// объявленный каталог existует. Вычеркнув из литерала два имени, рецензент
// получил PASS при одном прочитанном каталоге. Здесь схлопывание кладётся в
// каталог, которого в том литерале не было ВОВСЕ (`tools`), — и обязано быть
// найдено.
func TestCheckLaneGateWatchesEveryProdRootOfTheTree(t *testing.T) {
	root := synthCarrierTree(t, map[string]string{
		"tools/x/gate.go":     separateAssignCollapsedSrc,
		"terraform/y/gate.go": renamedCtxSrc,
	})
	roots, err := prodGoRoots(root)
	if err != nil {
		t.Fatalf("вывод каталогов: %v", err)
	}
	if len(roots) != 2 {
		t.Fatalf("каталоги выведены неверно: %v", roots)
	}
	found, questions, _, serr := scanSynth(root)
	if serr != nil {
		t.Fatalf("обход: %v", serr)
	}
	if questions != 2 || len(found) != 2 {
		t.Fatalf("каталог вне прежнего литерала остался без наблюдения: вопросов=%d, находок=%v",
			questions, found)
	}
}

// TestCheckLaneGateDoesNotCountGeneratedFiles — перепись не завышается
// порождённым кодом.
func TestCheckLaneGateDoesNotCountGeneratedFiles(t *testing.T) {
	root := synthCarrierTree(t, map[string]string{
		"pkg/api/x/service.pb.gw.go": generatedSrc,
	})
	found, questions, files, err := scanSynth(root)
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if questions != 0 || len(found) != 0 {
		t.Fatalf("порождённый файл засчитан вопросом о правах: вопросов=%d, находок=%v",
			questions, found)
	}
	if files != 0 {
		t.Fatalf("порождённый файл засчитан прочитанным прод-файлом: %d", files)
	}
}

// TestCheckLaneGateFailsOnAnEmptyTree — предпосылка обхода: пустое дерево не
// выдаётся за чистое.
func TestCheckLaneGateFailsOnAnEmptyTree(t *testing.T) {
	root := synthCarrierTree(t, map[string]string{"docs/readme.md": "нет кода"})
	roots, err := prodGoRoots(root)
	if err != nil {
		t.Fatalf("вывод каталогов: %v", err)
	}
	if len(roots) != 0 {
		t.Fatalf("каталоги с кодом Go выведены из дерева, где его нет: %v", roots)
	}
}

// typeCheckerSrc — одноимённый метод ТОЙ ЖЕ арности, вопросом о правах не
// являющийся: проверяльщик типов Go. Первым доводом у него поле, а не
// переменная-контекст.
//
// Взято из дерева дословно (`tools/unreadfieldaudit`): расширив узнавание до
// класса, я снял привязку к имени `ctx` — и этот вызов немедленно попал в
// перепись и в находки. Перепись по дереву показала, что он там ровно один и
// единственный отличается формой первого довода.
const typeCheckerSrc = `package idx

type conf struct{}

func (conf) Check(path string, a, b, c int) (bool, error) { return path != "", nil }

type pkg struct{ ImportPath string }

func run(cfg conf, p pkg, fset, syn, info int) {
	_, _ = cfg.Check(p.ImportPath, fset, syn, info)
}
`

// TestCheckLaneGateIgnoresATypeCheckerOfTheSameArity — отрицательный контроль
// РАСПОЗНАВАНИЯ для расширенной формы: снятие привязки к имени `ctx` не должно
// втягивать посторонние методы.
func TestCheckLaneGateIgnoresATypeCheckerOfTheSameArity(t *testing.T) {
	root := synthCarrierTree(t, map[string]string{
		"tools/unreadfieldaudit/index.go": typeCheckerSrc,
	})
	found, questions, files, err := scanSynth(root)
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if files == 0 {
		t.Fatal("синтетическое дерево не прочитано")
	}
	if questions != 0 {
		t.Fatalf("проверяльщик типов засчитан вопросом о правах: %d — перепись раздувается, "+
			"и предпосылка «вопросы в дереве есть» перестаёт что-либо значить", questions)
	}
	if len(found) != 0 {
		t.Fatalf("проверяльщик типов объявлен схлопыванием: %v", found)
	}
}
