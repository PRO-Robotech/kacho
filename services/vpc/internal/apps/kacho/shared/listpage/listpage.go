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
// `pagetoken.Decode` (единственное объявление формата курсора на дерево).
// Собственный разбор здесь означал бы второй кодек, который разъедется с первым
// молча.
//
// Кодек живёт в pkg/, а не в слое репозитория, ровно затем, чтобы эта проверка
// могла звать ИСПОЛНЯЕМЫЙ разбор, не импортируя адаптер: у соседних сервисов
// рукописные зеркала формата завелись именно из-за того, что звать было нечего.
//
// Репозиторий остаётся авторитетным: он повторяет обе проверки на служимом
// пути, поэтому вызывающий, минующий этот пакет, не остаётся без них.
package listpage

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/pagetoken"
	corevalidate "github.com/PRO-Robotech/kacho/pkg/validate"
)

// ValidatePagination — sync-проверка формата пагинации: page_size вне [0..1000]
// и нерасшифровываемый page_token дают InvalidArgument.
//
// Пустой page_token — это первая страница, законное значение.
func ValidatePagination(pageToken string, pageSize int64) error {
	if _, err := corevalidate.PageSize("page_size", pageSize); err != nil {
		return err
	}
	if _, err := pagetoken.Decode(pageToken); err != nil {
		return status.Error(codes.InvalidArgument, "page_token is invalid")
	}
	return nil
}
