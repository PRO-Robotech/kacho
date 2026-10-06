// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware

import (
	"strings"

	"github.com/PRO-Robotech/corelib/ids"
	corevalidate "github.com/PRO-Robotech/corelib/validate"
)

// idMintingRoot — корень контракта, чьи службы чеканят id своих типов ТОЛЬКО
// функциями фундамента: `ids.NewID` (префикс + 17 знаков Crockford) и
// `ids.NewHyphenID` (префикс-17 знаков Crockford). Иной формы у id этих типов не
// бывает, поэтому край вправе судить её сам (kacho#2976).
//
// Корень назван здесь, а не взят перечнем `contractroot.Roots`, потому что
// свойство принадлежит ОДНОМУ корню: у службы доступа (`kaname`) есть законные id
// вне канона формы — системные роли `rol000000000sysadmin` и
// `rol000000000sysviewer` (kaname internal/domain/constants_extended.go), — и
// форму её типов судит она сама.
const idMintingRoot = "kacho"

// knownHyphenPrefixes — префиксы формы с дефисом, объявленные фундаментом.
// `ids.KnownHyphenPrefixes` отдаёт новую карту на каждый вызов, поэтому она
// берётся один раз.
var knownHyphenPrefixes = ids.KnownHyphenPrefixes()

// hasCanonicalIDShape — id ровно той формы, какую выдаёт чеканка фундамента:
//
//   - слитная: `ids.HasKnownPrefix` — длина, известный префикс, тело Crockford;
//   - с дефисом: префикс из `ids.KnownHyphenPrefixes`, тело — те же 17 знаков
//     Crockford.
//
// Пары `HasKnownPrefix` для формы с дефисом у фундамента нет. Тело судится ЕГО
// ЖЕ кодом: приставленное к известному слитному префиксу, оно проходит
// `ids.HasKnownPrefix` ровно тогда, когда в нём столько знаков, сколько выдаёт
// чеканка, и все они из алфавита Crockford. Своей копии алфавита и длины здесь
// нет, и разойтись с чеканкой им не с чем.
func hasCanonicalIDShape(id string) bool {
	if ids.HasKnownPrefix(id) {
		return true
	}
	hy := strings.IndexByte(id, '-')
	if hy <= 0 {
		return false
	}
	if _, ok := knownHyphenPrefixes[id[:hy]]; !ok {
		return false
	}
	return ids.HasKnownPrefix(ids.PrefixNetwork + id[hy+1:])
}

// resourceIDFormAccepted — суд полосы 5b над формой id конкретной области.
//
// Запись, чей тип чеканит служба корня idMintingRoot (catalogEntry.judgesIDShape,
// выводится при загрузке каталога — markIDShapeJudged), судится строгой формой
// hasCanonicalIDShape. Прочие записи — прежним судом `validate.ResourceID`:
// известный префикс, без длины и алфавита тела. Его словарь расширяется ручками
// `EXTRA_RESOURCE_ID_PREFIXES`; строгий суд их не читает — у типов, которые
// чеканит kacho, префикс вне словаря фундамента не выдаётся.
func resourceIDFormAccepted(entry CatalogEntry, id string) bool {
	if entry.judgesIDShape {
		return hasCanonicalIDShape(id)
	}
	return corevalidate.ResourceID("resource", "", id) == nil
}

// markIDShapeJudged — выставляет judgesIDShape записям, чей id область край
// судит строгой формой.
//
// Поля «служба-владелец типа» в каталоге нет, поэтому принадлежность выводится из
// самого каталога, двумя признаками сразу:
//
//  1. метод принадлежит корню idMintingRoot — по пакету его FQN;
//  2. тип области не служит конкретной областью ни одному методу ДРУГОГО корня.
//
// Второй признак отсекает типы, которые служба kacho лишь называет, а владеет
// ими служба доступа: `project` — область 40 записей методов kacho, но и 5
// записей `kaname.cloud.iam`. Выписанного перечня таких типов нет: он разошёлся
// бы с каталогом молча.
func markIDShapeJudged(entries map[string]CatalogEntry) {
	foreign := make(map[string]struct{})
	for fqn, e := range entries {
		if isConcreteResourceScope(e) && fqnRoot(fqn) != idMintingRoot {
			foreign[e.ScopeExtractor.ObjectType] = struct{}{}
		}
	}
	for fqn, e := range entries {
		if !isConcreteResourceScope(e) || fqnRoot(fqn) != idMintingRoot {
			continue
		}
		if _, isForeign := foreign[e.ScopeExtractor.ObjectType]; isForeign {
			continue
		}
		e.judgesIDShape = true
		entries[fqn] = e
	}
}

// fqnRoot — первый сегмент пакета FQN метода: "kacho" у
// "kacho.cloud.storage.v1.SnapshotService/Get".
func fqnRoot(fqn string) string {
	root, _, _ := strings.Cut(fqn, ".")
	return root
}
