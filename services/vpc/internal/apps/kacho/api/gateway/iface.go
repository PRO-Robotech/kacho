// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package gateway — use-case-структура ресурса Gateway. Бизнес-логика
// CreateGatewayUseCase / UpdateGatewayUseCase / DeleteGatewayUseCase /
// GetGatewayUseCase / ListGatewaysUseCase / ListOperationsUseCase плюс тонкий
// gRPC-handler.
//
// Gateway use-case'ы работают через CQRS-Repository (Reader / Writer split).
// Каждый mutating use-case открывает TX явно (`u.repo.Writer(ctx)`), эмитит
// outbox через `w.Outbox().Emit(...)` в той же TX, затем Commit — атомарность
// DML + outbox гарантирована.
package gateway

import (
	"context"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// Pagination — пере-используем единый value-объект `internal/repo` (type-alias).
type (
	Pagination    = repo.Pagination
	GatewayFilter = kacho.GatewayFilter
)

// Re-export CQRS-Repository типов из `internal/repo/kacho` — use-case-код
// работает с ними под коротким именем (`Repo` / `Reader` / `Writer`). Type-alias
// (не type wrap) — тип взаимозаменяем с источником, никаких shim'ов.
//
// Use-case-слой открывает TX явно через `repo.Reader(ctx)` / `repo.Writer(ctx)` и
// видит разделение reader/writer в типе вызова — это держит сервис тонким и
// фиксирует точку транзакции.
type (
	Repo               = kacho.Repository
	Reader             = kacho.RepositoryReader
	Writer             = kacho.RepositoryWriter
	GatewayReaderIface = kacho.GatewayReaderIface
	GatewayWriterIface = kacho.GatewayWriterIface
	OutboxEmitter      = kacho.OutboxEmitter
)

// ProjectClient — то, что use-case'ам Gateway нужно от peer-сервиса
// kaname: проверка существования project'а.
type ProjectClient interface {
	Exists(ctx context.Context, projectID string) (bool, error)
}

// QuotaGuard — совещательная полоса учёта числа ресурсов.
//
// Приёмка `docs/specs/sub-phase-quota-v2-materialised-usage-acceptance.md`
// (APPROVED, раунд 2), DoD S2 п.3 и п.5. Порт объявлен здесь, у вызывающего;
// реализация — `apps/kacho/shared/quota`.
//
// Полоса НЕ ПРИНИМАЕТ решения: между её ответом и вставкой помещается чужая
// запись, и место занимает атомарное списание триггера в writer-транзакции
// (ban #10). Она существует ради РАННЕГО отказа — иначе исчерпание предела
// наблюдается как «200 и операция, упавшая через секунду», — и ради
// материализации строк учёта на промахе: без неё триггеру нечего списывать.
type QuotaGuard interface {
	Admit(ctx context.Context, projectID, kind string) error
}
