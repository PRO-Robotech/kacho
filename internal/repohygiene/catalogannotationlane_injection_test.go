// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/authz/catalogderive"
)

// ДОКАЗАТЕЛЬСТВО СПОСОБНОСТИ ПАДАТЬ у разбора полосы аннотации (задача #2692).
//
// Мир подставной: аннотации собираются в пробе, а не читаются из дескрипторов
// дерева. Подменяется ВХОД разбора, а не сам разбор, — иначе проба доказывала
// бы свойство своей копии. Дерево сегодня чисто (338 строк каталога: 22 exempt ·
// 31 scope_filtered · 285 relation, без аннотации — 0), поэтому красное здесь
// приходит ТОЛЬКО от инъекции: живого экземпляра, на котором гейт покраснел бы,
// в дереве нет, и это сказано прямо.
//
// Каждая ось идёт ПАРОЙ: внесённый дефект обязан краснеть и называть, ЧЕГО не
// хватает; законный близнец — молчать и называть свою полосу. Инъекция меняет
// РОВНО ОДИН факт против близнеца.

// ЗАКОННЫЕ БЛИЗНЕЦЫ: три полосы, которые дерево производит, — разбор молчит и
// называет каждую своим именем.
func TestAnnotationLaneInjection_LegalTwinsAreSilent(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		a    catalogderive.Annotations
		want string
	}{
		{"exempt", catalogderive.Annotations{Permission: catalogderive.ExemptPermission}, laneExempt},
		{"scope_filtered", catalogderive.Annotations{Permission: "geo.regions.list", ScopeFiltered: true}, laneScopeFiltered},
		{"relation", catalogderive.Annotations{
			Permission: "geo.regions.create", RequiredRelation: "system_admin",
			ScopeObjectType: "cluster", ScopeFromRequestField: "*",
		}, laneRelation},
	}
	for _, c := range cases {
		lane, complaint := annotationLane(c.a)
		if complaint != "" {
			t.Errorf("%s: законный близнец объявлен находкой: %q", c.name, complaint)
		}
		if lane != c.want {
			t.Errorf("%s: полоса %q, ожидалась %q", c.name, lane, c.want)
		}
	}
}

// ДЕФЕКТ 1: метод без единой аннотации авторизации. Ровно тот вход, на котором
// плагин каталога эмитит строку с пустыми полями, а сверка «аннотация == строка»
// зеленеет на равенстве двух пустот.
func TestAnnotationLaneInjection_UnannotatedMethodIsAFinding(t *testing.T) {
	t.Parallel()
	lane, complaint := annotationLane(catalogderive.Annotations{})
	if complaint == "" {
		t.Fatalf("метод без аннотации прошёл молча (полоса %q)", lane)
	}
	if !strings.Contains(complaint, "не несёт аннотации") {
		t.Errorf("находка не называет предмет: %q", complaint)
	}
	if lane != "" {
		t.Errorf("у находки не может быть полосы, получено %q", lane)
	}
}

// ДЕФЕКТ 2: имя права названо, отношение — нет. Производитель карты читает это
// как ПУБЛИЧНУЮ полосу (нет отношения и нет сужения владельцем — значит проверки
// нет), то есть строка с именем действия открывает метод без единого вопроса к
// модели. Один факт против близнеца «relation»: снято отношение и область.
func TestAnnotationLaneInjection_PermissionWithoutRelationIsAFinding(t *testing.T) {
	t.Parallel()
	lane, complaint := annotationLane(catalogderive.Annotations{Permission: "geo.regions.create"})
	if complaint == "" {
		t.Fatalf("право без отношения прошло молча (полоса %q)", lane)
	}
	if !strings.Contains(complaint, "geo.regions.create") || !strings.Contains(complaint, "отношени") {
		t.Errorf("находка не называет ни право, ни чего не хватает: %q", complaint)
	}
}

// КОНТРОЛЬ В ОБРАТНУЮ СТОРОНУ: exempt с названным отношением — противоречие, а
// не законный близнец; корелиб на нём отказывает в старте. Здесь он тоже
// находка, иначе гейт молчал бы там, где сервис не поднимется.
func TestAnnotationLaneInjection_ExemptNamingARelationIsAFinding(t *testing.T) {
	t.Parallel()
	_, complaint := annotationLane(catalogderive.Annotations{
		Permission: catalogderive.ExemptPermission, RequiredRelation: "system_admin",
	})
	if complaint == "" {
		t.Fatalf("exempt с отношением прошёл молча")
	}
}
