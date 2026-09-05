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
	"github.com/PRO-Robotech/kacho/services/registry/internal/apps/kacho/config"
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

// TestBootPosture_Production — kacho-registry самоотчитывается о принятой
// posture. Публичный/internal флаги — это ДВА gRPC-листенера (:9090/:9091);
// data-plane docker-листенер в эти два поля не подмешивается.
func TestBootPosture_Production(t *testing.T) {
	cfg := config.Config{
		AuthMode:         "production-strict",
		DBSSLMode:        "require",
		AuthZIAMGRPCAddr: "kaname-internal:9091",
	}
	cfg.PublicServerMTLS.Enable = true
	cfg.InternalServerMTLS.Enable = true

	requireFields(t, captureBootPosture(t, bootPosture(cfg)), map[string]any{
		"msg":           observability.BootPostureMsg,
		"service":       "registry",
		"auth_mode":     "production-strict",
		"db_sslmode":    "require",
		"public_mtls":   true,
		"internal_mtls": "true",
		"authz_check":   true,
	})
}

// TestBootPosture_InsecureIsReportedHonestly — dev + plaintext-DB (пустой
// DBSSLMode деривится в `disable`) + отсутствие mTLS/authz обязаны быть видны.
func TestBootPosture_InsecureIsReportedHonestly(t *testing.T) {
	cfg := config.Config{AuthMode: "dev", DBSSLMode: ""}

	requireFields(t, captureBootPosture(t, bootPosture(cfg)), map[string]any{
		"service":       "registry",
		"auth_mode":     "dev",
		"db_sslmode":    "disable",
		"public_mtls":   false,
		"internal_mtls": "false",
		"authz_check":   false,
	})
}

// TestBootPosture_EmittedFromTheLiveBootPath — статический guard размещения:
// строка обязана эмититься ИЗ composition root'а реальным логгером, ПОСЛЕ
// secure-by-default boot-guard'а и ДО подъёма листенеров.
func TestBootPosture_EmittedFromTheLiveBootPath(t *testing.T) {
	src, err := os.ReadFile("serve.go")
	if err != nil {
		t.Fatalf("read composition root: %v", err)
	}
	root := string(src)

	call := strings.Index(root, "observability.LogBootPosture(logger, bootPosture(")
	if call < 0 {
		t.Fatal("composition root must emit the posture line: observability.LogBootPosture(logger, bootPosture(…))")
	}
	guard := strings.Index(root, "validateSecurityConfig(cfg)")
	if guard < 0 || call < guard {
		t.Fatal("posture line must be emitted AFTER validateSecurityConfig (a config the process accepted)")
	}
	// Слушатели поднимает носитель контура, поэтому якорь «до подъёма» — его
	// вызов, а не конструктор сервера: конструктора в корне больше нет вовсе
	// (см. trusted_forwarders_wiring_test.go), и якорь на нём означал бы «до
	// того, чего тут не бывает» — то есть утверждение без предмета.
	listener := strings.Index(root, "servicehost.Serve(")
	if listener < 0 {
		t.Fatal("composition root must raise its listeners through the contour host: servicehost.Serve(…)")
	}
	if call > listener {
		t.Fatal("posture line must be emitted BEFORE the gRPC listeners are built")
	}
}
