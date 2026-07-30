// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package authzfilter

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
)

// Вопрос про ОДИН названный объект с явно заданным набором отношений — гейт
// мутации, а не фильтр видимости. Проверяется реализация, а не фейк порта:
// use-case-тесты доказывают, что вопрос ЗАДАЁТСЯ, эти — что он задаётся ВЕРНО и
// что ни один отказ не превращается в «да».

const (
	gateInstance = "compute_instance"
	gateAction   = "storage.volumes.attach"
	gateSubject  = "user:usr_alice"
)

func gateFilter(t *testing.T, cli AuthorizeClient) *FGAFilter {
	t.Helper()
	return NewFGAFilter(cli, Config{Enabled: true, Timeout: time.Second})
}

// TestAllowedOnObject_AnyOfTheRelationsAllows — союз: разрешено, если резолвится
// ЛЮБОЕ из названных отношений. Отвязка принимает право изменения ИЛИ право сноса.
func TestAllowedOnObject_AnyOfTheRelationsAllows(t *testing.T) {
	cli := newFakeAuthorizeClient().allow("v_delete", "ins-mine")
	f := gateFilter(t, cli)

	ok, err := f.AllowedOnObject(context.Background(), gateSubject, gateInstance, gateAction,
		[]string{"v_update", "v_delete"}, "ins-mine")
	if err != nil {
		t.Fatalf("AllowedOnObject: %v", err)
	}
	if !ok {
		t.Fatal("ни одно отношение не разрешило, хотя право сноса выдано — союз не работает")
	}
	calls, checked, _, _ := cli.snapshot()
	if calls != 1 {
		t.Fatalf("round-trip'ов = %d, want 1: оба отношения обязаны спрашиваться ОДНИМ вызовом", calls)
	}
	if checked != 2 {
		t.Fatalf("проверок в запросе = %d, want 2 (по одному на отношение)", checked)
	}
}

// TestAllowedOnObject_NoRelationAllowsIsADenial — ни одно отношение не резолвится
// → отказ, и это НЕ ошибка (обычный вердикт модели).
func TestAllowedOnObject_NoRelationAllowsIsADenial(t *testing.T) {
	f := gateFilter(t, newFakeAuthorizeClient().allow("v_update", "ins-theirs"))

	ok, err := f.AllowedOnObject(context.Background(), gateSubject, gateInstance, gateAction,
		[]string{"v_update", "v_delete"}, "ins-mine")
	if err != nil {
		t.Fatalf("отказ модели — вердикт, а не ошибка: %v", err)
	}
	if ok {
		t.Fatal("разрешено на объект, на который прав нет")
	}
}

// TestAllowedOnObject_AsksAboutTheNamedObject — форма вопроса: тот субъект, тот
// объект, те отношения. Иначе проверка была бы «про что-то другое».
func TestAllowedOnObject_AsksAboutTheNamedObject(t *testing.T) {
	cli := newFakeAuthorizeClient().allow("v_update", "ins-mine")
	f := gateFilter(t, cli)

	if _, err := f.AllowedOnObject(context.Background(), gateSubject, gateInstance, gateAction,
		[]string{"v_update"}, "ins-mine"); err != nil {
		t.Fatalf("AllowedOnObject: %v", err)
	}
	cli.mu.Lock()
	defer cli.mu.Unlock()
	if len(cli.gotReqs) != 1 {
		t.Fatalf("проверок = %d, want 1", len(cli.gotReqs))
	}
	r := cli.gotReqs[0]
	if r.GetSubject() != gateSubject {
		t.Fatalf("subject = %q, want %q", r.GetSubject(), gateSubject)
	}
	if r.GetResource().GetType() != gateInstance || r.GetResource().GetId() != "ins-mine" {
		t.Fatalf("объект = (%q,%q), want (%q,%q)",
			r.GetResource().GetType(), r.GetResource().GetId(), gateInstance, "ins-mine")
	}
	if r.GetRequiredRelation() != "v_update" {
		t.Fatalf("отношение = %q, want %q", r.GetRequiredRelation(), "v_update")
	}
	if r.GetAction() != gateAction {
		t.Fatalf("действие = %q, want %q (строка аудита совпадает с записью каталога)", r.GetAction(), gateAction)
	}
}

// TestAllowedOnObject_UnreachableModelIsNotAYes — недоступный ответ модели не есть
// ответ «да».
func TestAllowedOnObject_UnreachableModelIsNotAYes(t *testing.T) {
	cli := newFakeAuthorizeClient()
	cli.err = status.Error(codes.Unavailable, "iam down")
	f := gateFilter(t, cli)

	ok, err := f.AllowedOnObject(context.Background(), gateSubject, gateInstance, gateAction,
		[]string{"v_update"}, "ins-mine")
	if ok {
		t.Fatal("разрешено, пока модель недоступна")
	}
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("код = %v, want Unavailable", status.Code(err))
	}
}

