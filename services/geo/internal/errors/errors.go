// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package errors — чистые sentinel-ошибки repo-слоя kacho-geo. Leaf-пакет: НЕ
// импортирует pgx/grpc (только stdlib errors), поэтому его безопасно тянет
// use-case/domain-слой за одними константами, не втягивая Postgres-драйвер в свой
// dependency-closure. Трансляция SQLSTATE→sentinel (adapter, зависит от pgx)
// живёт отдельно в repo-adjacent пакете internal/repo/kacho/dberr.
package errors

import "errors"

// Sentinel-ошибки repo-слоя. Сырой pgx-текст наружу не утекает (handler/worker
// маппят их в фиксированный gRPC-код через serviceerr.ToStatus).
var (
	// ErrNotFound — строки не существует (pgx.ErrNoRows).
	ErrNotFound = errors.New("not found")
	// ErrAlreadyExists — нарушение UNIQUE / PK (SQLSTATE 23505).
	ErrAlreadyExists = errors.New("already exists")
	// ErrFailedPrecondition — нарушение FK / конфликт состояния (SQLSTATE 23503).
	ErrFailedPrecondition = errors.New("failed precondition")
	// ErrInvalidArg — нарушение CHECK (SQLSTATE 23514).
	ErrInvalidArg = errors.New("invalid argument")
	// ErrInternal — некатегоризированная ошибка БД (без утечки pgx-текста).
	ErrInternal = errors.New("internal database error")
	// ErrCanceled — запрос отменён вызывающей стороной (context.Canceled): нормальный
	// исход client-cancel, не серверный сбой. Маппится в codes.Canceled, НЕ Internal.
	ErrCanceled = errors.New("canceled")
	// ErrDeadlineExceeded — истёк per-call deadline (context.DeadlineExceeded):
	// latency/timeout, не серверный сбой. Маппится в codes.DeadlineExceeded, НЕ Internal.
	ErrDeadlineExceeded = errors.New("deadline exceeded")
)
