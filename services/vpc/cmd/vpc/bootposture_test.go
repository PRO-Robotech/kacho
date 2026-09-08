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
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/config"
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

// TestBootPosture_Production — kacho-vpc самоотчитывается о принятой posture.
func TestBootPosture_Production(t *testing.T) {
	var cfg config.Config
	cfg.AuthN.Mode = config.ModeProduction
	cfg.Repository.Postgres.URL = "postgres://u:p@pg-vpc:5432/kacho_vpc"
	cfg.Repository.Postgres.SSLMode = "require"
	cfg.AuthZ.IAMEndpoint = "kaname-internal:9091"
	var mtls config.MTLSConfig
	mtls.PublicServerMTLS.Enable = true
	mtls.InternalServerMTLS.Enable = true

	requireFields(t, captureBootPosture(t, bootPosture(cfg, mtls)), map[string]any{
		"msg":           observability.BootPostureMsg,
		"service":       "vpc",
		"auth_mode":     "production",
		"db_sslmode":    "require",
		"public_mtls":   true,
		"internal_mtls": "true",
		"authz_check":   true,
	})
}

// TestBootPosture_SSLModeComesFromTheDSNThatReachesThePool — sslmode может жить
// прямо в raw-URL (composeDSN его НЕ перетирает). Отчёт обязан показывать
// значение, реально доехавшее до пула, а не сырое поле ssl-mode.
func TestBootPosture_SSLModeComesFromTheDSNThatReachesThePool(t *testing.T) {
	var cfg config.Config
	cfg.AuthN.Mode = config.ModeProductionStrict
	cfg.Repository.Postgres.URL = "postgres://u:p@pg-vpc:5432/kacho_vpc?sslmode=verify-full"
	cfg.Repository.Postgres.SSLMode = "" // сырое поле пустое — URL выигрывает

	requireFields(t, captureBootPosture(t, bootPosture(cfg, config.MTLSConfig{})), map[string]any{
		"auth_mode":  "production-strict",
		"db_sslmode": "verify-full",
	})
}

// TestBootPosture_InsecureIsReportedHonestly — dev + plaintext-DB + оба листенера
// без mTLS + отсутствующий authz-адрес обязаны быть ВИДНЫ (именно этот случай
// гейт проспал, читая хранимый конфиг). Асимметрия листенеров не схлопывается.
func TestBootPosture_InsecureIsReportedHonestly(t *testing.T) {
	var cfg config.Config
	cfg.AuthN.Mode = config.ModeDev
	cfg.Repository.Postgres.URL = "postgres://u:p@pg-vpc:5432/kacho_vpc"
	cfg.Repository.Postgres.SSLMode = "" // → composeDSN деривит `disable`
	cfg.AuthZ.IAMEndpoint = ""
	var mtls config.MTLSConfig
	mtls.PublicServerMTLS.Enable = true // public включён, internal — нет

	requireFields(t, captureBootPosture(t, bootPosture(cfg, mtls)), map[string]any{
		"service":       "vpc",
		"auth_mode":     "dev",
		"db_sslmode":    "disable",
		"public_mtls":   true,
		"internal_mtls": "false",
		"authz_check":   false,
	})
}

// TestBootPosture_EmittedFromTheLiveBootPath — статический страж РАЗМЕЩЕНИЯ:
// строка посадки обязана эмититься ИЗ композиционного корня реальным логгером,
// ПОСЛЕ приёма дескриптора и ДО того, как носитель поднимет слушатели. Без него
// отчёт о посадке деградировал бы в «функцию, которую никто не зовёт», и гейт
// посадки снова читал бы намерение, а не факт.
//
// # Почему нижняя граница переехала на приём ДЕСКРИПТОРА
//
// Прежде она стояла на собственном конструкторе сервера vpc — его больше нет,
// слушатели поднимает носитель. Но замена не механическая: часть измерений
// отчёта (круг отправителей, транспорт обоих слушателей, ребро решения о
// доступе) судит именно КОНСТРУКТОР ДЕСКРИПТОРА, и напечатать их раньше значило
// бы напечатать то, что ещё может не пройти, — то есть намерение вместо посадки.
// Поэтому граница именно там, а не «где-нибудь до Serve».
//
// # Что здесь НЕ пинится намеренно
//
// Списки аргументов. Порядок вызовов к ним отношения не имеет, а якорь по полной
// сигнатуре краснеет на верном коде при первой же правке проводки — это ровно тот
// класс, из-за которого прежний якорь (`grpcsrv.NewServer(`) пережил свой предмет
// незамеченным.
func TestBootPosture_EmittedFromTheLiveBootPath(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read composition root: %v", err)
	}
	root := string(src)

	call := strings.Index(root, "observability.LogBootPosture(")
	if call < 0 {
		t.Fatal("composition root must emit the posture line: observability.LogBootPosture(…)")
	}
	guard := strings.Index(root, "cfg.ValidatePeerTransport(")
	if guard < 0 || call < guard {
		t.Fatal("posture line must be emitted AFTER the secure-by-default boot guards (config/serverMTLS/peerTransport)")
	}
	accepted := strings.Index(root, "desc, err := describe(")
	if accepted < 0 {
		t.Fatal("composition root must build its descriptor: desc, err := describe(…)")
	}
	if call < accepted {
		t.Fatal("posture line must be emitted AFTER the descriptor is accepted — before that it " +
			"describes an intention, not a posture (the descriptor constructor judges the forwarder " +
			"circle, both listener transports and the authorization decision edge)")
	}
	serve := strings.Index(root, "servicehost.Serve(")
	if serve < 0 {
		t.Fatal("composition root must raise both listeners through the carrier: servicehost.Serve(…)")
	}
	if call > serve {
		t.Fatal("posture line must be emitted BEFORE the carrier raises the listeners")
	}
}
