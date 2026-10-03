// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// ka1_silence_test.go — приёмка KA1, Предмет 1 (kacho#2728, Р1): наш авторитет
// не ответил → один и тот же ответ `503` на всех полосах, носитель цел.
//
// Сценарии KA1-01…KA1-07. Каждое отрицание стоит рядом со своим близнецом
// (KA1-06, состояние `отвечает` в KA1-07), отличающимся ровно одним фактом —
// ответил ли авторитет: читатель, отвечающий отказом ВСЕГДА, прошёл бы
// отрицательные строки целиком.
package e2e_test

import (
	"net/http"
	"testing"
	"time"
)

// KA1-01 — предъявитель нашей чеканки, авторитет молчит.
func TestKA1_01_OurIssuerTokenWhenOurAuthorityIsSilent(t *testing.T) {
	st := newKA1Stand(t, ka1Options{})
	st.ourAuth.q.set(ka1Silent)
	tok := st.ourToken(t, ka1JTILive, nil)

	requireUnavailable(t, "KA1-01 REST", st.rest(t, http.MethodGet, ka1ListRoute, bearer(tok)))
	requireNativeUnavailable(t, "KA1-01 нативная", st.grpc(t, ka1PingMethod, tok, nil))
}

// KA1-02 — предъявитель чужой записи, запись отзыва молчит. Имя пробы — из
// предиката снятия kacho#2728 (п. 1): проба сквозная через полосу записи отзыва
// и сравнивает её ответ с ответом полосы авторитета.
func TestBearerLaneRefusesWhenOurOwnRevocationSourceIsSilent(t *testing.T) {
	st := newKA1Stand(t, ka1Options{})
	st.ident.isRevoked.set(ka1Silent)
	tok := st.legacyToken(t, ka1JTILive)

	got := st.rest(t, http.MethodGet, ka1ListRoute, bearer(tok))
	requireUnavailable(t, "KA1-02 REST", got)
	requireNativeUnavailable(t, "KA1-02 нативная", st.grpc(t, ka1PingMethod, tok, nil))

	// Побайтово равен ответу KA1-01 (правило сравнения Р2).
	st.ourAuth.q.set(ka1Silent)
	ours := st.rest(t, http.MethodGet, ka1ListRoute, bearer(st.ourToken(t, ka1JTILive, nil)))
	if !got.same(ours) {
		t.Errorf("KA1-02: ответ полосы записи отзыва отличается от ответа полосы авторитета\n  запись:    %s\n  авторитет: %s", got, ours)
	}
}

// KA1-03 (б), (в) — наша сессия, служба не ответила о сессии: UNAVAILABLE,
// UNIMPLEMENTED. Строка (а) — молчание в пределах бюджета — в
// TestKA1_03a_SessionQuestionSilentIsAnsweredWithinBudget (стадия S2).
func TestKA1_03_OurSessionWhenTheSessionQuestionIsUnanswered(t *testing.T) {
	for _, s := range []ka1State{ka1Unavailable, ka1Unimplemented} {
		t.Run(s.String(), func(t *testing.T) {
			st := newKA1Stand(t, ka1Options{})
			st.ident.resolve.set(s)
			requireUnavailable(t, "KA1-03 "+s.String(),
				st.rest(t, http.MethodGet, ka1ListRoute, sessionCarrier(ka1SessionLive)))
		})
	}
}

// KA1-04 (б) — сессия жива, служба не ответила об отсечке (UNAVAILABLE).
// Строка (а) — в TestKA1_04a_CutoffQuestionSilentIsAnsweredWithinBudget (S2).
func TestKA1_04_OurSessionWhenTheCutoffQuestionIsUnanswered(t *testing.T) {
	st := newKA1Stand(t, ka1Options{})
	st.ident.cutoff.set(ka1Unavailable)
	requireUnavailable(t, "KA1-04 UNAVAILABLE",
		st.rest(t, http.MethodGet, ka1ListRoute, sessionCarrier(ka1SessionLive)))
}

