// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package dataplane

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/PRO-Robotech/kacho/pkg/ids"
)

// intent_carries_no_apply_state_test.go — поток намерения НЕ несёт состояния
// применения (APPLY-20).
//
// # Предмет
//
// Состояние применения — это отчёт САМОГО исполнителя, сведённый к двум фактам.
// Вернув его исполнителю обратно в потоке намерения, платформа завела бы второй
// источник одного факта и замкнула бы петлю: исполнитель, читающий из потока
// собственное «не применено», не может отличить его от утверждения платформы.
//
// # Почему это надо утверждать, а не считать очевидным
//
// Поле живёт на СООБЩЕНИИ РЕСУРСА, а поток несёт те же сообщения. То есть
// «не заполняется» здесь — не свойство типа, а свойство пути: достаточно одному
// будущему автору позвать заполнитель в сборщике потока, и петля замкнётся, ничего
// не сломав по типам.
//
// # Положительный контроль обязателен
//
// Без него «состояния нет» было бы верно и на пустом теле: тело собрано и
// содержит идентификатор — значит утверждение относится к заполненному телу, а
// не к отсутствию тела.

// TestIntentStreamCarriesNoApplyState — у каждого вида объекта тело намерения
// приходит без состояния применения.
func TestIntentStreamCarriesNoApplyState(t *testing.T) {
	require.NotEmpty(t, KnownKinds, "перечень видов пуст — проверять нечего")

	checked := 0
	for _, kind := range KnownKinds {
		t.Run(string(kind), func(t *testing.T) {
			id := ids.NewID("dpi")
			msg, err := intentMessage(liveRowOfKind(t, kind, 7, id))
			require.NoError(t, err)

			body := oneofBody(t, msg)

			// ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: тело собрано, а не пусто.
			idField := body.Descriptor().Fields().ByName("id")
			require.NotNil(t, idField, "у тела вида %s нет поля идентичности — проверяется не то", kind)
			assert.Equal(t, id, body.Get(idField).String(),
				"тело намерения не собрано: «состояния нет» относилось бы к пустоте")

			fd := body.Descriptor().Fields().ByName("apply_state")
			require.NotNil(t, fd,
				"у сообщения ресурса нет поля состояния применения — предпосылка пробы ложна, "+
					"и «поток его не несёт» стало бы утверждением ни о чём")
			assert.False(t, body.Has(fd),
				"поток намерения вернул исполнителю его же отчёт: второй источник одного факта")
			checked++
		})
	}
	t.Logf("осмотрено %d вид(ов) объекта намерения", checked)
}

// oneofBody достаёт сообщение ресурса из выбранной ветви намерения.
//
// У сети ветвь несёт обёртку с координатой изоляции, поэтому ресурс лежит на
// уровень глубже; у остальных видов ресурс и есть тело ветви.
func oneofBody(t *testing.T, msg protoreflect.ProtoMessage) protoreflect.Message {
	t.Helper()
	m := msg.ProtoReflect()
	od := m.Descriptor().Oneofs().ByName("object")
	require.NotNil(t, od, "в контракте нет ветвления по объекту")
	fd := m.WhichOneof(od)
	require.NotNil(t, fd, "ветвь объекта не выбрана")

	body := m.Get(fd).Message()
	if inner := body.Descriptor().Fields().ByName("network"); inner != nil &&
		body.Descriptor().Name() == "NetworkIntent" {
		return body.Get(inner).Message()
	}
	return body
}
