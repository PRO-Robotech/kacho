// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// own_sessions_lane_test.go — три глагола своих сессий (приёмка F8-S5 Р1,
// F8-S5-10; kacho#3061) на полосе сессии края.
//
// Решения записей: до подтверждения адреса — закрыты (служба держит то же, OS
// Р8), недоступность службы — ответ края (KA1): все три читают носитель живой
// сессии. Здесь судится ИСХОД этих решений через цепочку, каждое отрицание — в
// паре с близнецом, отличающимся одним фактом: отметкой подтверждения адреса
// либо записью, разрешающей ретрансляцию при неответе.
package middleware

import (
	"net/http"
	"testing"
)

// F8-S5-10 (а): неподтверждённая сессия на перечне своих сессий — отказ адреса
// края побайтно, цель формы не вызвана; близнец — та же сессия с отметкой
// подтверждения: цель формы вызвана ровно раз.
func TestOwnSessionLane_F8_S5_10_UnverifiedAddressIsRefusedVerifiedTwinReachesTheFormTarget(t *testing.T) {
	sessionsPath := ownSessionsWant["sessions"]

	rig := newAddressRig(t, unverifiedOwnSession())
	requireAddressRefusal(t, "F8-S5-10 (а) неподтверждённый адрес · sessions", rig.present(http.MethodGet, sessionsPath, true))
	if rig.reached[sessionsPath] != 0 || rig.relayed[RelayTargetForm] != 0 || rig.next.served != 0 {
		t.Fatalf("F8-S5-10 (а): неподтверждённая сессия прошла край — цель формы %d, слушатель формы %d, следующее звено %d",
			rig.reached[sessionsPath], rig.relayed[RelayTargetForm], rig.next.served)
	}

	twin := newAddressRig(t, liveOwnSession())
	rec := twin.present(http.MethodGet, sessionsPath, true)
	if rec.Code != http.StatusOK || !notAddressRefusal(rec) {
		t.Fatalf("F8-S5-10 (а) близнец с подтверждённым адресом: %d %s", rec.Code, rec.Body.String())
	}
	if twin.reached[sessionsPath] != 1 || twin.relayed[RelayTargetForm] != 1 || twin.next.served != 0 {
		t.Fatalf("F8-S5-10 (а) близнец: цель формы вызвана %d раз (ожидался 1), слушатель формы %d, следующее звено %d (ожидался 0) — путь обязан быть записью объявления",
			twin.reached[sessionsPath], twin.relayed[RelayTargetForm], twin.next.served)
	}
}

// F8-S5-10 (б): служба не ответила о сессии — ответ края (KA1) побайтно,
// носитель цел, цель формы не вызвана. Близнецы: тот же запрос при ответившей
// службе — ретранслирован; тот же режим на записи `login`
// (`relayWhenUnanswered`) — ретранслирован.
func TestOwnSessionLane_F8_S5_10_UnansweredServiceIsAnsweredByTheEdgeLoginTwinIsRelayed(t *testing.T) {
	sessionsPath := ownSessionsWant["sessions"]

	book, cut := f12Books()
	chain, relay := formChain(t, book, cut)
	if got := runFormVerb(chain, relay, formVerbRequest(sessionsPath, carrierA, false)); got.relayed != 1 || got.code != http.StatusOK {
		t.Fatalf("F8-S5-10 (б) близнец «служба отвечает»: получено %d %q, ретранслировано %d — путь обязан быть записью объявления",
			got.code, got.body, got.relayed)
	}

	for _, mode := range unansweredModes {
		book, cut := f12Books()
		mode.set(book, cut)
		chain, relay := formChain(t, book, cut)
		expectRefusedKept(t, "F8-S5-10 (б) "+mode.name+" · sessions", runFormVerb(chain, relay, formVerbRequest(sessionsPath, carrierA, false)))

		if got := runFormVerb(chain, relay, formVerbRequest(LoginLanePathLogin, carrierA, false)); got.relayed != 1 {
			t.Errorf("F8-S5-10 (б) близнец %s · login: запись с relayWhenUnanswered обязана ретранслироваться; получено %d %q, ретранслировано %d",
				mode.name, got.code, got.body, got.relayed)
		}
	}
}
