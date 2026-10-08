// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware/anonmail"
)

func anonLimits(t *testing.T) config.AnonMailLimits {
	t.Helper()
	l, err := config.ResolveEdgeLimits(config.Config{
		AnonMailIPFreeLimit: "3", AnonMailIPPoWLimit: "5", AnonMailIPHardLimit: "8",
		AnonMailIPFreeWindow: "15m", AnonMailIPPoWWindow: "1h", AnonMailIPHardWindow: "2h",
		AnonMailPoWBitsBase: "10", AnonMailPoWBitsHigh: "14",
		AnonMailSubnetV4Len24PoWLimit: "20", AnonMailSubnetV4Len24HardLimit: "40",
		AnonMailSubnetV6Len56PoWLimit: "30", AnonMailSubnetV6Len56HardLimit: "60",
		AnonMailSubnetV6Len48PoWLimit: "50", AnonMailSubnetV6Len48HardLimit: "100",
		AnonMailSubnetPoWWindow: "1h", AnonMailSubnetHardWindow: "2h",
		AnonMailGlobalRatePerSecond: "5", AnonMailGlobalBurst: "10", TrustedHops: "1",
	})
	if err != nil {
		t.Fatal(err)
	}
	return l.AnonMail
}

// TestBuildAnonMailStore_CX2_44_PostgresNeedsTheBuiltIdempotencyStore — пул
// ограничителя строится функцией корня, аргументом которой служит УЖЕ
// построенное хранилище однократности (CX2-44 (1)): при виде postgres без него
// сборка корня отказывает — порядок выражен входом, а не соглашением. Вид
// memory хранилища однократности не требует.
func TestBuildAnonMailStore_CX2_44_PostgresNeedsTheBuiltIdempotencyStore(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	l := anonLimits(t)
	_, err := buildAnonMailStore(context.Background(), nil, config.Config{IdempotencyStoreKind: "postgres"}, l, log)
	if err == nil || !strings.Contains(err.Error(), "idempotency store") {
		t.Fatalf("postgres без построенного хранилища однократности: %v — ожидался отказ сборки корня", err)
	}
	s, err := buildAnonMailStore(context.Background(), nil, config.Config{IdempotencyStoreKind: "memory"}, l, log)
	if err != nil {
		t.Fatalf("memory: %v", err)
	}
	defer func() { _ = s.Close() }()
	if _, ok := s.(*anonmail.MemoryStore); !ok {
		t.Errorf("memory: хранилище %T", s)
	}
}
