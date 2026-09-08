// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// crossmoduleworkspace_injection_test.go — ДОКАЗАТЕЛЬСТВО, что гейт образца
// рабочего пространства СПОСОБЕН упасть и способен смолчать.
//
// Инъекция зовёт ТО ЖЕ тело (`checkCrossModuleWorkspace`), что исполняется на
// дереве: доказывать способность падать у копии значило бы доказывать про копию.
//
// По каждой оси — пара: внесённый дефект даёт находку ИМЕННО своего рода, а
// законный близнец той же формы молчит. Односторонняя проба зеленела бы на
// гейте, объявляющем находку на всём подряд.
package repohygiene

import (
	"strings"
	"testing"
)

func workspaceFindingKinds(fs []workspaceFinding) []string {
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.Kind+":"+f.What)
	}
	return out
}

func TestCrossModuleWorkspaceGateCanFailAndCanStaySilent(t *testing.T) {
	t.Parallel()
	const good = "go 1.26.0\n\nuse (\n\t.\n\t./services/iam\n)\n"

	cases := []struct {
		name    string
		modules []string
		src     string
		present bool
		want    []string
	}{
		{
			name:    "контроль: образец называет оба модуля — молчание",
			modules: []string{".", "services/iam"},
			src:     good,
			present: true,
			want:    nil,
		},
		{
			name:    "инъекция: модуль дерева не назван",
			modules: []string{".", "services/iam"},
			src:     "go 1.26.0\n\nuse .\n",
			present: true,
			want:    []string{"модуль-без-use:services/iam"},
		},
		{
			name:    "инъекция: use называет каталог без модуля",
			modules: []string{".", "services/iam"},
			src:     "go 1.26.0\n\nuse (\n\t.\n\t./services/iam\n\t./services/vpc\n)\n",
			present: true,
			want:    []string{"use-без-модуля:services/vpc"},
		},
		{
			name:    "инъекция: образца нет вовсе",
			modules: []string{".", "services/iam"},
			src:     "",
			present: false,
			want:    []string{"нет-образца:go.work.example"},
		},
		{
			name:    "контроль: однострочная и блочная запись — одна координата",
			modules: []string{".", "services/iam"},
			src:     "go 1.26.0\n\nuse .\nuse \"./services/iam\"\n",
			present: true,
			want:    nil,
		},
		{
			name: "контроль: гейт не краснеет на собственном объяснении — " +
				"слово use в комментарии директивой не является",
			modules: []string{".", "services/iam"},
			src: "// Перечень ниже обязан называть каждый модуль; лишняя запись\n" +
				"// use ./services/vpc была бы находкой — но это проза, а не директива.\n" +
				good,
			present: true,
			want:    nil,
		},
		{
			name:    "контроль: третий модуль, названный образцом, — молчание",
			modules: []string{".", "pkg", "services/iam"},
			src:     "go 1.26.0\n\nuse (\n\t.\n\t./pkg\n\t./services/iam\n)\n",
			present: true,
			want:    nil,
		},
		{
			name:    "инъекция: третий модуль заведён, образец о нём не знает",
			modules: []string{".", "pkg", "services/iam"},
			src:     good,
			present: true,
			want:    []string{"модуль-без-use:pkg"},
		},

		// ── ОДИН МОДУЛЬ: оси расходятся, и это несущая часть пары ───────────
		//
		// До разведения осей все четыре случая ниже давали ОДНО и то же —
		// молчание, потому что тело гейта не звалось вовсе. Три из четырёх были
		// верны случайно, четвёртый (директива на несуществующий каталог) —
		// неверен, и обнаружить это было нечем: находок ноль, перепись ноль,
		// вердикт зелёный.
		{
			name: "инъекция: модуль один, а образец называет каталог, которого нет — " +
				"находка при ЛЮБОМ числе модулей",
			modules: []string{"."},
			src:     good, // `use ./services/iam` пережил свой каталог
			present: true,
			want:    []string{"use-без-модуля:services/iam"},
		},
		{
			name:    "близнец: модуль один, образец называет только его — молчание",
			modules: []string{"."},
			src:     "go 1.26.0\n\nuse .\n",
			present: true,
			want:    nil,
		},
		{
			name: "близнец: модуль один, образца нет вовсе — молчание " +
				"(снять образец вместе с предметом есть объявленный исход)",
			modules: []string{"."},
			src:     "",
			present: false,
			want:    nil,
		},
		{
			name: "близнец: модуль один и НЕ назван образцом — молчание: " +
				"кросс-модульной разработки при одном модуле не бывает",
			modules: []string{"."},
			src:     "go 1.26.0\n\nuse ./services/iam\n",
			present: true,
			// Ровно одна находка — вторая ось. Оси «модуль без use» здесь
			// предмета нет, и «модуль-без-use:.» появиться не вправе.
			want: []string{"use-без-модуля:services/iam"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			found, census := checkCrossModuleWorkspace(c.modules, c.src, c.present)
			got := workspaceFindingKinds(found)
			if strings.Join(got, "|") != strings.Join(c.want, "|") {
				t.Fatalf("находки разошлись с ожидаемыми\n  получено: %v\n  ожидалось: %v", got, c.want)
			}
			// Перепись обязана подтверждать, что ось РАБОТАЛА, а не молчала: без
			// этого «находок 0» законного близнеца неотличимо от незвавшегося тела.
			if c.present && census.Uses != census.UsesJudged {
				t.Fatalf("ось «use без модуля» осмотрела %d директив из %d — она не звалась",
					census.UsesJudged, census.Uses)
			}
			if want := len(c.modules); c.present && want > 1 && census.ModulesJudged != want {
				t.Fatalf("ось «модуль без use» осмотрела %d модулей из %d", census.ModulesJudged, want)
			}
			if c.present && len(c.modules) == 1 && census.ModulesJudged != 0 {
				t.Fatalf("ось «модуль без use» судила при одном модуле: осмотрено %d — "+
					"предмета у неё здесь нет", census.ModulesJudged)
			}
			t.Logf("осмотрено модулей %d (осью %d), директив %d (осью %d); находок %d: %v",
				len(c.modules), census.ModulesJudged, census.Uses, census.UsesJudged, len(got), got)
		})
	}
}

