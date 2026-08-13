// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// ---- поля, снятые с контракта ----------------------------------------------------------

// Снятое с контракта поле обязано исчезнуть и из схемы.
//
// Оставленное, оно хуже отсутствующего: пользователь задаёт размер блока, план принимает
// его без возражений, а край отвергает запрос как несущий неизвестное поле — то есть
// провайдер обещает возможность, которой в продукте нет.
//
// Проба перечисляет ОБЕ стороны сразу: снятое отсутствует, а поля, которые контракт несёт,
// на месте. Без второй половины она зеленела бы и на схеме, где нет ничего.
func TestVolumeSchemaFollowsTheContract(t *testing.T) {
	attrs := flatSchemaOf(t, storageVolumeSpec).Attributes

	for _, gone := range []string{"block_size"} {
		if _, ok := attrs[gone]; ok {
			t.Errorf("атрибут %s остался в схеме, хотя контракт его не несёт: край отвергнет "+
				"запрос с ним, а план примет — обещание без исполнителя", gone)
		}
	}
	for _, want := range []string{
		"zone_id", "disk_type_id", "size_bytes", "source_snapshot_id", "source_image_id",
		"used_bytes", "status", "status_reason", "updated_at", "created_at",
	} {
		if _, ok := attrs[want]; !ok {
			t.Errorf("атрибут %s контракт несёт, а схема — нет", want)
		}
	}
}

// Снимок и образ несут второй источник — тот, которым создаётся копия.
func TestSnapshotAndImageDeclareTheirCopySource(t *testing.T) {
	snap := flatSchemaOf(t, storageSnapshotSpec).Attributes
	for _, want := range []string{"source_volume_id", "source_snapshot_id", "zone_id", "size_bytes"} {
		if _, ok := snap[want]; !ok {
			t.Errorf("у снимка нет атрибута %s", want)
		}
	}
	// Источник тома у снимка перестал быть обязательным: копия его не задаёт вовсе.
	// Обязательным он остался бы ровно до первой попытки описать копию.
	if a, ok := snap["source_volume_id"].(schema.StringAttribute); ok && a.Required {
		t.Error("source_volume_id у снимка объявлен обязательным — копию описать нечем")
	}

	img := flatSchemaOf(t, storageImageSpec).Attributes
	for _, want := range []string{"source_snapshot_id", "source_volume_id", "source_image_id",
		"placement_type", "size_bytes", "min_disk_bytes", "format"} {
		if _, ok := img[want]; !ok {
			t.Errorf("у образа нет атрибута %s", want)
		}
	}
}

// ---- класс диска: перенос данных, а не пересоздание -------------------------------------

// Смена класса диска НЕ пересоздаёт том.
//
// Край переносит данные собственным глаголом; пересоздание на том же намерении означало бы
// потерю данных на операции, которую край делает без потерь. Парный контроль — зона: она
// действительно неизменяема, и пересоздание на ней обязано остаться.
func TestDiskTypeChangeDoesNotReplaceTheVolumeWhileZoneStillDoes(t *testing.T) {
	attrs := flatSchemaOf(t, storageVolumeSpec).Attributes

	if replacesOnChange(t, attrs["disk_type_id"], "network-ssd", "network-hdd") {
		t.Error("смена класса диска требует пересоздания тома — это потеря данных там, " +
			"где у края есть перенос")
	}
	if !replacesOnChange(t, attrs["zone_id"], "ru-central1-a", "ru-central1-b") {
		t.Error("смена зоны НЕ требует пересоздания — зона неизменяема, и молчаливое " +
			"изменение здесь означало бы, что проба выше зеленеет на схеме без модификаторов вовсе")
	}
}

