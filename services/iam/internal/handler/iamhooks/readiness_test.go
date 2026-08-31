// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package iamhooks

// readiness_test.go — `/readyz` отражает здоровье зависимостей, `/healthz`
// остаётся чистой живостью, а НЕПРОВЯЗАННЫЙ носитель даёт «не готов», а не 200.
//
// Прежняя редакция строила проверки СВОИМ типом (`ReadinessChecker`) — его
// больше нет (#1752): форму именованной проверки объявляет
// `pkg/observability/health`, и об одном предмете высказывается одно место.
// Утверждения о наблюдаемом (коды и имя упавшей зависимости) сохранены
// дословно; добавлены два, которых прежняя форма выразить не могла.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/observability/health"
)

func TestReadinessHandler(t *testing.T) {
	okCheck := health.Checker{Name: "database", Check: func(context.Context) error { return nil }}
	failCheck := health.Checker{Name: "lro-worker", Check: func(context.Context) error { return errors.New("worker down") }}

	get := func(t *testing.T, mux *http.ServeMux, path string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}

	t.Run("all healthy → 200", func(t *testing.T) {
		mux := NewMux(Handlers{Health: health.New([]health.Checker{okCheck})})
		require.Equal(t, http.StatusOK, get(t, mux, "/readyz").Code)
	})

	t.Run("a dependency down → 503 with its name", func(t *testing.T) {
		mux := NewMux(Handlers{Health: health.New([]health.Checker{okCheck, failCheck})})
		rec := get(t, mux, "/readyz")
		require.Equal(t, http.StatusServiceUnavailable, rec.Code)
		require.Contains(t, rec.Body.String(), "lro-worker")
	})

	t.Run("healthz stays pure liveness even when a dependency is down", func(t *testing.T) {
		mux := NewMux(Handlers{Health: health.New([]health.Checker{failCheck})})
		require.Equal(t, http.StatusOK, get(t, mux, "/healthz").Code)
	})

	// НЕПРОВЯЗАННЫЙ носитель — «не готов», а не 200.
	//
	// Прежняя форма на пустом наборе проверок отдавала 200 («пусто = готов»), то
	// есть корень, забывший провязать готовность, получал под, который kubelet
	// считает готовым и на который шлёт трафик. Различить это состояние было
	// нечем: ответ совпадал с ответом полностью исправного сервиса.
	t.Run("carrier not wired → readiness is 503, liveness stays 200", func(t *testing.T) {
		mux := NewMux(Handlers{})
		rec := get(t, mux, "/readyz")
		require.Equal(t, http.StatusServiceUnavailable, rec.Code)
		require.Contains(t, rec.Body.String(), "readiness-carrier")
		// Живость при этом остаётся 200: процесс жив, и перезапускать его
		// незачем — иначе ошибка сборки давала бы шторм перезапусков вместо
		// пода, стоящего вне ротации и называющего причину.
		require.Equal(t, http.StatusOK, get(t, mux, "/healthz").Code)
	})

	// Гашение снимает под из ротации.
	//
	// Своя форма этого не умела вовсе: `/readyz` продолжал отвечать 200, пока
	// серверы уже останавливались, и kubelet слал трафик в закрывающийся под.
	t.Run("shutting down → readiness 503 even when every dependency is healthy", func(t *testing.T) {
		agg := health.New([]health.Checker{okCheck})
		mux := NewMux(Handlers{Health: agg})
		require.Equal(t, http.StatusOK, get(t, mux, "/readyz").Code)
		agg.SetShuttingDown()
		require.Equal(t, http.StatusServiceUnavailable, get(t, mux, "/readyz").Code)
	})

	// Отрицательный контроль маршрутизации: не-GET на диагностической
	// поверхности получает 405 от самого маршрутизатора. Без него «405 больше не
	// отдаётся» прошло бы незамеченным при переходе на образец с методом.
	t.Run("non-GET on the diagnostic surface is rejected by the router", func(t *testing.T) {
		mux := NewMux(Handlers{Health: health.New([]health.Checker{okCheck})})
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/readyz", nil))
		require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	})
}