// ── ВЕРНУТЬ ДЕФЕКТ: прежняя форма ВЫЗОВА обязана молчать там, где нынешняя краснеет

// crossModuleWorkspaceBeforeTheSplit — форма вызова, стоявшая в гейте до
// 2026-09-08, воспроизведённая дословно:
//
//	var found []workspaceFinding
//	if len(modules) > 1 {
//	    found = checkCrossModuleWorkspace(modules, src, examplePresent)
//	}
//
// Она здесь не «копия суждения», а САМ ДЕФЕКТ: суждение в ней то же самое, и
// отличается она от нынешней ровно одним фактом — условием перед вызовом.
// Инъекция без неё была бы невозможна: дефект жил в вызывающем, а тело гейта на
// одном модуле отвечало и тогда верно — его просто никто не спрашивал.
func crossModuleWorkspaceBeforeTheSplit(modules []string, src string, present bool) ([]workspaceFinding, workspaceCensus) {
	if len(modules) > 1 {
		return checkCrossModuleWorkspace(modules, src, present)
	}
	return nil, workspaceCensus{Modules: len(modules)}
}

// TestCrossModuleWorkspaceSplitIsWhatMakesTheSecondAxisReachable — пара
// «дефект ↔ починка» на ОДНОМ входе.
//
// Вход один и тот же: модуль в дереве один, образец называет каталог, которого
// нет. Различие между строками таблицы — только форма вызова. Прежняя молчит,
// нынешняя краснеет; без этой пробы разведение осей было бы объявлением, а не
// доказанным свойством.
func TestCrossModuleWorkspaceSplitIsWhatMakesTheSecondAxisReachable(t *testing.T) {
	t.Parallel()

	const modulesOne = "."
	// Образец пережил свой каталог: `use ./services/iam` при отсутствующем модуле.
	const src = "go 1.26.0\n\nuse (\n\t.\n\t./services/iam\n)\n"
	modules := []string{modulesOne}

	before, censusBefore := crossModuleWorkspaceBeforeTheSplit(modules, src, true)
	if len(before) != 0 {
		t.Fatalf("прежняя форма вызова внезапно краснеет: %v — тогда предмет задачи не тот, "+
			"что описан, и разведение осей ничего не чинило", workspaceFindingKinds(before))
	}
	if censusBefore.UsesJudged != 0 {
		t.Fatalf("прежняя форма осмотрела %d директив — она звала тело, чего не делала",
			censusBefore.UsesJudged)
	}

	after, censusAfter := checkCrossModuleWorkspace(modules, src, true)
	kinds := workspaceFindingKinds(after)
	if len(kinds) != 1 || kinds[0] != "use-без-модуля:services/iam" {
		t.Fatalf("нынешняя форма не находит директиву, пережившую свой каталог: %v", kinds)
	}
	if censusAfter.UsesJudged != 2 {
		t.Fatalf("нынешняя форма осмотрела %d директив из 2 — ось звалась не вся",
			censusAfter.UsesJudged)
	}
	// И ровно одна ось: у «модуль без use» при одном модуле предмета нет, и
	// починка не имеет права заводить ей находку заодно.
	if censusAfter.ModulesJudged != 0 {
		t.Fatalf("ось «модуль без use» судила при одном модуле: осмотрено %d",
			censusAfter.ModulesJudged)
	}

	t.Logf("одно-фактная пара: прежняя форма — находок %d (директив осмотрено %d); "+
		"нынешняя — находок %d (директив осмотрено %d)",
		len(before), censusBefore.UsesJudged, len(after), censusAfter.UsesJudged)
}