// Класс диска меняется своим глаголом и в маску изменения не попадает.
//
// Маска и глагол — разные запросы края. Класс, попавший в маску, отказал бы утверждением
// «в контракте нет поля» — то есть правка имени рядом с ним перестала бы применяться тоже.
func TestDiskTypeChangeGoesToItsVerbAndNameStaysInTheMask(t *testing.T) {
	edge := newEdge(t)
	edge.get("/storage/v1/volumes/vol1", volumeJSON("network-hdd", "renamed"))

	r := &flatResource{spec: storageVolumeSpec, c: mustProviderClient(t, edge.URL())}
	sch := flatSchemaOf(t, storageVolumeSpec)

	state := stateOf(t, sch, map[string]any{
		"id": "vol1", "project_id": "prj1", "name": "before", "zone_id": "ru-central1-a",
		"disk_type_id": "network-ssd", "size_bytes": int64(10),
	})
	plan := planOf(t, sch, map[string]any{
		"id": "vol1", "project_id": "prj1", "name": "renamed", "zone_id": "ru-central1-a",
		"disk_type_id": "network-hdd", "size_bytes": int64(10),
	})

	resp := &resource.UpdateResponse{State: tfsdk.State{Schema: sch, Raw: nullObject(t, sch)}}
	r.Update(context.Background(), resource.UpdateRequest{Plan: plan, State: state}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("изменение отвергнуто: %v", resp.Diagnostics.Errors())
	}

	verb := edge.calls(http.MethodPost, "/storage/v1/volumes/vol1:changeDiskType")
	if len(verb) != 1 {
		t.Fatalf("вызовов глагола смены класса: %d, ожидался 1", len(verb))
	}
	if got := jsonField(t, verb[0], "diskTypeId"); got != "network-hdd" {
		t.Errorf("глагол не назвал новый класс: %v", got)
	}

	patch := edge.calls(http.MethodPatch, "/storage/v1/volumes/vol1")
	if len(patch) != 1 {
		t.Fatalf("правок маской: %d, ожидалась 1 (имя)", len(patch))
	}
	mask, _ := jsonField(t, patch[0], "updateMask").(string)
	if !strings.Contains(mask, "name") {
		t.Errorf("имя не попало в маску изменения: %q", mask)
	}
	if strings.Contains(mask, "disk_type_id") || strings.Contains(mask, "diskTypeId") {
		t.Errorf("класс диска попал в маску изменения (%q) — запрос изменения такого поля "+
			"не несёт, и правка имени рядом с ним не применилась бы вовсе", mask)
	}

	// Порядок несущий: среди полей маски есть НЕОБРАТИМОЕ (размер тома растёт и не
	// уменьшается). Отказ глагола после применённой маски оставил бы вызывающего с
	// изменением, которого он отдельно не просил и которого ему не отменить.
	got := edge.mutations()
	want := []string{
		"POST /storage/v1/volumes/vol1:changeDiskType",
		"PATCH /storage/v1/volumes/vol1",
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("порядок мутаций: %v, ожидался %v", got, want)
	}
}

// Зеркальная проба: правка ОДНОГО имени не зовёт глагол смены класса.
//
// Без неё проба выше зеленела бы и на провайдере, который зовёт глагол при каждом
// изменении — то есть переносит данные на правке метки.
func TestNameOnlyChangeDoesNotTouchTheDiskType(t *testing.T) {
	edge := newEdge(t)
	edge.get("/storage/v1/volumes/vol1", volumeJSON("network-ssd", "renamed"))

	r := &flatResource{spec: storageVolumeSpec, c: mustProviderClient(t, edge.URL())}
	sch := flatSchemaOf(t, storageVolumeSpec)
	base := map[string]any{
		"id": "vol1", "project_id": "prj1", "name": "before", "zone_id": "ru-central1-a",
		"disk_type_id": "network-ssd", "size_bytes": int64(10),
	}
	state := stateOf(t, sch, base)
	renamed := map[string]any{}
	for k, v := range base {
		renamed[k] = v
	}
	renamed["name"] = "renamed"

	resp := &resource.UpdateResponse{State: tfsdk.State{Schema: sch, Raw: nullObject(t, sch)}}
	r.Update(context.Background(), resource.UpdateRequest{Plan: planOf(t, sch, renamed), State: state}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("изменение отвергнуто: %v", resp.Diagnostics.Errors())
	}
	if n := len(edge.calls(http.MethodPost, "/storage/v1/volumes/vol1:changeDiskType")); n != 0 {
		t.Fatalf("глагол смены класса позван %d раз на правке одного имени — это перенос "+
			"данных без повода", n)
	}
}

