// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// stand_image_producer_injection_test.go — доказательство падучести
// TestEveryStandLocalImageHasAProducer и TestServiceMakefileImageAgreesWithTheStand.
//
// ПРЕДПОСЫЛКА объявлена и проверяется первой: вход берётся ИЗ ДЕРЕВА (настоящие
// профили стендов, настоящий перечень рецепта и настоящий источник имён), а не
// сочиняется. Дефект вносится в КОПИЮ этого входа ОДНИМ фактом — тем самым, что
// разошёлся вживую. Синтетический вход доказал бы работу арифметики, а не то,
// что гейт видит дерево.
//
// При каждой оси рядом стоит ЗАКОННЫЙ БЛИЗНЕЦ: службы, которых дефект не
// касался, обязаны молчать. Без него красное доказывало бы лишь, что функция
// умеет возвращать непустой перечень.
package deploy_test

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/productnaming"
)

func TestStandImageProducerGateCanFail(t *testing.T) {
	svcs := standRecipeServices(t)
	produced := recipeProducedImages(t, svcs)
	asked, standsSeen, _ := collectAskedLocalImages(t)

	if standsSeen == 0 || len(produced) == 0 {
		t.Fatalf("предпосылка не выполнена: стендов %d, производимых образов %d — "+
			"дефект вносить не во что", standsSeen, len(produced))
	}
	var standWithImages string
	for s := range asked {
		if len(asked[s]) > 0 && (standWithImages == "" || s < standWithImages) {
			standWithImages = s
		}
	}
	if standWithImages == "" {
		t.Fatalf("предпосылка не выполнена: ни один стенд дерева не просит локального образа")
	}
	// Дефект, ради которого проба заведена, вносится в ту самую часть, что
	// разошлась вживую. Если её имя перестало быть каноническим — вносить некуда.
	kaname := productnaming.ChartName("iam")
	if produced[kaname] != "iam" {
		t.Fatalf("предпосылка не выполнена: рецепт не производит образа %q службы iam — "+
			"дефект, ради которого проба заведена, вносить некуда", kaname)
	}
	var kanameComp string
	for comp, img := range asked[standWithImages] {
		if img == kaname {
			kanameComp = comp
		}
	}
	if kanameComp == "" {
		t.Fatalf("предпосылка не выполнена: стенд %q не просит образа %q", standWithImages, kaname)
	}

	// Контроль: дерево как есть — находок ноль. Без него всякое красное ниже
	// доказывало бы только то, что функция умеет возвращать непустой перечень.
	if got := imageDisagreements(asked, produced); len(got) != 0 {
		t.Fatalf("контроль: на нетронутом дереве ожидалось 0 находок, получено %d: %v", len(got), got)
	}

	copyAsked := func() standAsked {
		out := standAsked{}
		for s, m := range asked {
			c := map[string]string{}
			for k, v := range m {
				c[k] = v
			}
			out[s] = c
		}
		return out
	}
	copyMap := func(m map[string]string) map[string]string {
		out := map[string]string{}
		for k, v := range m {
			out[k] = v
		}
		return out
	}
	// countMentioning — сколько находок называют это имя. Законный близнец
	// проверяется тем, что находок ровно столько, сколько внесено фактов:
	// остальные службы молчат.
	countMentioning := func(fs []string, needle string) int {
		n := 0
		for _, f := range fs {
			if strings.Contains(f, needle) {
				n++
			}
		}
		return n
	}
	legitimateTwinsSilent := func(t *testing.T, got []string) {
		t.Helper()
		for _, other := range []string{"kacho-vpc", "kacho-geo", "kacho-storage", "kacho-registry"} {
			if countMentioning(got, other) != 0 {
				t.Errorf("законный близнец %s попал в находки — гейт ловит форму, а не расхождение: %v",
					other, got)
			}
		}
	}

	t.Run("рецепт вернулся к выводу имени приставкой — тот самый дефект", func(t *testing.T) {
		// Ровно то, что было вживую: профиль просит `kaname:dev`, а сборка
		// кладёт в узлы `kacho-iam:dev`.
		p := copyMap(produced)
		delete(p, kaname)
		p["kacho-iam"] = "iam"
		got := imageDisagreements(asked, p)
		if len(got) == 0 {
			t.Fatal("гейт промолчал на расхождении, которое роняло стенд вживую")
		}
		if countMentioning(got, kaname) == 0 {
			t.Errorf("находка не назвала спрошенного образа %s: %v", kaname, got)
		}
		if countMentioning(got, "kacho-iam") == 0 {
			t.Errorf("находка не назвала того, что собирается вместо него: %v", got)
		}
		legitimateTwinsSilent(t, got)
		t.Logf("внесён один факт → находок %d, обе половины расхождения названы; "+
			"нетронутые службы молчат", len(got))
	})

	t.Run("производителя у спрошенного образа нет вовсе", func(t *testing.T) {
		p := copyMap(produced)
		delete(p, kaname)
		got := imageDisagreements(asked, p)
		if countMentioning(got, "производителя у него нет") == 0 {
			t.Fatalf("гейт промолчал на образе без производителя: %v", got)
		}
		legitimateTwinsSilent(t, got)
	})

	t.Run("рецепт собирает образ, которого не просит никто", func(t *testing.T) {
		p := copyMap(produced)
		p["kacho-nobody-asks"] = "выдуманная"
		got := imageDisagreements(asked, p)
		if countMentioning(got, "kacho-nobody-asks") != 1 {
			t.Fatalf("вторая сторона расхождения не поймана ровно одной находкой: %v", got)
		}
		legitimateTwinsSilent(t, got)
	})

	t.Run("профиль просит образ, которого не собирает никто", func(t *testing.T) {
		a := copyAsked()
		a[standWithImages]["выдуманный"] = "kacho-never-built"
		got := imageDisagreements(a, produced)
		if countMentioning(got, "kacho-never-built") != 1 {
			t.Fatalf("новый компонент профиля прошёл незамеченным: %v", got)
		}
		legitimateTwinsSilent(t, got)
	})

	t.Run("Makefile службы собирает под другим именем", func(t *testing.T) {
		declared := map[string]string{}
		for img, svc := range produced {
			declared["../"+svc+"/Makefile"] = img + ":dev"
		}
		if got := makefileImageDisagreements(declared, produced); len(got) != 0 {
			t.Fatalf("контроль: согласные объявления дали %d находок: %v", len(got), got)
		}
		declared["../iam/Makefile"] = "kacho-iam:dev"
		got := makefileImageDisagreements(declared, produced)
		if countMentioning(got, "kacho-iam:dev") != 1 {
			t.Fatalf("третья сторона расхождения не поймана: %v", got)
		}
		if countMentioning(got, kaname+":dev") != 1 {
			t.Fatalf("вторая половина той же находки не названа: %v", got)
		}
		legitimateTwinsSilent(t, got)
	})

	t.Run("Makefile службы не объявляет IMAGE вовсе", func(t *testing.T) {
		declared := map[string]string{}
		for img, svc := range produced {
			declared["../"+svc+"/Makefile"] = img + ":dev"
		}
		declared["../iam/Makefile"] = ""
		got := makefileImageDisagreements(declared, produced)
		if countMentioning(got, "не объявляет IMAGE") != 1 {
			t.Fatalf("снятое объявление IMAGE прошло незамеченным: %v", got)
		}
	})
}
