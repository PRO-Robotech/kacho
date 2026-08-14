// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package serviceerr

import "github.com/PRO-Robotech/kacho/services/compute/internal/ports"

// Sentinel-ошибки живут в leaf-пакете `internal/ports` — это позволяет общему
// test-helper'у `internal/ports/portmock` возвращать их без зависимости от
// use-case-пакетов. Здесь — ре-экспорт через `var`-alias'ы (те же error-value,
// поэтому `errors.Is(err, serviceerr.ErrNotFound)` работает). Зеркалит
// kacho-vpc/internal/apps/kacho/shared/serviceerr/errors.go.
var (
	// ErrNotFound возвращается, когда ресурс не найден.
	ErrNotFound = ports.ErrNotFound
	// ErrAlreadyExists возвращается при нарушении UNIQUE constraint.
	ErrAlreadyExists = ports.ErrAlreadyExists
	// ErrInvalidArg возвращается при некорректных входных данных.
	ErrInvalidArg = ports.ErrInvalidArg
	// ErrFailedPrecondition возвращается, когда операция отклонена из-за
	// состояния ресурса. Маппится в gRPC FailedPrecondition.
	ErrFailedPrecondition = ports.ErrFailedPrecondition
	// ErrInternal — generic-ошибка для неклассифицированных DB-проблем.
	ErrInternal = ports.ErrInternal
	// ErrQuotaExceeded — место кончилось: потолок назван и выбран.
	ErrQuotaExceeded = ports.ErrQuotaExceeded
	// ErrQuotaNotProvisioned — потолок не назван ни на одной области.
	ErrQuotaNotProvisioned = ports.ErrQuotaNotProvisioned
)