// Поле, меняющееся своим глаголом, нельзя СНЯТЬ: у операции края нет исхода «никакой».
//
// Проба гоняется на собственном описании, где такое поле необязательно, — иначе ветка
// осталась бы недостижимой, а недостижимая ветка не проверяет ничего.
func TestVerbFieldCannotBeUnset(t *testing.T) {
	spec := storageVolumeSpec
	spec.fields = append([]fieldSpec{}, spec.fields...)
	for i := range spec.fields {
		if spec.fields[i].name == "disk_type_id" {
			spec.fields[i].required = false
		}
	}

	edge := newEdge(t)
	edge.get("/storage/v1/volumes/vol1", volumeJSON("network-ssd", "n"))
	r := &flatResource{spec: spec, c: mustProviderClient(t, edge.URL())}
	sch := flatSchemaOf(t, spec)

	state := stateOf(t, sch, map[string]any{
		"id": "vol1", "project_id": "prj1", "name": "n", "zone_id": "z",
		"disk_type_id": "network-ssd", "size_bytes": int64(10),
	})
	plan := planOf(t, sch, map[string]any{
		"id": "vol1", "project_id": "prj1", "name": "n", "zone_id": "z", "size_bytes": int64(10),
	})

	resp := &resource.UpdateResponse{State: tfsdk.State{Schema: sch, Raw: nullObject(t, sch)}}
	r.Update(context.Background(), resource.UpdateRequest{Plan: plan, State: state}, resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("снятие класса диска принято молча — вызывающий убрал значение из настройки " +
			"и остался бы уверен, что оно снято")
	}
	if !strings.Contains(diagText(resp.Diagnostics.Errors()), "disk_type_id") {
		t.Errorf("отказ не называет атрибут: %v", resp.Diagnostics.Errors())
	}
}

// ---- копия ------------------------------------------------------------------------------

// Копия снимка идёт СВОИМ глаголом края, а идентификатор берётся из его метаданных.
func TestSnapshotCopyUsesTheCopyVerb(t *testing.T) {
	edge := newEdge(t)
	edge.operation("/storage/v1/snapshots/snpSRC:copy", map[string]any{"snapshotId": "snpNEW"})
	edge.get("/storage/v1/snapshots/snpNEW", snapshotJSON("snpNEW", "ru-central1-b", ""))

	sch := flatSchemaOf(t, storageSnapshotSpec)
	r := &flatResource{spec: storageSnapshotSpec, c: mustProviderClient(t, edge.URL())}
	plan := planOf(t, sch, map[string]any{
		"project_id": "prj1", "name": "nightly-b",
		"source_snapshot_id": "snpSRC", "zone_id": "ru-central1-b",
	})

	resp := &resource.CreateResponse{State: tfsdk.State{Schema: sch, Raw: nullObject(t, sch)}}
	r.Create(context.Background(), resource.CreateRequest{Plan: plan}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("создание копии отвергнуто: %v", resp.Diagnostics.Errors())
	}

	body := edge.calls(http.MethodPost, "/storage/v1/snapshots/snpSRC:copy")
	if len(body) != 1 {
		t.Fatalf("вызовов глагола копии: %d, ожидался 1", len(body))
	}
	if got := jsonField(t, body[0], "targetZoneId"); got != "ru-central1-b" {
		t.Errorf("целевая зона не доехала до глагола: %v — копия легла бы туда же, где лежит источник", got)
	}
	if got := jsonField(t, body[0], "projectId"); got != "prj1" {
		t.Errorf("проект не доехал до глагола: %v — а именно он объект вопроса о правах", got)
	}
	if n := len(edge.calls(http.MethodPost, "/storage/v1/snapshots")); n != 0 {
		t.Errorf("копия ушла ещё и в коллекцию (%d раз) — это создал бы второй снимок", n)
	}
	assertState(t, resp.State, "id", "snpNEW")
	// Источник копии обязан ПЕРЕЖИТЬ обратное чтение: контракт ресурса его не несёт, и
	// записанная сюда пустота на следующем плане прочиталась бы как другое значение
	// неизменяемого поля — то есть предложила бы скопировать данные заново.
	assertState(t, resp.State, "source_snapshot_id", "snpSRC")
	assertState(t, resp.State, "zone_id", "ru-central1-b")
}

