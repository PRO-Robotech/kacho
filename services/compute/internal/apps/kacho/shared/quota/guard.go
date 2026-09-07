// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package quota — совещательная полоса учёта числа ресурсов и материализация
// строк учёта у владельца типа.
package quota

import (
	"context"
	"errors"
	"fmt"

	"github.com/PRO-Robotech/kacho/pkg/peer"
	"github.com/PRO-Robotech/kacho/pkg/quota/quotaread"
	"github.com/PRO-Robotech/kacho/services/compute/internal/ports"
)

// ЧТО ЭТА ПОЛОСА ЕСТЬ И ЧЕМ ОНА НЕ ЯВЛЯЕТСЯ. Она даёт арендатору РАННИЙ отказ —
// до создания операции — и она же есть повод материализовать строки учёта.
// Решением она НЕ является: решение принимает условный `UPDATE` триггера в той
// же транзакции, что вставка строки ресурса. Трактовать её «да» как разрешение
// нельзя — между ней и вставкой помещается чужая запись.
//
// ПОЧЕМУ МАТЕРИАЛИЗАЦИЯ ВИСИТ ИМЕННО НА ПРОМАХЕ. Материализовать «при создании
// проекта» нельзя: проект заводит kaname, а ребра `iam → владелец` не
// существует и не будет — iam остаётся листом, и синхронный вызов в семь
// владельцев сделал бы создание проекта заложником доступности каждого. Значит
// строку учёта заводит владелец, и единственный момент, когда он узнаёт о новом
// проекте, — первая мутация в нём.

// limitRevisionUnknown — ревизия величины на момент материализации.
//
// Ноль означает «ревизия ещё не сверялась»: `Resolve` отдаёт разрешённую
// величину, но не номер ревизии, по которому ходит дельта. Первая же сверка
// синхронизатора проставит настоящую. Ноль выбран сознательно как значение,
// меньшее любой настоящей ревизии, — тогда «не сверялось» и «сверялось давно»
// упорядочены одинаково и строка не пропускается дельтой.
const limitRevisionUnknown = 0

// LimitResolver — порт владельца величин (`InternalLimitService.Resolve`).
type LimitResolver interface {
	Resolve(ctx context.Context, scopeID, service string) ([]ports.ResolvedLimit, error)
}

// AccountLocator — порт резолва аккаунта проекта.
type AccountLocator interface {
	AccountOf(ctx context.Context, projectID string) (string, error)
}

// QuotaStore — порт учёта у владельца.
type QuotaStore interface {
	Admit(ctx context.Context, carrierType, carrierID, kind string) error
	Materialize(ctx context.Context, rows []ports.QuotaRow) (int64, error)
	// ListStates отдаёт строки учёта носителя — то, что арендатор читает как свои
	// квоты.
	//
	// Пустой срез означает «строк учёта ещё нет», а НЕ «квот нет»: различать эти
	// два состояния обязан вызывающий, и он же обязан ответить арендатору полным
	// набором видов с нулевым потреблением.
	ListStates(ctx context.Context, carrierType, carrierID string) ([]quotaread.State, error)
}

// Guard — совещательная полоса плюс материализация по промаху.
type Guard struct {
	quotas   QuotaStore
	resolver LimitResolver
	accounts AccountLocator
	service  string
}

// NewGuard создаёт стража. `service` — домен, чьи виды резолвятся (`compute`).
func NewGuard(quotas QuotaStore, resolver LimitResolver, accounts AccountLocator, service string) *Guard {
	return &Guard{quotas: quotas, resolver: resolver, accounts: accounts, service: service}
}

// Admit спрашивает о месте и, если строки учёта нет, заводит её и спрашивает
// СНОВА.
//
// Повтор ровно один: если и после материализации потолка нет, значит владелец
// величин его не назвал — и это терминальный отказ `QUOTA_NOT_PROVISIONED`, а не
// повод крутиться. Бесконечный повтор превратил бы «не назначено» в зависание.
//
// Нулевой страж пропускает: он означает «полоса не провязана» и встречается
// только в пробах, где предмет проверки другой. На развёрнутом стенде страж
// всегда провязан — иначе первая же мутация в свежем проекте отвергалась бы
// навсегда, потому что материализовать строки было бы некому.
func (g *Guard) Admit(ctx context.Context, projectID, kind string) error {
	if g == nil {
		return nil
	}
	err := g.quotas.Admit(ctx, ports.QuotaCarrierProject, projectID, kind)
	if !errors.Is(err, ports.ErrQuotaNotProvisioned) {
		return err
	}
	if merr := g.materialise(ctx, projectID); merr != nil {
		return merr
	}
	return g.quotas.Admit(ctx, ports.QuotaCarrierProject, projectID, kind)
}

// materialise заводит строки учёта по ВСЕМ видам домена разом.
//
// По всем, а не по тому виду, куда пришла первая запись: иначе новый вид не
// появился бы у уже живущих проектов и потребовал бы дозаписи беклогом.
func (g *Guard) materialise(ctx context.Context, projectID string) error {
	accountID, err := g.accounts.AccountOf(ctx, projectID)
	if err != nil {
		return g.peerRefusal(err, "iam.project", projectID)
	}
	if accountID == "" {
		// Пустое зеркало не записывается: строка без него невидима аккаунтной
		// дельте и жила бы со старой величиной, а снаружи это неотличимо от
		// исправной работы — дельта отчитается успехом, просто не тронув её.
		// Схема такую строку и не примет; отказ здесь называет причину, вместо
		// того чтобы отдать наружу нарушение ограничения.
		return fmt.Errorf("%w: project %s carries no account", ports.ErrFailedPrecondition, projectID)
	}

	limits, err := g.resolver.Resolve(ctx, projectID, g.service)
	if err != nil {
		return g.peerRefusal(err, "iam.limit", projectID)
	}
	if len(limits) == 0 {
		// Владелец величин не назвал ни одного вида этого домена. Строк не
		// заводим — и повторный вопрос отдаст `QUOTA_NOT_PROVISIONED`, то есть
		// ровно то, что здесь и произошло. Подставлять свою величину нельзя:
		// числа живут в посеве владельца, а не в коде потребителя.
		return nil
	}

	rows := make([]ports.QuotaRow, 0, len(limits))
	for _, l := range limits {
		rows = append(rows, ports.QuotaRow{
			CarrierType:   ports.QuotaCarrierProject,
			CarrierID:     projectID,
			Kind:          l.Kind,
			Limit:         l.Value,
			SourceScope:   l.SourceScope,
			SourceScopeID: l.SourceScopeID,
			LimitRevision: limitRevisionUnknown,
			AccountID:     accountID,
		})
	}
	_, err = g.quotas.Materialize(ctx, rows)
	return err
}

// peerRefusal переводит отказ соседа в полосу, которую вызывающий обязан
// различить: недоступность повторяема, а негодная ссылка — нет.
//
// Мутация fail-closed: недоступный владелец величин НЕ означает «разрешено».
func (g *Guard) peerRefusal(err error, resourceType, resourceID string) error {
	return peer.Classify(err).Status(
		peer.Ref{Service: "compute", ResourceType: resourceType, ResourceID: resourceID},
		peer.Prose{
			Missing:     "resource count limits are not stated for project %s",
			State:       "resource count limits for project %s cannot be read",
			Unavailable: "resource count limits are temporarily unavailable",
		},
	)
}
