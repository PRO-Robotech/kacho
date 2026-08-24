// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// basic_credential_cache_test.go — заполнение кэша вердиктов базовой полосы
// выведено СЕРИЕЙ, а не только записью в журнал (#1221).
package metrics_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
	gwmetrics "github.com/PRO-Robotech/kacho/gateway/internal/observability/metrics"
)

// TestFreshProcessDeclaresTheVerdictCacheFill — три величины стоят на
// поверхности с первой секунды, включая нулевые.
//
// Потолок объявляется СЕРИЕЙ, а не только строкой журнала при старте: строка
// живёт в чужом хранилище, её нельзя ни сложить с занятостью, ни построить по
// ней долю. Занятость без потолка — число без шкалы; потолок без занятости —
// шкала без числа.
func TestFreshProcessDeclaresTheVerdictCacheFill(t *testing.T) {
	m := gwmetrics.New("test", "deadbeef")
	m.RegisterBasicCredentialCache(func() middleware.BasicCredentialCacheStats {
		return middleware.BasicCredentialCacheStats{Capacity: 4096}
	})

	body := expose(t, m)
	for _, want := range []string{
		`kacho_api_gateway_basic_credential_verdict_cache_entries 0`,
		`kacho_api_gateway_basic_credential_verdict_cache_capacity 4096`,
		`kacho_api_gateway_basic_credential_verdict_cache_evictions_total 0`,
	} {
		assert.Contains(t, body, want,
			"величина заполнения обязана стоять нулём, а не отсутствовать")
	}
}

// TestVerdictCacheFillFollowsTheLane — три величины идут ИЗ ПОЛОСЫ и не
// перепутаны между собой.
//
// Числа намеренно разные: одинаковые сделали бы невидимой перепутанную пару
// «серия ↔ поле снимка», а их здесь ровно три — тот размер, на котором такая
// опечатка и заводится.
func TestVerdictCacheFillFollowsTheLane(t *testing.T) {
	m := gwmetrics.New("test", "deadbeef")
	m.RegisterBasicCredentialCache(func() middleware.BasicCredentialCacheStats {
		return middleware.BasicCredentialCacheStats{
			Entries: 17, Capacity: 4096, Evictions: 93, AtCapacity: true,
		}
	})

	body := expose(t, m)
	for _, want := range []string{
		`kacho_api_gateway_basic_credential_verdict_cache_entries 17`,
		`kacho_api_gateway_basic_credential_verdict_cache_capacity 4096`,
		`kacho_api_gateway_basic_credential_verdict_cache_evictions_total 93`,
	} {
		assert.Contains(t, body, want)
	}
}

// TestVerdictCacheFillIsAbsentWithoutRegistration — инъекция в обе стороны к
// двум пробам выше.
//
// Без неё «серии присутствуют» неотличимо от пробы, которая не читает ничего.
func TestVerdictCacheFillIsAbsentWithoutRegistration(t *testing.T) {
	m := gwmetrics.New("test", "deadbeef") // читатель НЕ провязан
	body := expose(t, m)
	assert.NotContains(t, body, "kacho_api_gateway_basic_credential_verdict_cache",
		"снятие провязки обязано быть заметно пробе")

	// Законный близнец: реестр без этого коллектора всё равно отдаёт свои
	// серии, поэтому «пусто» не может означать «обработчик не ответил».
	assert.Contains(t, body, "kacho_api_gateway_build_info")
}

// TestVerdictCacheFillSurvivesANilReader — провязка наблюдаемости
// nil-безопасна: она не имеет права ронять подъём края.
func TestVerdictCacheFillSurvivesANilReader(t *testing.T) {
	m := gwmetrics.New("test", "deadbeef")
	m.RegisterBasicCredentialCache(nil)
	assert.NotContains(t, expose(t, m), "kacho_api_gateway_basic_credential_verdict_cache")
}
