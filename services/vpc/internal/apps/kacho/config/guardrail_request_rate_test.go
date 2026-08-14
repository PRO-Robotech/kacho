// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config_test

// guardrail_request_rate_test.go — страж S7: величины допуска запросов.
//
// Предмет тот же, что у перечня служебных диапазонов (S6), и довод дословно тот
// же: у настройки есть состояние «не задана», и оно НЕ безобидно. Нулевые
// величины означают «не ограничиваем», а не «ограничивать нечего»; ограничитель
// при этом либо не навешивается вовсе, либо навешивается пустым — и в обоих
// случаях выглядит включённым, ни разу не отказав.
//
// Отличие от S6 названо явно: там негодной бывает ОТДЕЛЬНАЯ запись, здесь —
// НЕПОЛНЫЙ набор осей. Оператор, задавший темп и забывший одновременность,
// считает предел выставленным, а стоимость одного мгновения остаётся
// неограниченной.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/config"
)

// rateLimitedProductionConfig — боевая посадка, где величины ОБЪЯВЛЕНЫ.
// Положительный контроль для каждого отрицания ниже.
func rateLimitedProductionConfig() config.Config {
	c := config.Config{}
	c.AuthN.Mode = config.ModeProduction
	c.APIServer.RateLimit.Public = config.AdmissionLimitsConfig{
		ReadPerSec: 100, MutationPerSec: 20, BurstFactor: 5, InFlight: 16,
	}
	c.APIServer.RateLimit.Internal = config.AdmissionLimitsConfig{
		ReadPerSec: 1000, MutationPerSec: 500, BurstFactor: 5, InFlight: 256,
	}
	return c
}

// TestRequestRateGuardPassesOnADeclaredProductionPosture — ПОЛОЖИТЕЛЬНЫЙ
// КОНТРОЛЬ ко всем отрицаниям файла.
//
// Без него «страж отверг настройку» неотличимо от «страж отвергает всё».
func TestRequestRateGuardPassesOnADeclaredProductionPosture(t *testing.T) {
	require.NoError(t, rateLimitedProductionConfig().ValidateRequestRateLimits())
}

// TestRequestRateGuardRefusesUndeclaredProduction — боевая посадка без величин
// не поднимается, и отказ НАЗЫВАЕТ обе ручки.
func TestRequestRateGuardRefusesUndeclaredProduction(t *testing.T) {
	c := rateLimitedProductionConfig()
	c.APIServer.RateLimit.Public = config.AdmissionLimitsConfig{}
	c.APIServer.RateLimit.Internal = config.AdmissionLimitsConfig{}

	err := c.ValidateRequestRateLimits()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "api-server.rate-limit.public",
		"отказ обязан называть ручку, которую оператору предстоит выставить")
	assert.Contains(t, err.Error(), "api-server.rate-limit.internal",
		"оператор обязан увидеть ОБА листенера за один прогон, а не чинить их по одному за перезапуск")
}

// TestRequestRateGuardRefusesEachListenerSeparately — листенеры судятся порознь.
//
// Публичный и внутренний имеют РАЗНЫХ вызывающих и разные величины, поэтому
// объявленный один не покрывает другого.
func TestRequestRateGuardRefusesEachListenerSeparately(t *testing.T) {
	t.Run("не объявлен публичный", func(t *testing.T) {
		c := rateLimitedProductionConfig()
		c.APIServer.RateLimit.Public = config.AdmissionLimitsConfig{}
		err := c.ValidateRequestRateLimits()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "api-server.rate-limit.public")
		assert.NotContains(t, err.Error(), "api-server.rate-limit.internal",
			"объявленный листенер в отказе не упоминается — иначе оператор чинит не то")
	})
	t.Run("не объявлен внутренний", func(t *testing.T) {
		c := rateLimitedProductionConfig()
		c.APIServer.RateLimit.Internal = config.AdmissionLimitsConfig{}
		err := c.ValidateRequestRateLimits()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "api-server.rate-limit.internal")
	})
}

