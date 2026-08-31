// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

// TestSubscriptionAsksTheFloorAfterThePage — пол журнала берётся НЕ РАНЬШЕ
// страницы (задача #1764).
//
// Разбор предмета, формы и предпосылок — в шапке
// `subscriptionfloorafterpage.go`; здесь он не пересказывается, иначе два места
// об одном предмете разошлись бы молча.
func TestSubscriptionAsksTheFloorAfterThePage(t *testing.T) {
	root := repoRoot(t)
	var log strings.Builder

	findings, census, err := AuditSubscriptionFloorAfterPage(
		SubscriptionFloorAfterPageOptions{Root: root}, &log)
	if err != nil {
		t.Fatalf("обход дерева: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))

	for _, f := range findings {
		t.Errorf("НАХОДКА: %s", f.What)
	}

	// Объём осмотренного утверждается ОТДЕЛЬНО: «ноль находок» обязано быть
	// отличимо от «ноль прочитанного». Пустые полосы анализатор роняет сам
	// ошибкой; здесь — вторая половина: каждая рассмотренная функция обязана
	// оказаться в полосе верного порядка, иначе расхождение выше не было
	// названо находкой.
	if len(census.OrderOK) != len(census.Judged) {
		t.Errorf("рассмотрено функций %d, порядок верен у %d — расхождение обязано быть находкой выше",
			len(census.Judged), len(census.OrderOK))
	}
	for _, where := range census.Judged {
		t.Logf("рассмотрено: %s", where)
	}
}
