// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package domain_test

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
)

// TestCidrGroup_Validate — доменные границы именованного набора префиксов.
//
// Отрицания стоят в паре с положительным контролем: без него «отвергнуто»
// неотличимо от «валидатор отвергает всё».
func TestCidrGroup_Validate(t *testing.T) {
	t.Parallel()

	base := func() domain.CidrGroup {
		return domain.CidrGroup{
			ID:        "cdg-01hqzx9k2m4n6p8r0",
			ProjectID: "prj-01hqzx9k2m4n6p8r0",
			Name:      domain.RcNameVPC("office-egress"),
		}
	}

	t.Run("законный набор проходит (положительный контроль)", func(t *testing.T) {
		t.Parallel()
		g := base()
		g.Description = domain.RcDescription("офисные диапазоны")
		g.Labels = domain.LabelsFromMap(map[string]string{"env": "prod"})
		g.V4CidrBlocks = []string{"203.0.113.0/24"}
		if err := g.Validate(); err != nil {
			t.Fatalf("законный набор отвергнут: %v", err)
		}
	})

	t.Run("пустое имя допустимо — оно косметическое", func(t *testing.T) {
		t.Parallel()
		g := base()
		g.Name = ""
		if err := g.Validate(); err != nil {
			t.Fatalf("пустое имя отвергнуто: %v", err)
		}
	})

	t.Run("имя длиннее предела отвергается", func(t *testing.T) {
		t.Parallel()
		g := base()
		g.Name = domain.RcNameVPC("a" + strings.Repeat("b", domain.MaxNameLen))
		if err := g.Validate(); err == nil {
			t.Fatal("имя сверх предела принято")
		}
	})

	t.Run("описание длиннее предела отвергается", func(t *testing.T) {
		t.Parallel()
		g := base()
		g.Description = domain.RcDescription(strings.Repeat("x", domain.MaxDescriptionLen+1))
		if err := g.Validate(); err == nil {
			t.Fatal("описание сверх предела принято")
		}
	})

	t.Run("метка с недопустимым ключом отвергается", func(t *testing.T) {
		t.Parallel()
		g := base()
		g.Labels = domain.LabelsFromMap(map[string]string{"Env": "prod"})
		if err := g.Validate(); err == nil {
			t.Fatal("недопустимый ключ метки принят")
		}
	})
}

// TestCidrGroup_Equal — равенство по доменным полям, включая ОБА семейства.
//
// Проба существует ради одного свойства: набор второго семейства обязан входить
// в сравнение. Пока он не входил бы, правка v6-состава читалась бы как «ничего
// не изменилось» на любом пути, который принимает решение по Equal.
func TestCidrGroup_Equal(t *testing.T) {
	t.Parallel()

	mk := func() domain.CidrGroup {
		return domain.CidrGroup{
			ID:           "cdg-01hqzx9k2m4n6p8r0",
			ProjectID:    "prj-01hqzx9k2m4n6p8r0",
			Name:         domain.RcNameVPC("office"),
			Description:  domain.RcDescription("d"),
			Labels:       domain.LabelsFromMap(map[string]string{"env": "prod"}),
			V4CidrBlocks: []string{"203.0.113.0/24"},
			V6CidrBlocks: []string{"2001:db8::/32"},
		}
	}

	if !mk().Equal(mk()) {
		t.Fatal("одинаковые наборы объявлены разными (положительный контроль)")
	}

	cases := map[string]func(g *domain.CidrGroup){
		"id":          func(g *domain.CidrGroup) { g.ID = "cdg-other" },
		"project":     func(g *domain.CidrGroup) { g.ProjectID = "prj-other" },
		"имя":         func(g *domain.CidrGroup) { g.Name = "other" },
		"описание":    func(g *domain.CidrGroup) { g.Description = "other" },
		"метки":       func(g *domain.CidrGroup) { g.Labels = domain.LabelsFromMap(map[string]string{"env": "dev"}) },
		"состав v4":   func(g *domain.CidrGroup) { g.V4CidrBlocks = []string{"198.51.100.0/24"} },
		"состав v6":   func(g *domain.CidrGroup) { g.V6CidrBlocks = []string{"2001:db8:1::/48"} },
		"пустой v6":   func(g *domain.CidrGroup) { g.V6CidrBlocks = nil },
		"лишний блок": func(g *domain.CidrGroup) { g.V4CidrBlocks = append(g.V4CidrBlocks, "192.0.2.0/24") },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			other := mk()
			mutate(&other)
			if mk().Equal(other) {
				t.Fatalf("различие в поле %q не замечено", name)
			}
		})
	}
}

// TestMaxCidrGroupBlocks_MatchesSiblingSets — потолок набора равен потолку
// адресных наборов сети и подсети.
//
// Значение здесь не «красивое число»: набор становится РАЗМЕРОМ ПРАВИЛА у
// исполнителя, а сеть и подсеть уже держат ту же величину по той же причине —
// адресный набор, который целиком уезжает в каждый ответ и раскладывается в
// строки дочерней таблицы. Разъехавшись, три потолка стали бы тремя разными
// обещаниями об одном классе.
func TestMaxCidrGroupBlocks_MatchesSiblingSets(t *testing.T) {
	t.Parallel()
	if domain.MaxCidrGroupBlocks != domain.MaxNetworkCidrBlocks {
		t.Fatalf("потолок набора %d разошёлся с супернетом сети %d",
			domain.MaxCidrGroupBlocks, domain.MaxNetworkCidrBlocks)
	}
	if domain.MaxCidrGroupBlocks != domain.MaxSubnetCidrBlocks {
		t.Fatalf("потолок набора %d разошёлся с диапазонами подсети %d",
			domain.MaxCidrGroupBlocks, domain.MaxSubnetCidrBlocks)
	}
}
