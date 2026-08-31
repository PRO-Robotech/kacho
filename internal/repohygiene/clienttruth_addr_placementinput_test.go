// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

// placementInputOptions — вход вердикта о НАСТОЯЩЕМ дереве.
//
// # Одно послабление, и почему оно именно такой формы
//
// `CreatePlacementGroupRequest` домена compute требует дискриминатор от клиента.
// Снятие требования — ЛОМАЮЩЕЕ изменение: вызывающий, посылающий поле сегодня,
// начал бы получать отказ. Переводится расширением-и-сужением своим изменением
// (`kacho#1621`): сперва поле становится необязательным и выводимым, и только
// после окна вход отвергается.
//
// Запись стоит здесь, а не в виде суженного обхода: суженный обход молчал бы
// навсегда, а послабление ИСТЕКАЕТ САМО.
func placementInputOptions(t *testing.T) PlacementInputOptions {
	t.Helper()
	return PlacementInputOptions{
		Tree:      clientTruthRepoTree(t),
		ProtoRoot: "proto",
		Exemptions: []PlacementInputExemption{
			{
				File:    "proto/kacho/cloud/compute/v1/placement_group_service.proto",
				Message: "CreatePlacementGroupRequest",
				Reason: "требование landed; снятие ломающее — переводится расширением-и-" +
					"сужением, задача kacho#1621",
			},
		},
	}
}

// TestPlacementDiscriminatorIsDeclaredDerived — вердикт о настоящем дереве.
//
// Способность падать доказывает не этот прогон, а инъекция
// (`clienttruth_addr_placementinput_injection_test.go`).
func TestPlacementDiscriminatorIsDeclaredDerived(t *testing.T) {
	var log strings.Builder
	findings, census, err := AuditPlacementInput(placementInputOptions(t), &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))

	if census.ProtoFiles < 20 {
		t.Fatalf("файлов контракта %d — обход пуст, вердикт беспредметен", census.ProtoFiles)
	}
	if census.CreateMessages < 20 {
		t.Fatalf("запросов создания %d — распознаватель сообщений не сработал", census.CreateMessages)
	}
	// Вердикт выносится только о запросах, НЕСУЩИХ поле. Ноль означал бы, что он
	// не вынесен ни разу, — «находок ноль» получено даром.
	if census.WithField == 0 {
		t.Fatalf("запросов с дискриминатором размещения 0 — сверка не состоялась")
	}
	// Канон обязан быть представлен: ноль выводимых означал бы, что требовать
	// нечего.
	if census.Derived == 0 {
		t.Fatalf("объявлено выводимым 0 — канон в дереве не представлен")
	}

	if len(findings) == 0 {
		return
	}
	lines := make([]string, 0, len(findings))
	for _, f := range findings {
		lines = append(lines, "  "+f.String())
	}
	t.Errorf("дискриминатор размещения объявлен входом, а не выводимым "+
		"(запросов с полем %d, выводимым %d, снято послаблением %d):\n%s\n\n"+
		"Пара «координата + дискриминатор» избыточна: какая из двух координат задана, "+
		"то и есть дискриминатор. Требование поля от клиента даёт ему только новую "+
		"возможность противоречить себе. Канон — вывести и отвергнуть вход "+
		"(`data-integrity.md` §Placement-coherence, якорь — подсеть). Строка про "+
		"устаревшее послабление означает обратное: требование снято, снимите запись "+
		"в `placementInputOptions`.",
		census.WithField, census.Derived, census.Exempted, strings.Join(lines, "\n"))
}
