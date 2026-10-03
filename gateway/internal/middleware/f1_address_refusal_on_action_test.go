// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// f1_address_refusal_on_action_test.go — вторые половины сценариев Ф1-06 и
// Ф1-23 приёмки Ф1 службы доступа
// (`kaname:docs/engineering/acceptance/login-session-and-credentials-are-our-contract.md`,
// строка Ф6 таблицы производителей §5 — фаза kacho#1272) на крае платформы.
//
// # Что утверждается
//
// Рубеж подтверждённого адреса стоит на ДЕЙСТВИИ, а не на входе (исход разбора
// kacho#1272). Сценарий утверждается ЦЕПОЧКОЙ одного носителя, а не
// отдельными обращениями:
//
//  1. вход (Ф1-06) либо регистрация (Ф1-23) неподтверждённого состоялись —
//     край ретранслировал глагол и отдал выданный службой носитель;
//  2. действие платформы с этим носителем отвергнуто отказом, который
//     НАЗЫВАЕТ следующий шаг — парой «код + reason-token», а не прозой;
//  3. названный шаг исполним тем же носителем: глаголы подтверждения
//     ретранслируются, а не отвергаются тем же отказом;
//  4. после подтверждения то же действие тем же носителем доходит дальше
//     края.
//
// Отдельные звенья держат пробы F6b (own_session_address_gate_test.go);
// здесь — то, чего ни одна из них не утверждает: что отказ ведёт к шагу,
// который тот же человек тем же носителем может сделать, и что шаг снимает
// отказ. Отказ, называющий шаг, который закрыт тем же рубежом, — тупик, а не
// следующий шаг.
//
// У каждого отрицания — близнец, отличающийся одним фактом: подтверждённостью
// адреса на момент выдачи носителя.
package middleware

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// f1IssuedCarrier — носитель, который дублёр службы выдаёт на входе и
// регистрации: значение отличимо от носителей прочих проб пакета.
const f1IssuedCarrier = "opaque-f1-issued"

// f1Action — действие платформы, требующее подтверждённого адреса.
const f1Action = "/vpc/v1/networks"

// f1Rig — полоса сессии края над mux, где дублёр службы на глаголах входа и
// регистрации выдаёт носитель, а на глаголе предъявления кода подтверждает
// адрес той же сессии. Дублёр не снисходительнее службы: подтверждает только
// предъявление кода с носителем той сессии, чей адрес подтверждается.
type f1Rig struct {
	reader  *fakeHumanSession
	next    *countingNext
	reached map[string]int
	chain   http.Handler
}

