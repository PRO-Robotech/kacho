// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/terraform/internal/client"
)

// newTestDataSource поднимает край-двойник и подключённый к нему источник данных.
//
// Двойник отвечает ровно тем, что просит проба, — и не более снисходительно, чем
// настоящий край: разбор ответа идёт тем же кодом, что в бою.
func newTestDataSource(t *testing.T, handler http.HandlerFunc) *machineTypeDataSource {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c, err := client.New(client.Config{Endpoint: srv.URL, Token: "t"})
	if err != nil {
		t.Fatalf("клиент: %v", err)
	}
	return &machineTypeDataSource{c: c}
}

func listBody(items string) string {
	return `{"machineTypes":[` + items + `],"nextPageToken":""}`
}

const stdV3 = `{"id":"mt-abc","name":"std-v3-2","family":"STANDARD","status":"AVAILABLE",
	"availableZones":["ru-central1-a"],
	"effectiveResources":{"vCpu":2,"memoryMib":"8192","gpus":0,"gpuType":""}}`

// TestGetByName_ZeroAndManyAreBothRefusals — ноль записей и несколько записей суть
// РАЗНЫЕ отказы, и оба отказы.
//
// Молча взять первую из нескольких значило бы выбрать размер машины за
// арендатора. Отдать пустое состояние на ноль — предъявить план, создающий
// машину с пустым типом.
func TestGetByName_ZeroAndManyAreBothRefusals(t *testing.T) {
	ctx := context.Background()

	t.Run("ноль записей — отказ, называющий имя", func(t *testing.T) {
		d := newTestDataSource(t, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(listBody("")))
		})
		_, err := d.getByName(ctx, "нет-такого")
		if err == nil {
			t.Fatal("пустой каталог принят — источник данных отдал бы пустой тип машины")
		}
		if !strings.Contains(err.Error(), "нет-такого") {
			t.Errorf("отказ не называет имя: %v", err)
		}
	})

	t.Run("несколько записей — отказ, а не выбор за арендатора", func(t *testing.T) {
		dup := strings.ReplaceAll(stdV3, `"mt-abc"`, `"mt-dup"`)
		d := newTestDataSource(t, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(listBody(stdV3 + "," + dup)))
		})
		_, err := d.getByName(ctx, "std-v3-2")
		if err == nil {
			t.Fatal("двусмысленное имя разрешено молча — выбран первый попавшийся размер машины")
		}
	})

	// Положительный контроль: ровно одна запись РАЗРЕШАЕТСЯ. Без него оба отрицания
	// зеленели бы на реализации, отвергающей вообще всё.
	t.Run("ровно одна запись разрешается", func(t *testing.T) {
		d := newTestDataSource(t, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(listBody(stdV3)))
		})
		mt, err := d.getByName(ctx, "std-v3-2")
		if err != nil {
			t.Fatalf("однозначное имя отвергнуто: %v", err)
		}
		if mt.ID != "mt-abc" {
			t.Errorf("идентификатор = %q, ожидался mt-abc", mt.ID)
		}
	})
}

// TestGetByName_LooseEdgeFilterIsNotTrusted — имя сверяется ЗДЕСЬ, а не только
// на крае.
//
// Фильтр края сегодня точный. Ослабнет он (станет префиксным, регистронезависимым,
// подстрочным) — и источник данных начнёт молча отдавать не тот размер: план
// покажет замену машины, причина которой лежит в другом сервисе.
func TestGetByName_LooseEdgeFilterIsNotTrusted(t *testing.T) {
	other := strings.ReplaceAll(strings.ReplaceAll(stdV3, `"mt-abc"`, `"mt-xyz"`),
		`"std-v3-2"`, `"std-v3-24"`)
	d := newTestDataSource(t, func(w http.ResponseWriter, _ *http.Request) {
		// Двойник изображает ОСЛАБШИЙ фильтр: на "std-v3-2" отдаёт и "std-v3-24".
		_, _ = w.Write([]byte(listBody(stdV3 + "," + other)))
	})

	mt, err := d.getByName(context.Background(), "std-v3-2")
	if err != nil {
		t.Fatalf("точное имя не разрешилось при лишней записи в ответе: %v", err)
	}
	if mt.Name != "std-v3-2" {
		t.Errorf("выбрано %q — сверка точного имени на стороне провайдера не сработала", mt.Name)
	}
}

// TestApplyMachineType_SizeSurvivesBothWireForms — 64-битный размер приходит и
// числом, и строкой.
//
// JSON не выражает 64-битное целое без потери точности, поэтому кодировщик
// protobuf отдаёт такие поля строкой. Прочитать их только числом значит получить
// НОЛЬ на каждом размере — и разбор упал бы там, где его легко списать на пустой
// каталог.
func TestApplyMachineType_SizeSurvivesBothWireForms(t *testing.T) {
	ctx := context.Background()

	for _, tc := range []struct {
		name string
		body string
	}{
		{"память строкой (форма protobuf-кодировщика)", `"8192"`},
		{"память числом (если кодировщик на крае сменят)", `8192`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			item := `{"id":"mt-1","name":"n","status":"AVAILABLE",
				"effectiveResources":{"vCpu":2,"memoryMib":` + tc.body + `,"gpus":0}}`
			d := newTestDataSource(t, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(listBody(item)))
			})
			mt, err := d.getByName(ctx, "n")
			if err != nil {
				t.Fatalf("разбор: %v", err)
			}
			var m machineTypeModel
			applyMachineType(ctx, &m, mt)
			if got := m.MemoryMiB.ValueInt64(); got != 8192 {
				t.Errorf("память = %d, ожидалось 8192 — размер прочитан неверно, "+
					"и машина заказалась бы не того размера", got)
			}
		})
	}

	// Размер может не приехать вовсе: край вправе его не объявить. Разыменование
	// без проверки уронило бы провайдер там, где ответ корректен.
	t.Run("размер не объявлен — ноль, а не паника", func(t *testing.T) {
		var m machineTypeModel
		applyMachineType(ctx, &m, &machineTypeJSON{ID: "mt-1", Name: "n", Status: "AVAILABLE"})
		if m.MemoryMiB.ValueInt64() != 0 || m.VCPU.ValueInt64() != 0 {
			t.Error("необъявленный размер дал ненулевые значения")
		}
	})
}

// TestFlexInt64_RejectsGarbage — отрицание в паре с положительным.
//
// Без этой пробы разбор, тихо превращающий мусор в ноль, выглядел бы работающим:
// «8192» и «восемь тысяч» дали бы одинаково пригодный результат.
func TestFlexInt64_RejectsGarbage(t *testing.T) {
	var v flexInt64
	if err := v.UnmarshalJSON([]byte(`"не-число"`)); err == nil {
		t.Error("мусорная строка принята — неразобранное значение стало бы нулём молча")
	}
	if err := v.UnmarshalJSON([]byte(`null`)); err != nil {
		t.Errorf("null отвергнут: %v — незаполненное краем поле означает «не объявлено», а не отказ", err)
	}
	if err := v.UnmarshalJSON([]byte(`"8192"`)); err != nil || v != 8192 {
		t.Errorf("законное значение отвергнуто: v=%d err=%v", v, err)
	}
}
