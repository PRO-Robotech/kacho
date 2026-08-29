// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy

import "testing"

// subscription_owner_naming_injection_test.go — доказательство того, что судья
// [judgeOwnerTreeDirAliases] СПОСОБЕН упасть и способен смолчать.
//
// Гейт, о котором известно лишь что он зелёный, неотличим от гейта, потерявшего
// способность краснеть. Поэтому здесь прогоняется ТА ЖЕ функция суждения — не её
// копия, — на синтетическом входе: дерево и конфигурация не трогаются вовсе.
//
// Каждая инъекция снимает РОВНО ОДНО свойство у входа, чьи остальные свойства на
// месте. Инъекция вида «добавить ещё один элемент» не годилась бы: новый элемент
// нарушает всё, что требуется от элементов вообще, и красное пришло бы от
// соседнего условия, ничего не сказав о проверяемом.
func TestOwnerNamingJudgeFindsEachWayTheAliasCanLie(t *testing.T) {
	// Множество имён края и состав дерева — синтетические и неизменные во всех
	// случаях: различие между случаями обязано быть только в карте псевдонимов.
	accepted := []string{"compute", "loadbalancer", "vpc"}
	dirs := map[string]bool{"compute": true, "nlb": true, "vpc": true}
	exists := func(dir string) bool { return dirs[dir] }

	cases := []struct {
		name    string
		aliases map[string][]string
		want    int
		wantWhy string
	}{
		{
			name:    "законный близнец: имя принимается, каталог существует и именем не является",
			aliases: map[string][]string{"loadbalancer": {"nlb"}},
			want:    0,
		},
		{
			name:    "псевдоним заведён на имя, которого край не принимает",
			aliases: map[string][]string{"nlb": {"loadbalancer"}},
			// Три находки, и все три законны: имя не принимается · каталог
			// оказался принимаемым именем · каталога с таким именем в дереве нет.
			want:    3,
			wantWhy: "имени нет среди принимаемых краем",
		},
		{
			name:    "каталог совпал с принимаемым именем — это второе имя, а не путь",
			aliases: map[string][]string{"loadbalancer": {"vpc"}},
			want:    1,
			wantWhy: "каталог совпал с ПРИНИМАЕМЫМ именем — это второе имя владельца, а не путь в дереве",
		},
		{
			name:    "каталога с таким именем в дереве нет",
			aliases: map[string][]string{"loadbalancer": {"balancer"}},
			want:    1,
			wantWhy: "каталога сервиса с таким именем в дереве нет",
		},
		{
			name:    "псевдонимов нет вовсе — судить нечего, и это НЕ поломка",
			aliases: map[string][]string{},
			want:    0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := judgeOwnerTreeDirAliases(accepted, tc.aliases, exists)
			t.Logf("перепись: псевдонимов подано %d · находок %d %v", len(tc.aliases), len(got), got)
			if len(got) != tc.want {
				t.Fatalf("находок %d, ожидалось %d: %v", len(got), tc.want, got)
			}
			if tc.wantWhy == "" {
				return
			}
			for _, f := range got {
				if f.why == tc.wantWhy {
					return
				}
			}
			t.Fatalf("ни одна находка не называет причину %q: %v", tc.wantWhy, got)
		})
	}
}

// TestOwnerNamingJudgeRefusesToBlessAnEmptyEdge — предпосылка судьи проверяется
// им самим: без имён края всякий псевдоним оказывается заведённым на непринимаемое
// имя, и молчание здесь означало бы «нечего сверять», а не «сходится».
func TestOwnerNamingJudgeRefusesToBlessAnEmptyEdge(t *testing.T) {
	got := judgeOwnerTreeDirAliases(nil, map[string][]string{"loadbalancer": {"nlb"}},
		func(string) bool { return true })
	t.Logf("перепись: имён края 0 · псевдонимов 1 · находок %d %v", len(got), got)
	if len(got) == 0 {
		t.Fatal("судья смолчал на ПУСТОМ множестве имён края: тогда его зелёное " +
			"неотличимо от «сверять было нечем»")
	}
}

// TestOwnersTheEdgeWillRefuseNamesEveryUnknownSpelling — судья
// [ownersTheEdgeWillRefuse] способен упасть и способен смолчать.
//
// Функция вынесена из гейта именно ради этого доказательства, а доказательства
// не было: гейт краснел только на настоящем дереве, то есть его способность
// падать держалась ручным опытом, который никто не повторит. Класс тот же, что
// приёмка нашла у гейта поставки (2026-08-29): проверка, о которой известно лишь
// что она зелёная, неотличима от потерявшей способность краснеть.
//
// Множество принимаемых имён и состав объявленного различаются между случаями
// РОВНО ОДНИМ свойством: остальные имена законны во всех.
func TestOwnersTheEdgeWillRefuseNamesEveryUnknownSpelling(t *testing.T) {
	accepted := []string{"compute", "loadbalancer", "vpc"}

	cases := []struct {
		name     string
		declared []string
		want     []string
	}{
		{
			name:     "законный близнец: все написания принимаются",
			declared: []string{"compute", "loadbalancer", "vpc"},
			want:     []string{},
		},
		{
			name:     "написание каталога сервиса вместо домена контракта",
			declared: []string{"compute", "nlb", "vpc"},
			want:     []string{"nlb"},
		},
		{
			name:     "принимаемое имя с посторонним регистром — тоже не принимается",
			declared: []string{"LoadBalancer"},
			want:     []string{"LoadBalancer"},
		},
		{
			name:     "объявлять нечего — судить нечего, и это НЕ поломка",
			declared: []string{},
			want:     []string{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ownersTheEdgeWillRefuse(tc.declared, accepted)
			t.Logf("перепись: объявлено %d %v · принимает край %d %v · не принимается %d %v",
				len(tc.declared), tc.declared, len(accepted), accepted, len(got), got)
			if len(got) != len(tc.want) {
				t.Fatalf("не принимается %v, ожидалось %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("не принимается %v, ожидалось %v", got, tc.want)
				}
			}
		})
	}
}

// TestOwnersTheEdgeWillRefuseRefusesToBlessAnEmptyEdge — предпосылка судьи.
//
// Пустое множество принимаемых имён означает «сверять было не с чем», а не
// «сходится»: молчание на нём сделало бы зелёное гейта свойством пустоты. Сам
// гейт отвергает такое состояние отдельным утверждением; здесь закрепляется, что
// СУДЬЯ в этом состоянии называет каждое объявленное имя, а не молчит.
func TestOwnersTheEdgeWillRefuseRefusesToBlessAnEmptyEdge(t *testing.T) {
	got := ownersTheEdgeWillRefuse([]string{"compute", "vpc"}, nil)
	t.Logf("перепись: имён края 0 · объявлено 2 · не принимается %d %v", len(got), got)
	if len(got) != 2 {
		t.Fatalf("судья назвал %d имён из 2 при ПУСТОМ множестве принимаемых: тогда его "+
			"молчание неотличимо от согласия", len(got))
	}
}
