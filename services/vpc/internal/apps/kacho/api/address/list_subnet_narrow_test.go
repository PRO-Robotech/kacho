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

	"github.com/PRO-Robotech/kacho/pkg/listnarrow/narrowtest"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/kachomock"
)

// Сужение списка по подсети — ссылка на СВОЙ ресурс, поэтому её форма
// проверяется, а не уносится в SQL.
//
// Предмет. `subnet_id` списочного запроса — идентификатор подсети, а подсеть
// принадлежит этому же сервису (own-owned). Конвенция для такой ссылки одна:
// малформед отвергается синхронно, `INVALID_ARGUMENT "invalid subnet id '<X>'"`,
// до обращения к хранилищу. Без проверки мусор доезжает до SQL и возвращается
// пустой страницей с кодом 200 — то есть продукт отвечает «в этой подсети адресов
// нет» про строку, которая подсетью быть не может. Ровно тот же вопрос через
// дочерний список (`ListBySubnet`) отвергался с самого начала, поэтому один и тот
// же ввод имел два разных ответа в зависимости от того, каким путём его задали.
//
// Порядок тоже утверждается: проверка формата стоит ДО замыкания по личности
// вызывающего — иначе ответ на некорректный ввод зависел бы от того, что
// вызывающему выдано (см. `list_pagination_order_test.go`, тот же класс).
func TestListNarrowBySubnet_MalformedIdRejectedBeforeIdentityShortCircuit(t *testing.T) {
	const malformed = "не-подсеть-вовсе"

	t.Run("названный вызывающий — отказ по формату (положительный контроль)", func(t *testing.T) {
		uc := NewListAddressesUseCase(kachomock.NewRepository(), narrowtest.AllowingAll())

		_, _, err := uc.Execute(narrowtest.Caller(),
			AddressFilter{ProjectID: "prj_1", SubnetID: malformed}, Pagination{PageSize: 50})

		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
		assert.Equal(t, "invalid subnet id '"+malformed+"'", status.Convert(err).Message(),
			"текст отказа — часть контракта и совпадает с дочерним списком подсети")
	})

	t.Run("вызывающий не опознан — тот же отказ по формату", func(t *testing.T) {
		uc := NewListAddressesUseCase(kachomock.NewRepository(), narrowtest.AllowingAll())

		_, _, err := uc.Execute(context.Background(),
			AddressFilter{ProjectID: "prj_1", SubnetID: malformed}, Pagination{PageSize: 50})

		require.Error(t, err,
			"пустая страница вместо отказа: замыкание по личности опередило проверку формата")
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("законная ссылка на подсеть проходит — проба не отвергает всё подряд", func(t *testing.T) {
		uc := NewListAddressesUseCase(kachomock.NewRepository(), narrowtest.AllowingAll())

		_, _, err := uc.Execute(narrowtest.Caller(),
			AddressFilter{ProjectID: "prj_1", SubnetID: "sub00000000000000000"}, Pagination{PageSize: 50})

		require.NoError(t, err)
	})

	t.Run("сужение не задано — список остаётся списком проекта", func(t *testing.T) {
		// Парный контроль к отказу: пустая строка означает «без сужения», а не
		// «малформед». Без этого утверждения проверка формата могла бы отвергать
		// каждый список без подсети, и отказ выше зеленел бы на сломанном.
		uc := NewListAddressesUseCase(kachomock.NewRepository(), narrowtest.AllowingAll())

		_, _, err := uc.Execute(narrowtest.Caller(),
			AddressFilter{ProjectID: "prj_1"}, Pagination{PageSize: 50})

		require.NoError(t, err)
	})

	t.Run("формат страницы проверяется раньше формата ссылки", func(t *testing.T) {
		// Оба — проверки ввода, но у пагинации первый стейтмент: она не зависит ни
		// от чего в запросе. Утверждение пиннит порядок ТЕКСТОМ отказа, иначе
		// «InvalidArgument» был бы верен при любом из двух.
		uc := NewListAddressesUseCase(kachomock.NewRepository(), narrowtest.AllowingAll())

		_, _, err := uc.Execute(narrowtest.Caller(),
			AddressFilter{ProjectID: "prj_1", SubnetID: malformed},
			Pagination{PageToken: "not-a-real-token!!"})

		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
		assert.NotContains(t, status.Convert(err).Message(), "invalid subnet id",
			"первым обязан отвечать формат страницы")
	})
}