// KA1-05 — базовое удостоверение, служба молчит о годности.
func TestKA1_05_BasicCredentialWhenTheServiceIsSilent(t *testing.T) {
	st := newKA1Stand(t, ka1Options{})
	st.ident.basic.set(ka1Silent)

	requireUnavailable(t, "KA1-05 REST", st.rest(t, http.MethodGet, ka1ListRoute, bearer(st.basicGood)))
	requireNativeUnavailable(t, "KA1-05 нативная", st.grpc(t, ka1PingMethod, st.basicGood, nil))
}

// KA1-06 — положительные близнецы KA1-01…05: авторитет и каждый вопрос службы
// отвечают. Отличие — ровно один факт: ответил ли авторитет.
func TestKA1_06_TwinsWhenTheAuthorityAnswersLive(t *testing.T) {
	st := newKA1Stand(t, ka1Options{})
	ours := st.ourToken(t, ka1JTILive, nil)
	legacy := st.legacyToken(t, ka1JTILive)

	rows := []struct {
		name    string
		present ka1Presented
		native  string
	}{
		{"KA1-01 близнец", bearer(ours), ours},
		{"KA1-02 близнец", bearer(legacy), legacy},
		{"KA1-03 близнец", sessionCarrier(ka1SessionLive), ""},
		{"KA1-04 близнец", sessionCarrier(ka1SessionLive), ""},
		{"KA1-05 близнец", bearer(st.basicGood), st.basicGood},
	}
	for _, r := range rows {
		got := st.rest(t, http.MethodGet, ka1ListRoute, r.present)
		if got.status != http.StatusOK || len(got.header.Values("Set-Cookie")) != 0 {
			t.Errorf("%s: ожидался 200 без Set-Cookie\n  получено: %s", r.name, got)
		}
		if r.native != "" {
			if n := st.grpc(t, ka1PingMethod, r.native, nil); n.st.Code() != 0 {
				t.Errorf("%s: нативная поверхность ожидала OK\n  получено: %s", r.name, n)
			}
		}
	}
}

// KA1-07 (UNAVAILABLE, `отвечает`) — сравнение трёх полос на ОДНОМ состоянии
// авторитета. Состояние `молчит` — в TestKA1_07_SilentStateAgreesWithinBudget (S2).
func TestKA1_07_ThreeLanesAgreeOnOneAuthorityState(t *testing.T) {
	for _, s := range []ka1State{ka1Answers, ka1Unavailable} {
		t.Run(s.String(), func(t *testing.T) { ka1LanesAgree(t, s) })
	}
}

// ka1LanesAgree — одна величина задаёт состояние И авторитета нашей чеканки, И
// каждого вопроса службы; на ней подаются три полосы.
func ka1LanesAgree(t *testing.T, s ka1State) {
	t.Helper()
	st := newKA1Stand(t, ka1Options{})
	tok := st.ourToken(t, ka1JTILive, nil)
	for _, q := range []*ka1Question{st.ourAuth.q, st.ident.resolve, st.ident.cutoff, st.ident.isRevoked, st.ident.basic} {
		q.set(s)
	}
	lanes := []struct {
		name    string
		present ka1Presented
	}{
		{"предъявитель нашей чеканки", bearer(tok)},
		{"носитель нашей сессии", sessionCarrier(ka1SessionLive)},
		{"базовое удостоверение", bearer(st.basicGood)},
	}
	shots := make([]ka1Shot, len(lanes))
	for i, l := range lanes {
		shots[i] = st.rest(t, http.MethodGet, ka1ListRoute, l.present)
		switch s {
		case ka1Answers:
			if shots[i].status != http.StatusOK {
				t.Errorf("KA1-07 %s / %s: ожидался 200\n  получено: %s", s, l.name, shots[i])
			}
		default:
			requireUnavailable(t, "KA1-07 "+s.String()+" / "+l.name, shots[i])
			if s == ka1Silent && (shots[i].timedOut || shots[i].elapsed >= 2500*time.Millisecond) {
				t.Errorf("KA1-07 %s / %s: ответ не пришёл раньше 2500ms (%s)", s, l.name, shots[i].elapsed)
			}
		}
	}
	for i := 1; i < len(shots); i++ {
		if shots[0].status != shots[i].status || string(shots[0].body) != string(shots[i].body) {
			t.Errorf("KA1-07 %s: полосы разошлись по статусу или телу\n  %s: %s\n  %s: %s",
				s, lanes[0].name, shots[0], lanes[i].name, shots[i])
		}
	}
}
