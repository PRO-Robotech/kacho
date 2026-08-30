// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package kacho

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// # ТРИ ПРОБЫ СНЯТЫ ВМЕСТЕ СО СВОИМ ПРЕДМЕТОМ (#1551)
//
// Здесь стояли `TestPayloadKeysAreAssertedOnTheWire`,
// `TestEmptyFieldsAreOmittedFromTheWire` и `TestLegacyKeyNamesAreGone` — они
// утверждали имена ключей МИНИМАЛЬНОГО СНИМКА (`LifecyclePayload`) по проводу.
// Снимка больше нет: последний его писатель — балансировщик — перешёл на конверт
// полного состояния, и у типа не осталось ни одного вызывающего.
//
// Снято ВМЕСТЕ с предметом, а не ослаблено: проба, у которой исчез вход, не
// краснеет и не зеленеет — она молчит, продолжая считаться исполненной. Форма её
// утверждения («ключи называются литералами, а не константами, которыми
// собраны») остаётся нормой и живёт в пробах конвертов ниже.

// Пробы строителя нагрузки слушателя переехали сюда ВМЕСТЕ со своим предметом:
// строитель живёт теперь в этом пакете, потому что его зовут ДВА пакета use-case
// (#1549). Проба, оставленная там, где предмета больше нет, утверждала бы о
// функции, которой в её пакете не осталось.
// TestListenerStatePayload_NilGuard — nil input returns nil.
func TestListenerStatePayload_NilGuard(t *testing.T) {
	t.Parallel()
	require.Nil(t, ListenerStatePayload(nil))
}

// TestLoadBalancerStatePayload_NilGuard — та же проба у строителя балансировщика.
//
// Она переехала сюда ВМЕСТЕ со своим предметом: строитель живёт теперь в этом
// пакете, потому что его зовут ДВА пакета use-case. Проба, оставленная там, где
// функции больше нет, утверждала бы о ней в чужом пакете.
func TestLoadBalancerStatePayload_NilGuard(t *testing.T) {
	t.Parallel()
	require.Nil(t, LoadBalancerStatePayload(nil))
}

// TestLoadBalancerStatePayloadCarriesTheRowUnderTheEnvelope — строитель
// балансировщика кладёт СТРОКУ ТАБЛИЦЫ под конвертом полного состояния, и ключи
// у неё — ИМЕНА КОЛОНОК.
//
// Утверждение сделано ПО ПРОВОДУ — через настоящий JSON, а не через читателя,
// собранный из тех же тегов: круговой ход был бы истинен при любом их значении.
// Названы литералами те ключи, потеря которых ТИХА: метки (клиентский отбор
// остался бы без источника), административное состояние и набор групп
// безопасности (поля, которых у прежней, минимальной формы не было вовсе).
//
// Полный словарь провода утверждает не эта проба, а
// `TestJournalPayloadSpeaksTheColumnVocabulary` в пакете `pg`: он сверяет ключи
// со списком колонок, которым репозиторий читает строку, — то есть с деревом, а
// не с выписанным здесь перечнем.
func TestLoadBalancerStatePayloadCarriesTheRowUnderTheEnvelope(t *testing.T) {
	t.Parallel()
	rec := &LoadBalancerRecord{}
	rec.ID = domain.ResourceID("nlb-1234567890abcdef")
	rec.ProjectID = domain.ProjectID("prj-1234567890abcdef")
	rec.Name = domain.LbName("front")
	rec.Labels = domain.LabelsFromMap(map[string]string{"env": "prod"})
	rec.AdminState = domain.AdminStateDisabled
	rec.SecurityGroupIDs = []string{"sg-1234567890abcdef"}

	raw, err := json.Marshal(LoadBalancerStatePayload(rec))
	require.NoError(t, err)

	var wire map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &wire))
	body, ok := wire["state"]
	require.True(t, ok,
		"нагрузка не несёт конверта полного состояния — сборщик журнала обязан отличать её "+
			"от строк прежней, минимальной формы, а по удаче разбора этого не сделать")

	var fields map[string]any
	require.NoError(t, json.Unmarshal(body, &fields))
	require.Equal(t, "nlb-1234567890abcdef", fields["id"])
	require.Equal(t, "prj-1234567890abcdef", fields["project_id"])
	require.Equal(t, "front", fields["name"])
	require.Equal(t, map[string]any{"env": "prod"}, fields["labels"],
		"метки на проводе обязаны быть ОБЪЕКТОМ: строка триггера кладёт их `to_jsonb`, "+
			"и массив пар она разобрать не сможет — потеря будет тихой")
	require.Equal(t, "DISABLED", fields["admin_state"])
	require.Equal(t, []any{"sg-1234567890abcdef"}, fields["security_group_ids"])

	// Обратный ход: читатель собирает ТУ ЖЕ запись. Утверждается он отдельно от
	// имён ключей — иначе одна проба зеленела бы на согласованной ошибке обеих
	// сторон.
	back, err := LoadBalancerStateFromPayload(raw)
	require.NoError(t, err)
	require.NotNil(t, back)
	require.Equal(t, map[string]string{"env": "prod"}, domain.LabelsToMap(back.Labels))
	require.Equal(t, domain.AdminStateDisabled, back.AdminState)
	require.Equal(t, []string{"sg-1234567890abcdef"}, back.SecurityGroupIDs)
}