// TestRequestRateGuardRefusesAPartialDeclarationInAnyMode — НЕПОЛНЫЙ набор осей
// негоден САМ ПО СЕБЕ и отвергается в любом режиме.
//
// Это та же граница, что у S6 между «посадка не объявила» (вопрос режима) и
// «объявление противоречит себе» (негодность). Заданный темп при нулевой
// одновременности — не выбор оператора, а опечатка, и молча принять её значило бы
// получить предел, который оператор считает выставленным, а процесс — нет.
func TestRequestRateGuardRefusesAPartialDeclarationInAnyMode(t *testing.T) {
	for _, mode := range []config.Mode{config.ModeDev, config.ModeProduction, config.ModeProductionStrict} {
		t.Run(mode.String(), func(t *testing.T) {
			c := rateLimitedProductionConfig()
			c.AuthN.Mode = mode
			c.APIServer.RateLimit.Public = config.AdmissionLimitsConfig{
				ReadPerSec: 100, MutationPerSec: 20, BurstFactor: 5, // одновременность забыта
			}
			err := c.ValidateRequestRateLimits()
			require.Error(t, err, "неполный набор осей негоден в ЛЮБОМ режиме")
			assert.Contains(t, err.Error(), "api-server.rate-limit.public")
		})
	}
}

// TestRequestRateGuardRefusesASelfContradictingBurst — кратность всплеска ниже
// единицы отвергается в любом режиме.
//
// Всплеск ниже устойчивого темпа означает ведро, которое не наполняется до одного
// токена: ограничитель отвергает даже законный поток, то есть выглядит работающим
// и ломает продукт.
func TestRequestRateGuardRefusesASelfContradictingBurst(t *testing.T) {
	c := rateLimitedProductionConfig()
	c.AuthN.Mode = config.ModeDev
	c.APIServer.RateLimit.Public.BurstFactor = 0.5

	err := c.ValidateRequestRateLimits()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "api-server.rate-limit.public")
}

// TestRequestRateGuardStaysSilentOnDevWithNothingDeclared — dev с ПУСТЫМ набором
// молчит.
//
// Dev остаётся режимом внутрипроцессных фикстур; любой РАЗВЁРНУТЫЙ стенд работает
// в боевом режиме (правило #16), поэтому освобождение dev не оставляет дыры ни на
// одном стенде. Граница ровно та же, что у S5/S6, и названа здесь, чтобы её не
// приняли за послабление.
func TestRequestRateGuardStaysSilentOnDevWithNothingDeclared(t *testing.T) {
	c := config.Config{}
	c.AuthN.Mode = config.ModeDev

	require.NoError(t, c.ValidateRequestRateLimits())
}

// TestAdmissionLimitsReachTheCarrierUnchanged — величины доезжают до носителя
// ТЕМ ЖЕ значением, которое одобрил страж.
//
// Один источник на процесс: и страж, и проводка спрашивают один метод. Иначе
// «страж считал одно, листенер получил другое» стало бы возможным молча.
func TestAdmissionLimitsReachTheCarrierUnchanged(t *testing.T) {
	c := rateLimitedProductionConfig()

	pub := c.PublicAdmissionLimits()
	require.True(t, pub.IsDeclared())
	assert.Equal(t, float64(100), pub.ReadPerSec)
	assert.Equal(t, float64(20), pub.MutationPerSec)
	assert.Equal(t, float64(5), pub.BurstFactor)
	assert.Equal(t, 16, pub.InFlight)

	internal := c.InternalAdmissionLimits()
	require.True(t, internal.IsDeclared())
	assert.Greater(t, internal.ReadPerSec, pub.ReadPerSec,
		"внутренний листенер обязан идти с пределами ЗАВЕДОМО ВЫШЕ рабочих: ограничитель, "+
			"задушивший наш собственный поток намерения, воспроизводит заклинивание головы очереди — "+
			"класс, при котором работа не доезжает без единого видимого симптома")
	assert.Greater(t, internal.InFlight, pub.InFlight)
}
