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

// TestGeo_ADM1GEO12_PublicAdminPairsReachThePublicBackendOnBothListeners —
// приёмка ADM-1 geo, §Р5 и сценарий 12 на уровне края.
//
// Шесть пар `/geo/v1/{regions,zones}` (POST/PATCH/DELETE) обязаны быть
// смонтированы на ОБОИХ слушателях и вести в ПУБЛИЧНЫЙ бэкенд geo — не во
// внутренний. Утверждение строится по адресу бэкенда, который цитирует тело
// ответа при недозвоне: адрес подделать нечем, а код — можно. Внутренние восемь
// пар `/geo/v1/internal/…` снаружи по-прежнему 404 — это держит
// TestGeo_S5_InternalPathsRejectedOnExternal, и здесь не повторяется.
func TestGeo_ADM1GEO12_PublicAdminPairsReachThePublicBackendOnBothListeners(t *testing.T) {
	const publicLiteral, internalLiteral = "127.0.0.1:1", "127.0.0.1:2"
	addrs := geoMuxAddrs()
	addrs["geoInternal"] = internalLiteral
	h, err := NewMux(context.Background(), addrs, nil, nil, 30*time.Second)
	if err != nil {
		t.Fatalf("NewMux: %v", err)
	}

	// Положительный контроль предиката: заведомо внутренний путь изнутри цитирует
	// внутренний литерал — иначе «не дошёл до внутреннего» ничего не значило бы.
	if _, body := serveProbe(t, h, "POST", "/geo/v1/internal/regions", true); !strings.Contains(body, internalLiteral) {
		t.Fatalf("положительный контроль провален: POST /geo/v1/internal/regions изнутри не цитирует %q: %s",
			internalLiteral, body)
	}

	pairs := []struct{ method, path string }{
		{"POST", "/geo/v1/regions"},
		{"PATCH", "/geo/v1/regions/ru-central1"},
		{"DELETE", "/geo/v1/regions/ru-central1"},
		{"POST", "/geo/v1/zones"},
		{"PATCH", "/geo/v1/zones/ru-central1-a"},
		{"DELETE", "/geo/v1/zones/ru-central1-a"},
	}
	for _, tc := range pairs {
		for _, internal := range []bool{false, true} {
			side := "EXT"
			if internal {
				side = "INT"
			}
			t.Run(side+" "+tc.method+" "+tc.path, func(t *testing.T) {
				code, body := serveProbe(t, h, tc.method, tc.path, internal)
				if code == http.StatusNotFound || code == http.StatusNotImplemented {
					t.Fatalf("%s %s на слушателе %s: маршрута нет (%d) — публичный глагол каталога не смонтирован: %s",
						tc.method, tc.path, side, code, body)
				}
				if strings.Contains(body, internalLiteral) {
					t.Fatalf("%s %s на слушателе %s дошёл до ВНУТРЕННЕГО бэкенда geo: %s", tc.method, tc.path, side, body)
				}
				if !strings.Contains(body, publicLiteral) {
					t.Fatalf("%s %s на слушателе %s не дошёл до публичного бэкенда geo (код %d): %s",
						tc.method, tc.path, side, code, body)
				}
			})
		}
	}
}
