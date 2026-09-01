// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package authzmap

// catalog_seed.go — ЕДИНСТВЕННЫЙ производитель строк каталога модуля.
//
// # Зачем это существует
//
// Каталог типов заведён строками в `kacho_iam.catalog_module` / `catalog_resource`
// / `catalog_verb` (миграция 20260901113757, задача #1030), а его источником остаются
// литералы этого пакета: `objectTypes` — какие пары грантуемы, `typeVerbRelations`
// — какие глаголы объявлены каждым типом.
//
// Мест, знающих соответствие «литерал → строка», обязано быть ОДНО. Их трое:
// посев миграции, гейт паритета дерева и страж старта. Разойдясь, они дали бы
// худший из исходов — посев, согласный с гейтом, и оба неверные относительно
// того, что судит ключ. Поэтому перечень выводится здесь, а те трое его ЗОВУТ.
//
// # Почему не «сгенерировать SQL и забыть»
//
// Литерал меняется — миграция уже применена и правке не подлежит (запрет #5).
// Значит расхождение неизбежно, и вопрос не «как его не допустить», а «как
// сделать его ВИДИМЫМ». Отсюда две проверки поверх одного перечня: гейт дерева
// сверяет литерал с текстом миграции, страж старта — литерал с живыми строками.

import "sort"

// CatalogSeedRow — одна строка посева каталога ресурсов.
type CatalogSeedRow struct {
	Module   string
	Resource string
	// Dotted — производная форма, та же, какой говорит `role_verb.object_type` и
	// `role_rule_selectors.object_types`. Хранится колонкой под проверкой
	// согласия, а не собирается читателем: словарей имени типа в дереве ровно
	// два, и третьего здесь не заводится.
	Dotted string
}

// CatalogSeedVerb — одна строка посева каталога глаголов.
type CatalogSeedVerb struct {
	Module   string
	Resource string
	// Verb — каноническая форма БЕЗ приставки отношения, та же, какой говорит
	// `role_verb.verb`. Приставку знает компилятор модели.
	Verb string
}

// CatalogSeedResources — грантуемые пары в порядке точечного ключа.
func CatalogSeedResources() []CatalogSeedRow {
	entries := Catalog()
	out := make([]CatalogSeedRow, 0, len(entries))
	for _, e := range entries {
		out = append(out, CatalogSeedRow{
			Module:   e.Module,
			Resource: e.Resource,
			Dotted:   e.Module + "." + e.Resource,
		})
	}
	return out
}

// CatalogSeedVerbs — пары «(модуль, ресурс) × глагол» в детерминированном
// порядке. Тип, у которого набор глаголов не объявлен вовсе, строк не даёт: у
// него нет отношения `v_*`, и строка каталога адресовала бы отношение, которого
// нет в модели.
func CatalogSeedVerbs() []CatalogSeedVerb {
	out := make([]CatalogSeedVerb, 0, 128)
	for _, r := range CatalogSeedResources() {
		fgaType, ok := ObjectType(r.Module, r.Resource)
		if !ok {
			continue
		}
		verbs := VerbsOfType(fgaType)
		sort.Strings(verbs)
		for _, v := range verbs {
			out = append(out, CatalogSeedVerb{Module: r.Module, Resource: r.Resource, Verb: v})
		}
	}
	return out
}
