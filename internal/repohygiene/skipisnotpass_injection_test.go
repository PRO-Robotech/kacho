// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// skipisnotpass_injection_test.go — способность гейта упасть доказана внесённым
// дефектом, а его молчание — законным близнецом той же формы.
//
// Без этой пары гейт неотличим от немого: «ноль находок» по дереву означало бы
// «ноль того, что он умеет видеть». Каждый случай меняет РОВНО ОДИН факт против
// своей пары — иначе неизвестно, что дало исход.
package repohygiene

import (
	"strings"
	"testing"
)

// wfWithCensus — законная форма: гасимые шаги имеют `id`, перепись исходов стоит
// последней, объявлена `always()` и получает свой предмет.
const wfWithCensus = `
jobs:
  probes:
    steps:
      - name: стенд
        id: stand
        run: .github/scripts/stand-up.sh --label x -- make dev-up
      - name: прогон проб
        id: probes
        if: ${{ env.STAND_PRECONDITION_UNMET != '1' }}
        run: npx playwright test
      - name: перепись исходов
        id: verdict-census
        if: ${{ always() }}
        run: python3 .github/scripts/assert-declared-verdicts-ran.py --workflow x --job probes --steps s.json
`

// mutate — одна текстовая подмена в законной форме. Один факт на случай.
func skipMutate(t *testing.T, old, new string) string {
	t.Helper()
	if !strings.Contains(wfWithCensus, old) {
		t.Fatalf("фикстура не содержит «%s» — подмена молча не состоялась бы, "+
			"и случай оказался бы законным близнецом самого себя", old)
	}
	return strings.Replace(wfWithCensus, old, new, 1)
}

func skipFindingsFor(raw string) []string {
	f, _, _ := checkSkipIsNotPass("probe.yml", raw)
	return f
}

func skipMustBeSilent(t *testing.T, name, raw string) {
	t.Helper()
	if f := skipFindingsFor(raw); len(f) != 0 {
		t.Errorf("%s: законная форма дала находки — гейт красит верное: %v", name, f)
	}
}

func skipMustFind(t *testing.T, name, raw, want string) {
	t.Helper()
	f := skipFindingsFor(raw)
	for _, s := range f {
		if strings.Contains(s, want) {
			return
		}
	}
	t.Errorf("%s: внесённый дефект НЕ найден (искали «%s»); находки: %v", name, want, f)
}

// (а) ЗАКОННЫЙ БЛИЗНЕЦ — без него всякое «нашёл» ниже ничего не значит.
func TestSkipIsNotPassInjection_LegalFormIsSilent(t *testing.T) {
	t.Parallel()
	skipMustBeSilent(t, "перепись на месте", wfWithCensus)
}

// (б) ДЕФЕКТ #1 — переписи нет вовсе. Это ровно наблюдённое состояние дерева.
func TestSkipIsNotPassInjection_NoCensusAtAllIsFound(t *testing.T) {
	t.Parallel()
	skipMustFind(t, "переписи нет",
		skipMutate(t, "assert-declared-verdicts-ran.py", "echo ничего-не-проверяю"),
		"переписи исходов в работе НЕТ")
}

// (в) ДЕФЕКТ #2 — перепись есть, но без функции состояния: промолчит ровно тогда,
// когда нужна.
func TestSkipIsNotPassInjection_CensusWithoutStateFunctionIsFound(t *testing.T) {
	t.Parallel()
	skipMustFind(t, "без always()",
		skipMutate(t, "if: ${{ always() }}", "if: ${{ github.event_name == 'push' }}"),
		"без функции состояния")
}

// (г) ДЕФЕКТ #3 — перепись гасится той самой отметкой, о которой обязана доложить.
func TestSkipIsNotPassInjection_SelfGatedCensusIsFound(t *testing.T) {
	t.Parallel()
	skipMustFind(t, "самогашение",
		skipMutate(t, "if: ${{ always() }}",
			"if: ${{ always() && env.STAND_PRECONDITION_UNMET != '1' }}"),
		"ГАСИТСЯ")
}

// (д) ДЕФЕКТ #4 — перепись стоит ДО гасимого шага и уронила бы зелёный прогон.
func TestSkipIsNotPassInjection_CensusBeforeGatedStepIsFound(t *testing.T) {
	t.Parallel()
	moved := `
jobs:
  probes:
    steps:
      - name: перепись исходов
        id: verdict-census
        if: ${{ always() }}
        run: python3 .github/scripts/assert-declared-verdicts-ran.py --workflow x --job probes --steps s.json
      - name: прогон проб
        id: probes
        if: ${{ env.STAND_PRECONDITION_UNMET != '1' }}
        run: npx playwright test
`
	skipMustFind(t, "перепись раньше предмета", moved, "ДО последнего гасимого шага")
}

