// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
	"github.com/PRO-Robotech/kacho/pkg/observability"
)

// captureBootPosture runs the posture through the real JSON logger and returns
// the parsed line: the contract locked here is the OBSERVED output (the
// production-posture gate parses it with jq), not the call.
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

func requireBootPostureFields(t *testing.T, line map[string]any, want map[string]any) {
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

// TestBootPosture_Production — the gateway self-reports its accepted posture.
// It owns no database, so db_sslmode is the literal "n/a".
func TestBootPosture_Production(t *testing.T) {
	cfg := config.Config{
		AuthNMode:          "production-strict",
		AuthZEnabled:       true,
		TLSListenAddr:      ":8443",
		TLSCertFile:        "/etc/certs/tls.crt",
		TLSKeyFile:         "/etc/certs/tls.key",
		HybridMTLSExternal: true,
	}

	requireBootPostureFields(t, captureBootPosture(t, bootPosture(cfg, true)), map[string]any{
		"msg":           observability.BootPostureMsg,
		"service":       "api-gateway",
		"auth_mode":     "production-strict",
		"db_sslmode":    "n/a",
		"public_mtls":   true,
		"internal_mtls": true,
		"authz_check":   true,
	})
}

// TestBootPosture_PublicMTLSNeedsTheTLSListener — a hybrid-mTLS wish without a
// TLS listener verifies no client certificate at all; the report must not claim
// public mTLS in that case (values-file wish vs wired listener).
func TestBootPosture_PublicMTLSNeedsTheTLSListener(t *testing.T) {
	cfg := config.Config{
		AuthNMode:          "production",
		AuthZEnabled:       true,
		HybridMTLSExternal: true, // no cert/key/addr → TLS listener never starts
	}

	requireBootPostureFields(t, captureBootPosture(t, bootPosture(cfg, true)), map[string]any{
		"public_mtls": false,
	})
}

// TestBootPosture_InsecureIsReportedHonestly — dev authN + pass-through authz +
// a plaintext internal listener must be visible as they are.
func TestBootPosture_InsecureIsReportedHonestly(t *testing.T) {
	cfg := config.Config{AuthNMode: "dev"}

	requireBootPostureFields(t, captureBootPosture(t, bootPosture(cfg, false)), map[string]any{
		"service":       "api-gateway",
		"auth_mode":     "dev",
		"db_sslmode":    "n/a",
		"public_mtls":   false,
		"internal_mtls": false,
		"authz_check":   false,
	})
}

// TestBootPosture_EmittedFromTheLiveBootPath — static placement guard: the line
// must be emitted from the composition root with the real logger, AFTER the boot
// guards, BEFORE any listener serves, and its internal_mtls must come from the
// RESOLVED listener security rather than the raw config flag.
func TestBootPosture_EmittedFromTheLiveBootPath(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read composition root: %v", err)
	}
	root := string(src)

	call := strings.Index(root, "observability.LogBootPosture(logger, bootPosture(cfg, internalSec.mtlsEnabled))")
	if call < 0 {
		t.Fatal("composition root must emit the posture line with the RESOLVED internal-listener security: " +
			"observability.LogBootPosture(logger, bootPosture(cfg, internalSec.mtlsEnabled))")
	}
	guard := strings.Index(root, "validateProductionInternalListener(")
	if guard < 0 || call < guard {
		t.Fatal("posture line must be emitted AFTER the production internal-listener boot guard")
	}
	listener := strings.Index(root, "startInternalGRPCListener(")
	if listener < 0 || call > listener {
		t.Fatal("posture line must be emitted BEFORE any listener starts serving")
	}
}