// TestAllowedOnObject_FailOpenDoesNotApplyToAMutationGate — ручка мягкого прохода
// — осознанный размен на ЧТЕНИИ. На мутации, пишущей строку в набор ЧУЖОГО
// ресурса, «продолжить, потому что модель не ответила» защитимого прочтения не
// имеет: недоступность iam не делает вызывающего владельцем названной машины.
func TestAllowedOnObject_FailOpenDoesNotApplyToAMutationGate(t *testing.T) {
	cli := newFakeAuthorizeClient()
	cli.err = status.Error(codes.Unavailable, "iam down")
	f := NewFGAFilter(cli, Config{Enabled: true, Timeout: time.Second, FailOpen: true})

	ok, err := f.AllowedOnObject(context.Background(), gateSubject, gateInstance, gateAction,
		[]string{"v_update"}, "ins-mine")
	if ok || err == nil {
		t.Fatalf("мягкий проход применился к гейту мутации: ok=%v err=%v", ok, err)
	}
}

// TestAllowedOnObject_NotConfiguredIsADenial — порт есть, спросить негде. Это
// состояние посадки, а не ответ модели, поэтому отказ.
func TestAllowedOnObject_NotConfiguredIsADenial(t *testing.T) {
	for name, f := range map[string]*FGAFilter{
		"nil filter":    nil,
		"nil client":    NewFGAFilter(nil, Config{Enabled: true}),
		"disabled":      NewFGAFilter(newFakeAuthorizeClient(), Config{Enabled: false}),
		"both disabled": NewFGAFilter(nil, Config{Enabled: false}),
	} {
		ok, err := f.AllowedOnObject(context.Background(), gateSubject, gateInstance, gateAction,
			[]string{"v_update"}, "ins-mine")
		if ok {
			t.Errorf("%s: разрешено, хотя спросить негде", name)
		}
		if status.Code(err) != codes.PermissionDenied {
			t.Errorf("%s: код = %v, want PermissionDenied", name, status.Code(err))
		}
	}
}

// TestAllowedOnObject_EmptySubjectIsADenial — «не знаю, кто ты» не есть
// «доверенный».
func TestAllowedOnObject_EmptySubjectIsADenial(t *testing.T) {
	f := gateFilter(t, newFakeAuthorizeClient().allow("v_update", "ins-mine"))
	ok, err := f.AllowedOnObject(context.Background(), "", gateInstance, gateAction,
		[]string{"v_update"}, "ins-mine")
	if ok {
		t.Fatal("разрешено без субъекта")
	}
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("код = %v, want Unauthenticated", status.Code(err))
	}
}

// TestAllowedOnObject_MalformedQuestionIsAnError — пустой объект/действие/набор
// отношений: вопрос не сформулирован, поэтому ответа нет.
func TestAllowedOnObject_MalformedQuestionIsAnError(t *testing.T) {
	f := gateFilter(t, newFakeAuthorizeClient())
	cases := map[string]struct {
		resType, action, id string
		relations           []string
	}{
		"нет типа объекта": {"", gateAction, "ins-mine", []string{"v_update"}},
		"нет действия":     {gateInstance, "", "ins-mine", []string{"v_update"}},
		"нет объекта":      {gateInstance, gateAction, "", []string{"v_update"}},
		"нет отношений":    {gateInstance, gateAction, "ins-mine", nil},
	}
	for name, c := range cases {
		ok, err := f.AllowedOnObject(context.Background(), gateSubject, c.resType, c.action, c.relations, c.id)
		if ok || err == nil {
			t.Errorf("%s: ok=%v err=%v, want отказ с ошибкой", name, ok, err)
		}
	}
}

// TestAllowedOnObject_ShortResponseIsFailClosed — контракт длины: расхождение —
// fail-closed ошибка, а не «считаем отказом». Молчаливое смещение выдало бы
// вердикт одного отношения за другой.
func TestAllowedOnObject_ShortResponseIsFailClosed(t *testing.T) {
	cli := &shortResponseClient{}
	f := gateFilter(t, cli)

	ok, err := f.AllowedOnObject(context.Background(), gateSubject, gateInstance, gateAction,
		[]string{"v_update", "v_delete"}, "ins-mine")
	if ok {
		t.Fatal("разрешено по ответу, длина которого не совпадает с числом вопросов")
	}
	if err == nil || status.Code(err) != codes.Unavailable {
		t.Fatalf("err = %v (код %v), want Unavailable", err, status.Code(err))
	}
}

// shortResponseClient — пир, отвечающий меньшим числом вердиктов, чем задано
// вопросов (нарушение контракта BatchCheck).
type shortResponseClient struct{}

func (shortResponseClient) BatchCheck(
	_ context.Context, in *iamv1.BatchAuthorizeCheckRequest, _ ...grpc.CallOption,
) (*iamv1.BatchAuthorizeCheckResponse, error) {
	if len(in.GetChecks()) <= 1 {
		return &iamv1.BatchAuthorizeCheckResponse{}, nil
	}
	return &iamv1.BatchAuthorizeCheckResponse{
		Responses: []*iamv1.AuthorizeCheckResponse{{Allowed: true}},
	}, nil
}
