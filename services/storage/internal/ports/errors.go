// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package ports — чистые sentinel-ошибки, разделяемые между use-case-слоем
// (internal/service/*) и adapter'ами (internal/repo, internal/clients). Leaf-пакет:
// НЕ импортирует pgx / grpc (только stdlib errors), поэтому его безопасно тянет
// use-case, не втягивая Postgres-драйвер в dependency-closure. Трансляция
// SQLSTATE→sentinel (зависит от pgx) живёт в repo-adjacent adapter, gRPC-статус →
// в internal/serviceerr.
package ports

import "errors"

// Sentinel-ошибки adapter/use-case-слоя. Сырой pgx/gRPC-текст наружу не утекает —
// handler маппит их в фиксированный gRPC-код через serviceerr.ToStatus.
var (
	// ErrNotFound — строки не существует (pgx.ErrNoRows).
	ErrNotFound = errors.New("not found")
	// ErrAlreadyExists — нарушение UNIQUE / PK (SQLSTATE 23505).
	ErrAlreadyExists = errors.New("already exists")
	// ErrFailedPrecondition — нарушение FK / конфликт состояния (SQLSTATE 23503/CAS).
	ErrFailedPrecondition = errors.New("failed precondition")
	// ErrInvalidArg — нарушение CHECK / доменной валидации (SQLSTATE 23514).
	ErrInvalidArg = errors.New("invalid argument")
	// ErrInternal — некатегоризированная ошибка БД (без утечки pgx-текста).
	ErrInternal = errors.New("internal database error")
	// ErrUnimplemented — путь ещё не реализован (скелет). rpc-implementer заменяет
	// каждую заглушку реальной реализацией по строгому TDD.
	ErrUnimplemented = errors.New("not implemented")
)
