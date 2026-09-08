// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// moduleselfgating_injection_test.go — ОПЫТ: способна ли сверка объявления с
// прод-кодом упасть по КАЖДОЙ из двух своих сторон и смолчать на согласии
// (задача продукта #2375).
//
// Каждая инъекция меняет РОВНО ОДИН факт против законного близнеца, и дельта
// вычисляется, а не объявляется: контроль обязан быть пуст, инъекция — дать
// ровно одну находку названного вида.
//
// Отдельная ось — ПЕРЕПИСЬ: гейт обязан отличать «ноль находок» от «ноль
// прочитанного», поэтому пустые стороны проверяются как самостоятельный вход.
package repohygiene

import (
	"strings"
	"testing"
)

// selfGatingTwin — законный близнец: объявление и наблюдение сходятся.
func selfGatingTwin() (declared, observed map[string]map[string]bool) {
	declared = map[string]map[string]bool{
		"registry": {"v_get": true, "v_list": true},
		"vpc":      {"v_get": true},
	}
	observed = map[string]map[string]bool{
		"registry": {"v_get": true, "v_list": true},
		"vpc":      {"v_get": true},
	}
	return declared, observed
}

func TestSelfGatingInjection_SilentOnALegitimateTwin(t *testing.T) {
	t.Parallel()
	d, o := selfGatingTwin()
	found, c := auditModuleSelfGating(d, o)
	if len(found) != 0 {
		t.Fatalf("законный близнец объявлен находкой: %v", found)
	}
	if c.PairsDeclared != 3 || c.PairsObserved != 3 || c.ModulesDeclared != 2 || c.ModulesWalked != 2 {
		t.Fatalf("перепись не сходится с входом: %s", c)
	}
}

// TestSelfGatingInjection_FindsADeclarationThatOutlivedItsSubject — ИНЪЕКЦИЯ
// первой стороны: объявлено, а код больше не читает.
//
// Это МОЛЧАЛИВЫЙ исход у читателя: он засчитает читателя, которого нет, и
// мёртвое отношение продолжит оплачиваться выводом, записью и индексом.
func TestSelfGatingInjection_FindsADeclarationThatOutlivedItsSubject(t *testing.T) {
	t.Parallel()
	d, o := selfGatingTwin()
	delete(o["registry"], "v_list")

	found, _ := auditModuleSelfGating(d, o)
	if len(found) != 1 {
		t.Fatalf("одно-фактная инъекция дала %d находок, а обязана одну: %v", len(found), found)
	}
	if !strings.Contains(found[0].String(), "объявление пережило предмет") {
		t.Fatalf("находка не о том: %q", found[0])
	}
	if found[0].Module != "registry" || found[0].Relation != "v_list" {
		t.Fatalf("находка не называет координату: %+v", found[0])
	}
}

// TestSelfGatingInjection_FindsCodeTheDeclarationIsSilentAbout — ИНЪЕКЦИЯ второй
// стороны: код читает, объявление молчит.
//
// Исход противоположный и НАБЛЮДАЕМЫЙ у соседнего продукта: читатель объявления
// назовёт отношение мёртвым и получит ложную находку о чужом коде.
func TestSelfGatingInjection_FindsCodeTheDeclarationIsSilentAbout(t *testing.T) {
	t.Parallel()
	d, o := selfGatingTwin()
	o["vpc"]["v_update"] = true

	found, _ := auditModuleSelfGating(d, o)
	if len(found) != 1 {
		t.Fatalf("одно-фактная инъекция дала %d находок, а обязана одну: %v", len(found), found)
	}
	if !strings.Contains(found[0].String(), "объявление об этом молчит") {
		t.Fatalf("находка не о том: %q", found[0])
	}
	if found[0].Module != "vpc" || found[0].Relation != "v_update" {
		t.Fatalf("находка не называет координату: %+v", found[0])
	}
}

// TestSelfGatingInjection_FindsAModuleDeclaredButNeverWalked — ИНЪЕКЦИЯ: модуль
// объявлен целиком, а обхода у него нет вовсе. Без этой оси запись про
// несуществующий модуль жила бы вечно: она ничему не противоречит.
func TestSelfGatingInjection_FindsAModuleDeclaredButNeverWalked(t *testing.T) {
	t.Parallel()
	d, o := selfGatingTwin()
	d["vpc-operator"] = map[string]bool{"v_get": true}

	found, _ := auditModuleSelfGating(d, o)
	if len(found) != 1 {
		t.Fatalf("одно-фактная инъекция дала %d находок, а обязана одну: %v", len(found), found)
	}
	if found[0].Module != "vpc-operator" || !found[0].Declared || found[0].Observed {
		t.Fatalf("находка не о снятом модуле: %+v", found[0])
	}
}

// TestSelfGatingInjection_EmptySidesAreDistinguishable — перепись обязана
// РАЗЛИЧАТЬ пустые стороны, а не схлопывать их в «находок нет».
//
// Пустое объявление при непустом обходе — это столько находок, сколько пар в
// обходе; пустой обход при непустом объявлении — зеркально. Вердикт «находок 0»
// здесь недостижим by construction, и это то самое свойство, ради которого гейт
// печатает объём осмотренного.
func TestSelfGatingInjection_EmptySidesAreDistinguishable(t *testing.T) {
	t.Parallel()
	_, o := selfGatingTwin()
	found, c := auditModuleSelfGating(map[string]map[string]bool{}, o)
	if len(found) != 3 {
		t.Fatalf("пустое объявление при обходе из 3 пар дало %d находок: %v", len(found), found)
	}
	if c.PairsDeclared != 0 || c.PairsObserved != 3 {
		t.Fatalf("перепись схлопнула пустую сторону: %s", c)
	}

	d, _ := selfGatingTwin()
	found, c = auditModuleSelfGating(d, map[string]map[string]bool{})
	if len(found) != 3 {
		t.Fatalf("пустой обход при объявлении из 3 пар дал %d находок: %v", len(found), found)
	}
	if c.PairsObserved != 0 || c.PairsDeclared != 3 {
		t.Fatalf("перепись схлопнула пустую сторону: %s", c)
	}
}

// TestSelfGatingInjection_BothSidesEmptyIsNotAVerdict — обе стороны пусты:
// находок нет, и перепись ОБЯЗАНА это назвать нулями. Вердикт по такому входу
// выносит вызывающий (гейт роняет прогон), а не предикат: сам по себе пустой
// вход не является ни согласием, ни находкой.
func TestSelfGatingInjection_BothSidesEmptyIsNotAVerdict(t *testing.T) {
	t.Parallel()
	found, c := auditModuleSelfGating(map[string]map[string]bool{}, map[string]map[string]bool{})
	if len(found) != 0 {
		t.Fatalf("пустой вход дал находки: %v", found)
	}
	if c.PairsDeclared != 0 || c.PairsObserved != 0 || c.ModulesDeclared != 0 || c.ModulesWalked != 0 {
		t.Fatalf("перепись пустого входа не ноль: %s", c)
	}
}
