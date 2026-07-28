// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config_test

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/services/storage/internal/check"
)

// Degraded-mode ручка фильтра видимости (KACHO_STORAGE_LIST_FILTER_FAIL_OPEN) на
// ошибке iam отдаёт прочитанную страницу НЕОТФИЛЬТРОВАННОЙ. Пока карта прав несёт
// хотя бы одну запись ScopeFiltered, за этой ручкой не остаётся ничего: у такого
// RPC per-RPC Check не задаётся вовсе, поэтому фильтр там не второй слой, а
// единственный.
//
// Отсюда требование к страже: включённый fail-open в production обязан ОТКАЗЫВАТЬ
// в старте. Размен осознанный — стенд, намеренно выставивший fail-open, не
// поднимется, и это верно: снять защиту одной ручкой незаметно больше нельзя.

// TestValidate_productionRefusesListFilterFailOpen — сама стража. Ослаблено ровно
// одно измерение: всё остальное в посадке secureProd() безопасно.
func TestValidate_productionRefusesListFilterFailOpen(t *testing.T) {
	c := secureProd()
	c.ListFilterFailOpen = true

	err := c.Validate()
	if err == nil {
		t.Fatal("production with LIST_FILTER_FAIL_OPEN=true must refuse to start " +
			"(on an iam error the page is returned unfiltered, and the ScopeFiltered " +
			"RPC has no per-RPC Check behind it)")
	}
	if !strings.Contains(err.Error(), "LIST_FILTER_FAIL_OPEN") {
		t.Fatalf("refusal must name the knob, got %v", err)
	}

	// production-strict — требование не слабее.
	c = secureProd()
	c.AuthMode = "production-strict"
	c.ListFilterFailOpen = true
	if err := c.Validate(); err == nil {
		t.Fatal("production-strict with LIST_FILTER_FAIL_OPEN=true must refuse to start")
	}

	// Обратная сторона: та же посадка с fail-closed стартует — гейт отвергает
	// именно это измерение, а не «всё подряд».
	c = secureProd()
	c.ListFilterFailOpen = false
	if err := c.Validate(); err != nil {
		t.Fatalf("fail-closed production posture must start, got %v", err)
	}
}

// TestValidate_failOpenGuardIsArmedByTheScopeFilteredMarks — связывает два артефакта
// наблюдаемо: карту прав (какие RPC остались без per-RPC Check) и config.Validate
// (что именно отвергается на старте). Снимут стражу — покраснеет здесь, где видно,
// ЧТО именно осталось без защиты, а не только в конфиг-тестах, где связь с картой
// не видна.
//
// Зеркалит check.TestScopeFilteredRPCs_AreBackedByTheProductionBootGuard, но для
// второй ручки: включённый фильтр, который на ошибке iam отдаёт страницу целиком,
// защищает не больше выключенного.
func TestValidate_failOpenGuardIsArmedByTheScopeFilteredMarks(t *testing.T) {
	var scopeFiltered []string
	for fullMethod, e := range check.PermissionMap() {
		if e.ScopeFiltered {
			scopeFiltered = append(scopeFiltered, fullMethod)
		}
	}
	if len(scopeFiltered) == 0 {
		t.Skip("карта не несёт ScopeFiltered RPC — стража здесь по построению no-op")
	}

	c := secureProd()
	c.ListFilterFailOpen = true
	err := c.Validate()
	if err == nil {
		t.Fatalf("production must refuse to start: %v rely on the filter entirely, "+
			"and fail-open bypasses it on any iam error", scopeFiltered)
	}
	// Отказ обязан назвать поверхность, которая осталась без защиты, — иначе
	// оператор видит запрет ручки без причины и снимет стражу, а не ручку.
	for _, m := range scopeFiltered {
		if !strings.Contains(err.Error(), m) {
			t.Fatalf("refusal must name the unguarded RPC %q, got %v", m, err)
		}
	}
}

// TestValidate_devToleratesFailOpen — стража производственная. dev-посадка (только
// in-process фикстуры; на РАЗВЁРНУТОМ стенде dev запрещён отдельным правилом)
// по-прежнему поднимается, иначе гейт сломал бы локальные фикстуры вместо того,
// чтобы закрыть боевую дыру.
func TestValidate_devToleratesFailOpen(t *testing.T) {
	c := secureProd()
	c.AuthMode = "dev"
	c.ListFilterFailOpen = true
	if err := c.Validate(); err != nil {
		t.Fatalf("dev mode must tolerate fail-open, got %v", err)
	}
}
