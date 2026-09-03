// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// offers_only_live_types_test.go — витрина не предлагает того, на что ключ
// выдачу не примет (#1976).
//
// # Предмет
//
// Витрина строит ответ из ПЕРЕЧНЯ СБОРКИ (`authzmap.Catalog()`) и базы не
// спрашивает — это сказано в шапке `list_catalog.go`. Строки каталога с #1861
// вправе быть УЖЕ этого перечня: снятая строка живой не считается, а перечень
// сборки её по-прежнему называет. Тогда арендатор видит в витрине тип, на
// который выдача отвергается ключом (`role_rule_ref_res_fk` /
// `role_verb_type_fk` → `catalog_resource(..., live)`).
//
// Доступ при этом НЕ расширяется: отказ ключа fail-closed. Цена другая — клиент
// читает витрину как перечень того, что можно выдать, и получает отказ на том,
// что витрина ему предложила.
//
// # Чего эта проба НЕ делает
//
// Она НЕ выбирает исход. Их три (витрина спрашивает строки · помечает снятые ·
// объявляет, что показывает возможности образа), выбор продуктовый и закрывается
// приёмкой, а не пробой. Проба закрывает ровно то, ради чего расхождение названо
// в задаче: чтобы оно не считалось незамеченным. Сегодня пересечение ПУСТО;
// проба краснеет в тот день, когда снимут строку, которую перечень сборки ещё
// называет, — то есть ровно когда выбор исхода становится срочным.
package permission_catalog

import (
	"sort"
	"testing"

	"github.com/PRO-Robotech/kacho/services/iam/internal/authzmap"
	"github.com/PRO-Robotech/kacho/services/iam/internal/domain"
)

// offeredButRetired — пары, которые витрина называет, а решение о снятии уже
// вынесено. Вынесено отдельной функцией, чтобы предикат можно было предъявить
// синтетическому входу: на живом дереве пересечение пусто, и без такого
// предъявления «ноль находок» было бы неотличимо от «предикат ничего не ищет».
func offeredButRetired(offered []string, retired map[string]bool) []string {
	var out []string
	for _, dotted := range offered {
		if retired[dotted] {
			out = append(out, dotted)
		}
	}
	sort.Strings(out)
	return out
}

// offeredPairs — точечные пары, которые витрина отдаёт арендатору.
func offeredPairs(t *testing.T) []string {
	t.Helper()
	resp := callCatalog(t)
	var out []string
	for _, m := range resp.GetModules() {
		for _, r := range m.GetResources() {
			out = append(out, m.GetModule()+"."+r.GetResource())
		}
	}
	sort.Strings(out)
	return out
}

// TestCatalogOffersNoRetiredType — витрина не называет типа, чья строка снята.
func TestCatalogOffersNoRetiredType(t *testing.T) {
	offered := offeredPairs(t)
	retiredList := domain.RetiredTypes()

	retired := make(map[string]bool, len(retiredList))
	for _, ty := range retiredList {
		retired[ty] = true
	}

	// Два положительных контроля. Без ПЕРВОГО отрицание зеленело бы на витрине,
	// не назвавшей ничего; без ВТОРОГО — на пустом перечне снятого, то есть
	// тогда, когда сверять не с чем. Оба состояния снаружи выглядят как «чисто».
	if len(offered) == 0 {
		t.Fatal("витрина не назвала ни одной пары: обход пуст, и всякое отрицание ниже " +
			"зеленеет, ничего не проверив")
	}
	if len(retired) == 0 {
		t.Fatal("перечень снятых типов пуст: сверять не с чем, и «пересечение пусто» " +
			"означало бы «не искали», а не «чисто»")
	}

	got := offeredButRetired(offered, retired)

	t.Logf("осмотрено: витрина называет пар %d, перечень сборки %d, снятых типов %d; "+
		"предложено снятого %d", len(offered), len(authzmap.Catalog()), len(retired), len(got))

	if len(got) > 0 {
		t.Errorf("витрина предлагает %d снятых типов %v: выдача на них отвергается ключом "+
			"каталога, то есть возможность объявлена и неисполнима. Исход выбирается "+
			"продуктовым решением (#1976): витрина спрашивает живые строки · помечает "+
			"снятые · объявляет, что показывает возможности образа", len(got), got)
	}
}

// TestCatalogOffersNoRetiredTypeDetectsAnOverlap — предъявление предиката
// синтетическому входу: на живом дереве пересечение пусто, и без этой пробы
// «ноль находок» было бы неотличимо от предиката, который не ищет ничего.
//
// Меняется РОВНО ОДИН факт против положительного близнеца выше: тот же перечень
// витрины, но снятым объявлен тип, который витрина называет.
func TestCatalogOffersNoRetiredTypeDetectsAnOverlap(t *testing.T) {
	offered := offeredPairs(t)
	if len(offered) == 0 {
		t.Fatal("витрина не назвала ни одной пары — предъявлять предикат не на чем")
	}

	// Законный близнец: снятым объявлен тип, которого витрина НЕ называет.
	if got := offeredButRetired(offered, map[string]bool{"compute.disk": true}); len(got) != 0 {
		t.Fatalf("законный близнец обязан молчать: тип снят, но витриной не предлагается, "+
			"получено %v", got)
	}

	// Инъекция: снятым объявлен ПЕРВЫЙ из тех, что витрина называет.
	victim := offered[0]
	got := offeredButRetired(offered, map[string]bool{victim: true})
	if len(got) != 1 || got[0] != victim {
		t.Fatalf("предикат обязан назвать пересечение по имени, ждали [%s], получено %v",
			victim, got)
	}
}