// Парный положительный контроль: обычный снимок идёт в КОЛЛЕКЦИЮ, а не в глагол копии.
//
// Без него проба выше зеленела бы на провайдере, который любое создание снимка отправляет
// глаголом копии.
func TestPlainSnapshotStillGoesToTheCollection(t *testing.T) {
	edge := newEdge(t)
	edge.operation("/storage/v1/snapshots", map[string]any{"snapshotId": "snp1"})
	edge.get("/storage/v1/snapshots/snp1", snapshotJSON("snp1", "ru-central1-a", "vol1"))

	sch := flatSchemaOf(t, storageSnapshotSpec)
	r := &flatResource{spec: storageSnapshotSpec, c: mustProviderClient(t, edge.URL())}
	plan := planOf(t, sch, map[string]any{
		"project_id": "prj1", "name": "nightly", "source_volume_id": "vol1",
	})

	resp := &resource.CreateResponse{State: tfsdk.State{Schema: sch, Raw: nullObject(t, sch)}}
	r.Create(context.Background(), resource.CreateRequest{Plan: plan}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("создание снимка отвергнуто: %v", resp.Diagnostics.Errors())
	}
	if n := len(edge.calls(http.MethodPost, "/storage/v1/snapshots")); n != 1 {
		t.Fatalf("постов в коллекцию: %d, ожидался 1", n)
	}
	assertState(t, resp.State, "source_volume_id", "vol1")
	// Незаданный источник копии становится null, а не пустой строкой: пустая строка —
	// значение, и неизменяемое поле с ней потребовало бы пересоздания на первом же плане.
	var src types.String
	if d := resp.State.GetAttribute(context.Background(), path.Root("source_snapshot_id"), &src); d.HasError() {
		t.Fatalf("чтение source_snapshot_id: %v", d.Errors())
	}
	if !src.IsNull() {
		t.Errorf("незаданный источник копии записан значением %q", src.ValueString())
	}
}

// Копия образа идёт своим глаголом, а целевой регион едет тем же атрибутом region_id.
func TestImageCopyCarriesTargetRegion(t *testing.T) {
	edge := newEdge(t)
	edge.operation("/storage/v1/images/imgSRC:copy", map[string]any{"imageId": "imgNEW"})
	edge.get("/storage/v1/images/imgNEW", imageJSON("imgNEW", "ru-central2"))

	sch := flatSchemaOf(t, storageImageSpec)
	r := &flatResource{spec: storageImageSpec, c: mustProviderClient(t, edge.URL())}
	plan := planOf(t, sch, map[string]any{
		"project_id": "prj1", "name": "golden-west",
		"source_image_id": "imgSRC", "region_id": "ru-central2",
	})

	resp := &resource.CreateResponse{State: tfsdk.State{Schema: sch, Raw: nullObject(t, sch)}}
	r.Create(context.Background(), resource.CreateRequest{Plan: plan}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("создание копии образа отвергнуто: %v", resp.Diagnostics.Errors())
	}
	body := edge.calls(http.MethodPost, "/storage/v1/images/imgSRC:copy")
	if len(body) != 1 {
		t.Fatalf("вызовов глагола копии образа: %d, ожидался 1", len(body))
	}
	if got := jsonField(t, body[0], "targetRegionId"); got != "ru-central2" {
		t.Errorf("целевой регион не доехал: %v", got)
	}
	assertState(t, resp.State, "id", "imgNEW")
	assertState(t, resp.State, "source_image_id", "imgSRC")
}

// Несовместимые наборы источников отвергаются НА ПЛАНЕ, с именем атрибута.
//
// Край отвергнет их тоже, но уже внутри apply и своим именем поля контракта. Отказ здесь
// называет атрибут ровно так, как его написал вызывающий.
func TestOriginConflictsAreRejectedBeforeTheEdgeIsCalled(t *testing.T) {
	cases := []struct {
		name   string
		spec   flatSpec
		values map[string]any
		says   string
	}{
		{
			name: "копия снимка без целевой зоны",
			spec: storageSnapshotSpec,
			values: map[string]any{"project_id": "prj1", "name": "c",
				"source_snapshot_id": "snpSRC"},
			says: "zone_id",
		},
		{
			name: "снимок с двумя источниками",
			spec: storageSnapshotSpec,
			values: map[string]any{"project_id": "prj1", "name": "c",
				"source_snapshot_id": "snpSRC", "zone_id": "z", "source_volume_id": "vol1"},
			says: "source_volume_id",
		},
		{
			name: "зона у снимка, который не копия",
			spec: storageSnapshotSpec,
			values: map[string]any{"project_id": "prj1", "name": "c",
				"source_volume_id": "vol1", "zone_id": "z"},
			says: "zone_id",
		},
		{
			name: "копия образа поверх снимка-источника",
			spec: storageImageSpec,
			values: map[string]any{"project_id": "prj1", "name": "c", "region_id": "r",
				"source_image_id": "imgSRC", "source_snapshot_id": "snpSRC"},
			says: "source_snapshot_id",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			edge := newEdge(t)
			sch := flatSchemaOf(t, tc.spec)
			r := &flatResource{spec: tc.spec, c: mustProviderClient(t, edge.URL())}
			resp := &resource.CreateResponse{State: tfsdk.State{Schema: sch, Raw: nullObject(t, sch)}}
			r.Create(context.Background(), resource.CreateRequest{Plan: planOf(t, sch, tc.values)}, resp)
			if !resp.Diagnostics.HasError() {
				t.Fatal("несовместимый набор источников принят")
			}
			if !strings.Contains(diagText(resp.Diagnostics.Errors()), tc.says) {
				t.Errorf("отказ не называет %s: %v", tc.says, resp.Diagnostics.Errors())
			}
			if n := edge.total(); n != 0 {
				t.Errorf("край позван %d раз на заведомо негодной настройке", n)
			}
		})
	}
}

