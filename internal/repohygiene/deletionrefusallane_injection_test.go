// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene_test

// Доказательство, что гейт полос отказа на снятие СПОСОБЕН упасть — и способен
// смолчать.
//
// Каждая ось проверяется ПАРОЙ: внесённый дефект обязан дать находку С
// КООРДИНАТОЙ, а законный близнец той же формы — молчание. Без второй половины
// гейт ловил бы форму, а не существо, и первый же ложный срабат его отключил бы
// (testing.md §Гейт на класс, п.2).
//
// Инъекция роняет ТОЛЬКО проверяемое (п.2в): у каждой оси близнец отличается от
// дефекта ОДНИМ названным фактом, а не набором. Поэтому дефект одной оси не
// краснит соседнюю, и молчание соседней остаётся вердиктом, а не совпадением.
//
// Дерево синтетическое: оно НЕ собирается и собираться не должно. Гейт судит по
// узлам разбора, поэтому сборка ему не нужна, — и это же делает инъекцию
// независимой от того, что лежит в настоящем дереве сегодня.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/internal/repohygiene"
)

// laneProbeTree раскладывает синтетические файлы и отдаёт их гейту.
func laneProbeTree(t *testing.T, files map[string]string) (repohygiene.DeletionRefusalFacts, []string, []string) {
	t.Helper()
	root := t.TempDir()
	var paths []string
	for rel, body := range files {
		full := filepath.Join(root, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o750))
		require.NoError(t, os.WriteFile(full, []byte(body), 0o600))
		paths = append(paths, full)
	}
	serviceOf := func(p string) string {
		s := filepath.ToSlash(p)
		i := strings.Index(s, "/services/")
		if i < 0 {
			return ""
		}
		return strings.Split(s[i+len("/services/"):], "/")[0]
	}
	shorten := func(p string) string {
		s := filepath.ToSlash(p)
		if i := strings.Index(s, "/services/"); i >= 0 {
			return s[i+1:]
		}
		return filepath.Base(s)
	}
	facts, parseErrs := repohygiene.ScanDeletionRefusalLanes(paths, serviceOf, shorten)
	require.Empty(t, parseErrs, "синтетический файл не разобрался — инъекция утверждала бы не о том")
	var services []string
	for _, p := range paths {
		if svc := serviceOf(p); svc != "" && !contains(services, svc) {
			services = append(services, svc)
		}
	}
	findings, stale := repohygiene.DeletionRefusalFindings(facts, services, nil)
	return facts, findings, stale
}

