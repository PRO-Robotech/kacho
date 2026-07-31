// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// absent_model_is_not_yes_test.go — «модели здесь нет» не есть «да».
//
// Эталон стоит в этом же сервисе: authzfilter.AllowedOnObject на условии «порт есть,
// спросить негде» ОТКАЗЫВАЕТ и говорит почему — «Это состояние посадки, а не ответ
// модели, — поэтому отказ, а не „да"». Тот же вопрос стоит здесь, у ListAttachments:
// per-RPC Check за ним не задаётся вовсе (ScopeFiltered), значит отсутствие фильтра
// означает отсутствие авторизации, а не «сужение отключено».
//
// Половина уже закрыта соседним файлом: вызывающий без личности отсекается
// БЕЗУСЛОВНО, и его комментарий прямо называет причину — «привяжи мы fail-closed к
// наличию фильтра, осталась бы посадка, в которой RPC отдаёт привязки всего кластера
// кому угодно». Вторая половина — НАЗВАННЫЙ вызывающий при отсутствующем фильтре —
// осталась непроведённой, потому что общий помощник FilterVisiblePage отдаёт страницу
// как есть. Помощник здесь чинить нельзя: за ним стоят и публичные List'ы, у которых
// per-RPC Check остаётся, — одно имя обслуживает две РАЗНЫЕ точки, и значение у них
// разное.
package volume_test

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/operations"
)

func namedCallerCtx() context.Context {
	return operations.WithPrincipal(context.Background(),
		operations.Principal{Type: "user", ID: "usr_alice"})
}

// TestListAttachments_AbsentModelRefusesANamedCaller — фильтра нет, вызывающий назван:
// привязки не отдаются и строки не читаются. Инстансы называет ВЫЗЫВАЮЩИЙ, поэтому
// проход означал бы выдачу привязок любых названных инстансов из чужих проектов.
func TestListAttachments_AbsentModelRefusesANamedCaller(t *testing.T) {
	reader := newCountingReader(attPair())
	uc := newListUC(reader, nil) // фильтр не подключён

	got, err := uc.ListAttachments(namedCallerCtx(), []string{"ins-mine", "ins-theirs"})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("err = %v, want PermissionDenied — спросить негде значит отказ, а не «да»", err)
	}
	if len(got) != 0 {
		t.Fatalf("вернулось %v при неподключённом фильтре; за этим RPC нет второй линии",
			volumeIDsOf(got))
	}
	if reader.calls != 0 {
		t.Fatalf("строки прочитаны (%d вызовов) при отсутствующей модели прав", reader.calls)
	}
}

// TestListAttachments_PresentModelStillAnswers — ПАРНЫЙ ПОЛОЖИТЕЛЬНЫЙ. Без него отказ
// выше неотличим от «отказывает всегда» и зеленел бы на полностью сломанном пути.
func TestListAttachments_PresentModelStillAnswers(t *testing.T) {
	f := &fakeListFilter{allow: map[string]bool{"ins-mine": true}}
	uc := newListUC(readerAttachments(attPair()), f)

	got, err := uc.ListAttachments(namedCallerCtx(), []string{"ins-mine", "ins-theirs"})
	if err != nil {
		t.Fatalf("модель на месте — ответ обязан быть получен: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("сужение вернуло пусто на разрешённом инстансе — отказ выше был бы бессодержателен")
	}
	for _, a := range got {
		if a.InstanceID != "ins-mine" {
			t.Fatalf("вернулась привязка чужого инстанса %s", a.InstanceID)
		}
	}
}