// ---- отсутствие значения отдельно от значения -------------------------------------------

// Незаполненные краем занятые байты становятся ОТСУТСТВИЕМ, а не нулём.
//
// Ноль здесь — утверждение «том пуст», и по нему строят счёт и решают о расширении.
// Парная половина обязательна: заданное значение доезжает, иначе проба зеленела бы на
// разборе, который обнуляет всё подряд.
func TestUsedBytesAbsenceIsNotZero(t *testing.T) {
	sch := flatSchemaOf(t, storageVolumeSpec)
	r := &flatResource{spec: storageVolumeSpec}

	st := tfsdk.State{Schema: sch, Raw: planOf(t, sch, map[string]any{
		"id": "vol1", "project_id": "prj1", "name": "n", "zone_id": "z",
		"disk_type_id": "network-ssd", "size_bytes": int64(10),
	}).Raw}
	if err := r.applyWire(context.Background(), stateSetter{&st},
		[]byte(volumeJSON("network-ssd", "n"))); err != nil {
		t.Fatalf("разбор ответа: %v", err)
	}
	var used types.Int64
	if d := st.GetAttribute(context.Background(), path.Root("used_bytes"), &used); d.HasError() {
		t.Fatalf("чтение used_bytes: %v", d.Errors())
	}
	if !used.IsNull() {
		t.Errorf("несообщённое потребление записано числом %d — это утверждение «том пуст», "+
			"которого край не делал", used.ValueInt64())
	}

	if err := r.applyWire(context.Background(), stateSetter{&st},
		[]byte(`{"id":"vol1","projectId":"prj1","name":"n","zoneId":"z",
			"diskTypeId":"network-ssd","sizeBytes":"10","usedBytes":"4096","status":"AVAILABLE"}`)); err != nil {
		t.Fatalf("разбор ответа с потреблением: %v", err)
	}
	if d := st.GetAttribute(context.Background(), path.Root("used_bytes"), &used); d.HasError() {
		t.Fatalf("чтение used_bytes: %v", d.Errors())
	}
	if used.IsNull() || used.ValueInt64() != 4096 {
		t.Errorf("сообщённое потребление потеряно: %v", used)
	}
}

// ---- оснастка проб -----------------------------------------------------------------------

func flatSchemaOf(t *testing.T, spec flatSpec) schema.Schema {
	t.Helper()
	var resp resource.SchemaResponse
	(&flatResource{spec: spec}).Schema(context.Background(), resource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("схема ресурса %s не собралась: %v", spec.tfName, resp.Diagnostics.Errors())
	}
	return resp.Schema
}

// replacesOnChange — требует ли атрибут пересоздания ресурса при смене значения.
//
// Модификаторы ИСПОЛНЯЮТСЯ, а не разглядываются по типу: разглядывание закрепило бы состав
// списка, а вопрос стоит о поведении. Состояние и план непусты намеренно — на создании и
// удалении модификатор молчит by construction, и на пустых сюда любой атрибут ответил бы
// «не пересоздаёт».
func replacesOnChange(t *testing.T, a schema.Attribute, before, after string) bool {
	t.Helper()
	sa, ok := a.(schema.StringAttribute)
	if !ok {
		t.Fatalf("атрибут не строковый: %T", a)
	}
	nonEmpty := tftypes.NewValue(tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"x": tftypes.String}}, map[string]tftypes.Value{"x": tftypes.NewValue(tftypes.String, "x")})
	for _, m := range sa.PlanModifiers {
		req := planmodifier.StringRequest{
			Path:        path.Root("probe"),
			StateValue:  types.StringValue(before),
			PlanValue:   types.StringValue(after),
			ConfigValue: types.StringValue(after),
			State:       tfsdk.State{Raw: nonEmpty},
			Plan:        tfsdk.Plan{Raw: nonEmpty},
		}
		resp := &planmodifier.StringResponse{PlanValue: req.PlanValue}
		m.PlanModifyString(context.Background(), req, resp)
		if resp.RequiresReplace {
			return true
		}
	}
	return false
}