// (е) ДЕФЕКТ #5 — у гасимого шага сняли `id`: он невидим контексту `steps`.
func TestSkipIsNotPassInjection_GatedStepWithoutIDIsFound(t *testing.T) {
	t.Parallel()
	skipMustFind(t, "гасимый шаг без id",
		skipMutate(t, "        id: probes\n", ""),
		"не имеет `id`")
}

// (ж) ДЕФЕКТ #6 — вызов не называет своего предмета: владелец вывел бы перечень
// не из того объявления.
func TestSkipIsNotPassInjection_CensusWithoutItsSubjectIsFound(t *testing.T) {
	t.Parallel()
	skipMustFind(t, "вызов без --job", skipMutate(t, "--job probes ", ""), "не передаёт `--job`")
}

// (з) ЗАКОННЫЙ БЛИЗНЕЦ ДРУГОЙ ФОРМЫ: работа БЕЗ гашения переписи не требует —
// иначе гейт требовал бы её от каждой работы дерева.
func TestSkipIsNotPassInjection_JobWithoutGatingNeedsNoCensus(t *testing.T) {
	t.Parallel()
	skipMustBeSilent(t, "работа без гашения", `
jobs:
  lint:
    steps:
      - name: линт
        run: make lint
`)
}

// (и) ЗАКОННЫЙ БЛИЗНЕЦ: `!cancelled()` — тоже функция состояния, и требовать
// именно `always()` значило бы диктовать форму вместо свойства.
func TestSkipIsNotPassInjection_NotCancelledIsAlsoAStateFunction(t *testing.T) {
	t.Parallel()
	skipMustBeSilent(t, "!cancelled()", skipMutate(t, "if: ${{ always() }}", "if: ${{ !cancelled() }}"))
}

// (к) ОТКАЗ, А НЕ МОЛЧАНИЕ: неразобранный YAML обязан быть находкой. Иначе файл,
// который гейт не смог прочитать, зачитывался бы в «замечаний нет».
func TestSkipIsNotPassInjection_UnparsableFileIsAFinding(t *testing.T) {
	t.Parallel()
	skipMustFind(t, "битый YAML", "jobs:\n  a:\n   steps:\n  - [", "файл НЕ проверен")
}

// (л) ОСВОБОЖДЁННАЯ РАБОТА: послабление срабатывает и молчит о механизме, но
// перепись объёма её ВИДИТ — иначе «ноль находок» стало бы «ноль прочитанного».
func TestSkipIsNotPassInjection_ExemptJobIsCountedAndSilent(t *testing.T) {
	t.Parallel()
	raw := `
jobs:
  shard:
    steps:
      - name: прогон суит
        if: ${{ env.STAND_PRECONDITION_UNMET != '1' }}
        run: newman-shard-run.sh
`
	f, census, hit := checkSkipIsNotPass(".github/workflows/e2e-newman.yml", raw)
	if len(f) != 0 {
		t.Errorf("освобождённая работа дала находки: %v", f)
	}
	if census.Gated != 1 || census.Jobs != 1 || census.Exempt != 1 {
		t.Errorf("перепись освобождённой работы: гасимых %d, работ %d, послаблений %d "+
			"— ждали 1/1/1; освобождение не должно делать работу непрочитанной",
			census.Gated, census.Jobs, census.Exempt)
	}
	if !hit[[2]string{"e2e-newman.yml", "shard"}] {
		t.Error("послабление не отмечено сработавшим — тогда гейт по дереву потребует его снятия")
	}
}

// (м) ПОСЛАБЛЕНИЕ ИСТЕКАЕТ САМО: как только освобождённая работа заводит
// перепись, запись обязана стать находкой — иначе она переживёт свой предмет.
func TestSkipIsNotPassInjection_ExemptionWithNothingToExcludeIsFound(t *testing.T) {
	t.Parallel()
	raw := `
jobs:
  shard:
    steps:
      - name: прогон суит
        id: suites
        if: ${{ env.STAND_PRECONDITION_UNMET != '1' }}
        run: newman-shard-run.sh
      - name: перепись исходов
        id: verdict-census
        if: ${{ always() }}
        run: python3 .github/scripts/assert-declared-verdicts-ran.py --workflow x --job shard --steps s.json
`
	f, _, _ := checkSkipIsNotPass(".github/workflows/e2e-newman.yml", raw)
	found := false
	for _, s := range f {
		if strings.Contains(s, "послаблению нечего исключать") {
			found = true
		}
	}
	if !found {
		t.Errorf("послабление не истекло на работе, которая УЖЕ зовёт перепись; находки: %v", f)
	}
}
