// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package kachomock

import (
	corevalidate "github.com/PRO-Robotech/kacho/pkg/validate"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/helpers"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// checkPagination — та же проверка формата пагинации, что делает настоящий
// адаптер Postgres в начале каждого List (`validate.PageSize` +
// `helpers.DecodePageToken`).
//
// Зачем она нужна ДУБЛЁРУ. Пока дублёр молча принимал любой page_token и любой
// page_size, ни один unit-тест не мог увидеть, проверяет ли вызывающий формат
// пагинации: подставной репозиторий отвечал успехом на ввод, на котором
// настоящий отвечает отказом. Дублёр, принимающий больше настоящего, делает
// невидимым именно тот дефект, ради которого его подставляют, — поэтому
// проверка формата здесь не «строгость ради строгости», а условие
// наблюдаемости.
//
// Реализация не своя: обе половины взяты у того же кода, который исполняется в
// бою, поэтому расхождение дублёра с адаптером невозможно by construction.
func checkPagination(p kacho.Pagination) error {
	if _, err := corevalidate.PageSize("page_size", p.PageSize); err != nil {
		return err
	}
	if p.PageToken == "" {
		return nil
	}
	if _, _, err := helpers.DecodePageToken(p.PageToken); err != nil {
		return helpers.InvalidPageTokenErr(err)
	}
	return nil
}
