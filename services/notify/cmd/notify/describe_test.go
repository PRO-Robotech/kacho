// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/observability"
	"github.com/PRO-Robotech/corelib/servicecontract"

	"github.com/PRO-Robotech/kacho/services/notify/internal/config"
)

func fixtureConfig(t *testing.T, edit func(map[string]string)) config.Config {
	t.Helper()
	env := fixtureEnv(t)
	if edit != nil {
		edit(env)
	}
	for _, k := range config.Knobs() {
		if v, ok := env[k.Env]; ok {
			t.Setenv(k.Env, v)
		} else {
			unsetEnv(t, k.Env)
		}
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("загрузка фикстуры: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("фикстура не проходит стража: %v", err)
	}
	return cfg
}

// Дескриптор notify — форма хоста без gRPC-слушателей (З15, Д17): сервисов
// процесс не обслуживает, самоотчёт берёт форму из ПРИНЯТОГО дескриптора.
func TestDescriptorIsHostNoGRPC(t *testing.T) {
	cfg := fixtureConfig(t, nil)
	d, err := describe(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("дескриптор фикстуры отвергнут: %v", err)
	}
	if d.HostForm() != servicecontract.HostNoGRPC || !d.NoServedServices() {
		t.Fatalf("форма хоста %s, NoServedServices=%v; ожидалась no-grpc без сервисов", d.HostForm(), d.NoServedServices())
	}
	p := bootPosture(cfg, d)
	if !p.NoServedServices || p.ListenerForm != observability.ListenerFormPair {
		t.Fatalf("самоотчёт называет форму %v, NoServedServices=%v; дескриптор — %s",
			p.ListenerForm, p.NoServedServices, d.HostForm())
	}
	if got := p.Notifications.String(); got != observability.NotificationsNotApplicable {
		t.Fatalf("самоотчёт notify называет флаг ленты %q; у notify ленты источника нет — %q",
			got, observability.NotificationsNotApplicable)
	}
	if p.DBSSLMode != "require" {
		t.Fatalf("самоотчёт называет sslmode %q, фикстура — require", p.DBSSLMode)
	}
}

// NTF1-G15, ось sslmode: её судит общий дескриптор, отказ называет ось.
func TestNTF1G15DBSSLModeAxisRefusesStart(t *testing.T) {
	cfg := fixtureConfig(t, func(e map[string]string) { e["KACHO_NOTIFY_DB_SSLMODE"] = "disable" })
	_, err := describe(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err == nil || !strings.Contains(err.Error(), "DBSSLMode") || !strings.Contains(err.Error(), `sslmode="disable"`) {
		t.Fatalf("sslmode=disable на боевой посадке не отвергнут с именем оси: %v", err)
	}
	ok := fixtureConfig(t, func(e map[string]string) { e["KACHO_NOTIFY_DB_SSLMODE"] = "verify-full" })
	if _, err := describe(ok, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatalf("близнец verify-full отвергнут: %v", err)
	}
}

func unsetEnv(t *testing.T, k string) {
	t.Helper()
	t.Setenv(k, "")
	if err := os.Unsetenv(k); err != nil {
		t.Fatalf("снять %s: %v", k, err)
	}
}
