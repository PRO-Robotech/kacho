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
	"github.com/PRO-Robotech/kacho/services/storage/internal/config"
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

// TestBootPosture_Production — эталонная строка storage теперь несёт и `service`,
// поэтому все восемь сервисов идентичны по форме.
func TestBootPosture_Production(t *testing.T) {
	cfg := config.Config{
		AuthMode:         "production",
		DBSSLMode:        "require",
		AuthZIAMGRPCAddr: "kaname-internal:9091",
	}
	cfg.PublicServerMTLS.Enable = true
	cfg.InternalServerMTLS.Enable = true

	requireFields(t, captureBootPosture(t, bootPosture(cfg)), map[string]any{
		"msg":           observability.BootPostureMsg,
		"service":       "storage",
		"auth_mode":     "production",
		"db_sslmode":    "require",
		"public_mtls":   true,
		"internal_mtls": "true",
		"authz_check":   true,
	})
}

// TestBootPosture_InsecureIsReportedHonestly — ровно тот класс, который гейт
// проспал: сервис в dev-режиме с незашифрованным DB-соединением.
func TestBootPosture_InsecureIsReportedHonestly(t *testing.T) {
	cfg := config.Config{AuthMode: "dev", DBSSLMode: "disable"}

	requireFields(t, captureBootPosture(t, bootPosture(cfg)), map[string]any{
		"service":       "storage",
		"auth_mode":     "dev",
		"db_sslmode":    "disable",
		"public_mtls":   false,
		"internal_mtls": "false",
		"authz_check":   false,
	})
}

// TestBootPosture_EmittedFromTheLiveBootPath — статический guard размещения:
// строка обязана эмититься ИЗ composition root'а реальным логгером, ПОСЛЕ
// fail-closed cfg.Validate() и ДО подъёма листенеров.
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
	guard := strings.Index(root, "cfg.Validate()")
	if guard < 0 || call < guard {
		t.Fatal("posture line must be emitted AFTER the fail-closed cfg.Validate() boot guard")
	}
	// Якорь подъёма слушателей ПЕРЕАНКЕРЁН: собственной сборки серверов в этом
	// корне больше нет — оба слушателя поднимает носитель, и точка их подъёма
	// ровно одна. Оставленный как был, страж искал бы исчезнувший конструктор и
	// падал бы на ВЕРНОМ коде.
	listener := strings.Index(root, "servicehost.Serve(")
	if listener < 0 {
		t.Fatal("composition root must hand both listeners to the carrier: servicehost.Serve(…)")
	}
	if call > listener {
		t.Fatal("posture line must be emitted BEFORE the carrier raises the listeners")
	}
	// И ПОСЛЕ приёма дескриптора: до него посадка ещё не прошла отказов старта,
	// поэтому строка отчитывалась бы о конфигурации, по которой процесс, может
	// быть, и не поднимется.
	accepted := strings.Index(root, "desc, err := describe(")
	if accepted < 0 || call < accepted {
		t.Fatal("posture line must be emitted AFTER the descriptor has been accepted by its constructor")
	}
}
