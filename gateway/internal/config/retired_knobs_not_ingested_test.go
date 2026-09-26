// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

import (
	"reflect"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/retiredknobs"
)

// TestRetiredKnobIsNotIngestedByTheEdge — снятая ручка не доезжает до
// конфигурации процесса НИ ОДНИМ полем, даже если окружение её задаёт (#2734).
//
// Адрес, который процесс не прочитал, он не может и набрать: исходящего хода к
// снятому поставщику у края нет by construction, и это доказывается
// наблюдением загрузки, а не чтением кода. Окружение пробы задаёт КАЖДУЮ
// снятую ручку своим меченым значением, затем конфигурация грузится тем же
// загрузчиком, что у процесса, и обходом всех строковых полей ищется хоть одна
// метка.
//
// Положительный контроль — живая ручка той же формы: её метка ОБЯЗАНА
// доехать, иначе обход полей ничего не видит и «метки нет» ничего не значит.
func TestRetiredKnobIsNotIngestedByTheEdge(t *testing.T) {
	retired := retiredknobs.Edge()
	if len(retired) == 0 {
		t.Fatal("ведомость снятых ручек пуста — проверять нечего, и молчание здесь не утверждение")
	}
	const mark = "retired-knob-sentinel:"
	for name := range retired {
		t.Setenv(name, mark+name)
	}
	t.Setenv(LoginLaneURLKnob, "https://live-control.invalid")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("конфигурация не загрузилась: %v", err)
	}

	var ingested, fields []string
	var live bool
	walkStringFields(reflect.ValueOf(cfg), "Config", func(path, v string) {
		fields = append(fields, path)
		if strings.HasPrefix(v, mark) {
			ingested = append(ingested, path+" = "+strings.TrimPrefix(v, mark))
		}
		if v == "https://live-control.invalid" {
			live = true
		}
	})
	if !live {
		t.Fatalf("положительный контроль не доехал: живая ручка %s не найдена ни в одном из %d "+
			"строковых полей — обход полей слеп, и отсутствие меток ничего не значит",
			LoginLaneURLKnob, len(fields))
	}
	for _, f := range ingested {
		t.Errorf("снятая ручка доехала до конфигурации края: %s — процесс её читает и "+
			"может набрать адрес снятого поставщика", f)
	}
	t.Logf("перепись: снятых ручек задано %d · строковых полей осмотрено %d · доехало %d",
		len(retired), len(fields), len(ingested))
}

// walkStringFields обходит все строковые поля значения, включая вложенные
// структуры, и зовёт visit с путём поля.
func walkStringFields(v reflect.Value, path string, visit func(path, value string)) {
	switch v.Kind() {
	case reflect.String:
		visit(path, v.String())
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			f := v.Type().Field(i)
			if !f.IsExported() {
				continue
			}
			walkStringFields(v.Field(i), path+"."+f.Name, visit)
		}
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			walkStringFields(v.Elem(), path, visit)
		}
	}
}
