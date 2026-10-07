// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package addresspool

import (
	"context"
	"fmt"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/helpers"
)

// Привязка пула по умолчанию к сети собственного вида в журнале не имеет: у неё
// нет типа в модели прав и нет читателя. Глаголы привязки и снятия привязки
// пишут событие правки САМОГО ПУЛА (`AddressPool` `UPDATED`, без якоря — пул
// уровня кластера) с состоянием пула той же формы, что у прочих строк пула
// (NTF-3, Р2, NTF3-62).

// BindAsNetworkDefaultUseCase — назначить pool как default для Network.
// Family-agnostic: family-фильтр применяется на resolve-этапе, не на bind.
//
// Проверка существования Network/Pool, Set binding и outbox-emit идут в одной
// writer-TX kacho.Repository.Writer(ctx).
type BindAsNetworkDefaultUseCase struct {
	repo    Repo
	netRepo NetworkRepo
}

// NewBindAsNetworkDefaultUseCase собирает use-case.
func NewBindAsNetworkDefaultUseCase(r Repo, netRepo NetworkRepo) *BindAsNetworkDefaultUseCase {
	return &BindAsNetworkDefaultUseCase{repo: r, netRepo: netRepo}
}

// Execute проверяет Network и AddressPool существуют, затем upsert'ит binding и
// пишет правку пула в журнал той же транзакцией.
func (u *BindAsNetworkDefaultUseCase) Execute(ctx context.Context, networkID, poolID string) error {
	if _, err := u.netRepo.Get(ctx, networkID); err != nil {
		return err
	}
	w, err := u.repo.Writer(ctx)
	if err != nil {
		return err
	}
	defer w.Abort()

	pool, err := w.AddressPools().Get(ctx, poolID)
	if err != nil {
		return err
	}
	if err := w.AddressPoolBindings().SetNetworkDefault(ctx, networkID, poolID); err != nil {
		return err
	}
	if err := w.Outbox().Emit(ctx, "AddressPool", pool.ID, helpers.NoProjectAnchor, "UPDATED",
		helpers.AddressPoolDomainPayload(&pool.AddressPool)); err != nil {
		return fmt.Errorf("%w: outbox emit: %v", serviceerr.ErrInternal, err)
	}
	return w.Commit()
}

// UnbindNetworkDefaultUseCase — снятие per-network binding'а (идемпотентно).
type UnbindNetworkDefaultUseCase struct {
	repo Repo
}

// NewUnbindNetworkDefaultUseCase собирает use-case.
func NewUnbindNetworkDefaultUseCase(r Repo) *UnbindNetworkDefaultUseCase {
	return &UnbindNetworkDefaultUseCase{repo: r}
}

// Execute удаляет binding. Idempotent — no error если binding не задан; тогда
// ничего не изменилось, и строки журнала нет.
//
// Пул, чья привязка снята, берётся из `RETURNING` удаляющего оператора, а не
// чтением до него: привязка, переназначенная между чтением и удалением, дала бы
// событие чужого пула.
func (u *UnbindNetworkDefaultUseCase) Execute(ctx context.Context, networkID string) error {
	w, err := u.repo.Writer(ctx)
	if err != nil {
		return err
	}
	defer w.Abort()

	poolID, err := w.AddressPoolBindings().UnsetNetworkDefault(ctx, networkID)
	if err != nil {
		return err
	}
	if poolID != "" {
		pool, err := w.AddressPools().Get(ctx, poolID)
		if err != nil {
			return err
		}
		if err := w.Outbox().Emit(ctx, "AddressPool", pool.ID, helpers.NoProjectAnchor, "UPDATED",
			helpers.AddressPoolDomainPayload(&pool.AddressPool)); err != nil {
			return fmt.Errorf("%w: outbox emit: %v", serviceerr.ErrInternal, err)
		}
	}
	return w.Commit()
}
