// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// foreignproductnameincontract.go — разбор ПРОДУКТОВЫХ ИМЁН ЧУЖИХ ОБЛАКОВ в
// дереве контрактов (ban #2).
//
// # Почему предмет — именно дерево контрактов
//
// Запрет на упоминания чужих облаков держится сегодня одной проверкой, и её
// популяция — КОНСОЛЬ (`ui-future/shared/src/test/console-naming-schemes.test.ts`,
// ось `product-title`). Дерево контрактов в её обход не входит by construction,
// и это не край: `.proto` — публичный текст, который читает арендатор, а
// комментарий метода уезжает В ПОРОЖДЁННЫЕ ЗАГЛУШКИ опубликованного модуля
// фундамента, то есть в чужие репозитории, куда никакая проверка этого дерева
// уже не дотянется.
//
// Замер 2026-09-16, ради которого гейт заведён: словарь консольной оси,
// прогнанный по НЕ-консольному дереву, дал ровно одно попадание —
// `proto/corelib/operation/operation_service.proto`, комментарий к `Cancel`
// говорил о чужом API хранилища (PRO-Robotech/corelib#10). Комментарий пережил
// сам продукт, из которого приехал, и доехал до заглушек фундамента двумя
// местами.
//
// # Словарь здесь СВОЙ, и это названо, а не спрятано
//
// Консоль судит свою популяцию своим словарём, здесь — свой. Это ДВА места об
// одном запрете, и они разойдутся: ось `product-title` консоли ловит четыре
// имени, этот гейт — их же. Свести словари в один источник нечем — популяции
// читают разные среды (TypeScript и Go), — и предмет заведён отдельной задачей
// PRO-Robotech/kacho-workspace#653. До её закрытия расхождение держится тем, что
// обе стороны называют друг друга.
//
// Имена собственных доменов Kachō сюда НЕ входят: `Kachō Network Load Balancer`
// — наше собственное описание, и консольная ось `load-balancer-taxonomy`,
// применённая к дереву продукта, дала бы 4 находки на законном тексте. Словарь
// этого гейта ограничен ПРОДУКТОВЫМИ ИМЕНАМИ ЧУЖИХ платформ.
//
// # Чего разбор НЕ видит — названо
//
//  1. имя, разорванное переносом строки внутри комментария: `Object\n// Storage`.
//     Разбор судит строку, а не склеенный текст комментария;
//  2. имя в кириллической транслитерации («Обжект Сторидж») — предмет другой и
//     ловится обзором;
//  3. чужое имя в ЗНАЧЕНИИ поля, а не в комментарии: разбор не различает
//     комментарий и код намеренно — в `.proto` публичным текстом является и то,
//     и другое.
package repohygiene

import (
	"sort"
	"strings"
)

// ForeignProductName — одно продуктовое имя чужой платформы и то, чем его
// заменяют.
type ForeignProductName struct {
	// Name — имя, как оно пишется.
	Name string
	// Instead — чем называть то же в терминах Kachō.
	Instead string
}

// ForeignProductNames — словарь гейта. Ровно продуктовые имена ЧУЖИХ платформ:
// собственные домены Kachō сюда не входят (см. шапку).
func ForeignProductNames() []ForeignProductName {
	return []ForeignProductName{
		{Name: "Object Storage", Instead: "домен Kachō, отвечающий за предмет, — Storage"},
		{Name: "Compute Cloud", Instead: "домен Kachō — Compute"},
		{Name: "Container Registry", Instead: "домен Kachō — Registry"},
		{Name: "Managed Service for", Instead: "у Kachō управляемых надстроек нет; называй домен"},
	}
}

// ForeignProductSite — координата находки.
type ForeignProductSite struct {
	File string
	Line int
	// Name — какое именно имя встретилось.
	Name string
	// Instead — чем его заменить.
	Instead string
}

// ScanForeignProductNames разбирает один файл контракта построчно.
//
// Возвращает находки и число ОСМОТРЕННЫХ строк: «ноль находок» обязано быть
// отличимо от «ноль прочитанного».
func ScanForeignProductNames(path string, src []byte, names []ForeignProductName) (
	sites []ForeignProductSite, linesRead int,
) {
	for i, line := range strings.Split(string(src), "\n") {
		linesRead++
		for _, n := range names {
			if strings.Contains(line, n.Name) {
				sites = append(sites, ForeignProductSite{
					File:    path,
					Line:    i + 1,
					Name:    n.Name,
					Instead: n.Instead,
				})
			}
		}
	}
	sort.Slice(sites, func(a, b int) bool {
		if sites[a].File != sites[b].File {
			return sites[a].File < sites[b].File
		}
		return sites[a].Line < sites[b].Line
	})
	return sites, linesRead
}
