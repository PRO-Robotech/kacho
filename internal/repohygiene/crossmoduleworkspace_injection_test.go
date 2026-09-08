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
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := workspaceFindingKinds(checkCrossModuleWorkspace(c.modules, c.src, c.present))
			if strings.Join(got, "|") != strings.Join(c.want, "|") {
				t.Fatalf("находки разошлись с ожидаемыми\n  получено: %v\n  ожидалось: %v", got, c.want)
			}
			t.Logf("осмотрено модулей %d; находок %d: %v", len(c.modules), len(got), got)
		})
	}
}
