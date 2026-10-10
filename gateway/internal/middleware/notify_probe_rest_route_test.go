// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// notify_probe_rest_route_test.go — глагол стендовой пробы notify-probe
// доходит до каталога прав края REST-путём из контракта.
//
// Прослойка прав края переводит REST-запрос в FQN по таблице, порождённой из
// `google.api.http`; путь без записи в таблице до каталога не доходит, и
// запрос отвергается отказом по умолчанию. Метод без биндинга grpc-gateway
// служит по пути `POST /<FQN>`, которого в таблице нет, — так глагол пробы
// был недостижим администратору кластера через внутренний край (решение
// владельца 2026-10-08: Send зовётся его личностью через внутренний край).
// Близнец — `InternalNoticeService/Create`: тот же внутренний домен, тот же
// подход, путь из контракта.
package middleware

import (
	"testing"

	notifypb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/notify/v1"
)

func TestNotifyProbeSendResolvesToItsCatalogKey(t *testing.T) {
	r := NewRestRouter()
	catalog, err := LoadEmbeddedPermissionCatalog("")
	if err != nil {
		t.Fatalf("вшитый каталог прав: %v", err)
	}
	cases := []struct {
		name, method, path, fullMethod string
	}{
		{"близнец InternalNoticeService/Create", "POST", "/notify/v1/internal/notices",
			notifypb.InternalNoticeService_Create_FullMethodName},
		{"InternalNotifyProbeService/Send", "POST", "/notify/v1/internal/probeNotifications:send",
			notifypb.InternalNotifyProbeService_Send_FullMethodName},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			want := c.fullMethod[1:]
			got, ok := r.Resolve(c.method, c.path)
			if !ok || got != want {
				t.Fatalf("%s %s → %q (ok=%v), ждали %q: путь не доходит до каталога прав края",
					c.method, c.path, got, ok, want)
			}
			if _, ok := catalog.Lookup(want); !ok {
				t.Fatalf("FQN %q разрешился, но записи в вшитом каталоге прав нет", want)
			}
		})
	}
}
