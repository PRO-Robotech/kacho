// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gwmetrics "github.com/PRO-Robotech/kacho/gateway/internal/observability/metrics"
	"github.com/PRO-Robotech/kacho/pkg/servicecontract"
	"github.com/PRO-Robotech/kacho/pkg/servicehost"
)

// journal — журнал, который проба ЧИТАЕТ. Утверждать про «строку с причиной»
// по её отсутствию в выводе процесса нельзя: там её не видно ни при каком
// исходе.
func journal(t *testing.T) (*slog.Logger, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	return slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})), &buf
}

// TestDiagnosticSurfaceDeclaredDisabledDoesNotGoSilent — OBS-1-14.
//
// Профиль объявляет адрес поверхности пустым: сборка проходит, отказа старта
// нет, слушатель не привязывается, а причина названа словами — и в объявлении, и
// в самоотчёте профиля при подъёме. Различие «профиль забыл» ↔ «сбора здесь нет
// намеренно» иначе не выражается ничем.
func TestDiagnosticSurfaceDeclaredDisabledDoesNotGoSilent(t *testing.T) {
	logger, buf := journal(t)
	desc, err := describeDiagnosticSurface("", gwmetrics.New("test", "x"),
		servicecontract.ModeProduction, logger)
	require.NoError(t, err, "объявленное выключение — не отказ старта")
	assert.False(t, desc.Enabled(), "поверхность не поднимается")

	because := desc.DisabledBecause()
	assert.Contains(t, because, "KACHO_API_GATEWAY_METRICS_ADDR",
		"причина обязана называть РУЧКУ: без имени оператор не знает, что включать")
	assert.Contains(t, because, "сбора величин на этой посадке нет",
		"причина обязана называть СЛЕДСТВИЕ, а не только факт выключения")

	// Самоотчёт: профиль поднимается «вхолостую» и говорит об этом в журнал.
	waitDiag, serveErr := servicehost.ServeSurface(context.Background(), desc)
	require.NoError(t, serveErr)
	require.NoError(t, waitDiag())
	assert.Contains(t, buf.String(), "KACHO_API_GATEWAY_METRICS_ADDR",
		"самоотчёт процесса обязан нести то же состояние, что объявление")
}

// TestDiagnosticSurfaceRisesAndServesExposition — OBS-1-01 (детерминированная
// половина: уровень «в процессе»).
//
// Стендовая половина сценария 01 утверждает то же обращением к поду; здесь
// проверяется то, что от стенда не зависит: по объявленному адресу поднимается
// слушатель, `GET /metrics` отвечает `200` в формате текстовой экспозиции и
// несёт объявленные семейства.
func TestDiagnosticSurfaceRisesAndServesExposition(t *testing.T) {
	logger, _ := journal(t)
	m := gwmetrics.New("test", "x")
	m.RegisterAuthz(func() gwmetrics.AuthzSnapshot { return gwmetrics.AuthzSnapshot{} })

	addr := freeAddr(t)
	desc, err := describeDiagnosticSurface(addr, m, servicecontract.ModeProduction, logger)
	require.NoError(t, err)
	require.True(t, desc.Enabled())

	ctx, cancel := context.WithCancel(context.Background())
	waitDiag, serveErr := servicehost.ServeSurface(ctx, desc)
	require.NoError(t, serveErr)
	defer func() { cancel(); _ = waitDiag() }()

	resp, gerr := (&http.Client{Timeout: 5 * time.Second}).Get("http://" + addr + "/metrics")
	require.NoError(t, gerr)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.True(t, strings.HasPrefix(resp.Header.Get("Content-Type"), "text/plain"),
		"формат текстовой экспозиции, а не что придётся: "+resp.Header.Get("Content-Type"))

	body := make([]byte, 1<<16)
	n, _ := resp.Body.Read(body)
	text := string(body[:n])
	for _, want := range []string{
		"kacho_api_gateway_authz_check_decisions_total",
		"kacho_api_gateway_authz_cache_total",
		"kacho_api_gateway_authz_client_calls_total",
		"kacho_api_gateway_authz_check_duration_seconds",
	} {
		assert.Contains(t, text, want)
	}
}

// TestDiagnosticSurfaceDeclaresItsPosture — досягаемость и решение об
// аутентификации объявлены, и объявлены ИМЕННО так, как их защищает
// задокументированное исключение.
//
// Проба стоит здесь, а не в общем профиле: конструктор поверхности отвергает
// только НЕОБЪЯВЛЕННУЮ ось, а какое именно значение объявил край — его
// собственное решение, и менять его молча нельзя.
func TestDiagnosticSurfaceDeclaresItsPosture(t *testing.T) {
	logger, _ := journal(t)
	desc, err := describeDiagnosticSurface(":9095", gwmetrics.New("test", "x"),
		servicecontract.ModeProduction, logger)
	require.NoError(t, err)

	spec := desc.Spec()
	assert.Equal(t, servicecontract.ReachClusterInternal, spec.Reach,
		"диагностика края НЕ выставляется наружу кластера")
	_, hasMech := spec.Auth.Get()
	assert.False(t, hasMech, "аутентификации здесь нет — это объявленное исключение")
	assert.Contains(t, desc.AuthStatement(), "ОСОЗНАННО")
	assert.Contains(t, desc.AuthStatement(), "внутрь кластера")
}

// TestSurfaceModeFailsClosedOnAnUnknownLabel — неизвестная метка посадки
// читается как БОЕВАЯ.
//
// Тот же fail-closed, что у загрузочного стража края: посадка, выведенная из
// незнания, не должна оказаться самой мягкой.
func TestSurfaceModeFailsClosedOnAnUnknownLabel(t *testing.T) {
	assert.Equal(t, servicecontract.ModeDev, surfaceMode("dev"))
	assert.Equal(t, servicecontract.ModeProduction, surfaceMode("production"))
	assert.Equal(t, servicecontract.ModeProductionStrict, surfaceMode("production-strict"))
	assert.Equal(t, servicecontract.ModeProduction, surfaceMode(""),
		"пустая метка — боевая посадка, а не dev")
	assert.Equal(t, servicecontract.ModeProduction, surfaceMode("что-то-новое"))
}

// freeAddr — свободный адрес на петле. Фиксированный порт сделал бы пробу
// зависимой от того, что ещё поднято на машине.
func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	require.NoError(t, l.Close())
	return addr
}
