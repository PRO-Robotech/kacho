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
		"services/x/internal/apps/kacho/api/limit/gate.go": collapsedSrc,
	})
	found, questions, files, _, err := scanCollapsedRelationChecks(root)
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
	found, _, _, _, err := scanCollapsedRelationChecks(root)
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
		"services/x/internal/apps/kacho/api/limit/gate.go": separatedSrc,
	})
	found, questions, files, _, err := scanCollapsedRelationChecks(root)
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
	found, questions, _, _, err := scanCollapsedRelationChecks(root)
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
	found, questions, _, _, err := scanCollapsedRelationChecks(root)
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
	found, questions, files, _, err := scanCollapsedRelationChecks(root)
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
	found, questions, _, _, err := scanCollapsedRelationChecks(root)
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	if questions != 0 || len(found) != 0 {
		t.Fatalf("проба осмотрена как прод-код: вопросов=%d, находок=%v", questions, found)
	}
}
