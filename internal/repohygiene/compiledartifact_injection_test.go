// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// compiledartifact_injection_test.go — способность гейта скомпилированных
// артефактов упасть и смолчать, доказанная НАСТОЯЩИМИ первыми байтами.
//
// Инъекция в обе стороны: артефакт каждого узнаваемого вида распознаётся, а
// законный сосед той же формы — текст, YAML, JSON, пустой файл, файл короче
// магии — остаётся молчаливым. Без второй половины гейт ловил бы форму, а не
// существо, и первый же ложный срабат его отключил бы.
package repohygiene

import "testing"

func TestCompiledArtifactPredicateRecognisesEachKind(t *testing.T) {
	cases := map[compiledArtifactKind][]byte{
		kindELF:     {0x7f, 'E', 'L', 'F', 2, 1, 1, 0},
		kindMachO:   {0xcf, 0xfa, 0xed, 0xfe, 7, 0, 0, 1},
		kindPE:      {'M', 'Z', 0x90, 0, 3, 0, 0, 0},
		kindArchive: []byte("!<arch>\n"),
	}
	for want, head := range cases {
		if got := classifyCompiledArtifact(head); got != want {
			t.Errorf("вид %q не распознан: получено %q", want, got)
		}
	}
	if len(cases) == 0 {
		t.Fatal("таблица видов пуста — молчание этой пробы сказано ни о чём")
	}
}

func TestCompiledArtifactPredicateIsSilentOnLegitimateNeighbours(t *testing.T) {
	// Законные соседи той же формы: файлы, которые гейт обходит каждым прогоном
	// и обязан пропускать. Среди них намеренно есть короче магии и пустой —
	// именно на них предикат по длине падал бы паникой, а не молчанием.
	neighbours := map[string][]byte{
		"исходник Go":       []byte("package main\n"),
		"YAML":              []byte("apiVersion: v1\n"),
		"JSON":              []byte("{\"keys\": []}\n"),
		"markdown":          []byte("# Заголовок\n"),
		"сценарий оболочки": []byte("#!/usr/bin/env bash\n"),
		"пустой файл":       {},
		"короче магии":      {'M'},
		"текст, начинающийся с M": []byte("Makefile-подобное содержимое"),
		"PEM": []byte("-----BEGIN CERTIFICATE-----\n"),
	}
	for name, head := range neighbours {
		if got := classifyCompiledArtifact(head); got != kindNone {
			t.Errorf("законный сосед «%s» опознан как артефакт %q — гейт ловит форму, "+
				"а не существо, и первый же ложный срабат его отключит", name, got)
		}
	}
}