// TestListenerStatePayloadCarriesTheWholeRecordUnderTheEnvelope — строитель
// нагрузки слушателя кладёт ЗАПИСЬ ЦЕЛИКОМ, под конвертом полного состояния.
//
// Здесь стояла проба про имя ключа `parent_resource_id` — предмет её исчез
// вместе с минимальным снимком: форма нагрузки заменена, а не дополнена (задача
// #1381). Ослабить её было нельзя, поэтому она ЗАМЕНЕНА утверждением о новой
// форме, и утверждение сделано ПО ПРОВОДУ — через настоящий JSON, а не через
// разборщик, собранный из тех же констант: круговой ход был бы истинен при любом
// их значении.
//
// Метки названы отдельно: клиентский отбор по меткам берёт источник ровно
// отсюда, и потеря их в кодировании оставила бы его без источника МОЛЧА.
func TestListenerStatePayloadCarriesTheWholeRecordUnderTheEnvelope(t *testing.T) {
	t.Parallel()
	rec := &ListenerRecord{}
	rec.ID = domain.ResourceID("nlb-listener-1")
	rec.LoadBalancerID = domain.ResourceID("nlb-1")
	rec.ProjectID = domain.ProjectID("prj-b")
	rec.Name = domain.LbName("front")
	rec.Labels = domain.LabelsFromMap(map[string]string{"env": "prod"})
	rec.Protocol = domain.ProtoTCP
	rec.Port = domain.LbPort(443)
	rec.Status = domain.ListenerStatusActive

	raw, err := json.Marshal(ListenerStatePayload(rec))
	require.NoError(t, err)

	var wire struct {
		State *ListenerRecord `json:"state"`
	}
	require.NoError(t, json.Unmarshal(raw, &wire))
	require.NotNil(t, wire.State,
		"нагрузка не несёт конверта полного состояния — сборщик журнала обязан отличать её "+
			"от строк прежней, минимальной формы, а по удаче разбора этого не сделать")
	require.Equal(t, "nlb-listener-1", string(wire.State.ID))
	require.Equal(t, "prj-b", string(wire.State.ProjectID))
	require.Equal(t, "nlb-1", string(wire.State.LoadBalancerID))
	require.Equal(t, map[string]string{"env": "prod"}, domain.LabelsToMap(wire.State.Labels),
		"метки не пережили круг — клиентский отбор по меткам остался бы без источника")
	require.Equal(t, domain.ProtoTCP, wire.State.Protocol)
	require.Equal(t, domain.LbPort(443), wire.State.Port)
	require.Equal(t, domain.ListenerStatusActive, wire.State.Status)
}