// planOf / stateOf собирают план и состояние из НАСТОЯЩЕЙ схемы ресурса.
//
// Значения, не названные вызывающим, ведут себя как в жизни: вычисляемый атрибут неизвестен
// (край его ещё не сообщил), остальные пусты. Подмена этого правила сделала бы пробы
// снисходительнее продукта — ровно там, где проверяется различение «не задано» и «задано».
func planOf(t *testing.T, sch schema.Schema, values map[string]any) tfsdk.Plan {
	t.Helper()
	return tfsdk.Plan{Schema: sch, Raw: objectOf(t, sch, values)}
}

func stateOf(t *testing.T, sch schema.Schema, values map[string]any) tfsdk.State {
	t.Helper()
	return tfsdk.State{Schema: sch, Raw: objectOf(t, sch, values)}
}

func nullObject(t *testing.T, sch schema.Schema) tftypes.Value {
	t.Helper()
	return tftypes.NewValue(sch.Type().TerraformType(context.Background()), nil)
}

func objectOf(t *testing.T, sch schema.Schema, values map[string]any) tftypes.Value {
	t.Helper()
	obj, ok := sch.Type().TerraformType(context.Background()).(tftypes.Object)
	if !ok {
		t.Fatal("схема ресурса не является объектом")
	}
	out := make(map[string]tftypes.Value, len(obj.AttributeTypes))
	for name, at := range obj.AttributeTypes {
		v, named := values[name]
		if !named {
			if computedOnly(sch.Attributes[name]) {
				out[name] = tftypes.NewValue(at, tftypes.UnknownValue)
			} else {
				out[name] = tftypes.NewValue(at, nil)
			}
			continue
		}
		out[name] = tfValueOf(t, at, v)
	}
	return tftypes.NewValue(obj, out)
}

// computedOnly — атрибут, значение которого приходит от края.
//
// Необязательный атрибут каркаса тоже вычисляем (край подставляет умолчание), поэтому
// «неизвестен в плане» — верно и для него: пользователь его не задал.
func computedOnly(a schema.Attribute) bool { return a != nil && a.IsComputed() }

func tfValueOf(t *testing.T, at tftypes.Type, v any) tftypes.Value {
	t.Helper()
	switch x := v.(type) {
	case string:
		return tftypes.NewValue(at, x)
	case int64:
		return tftypes.NewValue(at, x)
	case bool:
		return tftypes.NewValue(at, x)
	case map[string]string:
		el := make(map[string]tftypes.Value, len(x))
		for k, s := range x {
			el[k] = tftypes.NewValue(tftypes.String, s)
		}
		return tftypes.NewValue(at, el)
	case []string:
		el := make([]tftypes.Value, 0, len(x))
		for _, s := range x {
			el = append(el, tftypes.NewValue(tftypes.String, s))
		}
		return tftypes.NewValue(at, el)
	}
	t.Fatalf("проба не умеет собирать значение %T", v)
	return tftypes.Value{}
}

func assertState(t *testing.T, st tfsdk.State, attr, want string) {
	t.Helper()
	var got types.String
	if d := st.GetAttribute(context.Background(), path.Root(attr), &got); d.HasError() {
		t.Fatalf("чтение %s: %v", attr, d.Errors())
	}
	if got.ValueString() != want {
		t.Errorf("%s в состоянии = %q, ожидалось %q", attr, got.ValueString(), want)
	}
}

func diagText(ds diag.Diagnostics) string {
	var b strings.Builder
	for _, d := range ds {
		b.WriteString(d.Summary())
		b.WriteString(" ")
		b.WriteString(d.Detail())
		b.WriteString("\n")
	}
	return b.String()
}

// ---- подставной край ---------------------------------------------------------------------

