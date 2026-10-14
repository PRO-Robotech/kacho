// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// TestCutoffQuestionIsAskedByATypedSubject — kacho#2760 п. 2: вопрос об отсечке
// задаётся ТИПОМ, а не свободной строкой. Параметр порта — `CutoffSubject`, и
// собрать его можно только конструктором: свободная строка на границе порта не
// принимается (код, передающий строку, не компилируется), а второй экземпляр
// сборки невыразим.
func TestCutoffQuestionIsAskedByATypedSubject(t *testing.T) {
	m, ok := reflect.TypeOf((*middleware.SessionCutoffReader)(nil)).Elem().MethodByName("SessionCutoffOf")
	if !ok {
		t.Fatal("у порта отсечки нет метода SessionCutoffOf")
	}
	if got := m.Type.In(1); got != reflect.TypeOf(middleware.CutoffSubject{}) {
		t.Fatalf("параметр вопроса об отсечке — %v, а не middleware.CutoffSubject: свободная строка принимает "+
			"любое значение любого происхождения", got)
	}
	var _ = context.Background
}

// Конструктор один и судит вид: только человек с идентификатором. Машинная
// учётка и пустой идентификатор вопроса об отсечке не порождают — отсечка
// ключуется человеком (законный близнец — человек — порождает).
func TestCutoffSubjectIsBuiltOnlyForAHuman(t *testing.T) {
	if s, ok := middleware.NewCutoffSubject("user", "usr-00000000000000ka1"); !ok || s.UserID() != "usr-00000000000000ka1" {
		t.Fatalf("человек с идентификатором обязан давать субъекта отсечки: %v %v", s, ok)
	}
	for _, c := range []struct{ typ, id string }{
		{"service_account", "sva-00000000000000ka1"},
		{"user", ""},
		{"", "usr-00000000000000ka1"},
		{"system", "anonymous"},
	} {
		if s, ok := middleware.NewCutoffSubject(c.typ, c.id); ok || s.UserID() != "" {
			t.Errorf("(%q, %q) не обязан давать субъекта отсечки: %v %v", c.typ, c.id, s, ok)
		}
	}
	if (middleware.CutoffSubject{}).UserID() != "" {
		t.Error("нулевое значение несёт идентификатор")
	}
}
