// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package instance

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	computev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"
	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
	"github.com/PRO-Robotech/kacho/services/compute/internal/ports/portmock"
)

// Проверки единственной формы имени для Instance: строка ресурса не может нести
// пустое имя, и снять имя правкой нельзя.
//
// Пробы стоят на уровне use-case, а не на уровне corevalidate: общая функция уже
// проверена своими пробами в pkg/validate, и повторять её ответ здесь означало бы
// закрепить ОТВЕТ вместо МЕСТА. Предмет этого файла — что compute её действительно
// зовёт, зовёт в той точке, где id уже есть, и записывает результат.

// createUnnamedInstance — создание без имени, доведённое до конца операции.
// Возвращает то, что увидел бы вызывающий в ответе.
func createUnnamedInstance(t *testing.T, k instSvcKit, req CreateInstanceReq) *computev1.Instance {
	t.Helper()
	req.Name = ""
	op, err := k.svc.Create(context.Background(), req)
	require.NoError(t, err, "создание без имени обязано пройти: пустое имя означает «назови сам»")
	done := portmock.AwaitOpDone(t, k.ops, op.ID)
	require.Nil(t, done.Error, "создание без имени обязано пройти, отказ = %v", done.Error)
	return instanceFromOp(t, done)
}

// TestInstance_Create_EmptyName_WritesIdDerivedDefault — создание без имени
// записывает имя, производное от идентификатора, а не пустую строку.
//
// Утверждать надо ЗАПИСАННОЕ, а не факт вызова: подстановка, сделанная до
// генерации id, дала бы пустое умолчание и осталась бы «вызовом общей функции».
func TestInstance_Create_EmptyName_WritesIdDerivedDefault(t *testing.T) {
	k := newInstanceSvc(t, true)
	in := createUnnamedInstance(t, k, baseCreateReq())

	assert.NotEmpty(t, in.Name, "строка ресурса не может нести пустое имя")
	assert.Equal(t, in.Id, in.Name, "умолчание — сам идентификатор (pkg/validate.NameOrDefault)")
}

// TestInstance_Create_TwoEmptyNames_BothSucceedWithDistinctNames — два безымянных
// создания в ОДНОМ проекте проходят ОБА и получают РАЗНЫЕ имена.
//
// Умолчание, производное от чего угодно, кроме идентификатора (константа,
// «unnamed», имя вида ресурса), столкнулось бы на уникальности (project, name) —
// и второе создание отвергалось бы AlreadyExists у арендатора, который не сделал
// ничего неверного. Дублёр хранилища это ограничение несёт (portmock.InstanceRepo
// .Insert), поэтому утверждение не вакуумно: сними умолчание с идентификатора —
// и вторая строка не вставится.
func TestInstance_Create_TwoEmptyNames_BothSucceedWithDistinctNames(t *testing.T) {
	k := newInstanceSvc(t, true)
	first := createUnnamedInstance(t, k, baseCreateReq())
	second := createUnnamedInstance(t, k, baseCreateReq())

	assert.NotEqual(t, first.Name, second.Name,
		"два безымянных создания в одном проекте обязаны получить разные имена")
	assert.Equal(t, first.Id, first.Name)
	assert.Equal(t, second.Id, second.Name)
}

// TestInstance_Create_NameStillValidated — форма имени по-прежнему проверяется.
//
// Положительный контроль обязателен рядом с отрицанием: без него проба зеленела
// бы и на реализации, отвергающей ЛЮБОЕ имя, — то есть на прямо противоположном
// дефекте.
func TestInstance_Create_NameStillValidated(t *testing.T) {
	k := newInstanceSvc(t, true)

	legal := baseCreateReq()
	legal.Name = "vm-legal-1"
	op, err := k.svc.Create(context.Background(), legal)
	require.NoError(t, err, "законное имя обязано проходить")
	in := instanceFromOp(t, portmock.AwaitOpDone(t, k.ops, op.ID))
	require.Equal(t, "vm-legal-1", in.Name, "названное имя не подменяется умолчанием")

	bad := baseCreateReq()
	bad.Name = "Bad_Name"
	_, err = k.svc.Create(context.Background(), bad)
	require.Error(t, err, "заглавные и подчёркивание формой не приняты")
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.True(t, rejectedField(t, err, "name"), "отказ обязан называть поле")
}

// TestInstance_Update_MaskNamesName_EmptyRejected — маска НАЗВАЛА имя, значение
// пусто: отказ с именем поля.
//
// Отказ синхронный и приходит из проверки входа, а не из базы: без него пустое
// имя доехало бы до столбца, на который миграция 715001 поставила ограничение
// формы, и вызывающий получил бы внутреннюю ошибку вместо контрактного отказа.
func TestInstance_Update_MaskNamesName_EmptyRejected(t *testing.T) {
	err := validateInstanceUpdate(UpdateInstanceReq{
		InstanceID: seedID, UpdateMask: []string{"name"}, Name: "",
	})
	require.Error(t, err, "снять имя правкой нельзя — имени без значения не бывает")
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.True(t, rejectedField(t, err, "name"),
		"отказ обязан называть поле: код InvalidArgument возвращает вся проверка входа, "+
			"и по одному коду вызывающий не узнает, что именно прислал неверно")

	require.NoError(t, validateInstanceUpdate(UpdateInstanceReq{
		InstanceID: seedID, UpdateMask: []string{"name"}, Name: "vm-2",
	}), "положительный контроль: та же маска с законным именем проходит")
}

// TestInstance_Update_EmptyMask_EmptyNameKeepsCurrent — полная правка, НЕ
// назвавшая имя, имя не стирает.
//
// Предмет — дыра, которую проверка входа закрыть не может: при пустой маске
// пустое имя законно (в proto3 «не прислано» и «пусто» неразличимы), поэтому
// вопрос «записывать ли» решается уже на применении. Записав пустоту, полный
// PATCH описания молча оставил бы машину без имени — и упёрся бы в ограничение
// столбца на пути, где вызывающий не сделал ничего неверного.
func TestInstance_Update_EmptyMask_EmptyNameKeepsCurrent(t *testing.T) {
	k := newInstanceSvc(t, true)
	seedInst(k.repo, seedID, domain.InstanceStatusProvisioning)

	op, err := k.svc.Update(context.Background(), UpdateInstanceReq{
		InstanceID: seedID, Description: "полная правка описания",
	})
	require.NoError(t, err)
	in := instanceFromOp(t, portmock.AwaitOpDone(t, k.ops, op.ID))

	assert.Equal(t, "vm", in.Name, "полная правка без имени обязана сохранить текущее имя")
	assert.NotEmpty(t, in.Name, "ресурса без имени не бывает")
	assert.Equal(t, "полная правка описания", in.Description,
		"положительный контроль: названное этой же правкой обязано примениться")
}

// TestInstance_Update_EmptyMask_NewNameApplied — вторая половина предыдущего:
// та же полная правка с НЕПУСТЫМ именем имя меняет. Без неё проба выше зеленела
// бы и на применении, которое имя не трогает вовсе.
func TestInstance_Update_EmptyMask_NewNameApplied(t *testing.T) {
	k := newInstanceSvc(t, true)
	seedInst(k.repo, seedID, domain.InstanceStatusProvisioning)

	op, err := k.svc.Update(context.Background(), UpdateInstanceReq{
		InstanceID: seedID, Name: "vm-renamed",
	})
	require.NoError(t, err)
	in := instanceFromOp(t, portmock.AwaitOpDone(t, k.ops, op.ID))
	assert.Equal(t, "vm-renamed", in.Name, "непустое имя при полной правке обязано примениться")
}