// edgeStub — край, отвечающий ровно то, что ему велено, и запоминающий обращения.
//
// Незаявленный путь отвечает 404 намеренно: проба, ушедшая не туда, обязана падать, а не
// получать заглушку. Иначе подставной край оказался бы снисходительнее настоящего.
type edgeStub struct {
	t    *testing.T
	srv  *httptest.Server
	mu   sync.Mutex
	body map[string]string   // GET-путь → тело
	seen map[string][]string // "METHOD path" → тела запросов
	ops  map[string]bool     // POST-путь, отвечающий операцией
	meta map[string]map[string]any
	// order — обращения в порядке поступления. Нужен там, где порядок запросов сам по себе
	// свойство: у изменения тома глагол обязан идти раньше маски, потому что среди полей
	// маски есть необратимое.
	order []string
	// last — метаданные ПОСЛЕДНЕЙ принятой мутации.
	//
	// Живут отдельно потому, что край отвечает на мутацию идентификатором операции, а
	// метаданные вызывающий читает уже опросом `/operations/<id>` — то есть по ДРУГОМУ
	// пути. Подставной край, отдающий их сразу в ответе на мутацию, был бы снисходительнее
	// настоящего: провайдер, разучившийся опрашивать операцию, остался бы зелёным.
	last map[string]any
}

func newEdge(t *testing.T) *edgeStub {
	t.Helper()
	e := &edgeStub{t: t, body: map[string]string{}, seen: map[string][]string{},
		ops: map[string]bool{}, meta: map[string]map[string]any{}}
	e.srv = httptest.NewServer(http.HandlerFunc(e.serve))
	t.Cleanup(e.srv.Close)
	return e
}

func (e *edgeStub) URL() string { return e.srv.URL }

func (e *edgeStub) get(path, body string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.body[path] = body
}

func (e *edgeStub) operation(path string, metadata map[string]any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.ops[path] = true
	e.meta[path] = metadata
}

func (e *edgeStub) calls(method, path string) []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string{}, e.seen[method+" "+path]...)
}

// mutations — мутирующие обращения в порядке поступления.
func (e *edgeStub) mutations() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string{}, e.order...)
}

func (e *edgeStub) total() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for _, v := range e.seen {
		n += len(v)
	}
	return n
}

func (e *edgeStub) serve(w http.ResponseWriter, req *http.Request) {
	var raw []byte
	if req.Body != nil {
		raw, _ = io.ReadAll(req.Body)
	}
	e.mu.Lock()
	key := req.Method + " " + req.URL.Path
	e.seen[key] = append(e.seen[key], string(raw))
	if req.Method != http.MethodGet {
		e.order = append(e.order, key)
	}
	body, hasBody := e.body[req.URL.Path]
	isOp := e.ops[req.URL.Path]
	meta := e.meta[req.URL.Path]
	if req.Method != http.MethodGet {
		e.last = meta
	}
	last := e.last
	e.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	switch {
	case req.Method == http.MethodPost && isOp:
		// Ответ на мутацию несёт ТОЛЬКО идентификатор операции — как у края.
		_, _ = w.Write([]byte(`{"id":"op1","done":false}`))
	case req.Method == http.MethodPost || req.Method == http.MethodPatch || req.Method == http.MethodDelete:
		// Мутация без объявленной операции — тоже операция: у края асинхронны все.
		_, _ = w.Write([]byte(`{"id":"op1","done":false}`))
	case strings.HasPrefix(req.URL.Path, "/operations/"):
		out, _ := json.Marshal(map[string]any{"id": "op1", "done": true, "metadata": last})
		_, _ = w.Write(out)
	case hasBody:
		_, _ = w.Write([]byte(body))
	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":5,"message":"not found"}`))
	}
}

func jsonField(t *testing.T, body, field string) any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("тело запроса не разобрано (%q): %v", body, err)
	}
	return m[field]
}

func volumeJSON(diskType, name string) string {
	return `{"id":"vol1","projectId":"prj1","name":"` + name + `","zoneId":"ru-central1-a",
		"diskTypeId":"` + diskType + `","sizeBytes":"10","status":"AVAILABLE",
		"createdAt":"2026-08-13T00:00:00Z","updatedAt":"2026-08-13T00:00:00Z"}`
}

