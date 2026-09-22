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

		// Непустота причины — необходимое, но не достаточное: СОДЕРЖАНИЕ
		// пришпилено ведомостью ниже. Здесь остаётся форма.
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

	// Состав КАЖДОГО из трёх состояний назван поимённо, а не одного. Пока
	// закреплено было только `toolGuardRequired`, перевод записи между двумя
	// другими состояниями проходил молча: гейт ведёт себя в них одинаково, и
	// отличить их можно было лишь глазом по строке переписи.
	for _, st := range []struct {
		decision subprocessToolDecision
		want     []string
	}{
		{toolGuardRequired, []string{"helm"}},
		{toolGuardNotNeeded, nil},
		{toolNotAnalysed, []string{"bash", "gh", "go", "jq", "make", "python3"}},
	} {
		got := byState[st.decision]
		sort.Strings(got)
		if strings.Join(got, ",") != strings.Join(st.want, ",") {
			t.Errorf("состояние %q несут %v, а ведомость знает %v — состав состояния изменился молча",
				st.decision, got, st.want)
		}
	}
}

// subprocessCanonLedger — ВЕДОМОСТЬ словаря: решение и ОСНОВАНИЕ каждой записи,
// дословно.
//
// Вторая копия текста здесь намеренна и несущая. Словарь — исполняемая часть
// гейта: по нему решается, требует ли место запуска стража. Пока проба читала
// причину лишь на непустоту, основание можно было заменить на "x", а запись
// перевести из «НЕ РАЗБИРАЛИ» в «страж не нужен», не тронув ни одного
// основания, — и всё оставалось зелёным. Ведомость превращает такую правку в
// правку, ВИДНУЮ В ДИФФЕ: изменить решение нельзя, не изменив запись здесь.
//
// Это не чтение чужого исходника текстом (тот запрет — про гейт, добывающий
// предикат разбором чужого файла): обе стороны компилируются вместе, и
// расхождение даёт красную пробу, а не молчание.
var subprocessCanonLedger = map[string]struct {
	decision subprocessToolDecision
	reason   string
}{
	"helm": {toolGuardRequired,
		"рендер чарта читает шаблоны, профили и подчарты — ни один из этих " +
			"файлов проба не открывает сама, поэтому их правка кеш не сбрасывает"},
	"go":      {toolNotAnalysed, "вход инструмента — модуль и его кеш, не профили посадки"},
	"bash":    {toolNotAnalysed, "скрипт задаётся путём, содержимое читает оболочка"},
	"python3": {toolNotAnalysed, "генератор задаётся путём, содержимое читает интерпретатор"},
	"gh":      {toolNotAnalysed, "вход — состояние трекера, а не дерева"},
	"make":    {toolNotAnalysed, "цель читает Makefile и всё, до чего он дотянется"},
	"jq":      {toolNotAnalysed, "программа фильтра приходит из самой пробы"},
}

// TestSubprocessToolCanonMatchesItsLedger — словарь сходится с ведомостью по
// СОСТАВУ, по решению и по ОСНОВАНИЮ каждой записи.
func TestSubprocessToolCanonMatchesItsLedger(t *testing.T) {
	t.Parallel()

	for name, want := range subprocessCanonLedger {
		pol, ok := subprocessToolCanon[name]
		if !ok {
			t.Errorf("%q: ведомость знает запись, а словаря о ней нет — "+
				"запись снята, но ведомость этого не заметила", name)
			continue
		}
		if pol.decision != want.decision {
			t.Errorf("%q: решение %q, а ведомость знает %q — запись переклассифицирована молча",
				name, pol.decision, want.decision)
		}
		if pol.reason != want.reason {
			t.Errorf("%q: основание разошлось с ведомостью; словарь %q, ведомость %q — "+
				"основание есть часть решения, и менять его молча нельзя",
				name, pol.reason, want.reason)
		}
	}
	for name := range subprocessToolCanon {
		if _, ok := subprocessCanonLedger[name]; !ok {
			t.Errorf("%q: словарь несёт запись, которой нет в ведомости — "+
				"инструмент заведён молча, основание никем не прочитано", name)
		}
	}
}

// TestSubprocessGuardCensusSeparatesNotAnalysedFromNoGuardNeeded — два
// состояния, ведущие гейт одинаково, печатаются РАЗНЫМИ числами.
//
// Пока они складывались в одно, перевод записи между ними читался как убыль:
// «НЕ РАЗБИРАЛИ мест» молча 56 → 38, и ничто не говорило, куда делись 18.
func TestSubprocessGuardCensusSeparatesNotAnalysedFromNoGuardNeeded(t *testing.T) {
	t.Parallel()
	line := SubprocessGuardCensus{
		FilesRead: 1, Sites: 3, NeedGuard: 1, Guarded: 1,
		Programs:      map[string]int{"helm": 1, "bash": 1, "jq": 1},
		Forms:         map[string]int{formLiteral: 3},
		Undecided:     map[string]int{"bash": 1},
		NoGuardNeeded: map[string]int{"jq": 1},
	}.Line()
	for _, want := range []string{
		"НЕ РАЗБИРАЛИ мест 1 [bash×1]",
		"разобрано «страж не нужен» мест 1 [jq×1]",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("перепись не несёт %q — перевод записи между состояниями читался бы как убыль:\n%s", want, line)
		}
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
