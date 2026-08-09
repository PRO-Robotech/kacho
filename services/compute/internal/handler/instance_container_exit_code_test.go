// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package handler

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	computev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"
	"github.com/PRO-Robotech/kacho/pkg/operations"
)

// containerCreateReq — валидный CONTAINER-Create: тот же базис, что validCreateReq,
// но с контейнерной веткой kind-oneof.
func containerCreateReq() *computev1.CreateInstanceRequest {
	req := validCreateReq()
	req.InstanceKind = computev1.InstanceKind_CONTAINER
	req.Spec = &computev1.CreateInstanceRequest_ContainerSpec{
		ContainerSpec: &computev1.ContainerSpec{Command: []string{"/bin/true"}},
	}
	return req
}

// TestInstanceHandler_Create_RejectsContainerExitCode — `containerSpec.exitCode`
// на ВХОДЕ отвергается синхронно и с именем поля.
//
// ПРЕДМЕТ (api-conventions.md, «Принято-и-проигнорировано — ЗАПРЕЩЕНО»). Код
// возврата — величина ВЫХОДНАЯ: её выставляет терминальное состояние задания, и
// сервис заполняет её на пути ответа. `ContainerSpec` стоит при этом и в теле
// создания, а отображение proto → domain (`containerSpecFromProto`) этого поля
// не переносило, — значит присланное значение принималось и молча выбрасывалось:
// вызывающий получал успех и мог решить, что исход задания чем-то предопределён.
// Из трёх законных исходов выбран второй — синхронный отказ с именем поля.
//
// ПОЧЕМУ ОТКАЗ ЖИВЁТ В ТРАНСПОРТЕ, А НЕ В USE-CASE. У use-case-DTO этого поля
// нет и заводить его ради отказа значило бы завести мёртвое поле (тот же довод,
// что у RejectUnsupportedCreateFields). Проверка в use-case не имела бы
// ПРОИЗВОДИТЕЛЯ ВХОДА: конвертация обнуляет поле, поэтому ветка не срабатывала
// бы никогда — форма проверки без содержания.
//
// Пара обязательна: без положительного контроля (нулевой код возврата проходит и
// Operation создаётся) отрицание зеленело бы и на обработчике, отвергающем любую
// контейнерную задачу.
func TestInstanceHandler_Create_RejectsContainerExitCode(t *testing.T) {
	t.Run("непустой exitCode — отказ, Operation не создана", func(t *testing.T) {
		h, ops := newInstanceHandlerForValidation(t)
		req := containerCreateReq()
		req.GetContainerSpec().ExitCode = 7

		op, err := h.Create(context.Background(), req)

		require.Nil(t, op, "отвергнутый Create не возвращает Operation")
		require.Equal(t, codes.InvalidArgument, status.Code(err))
		require.Contains(t, violationFields(t, err), "container_spec.exit_code",
			"имя поля живёт в google.rpc.BadRequest.field_violations[].field")
		require.Equal(t, "invalid argument", status.Convert(err).Message(),
			"текст сообщения — часть стабильного контракта и полей не называет")

		all, _, lerr := ops.List(context.Background(), operations.ListFilter{})
		require.NoError(t, lerr)
		require.Empty(t, all, "отказ обязан произойти ДО создания операции")
	})

	t.Run("нулевой exitCode проходит (положительный контроль)", func(t *testing.T) {
		h, ops := newInstanceHandlerForValidation(t)

		op, err := h.Create(context.Background(), containerCreateReq())

		require.NoError(t, err, "законная контейнерная задача обязана проходить")
		require.NotEmpty(t, op.GetId())
		all, _, lerr := ops.List(context.Background(), operations.ListFilter{})
		require.NoError(t, lerr)
		require.Len(t, all, 1)
	})
}
