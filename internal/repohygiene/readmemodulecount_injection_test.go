// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// readmemodulecount_injection_test.go — ДОКАЗАТЕЛЬСТВО, что гейт README СПОСОБЕН
// упасть и способен смолчать.
//
// Инъекция зовёт ТО ЖЕ тело (`checkReadmeModuleCount`), что исполняется на
// дереве: доказывать способность падать у копии значило бы доказывать про копию.
//
// По каждой оси — ПАРА: внесённый дефект даёт находку ИМЕННО своего рода, а
// законный близнец той же формы молчит. Односторонняя проба зеленела бы на
// гейте, объявляющем находку на всём подряд.
//
// Каждая инъекция меняет РОВНО ОДИН факт против контроля. Дельта в два факта
// сделала бы вердикт недействительным: покраснеть мог сосед.
package repohygiene

import (
	"strings"
	"testing"
)

func readmeModuleFindingKinds(fs []readmeModuleFinding) []string {
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.Kind+":"+f.What)
	}
	return out
}

// readmeTrackedSet — состав дерева для инъекции: отвечает только о том, что ему
// подано, поэтому «цель не найдена» означает факт, а не недочитанный обход.
func readmeTrackedSet(paths ...string) func(string) bool {
	set := map[string]bool{}
	for _, p := range paths {
		set[p] = true
	}
	return func(rel string) bool { return set[rel] }
}

func TestReadmeModuleCountGateCanFailAndCanStaySilent(t *testing.T) {
	t.Parallel()
	// Законный близнец: перечень назван командой, следствия адресованы документу
	// дерева. Гейт обязан МОЛЧАТЬ.
	const sound = "# Kacho\n\n" +
		"```sh\ngit ls-files '*go.mod'\n```\n\n" +
		"Порядок пина — [док](docs/architecture/cross-module-development.md).\n"

	two := []string{".", "services/iam"}
	one := []string{"."}
	tracked := readmeTrackedSet("docs/architecture/cross-module-development.md", "README.md")

	cases := []struct {
		name    string
		present bool
		readme  string
		modules []string
		tracked func(string) bool
		want    []string
	}{
		{
			name:    "контроль: команда названа, ссылка резолвится — молчание",
			present: true, readme: sound, modules: two, tracked: tracked,
			want: nil,
		},
		{
			name:    "инъекция: число выписано вместо команды",
			present: true,
			readme: "# Kacho\n\n`github.com/PRO-Robotech/kacho` — **один** `go.mod` " +
				"на весь репозиторий.\n\nСм. [док](docs/architecture/cross-module-development.md).\n",
			modules: two, tracked: tracked,
			want: []string{"число-не-выведено:перечень модулей"},
		},
		{
			name:    "инъекция: следствия без адреса — ни одной ссылки в дерево",
			present: true,
			readme:  "# Kacho\n\n```sh\ngit ls-files '*go.mod'\n```\n\nЗапрет `replace` действует.\n",
			modules: two, tracked: tracked,
			want: []string{"граница-без-адреса:README.md"},
		},
		{
			name:    "инъекция: ссылка пережила свой предмет",
			present: true,
			readme:  strings.Replace(sound, "cross-module-development.md", "cross-module-dev.md", 1),
			modules: two, tracked: tracked,
			want: []string{"ссылка-в-никуда:docs/architecture/cross-module-dev.md"},
		},
		{
			name:    "инъекция: README в составе нет вовсе",
			present: false, readme: "", modules: two, tracked: tracked,
			want: []string{"нет-README:README.md"},
		},
		{
			name: "контроль: модуль ОДИН — предмета у двух осей нет, это законное " +
				"состояние дерева, а не послабление",
			present: true,
			readme:  "# Kacho\n\nОдин модуль, границы с вынесенной службой нет.\n",
			modules: one, tracked: tracked,
			want: nil,
		},
		{
			name: "контроль: внешний адрес и якорь ссылкой в дерево не являются — " +
				"иначе полоса краснела бы на http и на оглавлении",
			present: true,
			readme: sound + "\n[внешнее](https://example.org/x) [почта](mailto:a@b.c) " +
				"[оглавление](#разделы)\n",
			modules: two, tracked: tracked,
			want: nil,
		},
		{
			name:    "контроль: фрагмент срезается — ссылка на живой файл с якорем молчит",
			present: true,
			readme: "# Kacho\n\n```sh\ngit ls-files '*go.mod'\n```\n\n" +
				"[док](docs/architecture/cross-module-development.md#порядок)\n",
			modules: two, tracked: tracked,
			want: nil,
		},
		{
			name: "инъекция: части команды разнесены по строкам — командой это не " +
				"является, и порознь каждая встречается по другому поводу",
			present: true,
			readme: "# Kacho\n\nОбход `ls-files` по составу дерева;\n" +
				"объявление модуля лежит в `go.mod`.\n\n" +
				"[док](docs/architecture/cross-module-development.md)\n",
			modules: two, tracked: tracked,
			want: []string{"число-не-выведено:перечень модулей"},
		},
		{
			name:    "инъекция: обе оси разом — находки обе, ни одна не маскирует другую",
			present: true,
			readme:  "# Kacho\n\nОдин `go.mod` на весь репозиторий.\n",
			modules: two, tracked: tracked,
			want: []string{"число-не-выведено:перечень модулей", "граница-без-адреса:README.md"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := readmeModuleFindingKinds(
				checkReadmeModuleCount(c.present, c.readme, c.modules, c.tracked))
			if strings.Join(got, "|") != strings.Join(c.want, "|") {
				t.Fatalf("находки разошлись с ожидаемыми\n  получено: %v\n  ожидалось: %v", got, c.want)
			}
			t.Logf("осмотрено модулей %d; ссылок %d; находок %d: %v",
				len(c.modules), len(readmeRelativeLinks(c.readme)), len(got), got)
		})
	}
}
