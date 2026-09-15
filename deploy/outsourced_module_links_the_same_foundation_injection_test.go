// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// outsourced_module_links_the_same_foundation_injection_test.go — доказательство
// СПОСОБНОСТИ соседа упасть, и упасть на предмете, а не на форме.
//
// Инъекция подаёт СИНТЕТИЧЕСКИЙ вход чистой функции: ни дерево, ни кэш модулей
// не трогаются. Иначе доказательство зависело бы от того, что сегодня пинит
// go.mod, — то есть исчезало бы ровно тогда, когда дерево приходит в порядок
// (класс «самопроверка, опирающаяся на живую запись»).
//
// Каждая ось — дефект И его законный близнец. Односторонняя инъекция зеленела бы
// на проверке, отвергающей всё.
package deploy_test

import (
	"strings"
	"testing"
)

// TestFoundationDisagreementsFindsTheDefectAndSpares TheTwin — ось «версии расходятся».
func TestFoundationDisagreementsFindsTheDefectAndSparesTheTwin(t *testing.T) {
	cases := []struct {
		name   string
		links  []foundationLink
		tree   string
		expect bool // ждём ли находку
		says   string
	}{
		{
			// ДЕФЕКТ, ВОСПРОИЗВЕДЁННЫЙ ДОСЛОВНО: ровно то состояние, при котором
			// стенд отвечал 501 при ЗЕЛЁНОМ соседе (оба пина службы — `v0.4.0`).
			name:   "дефект: часть на v1.5.0, дерево на v1.7.0",
			links:  []foundationLink{{Part: "kaname", Version: "v0.4.0", Foundation: "v1.5.0"}},
			tree:   "v1.7.0",
			expect: true,
			says:   "kaname@v0.4.0",
		},
		{
			name:   "законный близнец: часть и дерево на одном фундаменте",
			links:  []foundationLink{{Part: "kaname", Version: "v0.4.0", Foundation: "v1.7.0"}},
			tree:   "v1.7.0",
			expect: false,
		},
		{
			// Псевдоверсия части — вторая законная форма пина, и на согласие по
			// фундаменту она не влияет вовсе.
			name: "законный близнец: псевдоверсия части на том же фундаменте",
			links: []foundationLink{
				{Part: "kaname", Version: "v0.4.1-0.20260914234811-fa90b8b3e91b", Foundation: "v1.7.0"},
			},
			tree:   "v1.7.0",
			expect: false,
		},
		{
			name:   "дефект: часть не объявляет фундамента вовсе",
			links:  []foundationLink{{Part: "kaname", Version: "v0.4.0", Foundation: ""}},
			tree:   "v1.7.0",
			expect: true,
			says:   "НЕ ОБЪЯВЛЯЕТ",
		},
		{
			// Расхождение в МЛАДШЕМ разряде — тоже находка, и это решение, а не
			// пересол: «переименований между этими версиями не было» есть
			// утверждение о чужой истории выпусков, производителя у которого в
			// этом дереве нет.
			name:   "дефект: расхождение в младшем разряде",
			links:  []foundationLink{{Part: "kaname", Version: "v0.4.0", Foundation: "v1.7.1"}},
			tree:   "v1.7.0",
			expect: true,
			says:   "v1.7.1",
		},
		{
			name: "находка называется ПОИМЁННО среди согласных",
			links: []foundationLink{
				{Part: "alpha", Version: "v1.0.0", Foundation: "v1.7.0"},
				{Part: "kaname", Version: "v0.4.0", Foundation: "v1.5.0"},
				{Part: "omega", Version: "v2.0.0", Foundation: "v1.7.0"},
			},
			tree:   "v1.7.0",
			expect: true,
			says:   "kaname@v0.4.0",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := foundationDisagreements(c.links, c.tree)
			if c.expect && len(got) == 0 {
				t.Fatalf("дефект внесён, а находки нет — проверка не способна упасть на этой оси")
			}
			if !c.expect && len(got) != 0 {
				t.Fatalf("законный близнец объявлен находкой: %v", got)
			}
			if c.expect {
				if len(got) != 1 {
					t.Fatalf("ждали ОДНУ находку, получили %d: %v", len(got), got)
				}
				if !strings.Contains(got[0], c.says) {
					t.Fatalf("находка не называет координаты %q: %s", c.says, got[0])
				}
			}
		})
	}
}

// TestRequirementInReadsTheRequirementAndNotItsMention — распознаватель судит
// СТРОКУ ТРЕБОВАНИЯ, а не встречу имени: путь фундамента стоит и в строке
// `module` самого фундамента, и в комментарии рядом.
func TestRequirementInReadsTheRequirementAndNotItsMention(t *testing.T) {
	const path = "github.com/PRO-Robotech/corelib"
	cases := []struct {
		name, gomod, want string
	}{
		{"требование в блоке", "module x\n\nrequire (\n\t" + path + " v1.7.0\n)\n", "v1.7.0"},
		{"требование строкой", "module x\n\nrequire " + path + " v1.6.0\n", "v1.6.0"},
		{"только упоминание в комментарии", "module x\n\n// " + path + " v9.9.9 — почему так\n", ""},
		{"хвостовой комментарий не крадёт версию", "require " + path + " v1.7.0 // indirect\n", "v1.7.0"},
		{"строка module самого фундамента", "module " + path + "\n\ngo 1.24\n", ""},
		{"не объявлен вовсе", "module x\n\nrequire github.com/other/pkg v1.0.0\n", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := requirementIn(c.gomod, path); got != c.want {
				t.Fatalf("прочитано %q, ждали %q", got, c.want)
			}
		})
	}
}
