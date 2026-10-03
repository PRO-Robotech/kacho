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

	"github.com/PRO-Robotech/kacho/gateway/internal/e2e/ka1stand"
)

// KA1-01 — предъявитель нашей чеканки, авторитет молчит.
func TestKA1_01_OurIssuerTokenWhenOurAuthorityIsSilent(t *testing.T) {
	st := ka1stand.New(t, ka1stand.Options{})
	st.OurAuth.Q.Set(ka1stand.Silent)
	tok := st.OurToken(t, ka1stand.JTILive, nil)

	ka1stand.RequireUnavailable(t, "KA1-01 REST", st.REST(t, http.MethodGet, ka1stand.ListRoute, ka1stand.Bearer(tok)))
	ka1stand.RequireNativeUnavailable(t, "KA1-01 нативная", st.GRPC(t, ka1stand.PingMethod, tok, nil))
}

// KA1-02 — предъявитель чужой записи, запись отзыва молчит. Имя пробы — из
// предиката снятия kacho#2728 (п. 1): проба сквозная через полосу записи отзыва
// и сравнивает её ответ с ответом полосы авторитета.
func TestBearerLaneRefusesWhenOurOwnRevocationSourceIsSilent(t *testing.T) {
	st := ka1stand.New(t, ka1stand.Options{})
	st.Ident.RevokedQ.Set(ka1stand.Silent)
	tok := st.LegacyToken(t, ka1stand.JTILive)

	got := st.REST(t, http.MethodGet, ka1stand.ListRoute, ka1stand.Bearer(tok))
	ka1stand.RequireUnavailable(t, "KA1-02 REST", got)
	ka1stand.RequireNativeUnavailable(t, "KA1-02 нативная", st.GRPC(t, ka1stand.PingMethod, tok, nil))

	// Побайтово равен ответу KA1-01 (правило сравнения Р2).
	st.OurAuth.Q.Set(ka1stand.Silent)
	ours := st.REST(t, http.MethodGet, ka1stand.ListRoute, ka1stand.Bearer(st.OurToken(t, ka1stand.JTILive, nil)))
	if !got.Same(ours) {
		t.Errorf("KA1-02: ответ полосы записи отзыва отличается от ответа полосы авторитета\n  запись:    %s\n  авторитет: %s", got, ours)
	}
}

// KA1-03 (б), (в) — наша сессия, служба не ответила о сессии: UNAVAILABLE,
// UNIMPLEMENTED. Строка (а) — молчание в пределах бюджета — в
// TestKA1_03a_SessionQuestionSilentIsAnsweredWithinBudget (стадия S2).
func TestKA1_03_OurSessionWhenTheSessionQuestionIsUnanswered(t *testing.T) {
	for _, s := range []ka1stand.State{ka1stand.Unavailable, ka1stand.Unimplemented} {
		t.Run(s.String(), func(t *testing.T) {
			st := ka1stand.New(t, ka1stand.Options{})
			st.Ident.SessionQ.Set(s)
			ka1stand.RequireUnavailable(t, "KA1-03 "+s.String(),
				st.REST(t, http.MethodGet, ka1stand.ListRoute, ka1stand.SessionCarrier(ka1stand.SessionLive)))
		})
	}
}

// KA1-04 (б) — сессия жива, служба не ответила об отсечке (UNAVAILABLE).
// Строка (а) — в TestKA1_04a_CutoffQuestionSilentIsAnsweredWithinBudget (S2).
func TestKA1_04_OurSessionWhenTheCutoffQuestionIsUnanswered(t *testing.T) {
	st := ka1stand.New(t, ka1stand.Options{})
	st.Ident.CutoffQ.Set(ka1stand.Unavailable)
	ka1stand.RequireUnavailable(t, "KA1-04 UNAVAILABLE",
		st.REST(t, http.MethodGet, ka1stand.ListRoute, ka1stand.SessionCarrier(ka1stand.SessionLive)))
}

// KA1-05 — базовое удостоверение, служба молчит о годности.
func TestKA1_05_BasicCredentialWhenTheServiceIsSilent(t *testing.T) {
	st := ka1stand.New(t, ka1stand.Options{})
	st.Ident.BasicQ.Set(ka1stand.Silent)

	ka1stand.RequireUnavailable(t, "KA1-05 REST", st.REST(t, http.MethodGet, ka1stand.ListRoute, ka1stand.Bearer(st.BasicGood)))
	ka1stand.RequireNativeUnavailable(t, "KA1-05 нативная", st.GRPC(t, ka1stand.PingMethod, st.BasicGood, nil))
}

