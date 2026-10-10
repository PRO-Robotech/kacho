// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// notify_probe_routability_test.go — маршрут края к глаголу стендовой пробы
// notify-probe живёт ТОЛЬКО во внутреннем блоке (решение владельца 2026-10-08
// (1), запрет #6).
//
// Глагол `InternalNotifyProbeService/Send` зовёт администратор кластера своей
// личностью через внутренний край; личность пересылает край, проба доверяет
// пересылке по кругу отправителей. Законных состояний маршрута три, и проба
// судит каждое на одном предмете:
//
//   - адрес пробы объявлен, внутреннее происхождение — маршрут есть;
//   - адрес пробы объявлен, внешнее происхождение — отказ маршрута, побайтно
//     равный ответу на путь, которого у края нет вовсе;
//   - адрес пробы не объявлен — маршрута нет и на внутреннем происхождении.
package restmux

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	notifypb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/notify/v1"

	"github.com/PRO-Robotech/kacho/gateway/internal/listenerorigin"
)

// notifyProbeAddrKnob — ручка адреса пробы notify-probe у края.
const notifyProbeAddrKnob = "KACHO_API_GATEWAY_NOTIFY_PROBE_INTERNAL_GRPC"

// notifyProbeSendRoute — HTTP-метод и путь глагола пробы, выведенные из
// контракта: биндинг `google.api.http`, если контракт его объявляет, иначе
// маршрут grpc-gateway по умолчанию (`generate_unbound_methods`) —
// `POST /<FQN>`. Путь не постулируется, поэтому проба переживает объявление
// биндинга контрактом без правки.
func notifyProbeSendRoute(t *testing.T) (method, path string) {
	t.Helper()
	fqn := strings.TrimPrefix(notifypb.InternalNotifyProbeService_Send_FullMethodName, "/")
	for _, b := range loadedHTTPBindings() {
		if b.fqn == fqn {
			return b.method, probePath(b.template)
		}
	}
	return http.MethodPost, notifypb.InternalNotifyProbeService_Send_FullMethodName
}

// serveProbe прогоняет запрос через диспетчер на объявленном происхождении.
func serveNotifyProbe(t *testing.T, addrs map[string]string, method, path string, internal bool) *httptest.ResponseRecorder {
	t.Helper()
	h, err := NewMux(context.Background(), addrs, nil, nil)
	if err != nil {
		t.Fatalf("NewMux: %v", err)
	}
	req := httptest.NewRequest(method, path, strings.NewReader(`{"address":"probe@example.test"}`))
	if internal {
		req = req.WithContext(listenerorigin.WithInternal(req.Context()))
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestNotifyProbeRouteIsInternalOnly — три состояния маршрута на одном предмете.
func TestNotifyProbeRouteIsInternalOnly(t *testing.T) {
	method, path := notifyProbeSendRoute(t)
	if !isInternalRoute(method, path) {
		t.Fatalf("путь пробы %s %s не классифицируется внутренним — диспетчер отдал бы его "+
			"публичному mux'у, и изоляция ниже держалась бы на случае", method, path)
	}

	t.Run("declared/internal", func(t *testing.T) {
		t.Setenv(notifyProbeAddrKnob, "kacho-notify-probe.kacho.svc:9091")
		addrs := probeAddrs(t)
		if addrs["notifyProbeInternal"] == "" {
			t.Fatal("адрес пробы объявлен, а ключа notifyProbeInternal в карте REST нет")
		}
		if rec := serveNotifyProbe(t, addrs, method, path, true); routingRefusal(rec.Code) {
			t.Fatalf("адрес пробы объявлен, а %s %s на внутреннем листенере отвечает %d — "+
				"маршрута нет", method, path, rec.Code)
		}
	})

	t.Run("declared/external", func(t *testing.T) {
		t.Setenv(notifyProbeAddrKnob, "kacho-notify-probe.kacho.svc:9091")
		addrs := probeAddrs(t)
		got := serveNotifyProbe(t, addrs, method, path, false)
		// Близнец: путь той же формы, которого нет нигде.
		absent := serveNotifyProbe(t, addrs, method,
			"/kacho.cloud.notify.v1.InternalNotifyProbeService/NoSuchMethod", false)
		if got.Code != http.StatusNotFound {
			t.Fatalf("глагол пробы на ВНЕШНЕМ листенере ответил %d — маршрут виден снаружи (запрет #6)", got.Code)
		}
		if got.Code != absent.Code || !bytes.Equal(got.Body.Bytes(), absent.Body.Bytes()) {
			t.Fatalf("снаружи глагол пробы отличим от несуществующего пути: %d %q против %d %q",
				got.Code, got.Body.String(), absent.Code, absent.Body.String())
		}
	})

	t.Run("undeclared/internal", func(t *testing.T) {
		t.Setenv(notifyAddrKnob, "notify-api.kacho.svc:9091")
		t.Setenv(notifyProbeAddrKnob, "")
		addrs := loadProbeAddrs(t)
		if _, ok := addrs["notifyProbeInternal"]; ok {
			t.Fatal("адрес пробы не объявлен, а ключ notifyProbeInternal в карте REST есть")
		}
		if rec := serveNotifyProbe(t, addrs, method, path, true); !routingRefusal(rec.Code) {
			t.Fatalf("адрес пробы не объявлен, а %s %s на внутреннем листенере маршрутизируется (%d)",
				method, path, rec.Code)
		}
	})
}
