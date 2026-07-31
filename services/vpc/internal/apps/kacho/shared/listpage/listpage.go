// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package listpage — проверка формата пагинации на границе use-case'а.
//
// Предмет. Списочный use-case решает, кто спрашивает, раньше, чем читает
// страницу: при неопознанном вызывающем он возвращает пустую страницу без
// ошибки и до репозитория не доходит. Пока формат `page_token`/`page_size`
// проверял только репозиторий, ответ на один и тот же некорректный ввод зависел
// от того, что вызывающему выдано: опознанный получал `400 InvalidArgument`,
// неопознанный — `200 []`. Проверка формата решением о доступе не является, и
// её место — до замыкания.
//
// Почему не своя реализация. Обе половины взяты у того кода, который уже
// исполняется на пути чтения: `validate.PageSize` (общая для платформы граница
// [0..1000], значение вне диапазона отвергается, а не зажимается) и
// `helpers.DecodePageToken` (кодек курсора vpc). Собственный разбор здесь
// означал бы второй кодек, который разъедется с первым молча.
//
// Репозиторий остаётся авторитетным: он повторяет обе проверки на служимом
// пути, поэтому вызывающий, минующий этот пакет, не остаётся без них.
package listpage

import (
	corevalidate "github.com/PRO-Robotech/kacho/pkg/validate"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/helpers"
)

// ValidatePagination — sync-проверка формата пагинации: page_size вне [0..1000]
// и нерасшифровываемый page_token дают InvalidArgument.
//
// Пустой page_token — это первая страница, законное значение.
func ValidatePagination(pageToken string, pageSize int64) error {
	if _, err := corevalidate.PageSize("page_size", pageSize); err != nil {
		return err
	}
	if pageToken == "" {
		return nil
	}
	if _, _, err := helpers.DecodePageToken(pageToken); err != nil {
		return helpers.InvalidPageTokenErr(err)
	}
	return nil
}
