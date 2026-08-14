// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package listener

// Backend-порт слушателя снят с контракта: он живёт на группе целей.
//
// ПОЧЕМУ. Одна величина — один хозяин. Backend-порт задавался ДВАЖДЫ: полем
// самого слушателя и полем группы целей, при том что решение принимало только
// второе. Поле слушателя записывалось, отдавалось обратно и НЕ читалось ничем —
// «принято-и-проигнорировано», что конвенция запрещает прямо. Наблюдаемая
// величина остаётся одна и остаётся на месте: `resolved_backend_port` — эхо
// `TargetGroup.port`.
//
// Хуже того, поле было обязательным на деле и необязательным на словах: ноль
// («не задавал») отвергался проверкой диапазона, поэтому объявленное умолчание —
// «не задан → наследуется порт группы» — не исполнялось ни при каком входе.
// Возможность была объявлена, задокументирована, покрыта типами — и не работала.
//
// Нужен другой backend-порт — ссылайтесь на ДРУГУЮ группу целей: группа дешёвая
// и переиспользуемая, а порт у неё ровно один. Решение зафиксировано приёмкой
// NLB-1b, сценарий NLB-1-25.
//
// ЧТО ЗДЕСЬ ПРОВЕРЯЕТСЯ И ПОЧЕМУ ТРЕМЯ РАЗНЫМИ УТВЕРЖДЕНИЯМИ.
//
//  1. Поля НЕТ в дескрипторе, а имя и номер стоят в `reserved`. Без второй
//     половины номер переиспользуют, и старый клиент получит на том же теге поле
//     с другим смыслом.
//  2. Создание БЕЗ этого поля проходит. Это предикат снятия из задачи, и он не
//     выводится из первого утверждения: дескриптор молчит о том, что делает
//     проверка входа.
//  3. Путь маски `target_port` отвергается, и отвергается как НЕИЗВЕСТНЫЙ — а не
//     как «неизменяемый». Прежняя редакция держала его в перечне неизменяемых
//     ОТДЕЛЬНОЙ строкой, и снятие поля само по себе её не убирает. Рядом —
//     положительный контроль: законный путь проходит, иначе отрицание зеленело
//     бы на слушателе, переставшем принимать изменения вовсе.

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// retiredTargetPortField — имя, снятое с контракта. Одно место на весь файл.
const retiredTargetPortField = "target_port"

// TestListener_TargetPort_RetiredFromContract — ни ресурс, ни запрос создания
// поля больше не несут, и в обоих имя зарезервировано.
//
// `UpdateListenerRequest` в перечне нет намеренно: этого поля у него не было
// никогда, поэтому требовать от него `reserved` значило бы требовать брони под
// то, чего он не выставлял, — послабление без предмета с зеркальной стороны.
func TestListener_TargetPort_RetiredFromContract(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		md   protoreflect.MessageDescriptor
	}{
		{"Listener", (&lbv1.Listener{}).ProtoReflect().Descriptor()},
		{"CreateListenerRequest", (&lbv1.CreateListenerRequest{}).ProtoReflect().Descriptor()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fields := tc.md.Fields()
			for i := range fields.Len() {
				require.NotEqual(t, retiredTargetPortField, string(fields.Get(i).Name()),
					"%s всё ещё несёт снятое поле: backend-порт задавался бы двумя "+
						"полями сразу, а решает по-прежнему одно", tc.name)
			}
			require.True(t, tc.md.ReservedNames().Has(protoreflect.Name(retiredTargetPortField)),
				"%s: имя снятого поля обязано стоять в reserved — иначе его "+
					"переиспользуют, и старый клиент получит поле с другим смыслом", tc.name)
		})
	}
}

// TestCreateListener_SucceedsWithoutTargetPort — предикат снятия из задачи:
// запрос без backend-порта создаёт слушателя.
//
// Прежде такой запрос отвергался, и отказ называл поле `port` — то, которое
// вызывающий как раз задал верно. Порознь каждый из двух дефектов стоил бы
// строки в документации; вместе они давали отладку с непонятным исходом.
func TestCreateListener_SucceedsWithoutTargetPort(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	ops := newFakeOpsRepo()
	lb := seedParentLB(t, repo)
	uc := newCreateUC(repo, ops)

	op, err := uc.Run(contextWithSubject("user:test-actor"), &lbv1.CreateListenerRequest{
		LoadBalancerId: string(lb.ID),
		Name:           "no-backend-port",
		Protocol:       lbv1.Listener_TCP,
		Port:           80,
	})
	require.NoError(t, err,
		"backend-порт слушателю задавать нечем и не нужно: он живёт на группе целей")
	final := awaitOpDone(t, ops, op.ID, testTimeout)
	require.Nil(t, final.Error)

	got := listenerByLB(repo, string(lb.ID))
	require.Len(t, got, 1)
	require.Equal(t, domain.LbPort(80), got[0].Port,
		"фронтенд-порт остаётся тем, что прислали")
}

// TestUpdateListener_TargetPortMaskPathIsUnknown — путь маски отвергается как
// НЕИЗВЕСТНЫЙ, а законный путь по-прежнему проходит.
//
// Отдельное утверждение, потому что снятие поля с контракта не вынимает его имя
// из перечня неизменяемых: пока строка там стоит, вызывающий слышит «поле
// неизменяемо после создания» о поле, которого нет, — то есть ему сообщают, что
// величина существует и её просто нельзя менять.
func TestUpdateListener_TargetPortMaskPathIsUnknown(t *testing.T) {
	t.Parallel()
	suite := newUpdateSuite(t)

	_, err := suite.uc.Run(context.Background(), &lbv1.UpdateListenerRequest{
		ListenerId: string(suite.listener.ID),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{retiredTargetPortField}},
	})
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	msg := status.Convert(err).Message()
	require.True(t, strings.Contains(msg, retiredTargetPortField),
		"отказ обязан НАЗВАТЬ путь, иначе вызывающий не узнает, что именно снято: %q", msg)
	require.False(t, strings.Contains(msg, "immutable"),
		"снятое поле не «неизменяемо», а неизвестно: первая формулировка утверждает, "+
			"что величина есть и её нельзя менять: %q", msg)

	// Положительный контроль — та же форма запроса на законном пути.
	op, okErr := suite.uc.Run(context.Background(), &lbv1.UpdateListenerRequest{
		ListenerId: string(suite.listener.ID),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"name"}},
		Name:       "positive-control",
	})
	require.NoError(t, okErr, "законный путь маски обязан проходить — иначе отрицание выше беспредметно")
	require.NotEmpty(t, op.ID)
}
