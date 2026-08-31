// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// ct2_idcanon_hyphen_minting.go — анализатор «префикс, который продукт ЧЕКАНИТ
// дефисной формой, обязан входить в каталог дефисных префиксов».
//
// # Предмет
//
// `validate.ResourceID` классифицирует дефисную форму по каталогу
// `ids.KnownHyphenPrefixes()`. Префикс, которым продукт чеканит, но которого в
// каталоге нет, даёт разбор, отвергающий СОБСТВЕННЫЙ идентификатор продукта:
// сегмент до дефиса каталогу неизвестен, а legacy-ветвь берёт первые ТРИ знака —
// то есть `sb-`, где дефис попадает внутрь префикса, и совпасть ей не с чем.
//
// Замер на день заведения (kacho#1722): чеканится дефисной формой **9**
// префиксов, в каталоге **32**; каталогу неизвестны **два** — `sb`
// (`StorageBackend`) и `dtb` (`DiskTypeBinding`), оба admin-only ресурсы
// Internal*-служб storage:
//
//	NewHyphenID("sb")  => sb-6n3ncgvbw8d51jv6p   ResourceID => InvalidArgument
//	NewHyphenID("dtb") => dtb-ynwww8dnasrjc6e7k  ResourceID => InvalidArgument
//
// **Латентность — худшее свойство этой находки, а не смягчение.** storage не
// зовёт проверку формата ни в одном RPC, поэтому сегодня отказа не видно.
// Проявится он у первого, кто поступит ПО КОНВЕНЦИИ и поставит malformed-id-check
// первым стейтментом (`api-conventions.md` §Gotcha'и): он получит отказ на
// идентификаторе, который платформа сама и произвела, и станет искать дефект у
// себя.
//
// # Почему существующий страж этого не ловил — by construction
//
// `pkg/ids` несёт `TestHyphenPrefixConstants_InCanon`, и он верен для своей
// популяции: читает ОДИН файл (`ids.go`) и отбирает константы по имени
// (`^Prefix.*Hyphen$`). Оба недостающих префикса объявлены **в домене storage**
// (`domain.PrefixStorageBackend`, `domain.PrefixDiskTypeBinding`) и этому имени
// не отвечают — страж не мог их увидеть ни при какой внимательности. Это п. 7
// §«Гейт на класс»: распознаватель знал ОДНУ законную форму объявления предмета
// и молчал обо всех, записанных иначе, — не находкой и не зелёным, а
// невидимостью.
//
// # Что судит анализатор
//
// Словарь чеканки ВЫВОДИТСЯ обходом дерева и переиспользуется ЦЕЛИКОМ у гейта
// формы идентификатора (`docsIDFormMintMap`): второй реализации резолва префикса
// не заводится, поэтому разойтись двум гейтам не на чем. Резолв покрывает все
// формы записи аргумента — литерал, константу своего пакета, константу чужого
// через импорт файла, псевдоним константы и транзитивную передачу параметром.
// Литерального `NewHyphenID("dtb")` в дереве НЕТ ни одного: наивный
// распознаватель, ищущий строковый литерал в вызове, этой находки не увидел бы
// вовсе.
//
// # ЧЕГО ОН НЕ СУДИТ
//
//  1. ТЕСТЫ не читаются: предмет — то, что продукт чеканит в бою. Фикстура,
//     минтящая невозможный идентификатор, — отдельный и меньший класс. Именно им
//     оказался третий «недостающий» префикс `dt` из первого замера: производителя
//     в прод-коде у него НЕТ, он живёт в одной интеграционной пробе, и там он
//     вдвойне неверен — идентификатор типа диска — admin-assigned слаг
//     (`block-standard`), а не чеканимый id.
//  2. ОБРАТНАЯ сторона (запись каталога без производителя) здесь не судится: это
//     другой предикат — «послабление, которому нечего исключать», — и у него
//     другой признак. Перепись печатает обе величины, поэтому разрыв виден.
package repohygiene

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// HyphenCanonCensus — объём осмотренного.
type HyphenCanonCensus struct {
	MintedHyphen int
	CanonSize    int
	MintedNames  []string
	GoFiles      int
}

// HyphenCanonFinding — префикс, который чеканится, но каталогу неизвестен.
type HyphenCanonFinding struct {
	Prefix string
	Sites  []string
}

func (f HyphenCanonFinding) String() string {
	return fmt.Sprintf("`%s` — чеканится дефисной формой (%s), но в каталоге дефисных префиксов его нет: "+
		"`validate.ResourceID` отвергнет id, который продукт сам произвёл",
		f.Prefix, strings.Join(f.Sites, ", "))
}

// AuditHyphenMintedPrefixesInCanon выносит вердикт о дереве.
//
// canon передаётся параметром, а не читается изнутри: инъекция обязана уметь
// подать СВОЙ каталог, иначе доказать способность гейта падать нельзя — настоящий
// каталог правится только вместе с продуктом.
func AuditHyphenMintedPrefixesInCanon(
	opts DocsIDFormOptions, canon map[string]struct{}, log io.Writer,
) ([]HyphenCanonFinding, HyphenCanonCensus, error) {
	var idCensus DocsIDFormCensus
	minted, err := docsIDFormMintMap(opts, &idCensus)
	if err != nil {
		return nil, HyphenCanonCensus{}, err
	}

	census := HyphenCanonCensus{CanonSize: len(canon), GoFiles: idCensus.GoFiles}
	var findings []HyphenCanonFinding
	for prefix, forms := range minted {
		if !forms[idFormHyphen] {
			continue
		}
		census.MintedHyphen++
		census.MintedNames = append(census.MintedNames, prefix)
		if _, ok := canon[prefix]; ok {
			continue
		}
		findings = append(findings, HyphenCanonFinding{Prefix: prefix, Sites: idCensus.MintedAt[prefix]})
	}
	sort.Strings(census.MintedNames)
	sort.Slice(findings, func(i, j int) bool { return findings[i].Prefix < findings[j].Prefix })

	if log != nil {
		_, _ = fmt.Fprintf(log,
			"перепись: исходников Go %d · чеканится дефисной формой %d %v · в каталоге дефисных %d · "+
				"чеканится, но каталогу неизвестно: %d\n",
			census.GoFiles, census.MintedHyphen, census.MintedNames, census.CanonSize, len(findings))
	}
	return findings, census, nil
}
