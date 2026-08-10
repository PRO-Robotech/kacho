// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package handler

// listener_scope_test.go — рубеж «какой это слушатель» проверяется НА ТОМ ПУТИ,
// которым его исполняет процесс: через регистратор, который композиционный корень
// отдаёт носителю контура.
//
// # Почему одной пробы на функцию было недостаточно
//
// Соседний `tenant_admin_listener_test.go` спрашивает [tenantFromMetadata] и
// интерсепторы напрямую: он утверждает, что функция, ПОЛУЧИВ «публичный», не
// читает authz-несущих заголовков. Он остаётся верным и в тот момент, когда
// проводка начинает передавать ей «внутренний» на публичной стороне, — то есть не
// видит ровно того дефекта, который и был снят первым кругом перевода: параметр,
// различающий слушатель, тогда удалили, и `x-kacho-admin` стал признаваться у
// любого доверенного пира.
//
// Здесь предмет другой: ЧТО ПЕРЕДАЁТ ПРОВОДКА. Обработчик регистрируется тем же
// сгенерированным регистратором, что и в бою, оборачивается тем же
// [PublicRegistrar]/[InternalRegistrar], и наблюдается то, что видит обработчик.
// Подставного описания службы здесь нет намеренно: собранное руками, оно
// повторяло бы форму сгенерированного и разошлось бы с ним молча.

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	computev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"
)

// recordingMachineTypes — настоящий сервер сгенерированной службы, который
// записывает личность, дошедшую до обработчика, и ничего больше не делает.
type recordingMachineTypes struct {
	computev1.UnimplementedMachineTypeServiceServer
	seen   TenantCtx
	called bool
}

func (r *recordingMachineTypes) Get(ctx context.Context, _ *computev1.GetMachineTypeRequest) (*computev1.MachineType, error) {
	r.seen = TenantFromCtx(ctx)
	r.called = true
	return &computev1.MachineType{}, nil
}

// captured — регистратор, запоминающий описание службы, которое ему отдали.
// Сервера здесь нет: носитель отдаёт сервису `grpc.ServiceRegistrar` — интерфейс
// с единственным методом, — и проба стоит на том же интерфейсе.
type captured struct {
	desc *grpc.ServiceDesc
	impl any
}

func (c *captured) RegisterService(sd *grpc.ServiceDesc, impl any) { c.desc, c.impl = sd, impl }

// invokeGet прогоняет `MachineTypeService/Get` через зарегистрированное описание
// службы — то есть через тот же сгенерированный обработчик, что и в бою.
//
// Цепочки носителя здесь нет (её предмет — извлечение личности, и оно у носителя
// своё), поэтому вместо неё передаётся `nil`: рубеж слушателя обязан исполниться
// и в этом случае, а не исчезнуть вместе с цепочкой.
func invokeGet(t *testing.T, wrap func(grpc.ServiceRegistrar) grpc.ServiceRegistrar, ctx context.Context) (TenantCtx, bool, error) {
	t.Helper()
	cap := &captured{}
	rec := &recordingMachineTypes{}
	computev1.RegisterMachineTypeServiceServer(wrap(cap), rec)
	require.NotNil(t, cap.desc, "регистратор не получил описания службы — проба ничего не прогнала бы")

	var get *grpc.MethodDesc
	for i := range cap.desc.Methods {
		if cap.desc.Methods[i].MethodName == "Get" {
			get = &cap.desc.Methods[i]
		}
	}
	require.NotNil(t, get, "в описании службы нет метода Get")

	_, err := get.Handler(cap.impl, ctx,
		func(v any) error { return nil },
		nil,
	)
	return rec.seen, rec.called, err
}

// TestPublicRegistrarNeverHonoursTheAdminHeader — РУБЕЖ: подложенный
// `x-kacho-admin` от ДОВЕРЕННОГО отправителя не поднимает признака администратора
// на публичной стороне, и подложенная проектная область не подставляется.
//
// Доверие здесь установлено намеренно: мост REST→gRPC края форвардит любой
// `Grpc-Metadata-*`, поэтому подлог приезжает как метадата доверенного пира, и
// проверка доверия на этом векторе бесполезна by construction.
func TestPublicRegistrarNeverHonoursTheAdminHeader(t *testing.T) {
	seen, called, err := invokeGet(t, func(r grpc.ServiceRegistrar) grpc.ServiceRegistrar {
		return PublicRegistrar(r, false)
	}, forgedAdminCtx())

	require.NoError(t, err)
	require.True(t, called, "обработчик не был вызван — наблюдать было нечего")
	assert.False(t, seen.Admin,
		"публичный слушатель поднял признак администратора по клиентскому заголовку: "+
			"предикат владения операцией снимается тем, что приносит сам вызывающий")
	assert.Empty(t, seen.ProjectIDs,
		"публичный слушатель подставил проектную область из клиентского заголовка")
	assert.True(t, seen.IsAnonymous(),
		"подложенные заголовки области удовлетворили бы боевой гейт аутентификации без единого удостоверения")
	assert.Equal(t, "attacker", seen.Actor,
		"x-kacho-actor остаётся аудит-полем и читается — рубеж сужен до authz-несущих ключей")
}

