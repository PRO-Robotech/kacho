// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package restmux

import (
	"context"
	"net/http"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"

	"github.com/PRO-Robotech/kacho/gateway/internal/listenerorigin"
)

// Mux — REST-фасад края: диспетчер split-mux'а (ServeHTTP) и таблица маршрутов
// внешнего слушателя, на которой стоит сторож маршрута (ExternalRouteGate).
type Mux struct {
	dispatch http.Handler
	// public — mux, обслуживающий внешний слушатель.
	public *runtime.ServeMux
	// routes — ТА ЖЕ таблица маршрутов, что у public: она собрана тем же циклом
	// регистрации, с тем же маршалером и тем же условием `mux == internalMux` у
	// административных служб. Отличие одно — её обработчики не исполняются:
	// промежуточное звено подменяет каждый найденный маршрут передачей запроса
	// цепочке края (forwardMatchedRoute). Сопоставление и ответ на промах
	// поэтому производит сам grpc-gateway, тот же, что отвечает на промах public.
	routes *runtime.ServeMux
}

// ServeHTTP — диспетчер (см. NewMux).
func (m *Mux) ServeHTTP(w http.ResponseWriter, r *http.Request) { m.dispatch.ServeHTTP(w, r) }

type routeGateKey struct{}

// routeGateEntry — то, что сторож передаёт сквозь таблицу маршрутов: исходный
// запрос (с телом) и цепочку, которой его отдать.
type routeGateEntry struct {
	orig *http.Request
	next http.Handler
}

// forwardMatchedRoute — промежуточное звено таблицы маршрутов. Маршрут найден:
// исходный запрос уходит цепочке края. Обработчик таблицы (мост к бэкенду) не
// вызывается НИКОГДА — таблица решает только «есть маршрут или нет».
func forwardMatchedRoute(routes **runtime.ServeMux) runtime.Middleware {
	return func(runtime.HandlerFunc) runtime.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request, _ map[string]string) {
			entry, ok := r.Context().Value(routeGateKey{}).(*routeGateEntry)
			if !ok {
				// Таблица маршрутов спрошена не сторожем — вызывающему, которому
				// её ответ не предназначен, отвечается промахом, а не мостом.
				_, outbound := runtime.MarshalerForRequest(*routes, r)
				runtime.DefaultRoutingErrorHandler(r.Context(), *routes, outbound, w, r, http.StatusNotFound)
				return
			}
			entry.next.ServeHTTP(w, entry.orig)
		}
	}
}

// ExternalRouteGate — сторож маршрута внешнего слушателя (kacho#3053, ban06).
//
// Ставится в цепочке края СНАРУЖИ аутентификации: запрос с внешнего слушателя
// по координате, которую этот слушатель не обслуживает, получает «маршрута нет»
// раньше, чем его увидят слои аутентификации, ступени подтверждения и прав.
// Иначе ответ давали они: аноним получал 401 на внутреннем пути, как на
// публичном, а вызывающий с действующей сессией — 403, чьи подробности называли
// внутренний метод и его право. Внутреннее пространство путей было снаружи
// перечислимо поимённо любому аутентифицированному.
//
// Решение одно для ВСЕХ необслуживаемых координат — внутренних и
// несуществующих разом, — иначе ответ отличал бы внутреннее от несуществующего.
// Цена названа: аноним отличает публичный маршрут (401) от отсутствующего
// (404); таблица публичных маршрутов опубликована и так.
//
// «Обслуживается» берётся из двух производителей, а не из перечня:
//   - собственные пути края на own (`/healthz`, «кто я», записи полосы входа,
//     выход, поток изменений) — любой шаблон own, кроме общего `/`;
//   - публичные маршруты grpc-gateway — таблица routes (см. Mux).
//
// Отказ fail-closed в сторону цепочки: сторож либо отдаёт запрос цепочке, либо
// отвечает промахом grpc-gateway; моста к бэкенду он не вызывает ни на одной
// ветке. Ошибка сопоставления поэтому стоит доступности, но не обходит
// аутентификацию. Внутренний слушатель сторож не судит: там обслуживается всё.
func (m *Mux) ExternalRouteGate(own *http.ServeMux, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !listenerorigin.IsExternal(r.Context()) {
			next.ServeHTTP(w, r)
			return
		}
		if own != nil {
			if _, pattern := own.Handler(r); pattern != "" && pattern != "/" {
				next.ServeHTTP(w, r)
				return
			}
		}
		// Таблица маршрутов спрашивается КОПИЕЙ запроса без тела: разбор формы
		// и подмена метода (X-HTTP-Method-Override) у grpc-gateway меняют запрос,
		// а цепочке уходит исходный — с телом и с методом, который прислал
		// вызывающий.
		probe := r.WithContext(context.WithValue(r.Context(), routeGateKey{}, &routeGateEntry{orig: r, next: next}))
		probe.Body = http.NoBody
		m.routes.ServeHTTP(w, probe)
	})
}
