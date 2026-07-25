// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package observability

import "log/slog"

// BootPostureMsg — сообщение единственной boot-строки, в которой процесс
// САМ отчитывается о posture, с которой он реально стартовал.
//
// Зачем это существует: production-posture гейт раньше читал ХРАНИМЫЙ конфиг
// (values/ConfigMap) и потому рапортовал успех, пока сервис фактически работал в
// dev-режиме с незашифрованным DB-соединением. Гейт обязан утверждать на
// НАБЛЮДАЕМОМ факте — на самоотчёте живого процесса, а не на намерении.
//
// Сообщение и имена полей парсятся гейтом (jq) → это ХАРД-КОНТРАКТ: переименование
// ключа или msg молча ослепляет гейт. Менять только вместе с гейтом.
const BootPostureMsg = "boot security posture"

// DBSSLModeNotApplicable — значение поля db_sslmode для сервиса БЕЗ базы
// (api-gateway). Литерал, а не пустая строка: гейт отличает «БД нет» от
// «поле не заполнено».
const DBSSLModeNotApplicable = "n/a"

// BootPosture — самоотчёт процесса о принятой security-posture.
//
// ВСЕ поля обязаны отражать то, что процесс РЕАЛЬНО собирается использовать
// (провалидированный config-struct / уже собранная проводка), а не сырое env и
// не константу. Заполняется в composition root каждого сервиса ПОСЛЕ
// secure-by-default boot-guard'ов и ДО старта листенеров.
type BootPosture struct {
	// Service — каноническое короткое имя сервиса: vpc | compute | nlb | iam |
	// geo | registry | storage | api-gateway.
	Service string
	// AuthMode — режим, который процесс реально принял (production |
	// production-strict | dev).
	AuthMode string
	// DBSSLMode — sslmode, который реально уходит в DSN/пул (у сервиса без БД —
	// DBSSLModeNotApplicable).
	DBSSLMode string
	// PublicMTLS / InternalMTLS — фактическое состояние mTLS на ДВУХ gRPC-листенерах
	// (public :9090 / cluster-internal :9091).
	PublicMTLS   bool
	InternalMTLS bool
	// AuthZCheck — проведён ли per-RPC authz-Check (непустой адрес / не-nil клиент).
	AuthZCheck bool
}

// LogBootPosture пишет BootPosture единственной структурированной строкой.
// Единственное место, где живут msg и имена полей контракта — поэтому восемь
// сервисов не могут разъехаться по форме.
func LogBootPosture(logger *slog.Logger, p BootPosture) {
	logger.Info(BootPostureMsg,
		"service", p.Service,
		"auth_mode", p.AuthMode,
		"db_sslmode", p.DBSSLMode,
		"public_mtls", p.PublicMTLS,
		"internal_mtls", p.InternalMTLS,
		"authz_check", p.AuthZCheck,
	)
}