// TestInternalRegistrarStillHonoursTheAdminHeader — ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ к
// предыдущей пробе.
//
// Без него отрицание зеленело бы на самом сломанном состоянии: если бы проводка
// перестала передавать личность вовсе, «на публичном нет администратора» осталось
// бы верным, а админ-путь молча умер бы. Разные конструкторы обязаны давать РАЗНЫЙ
// исход — иначе различия слушателей не существует.
func TestInternalRegistrarStillHonoursTheAdminHeader(t *testing.T) {
	seen, called, err := invokeGet(t, func(r grpc.ServiceRegistrar) grpc.ServiceRegistrar {
		return InternalRegistrar(r, false)
	}, forgedAdminCtx())

	require.NoError(t, err)
	require.True(t, called, "обработчик не был вызван — наблюдать было нечего")
	assert.True(t, seen.Admin,
		"внутренний слушатель перестал признавать x-kacho-admin от доверенного пира — "+
			"админ-путь закрыт, и предыдущая проба зеленела бы на этом")
	assert.Contains(t, seen.ProjectIDs, "prj-victim",
		"внутренний слушатель перестал признавать x-kacho-project-id от доверенного пира")
}

// TestPublicRegistrarDropsHeadersFromAnUntrustedPeer — второй, независимый вектор:
// пир, дотянувшийся до слушателя в обход края, не получает ничего даже на
// внутренней стороне.
//
// Два условия рубежа независимы, и проба именно на это: доверие отвечает на вопрос
// «кто со мной говорит по проводу», слушатель — на вопрос «откуда взялось
// значение». Снятие любого из них поодиночке обязано быть видно.
func TestPublicRegistrarDropsHeadersFromAnUntrustedPeer(t *testing.T) {
	md := metadata.New(map[string]string{
		"x-kacho-admin":      "true",
		"x-kacho-project-id": "prj-victim",
	})
	ctx := metadata.NewIncomingContext(context.Background(), md) // без отметки доверия

	for name, wrap := range map[string]func(grpc.ServiceRegistrar) grpc.ServiceRegistrar{
		"public":   func(r grpc.ServiceRegistrar) grpc.ServiceRegistrar { return PublicRegistrar(r, false) },
		"internal": func(r grpc.ServiceRegistrar) grpc.ServiceRegistrar { return InternalRegistrar(r, false) },
	} {
		t.Run(name, func(t *testing.T) {
			seen, called, err := invokeGet(t, wrap, ctx)
			require.NoError(t, err)
			require.True(t, called)
			assert.False(t, seen.Admin, "заголовки недоверенного пира приняты как личность")
			assert.Empty(t, seen.ProjectIDs, "проектная область недоверенного пира подставлена")
		})
	}
}

// TestRegistrarDoesNotMutateTheSharedServiceDesc — описание службы у
// сгенерированного кода ОДНО на всех, кто его регистрирует.
//
// Правка на месте надела бы рубеж одного слушателя на оба — то есть ровно сняла бы
// различие, ради которого существует этот файл, и сняла бы его молча: обе
// регистрации продолжали бы проходить.
func TestRegistrarDoesNotMutateTheSharedServiceDesc(t *testing.T) {
	original := computev1.MachineTypeService_ServiceDesc.Methods[0].Handler

	pub := &captured{}
	computev1.RegisterMachineTypeServiceServer(PublicRegistrar(pub, false), &recordingMachineTypes{})

	require.NotNil(t, computev1.MachineTypeService_ServiceDesc.Methods[0].Handler)
	assert.Equal(t,
		valuePointer(original),
		valuePointer(computev1.MachineTypeService_ServiceDesc.Methods[0].Handler),
		"пакетное описание службы переписано обёрткой: следующий регистратор получил бы рубеж "+
			"чужого слушателя")
	assert.NotEqual(t, valuePointer(original), valuePointer(pub.desc.Methods[0].Handler),
		"обёртка не подменила обработчик — рубеж слушателя не исполнялся бы вовсе")
}

// valuePointer — адрес функции как сравнимая величина. Функции в Go напрямую не
// сравниваются, поэтому сравнение идёт по указателю на код: это ровно то, что
// нужно вопросу «тот же это обработчик или подменённый».
func valuePointer(f any) uintptr {
	return reflect.ValueOf(f).Pointer()
}
