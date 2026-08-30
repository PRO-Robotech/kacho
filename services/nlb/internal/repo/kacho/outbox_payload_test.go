// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package kacho

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// Пробы этого файла называют ключи СТРОКОВЫМИ ЛИТЕРАЛАМИ, а не константами,
// которыми строитель их собирает, — и это не многословие, а единственная форма,
// способная упасть.
//
// Прежняя редакция утверждала круговым ходом: строитель собирал нагрузку по
// константе, разборщик читал её по ТОЙ ЖЕ константе, проба сверяла результат
// снова по ней. Такое утверждение истинно by construction при любом значении
// константы. Замер (задача #1452): значение `PayloadKeyOldProjectID` заменено на
// `totally_wrong_key` — все четыре пробы обеих сторон остались ЗЕЛЁНЫМИ. То есть
// имя ключа на проводе не удерживалось ничем.
//
// Разборщик при этом снят: вызывающих в прод-дереве у него не было, а как
// зеркало собственного строителя он и создавал эту круговую истинность.

// TestPayloadKeysAreAssertedOnTheWire — строитель эмитит ровно те имена, под
// которыми нагрузка лежит в журнале, и опускает пустые поля.
func TestPayloadKeysAreAssertedOnTheWire(t *testing.T) {
	m := LifecyclePayload{
		ID:               "nlb-listener-1",
		ParentResourceID: "nlb-1",
		ProjectID:        "prj-b",
		RegionID:         "ru-1",
		Name:             "l1",
		Status:           "ACTIVE",
		Type:             "EXTERNAL",
		Protocol:         "TCP",
		Port:             443,
		Trigger:          "listener_created",
		OldProjectID:     "prj-a",
		NewProjectID:     "prj-b",
	}.Map()

	// Провод целиком: имя слева — литерал, а не константа, которой оно собрано.
	want := map[string]any{
		"id":                 "nlb-listener-1",
		"parent_resource_id": "nlb-1",
		"project_id":         "prj-b",
		"region_id":          "ru-1",
		"name":               "l1",
		"status":             "ACTIVE",
		"type":               "EXTERNAL",
		"protocol":           "TCP",
		"port":               int32(443),
		"trigger":            "listener_created",
		"old_project_id":     "prj-a",
		"new_project_id":     "prj-b",
	}
	for key, wantVal := range want {
		got, ok := m[key]
		if !ok {
			t.Errorf("ключа %q нет на проводе: строитель собрал %v", key, m)
			continue
		}
		if got != wantVal {
			t.Errorf("ключ %q = %v, ожидалось %v", key, got, wantVal)
		}
	}
	// Словарь закрыт в обратную сторону: лишний ключ так же тих, как
	// отсутствующий, — подписчик его просто не увидит.
	for key := range m {
		if _, ok := want[key]; !ok {
			t.Errorf("на проводе ключ %q, которого проба не называет", key)
		}
	}
}

// TestEmptyFieldsAreOmittedFromTheWire — пустое поле в нагрузку не попадает.
//
// Это не косметика: пустая строка на проводе означала бы «значение известно и
// оно пусто», тогда как её отсутствие означает «этот вид ресурса такого поля не
// несёт».
func TestEmptyFieldsAreOmittedFromTheWire(t *testing.T) {
	m := LifecyclePayload{ID: "nlb-1", ProjectID: "prj-b"}.Map()
	for _, key := range []string{
		"parent_resource_id", "region_id", "name", "status", "type",
		"protocol", "trigger", "old_project_id", "new_project_id", "port",
	} {
		if _, ok := m[key]; ok {
			t.Errorf("пустое поле уехало на провод ключом %q: %v", key, m)
		}
	}
	if len(m) != 2 {
		t.Errorf("на проводе %d ключей, ожидалось 2: %v", len(m), m)
	}
}

// TestLegacyKeyNamesAreGone — имена, которые писали прежние строители
// (`load_balancer_id`, `src_project_id`), на провод не возвращаются.
func TestLegacyKeyNamesAreGone(t *testing.T) {
	m := LifecyclePayload{
		ID: "nlb-1", ParentResourceID: "nlb-parent",
		OldProjectID: "prj-a", NewProjectID: "prj-b",
	}.Map()
	for _, legacy := range []string{"load_balancer_id", "src_project_id", "dst_project_id"} {
		if _, ok := m[legacy]; ok {
			t.Errorf("прежнее имя %q вернулось на провод: %v", legacy, m)
		}
	}
}

// Пробы строителя нагрузки слушателя переехали сюда ВМЕСТЕ со своим предметом:
// строитель живёт теперь в этом пакете, потому что его зовут ДВА пакета use-case
// (#1549). Проба, оставленная там, где предмета больше нет, утверждала бы о
// функции, которой в её пакете не осталось.
// TestListenerStatePayload_NilGuard — nil input returns nil.
func TestListenerStatePayload_NilGuard(t *testing.T) {
	t.Parallel()
	require.Nil(t, ListenerStatePayload(nil))
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
