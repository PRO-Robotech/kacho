// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package cidrgroup — use-case-слой ресурса CidrGroup (именованный набор
// префиксов) плюс тонкий gRPC-handler.
//
// Форма пакета — ровно та же, что у сети: чтение синхронно, мутации через
// операцию, состав правится глаголами, а не полной заменой. Совпадение не
// косметическое: третья идиома для того же класса завела бы второй способ делать
// одно и то же, и первый же читатель спросил бы, чем они отличаются.
package cidrgroup

import (
	"context"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// Pagination / CidrGroupFilter — пере-используем единые value-объекты
// `internal/repo` (alias'ы, не копии).
type (
	Pagination      = repo.Pagination
	CidrGroupFilter = kacho.CidrGroupFilter
)

// Re-export CQRS-Repository типов — use-case-код работает с ними под коротким
// именем. Type-alias (не type wrap): тип взаимозаменяем с источником.
type (
	Repo   = kacho.Repository
	Reader = kacho.RepositoryReader
	Writer = kacho.RepositoryWriter
)

// ProjectClient — то, что use-case'ам нужно от peer-сервиса iam: существование
// проекта. Владелец проекта — iam, поэтому ссылка на него проверяется вызовом
// владельцу, а не локальной таблицей.
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
