// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package shared

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/pagetoken"
	corevalidate "github.com/PRO-Robotech/kacho/pkg/validate"
)

// ValidatePagination — синхронная проверка входа пагинации, которую каждый списочный
// use-case nlb выполняет ДО чтения страницы.
//
// Зачем: список, замыкающийся на гранте вызывающего раньше, чем разберёт курсор,
// отвечал бы `200 {[]}` на мусорный курсор и размер вне диапазона вместо `400`, и
// ответ на один и тот же некорректный ввод зависел бы от того, что вызывающему выдано.
//
// Разбор курсора берётся У ИСПОЛНЯЕМОГО КОДА (pkg/pagetoken) — здесь его не
// воспроизводят. Прежняя редакция описывала форму токена рукописно («декодируется и
// содержит нулевой байт») и несла СВОЙ предел размера страницы: смена формы у
// владельца не ломала бы компиляцию этой копии.
func ValidatePagination(pageToken string, pageSize int64) error {
	// Предел один на платформу — corevalidate.MaxPageSize. Третьей копии числа
	// здесь больше нет; текст отказа сохранён прежним намеренно (он часть контракта
	// и меняется отдельным решением, а не попутно со сведением кодека).
	if pageSize < 0 || pageSize > corevalidate.MaxPageSize {
		return status.Errorf(codes.InvalidArgument,
			"page_size must be in range [1, %d]", corevalidate.MaxPageSize)
	}
	if _, err := pagetoken.Decode(pageToken); err != nil {
		return status.Error(codes.InvalidArgument, "page_token is invalid")
	}
	return nil
}
