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
	"github.com/PRO-Robotech/kacho/services/compute/internal/config"
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

// TestBootPosture_Production — kacho-compute самоотчитывается о принятой posture.
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
		"service":       "compute",
		"auth_mode":     "production",
		"db_sslmode":    "require",
		"public_mtls":   true,
		"internal_mtls": "true",
		"authz_check":   true,
	})
}

// TestBootPosture_InsecureIsReportedHonestly — dev + plaintext-DB + асимметричный
// mTLS + отсутствующий authz-адрес обязаны быть видны как есть. Пустой DBSSLMode
// деривится в `disable` (то, что реально уходит в DSN).
func TestBootPosture_InsecureIsReportedHonestly(t *testing.T) {
	cfg := config.Config{AuthMode: "dev", DBSSLMode: ""}
	cfg.InternalServerMTLS.Enable = true // internal включён, public — нет

	requireFields(t, captureBootPosture(t, bootPosture(cfg)), map[string]any{
		"service":       "compute",
		"auth_mode":     "dev",
		"db_sslmode":    "disable",
		"public_mtls":   false,
		"internal_mtls": "true",
		"authz_check":   false,
	})
}

// TestBootPosture_EmittedFromTheLiveBootPath — статический guard РАЗМЕЩЕНИЯ:
// строка обязана эмититься ИЗ композиционного корня, ПОСЛЕ приёма дескриптора
// (то есть после всех отказов старта, являющихся свойствами объявленного) и ДО
// того, как слушатели поднимет носитель.
//
// Порядок несущий и он же был предметом дефекта посадки: гейт обязан утверждать
// на посадке, ОБЪЯВЛЕННОЙ ПРОЦЕССОМ ПРИ СТАРТЕ, а не на хранимом конфиге —
// правка настроек без переката пода оставляет процесс с прежним окружением.
// Строка, напечатанная до приёма дескриптора, описывала бы намерение; строка,
// напечатанная после подъёма слушателей, опоздала бы к первому соединению.
//
// Прежняя редакция якорилась на собственный конструктор сервера
// (`grpcsrv.NewServer`) как на «подъём слушателей». Его в корне больше нет —
// сервера собирает носитель, — и требование переехало на два новых якоря: приём
// дескриптора и вызов носителя. Якорь — НАЧАЛО вызова, а не полный его текст:
// проба утверждает ПОРЯДОК, и список аргументов к этому отношения не имеет,
// иначе она краснела бы на всяком новом поле дескриптора — то есть на верном коде.
func TestBootPosture_EmittedFromTheLiveBootPath(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read composition root: %v", err)
	}
	root := string(src)

	call := strings.Index(root, "observability.LogBootPosture(logger, bootPosture(")
	if call < 0 {
		t.Fatal("composition root must emit the posture line: observability.LogBootPosture(logger, bootPosture(…))")
	}
	guard := strings.Index(root, "validateAuthMode(cfg, logger)")
	if guard < 0 || call < guard {
		t.Fatal("posture line must be emitted AFTER the validateAuthMode boot guard")
	}
	accepted := strings.Index(root, "desc, err := describe(")
	if accepted < 0 || call < accepted {
		t.Fatal("posture line must be emitted AFTER the descriptor is accepted by its constructor — " +
			"before that it would describe an intent, not a posture the process actually took")
	}
	serve := strings.Index(root, "servicehost.Serve(serveCtx, desc,")
	if serve < 0 || call > serve {
		t.Fatal("posture line must be emitted BEFORE the carrier raises the listeners")
	}
}
