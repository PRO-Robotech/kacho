// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package declaredbreak

// Текст отказа НЕ НАЗЫВАЕТ причины, которой не проверял.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ, измеренный 2026-09-11 (#2594)
//
// Гейт отказался выносить вердикт (код 2) — и был прав. Но вместе с отказом он
// назвал причину: «наиболее вероятная причина — форма путей: запускай из каталога
// контрактов». Догадка добросовестная, правдоподобная и НЕВЕРНАЯ: прогонщик зовёт
// `buf` именно изнутри каталога, а расхождение пришло оттуда, что судимое дерево
// было не тем, которое уезжало. Читатель пошёл чинить форму путей и дефекта там не
// нашёл, потому что его там не было.
//
// Вторичная догадка в тексте отказа опаснее её отсутствия: она уводит от предмета и
// стоит захода. Поэтому отказ обязан называть ТО, ЧТО ВИДЕЛ, — обе половины вердикта
// и по образцу каждой стороны, — а причину оставить читателю, у которого есть то,
// чего нет у гейта: знание, какое дерево он судил.
//
// Утверждения двусторонние: одного «нет слова „причина“» мало — без образцов отказ
// стал бы беднее прежнего, и это было бы ухудшением, а не починкой.

import (
	"strings"
	"testing"
)

func TestIncoherentReportNamesWhatItSawAndNoCause(t *testing.T) {
	findings := []Finding{
		{Type: "RPC_NO_DELETE", Path: "proto/kacho/cloud/vpc/v1/a.proto", StartLine: 12, Message: `RPC "X" was deleted.`},
		{Type: "FIELD_NO_DELETE", Path: "proto/kacho/cloud/vpc/v1/b.proto", StartLine: 7, Message: `field "y" was deleted.`},
	}
	decls := []Declaration{
		{Rule: "RPC_NO_DELETE", Path: "kacho/cloud/vpc/v1/a.proto", Symbol: "X", Issue: "#1"},
		{Rule: "FIELD_NO_DELETE", Path: "kacho/cloud/vpc/v1/b.proto", Symbol: "y", Issue: "#2"},
	}
	res := Adjudicate(findings, decls)
	if !res.Incoherent() {
		t.Fatalf("фикстура не воспроизводит самопротиворечивый вердикт — утверждать нечего: %+v", res)
	}
	report := res.IncoherentReport()

	// 1. Обе половины вердикта названы числами.
	for _, want := range []string{"осмотрено находок 2", "записей перечня 2", "сопоставлено 0"} {
		if !strings.Contains(report, want) {
			t.Errorf("отказ не называет объёма осмотренного: нет %q\n%s", want, report)
		}
	}

	// 2. Названо то, ЧТО ГЕЙТ ВИДЕЛ, — по образцу с каждой стороны. Без них отказ
	//    беднее прежнего: читателю нечего сравнить, и он идёт гадать сам.
	for _, want := range []string{
		"proto/kacho/cloud/vpc/v1/a.proto",
		"kacho/cloud/vpc/v1/a.proto",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("отказ не показал образца стороны: нет %q\n%s", want, report)
		}
	}

	// 3. И НЕ НАЗВАНО ничего, чего гейт не проверял.
	for _, banned := range []string{
		"вероятная причина",
		"Запускай так же, как конвейер",
		"из корня репозитория",
	} {
		if strings.Contains(report, banned) {
			t.Errorf("отказ называет непроверенную причину — %q:\n%s", banned, report)
		}
	}
}

// TestIncoherentReportIsEmptyWhenTheVerdictIsCoherent — отрицание в паре с
// положительным: отчёт об отказе существует ТОЛЬКО у отказа. Без этой стороны
// проба зеленела бы на реализации, печатающей его всегда.
func TestIncoherentReportIsEmptyWhenTheVerdictIsCoherent(t *testing.T) {
	res := Adjudicate(
		[]Finding{{Type: "RPC_NO_DELETE", Path: "kacho/cloud/vpc/v1/a.proto", Message: `RPC "X" was deleted.`}},
		[]Declaration{{Rule: "RPC_NO_DELETE", Path: "kacho/cloud/vpc/v1/a.proto", Symbol: "X", Issue: "#1"}},
	)
	if res.Incoherent() {
		t.Fatalf("положительный контроль сам самопротиворечив — сравнивать не с чем: %+v", res)
	}
	if got := res.IncoherentReport(); got != "" {
		t.Errorf("вердикт согласован, а отчёт об отказе не пуст:\n%s", got)
	}
}
