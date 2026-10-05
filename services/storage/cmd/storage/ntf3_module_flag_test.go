// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// ntf3_module_flag_test.go — полоса RED S1-A4 issue-2918 (NTF-3, Н3-Ф2) для
// storage: флаг модуля KACHO_STORAGE_NOTIFICATIONS_ENABLED.
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

	"github.com/PRO-Robotech/kacho/services/storage/internal/config"
	"github.com/PRO-Robotech/kacho/services/storage/internal/reconciler"
	storagepg "github.com/PRO-Robotech/kacho/services/storage/internal/repo/pg"
	"github.com/PRO-Robotech/kacho/services/storage/internal/subscriptionjournal"
)

const (
	ntf3Module = "storage"
	ntf3Knob   = "KACHO_STORAGE_NOTIFICATIONS_ENABLED"
	// ntf3ModulePkg — путь импорта корня модуля: по нему перепись держателей
	// сопоставляет тип, отданный конструктором, с каталогом дерева.
	ntf3ModulePkg = "github.com/PRO-Robotech/kacho/services/storage"
	// ntf3ModuleDir — корень модуля относительно пакета пробы.
	ntf3ModuleDir = "../.."
)

// ntf3Holders — конструкторы держателей journaltx.Options модуля; перечень
// сверяется с переписью дерева (ntf3RequireHoldersCoverTheTree).
func ntf3Holders() []ntf3Holder {
	return []ntf3Holder{
		{"pg.NewDiskTypeBindingRepo", storagepg.NewDiskTypeBindingRepo},
		{"pg.NewImageRepo", storagepg.NewImageRepo},
		{"pg.NewSnapshotRepo", storagepg.NewSnapshotRepo},
		{"pg.NewVolumeRepo", storagepg.NewVolumeRepo},
		{"reconciler.NewStore", reconciler.NewStore},
	}
}

// ntf3Load — загрузка конфигурации ТЕМ ЖЕ путём, что на старте (config.Load,
// затем cfg.Validate — serve.go). value == nil — ручка не задана вовсе.
//
// Посадка dev: отказ по ручке обязан стоять на ЛЮБОЙ посадке (умолчания нет),
// а боевая требует файлов сертификатов, к ручке отношения не имеющих.
func ntf3Load(t *testing.T, value *string) (config.Config, error) {
	t.Helper()
	for k, v := range map[string]string{
		"KACHO_STORAGE_DB_PASSWORD":                  "secret",
		"KACHO_STORAGE_AUTHZ_IAM_GRPC_ADDR":          "kaname-internal:9091",
		"KACHO_STORAGE_AUTHZ_TRUSTED_FORWARDER_SANS": gatewaySAN + "," + computeSAN,
		"KACHO_STORAGE_AUTHZ_TRUST_DOMAIN":           "kacho.cloud",
		"KACHO_STORAGE_AUTH_MODE":                    "dev",
		"KACHO_STORAGE_QUOTA_AUTHORITY":              "not-deployed",
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

// TestNTF364_StorageKnobUnsetOrUnparsableRefusesStart — NTF3-64 (storage).
func TestNTF364_StorageKnobUnsetOrUnparsableRefusesStart(t *testing.T) {
	ntf3RequireKnobRefusal(t, func(t *testing.T, v *string) error { _, err := ntf3Load(t, v); return err })
}

// TestNTF3_65_67_StorageJournalKindsFollowTheFlag — NTF3-65 / NTF3-67, словарь видов.
func TestNTF3_65_67_StorageJournalKindsFollowTheFlag(t *testing.T) {
	ntf3RequireKindsFollowTheFlag(t, subscriptionjournal.Journal)
}

// TestNTF367_StorageBootPostureReportsTheFlag — NTF3-67: самоотчёт посадки
// называет значение флага; близнец — то же при false.
func TestNTF367_StorageBootPostureReportsTheFlag(t *testing.T) {
	ntf3RequireBootPostureReportsTheFlag(t, func(t *testing.T, value string) map[string]any {
		cfg, err := ntf3Load(t, &value)
		if err != nil {
			t.Fatalf("ФИКСТУРА: конфигурация с %s=%s не загрузилась: %v", ntf3Knob, value, err)
		}
		return captureBootPosture(t, bootPosture(cfg))
	})
}

// TestUK361_StorageJournalWritersTakeOptionsAndRefuseZero — УК3-61 (storage).
func TestUK361_StorageJournalWritersTakeOptionsAndRefuseZero(t *testing.T) {
	ntf3RequireHoldersRefuseZeroOptions(t, ntf3Holders())
}

// TestNTF3_StorageOptionsAreBuiltOnceByTheRoot — И6 (storage).
func TestNTF3_StorageOptionsAreBuiltOnceByTheRoot(t *testing.T) {
	ntf3RequireOptionsBuiltOnce(t, ntf3ModuleDir)
}
