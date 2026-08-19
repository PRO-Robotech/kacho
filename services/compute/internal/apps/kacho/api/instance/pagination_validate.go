// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package instance

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/pagetoken"
	"github.com/PRO-Robotech/kacho/pkg/validate"
)

// ValidateListPagination проверяет вход пагинации СИНХРОННО — до короткого замыкания
// пустого гранта.
//
// Зачем здесь, а не только в репозитории: каждый список compute отдаёт пустую страницу
// РАНО, когда пообъектный грант вызывающего резолвится в ноль идентификаторов, — не
// дойдя до репозитория вовсе. Тогда мусорный курсор и размер вне диапазона уезжали бы
// в `200 {[]}` вместо `400`, и ответ на один и тот же некорректный ввод зависел бы от
// того, что вызывающему выдано.
//
// Проверка зовёт ТОТ ЖЕ разбор, что и путь чтения (pkg/pagetoken), а не описывает его.
// Прежняя редакция воспроизводила форму токена рукописно — «декодируется и содержит
// двоеточие», — и это ровно тот второй кодек, который расходится с первым молча: смена
// формы у владельца не ломала компиляцию зеркала. Общий дом кодека лежит в pkg/,
// поэтому use-case зовёт его, не импортируя адаптер, — слои не нарушены.
func ValidateListPagination(p Pagination) error {
	if _, err := validate.PageSize("page_size", p.PageSize); err != nil {
		return err
	}
	if _, err := pagetoken.Decode(p.PageToken); err != nil {
		return status.Error(codes.InvalidArgument, "page_token is invalid")
	}
	return nil
}
