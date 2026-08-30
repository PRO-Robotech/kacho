// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import "testing"

// Инъекция гейта «предикат пулового ключа один на дерево» — В ОБЕ СТОРОНЫ.
//
// Гейт судится ПРОГОНОМ, а не чтением описания. Без обратной стороны он ловил бы
// форму, а не существо: `pool_id` — законное имя поля адресного пула и стоит в
// дереве десятками, а сама подстрока законна в объяснении запрета и в доме
// предиката. Гейт, краснеющий на этом, снимут как непонятный, и вместе с ним
// уйдёт настоящая находка.

const injectedGuardFile = "services/vpc/cmd/vpc/main.go"

// TestInjectionRedOnAReturnedSubstringGuard — вернули подстрочную проверку на
// месте вызова → гейт КРАСНЕЕТ и НАЗЫВАЕТ КООРДИНАТУ.
func TestInjectionRedOnAReturnedSubstringGuard(t *testing.T) {
	findings, census := FindPoolParamSubstringChecks(map[string]string{
		injectedGuardFile: `package main

import (
	"fmt"
	"strings"
)

func guard(dsn string) error {
	if strings.Contains(dsn, "pool_") {
		return fmt.Errorf("строка подключения несёт параметр пула (%q)", dsn)
	}
	return nil
}
`,
	})
	if census.Files != 1 {
		t.Fatalf("разобрано файлов %d, ожидался 1 — инъекция не доехала до разбора", census.Files)
	}
	if len(findings) != 1 {
		t.Fatalf("гейт МОЛЧИТ на возвращённой подстрочной проверке (находок %d) — "+
			"он не способен покраснеть, и его зелёный на дереве ничего не значит", len(findings))
	}
	if findings[0].File != injectedGuardFile {
		t.Fatalf("находка называет %q, а проверка стояла в %q", findings[0].File, injectedGuardFile)
	}
	if findings[0].Line != 9 {
		t.Fatalf("находка без верной координаты: строка %d, а вызов на 9-й — "+
			"гейт, который не называет место, невозможно исполнить", findings[0].Line)
	}
}

// TestInjectionSilentOnTheLegitimateTwins — законные близнецы той же формы.
// Каждый подаётся ОТДЕЛЬНО: в общей куче молчание по одному неотличимо от
// молчания по всем.
func TestInjectionSilentOnTheLegitimateTwins(t *testing.T) {
	twins := map[string]string{
		"дом предиката: pkg/db вправе искать ключ как угодно": `package db

import "strings"

func has(dsn string) bool { return strings.Contains(dsn, "pool_") }
`,
		"другой литерал: pool_id — поле адресного пула, а не ключ пула соединений": `package handler

import "strings"

func has(path string) bool { return strings.Contains(path, "pool_id") }
`,
		"подстрока в объяснении и в константе, а не в вызове": `package main

// Здесь запрещено искать "pool_" подстрокой — предикат живёт в pkg/db.
const marker = "pool_"

func name() string { return marker }
`,
	}
	homes := map[string]string{
		"дом предиката: pkg/db вправе искать ключ как угодно":                      "pkg/db/dsn.go",
		"другой литерал: pool_id — поле адресного пула, а не ключ пула соединений": "gateway/internal/handler/pool.go",
		"подстрока в объяснении и в константе, а не в вызове":                      "services/vpc/cmd/vpc/main.go",
	}
	for why, src := range twins {
		t.Run(why, func(t *testing.T) {
			findings, census := FindPoolParamSubstringChecks(map[string]string{homes[why]: src})
			if census.Files+census.Skipped != 1 {
				t.Fatalf("близнец не дошёл до разбора: разобрано %d, пропущено как дом %d — "+
					"молчание здесь означало бы «не читали», а не «нечего сказать»",
					census.Files, census.Skipped)
			}
			if len(findings) != 0 {
				t.Fatalf("гейт КРАСНЕЕТ на законной форме (%v) — он ловит форму, а не существо; "+
					"первый же ложный срабат его отключит", findings)
			}
		})
	}
}

// TestInjectionCensusDistinguishesUnparsedFromClean — «ноль находок» обязано
// быть отличимо от «ноль прочитанного»: неразобранный файл не судится, и его
// молчание не имеет права читаться как чистота.
func TestInjectionCensusDistinguishesUnparsedFromClean(t *testing.T) {
	_, census := FindPoolParamSubstringChecks(map[string]string{
		"services/vpc/cmd/vpc/broken.go": "это не Go",
	})
	if census.Files != 0 || census.Unparsed != 1 {
		t.Fatalf("перепись сказала «разобрано %d, не разобрано %d»; неразобранный файл "+
			"обязан быть назван отдельно, иначе он молча зачитывается в чистоту",
			census.Files, census.Unparsed)
	}
}
