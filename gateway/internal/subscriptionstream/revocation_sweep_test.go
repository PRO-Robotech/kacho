// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionstream_test

// revocation_sweep_test.go — РЕЕСТР открытых потоков как предмет отзыва
// (kacho#1022).
//
// # Что здесь утверждается и что — в другом месте
//
// Здесь: поток учитывается под тем ключом, которым его назовёт отзыв, а
// неотзываемый поток на учёт не встаёт вовсе. Оба свойства принадлежат реестру и
// от того, КАК приехал отзыв, не зависят.
//
// Сквозной вопрос — «отзыв, приехавший перепросом, закрывает открытый поток» —
// ставит `revocation_poll_sweep_test.go` рядом.
//
// # Почему проб внутреннего глагола здесь больше НЕТ
//
// Их было четыре, и все утверждали об отзыве, приехавшем ПО ПРОВОДУ на
// внутренний слушатель края. Слушатель снят вместе со службой, ради которой жил
// (kacho#1024): направление развёрнуто, соединение открывает край. Пробы сняты
// вместе со своим предметом, а не вместе со свойством — радиус закрытия,
// отсечение пустого имени и идемпотентность промаха утверждаются теперь на
// оставшихся путях: радиус — ниже, на двух живых потоках; пустое имя и повтор —
// у читателя (`pkg/subjectchange`, `TestPolledRevocationNamesEachSubjectOnce`).

import (
	"testing"
	"time"

	subscriptionv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"

	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
	"github.com/PRO-Robotech/kacho/gateway/internal/subscriptionstream"
)

// openHeldStream открывает поток названного субъекта и ждёт, пока владелец его
// примет. Возвращает канал, закрывающийся вместе с потоком.
func openHeldStream(
	t *testing.T, h *subscriptionstream.Handler, principalType, principalID string,
) <-chan struct{} {
	t.Helper()
	r := request("owner=probe")
	r.Header.Set(principalmeta.HeaderPrincipalType, principalType)
	r.Header.Set(principalmeta.HeaderPrincipalID, principalID)

	finished := make(chan struct{})
	go func() {
		defer close(finished)
		serve(t, h, r)
	}()
	return finished
}

// waitStreams ждёт, пока ручка НАСЧИТАЕТ ровно столько открытых потоков.
//
// Ожидание по СОСТОЯНИЮ, а не паузой: пауза либо мала (проба падает не на своём
// предмете), либо велика (каждый прогон платит за худший случай).
func waitStreams(t *testing.T, h *subscriptionstream.Handler, want int64) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if h.Stats().Open == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("открытых потоков %d, ожидалось %d", h.Stats().Open, want)
}

// TestClosingOneSubjectLeavesTheNeighbourAlone — РАДИУС закрытия на ДВУХ живых
// потоках.
//
// Отрицание («чужой не задет») зеленело бы на устройстве, которое не закрывает
// НИКОГО, — поэтому положительный контроль стоит в той же пробе: отозванный
// закрыт, сосед жив и закрывается только своим отзывом.
//
// Двух живых потоков требует именно вторая половина: на одном она не отличает
// «радиус — один субъект» от «закрыли всех», потому что закрывать больше некого.
func TestClosingOneSubjectLeavesTheNeighbourAlone(t *testing.T) {
	held := &ownerStub{
		script:  []*subscriptionv1.SubscriptionMessage{openedMessage("p", false)},
		hold:    true,
		started: make(chan struct{}, startedDepth),
	}
	h := newHandler(t, held, func(c *subscriptionstream.Config) {
		c.StreamBudget = 60 * time.Second
		c.Heartbeat = 20 * time.Second
	})

	revoked := openHeldStream(t, h, "user", "usr-revoked")
	held.awaitStreams(t, 1)
	neighbour := openHeldStream(t, h, "service_account", "sva-neighbour")
	// Второй поток обязан ДОЙТИ до владельца прежде, чем придёт отзыв: иначе
	// «сосед не задет» означало бы «соседа ещё не было».
	waitStreams(t, h, 2)

	if n := h.CloseSubject("user:usr-revoked"); n != 1 {
		t.Fatalf("закрыто потоков %d, ожидался 1", n)
	}
	select {
	case <-revoked:
	case <-time.After(10 * time.Second):
		t.Fatal("отозванный поток не закрылся")
	}

	select {
	case <-neighbour:
		t.Fatal("отзыв одного субъекта закрыл поток другого — радиус обязан быть один субъект")
	case <-time.After(500 * time.Millisecond):
	}

	if n := h.CloseSubject("service_account:sva-neighbour"); n != 1 {
		t.Fatalf("закрыто потоков соседа %d, ожидался 1", n)
	}
	select {
	case <-neighbour:
	case <-time.After(10 * time.Second):
		t.Fatal("поток соседа не закрылся своим отзывом — значит первая половина пробы ничего не доказала")
	}
}