func snapshotJSON(id, zone, sourceVolume string) string {
	return `{"id":"` + id + `","projectId":"prj1","name":"n","zoneId":"` + zone + `",
		"sourceVolumeId":"` + sourceVolume + `","sizeBytes":"10","status":"CREATING",
		"createdAt":"2026-08-13T00:00:00Z","updatedAt":"2026-08-13T00:00:00Z"}`
}

func imageJSON(id, region string) string {
	return `{"id":"` + id + `","projectId":"prj1","name":"n","regionId":"` + region + `",
		"placementType":"REGIONAL","sizeBytes":"10","minDiskBytes":"10","format":"STANDARD",
		"status":"CREATING","createdAt":"2026-08-13T00:00:00Z","updatedAt":"2026-08-13T00:00:00Z"}`
}

// Описания копий обязаны ссылаться на СУЩЕСТВУЮЩИЕ поля запросов глагола.
//
// Опечатка в имени поля не ловится компилятором: перенос идёт по имени через отражение, и
// разошедшееся описание отказало бы только на живом крае — у пользователя, посреди apply.
func TestCopyRoutesNameRealContractFields(t *testing.T) {
	specs := []flatSpec{storageSnapshotSpec, storageImageSpec}
	checked := 0
	for _, spec := range specs {
		route := spec.copyFrom
		if route == nil {
			t.Fatalf("у ресурса %s снят путь копии — проба перестала бы проверять что-либо", spec.tfName)
		}
		msg := route.newRequest()
		res := &flatResource{spec: spec}
		names := []string{route.sourceField}
		for _, c := range route.carry {
			names = append(names, c.field)
			if _, ok := res.field(c.attr); !ok {
				t.Errorf("%s: перенос называет атрибут %q, которого у ресурса нет", spec.tfName, c.attr)
			}
		}
		// Атрибуты, которыми описаны совместимость и обязательность, обязаны существовать.
		//
		// Опечатка в запрещающем перечне не видна НИКАК: несуществующий атрибут «не задан»
		// всегда, поэтому запрет молча перестал бы срабатывать — форма проверки без
		// содержания. Обязывающий перечень с опечаткой, наоборот, отвергал бы всё подряд,
		// и это заметили бы; молчаливую половину закрывает именно эта проба.
		attrs := append([]string{route.sourceAttr}, route.requiredWithCopy...)
		attrs = append(attrs, route.forbiddenWithCopy...)
		attrs = append(attrs, route.forbiddenWithoutCopy...)
		for _, a := range attrs {
			if _, ok := res.field(a); !ok {
				t.Errorf("%s: описание копии называет атрибут %q, которого у ресурса нет", spec.tfName, a)
			}
			checked++
		}
		for _, n := range names {
			if !hasProtoField(msg, n) {
				t.Errorf("%s: запрос %s не несёт поля %q", spec.tfName,
					msg.ProtoReflect().Descriptor().FullName(), n)
			}
			checked++
		}
	}
	if checked == 0 {
		t.Fatal("не проверено ни одного поля — обходчик пуст, и «ноль находок» здесь означал бы " +
			"ноль прочитанного")
	}
	t.Logf("осмотрено: описаний копии %d, полей запроса %d", len(specs), checked)
}

// То же для полей, меняющихся своим глаголом.
func TestVerbFieldsNameRealContractFields(t *testing.T) {
	checked := 0
	for _, spec := range []flatSpec{storageVolumeSpec, storageSnapshotSpec, storageImageSpec} {
		for _, vf := range spec.verbFields {
			msg := vf.newRequest()
			for _, n := range []string{vf.idField, vf.valueField} {
				if !hasProtoField(msg, n) {
					t.Errorf("%s: запрос глагола %s не несёт поля %q", spec.tfName, vf.verb, n)
				}
				checked++
			}
			if _, ok := (&flatResource{spec: spec}).field(vf.attr); !ok {
				t.Errorf("%s: глагол %s объявлен для атрибута %q, которого у ресурса нет",
					spec.tfName, vf.verb, vf.attr)
			}
		}
	}
	if checked == 0 {
		t.Fatal("полей глаголов не осмотрено ни одного — у storage объявлена смена класса диска, " +
			"значит обходчик сломан")
	}
	t.Logf("осмотрено: полей запросов глаголов %d", checked)
}

func hasProtoField(m proto.Message, name string) bool {
	return m.ProtoReflect().Descriptor().Fields().ByName(protoreflect.Name(name)) != nil
}
