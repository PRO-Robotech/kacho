// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware

import (
	"reflect"
	"testing"
)

// kacho#2740 — КАЖДОЕ окно доклада слоя аутентификации отдаётся поверхности
// сбора. Перечень окон выводится из полей AuthInterceptor, а не выписывается:
// новое окно без клетки краснит эту пробу.
func TestLogWindowsCarryEveryReporterOfTheAuthLayer(t *testing.T) {
	a := NewAuthInterceptor(AuthModeProduction, "", nil, nil)
	reporterType := reflect.TypeOf(&introspectionFailureReporter{})
	v := reflect.ValueOf(a).Elem()
	var reporters []*introspectionFailureReporter
	for i := 0; i < v.NumField(); i++ {
		if v.Type().Field(i).Type != reporterType {
			continue
		}
		r := newIntrospectionFailureReporter(0, nil)
		for n := 0; n <= len(reporters); n++ {
			r.observe() // у каждого окна свой итог: 1, 2, 3, …
		}
		reflect.NewAt(v.Field(i).Type(), v.Field(i).Addr().UnsafePointer()).Elem().Set(reflect.ValueOf(r))
		reporters = append(reporters, r)
	}
	if len(reporters) == 0 {
		t.Fatal("окон доклада в AuthInterceptor 0 — перечень не выведен")
	}

	w := reflect.ValueOf(a.LogWindows())
	seen := map[uint64]bool{}
	for i := 0; i < w.NumField(); i++ {
		total, ok := w.Field(i).Interface().(LogWindowTotal)
		if !ok || total == nil {
			t.Errorf("окно %s не отдано поверхности", w.Type().Field(i).Name)
			continue
		}
		seen[total.Total()] = true
	}
	t.Logf("перепись: окон в AuthInterceptor %d · полей LogWindows %d", len(reporters), w.NumField())
	if w.NumField() != len(reporters) {
		t.Fatalf("окон доклада %d, полей LogWindows %d — окно без клетки либо клетка без окна",
			len(reporters), w.NumField())
	}
	for n := 1; n <= len(reporters); n++ {
		if !seen[uint64(n)] {
			t.Errorf("итог %d (окно №%d по порядку полей) поверхности не отдан", n, n)
		}
	}
}

// Окно непровязанной полосы отдаёт ноль, а не падает: nil-получатель законен.
func TestLogWindowTotalOfAnUnwiredWindowIsZero(t *testing.T) {
	var r *introspectionFailureReporter
	if got := r.Total(); got != 0 {
		t.Fatalf("итог непровязанного окна %d, ожидался 0", got)
	}
}