// TestUnnameableSubjectIsRefusedBeforeItIsRegistered — ПУНКТ 4 предиката задачи.
//
// Поток учитывается под ключом «тип:идентификатор». Если тип не называет
// тенантного субъекта, такой ключ не сможет назвать НИ ОДИН отзыв: iam говорит о
// субъектах модели прав («user:…», «service_account:…»), и `»:usr-x»` либо
// `«workload:wid-x»` в этом словаре не существует. Поток под таким ключом
// закрыть нечем — то есть он неотзываем ПО ПОСТРОЕНИЮ.
//
// Отсекается это безусловно и ДО постановки на учёт, а не «когда провязан
// закрыватель»: иначе отзываемость потока становится свойством посадки.
func TestUnnameableSubjectIsRefusedBeforeItIsRegistered(t *testing.T) {
	for _, tc := range []struct {
		name          string
		principalType string
		principalID   string
		wantStatus    int
	}{
		// Безымянный — `401`. Названный, но не тенантный, — `403`: он
		// аутентифицирован, и `401` посылал бы его аутентифицироваться заново,
		// то есть отказ не восстанавливал бы следующий шаг.
		{name: "вызывающий не назван вовсе", principalType: "user", principalID: "", wantStatus: 401},
		{name: "тип не назван", principalType: "", principalID: "usr-x", wantStatus: 403},
		{name: "тип вне словаря модели", principalType: "workload", principalID: "wid-x", wantStatus: 403},
		{name: "тип написан псевдонимом", principalType: "sva", principalID: "sva-x", wantStatus: 403},
		{name: "идентификатор двигает границу типа", principalType: "user", principalID: "a:b", wantStatus: 403},
		{name: "идентификатор ссылается на набор", principalType: "user", principalID: "a#member", wantStatus: 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			held := &ownerStub{
				script:  []*subscriptionv1.SubscriptionMessage{openedMessage("p", false)},
				hold:    true,
				started: make(chan struct{}, startedDepth),
			}
			h := newHandler(t, held)
			r := request("owner=probe")
			r.Header.Set(principalmeta.HeaderPrincipalType, tc.principalType)
			r.Header.Set(principalmeta.HeaderPrincipalID, tc.principalID)
			rec := serve(t, h, r)
			if rec.Code != tc.wantStatus {
				t.Fatalf("ответ %d, ожидался %d", rec.Code, tc.wantStatus)
			}
			if got := h.Stats().Open; got != 0 {
				t.Fatalf("на учёте %d потоков — неотзываемый поток был зарегистрирован", got)
			}
		})
	}
}

// TestNameableSubjectIsAdmitted — положительный контроль к предыдущей пробе.
//
// Без него отсечение зеленело бы на ручке, отвергающей ВСЕХ, и «неотзываемых
// потоков нет» означало бы «потоков нет».
func TestNameableSubjectIsAdmitted(t *testing.T) {
	for _, principalType := range []string{"user", "service_account"} {
		t.Run(principalType, func(t *testing.T) {
			held := &ownerStub{
				script:  []*subscriptionv1.SubscriptionMessage{openedMessage("p", false)},
				hold:    true,
				started: make(chan struct{}, startedDepth),
			}
			h := newHandler(t, held, func(c *subscriptionstream.Config) {
				c.StreamBudget = 60 * time.Second
				c.Heartbeat = 20 * time.Second
			})
			done := openHeldStream(t, h, principalType, "id-probe")
			held.awaitStreams(t, 1)
			// Ключ учёта — тот же субъект модели прав, которым назовёт его отзыв.
			if n := h.CloseSubject(principalType + ":id-probe"); n != 1 {
				t.Fatalf("закрыто %d потоков по ключу «%s:id-probe» — ключ учёта разошёлся с именем отзыва",
					n, principalType)
			}
			<-done
		})
	}
}