// KA1-06 — положительные близнецы KA1-01…05: авторитет и каждый вопрос службы
// отвечают. Отличие — ровно один факт: ответил ли авторитет.
func TestKA1_06_TwinsWhenTheAuthorityAnswersLive(t *testing.T) {
	st := ka1stand.New(t, ka1stand.Options{})
	ours := st.OurToken(t, ka1stand.JTILive, nil)
	legacy := st.LegacyToken(t, ka1stand.JTILive)

	rows := []struct {
		name    string
		present ka1stand.Presented
		native  string
	}{
		{"KA1-01 близнец", ka1stand.Bearer(ours), ours},
		{"KA1-02 близнец", ka1stand.Bearer(legacy), legacy},
		{"KA1-03 близнец", ka1stand.SessionCarrier(ka1stand.SessionLive), ""},
		{"KA1-04 близнец", ka1stand.SessionCarrier(ka1stand.SessionLive), ""},
		{"KA1-05 близнец", ka1stand.Bearer(st.BasicGood), st.BasicGood},
	}
	for _, r := range rows {
		got := st.REST(t, http.MethodGet, ka1stand.ListRoute, r.present)
		if got.Status != http.StatusOK || len(got.Header.Values("Set-Cookie")) != 0 {
			t.Errorf("%s: ожидался 200 без Set-Cookie\n  получено: %s", r.name, got)
		}
		if r.native != "" {
			if n := st.GRPC(t, ka1stand.PingMethod, r.native, nil); n.St.Code() != 0 {
				t.Errorf("%s: нативная поверхность ожидала OK\n  получено: %s", r.name, n)
			}
		}
	}
}

// KA1-07 (UNAVAILABLE, `отвечает`) — сравнение трёх полос на ОДНОМ состоянии
// авторитета. Состояние `молчит` — в TestKA1_07_SilentStateAgreesWithinBudget (S2).
func TestKA1_07_ThreeLanesAgreeOnOneAuthorityState(t *testing.T) {
	for _, s := range []ka1stand.State{ka1stand.Answers, ka1stand.Unavailable} {
		t.Run(s.String(), func(t *testing.T) { ka1LanesAgree(t, s) })
	}
}

// ka1LanesAgree — одна величина задаёт состояние И авторитета нашей чеканки, И
// каждого вопроса службы; на ней подаются три полосы.
func ka1LanesAgree(t *testing.T, s ka1stand.State) {
	t.Helper()
	st := ka1stand.New(t, ka1stand.Options{})
	tok := st.OurToken(t, ka1stand.JTILive, nil)
	for _, q := range []*ka1stand.Question{st.OurAuth.Q, st.Ident.SessionQ, st.Ident.CutoffQ, st.Ident.RevokedQ, st.Ident.BasicQ} {
		q.Set(s)
	}
	lanes := []struct {
		name    string
		present ka1stand.Presented
	}{
		{"предъявитель нашей чеканки", ka1stand.Bearer(tok)},
		{"носитель нашей сессии", ka1stand.SessionCarrier(ka1stand.SessionLive)},
		{"базовое удостоверение", ka1stand.Bearer(st.BasicGood)},
	}
	shots := make([]ka1stand.Shot, len(lanes))
	for i, l := range lanes {
		shots[i] = st.REST(t, http.MethodGet, ka1stand.ListRoute, l.present)
		switch s {
		case ka1stand.Answers:
			if shots[i].Status != http.StatusOK {
				t.Errorf("KA1-07 %s / %s: ожидался 200\n  получено: %s", s, l.name, shots[i])
			}
		default:
			ka1stand.RequireUnavailable(t, "KA1-07 "+s.String()+" / "+l.name, shots[i])
			if s == ka1stand.Silent && (shots[i].TimedOut || shots[i].Elapsed >= 2500*time.Millisecond) {
				t.Errorf("KA1-07 %s / %s: ответ не пришёл раньше 2500ms (%s)", s, l.name, shots[i].Elapsed)
			}
		}
	}
	for i := 1; i < len(shots); i++ {
		if shots[0].Status != shots[i].Status || string(shots[0].Body) != string(shots[i].Body) {
			t.Errorf("KA1-07 %s: полосы разошлись по статусу или телу\n  %s: %s\n  %s: %s",
				s, lanes[0].name, shots[0], lanes[i].name, shots[i])
		}
	}
}
