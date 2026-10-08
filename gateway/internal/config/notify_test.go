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
