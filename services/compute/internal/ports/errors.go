// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package ports

import "errors"

// Sentinel-ошибки слоя use-case/repo. Живут здесь (в leaf-пакете ports), а не в
// use-case-пакете, чтобы общий test-helper `internal/ports/portmock` мог их
// возвращать, не завися от use-case (иначе — import-cycle с white-box тестами
// `apps/kacho/api/<resource>`). `apps/kacho/shared/serviceerr` ре-экспортирует их
// через `var`-alias'ы (`var ErrNotFound = ports.ErrNotFound` — тот же
// error-value, так что `errors.Is` работает прозрачно).
var (
	// ErrNotFound возвращается, когда ресурс не найден.
	ErrNotFound = errors.New("not found")

	// ErrPeerValidationSkipped — маркер Noop-peer'а при
	// KACHO_COMPUTE_SKIP_PEER_VALIDATION=true (dev/test): cross-service проверка
	// НЕ выполнялась. Use-case пропускает такую проверку явно — тем же
	// задокументированным dev-послаблением, что NoopGeoClient («любая зона
	// существует»). В любом развёрнутом стенде флаг выключен, и это никогда не
	// возвращается.
	ErrPeerValidationSkipped = errors.New("peer validation skipped")
	// ErrAlreadyExists возвращается при нарушении UNIQUE constraint.
	ErrAlreadyExists = errors.New("already exists")
	// ErrInvalidArg возвращается при некорректных входных данных.
	ErrInvalidArg = errors.New("invalid argument")
	// ErrFailedPrecondition возвращается, когда операция отклонена из-за
	// состояния ресурса (например, удаление Disk пока он attached — нарушение
	// FK 23503). Маппится в gRPC FailedPrecondition.
	ErrFailedPrecondition = errors.New("failed precondition")
	// ErrInternal — generic-ошибка для неклассифицированных DB-проблем.
	// Маппится на gRPC Internal с фиксированным сообщением (no leak).
	ErrInternal = errors.New("internal database error")
)
