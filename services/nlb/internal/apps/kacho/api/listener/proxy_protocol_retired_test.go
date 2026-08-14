// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package listener

// Обрамление PROXY-протокола снято с контракта слушателя.
//
// ПОЧЕМУ. Заголовок этого протокола по его собственной спецификации уходит ДО
// любых данных соединения — значит вставить его может только тот, кто владеет
// байтовым потоком к бекенду. Балансировщик четвёртого уровня потоком не владеет,
// он его пересылает; у рассмотренного программного датаплейна такого обрамления
// нет вовсе. То есть исполнителя не существует ни в одном из рассмотренных
// вариантов, а поле принималось — «принято-и-проигнорировано», что конвенция
// запрещает прямо. Решение и предикат его пересмотра (появление слушателя нового
// вида, владеющего соединением) записаны в плане датаплейна §7.11.
//
// ЧТО ЗДЕСЬ ПРОВЕРЯЕТСЯ И ПОЧЕМУ ДВУМЯ РАЗНЫМИ УТВЕРЖДЕНИЯМИ.
//
//  1. Поля НЕТ в дескрипторе, а имя и номер стоят в `reserved`. Без второй
//     половины номер переиспользуют, и старый клиент получит поле с другим
//     смыслом на том же теге.
//  2. Путь маски `proxy_protocol_v2` отвергается, и отвергается как НЕИЗВЕСТНЫЙ.
//     Это не дублирование первого: дескриптор молчит о том, что делает известный
//     набор путей use-case'а, а прежняя редакция держала путь в этом наборе
//     ОТДЕЛЬНОЙ строкой. Рядом стоит положительный контроль — законный путь
//     проходит, — иначе отрицание зеленело бы на любом сломанном наборе.

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
)

// retiredProxyProtocolField — имя, снятое с контракта. Одно место на весь файл:
// перечень ниже и утверждения обязаны говорить об одном и том же имени.
const retiredProxyProtocolField = "proxy_protocol_v2"

// TestListener_ProxyProtocolV2_RetiredFromContract — ни одно из трёх сообщений
// контракта слушателя поля больше не несёт, и во всех трёх имя зарезервировано.
//
// Три сообщения, а не одно: ресурс отдаёт значение наружу, запрос создания его
// принимал, запрос изменения — тоже. Снять с одного и оставить в двух значило бы
// оставить вход, у которого нет ни выхода, ни исполнителя.
func TestListener_ProxyProtocolV2_RetiredFromContract(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		md   protoreflect.MessageDescriptor
	}{
		{"Listener", (&lbv1.Listener{}).ProtoReflect().Descriptor()},
		{"CreateListenerRequest", (&lbv1.CreateListenerRequest{}).ProtoReflect().Descriptor()},
		{"UpdateListenerRequest", (&lbv1.UpdateListenerRequest{}).ProtoReflect().Descriptor()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fields := tc.md.Fields()
			for i := range fields.Len() {
				require.NotEqual(t, retiredProxyProtocolField, string(fields.Get(i).Name()),
					"%s всё ещё несёт снятое поле: край его примет, а исполнить его "+
						"не может ни один рассмотренный датаплейн", tc.name)
			}
			require.True(t, tc.md.ReservedNames().Has(protoreflect.Name(retiredProxyProtocolField)),
				"%s: имя снятого поля обязано стоять в reserved — иначе его "+
					"переиспользуют, и старый клиент получит поле с другим смыслом", tc.name)
		})
	}
}

// TestUpdateListener_ProxyProtocolV2_MaskPathIsUnknown — путь маски отвергается
// как неизвестный, а законный путь по-прежнему проходит.
//
// Положительный контроль обязателен: без него «отвергнуто» неотличимо от «набор
// путей пуст», и проба зеленела бы на слушателе, который перестал принимать
// изменения вовсе.
func TestUpdateListener_ProxyProtocolV2_MaskPathIsUnknown(t *testing.T) {
	t.Parallel()
	suite := newUpdateSuite(t)

	_, err := suite.uc.Run(context.Background(), &lbv1.UpdateListenerRequest{
		ListenerId: string(suite.listener.ID),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{retiredProxyProtocolField}},
	})
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.True(t,
		strings.Contains(status.Convert(err).Message(), retiredProxyProtocolField),
		"отказ обязан НАЗВАТЬ путь, иначе вызывающий не узнает, что именно снято: %q",
		status.Convert(err).Message())

	// Положительный контроль — та же форма запроса на законном пути.
	op, okErr := suite.uc.Run(context.Background(), &lbv1.UpdateListenerRequest{
		ListenerId: string(suite.listener.ID),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"name"}},
		Name:       "positive-control",
	})
	require.NoError(t, okErr, "законный путь маски обязан проходить — иначе отрицание выше беспредметно")
	require.NotEmpty(t, op.ID)
}
