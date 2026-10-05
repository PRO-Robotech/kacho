// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// ntf3_module_flag_test.go — полоса RED S1-A4 issue-2918 (NTF-3, Н3-Ф2) для
// vpc: флаг модуля KACHO_VPC_NOTIFICATIONS_ENABLED.
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

	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/config"
	vpcpg "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/pg"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/subscriptionjournal"
)

const (
	ntf3Module = "vpc"
	ntf3Knob   = "KACHO_VPC_NOTIFICATIONS_ENABLED"
	// ntf3ModulePkg — путь импорта корня модуля: по нему перепись держателей
	// сопоставляет тип, отданный конструктором, с каталогом дерева.
	ntf3ModulePkg = "github.com/PRO-Robotech/kacho/services/vpc"
	// ntf3ModuleDir — корень модуля относительно пакета пробы.
	ntf3ModuleDir = "../.."
)

// ntf3Holders — конструкторы держателей journaltx.Options модуля; перечень
// сверяется с переписью дерева (ntf3RequireHoldersCoverTheTree).
func ntf3Holders() []ntf3Holder {
	return []ntf3Holder{
		{"pg.New", vpcpg.New},
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
		"KACHO_VPC_CONFIG_PATH":                   "",
		"KACHO_VPC_AUTH_MODE":                     "dev",
		"KACHO_VPC_REPOSITORY__POSTGRES__URL":     "postgres://vpc@db-that-is-never-dialled:5432/kacho_vpc",
		"KACHO_VPC_AUTHZ__IAM_ENDPOINT":           "kaname-internal:9091",
		"KACHO_VPC_QUOTA__AUTHORITY":              "not-deployed",
		"KACHO_VPC_AUTHZ__TRUSTED_FORWARDER_SANS": "spiffe://kacho.cloud/ns/kacho/sa/kacho-api-gateway",
		"KACHO_VPC_AUTHZ__TRUST_DOMAIN":           "kacho.cloud",
	} {
		t.Setenv(k, v)
	}
	ntf3SetKnob(t, value)
	c, err := config.Load("")
	if err != nil {
		return c, err
	}
	return c, c.Validate()
}

// TestNTF364_VPCKnobUnsetOrUnparsableRefusesStart — NTF3-64 (vpc).
func TestNTF364_VPCKnobUnsetOrUnparsableRefusesStart(t *testing.T) {
	ntf3RequireKnobRefusal(t, func(t *testing.T, v *string) error { _, err := ntf3Load(t, v); return err })
}

// TestNTF3_65_67_VPCJournalKindsFollowTheFlag — NTF3-65 / NTF3-67, словарь видов.
func TestNTF3_65_67_VPCJournalKindsFollowTheFlag(t *testing.T) {
	ntf3RequireKindsFollowTheFlag(t, subscriptionjournal.Journal)
}

// TestNTF367_VPCBootPostureReportsTheFlag — NTF3-67: самоотчёт посадки
// называет значение флага; близнец — то же при false.
func TestNTF367_VPCBootPostureReportsTheFlag(t *testing.T) {
	ntf3RequireBootPostureReportsTheFlag(t, func(t *testing.T, value string) map[string]any {
		cfg, err := ntf3Load(t, &value)
		if err != nil {
			t.Fatalf("ФИКСТУРА: конфигурация с %s=%s не загрузилась: %v", ntf3Knob, value, err)
		}
		return captureBootPosture(t, bootPosture(cfg, config.MTLSConfig{}))
	})
}

// TestUK361_VPCJournalWritersTakeOptionsAndRefuseZero — УК3-61 (vpc).
func TestUK361_VPCJournalWritersTakeOptionsAndRefuseZero(t *testing.T) {
	ntf3RequireHoldersRefuseZeroOptions(t, ntf3Holders())
}

// TestNTF3_VPCOptionsAreBuiltOnceByTheRoot — И6 (vpc).
func TestNTF3_VPCOptionsAreBuiltOnceByTheRoot(t *testing.T) {
	ntf3RequireOptionsBuiltOnce(t, ntf3ModuleDir)
}

// TestNTF367_VPCRootRegistersTheGauge — NTF3-65 / NTF3-67: серию
// kacho_notifications_enabled{module="vpc"} ставит корень функцией фундамента.
func TestNTF367_VPCRootRegistersTheGauge(t *testing.T) {
	ntf3RequireRootRegistersTheGauge(t, ntf3ModuleDir)
}

// TestNTF367_VPCGaugeFollowsTheFlag — NTF3-65 / NTF3-67: серия флага на
// чистом реестре — 1 при true, 0 при false; конфигурация — тем же путём, что
// на старте.
func TestNTF367_VPCGaugeFollowsTheFlag(t *testing.T) {
	ntf3RequireGaugeFollowsTheFlag(t, func(t *testing.T, value string, reg prometheus.Registerer) error {
		cfg, err := ntf3Load(t, &value)
		if err != nil {
			t.Fatalf("ФИКСТУРА: конфигурация с %s=%s не загрузилась: %v", ntf3Knob, value, err)
		}
		return registerNotificationsGauge(reg, cfg.Notifications)
	})
}
