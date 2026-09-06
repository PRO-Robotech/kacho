// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// stand_image_producer_injection_test.go — доказательство падучести
// TestEveryStandLocalImageHasAProducer.
//
// ПРЕДПОСЫЛКА объявлена и проверяется первой: вход берётся ИЗ ДЕРЕВА (настоящие
// профили стендов и настоящий images.txt), а не сочиняется. Дефект вносится в
// эту копию ОДНИМ фактом — тем самым, что разошёлся вживую. Синтетический вход
// доказал бы работу арифметики, а не то, что гейт видит дерево.
//
// Осей ЧЕТЫРЕ, и каждая — своё расхождение; при каждой рядом стоит ЗАКОННЫЙ
// БЛИЗНЕЦ: семь остальных служб не тронуты и обязаны молчать. Без него красное
// доказывало бы лишь, что функция умеет возвращать непустой перечень.
package deploy_test

import (
	"strings"
	"testing"
)

func TestStandImageProducerGateCanFail(t *testing.T) {
	byService, byComponent := imageProducers(t)
	asked, standsSeen, _ := collectAskedLocalImages(t)

	if standsSeen == 0 || len(byService) == 0 {
		t.Fatalf("предпосылка не выполнена: стендов %d, производителей %d — дефект вносить не во что",
			standsSeen, len(byService))
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
	if _, ok := byComponent["kaname"]; !ok {
		t.Fatalf("предпосылка не выполнена: images.txt не знает компонента kaname — " +
			"дефект, ради которого проба заведена, вносить некуда")
	}

	// Контроль: дерево как есть — находок ноль. Без него всякое красное ниже
	// доказывало бы только то, что функция умеет возвращать непустой перечень.
	if got := imageDisagreements(asked, byService, byComponent); len(got) != 0 {
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
	// проверяется тем, что ЧИСЛО находок равно одной: остальные семь молчат.
	countMentioning := func(fs []string, needle string) int {
		n := 0
		for _, f := range fs {
			if strings.Contains(f, needle) {
				n++
			}
		}
		return n
	}

	t.Run("сборка называет прежнее выведенное имя — тот самый дефект", func(t *testing.T) {
		svc, comp := copyMap(byService), copyMap(byComponent)
		svc["iam"], comp["kaname"] = "kacho-iam", "kacho-iam"
		got := imageDisagreements(asked, svc, comp)
		if len(got) == 0 {
			t.Fatal("гейт промолчал на расхождении, которое роняло стенд вживую")
		}
		if countMentioning(got, "kacho-iam") != len(got) {
			t.Errorf("находки задели не только внесённый дефект: %v", got)
		}
		for _, other := range []string{"kacho-vpc", "kacho-geo", "kacho-storage"} {
			if countMentioning(got, "компонент "+other) != 0 {
				t.Errorf("законный близнец %s попал в находки — гейт ловит форму, а не расхождение", other)
			}
		}
		t.Logf("внесён один факт → находок %d, все про kacho-iam; семь нетронутых служб молчат", len(got))
	})

	t.Run("производителя у компонента нет вовсе", func(t *testing.T) {
		svc, comp := copyMap(byService), copyMap(byComponent)
		delete(comp, "kaname")
		delete(svc, "iam")
		got := imageDisagreements(asked, svc, comp)
		if countMentioning(got, "производителя у него нет") == 0 {
			t.Fatalf("гейт промолчал на компоненте без производителя: %v", got)
		}
	})

	t.Run("производитель объявлен, а не просит его никто", func(t *testing.T) {
		svc, comp := copyMap(byService), copyMap(byComponent)
		svc["выдуманная"] = "kacho-nobody-asks"
		comp["выдуманная"] = "kacho-nobody-asks"
		got := imageDisagreements(asked, svc, comp)
		if countMentioning(got, "kacho-nobody-asks") != 1 {
			t.Fatalf("вторая сторона расхождения не поймана: %v", got)
		}
	})

	t.Run("профиль просит образ, которого не объявлял никто", func(t *testing.T) {
		a := copyAsked()
		a[standWithImages]["выдуманный"] = "kacho-never-built"
		got := imageDisagreements(a, byService, byComponent)
		if countMentioning(got, "kacho-never-built") != 1 {
			t.Fatalf("новый компонент профиля прошёл незамеченным: %v", got)
		}
	})

	t.Run("Makefile службы собирает под другим именем", func(t *testing.T) {
		declared := map[string]string{}
		for svc, img := range byService {
			declared[svc] = img + ":dev"
		}
		if got := makefileImageDisagreements(declared, byService); len(got) != 0 {
			t.Fatalf("контроль: согласные объявления дали %d находок: %v", len(got), got)
		}
		declared["iam"] = "kacho-iam:dev"
		got := makefileImageDisagreements(declared, byService)
		if countMentioning(got, "служба iam") != 1 || len(got) != 1 {
			t.Fatalf("третья сторона расхождения не поймана ровно одной находкой: %v", got)
		}
		delete(declared, "iam")
		if countMentioning(makefileImageDisagreements(declared, byService), "не объявляет IMAGE") != 1 {
			t.Fatal("снятое объявление IMAGE прошло незамеченным")
		}
	})
}
