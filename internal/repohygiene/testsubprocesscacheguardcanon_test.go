// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// testsubprocesscacheguardcanon_test.go — проба СЛОВАРЯ инструментов.
//
// Словарь — исполняемая часть гейта: по нему решается, требует ли место запуска
// стража. До этой пробы ни одна строка не утверждала ни его состава, ни текстов,
// а поле `reason` не читалось ВООБЩЕ ничем — то есть причина решения жила в
// дереве на правах комментария, который ничто не держит.
//
// Проба стоит во ВНУТРЕННЕМ пакете (`repohygiene`, не `repohygiene_test`):
// словарь неэкспортируемый, и выносить его наружу ради пробы значило бы менять
// границу пакета под удобство проверки.
package repohygiene

import (
	"sort"
	"strings"
	"testing"
)

// TestSubprocessToolCanonHasThreeStatesAndEveryEntryIsReasoned — состав словаря
// и три состояния, а не два.
func TestSubprocessToolCanonHasThreeStatesAndEveryEntryIsReasoned(t *testing.T) {
	t.Parallel()

	if len(subprocessToolCanon) == 0 {
		// Пустой словарь — не «чисто»: гейт, у которого закрытый словарь пуст,
		// объявил бы находкой КАЖДОЕ место запуска в дереве.
		t.Fatal("словарь пуст — судить по нему нечего, а «ноль записей» неотличимо от «не читали»")
	}

	known := map[subprocessToolDecision]bool{
		toolGuardRequired:  true,
		toolGuardNotNeeded: true,
		toolNotAnalysed:    true,
	}
	byState := map[subprocessToolDecision][]string{}
	names := make([]string, 0, len(subprocessToolCanon))
	for name := range subprocessToolCanon {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		pol := subprocessToolCanon[name]
		if !known[pol.decision] {
			t.Errorf("%q: состояние %q не из трёх — читателю неизвестно, что оно значит для гейта",
				name, pol.decision)
			continue
		}
		byState[pol.decision] = append(byState[pol.decision], name)

		// Причина обязательна у КАЖДОЙ записи: запись без причины — умолчание,
		// выданное за решение.
		if strings.TrimSpace(pol.reason) == "" {
			t.Errorf("%q (%s): причины нет — решение не отличимо от того, что о нём забыли", name, pol.decision)
		}

		switch pol.decision {
		case toolNotAnalysed:
			// Предикат снятия обязателен ровно здесь: у решённых записей снятие
			// держит самоистечение словаря по дереву, а у неразобранной —
			// только назначенное условие.
			if strings.TrimSpace(pol.removal) == "" {
				t.Errorf("%q: НЕ РАЗБИРАЛИ без предиката снятия — запись живёт вечно", name)
			}
		default:
			if strings.TrimSpace(pol.removal) != "" {
				t.Errorf("%q (%s): у разобранной записи предикат снятия лишний — "+
					"снятие держит самоистечение словаря по дереву, и два предиката об одном "+
					"расходятся молча", name, pol.decision)
			}
		}
	}

	// Состав требующих стража назван поимённо: гейт судит ИМЕННО их, и тихое
	// пополнение этого множества обязано быть видно в диффе пробы.
	got := byState[toolGuardRequired]
	sort.Strings(got)
	want := []string{"helm"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("страж требуется от %v, а проба знает %v — множество судимого изменилось молча", got, want)
	}
	if len(byState[toolNotAnalysed]) == 0 {
		t.Error("записей «НЕ РАЗБИРАЛИ» ноль, а в переписи гейта по дереву они есть — " +
			"либо словарь разобран целиком (тогда снимается и эта проба), либо состояние потеряно")
	}
}

// TestSubprocessGuardCensusSaysWhatItDidNotJudge — «НЕ РАЗБИРАЛИ» обязано быть
// ЧИСЛОМ в переписи, а не молчанием.
//
// Без этого незнание гейта и его чистота выглядели одинаково: место, чей
// инструмент не разобран, не порождало ни находки, ни строки.
func TestSubprocessGuardCensusSaysWhatItDidNotJudge(t *testing.T) {
	t.Parallel()
	cen := SubprocessGuardCensus{
		FilesRead: 1, Sites: 2, NeedGuard: 1, Guarded: 1,
		Programs:  map[string]int{"helm": 1, "bash": 1},
		Forms:     map[string]int{formLiteral: 2},
		Undecided: map[string]int{"bash": 1},
	}
	line := cen.Line()
	for _, want := range []string{"НЕ РАЗБИРАЛИ мест 1", "bash×1", "единиц разбора (каталог×пакет)"} {
		if !strings.Contains(line, want) {
			t.Errorf("перепись не несёт %q:\n%s", want, line)
		}
	}
}