func newF1Rig(t *testing.T, verifiedAtIssue bool) *f1Rig {
	t.Helper()
	sess := liveOwnSession()
	sess.EmailVerified = verifiedAtIssue
	reader := &fakeHumanSession{found: true, sess: sess}
	cut := &fakeCutoff{}
	a := NewAuthInterceptor(AuthModeDev, "", cutoffLookup{}, slog.New(slog.NewTextHandler(io.Discard, nil))).
		WithHumanSession(reader).
		WithSessionCutoffCheck(cut, time.Hour)
	mux := http.NewServeMux()
	NewSessionIdentityHandler(slog.New(slog.NewTextHandler(io.Discard, nil))).
		WithHumanSession(reader).
		WithSessionCutoff(cut).
		WithAdminChecker(&countingAdmin{}).
		Register(mux)
	reached := map[string]int{}
	issue := func(path string) {
		mux.HandleFunc(path, func(w http.ResponseWriter, _ *http.Request) {
			reached[path]++
			http.SetCookie(w, &http.Cookie{Name: OurSessionCarrierName, Value: f1IssuedCarrier, Path: "/", HttpOnly: true, Secure: true})
			w.WriteHeader(http.StatusOK)
		})
	}
	issue(LoginLanePathLogin)
	issue(LoginLanePathRegister)
	mux.HandleFunc(LoginLanePathVerifyEmail, func(w http.ResponseWriter, _ *http.Request) {
		reached[LoginLanePathVerifyEmail]++
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc(LoginLanePathVerifyEmailConfirm, func(w http.ResponseWriter, r *http.Request) {
		reached[LoginLanePathVerifyEmailConfirm]++
		c, err := r.Cookie(OurSessionCarrierName)
		if err != nil || c.Value != f1IssuedCarrier {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		reader.sess.EmailVerified = true
		w.WriteHeader(http.StatusOK)
	})
	next := &countingNext{}
	mux.Handle("/", next)
	return &f1Rig{reader: reader, next: next, reached: reached, chain: a.HTTP(mux)}
}

// issuedCarrier — носитель, отданный клиенту ответом глагола; пусто — не отдан.
func issuedCarrier(rec *httptest.ResponseRecorder) string {
	for _, c := range rec.Result().Cookies() {
		if c.Name == OurSessionCarrierName && c.MaxAge >= 0 {
			return c.Value
		}
	}
	return ""
}

func (r *f1Rig) call(method, path, carrier string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if carrier != "" {
		req = withOurCarrier(req, carrier)
	}
	return serve(r.chain, req)
}

// f1Refusal — отказ в форме `google.rpc.Status` на REST-поверхности.
type f1Refusal struct {
	Code    int `json:"code"`
	Details []struct {
		Type   string `json:"@type"`
		Reason string `json:"reason"`
		Domain string `json:"domain"`
	} `json:"details"`
}

// requireRefusalNamesTheNextStep — отказ на действии называет следующий шаг:
// 403 и пара «PERMISSION_DENIED + reason-token подтверждения адреса» с доменом
// службы. Проза сообщения не утверждается — она не парсится.
func requireRefusalNamesTheNextStep(t *testing.T, where string, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusForbidden {
		t.Fatalf("%s: действие неподтверждённого обязано быть отвергнуто 403, получено %d %s", where, rec.Code, rec.Body.String())
	}
	var st f1Refusal
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatalf("%s: тело отказа не разбирается как google.rpc.Status: %v; тело %s", where, err, rec.Body.String())
	}
	if st.Code != 7 {
		t.Fatalf("%s: код отказа %d, ожидался 7 (PERMISSION_DENIED); тело %s", where, st.Code, rec.Body.String())
	}
	if len(st.Details) != 1 || st.Details[0].Reason != "EMAIL_NOT_VERIFIED" || st.Details[0].Domain != "iam.kaname.cloud" {
		t.Fatalf("%s: отказ не называет следующий шаг — ожидалась ровно одна деталь ErrorInfo "+
			"reason=EMAIL_NOT_VERIFIED domain=iam.kaname.cloud; тело %s", where, rec.Body.String())
	}
}

// walkUnverified — цепочка Ф1 для неподтверждённого после глагола выдачи.
func walkUnverified(t *testing.T, issuingVerb string) {
	t.Helper()
	rig := newF1Rig(t, false)

	// 1. Глагол выдачи состоялся: ретранслирован, носитель отдан.
	rec := rig.call(http.MethodPost, issuingVerb, "")
	if rec.Code != http.StatusOK || rig.reached[issuingVerb] != 1 {
		t.Fatalf("1. %s неподтверждённого не состоялся: %d %s, ретранслирован %d", issuingVerb, rec.Code, rec.Body.String(), rig.reached[issuingVerb])
	}
	carrier := issuedCarrier(rec)
	if carrier != f1IssuedCarrier {
		t.Fatalf("1. %s: край не отдал выданный службой носитель (получено %q)", issuingVerb, carrier)
	}

	// 2. Действие платформы этим носителем — отказ, называющий следующий шаг.
	requireRefusalNamesTheNextStep(t, "2. действие после "+issuingVerb, rig.call(http.MethodPost, f1Action, carrier))
	if rig.next.served != 0 {
		t.Fatalf("2. действие неподтверждённого дошло до следующего звена %d раз", rig.next.served)
	}

	// 3. Названный шаг исполним тем же носителем.
	for _, step := range []string{LoginLanePathVerifyEmail, LoginLanePathVerifyEmailConfirm} {
		rec := rig.call(http.MethodPost, step, carrier)
		if rec.Code != http.StatusOK || rig.reached[step] != 1 {
			t.Fatalf("3. следующий шаг %s тем же носителем не исполним: %d %s, ретранслирован %d — "+
				"отказ называет шаг, закрытый тем же рубежом", step, rec.Code, rec.Body.String(), rig.reached[step])
		}
	}
	if !rig.reader.sess.EmailVerified {
		t.Fatal("3. предпосылка: предъявление кода тем же носителем обязано подтвердить адрес сессии")
	}

	// 4. После подтверждения то же действие тем же носителем доходит дальше края.
	if rec := rig.call(http.MethodPost, f1Action, carrier); rec.Code == http.StatusForbidden || rig.next.served != 1 {
		t.Fatalf("4. после подтверждения действие не дошло до следующего звена: %d %s, дошло %d", rec.Code, rec.Body.String(), rig.next.served)
	}
}

// walkVerifiedTwin — близнец: тот же глагол выдачи и то же действие, адрес
// подтверждён к моменту выдачи — действие доходит сразу, без шага.
func walkVerifiedTwin(t *testing.T, issuingVerb string) {
	t.Helper()
	rig := newF1Rig(t, true)
	rec := rig.call(http.MethodPost, issuingVerb, "")
	carrier := issuedCarrier(rec)
	if rec.Code != http.StatusOK || carrier != f1IssuedCarrier {
		t.Fatalf("близнец: %s не состоялся: %d, носитель %q", issuingVerb, rec.Code, carrier)
	}
	if rec := rig.call(http.MethodPost, f1Action, carrier); rec.Code == http.StatusForbidden || rig.next.served != 1 {
		t.Fatalf("близнец: действие подтверждённого не дошло до следующего звена: %d %s, дошло %d", rec.Code, rec.Body.String(), rig.next.served)
	}
	if n := rig.reached[LoginLanePathVerifyEmail] + rig.reached[LoginLanePathVerifyEmailConfirm]; n != 0 {
		t.Fatalf("близнец: шаг подтверждения исполнялся %d раз — близнец отличается не одним фактом", n)
	}
}

// TestF1_06_LoginOfTheUnverifiedSucceedsAndTheActionRefusalNamesAnExecutableStep —
// Ф1-06: вход неподтверждённого состоялся, действие отвергнуто отказом,
// называющим шаг, шаг исполним тем же носителем и снимает отказ.
func TestF1_06_LoginOfTheUnverifiedSucceedsAndTheActionRefusalNamesAnExecutableStep(t *testing.T) {
	walkUnverified(t, LoginLanePathLogin)
}

// TestF1_06_Twin_VerifiedLoginReachesTheActionAtOnce — близнец Ф1-06.
func TestF1_06_Twin_VerifiedLoginReachesTheActionAtOnce(t *testing.T) {
	walkVerifiedTwin(t, LoginLanePathLogin)
}

// TestF1_23_SessionIssuedByRegistrationIsValidAndTheActionRefusalNamesAnExecutableStep —
// Ф1-23: сессия, выданная регистрацией неподтверждённого, годна («кто я»
// отвечает ею), действие отвергнуто отказом, называющим исполнимый шаг.
func TestF1_23_SessionIssuedByRegistrationIsValidAndTheActionRefusalNamesAnExecutableStep(t *testing.T) {
	rig := newF1Rig(t, false)
	rec := rig.call(http.MethodPost, LoginLanePathRegister, "")
	carrier := issuedCarrier(rec)
	if rec.Code != http.StatusOK || carrier != f1IssuedCarrier {
		t.Fatalf("регистрация не выдала носитель: %d, носитель %q", rec.Code, carrier)
	}
	me := rig.call(http.MethodGet, "/iam/v1/auth/me", carrier)
	if me.Code != http.StatusOK {
		t.Fatalf("выданная регистрацией сессия не годна: «кто я» ответил %d %s", me.Code, me.Body.String())
	}
	var who struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
		Session struct {
			EmailVerified bool `json:"emailVerified"`
		} `json:"session"`
	}
	if err := json.Unmarshal(me.Body.Bytes(), &who); err != nil || who.User.ID != rig.reader.sess.UserID || who.Session.EmailVerified {
		t.Fatalf("«кто я» не назвал субъекта выданной сессии %q с неподтверждённым адресом: %s", rig.reader.sess.UserID, me.Body.String())
	}
	walkUnverified(t, LoginLanePathRegister)
}

// TestF1_23_Twin_VerifiedRegistrationReachesTheActionAtOnce — близнец Ф1-23.
func TestF1_23_Twin_VerifiedRegistrationReachesTheActionAtOnce(t *testing.T) {
	walkVerifiedTwin(t, LoginLanePathRegister)
}