func contains(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

// laneDictionaryProducers — файл, называющий все четыре полосы и зовущий Attach.
//
// Стоит РЯДОМ с каждым случаем инъекции намеренно: без него любая проба краснела
// бы находками «полоса без производителя» и «служба не зовёт Attach», то есть
// инъекция утверждала бы про соседнюю ось, а не про свою (п.2в).
const laneDictionaryProducers = `package p

import "github.com/PRO-Robotech/kacho/pkg/refusal"

var ErrFailedPrecondition = e()

func e() error { return nil }

func all(err error) error {
	_ = refusal.Wrap(refusal.Protected, refusal.Ref{}, ErrFailedPrecondition)
	_ = refusal.Wrap(refusal.HoldsChildren, refusal.Ref{}, ErrFailedPrecondition)
	_ = refusal.Wrap(refusal.ReferredUnnamed, refusal.Ref{}, ErrFailedPrecondition)
	_ = refusal.Wrap(refusal.ReferredTo, refusal.Ref{}, ErrFailedPrecondition)
	out, _ := refusal.Attach(err, refusal.Ref{Service: "vpc"}, 0, "msg")
	return out
}
`

func TestDeletionRefusalGateFailsOnAnUnlanedRefusal(t *testing.T) {
	t.Parallel()

	const defect = `package p

import "fmt"

var ErrFailedPrecondition = fmt.Errorf("failed precondition")

func del(id string) error {
	return fmt.Errorf("%w: Network %s is not empty", ErrFailedPrecondition, id)
}
`
	const twin = `package p

import (
	"fmt"

	"github.com/PRO-Robotech/kacho/pkg/refusal"
)

var ErrFailedPrecondition = fmt.Errorf("failed precondition")

func del(id string) error {
	return refusal.Wrap(refusal.ReferredTo, refusal.Ref{ResourceID: id},
		fmt.Errorf("%w: Network %s is not empty", ErrFailedPrecondition, id))
}
`
	_, findings, _ := laneProbeTree(t, map[string]string{
		"services/vpc/internal/repo/producers.go": laneDictionaryProducers,
		"services/vpc/internal/repo/defect.go":    defect,
	})
	require.Len(t, findings, 1, "инъекция обязана дать РОВНО одну находку — свою, а не соседнюю: %v", findings)
	require.Contains(t, findings[0], "services/vpc/internal/repo/defect.go:8",
		"находка обязана называть координату, иначе читателю нечего открыть")
	require.Contains(t, findings[0], "Network %s is not empty")

	_, twinFindings, _ := laneProbeTree(t, map[string]string{
		"services/vpc/internal/repo/producers.go": laneDictionaryProducers,
		"services/vpc/internal/repo/twin.go":      twin,
	})
	require.Empty(t, twinFindings, "законный близнец обязан молчать: %v", twinFindings)
}

func TestDeletionRefusalGateFailsWhenALaneHasNoProducer(t *testing.T) {
	t.Parallel()

	// Дефект отличается от близнеца ОДНИМ фактом: у InUse снят производитель.
	defect := strings.Replace(laneDictionaryProducers,
		"\t_ = refusal.Wrap(refusal.ReferredTo, refusal.Ref{}, ErrFailedPrecondition)\n", "", 1)
	require.NotEqual(t, laneDictionaryProducers, defect, "инъекция не изменила фикстуру — она утверждала бы о близнеце")

	_, findings, _ := laneProbeTree(t, map[string]string{
		"services/vpc/internal/repo/producers.go": defect,
	})
	require.Len(t, findings, 1, "ожидалась ровно одна находка: %v", findings)
	require.Contains(t, findings[0], "refusal.ReferredTo")
	require.Contains(t, findings[0], "мертва")

	_, twinFindings, _ := laneProbeTree(t, map[string]string{
		"services/vpc/internal/repo/producers.go": laneDictionaryProducers,
	})
	require.Empty(t, twinFindings, "полный словарь производителей обязан молчать: %v", twinFindings)
}

func TestDeletionRefusalGateFailsWhenTheLaneIsMintedAndNeverAttached(t *testing.T) {
	t.Parallel()

	// ОДИН факт разницы: снят вызов Attach; полосы названы те же.
	defect := strings.Replace(laneDictionaryProducers,
		"\tout, _ := refusal.Attach(err, refusal.Ref{Service: \"vpc\"}, 0, \"msg\")\n\treturn out\n",
		"\t_ = err\n\treturn nil\n", 1)

	_, findings, _ := laneProbeTree(t, map[string]string{
		"services/nlb/internal/repo/producers.go": defect,
	})
	require.Len(t, findings, 1, "ожидалась ровно одна находка: %v", findings)
	require.Contains(t, findings[0], "nlb")
	require.Contains(t, findings[0], "refusal.Attach")

	_, twinFindings, _ := laneProbeTree(t, map[string]string{
		"services/nlb/internal/repo/producers.go": laneDictionaryProducers,
	})
	require.Empty(t, twinFindings, "служба, зовущая Attach, обязана молчать: %v", twinFindings)
}

func TestDeletionRefusalGateFailsWhenTheLaneRidesAForeignSentinel(t *testing.T) {
	t.Parallel()

	// ОДИН факт разницы: полоса наклеена на sentinel ОТСУТСТВИЯ вместо предусловия.
	defect := strings.Replace(laneDictionaryProducers,
		"refusal.Wrap(refusal.Protected, refusal.Ref{}, ErrFailedPrecondition)",
		"refusal.Wrap(refusal.Protected, refusal.Ref{}, ErrNotFound)", 1)
	defect = strings.Replace(defect, "var ErrFailedPrecondition = e()",
		"var ErrFailedPrecondition = e()\n\nvar ErrNotFound = e()", 1)

	_, findings, _ := laneProbeTree(t, map[string]string{
		"services/vpc/internal/repo/producers.go": defect,
	})
	require.Len(t, findings, 1, "ожидалась ровно одна находка: %v", findings)
	require.Contains(t, findings[0], "ErrNotFound")
	require.Contains(t, findings[0], "чужим кодом")

	_, twinFindings, _ := laneProbeTree(t, map[string]string{
		"services/vpc/internal/repo/producers.go": laneDictionaryProducers,
	})
	require.Empty(t, twinFindings, "полоса поверх предусловия обязана молчать: %v", twinFindings)
}

func TestDeletionRefusalGateFailsWhenAnExemptionHasNothingToExempt(t *testing.T) {
	t.Parallel()

	facts, _, _ := laneProbeTree(t, map[string]string{
		"services/vpc/internal/repo/producers.go": laneDictionaryProducers,
	})

	// ПОЛОЖИТЕЛЬНАЯ ПОЛОВИНА: запись, чей литерал в дереве есть, молчит.
	live := []repohygiene.DeletionRefusalExemption{{
		File: "services/vpc/internal/repo/live.go",
		Text: "%w: Volume %s is in use",
		Why:  "отказ на привязку",
	}}
	liveFacts, _, _ := laneProbeTree(t, map[string]string{
		"services/vpc/internal/repo/producers.go": laneDictionaryProducers,
		"services/vpc/internal/repo/live.go": `package p

import "fmt"

var ErrFailedPrecondition = fmt.Errorf("x")

func attach(id string) error {
	return fmt.Errorf("%w: Volume %s is in use", ErrFailedPrecondition, id)
}
`,
	})
	_, stale := repohygiene.DeletionRefusalFindings(liveFacts, []string{"vpc"}, live)
	require.Empty(t, stale, "запись с живым предметом обязана молчать: %v", stale)

	// ОТРИЦАТЕЛЬНАЯ ПОЛОВИНА: та же запись на дереве БЕЗ её литерала — находка.
	_, stale = repohygiene.DeletionRefusalFindings(facts, []string{"vpc"}, live)
	require.Len(t, stale, 1, "послабление без предмета обязано истекать само: %v", stale)
	require.Contains(t, stale[0], "нечего исключать")
}

func TestDeletionRefusalGateReadsNodesNotProse(t *testing.T) {
	t.Parallel()

	// Комментарий, дословно называющий полосу и отказ, полосой НЕ является:
	// гейт по тексту зеленел бы на прозе о самом себе.
	const prose = `package p

import "fmt"

var ErrFailedPrecondition = fmt.Errorf("x")

// Здесь мог бы стоять refusal.Wrap(refusal.HoldsChildren, …) — но его нет, и отказ
// ниже уходит клиенту без признака.
func del(id string) error {
	return fmt.Errorf("%w: registry is not empty", ErrFailedPrecondition)
}
`
	_, findings, _ := laneProbeTree(t, map[string]string{
		"services/registry/internal/repo/producers.go": laneDictionaryProducers,
		"services/registry/internal/repo/prose.go":     prose,
	})
	require.Len(t, findings, 1, "проза о полосе не делает отказ помеченным: %v", findings)
	require.Contains(t, findings[0], "prose.go")
}

func TestDeletionRefusalGateAcceptsALaneDeclaredByTheErrorType(t *testing.T) {
	t.Parallel()

	// Типизированный отказ: текст в Error(), полоса в Unwrap(). Позиционной
	// вложенности между ними нет, и без правила о типе гейт краснел бы на верном
	// коде.
	const typed = `package p

import (
	"fmt"

	"github.com/PRO-Robotech/kacho/pkg/refusal"
)

var ErrFailedPrecondition = fmt.Errorf("x")

type ErrKeyInUse struct{ KeyID string }

func (e *ErrKeyInUse) Error() string {
	return fmt.Sprintf("guest access key %s is attached to instances: %s", e.KeyID, "i-1")
}

func (e *ErrKeyInUse) Unwrap() error {
	return refusal.Wrap(refusal.ReferredTo, refusal.Ref{ResourceID: e.KeyID}, ErrFailedPrecondition)
}
`
	_, findings, _ := laneProbeTree(t, map[string]string{
		"services/compute/internal/repo/producers.go": laneDictionaryProducers,
		"services/compute/internal/repo/typed.go":     typed,
	})
	require.Empty(t, findings, "полоса, объявленная Unwrap'ом типа, обязана покрывать его Error(): %v", findings)

	// ОТРИЦАТЕЛЬНАЯ ПОЛОВИНА, отличающаяся ОДНИМ фактом: Unwrap полосы не называет.
	noLane := strings.Replace(typed,
		"\treturn refusal.Wrap(refusal.ReferredTo, refusal.Ref{ResourceID: e.KeyID}, ErrFailedPrecondition)",
		"\treturn ErrFailedPrecondition", 1)
	noLane = strings.Replace(noLane, "\n\t\"github.com/PRO-Robotech/kacho/pkg/refusal\"\n", "\n", 1)
	_, findings, _ = laneProbeTree(t, map[string]string{
		"services/compute/internal/repo/producers.go": laneDictionaryProducers,
		"services/compute/internal/repo/typed.go":     noLane,
	})
	require.Len(t, findings, 1, "тип без полосы обязан быть находкой: %v", findings)
	require.Contains(t, findings[0], "typed.go")
}

func TestDeletionRefusalGateLaneDoesNotLaunderSiblingMethods(t *testing.T) {
	t.Parallel()

	// Правило о типе узкое: полоса в `Unwrap` покрывает методы типа, полоса в
	// ЛЮБОМ другом методе — нет. Иначе у писателя, где рядом живут снятие и
	// привязка, второй уехал бы из-под наблюдения молча.
	const sibling = `package p

import (
	"fmt"

	"github.com/PRO-Robotech/kacho/pkg/refusal"
)

var ErrFailedPrecondition = fmt.Errorf("x")

type writer struct{}

func (w *writer) Delete(id string) error {
	return refusal.Wrap(refusal.ReferredTo, refusal.Ref{ResourceID: id},
		fmt.Errorf("%w: address %s is in use", ErrFailedPrecondition, id))
}

func (w *writer) SetReference(id string) error {
	return fmt.Errorf("%w: address already referenced by another resource", ErrFailedPrecondition)
}
`
	_, findings, _ := laneProbeTree(t, map[string]string{
		"services/vpc/internal/repo/producers.go": laneDictionaryProducers,
		"services/vpc/internal/repo/sibling.go":   sibling,
	})
	require.Len(t, findings, 1,
		"полоса у соседнего метода не вправе отбеливать этот литерал: %v", findings)
	require.Contains(t, findings[0], "already referenced")
}

func TestDeletionRefusalDocGateJudgesTheClaimInBothDirections(t *testing.T) {
	t.Parallel()

	lanes := map[string]map[string]bool{
		"vpc": {"Protected": true, "HoldsChildren": true},
	}

	// ЗАКОННЫЙ БЛИЗНЕЦ: страница называет ровно то, что служба производит.
	honest := "Отказ несёт `DELETION_PROTECTED`; либо `REFERENCE_IN_USE` с " +
		"`reference_kind`: `children` — ресурс держит своих."
	require.Empty(t,
		repohygiene.DeletionRefusalDocFindings(lanes, map[string]string{"vpc": honest}),
		"страница, называющая ровно производимое, обязана молчать")

	// ИНЪЕКЦИЯ 1, один факт разницы: со страницы убран вид ссылки `children`.
	silent := strings.Replace(honest, "`children` — ресурс держит своих", "ресурс держит своих", 1)
	f := repohygiene.DeletionRefusalDocFindings(lanes, map[string]string{"vpc": silent})
	require.Len(t, f, 1, "полоса без строки на странице обязана быть находкой: %v", f)
	require.Contains(t, f[0], "HoldsChildren")
	require.Contains(t, f[0], "молчит")

	// ИНЪЕКЦИЯ 2, один факт разницы: страница обещает вид ссылки, которого служба
	// не производит. Это хуже молчания — клиента посылают ловить отказ, которого
	// не бывает.
	overclaim := honest + " Бывает и `referrers`."
	f = repohygiene.DeletionRefusalDocFindings(lanes, map[string]string{"vpc": overclaim})
	require.Len(t, f, 1, "обещание без производителя обязано быть находкой: %v", f)
	require.Contains(t, f[0], "ReferredTo")
	require.Contains(t, f[0], "неисполнимо")

	// ИНЪЕКЦИЯ 3: обещан собственный токен защиты от удаления там, где его не
	// производят. Судится ТОКЕНОМ, а не видом ссылки: у этой полосы вида нет.
	noProtection := map[string]map[string]bool{"geo": {"HoldsChildren": true}}
	f = repohygiene.DeletionRefusalDocFindings(noProtection, map[string]string{"geo": honest})
	require.Len(t, f, 1, "обещание защиты от удаления без производителя обязано быть находкой: %v", f)
	require.Contains(t, f[0], "Protected")

	t.Logf("осмотрено: законных близнецов 1, инъекций 3")
}
