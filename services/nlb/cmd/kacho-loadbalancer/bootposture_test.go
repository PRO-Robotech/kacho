// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/observability"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/apps/kacho/config"
)

// captureBootPosture прогоняет posture через реальный JSON-логгер и возвращает
// разобранную строку: локаем НАБЛЮДАЕМЫЙ вывод (его парсит production-posture
// гейт), а не вызов.
func captureBootPosture(t *testing.T, p observability.BootPosture) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	observability.LogBootPosture(observability.NewSlogger(&buf), p)
	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("boot posture line is not one JSON object: %v (raw=%q)", err, buf.String())
	}
	return line
}

func requireFields(t *testing.T, line map[string]any, want map[string]any) {
	t.Helper()
	for k, v := range want {
		got, ok := line[k]
		if !ok {
			t.Fatalf("field %q missing: %v", k, line)
		}
		if got != v {
			t.Fatalf("field %q = %v (%T), want %v (%T)", k, got, got, v, v)
		}
	}
}

// TestBootPosture_Production — kacho-nlb самоотчитывается о принятой posture.
// sslmode у nlb живёт ТОЛЬКО внутри repository.postgres.url (отдельного поля нет)
// — отчёт обязан достать его оттуда.
func TestBootPosture_Production(t *testing.T) {
	var cfg config.Config
	cfg.ModeRaw = "production"
	cfg.Repository.Postgres.URL = "postgres://u:p@pg-nlb:5432/kacho_nlb?sslmode=require&search_path=kacho_nlb,public"
	cfg.MTLS.Server.Enable = true

	requireFields(t, captureBootPosture(t, bootPosture(&cfg, true)), map[string]any{
		"msg":           observability.BootPostureMsg,
		"service":       "nlb",
		"auth_mode":     "production",
		"db_sslmode":    "require",
		"public_mtls":   true,
		"internal_mtls": true,
		"authz_check":   true,
	})
}

// TestBootPosture_InsecureIsReportedHonestly — dev + DSN без sslmode (libpq
// деградирует в plaintext-fallback `prefer`) + листенеры без mTLS + не поднятый
// Check-клиент обязаны быть видны как есть.
func TestBootPosture_InsecureIsReportedHonestly(t *testing.T) {
	var cfg config.Config
	cfg.ModeRaw = "dev"
	cfg.Repository.Postgres.URL = "postgres://u:p@pg-nlb:5432/kacho_nlb"

	requireFields(t, captureBootPosture(t, bootPosture(&cfg, false)), map[string]any{
		"service":       "nlb",
		"auth_mode":     "dev",
		"db_sslmode":    "prefer",
		"public_mtls":   false,
		"internal_mtls": false,
		"authz_check":   false,
	})
}

// TestBootPosture_EmittedFromTheLiveBootPath — статический guard размещения:
// строка обязана эмититься ИЗ composition root'а реальным логгером, ПОСЛЕ
// config.Load→Validate и построения server-creds, но ДО подъёма листенеров, и
// брать authz_check из УЖЕ поднятой проводки (peers.Check), а не из конфига.
func TestBootPosture_EmittedFromTheLiveBootPath(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read composition root: %v", err)
	}
	root := string(src)

	call := strings.Index(root, "observability.LogBootPosture(logger, bootPosture(cfg, peers.Check != nil))")
	if call < 0 {
		t.Fatal("composition root must emit the posture line with the WIRED Check client: " +
			"observability.LogBootPosture(logger, bootPosture(cfg, peers.Check != nil))")
	}
	guard := strings.Index(root, "grpcsrv.TLSServerCreds(cfg.MTLS.Server)")
	if guard < 0 || call < guard {
		t.Fatal("posture line must be emitted AFTER the listener server-creds are resolved")
	}
	listener := strings.Index(root, "publicSrv := grpcsrv.NewServer(")
	if listener < 0 || call > listener {
		t.Fatal("posture line must be emitted BEFORE the gRPC listeners are built")
	}
}
