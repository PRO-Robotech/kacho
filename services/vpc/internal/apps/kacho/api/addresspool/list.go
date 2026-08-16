// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package addresspool

import (
	"context"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/listpage"

	kachorepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// ListAddressPoolsUseCase — admin-only list. AddressPool — глобальный
// infrastructure-ресурс (нет project_id), фильтрация по (zone_id, kind).
// Чтение идет в Reader-TX kacho.Repository.
type ListAddressPoolsUseCase struct {
	repo Repo
}

// NewListAddressPoolsUseCase собирает use-case.
func NewListAddressPoolsUseCase(r Repo) *ListAddressPoolsUseCase {
	return &ListAddressPoolsUseCase{repo: r}
}

// Execute возвращает страницу пулов (AddressPoolRecord, с CreatedAt) + next-page token.
//
// Форма страницы проверяется ЗДЕСЬ — в той же функции, что читает, и ТЕМ ЖЕ
// кодеком курсора, что путь чтения (`listpage.ValidatePagination` зовёт
// `helpers.DecodePageToken`). Второй кодек разошёлся бы с первым молча, и
// разошёлся бы именно там, где расхождение не видно: оба отвечают «годно» на
// годном входе.
func (u *ListAddressPoolsUseCase) Execute(ctx context.Context, f AddressPoolFilter, p Pagination) ([]*kachorepo.AddressPoolRecord, string, error) {
	if err := listpage.ValidatePagination(p.PageToken, p.PageSize); err != nil {
		return nil, "", err
	}
	rd, err := u.repo.Reader(ctx)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = rd.Close() }()

	return rd.AddressPools().List(ctx, f, p)
}
