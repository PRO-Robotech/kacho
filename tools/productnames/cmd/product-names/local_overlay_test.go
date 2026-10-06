// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// local_overlay_test.go — режим `--local-overlay` как его зовёт рецепт стенда:
// ИСХОД процесса (код и напечатанная накладка), а не только решение библиотеки.
//
// Решение о каждой форме объявления доказано у самой библиотеки
// (internal/localimages). Здесь — то, чего она знать не может: что режим
// ОТКАЗЫВАЕТСЯ отвечать там, где вызывающий иначе получил бы пустую накладку и
// поднял бы стенд на опубликованных образах чужой ревизии, и что находка
// (несобранная часть) даёт отдельный код, а не пустой успех.
package main

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func runOverlay(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	var out, errOut strings.Builder
	rc := localOverlayMode(&out, &errOut, args)
	return out.String(), errOut.String(), rc
}

// TestLocalOverlayPrintsTheOverlayOfTheChain — положительный контроль: цепочка
// из двух слоёв, накладка переводит часть этого дерева на образ сборки, а пин
// вынесенной части остаётся где был.
func TestLocalOverlayPrintsTheOverlayOfTheChain(t *testing.T) {
	ext := externalImageName(t)
	base := writeValues(t, "values.yaml", "vpc:\n  image: docker.io/prorobotech/kacho-vpc:main-1\n"+
		ext+":\n  image:\n    repository: docker.io/prorobotech/"+ext+"\n    tag: 366-1\n")
	site := writeValues(t, "values.site.yaml", "vpc:\n  image: docker.io/prorobotech/kacho-vpc:2798-2\n")

	out, census, rc := runOverlay(t, "-tag", "dev", "-built", "kacho-vpc kacho-compute", base, site)
	if rc != 0 {
		t.Fatalf("код %d, перепись: %s", rc, census)
	}
	var overlay map[string]any
	if err := yaml.Unmarshal([]byte(out), &overlay); err != nil {
		t.Fatalf("накладка не разбирается как YAML (%v):\n%s", err, out)
	}
	vpc, _ := overlay["vpc"].(map[string]any)
	if vpc["image"] != "kacho-vpc:dev" {
		t.Errorf("часть этого дерева не переведена на образ сборки: %v", overlay)
	}
	if _, touched := overlay[ext]; touched {
		t.Errorf("пин вынесенной части переписан: %v", overlay)
	}
	for _, want := range []string{"объявлений образа", "переписано 1", "не объявлены цепочкой: kacho-compute"} {
		if !strings.Contains(census, want) {
			t.Errorf("перепись не называет %q: %s", want, census)
		}
	}
}

// TestLocalOverlayFindingIsNotAnEmptySuccess — вход различается РОВНО перечнем
// собранного: часть этого дерева, которую сборка не произвела, — код 1 с именем
// образа; тот же вход с собранной частью — код 0.
func TestLocalOverlayFindingIsNotAnEmptySuccess(t *testing.T) {
	f := writeValues(t, "values.yaml", "vpc:\n  image: docker.io/prorobotech/kacho-vpc:main-1\n"+
		"compute:\n  image: docker.io/prorobotech/kacho-compute:main-1\n")

	_, census, rc := runOverlay(t, "-tag", "dev", "-built", "kacho-vpc", f)
	if rc != 1 || !strings.Contains(census, "kacho-compute") {
		t.Errorf("несобранная часть: код %d, перепись %q — ожидался код 1 с именем образа", rc, census)
	}
	if _, _, rc := runOverlay(t, "-tag", "dev", "-built", "kacho-vpc,kacho-compute", f); rc != 0 {
		t.Errorf("законный близнец: всё собрано, а код %d", rc)
	}
}

// TestLocalOverlayRefusesInsteadOfAnsweringEmpty — ПУСТОЙ ОТВЕТ НЕ ЕСТЬ УСПЕХ.
// Пустая накладка при коде 0 подняла бы стенд на опубликованных образах, и
// вердикт о посадке относился бы к чужой ревизии.
func TestLocalOverlayRefusesInsteadOfAnsweringEmpty(t *testing.T) {
	part := writeValues(t, "values.yaml", "vpc:\n  image: docker.io/prorobotech/kacho-vpc:main-1\n")
	foreign := writeValues(t, "values.yaml", "pg-vpc:\n  image: { registry: docker.io, repository: bitnamilegacy/postgresql }\n")
	for _, c := range []struct {
		name string
		args []string
		want string
	}{
		{"слоёв не названо", []string{"-tag", "dev", "-built", "kacho-vpc"}, "не названо ни одного слоя"},
		{"собранное не названо", []string{"-tag", "dev", part}, "не назван перечень собранного"},
		{"тег не назван", []string{"-built", "kacho-vpc", part}, "не назван тег"},
		{"слой не читается", []string{"-tag", "dev", "-built", "kacho-vpc", part + ".нет"}, "не читается"},
		{"части этого дерева не объявлены", []string{"-tag", "dev", "-built", "kacho-vpc", foreign}, "переводить нечего"},
	} {
		t.Run(c.name, func(t *testing.T) {
			out, census, rc := runOverlay(t, c.args...)
			if rc != 2 || !strings.Contains(census, c.want) {
				t.Errorf("код %d, диагностика %q — ожидался код 2 с причиной %q", rc, census, c.want)
			}
			if out != "" {
				t.Errorf("при отказе напечатана накладка: %q", out)
			}
		})
	}
}
