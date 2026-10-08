// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestBackendAddrs_NotifyFollowsTheDeclaredAddress — у адреса notify умолчания
// нет (NTF-4 Р20, NTF-5 З18 п.1): незаданный адрес — ключа нет вовсе, а не ключ
// с подставленным адресом; заданный — ровно один ключ notifyInternal, второго
// ключа адреса notify нет.
func TestBackendAddrs_NotifyFollowsTheDeclaredAddress(t *testing.T) {
	t.Run("undeclared", func(t *testing.T) {
		t.Setenv("KACHO_API_GATEWAY_NOTIFY_INTERNAL_GRPC", "")
		cfg, err := Load()
		require.NoError(t, err)
		addrs := cfg.BackendAddrs()
		_, ok := addrs["notifyInternal"]
		require.False(t, ok, "адрес notify не объявлен, а ключ notifyInternal в карте есть")
		_, ok = addrs["notify"]
		require.False(t, ok)
	})
	t.Run("declared", func(t *testing.T) {
		t.Setenv("KACHO_API_GATEWAY_NOTIFY_INTERNAL_GRPC", "notify-api.kacho.svc:9091")
		cfg, err := Load()
		require.NoError(t, err)
		addrs := cfg.BackendAddrs()
		require.Equal(t, "notify-api.kacho.svc:9091", addrs["notifyInternal"])
		_, ok := addrs["notify"]
		require.False(t, ok, "второй ключ адреса notify — у notify одно поле адреса")
	})
}

// TestNotifyMTLSEdge — ребро notify разрешается и, включённое, берёт имя сервера
// из адреса либо из своей ручки.
func TestNotifyMTLSEdge(t *testing.T) {
	t.Setenv("KACHO_API_GATEWAY_MTLS_CLIENT_CERT_FILE", "/etc/api-gateway/mtls/tls.crt")
	t.Setenv("KACHO_API_GATEWAY_MTLS_CLIENT_KEY_FILE", "/etc/api-gateway/mtls/tls.key")
	t.Setenv("KACHO_API_GATEWAY_MTLS_CA_FILE", "/etc/api-gateway/mtls/ca.crt")
	t.Setenv("KACHO_API_GATEWAY_MTLS_NOTIFY_ENABLE", "true")
	cfg, err := Load()
	require.NoError(t, err)

	tc, err := cfg.EdgeTLSClient("notify", "notify-api.kacho.svc:9091")
	require.NoError(t, err)
	require.True(t, tc.Enable)
	require.Equal(t, "notify-api.kacho.svc", tc.ServerName)

	t.Setenv("KACHO_API_GATEWAY_MTLS_NOTIFY_SERVER_NAME", "notify-api.kacho.svc.cluster.local")
	cfg, err = Load()
	require.NoError(t, err)
	tc, err = cfg.EdgeTLSClient("notify", "notify-api.kacho.svc:9091")
	require.NoError(t, err)
	require.Equal(t, "notify-api.kacho.svc.cluster.local", tc.ServerName)
}

// TestRESTBackendAddrs_NotifyProbeIsAnInternalRESTOnlyAddress — адрес пробы
// notify-probe (решение владельца 2026-10-08 (1)) — ключ ТОЛЬКО карты внутреннего
// REST: в карте gRPC-маршрутизатора его нет при любом объявлении, потому что
// служба пробы — Internal*, и наружу её не ведёт ни одна поверхность (запрет #6).
//
// Обе стороны оси на одном предмете: отличается только объявление адреса.
// Близнец — карта REST несёт ВСЕ ключи карты маршрутизатора: иначе
// «ключ пробы есть» могло бы означать, что это вообще другая карта.
func TestRESTBackendAddrs_NotifyProbeIsAnInternalRESTOnlyAddress(t *testing.T) {
	const knob = "KACHO_API_GATEWAY_NOTIFY_PROBE_INTERNAL_GRPC"
	const addr = "kacho-notify-probe.kacho.svc:9091"
	key := InternalBackendKey("notifyProbe")

	t.Run("undeclared", func(t *testing.T) {
		t.Setenv(knob, "")
		cfg, err := Load()
		require.NoError(t, err)
		_, ok := cfg.RESTBackendAddrs()[key]
		require.False(t, ok, "адрес пробы не объявлен, а ключ %q в карте REST есть", key)
		_, ok = cfg.BackendAddrs()[key]
		require.False(t, ok)
	})
	t.Run("declared", func(t *testing.T) {
		t.Setenv(knob, addr)
		cfg, err := Load()
		require.NoError(t, err)
		rest := cfg.RESTBackendAddrs()
		require.Equal(t, addr, rest[key])
		_, ok := cfg.BackendAddrs()[key]
		require.False(t, ok, "адрес пробы попал в карту gRPC-маршрутизатора — Internal*-служба "+
			"получила бы соединение на поверхности, которая наружу ведёт")
		for k, v := range cfg.BackendAddrs() {
			require.Equal(t, v, rest[k], "карта REST потеряла ключ маршрутизатора %q", k)
		}
		require.Len(t, rest, len(cfg.BackendAddrs())+1,
			"в карте REST сверх карты маршрутизатора есть что-то, кроме адреса пробы")
	})
}

// TestNotifyProbeMTLSEdge — у пробы СВОЁ ребро mTLS: другая служба, другое
// удостоверение сервера, свой флаг. Включённое ребро берёт имя сервера из
// адреса либо из своей ручки; ребро notify-api при этом не включается.
func TestNotifyProbeMTLSEdge(t *testing.T) {
	t.Setenv("KACHO_API_GATEWAY_MTLS_CLIENT_CERT_FILE", "/etc/api-gateway/mtls/tls.crt")
	t.Setenv("KACHO_API_GATEWAY_MTLS_CLIENT_KEY_FILE", "/etc/api-gateway/mtls/tls.key")
	t.Setenv("KACHO_API_GATEWAY_MTLS_CA_FILE", "/etc/api-gateway/mtls/ca.crt")
	t.Setenv("KACHO_API_GATEWAY_MTLS_NOTIFY_ENABLE", "false")
	t.Setenv("KACHO_API_GATEWAY_MTLS_NOTIFY_PROBE_ENABLE", "true")
	cfg, err := Load()
	require.NoError(t, err)

	tc, err := cfg.EdgeTLSClient("notifyProbe", "kacho-notify-probe.kacho.svc:9091")
	require.NoError(t, err)
	require.True(t, tc.Enable)
	require.Equal(t, "kacho-notify-probe.kacho.svc", tc.ServerName)

	// Близнец: ребро notify-api остаётся своим — выключенным.
	tn, err := cfg.EdgeTLSClient("notify", "notify-api.kacho.svc:9091")
	require.NoError(t, err)
	require.False(t, tn.Enable, "флаг ребра пробы включил ребро notify-api")

	t.Setenv("KACHO_API_GATEWAY_MTLS_NOTIFY_PROBE_SERVER_NAME", "kacho-notify-probe.kacho.svc.cluster.local")
	cfg, err = Load()
	require.NoError(t, err)
	tc, err = cfg.EdgeTLSClient("notifyProbe", "kacho-notify-probe.kacho.svc:9091")
	require.NoError(t, err)
	require.Equal(t, "kacho-notify-probe.kacho.svc.cluster.local", tc.ServerName)
}
