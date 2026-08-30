// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// subscriptionstatelabels.go — анализатор «состояние, которое подписка везёт
// клиенту, несёт МЕТКИ».
//
// # Предмет
//
// Контракт подписки объявляет три оси отбора и говорит прямо, что метки в них не
// входят: метка мутабельна, ресурс входит в выборку и выходит из неё её правкой,
// а сервер, у которого есть только ТЕКУЩЕЕ состояние строки, выход из выборки
// выразить не может. Отбор по меткам поэтому делает КЛИЕНТ — и делает он его
// ровно там, где событие принесло состояние.
//
// Это обещание арендатору, и оно исполнимо ровно до тех пор, пока состояние
// несёт метки. Вид, чьё состояние их не несёт, делает обещание НЕИСПОЛНИМЫМ — и
// делает молча: подписка откроется, события пойдут, а отобрать по метке будет не
// из чего. Класс назван в конвенциях контракта («неисполнимая возможность»), и
// его признак — возможность объявлена, задокументирована, покрыта типами и не
// работает ни при каком входе.
//
// # Откуда берётся перечень видов и перечень типов — ИЗ ДВУХ РАЗНЫХ МЕСТ
//
// Виды считаются по объявлениям владельцев журнала (`subscription.Mapping`), типы
// состояния — по клиентской странице подписки, где они названы полными именами
// контракта. Источника два намеренно: у каждого своя слепота — владелец не знает
// про страницу, страница не знает про владельца, — и сверка их ЧИСЕЛ ловит
// слепоту любого из двух. Один источник давал бы «ноль замечаний» и на чистом
// дереве, и на разборе, переставшем видеть половину предмета.
//
// # Чего разбор НЕ видит — названо, а не спрятано
//
//  1. **вид, объявленный вне композитного литерала** (`kinds[x] = …` отдельным
//     оператором) — считается по литералу `Kinds:`, как его пишут все пять
//     владельцев. Форма умозрительная, но ошибка здесь идёт в сторону
//     РАСХОЖДЕНИЯ ЧИСЕЛ, то есть в сторону замечания, а не молчания.
//  2. **сообщение, объявленное не на верхнем уровне файла контракта** —
//     вложенное сообщение адресуется через точку и в перечне страницы не
//     встречается.
//  3. **метка под другим именем** — поле ищется по имени `labels` и типу
//     отображения строк в строки. Второго имени у меток в этом дереве нет, и
//     заводить его было бы отдельным решением.
package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"sort"
	"strings"
)

// stateTypeMention — полное имя типа состояния, как его называет клиентская
// страница подписки.
var stateTypeMention = regexp.MustCompile(`kacho\.cloud\.([a-z][a-z0-9_]*)\.v1\.([A-Z][A-Za-z0-9]*)`)

// labelsField — объявление поля меток в контракте.
var labelsField = regexp.MustCompile(`(?m)^\s*map<string,\s*string>\s+labels\s*=`)

// StateType — тип состояния, названный клиентской страницей.
type StateType struct {
	// Domain — домен контракта (`vpc`, `storage`).
	Domain string
	// Message — имя сообщения (`Subnet`).
	Message string
}

// FullName — полное имя типа, как оно едет по проводу.
func (t StateType) FullName() string {
	return "kacho.cloud." + t.Domain + ".v1." + t.Message
}

// ScanStateTypesOnPage достаёт со страницы подписки полные имена типов
// состояния. Возвращает РАЗНЫЕ типы, отсортированные, и число упоминаний —
// перепись отдельно от результата.
func ScanStateTypesOnPage(text string) (types []StateType, mentions int) {
	seen := make(map[string]StateType)
	for _, m := range stateTypeMention.FindAllStringSubmatch(text, -1) {
		mentions++
		t := StateType{Domain: m[1], Message: m[2]}
		seen[t.FullName()] = t
	}
	var names []string
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		types = append(types, seen[n])
	}
	return types, mentions
}

// JournalKinds — сколько видов объявил один владелец журнала.
type JournalKinds struct {
	// File — путь объявления от корня дерева.
	File string
	// Count — видов в закрытом словаре владельца.
	Count int
}

// ScanJournalKinds считает виды в объявлении журнала владельца.
//
// Признак — поле `Kinds` композитного литерала, чьё значение само есть
// композитный литерал: столько элементов, сколько видов. Ноль означает, что
// объявления в файле нет вовсе (файл не журнал), и отличается от «журнал с
// пустым словарём» тем, что второго не бывает: владелец с пустым словарём не
// поднимается.
func ScanJournalKinds(rel string, src []byte) (JournalKinds, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, rel, src, parser.SkipObjectResolution)
	if err != nil {
		return JournalKinds{File: rel}, err
	}
	count := 0
	ast.Inspect(file, func(n ast.Node) bool {
		kv, ok := n.(*ast.KeyValueExpr)
		if !ok {
			return true
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok || key.Name != "Kinds" {
			return true
		}
		lit, ok := kv.Value.(*ast.CompositeLit)
		if !ok {
			return true
		}
		count += len(lit.Elts)
		return true
	})
	return JournalKinds{File: rel, Count: count}, nil
}

// MessageCarriesLabels отвечает, несёт ли сообщение message файла контракта поле
// меток.
//
// Возвращает второй величиной признак «сообщение в этом файле найдено»: «нет
// меток» и «нет сообщения» — разные замечания, и слить их значило бы обвинить
// контракт в том, что на деле есть мёртвая координата на странице.
func MessageCarriesLabels(protoSrc, message string) (labels, found bool) {
	head := "\nmessage " + message + " {"
	at := strings.Index("\n"+protoSrc, head)
	if at < 0 {
		return false, false
	}
	body := protoSrc[at:]
	if end := strings.Index(body, "\n}"); end >= 0 {
		body = body[:end]
	}
	return labelsField.MatchString(body), true
}
