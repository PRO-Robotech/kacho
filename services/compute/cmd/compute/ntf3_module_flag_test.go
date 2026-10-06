// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// ntf3_module_flag_test.go — полоса RED S1-A4 issue-2918 (NTF-3, Н3-Ф2) для
// compute: флаг модуля KACHO_COMPUTE_NOTIFICATIONS_ENABLED.
//
// Сценарии приёмки NTF-3 (отпечаток ac1f9fc9…), замысел З11, З4 (а), CX3M-02 (а):
//   - NTF3-64 — ручка не задана либо не разбирается → отказ старта, текст
//     называет ручку и допустимые значения true | false; близнецы true, false;
//   - NTF3-65 / NTF3-67 (словарь видов) — ключ журнала `notification` (вид на
//     проводе `notification_feed`) объявлен в Mapping.Kinds ровно при включённом
//     флаге; прочие виды от флага не зависят;
//   - NTF3-67 (самоотчёт посадки) — строка самоотчёта называет значение флага;
//   - УК3-61 — каждый держатель journaltx.Options (конструктор писателя журнала
//     модуля) принимает их позиционно и отвергает нулевые при сборке; близнец —
//     построенные NewOptions собраны;
//   - И6 — Options строит корень один раз из одного чтения ручки.
//
// Общая часть — ntf3_module_flag_probe_test.go.

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/PRO-Robotech/kacho/services/compute/internal/config"
	computerepo "github.com/PRO-Robotech/kacho/services/compute/internal/repo"
	"github.com/PRO-Robotech/kacho/services/compute/internal/subscriptionjournal"
)

const (
	ntf3Module = "compute"
	ntf3Knob   = "KACHO_COMPUTE_NOTIFICATIONS_ENABLED"
	// ntf3ModulePkg — путь импорта корня модуля: по нему перепись держателей
	// сопоставляет тип, отданный конструктором, с каталогом дерева.
	ntf3ModulePkg = "github.com/PRO-Robotech/kacho/services/compute"
	// ntf3ModuleDir — корень модуля относительно пакета пробы.
	ntf3ModuleDir = "../.."
)

// ntf3Holders — конструкторы держателей journaltx.Options модуля; перечень
// сверяется с переписью дерева (ntf3RequireHoldersCoverTheTree).
func ntf3Holders() []ntf3Holder {
	return []ntf3Holder{
		{"repo.NewGuestAccessKeyRepo", computerepo.NewGuestAccessKeyRepo},
		{"repo.NewInstanceRepo", computerepo.NewInstanceRepo},
		{"repo.NewPlacementGroupRepo", computerepo.NewPlacementGroupRepo},
	}
}

// ntf3Load — загрузка конфигурации ТЕМ ЖЕ путём, что на старте (config.Load, затем cfg.Validate — main.go).
// value == nil — ручка не задана вовсе.
//
// Посадка dev: отказ по ручке обязан стоять на ЛЮБОЙ посадке (умолчания нет),
// а боевая требует файлов сертификатов, к ручке отношения не имеющих.
func ntf3Load(t *testing.T, value *string) (config.Config, error) {
	t.Helper()
	for k, v := range map[string]string{
		"KACHO_COMPUTE_DB_PASSWORD":                  "secret",
		"KACHO_COMPUTE_AUTHZ_IAM_GRPC_ADDR":          "kaname-internal:9091",
		"KACHO_COMPUTE_AUTHZ_TRUSTED_FORWARDER_SANS": gatewaySAN,
		"KACHO_COMPUTE_AUTHZ_TRUST_DOMAIN":           "kacho.cloud",
		"KACHO_COMPUTE_AUTH_MODE":                    "dev",
		"KACHO_COMPUTE_QUOTA_AUTHORITY":              "not-deployed",
	} {
		t.Setenv(k, v)
	}
	ntf3SetKnob(t, value)
	c, err := config.Load()
	if err != nil {
		return c, err
	}
	return c, c.Validate()
}

// TestNTF364_ComputeKnobUnsetOrUnparsableRefusesStart — NTF3-64 (compute).
func TestNTF364_ComputeKnobUnsetOrUnparsableRefusesStart(t *testing.T) {
	ntf3RequireKnobRefusal(t, func(t *testing.T, v *string) error { _, err := ntf3Load(t, v); return err })
}

// TestNTF3_65_67_ComputeJournalKindsFollowTheFlag — NTF3-65 / NTF3-67, словарь видов.
func TestNTF3_65_67_ComputeJournalKindsFollowTheFlag(t *testing.T) {
	ntf3RequireKindsFollowTheFlag(t, subscriptionjournal.Journal)
}

// TestNTF367_ComputeBootPostureReportsTheFlag — NTF3-67: самоотчёт посадки
// называет значение флага; близнец — то же при false.
func TestNTF367_ComputeBootPostureReportsTheFlag(t *testing.T) {
	ntf3RequireBootPostureReportsTheFlag(t, func(t *testing.T, value string) map[string]any {
		cfg, err := ntf3Load(t, &value)
		if err != nil {
			t.Fatalf("ФИКСТУРА: конфигурация с %s=%s не загрузилась: %v", ntf3Knob, value, err)
		}
		return captureBootPosture(t, bootPosture(cfg))
	})
}

// TestUK361_ComputeJournalWritersTakeOptionsAndRefuseZero — УК3-61 (compute).
func TestUK361_ComputeJournalWritersTakeOptionsAndRefuseZero(t *testing.T) {
	ntf3RequireHoldersRefuseZeroOptions(t, ntf3Holders())
}

// TestNTF3_ComputeOptionsAreBuiltOnceByTheRoot — И6 (compute).
func TestNTF3_ComputeOptionsAreBuiltOnceByTheRoot(t *testing.T) {
	ntf3RequireOptionsBuiltOnce(t, ntf3ModuleDir)
}

// TestNTF367_ComputeRootRegistersTheGauge — NTF3-65 / NTF3-67: серию
// kacho_notifications_enabled{module="compute"} ставит корень функцией фундамента.
func TestNTF367_ComputeRootRegistersTheGauge(t *testing.T) {
	ntf3RequireRootRegistersTheGauge(t, ntf3ModuleDir)
}

// TestNTF367_ComputeGaugeFollowsTheFlag — NTF3-65 / NTF3-67: серия флага на
// чистом реестре — 1 при true, 0 при false; конфигурация — тем же путём, что
// на старте.
func TestNTF367_ComputeGaugeFollowsTheFlag(t *testing.T) {
	ntf3RequireGaugeFollowsTheFlag(t, func(t *testing.T, value string, reg prometheus.Registerer) error {
		cfg, err := ntf3Load(t, &value)
		if err != nil {
			t.Fatalf("ФИКСТУРА: конфигурация с %s=%s не загрузилась: %v", ntf3Knob, value, err)
		}
		return registerNotificationsGauge(reg, cfg.Notifications)
	})
}
