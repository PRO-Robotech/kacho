// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package observability_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/observability"
)

// TestLogBootPosture_ContractShape локает ХАРД-КОНТРАКТ boot-строки: сообщение и
// имена полей парсит production-posture гейт (jq), поэтому любое переименование
// ключа/сообщения ломает гейт молча. Тест держит форму на уровне НАБЛЮДАЕМОГО
// JSON-вывода, а не на уровне вызова.
func TestLogBootPosture_ContractShape(t *testing.T) {
	var buf bytes.Buffer
	observability.LogBootPosture(observability.NewSlogger(&buf), observability.BootPosture{
		Service:           "vpc",
		AuthMode:          "production",
		DBSSLMode:         "require",
		PublicMTLS:        true,
		InternalMTLS:      true,
		AuthZCheck:        true,
		TrustedForwarders: true,
	})

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("boot posture line is not one JSON object: %v (raw=%q)", err, buf.String())
	}
	want := map[string]any{
		"msg":                "boot security posture",
		"service":            "vpc",
		"auth_mode":          "production",
		"db_sslmode":         "require",
		"public_mtls":        true,
		"internal_mtls":      true,
		"authz_check":        true,
		"trusted_forwarders": true,
	}
	for k, v := range want {
		got, ok := line[k]
		if !ok {
			t.Fatalf("field %q missing from boot posture line: %v", k, line)
		}
		if got != v {
			t.Fatalf("field %q = %v (%T), want %v (%T)", k, got, got, v, v)
		}
	}
}

// TestLogBootPosture_FalseFlagsAreEmitted — false обязан ПРИСУТСТВОВАТЬ в строке
// (а не отсутствовать): гейт отличает «mTLS выключен» от «поле не эмитится».
func TestLogBootPosture_FalseFlagsAreEmitted(t *testing.T) {
	var buf bytes.Buffer
	observability.LogBootPosture(observability.NewSlogger(&buf), observability.BootPosture{
		Service:   "api-gateway",
		AuthMode:  "dev",
		DBSSLMode: observability.DBSSLModeNotApplicable,
	})

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("unmarshal: %v (raw=%q)", err, buf.String())
	}
	for _, k := range []string{"public_mtls", "internal_mtls", "authz_check", "trusted_forwarders"} {
		v, ok := line[k]
		if !ok {
			t.Fatalf("field %q must be emitted even when false: %v", k, line)
		}
		if v != false {
			t.Fatalf("field %q = %v, want false", k, v)
		}
	}
	if line["db_sslmode"] != "n/a" {
		t.Fatalf("db_sslmode = %v, want the literal \"n/a\" for a database-less service", line["db_sslmode"])
	}
}

// TestBootPostureMsg_IsTheParsedContract — сообщение экспортировано константой,
// чтобы сервис-тесты и гейт ссылались на один литерал.
func TestBootPostureMsg_IsTheParsedContract(t *testing.T) {
	if observability.BootPostureMsg != "boot security posture" {
		t.Fatalf("BootPostureMsg = %q — контракт гейта требует %q",
			observability.BootPostureMsg, "boot security posture")
	}
}
