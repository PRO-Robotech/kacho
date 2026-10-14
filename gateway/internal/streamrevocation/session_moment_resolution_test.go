// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package streamrevocation_test

// session_moment_resolution_test.go — ДВА ЧИТАТЕЛЯ ОДНОЙ ОТСЕЧКИ судят одну
// сессию одинаково (kacho#2690, kaname#176).
//
// # Предмет
//
// Отсечка субъекта приходит в разрешении хранилища (микросекунды), и выход из
// второй сессии датирует её на одну единицу НИЖЕ первой аутентификации (Ф3
// §4.1 п.19). Путь запроса сравнивает с ней момент из ответа службы о сессии —
// в том же разрешении (замок Ф3-16). Перепрос открытых потоков обязан
// сравнивать ТУ ЖЕ пару: иначе сессия S1 (t₁, µs ≠ 0) при отсечке t₁ − 1 µs на
// пути запроса годна, а её открытый поток закрыт — и закрыт штатно, на каждом
// выходе из второй сессии в ту же секунду.
//
// # Почему через НАСТОЯЩУЮ полосу приёма
//
// Поток здесь открывает полоса нашей сессии (`AuthInterceptor.HTTP`) по носителю,
// а не проба, выставившая заголовки руками: заголовок, сочинённый пробой, несёт
// то разрешение, которое выбрала проба, а не то, которое производит край. Ровно
// так расхождение и прожило незамеченным — соседние пробы кормили сметатель
// моментом, которого полоса не производит.
//
// Обе величины сравнения пересекают НАСТОЯЩУЮ сериализацию ответа службы
// (`timestamppb` поверх gRPC) и читаются настоящим адаптером: значение отсечки
// не подставляется в сравнение мимо провода.
//
// # Граница
//
// Включительная по контракту (F4d-22): сессия, аутентифицировавшаяся РОВНО в
// момент отсечки, недействительна. Три соседних значения отсечки — на единицу
// раньше момента, ровно в него и на единицу позже — меняют против близнеца
// ровно один факт: саму отсечку.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
	"github.com/PRO-Robotech/kacho/gateway/internal/subscriptionstream"
)

// sessionMoment — момент аутентификации S1 с НЕНУЛЕВОЙ микросекундной частью.
// Целая секунда сделала бы пробу слепой: усечение до секунды на ней тождественно.
var sessionMoment = time.Date(2026, 8, 29, 11, 0, 0, 123456000, time.UTC)

// storageUnit — единица разрешения хранилища, в которой датируется отсечка.
const storageUnit = time.Microsecond

// openStreamThroughSessionLane открывает поток запросом с носителем нашей
// сессии через настоящую полосу приёма края, смонтированную над проекцией.
// Полоса спрашивает тот же адаптер, что сметатель: о сессии и об отсечке.
//
// Отказ полосы — не «поток не открылся» молча: проба называет ответ края.
func openStreamThroughSessionLane(t *testing.T, s *stand, bearer string) <-chan struct{} {
	t.Helper()
	lane := middleware.NewAuthInterceptor(middleware.AuthModeProduction, "", nil, quiet()).
		WithHumanSession(s.revocations).
		WithSessionCutoffCheck(s.revocations, 0)
	h := lane.HTTP(s.projection)

	r := httptest.NewRequest(http.MethodGet, subscriptionstream.Path+"?owner=probe", nil)
	r.AddCookie(&http.Cookie{Name: middleware.OurSessionCarrierName, Value: bearer})

	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.ServeHTTP(rec, r)
	}()
	select {
	case <-s.owner.started:
	case <-done:
		t.Fatalf("полоса приёма не открыла поток: край ответил %d %q — предпосылка пробы "+
			"(сессия годна на пути запроса) не выполнена, вердикта о перепросе нет",
			rec.Code, rec.Body.String())
	case <-time.After(10 * time.Second):
		t.Fatal("поток не открылся за срок, и полоса не ответила — вердикта о перепросе нет")
	}
	return done
}

// TestOpenStreamIsJudgedByTheSessionMomentAtStorageResolution — проба уровня
// цепочки: −1 / 0 / +1 единица хранилища вокруг момента сессии.
func TestOpenStreamIsJudgedByTheSessionMomentAtStorageResolution(t *testing.T) {
	cases := []struct {
		name     string
		cutoff   time.Time
		wantOpen bool
		why      string
	}{
		{
			// Законный близнец целой секундой раньше: годен при любом разрешении
			// момента. Его зелёное отличает «проба падает на чём угодно» от
			// «проба падает на разрешении».
			name:     "cutoff a whole second before the session stays open",
			cutoff:   sessionMoment.Add(-time.Second),
			wantOpen: true,
			why:      "поток закрыт отсечкой на секунду раньше входа — перепрос закрывает не по отсечке",
		},
		{
			// Ф3 §4.1 п.19: выход из второй сессии той же секунды.
			name:     "cutoff one storage unit before the session stays open",
			cutoff:   sessionMoment.Add(-storageUnit),
			wantOpen: true,
			why: "поток сессии, ГОДНОЙ на пути запроса, закрыт перепросом: отсечка на единицу хранилища " +
				"раньше момента аутентификации — значит перепрос сравнивает момент в другом разрешении, " +
				"чем путь запроса, и выход из второй сессии той же секунды рвёт потоки первой",
		},
		{
			name:     "cutoff exactly at the session moment closes the stream",
			cutoff:   sessionMoment,
			wantOpen: false,
			why:      "поток пережил отсечку, равную моменту входа: граница включительная по контракту (F4d-22)",
		},
		{
			name:     "cutoff one storage unit after the session closes the stream",
			cutoff:   sessionMoment.Add(storageUnit),
			wantOpen: false,
			why:      "поток пережил отсечку позже своего входа — отзыв до открытого соединения не доехал",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			const (
				bearer = "brw-moment"
				userID = "usr00000000000000009"
			)
			s := newStand(t, nil)
			s.human.putAt(bearer, userID, sessionMoment)

			// Годная отсечка ставится ДО открытия: её обязан пропустить и путь
			// запроса — иначе предмет «два читателя судят одинаково» не поставлен.
			// Отвергающая ставится ПОСЛЕ: путь запроса отказал бы потоку открыться,
			// и «закрыт перепросом» стало бы неотличимо от «не открывался».
			if tc.wantOpen {
				s.authority.setCutoff(userID, tc.cutoff)
			}
			done := openStreamThroughSessionLane(t, s, bearer)
			if !tc.wantOpen {
				s.authority.setCutoff(userID, tc.cutoff)
			}

			// Путь запроса тоже спрашивает об отсечке — поэтому считается прирост
			// за перепрос, а не наличие вопроса вообще.
			_, before := s.authority.asked()
			s.sweeper.Sweep(context.Background())
			if _, after := s.authority.asked(); len(after) <= len(before) {
				t.Fatal("перепрос не спросил про отсечку НИ РАЗУ — исход здесь означал бы «контроль " +
					"не исполнялся», а не вердикт о моменте")
			}
			if tc.wantOpen {
				aliveFor(t, done, 300*time.Millisecond, tc.why)
				return
			}
			closedWithin(t, done, 10*time.Second, tc.why)
		})
	}
}
