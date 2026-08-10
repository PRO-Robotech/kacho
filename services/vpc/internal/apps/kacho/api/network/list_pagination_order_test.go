// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package network

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/kachomock"

	"github.com/PRO-Robotech/kacho/pkg/listnarrow/narrowtest"
)

// Формат пагинации проверяется ДО замыкания по личности вызывающего.
//
// Предмет. Use-case сначала решает, кто спрашивает: при неизвлечённом
// принципале и включенном фильтре видимости он возвращает пустую страницу без
// ошибки и до репозитория не доходит. Пока формат курсора проверял только
// репозиторий, один и тот же мусорный `page_token` получал разный ответ в
// зависимости от того, опознан ли вызывающий, — то есть проверка ввода зависела
// от прав.
//
// Пара обязательна. Одиночная проба «неопознанный получает InvalidArgument»
// зеленела бы и на полностью сломанном use-case (любой отказ выглядит как
// нужный), поэтому рядом стоит положительный контроль: тот же мусорный курсор у
// НАЗВАННОГО вызывающего, где отказ приходит с пути чтения. Обе половины
// утверждают один и тот же код и тем самым свойство «ответ на формат не зависит
// от личности».
//
// Чего здесь НЕТ и где оно вместо этого. Сохранность самого замыкания —
// «неопознанный с ЗАКОННОЙ пагинацией по-прежнему получает пустую страницу без
// ошибки, а не отказ» — в этом файле не утверждается, и намеренно. Утверждать
// это тут значило бы звать use-case на ПУСТОМ дублёре репозитория, где выдача
// пуста при любом исходе: проба осталась бы зелёной и со снятым замыканием.
// Свойство закреплено на ЗАСЕЯННОМ репозитории в TestNetworkListPerObject_EmptySubjectFailsClosed этого
// же пакета, где названный вызывающий видит строки, а неопознанный — ни одной.
// Дублировать его здесь незачем; назвать — обязательно, иначе отрицание выше
// остаётся без пары.
func TestListPaginationFormatCheckedBeforeIdentityShortCircuit(t *testing.T) {
	const garbageToken = "not-a-real-token!!"

	t.Run("названный вызывающий — отказ по формату (положительный контроль)", func(t *testing.T) {
		uc := NewListNetworksUseCase(kachomock.NewRepository(), narrowtest.AllowingAll())

		_, _, err := uc.Execute(narrowtest.Caller(), NetworkFilter{ProjectID: "prj_1"}, Pagination{PageToken: garbageToken})

		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err),
			"мусорный курсор у названного вызывающего обязан быть отвергнут по формату")
	})

	t.Run("вызывающий не опознан — тот же отказ по формату", func(t *testing.T) {
		uc := NewListNetworksUseCase(kachomock.NewRepository(), narrowtest.AllowingAll())

		_, _, err := uc.Execute(context.Background(), NetworkFilter{ProjectID: "prj_1"}, Pagination{PageToken: garbageToken})

		require.Error(t, err,
			"пустая страница вместо отказа: замыкание по личности опередило проверку формата")
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("page_size вне диапазона — отказ независимо от личности", func(t *testing.T) {
		for name, caller := range map[string]context.Context{
			"названный":    narrowtest.Caller(),
			"неопознанный": context.Background(),
		} {
			t.Run(name, func(t *testing.T) {
				uc := NewListNetworksUseCase(kachomock.NewRepository(), narrowtest.AllowingAll())

				_, _, err := uc.Execute(caller, NetworkFilter{ProjectID: "prj_1"}, Pagination{PageSize: 1001})

				require.Error(t, err)
				assert.Equal(t, codes.InvalidArgument, status.Code(err))
			})
		}
	})

	t.Run("законная первая страница проходит — проба не отвергает всё подряд", func(t *testing.T) {
		uc := NewListNetworksUseCase(kachomock.NewRepository(), narrowtest.AllowingAll())

		_, _, err := uc.Execute(narrowtest.Caller(), NetworkFilter{ProjectID: "prj_1"}, Pagination{PageSize: 50})

		require.NoError(t, err)
	})
}
