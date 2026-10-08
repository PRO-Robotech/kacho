// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// describe_test.go — проба объявления notify-api о себе: дескриптор формы
// «только внутренний слушатель» принимается конструктором носителя на годных
// ручках, а каждое поле, которое боевая посадка обязана нести, отвергается с
// его именем. Отказ конструктора рантаймовый: сборка его не видит, прогон
// носителя в пробах N2 — только на посадке dev.

import (
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/PRO-Robotech/corelib/authz"
	"github.com/PRO-Robotech/corelib/servicecontract"

	"github.com/PRO-Robotech/kacho/services/notify/cmd/notify-api/internal/config"
)

func describeCfg() config.Config {
	return config.Config{
		AuthMode:                  "production",
		DBSSLMode:                 "require",
		AuthzIAMGRPCAddr:          "kaname.kacho.svc:9091",
		InternalPort:              "9091",
		AuthzTrustDomain:          "kacho.cloud",
		AuthzTrustedForwarderSANs: []string{"spiffe://kacho.cloud/ns/kacho/sa/kacho-api-gateway"},
		AuthzCacheTTL:             5 * time.Second,
		AuthzCheckTimeout:         2 * time.Second,
		AuthzDenyBudgetPerSec:     100,
		HandlingBudget:            30 * time.Second,
	}
}

// verified — транспорт слушателя, который конструктор считает проверенным (mTLS).
func verified(t *testing.T) credentials.TransportCredentials {
	t.Helper()
	ca := newTestCA(t)
	return ca.serverCreds(t, "notify-api", apiSAN)
}

// verifiedPeer — проверенный транспорт ребра к службе доступа.
func verifiedPeer(t *testing.T) credentials.TransportCredentials {
	t.Helper()
	ca := newTestCA(t)
	return ca.clientTLS(t, "notify-api-client", apiSAN, "")
}

func describeWith(t *testing.T, cfg config.Config, internal credentials.TransportCredentials) error {
	t.Helper()
	mode, err := cfg.Mode()
	if err != nil {
		t.Fatal(err)
	}
	rt := apiRuntime{Metrics: prometheus.NewRegistry(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	_, err = describe(cfg, mode, internal, verifiedPeer(t), rt, func(func() authz.Metrics) {})
	return err
}

// TestDescribe_InternalOnlyFormIsAcceptedOnProductionKnobs — близнец: боевая
// посадка, проверенный транспорт, суженный круг — дескриптор принят, форма
// хоста internal-only.
func TestDescribe_InternalOnlyFormIsAcceptedOnProductionKnobs(t *testing.T) {
	t.Parallel()
	cfg := describeCfg()
	mode, _ := cfg.Mode()
	rt := apiRuntime{Metrics: prometheus.NewRegistry(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	d, err := describe(cfg, mode, verified(t), verifiedPeer(t), rt, func(func() authz.Metrics) {})
	if err != nil {
		t.Fatalf("годный дескриптор отвергнут: %v", err)
	}
	if d.HostForm() != servicecontract.HostInternalOnly {
		t.Fatalf("форма хоста %s, ожидалась internal-only", d.HostForm())
	}
}

// TestDescribe_ProductionRefusalNamesTheField — по одному изменённому факту
// против близнеца выше: незашифрованная база, открытый транспорт слушателя,
// открытый транспорт ребра к службе доступа, несуженный круг. Отказ называет
// ровно одно поле.
func TestDescribe_ProductionRefusalNamesTheField(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, field string
		edit        func(c *config.Config) credentials.TransportCredentials
	}{
		{"sslmode=disable", "DBSSLMode", func(c *config.Config) credentials.TransportCredentials {
			c.DBSSLMode = "disable"
			return verified(t)
		}},
		{"слушатель без TLS", "InternalCreds", func(*config.Config) credentials.TransportCredentials {
			return insecure.NewCredentials()
		}},
		{"пустой круг пересылающих", "Forwarders", func(c *config.Config) credentials.TransportCredentials {
			c.AuthzTrustedForwarderSANs = nil
			return verified(t)
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := describeCfg()
			creds := c.edit(&cfg)
			err := describeWith(t, cfg, creds)
			if err == nil || !strings.Contains(err.Error(), "(1 поле(й))") || !strings.Contains(err.Error(), c.field+":") {
				t.Fatalf("отказ %v, ожидался отказ ровно по полю %s", err, c.field)
			}
		})
	}
}

// TestDescribe_OpenEdgeToTheAccessServiceIsRefused — тот же близнец с открытым
// транспортом ребра решения о доступе: отказ по полю CheckEdge.
func TestDescribe_OpenEdgeToTheAccessServiceIsRefused(t *testing.T) {
	t.Parallel()
	cfg := describeCfg()
	mode, _ := cfg.Mode()
	rt := apiRuntime{Metrics: prometheus.NewRegistry(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	_, err := describe(cfg, mode, verified(t), insecure.NewCredentials(), rt, func(func() authz.Metrics) {})
	if err == nil || !strings.Contains(err.Error(), "(1 поле(й))") || !strings.Contains(err.Error(), "CheckEdge:") {
		t.Fatalf("отказ %v, ожидался отказ ровно по полю CheckEdge", err)
	}
}
