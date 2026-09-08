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
	"github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/config"
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

// TestBootPosture_Production — kacho-geo самоотчитывается о принятой posture.
func TestBootPosture_Production(t *testing.T) {
	cfg := config.Config{
		AuthMode:         "production",
		DBSSLMode:        "verify-full",
		AuthZIAMGRPCAddr: "kaname-internal:9091",
	}
	cfg.PublicServerMTLS.Enable = true
	cfg.InternalServerMTLS.Enable = true

	requireFields(t, captureBootPosture(t, bootPosture(cfg)), map[string]any{
		"msg":           observability.BootPostureMsg,
		"service":       "geo",
		"auth_mode":     "production",
		"db_sslmode":    "verify-full",
		"public_mtls":   true,
		"internal_mtls": "true",
		"authz_check":   true,
	})
}

// TestBootPosture_InsecureIsReportedHonestly — dev + plaintext-DB (пустой
// DBSSLMode деривится в `disable`) + отсутствие mTLS/authz обязаны быть видны.
func TestBootPosture_InsecureIsReportedHonestly(t *testing.T) {
	cfg := config.Config{AuthMode: "dev", DBSSLMode: ""}

	requireFields(t, captureBootPosture(t, bootPosture(cfg)), map[string]any{
		"service":       "geo",
		"auth_mode":     "dev",
		"db_sslmode":    "disable",
		"public_mtls":   false,
		"internal_mtls": "false",
		"authz_check":   false,
	})
}

// TestBootPosture_EmittedFromTheLiveBootPath — страж размещения: строка
// самоотчёта обязана эмититься ИЗ композиционного корня реальным журналом,
// ПОСЛЕ того как конфигурация принята, и ДО подъёма слушателей.
//
// # Почему координаты сменились, а свойство — нет
//
// Прежняя редакция целилась в собственные стражи geo и в его собственный вызов
// конструктора сервера. Ни того, ни другого в этом корне БОЛЬШЕ НЕТ: стражи
// переехали в конструктор дескриптора, сборка слушателей — в носитель.
// Оставленная как была, проба искала бы мёртвые координаты и падала бы на
// верном коде — то есть стала бы утверждением, пережившим свой предмет.
//
// Свойство при этом ровно то же, и границы его по существу те же:
//   - «конфигурация принята» — успешный возврат `describe`, то есть прохождение
//     ВСЕХ отказов старта, которые являются свойствами дескриптора;
//   - «до подъёма слушателей» — вызов `servicehost.Serve`, который их и поднимает.
//
// Утверждается по-прежнему ПОРЯДОК: посадка, доложенная до того, как её
// приняли, — отчёт о намерении, а не об исходе.
func TestBootPosture_EmittedFromTheLiveBootPath(t *testing.T) {
	src, err := os.ReadFile("serve.go")
	if err != nil {
		t.Fatalf("read composition root: %v", err)
	}
	root := string(src)

	call := strings.Index(root, "observability.LogBootPosture(logger, bootPosture(")
	if call < 0 {
		t.Fatal("composition root must emit the posture line: observability.LogBootPosture(logger, bootPosture(…))")
	}
	// Якорь — ИМЯ вызова, а не его полный список аргументов. Полный список
	// делал бы гейт проверкой написания: добавление законного порта роняло бы
	// его на неизменившемся ПОРЯДКЕ, который он и стережёт (так и случилось —
	// приёмник величин кеша вердиктов приехал седьмым аргументом).
	accepted := strings.Index(root, "describe(cfg, logger")
	if accepted < 0 || call < accepted {
		t.Fatal("posture line must be emitted AFTER the descriptor was accepted (describe(cfg, logger…)) — " +
			"a posture reported before it was accepted states an intent, not an outcome")
	}
	listener := strings.Index(root, "servicehost.Serve(")
	if listener < 0 || call > listener {
		t.Fatal("posture line must be emitted BEFORE the listeners are raised (servicehost.Serve)")
	}
}
