// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// ntf3_module_flag_test.go — полоса RED S1-A4 issue-2918 (NTF-3, Н3-Ф2) для
// nlb: флаг модуля KACHO_NLB_NOTIFICATIONS_ENABLED.
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

	"github.com/PRO-Robotech/kacho/services/nlb/internal/apps/kacho/config"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/apps/kacho/jobs"
	nlbpg "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho/pg"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/subscriptionjournal"
)

const (
	ntf3Module = "nlb"
	ntf3Knob   = "KACHO_NLB_NOTIFICATIONS_ENABLED"
	// ntf3ModulePkg — путь импорта корня модуля: по нему перепись держателей
	// сопоставляет тип, отданный конструктором, с каталогом дерева.
	ntf3ModulePkg = "github.com/PRO-Robotech/kacho/services/nlb"
	// ntf3ModuleDir — корень модуля относительно пакета пробы.
	ntf3ModuleDir = "../.."
)

// ntf3Holders — конструкторы держателей journaltx.Options модуля; перечень
// сверяется с переписью дерева (ntf3RequireHoldersCoverTheTree).
func ntf3Holders() []ntf3Holder {
	return []ntf3Holder{
		{"pg.New", nlbpg.New},
		{"jobs.NewFreeIPRunner", jobs.NewFreeIPRunner},
	}
}

// ntf3Load — загрузка конфигурации ТЕМ ЖЕ путём, что на старте (config.Load — main.go; Load сам зовёт Validate).
// value == nil — ручка не задана вовсе.
//
// Посадка dev: отказ по ручке обязан стоять на ЛЮБОЙ посадке (умолчания нет),
// а боевая требует файлов сертификатов, к ручке отношения не имеющих.
func ntf3Load(t *testing.T, value *string) (*config.Config, error) {
	t.Helper()
	for k, v := range map[string]string{
		"KACHO_NLB_MODE":                          "dev",
		"KACHO_NLB_REPOSITORY__POSTGRES__URL":     "postgres://u:p@pg-nlb:5432/kacho_nlb?sslmode=require",
		"KACHO_NLB_EXTAPI__IAM__INTERNAL-ADDR":    "kaname-internal:9091",
		"KACHO_NLB_EXTAPI__IAM__ADDR":             "kaname:9090",
		"KACHO_NLB_AUTHZ__TRUSTED-FORWARDER-SANS": probeGatewaySAN,
		"KACHO_NLB_AUTHZ__TRUST_DOMAIN":           "kacho.cloud",
		"KACHO_NLB_QUOTA__AUTHORITY":              "not-deployed",
	} {
		t.Setenv(k, v)
	}
	ntf3SetKnob(t, value)
	// Load судит конфигурацию сам (parse → Validate); путь к файлу пуст.
	return config.Load("")
}

// TestNTF364_NLBKnobUnsetOrUnparsableRefusesStart — NTF3-64 (nlb).
func TestNTF364_NLBKnobUnsetOrUnparsableRefusesStart(t *testing.T) {
	ntf3RequireKnobRefusal(t, func(t *testing.T, v *string) error { _, err := ntf3Load(t, v); return err })
}

// TestNTF3_65_67_NLBJournalKindsFollowTheFlag — NTF3-65 / NTF3-67, словарь видов.
func TestNTF3_65_67_NLBJournalKindsFollowTheFlag(t *testing.T) {
	ntf3RequireKindsFollowTheFlag(t, subscriptionjournal.Journal)
}

// TestNTF367_NLBBootPostureReportsTheFlag — NTF3-67: самоотчёт посадки
// называет значение флага; близнец — то же при false.
func TestNTF367_NLBBootPostureReportsTheFlag(t *testing.T) {
	ntf3RequireBootPostureReportsTheFlag(t, func(t *testing.T, value string) map[string]any {
		cfg, err := ntf3Load(t, &value)
		if err != nil {
			t.Fatalf("ФИКСТУРА: конфигурация с %s=%s не загрузилась: %v", ntf3Knob, value, err)
		}
		return captureBootPosture(t, bootPosture(cfg))
	})
}

// TestUK361_NLBJournalWritersTakeOptionsAndRefuseZero — УК3-61 (nlb).
func TestUK361_NLBJournalWritersTakeOptionsAndRefuseZero(t *testing.T) {
	ntf3RequireHoldersRefuseZeroOptions(t, ntf3Holders())
}

// TestNTF3_NLBOptionsAreBuiltOnceByTheRoot — И6 (nlb).
func TestNTF3_NLBOptionsAreBuiltOnceByTheRoot(t *testing.T) {
	ntf3RequireOptionsBuiltOnce(t, ntf3ModuleDir)
}
