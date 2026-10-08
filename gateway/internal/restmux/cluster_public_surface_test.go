// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package restmux

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

// clusterPublicSurface — четыре пары (метод, путь) публичного близнеца
// ClusterService (kaname#661): ровно те, что объявлены `google.api.http` в
// `cluster_service.proto`.
var clusterPublicSurface = []struct{ method, path string }{
	{"GET", "/iam/v1/cluster"},                                // Get
	{"GET", "/iam/v1/cluster/admins"},                         // ListAdmins
	{"POST", "/iam/v1/cluster/admins"},                        // GrantAdmin
	{"DELETE", "/iam/v1/cluster/admins/usr00000000000000001"}, // RevokeAdmin
}

// clusterInternalSurface — те же четыре глагола внутреннего близнеца.
var clusterInternalSurface = []struct{ method, path string }{
	{"GET", "/iam/v1/internal/cluster"},
	{"GET", "/iam/v1/internal/cluster/admins"},
	{"POST", "/iam/v1/internal/cluster/admins"},
	{"DELETE", "/iam/v1/internal/cluster/admins/usr00000000000000001"},
}

// TestExternalListener_ClusterPublicTwinReachesPublicIAMBackendOnly — kacho#3093.
//
// Утверждение строится по АДРЕСУ бэкенда, который цитирует тело ответа
// grpc-gateway при недозвоне (тот же приём, что у административной поверхности
// пула адресов): публичный бэкенд iam — 127.0.0.1:1, внутренний — 127.0.0.1:2.
//
//	(1) снаружи каждый публичный путь доходит до ПУБЛИЧНОГО бэкенда iam и
//	    никогда — до внутреннего;
//	(2) внутренний близнец снаружи не доходит до внутреннего бэкенда (запрет #6);
//	(3) изнутри внутренний близнец по-прежнему доходит до внутреннего бэкенда —
//	    публикация близнеца внутренний глагол не сняла и не перехватила.
func TestExternalListener_ClusterPublicTwinReachesPublicIAMBackendOnly(t *testing.T) {
	addrs, adminLiteral := splitAddrs(t)
	const publicLiteral = "127.0.0.1:1"
	if addrs["iam"] == "" || addrs["iamInternal"] == "" {
		t.Fatalf("в карте адресов нет iam/iamInternal — у пробы нет предмета: %v", addrs)
	}
	h, err := NewMux(context.Background(), addrs, nil, nil, 30*time.Second)
	if err != nil {
		t.Fatalf("NewMux: %v", err)
	}

	// Положительный контроль предиката: заведомо публичный путь iam снаружи
	// цитирует публичный литерал.
	if _, body := serveProbe(t, h, "GET", "/iam/v1/accounts", false); !strings.Contains(body, publicLiteral) {
		t.Fatalf("положительный контроль провален: GET /iam/v1/accounts не цитирует %q — предикат пробы негоден, тело: %s",
			publicLiteral, body)
	}

	for _, tc := range clusterPublicSurface {
		t.Run("EXT "+tc.method+" "+tc.path, func(t *testing.T) {
			code, body := serveProbe(t, h, tc.method, tc.path, false)
			if code == http.StatusNotFound && !strings.Contains(body, publicLiteral) {
				t.Fatalf("%s %s на ВНЕШНЕМ слушателе: 404 без бэкенда — публичный близнец ClusterService не зарегистрирован: %s",
					tc.method, tc.path, body)
			}
			if strings.Contains(body, adminLiteral) {
				t.Fatalf("%s %s на ВНЕШНЕМ слушателе дошёл до ВНУТРЕННЕГО бэкенда iam (запрет #6): %s",
					tc.method, tc.path, body)
			}
			if !strings.Contains(body, publicLiteral) {
				t.Fatalf("%s %s на ВНЕШНЕМ слушателе не дошёл до ПУБЛИЧНОГО бэкенда iam (код %d): %s",
					tc.method, tc.path, code, body)
			}
		})
	}

	for _, tc := range clusterInternalSurface {
		t.Run("EXT "+tc.method+" "+tc.path, func(t *testing.T) {
			_, body := serveProbe(t, h, tc.method, tc.path, false)
			if strings.Contains(body, adminLiteral) {
				t.Fatalf("внутренний близнец %s %s на ВНЕШНЕМ слушателе дошёл до ВНУТРЕННЕГО бэкенда (запрет #6): %s",
					tc.method, tc.path, body)
			}
		})
		t.Run("INT "+tc.method+" "+tc.path, func(t *testing.T) {
			_, body := serveProbe(t, h, tc.method, tc.path, true)
			if !strings.Contains(body, adminLiteral) {
				t.Fatalf("внутренний близнец %s %s на ВНУТРЕННЕМ слушателе не дошёл до ВНУТРЕННЕГО бэкенда: %s",
					tc.method, tc.path, body)
			}
		})
	}
}
