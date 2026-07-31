// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package address

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/kachomock"
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
func TestListPaginationFormatCheckedBeforeIdentityShortCircuit(t *testing.T) {
	const garbageToken = "not-a-real-token!!"

	t.Run("названный вызывающий — отказ по формату (положительный контроль)", func(t *testing.T) {
		uc := NewListAddressesUseCase(kachomock.NewRepository(), &fakeListFilter{allowAll: true})

		_, _, err := uc.Execute(context.Background(), "user:usr_alice",
			AddressFilter{ProjectID: "prj_1"}, Pagination{PageToken: garbageToken})

		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err),
			"мусорный курсор у названного вызывающего обязан быть отвергнут по формату")
	})

	t.Run("вызывающий не опознан — тот же отказ по формату", func(t *testing.T) {
		uc := NewListAddressesUseCase(kachomock.NewRepository(), &fakeListFilter{allowAll: true})

		_, _, err := uc.Execute(context.Background(), "",
			AddressFilter{ProjectID: "prj_1"}, Pagination{PageToken: garbageToken})

		require.Error(t, err,
			"пустая страница вместо отказа: замыкание по личности опередило проверку формата")
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("page_size вне диапазона — отказ независимо от личности", func(t *testing.T) {
		for name, subject := range map[string]string{"названный": "user:usr_alice", "неопознанный": ""} {
			t.Run(name, func(t *testing.T) {
				uc := NewListAddressesUseCase(kachomock.NewRepository(), &fakeListFilter{allowAll: true})

				_, _, err := uc.Execute(context.Background(), subject,
					AddressFilter{ProjectID: "prj_1"}, Pagination{PageSize: 1001})

				require.Error(t, err)
				assert.Equal(t, codes.InvalidArgument, status.Code(err))
			})
		}
	})

	t.Run("законная первая страница проходит — проба не отвергает всё подряд", func(t *testing.T) {
		uc := NewListAddressesUseCase(kachomock.NewRepository(), &fakeListFilter{allowAll: true})

		_, _, err := uc.Execute(context.Background(), "user:usr_alice",
			AddressFilter{ProjectID: "prj_1"}, Pagination{PageSize: 50})

		require.NoError(t, err)
	})
}
