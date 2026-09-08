// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Судящая часть гейта «контракт платформы не зависит от контракта выносимой
// службы доступа». Вынесена из пробы, чтобы инъекция гоняла ЕЁ, а не свою копию
// разбора.

// protoImportRe — объявление импорта в описании контракта.
//
// Судится ОПЕРАТОР, а не строка с именем: имя дерева контрактов службы стоит и в
// комментариях самих контрактов (там объясняют, откуда взят тип), поэтому
// предикат по подстроке краснел бы на собственном объяснении
// (`testing.md` §«Гейт на класс», п.4). Форма `weak`/`public` перед путём —
// законная часть синтаксиса, и распознаватель обязан её знать: контракт,
// объявленный ею, ушёл бы из-под наблюдения молча.
var protoImportRe = regexp.MustCompile(`(?m)^\s*import\s+(?:weak\s+|public\s+)?"([^"]+)"\s*;`)

// ContractSplitCensus — объём осмотренного. Печатается ВСЕГДА: «ноль находок»
// обязано быть отличимо от «ноль прочитанного», и отдельно — от «прочитали одно
// дерево из двух».
type ContractSplitCensus struct {
	// PlatformFiles — описаний контракта платформы осмотрено.
	PlatformFiles int
	// PlatformImports — импортов в них прочитано ВСЕГО.
	PlatformImports int
	// ServiceFiles, ServiceImports — то же по дереву выносимой службы. Читается
	// ради ОБРАТНОЙ стороны: импорты службы в платформу законны и многочисленны,
	// и напечатанное их число показывает, что распознаватель видит обе стороны, а
	// не ослеп на одной.
	ServiceFiles, ServiceImports int
	// ServiceToPlatform — из них ведущих в платформу; законны и не судятся.
	ServiceToPlatform int
}

func (c ContractSplitCensus) String() string {
	return fmt.Sprintf(
		"осмотрено: контрактов платформы %d (импортов %d), контрактов службы %d "+
			"(импортов %d, из них в платформу %d — законны и не судятся)",
		c.PlatformFiles, c.PlatformImports, c.ServiceFiles, c.ServiceImports, c.ServiceToPlatform)
}

// ContractFile — одно описание контракта: путь от корня дерева контрактов и текст.
type ContractFile struct {
	// Path — путь от каталога `proto/`, слешами: именно им контракт и
	// адресуется в операторе импорта, поэтому обе стороны сравнения записаны в
	// одном словаре.
	Path string
	// Src — исходник.
	Src string
}

// AuditContractSplitDirection судит НАПРАВЛЕНИЕ зависимости между двумя
// деревьями контрактов.
//
// # Предмет
//
// Служба доступа выносится отдельным продуктом. Её контракт вправе зависеть от
// контракта платформы — она её потребитель, и таких рёбер два десятка. Обратное
// ребро означает, что платформа не собирается без выносимой службы: снять её
// контракт станет нельзя, пока платформенный его импортирует, и обнаружится это
// не разбором, а неразрешимой сборкой контрактов.
//
// # Почему направление судится, а не перечисляется
//
// Перечень законных рёбер живёт ровно до первого нового контракта и стареет
// молча: запись, которой нечего покрывать, неотличима от записи, чей предмет
// ещё не завели. Направление же — свойство, а не список: оно не требует
// сопровождения и истекает только вместе с самим разделением.
func AuditContractSplitDirection(platform, service []ContractFile, platformRoot, serviceRoot string) ([]string, ContractSplitCensus) {
	cen := ContractSplitCensus{PlatformFiles: len(platform), ServiceFiles: len(service)}

	type edge struct{ from, to string }
	var bad []edge

	for _, f := range platform {
		for _, m := range protoImportRe.FindAllStringSubmatch(f.Src, -1) {
			cen.PlatformImports++
			if strings.HasPrefix(m[1], serviceRoot+"/") {
				bad = append(bad, edge{from: f.Path, to: m[1]})
			}
		}
	}
	for _, f := range service {
		for _, m := range protoImportRe.FindAllStringSubmatch(f.Src, -1) {
			cen.ServiceImports++
			if strings.HasPrefix(m[1], platformRoot+"/") {
				cen.ServiceToPlatform++
			}
		}
	}

	sort.Slice(bad, func(i, j int) bool {
		if bad[i].from != bad[j].from {
			return bad[i].from < bad[j].from
		}
		return bad[i].to < bad[j].to
	})

	findings := make([]string, 0, len(bad))
	for _, e := range bad {
		findings = append(findings,
			"контракт платформы "+e.from+" импортирует контракт выносимой службы "+e.to+
				": платформа перестаёт собираться без службы, а снять контракт службы "+
				"станет нельзя, пока этот импорт жив")
	}
	return findings, cen
}
