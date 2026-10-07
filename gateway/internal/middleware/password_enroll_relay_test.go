// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// password_enroll_relay_test.go — глагол заведения первого пароля из живой
// сессии (kaname#213, приёмка службы first-password-from-a-live-session;
// kacho#3056) в объявлении края и на полосе сессии.
//
// Служба объявила глагол у своего слушателя формы (`loginlanehttp.Paths()` —
// восемнадцать путей), край его не ретранслировал: путь не был записью
// объявления, и полоса сессии судила его как путь платформы — «сессии нет»
// краем, каталог прав до службы. Здесь судится РЕШЕНИЕ записи и его исход
// через цепочку, каждое отрицание — в паре с близнецом, отличающимся одним
// фактом.
//
// Решения записи берутся парой со сменой пароля (Ф3-20 «д»): оба глагола
// правят пароль своей сессии и читают её носитель. Значит: недоступность
// службы — ответ Р1 края (KA1), носитель цел; до подтверждения адреса — отказ
// адреса (служба держит то же, положение подтверждения = отказ, A7 Р2), перечень
// девяти доступных закрыт приёмкой F6b и не расширяется; удостоверение клиента
// базовой схемой не несётся.
package middleware

import (
	"net/http"
	"testing"
)

// passwordEnrollPath — путь глагола, выписанный дословно по объявлению службы
// (`loginlanehttp.PathPasswordEnroll`), а не взятый из объявления края: проба
// сверяет край с тем, что обязано быть верно, а не с самим собой.
const passwordEnrollPath = "/iam/v1/auth/password/enroll"

func TestPasswordEnroll_3056_DeclaredOnceAsAFormVerbThatReadsTheCarrier(t *testing.T) {
	rt, ok := LoginLaneRouteFor(passwordEnrollPath)
	if !ok {
		t.Fatalf("%s в объявлении края нет — глагол службы край не ретранслирует, путь судится как путь платформы", passwordEnrollPath)
	}
	if rt.Verb != "password-enroll" {
		t.Errorf("имя записи %q, ожидалось %q (значение метки ретрансляции)", rt.Verb, "password-enroll")
	}
	if rt.Target != RelayTargetForm {
		t.Errorf("цель ретрансляции %q, ожидалась %q — глагол обслуживает слушатель формы службы", rt.Target, RelayTargetForm)
	}
	if !IsLoginLanePath(passwordEnrollPath) || LoginLaneVerb(passwordEnrollPath) != rt.Verb {
		t.Errorf("ветка полосы сессии не узнаёт %s: IsLoginLanePath=%v, verb=%q", passwordEnrollPath, IsLoginLanePath(passwordEnrollPath), LoginLaneVerb(passwordEnrollPath))
	}
	if !isPublicHTTPPath(passwordEnrollPath) {
		t.Errorf("%s не освобождён от решения по каталогу (Р8) — глагол отвергался бы каталогом до службы", passwordEnrollPath)
	}
	// Пара со сменой пароля: оба читают носитель своей сессии.
	if got, pair := loginLaneRelaysWhenUnanswered(passwordEnrollPath), loginLaneRelaysWhenUnanswered(LoginLanePathPassword); got || got != pair {
		t.Errorf("ретрансляция при неответе службы = %v (у смены пароля %v): глагол читает носитель — ответ Р1 края, а не ретрансляция", got, pair)
	}
	if loginLaneOpenBeforeAddressConfirmation(passwordEnrollPath) {
		t.Errorf("%s доступен до подтверждения адреса — перечень девяти закрыт приёмкой F6b Р5", passwordEnrollPath)
	}
	if rt.CarriesClientBasic() {
		t.Errorf("%s несёт удостоверение клиента базовой схемой — исключение одно, у обмена кода", passwordEnrollPath)
	}
	// Близнец: смена пароля — СВОЙ глагол, путь нового — не её подпуть по совпадению.
	if LoginLaneVerb(LoginLanePathPassword) != "password" {
		t.Errorf("смена пароля потеряла свою запись: %q", LoginLaneVerb(LoginLanePathPassword))
	}
	// Отрицательный контроль: соседи по имени — не глаголы и не освобождены.
	for _, p := range []string{"/iam/v1/auth/password/", "/iam/v1/auth/password/enrollx", "/iam/v1/auth/password/enroll/",
		"/iam/v1/auth/password/x", "/iam/v1/auth/password-enroll"} {
		if IsLoginLanePath(p) || isPublicHTTPPath(p) {
			t.Errorf("путь %q признан глаголом формы или освобождённым — совпадение обязано быть точным", p)
		}
	}
}

// Исходы полосы сессии на глаголе через цепочку `AuthInterceptor.HTTP(mux)`,
// где на каждой записи объявления стоит считающий дублёр ретрансляции.
func TestPasswordEnroll_3056_SessionLaneOutcomesThroughTheChain(t *testing.T) {
	deny := denyBody(t)

	book, cut := f12Books()
	chain, relay := formChain(t, book, cut)

	// Живая сессия — ретранслирована, личность выставлена полосой.
	if got := runFormVerb(chain, relay, formVerbRequest(passwordEnrollPath, carrierA, false)); got.relayed != 1 || got.code != http.StatusOK {
		t.Errorf("живая сессия: глагол обязан ретранслироваться; получено %d %q, ретранслировано %d", got.code, got.body, got.relayed)
	}
	// «Сессии нет» — ретранслировано: отвечает служба своим отказом, а не край.
	if got := runFormVerb(chain, relay, formVerbRequest(passwordEnrollPath, carrierC, false)); got.relayed != 1 {
		t.Errorf("«сессии нет»: обязан ретранслироваться службе; получено %d %q, ретранслировано %d", got.code, got.body, got.relayed)
	}
	// Отсечка — отказ края F4d-22 до ретрансляции, тем же телом, что всюду.
	if got := runFormVerb(chain, relay, formVerbRequest(passwordEnrollPath, carrierB, false)); got.relayed != 0 || got.body != deny {
		t.Errorf("отсечка: ожидался отказ F4d-22 без ретрансляции; получено %d %q, ретранслировано %d", got.code, got.body, got.relayed)
	}
	// Недоступность службы — ответ Р1 края, носитель цел.
	for _, mode := range unansweredModes {
		book, cut := f12Books()
		mode.set(book, cut)
		chain, relay := formChain(t, book, cut)
		expectRefusedKept(t, mode.name+" · password-enroll", runFormVerb(chain, relay, formVerbRequest(passwordEnrollPath, carrierA, false)))
	}
}

// До подтверждения адреса — отказ адреса края; близнец с подтверждённым
// адресом — ретранслирован. Различие — один факт `EmailVerified`.
func TestPasswordEnroll_3056_UnverifiedAddressIsRefusedVerifiedTwinIsRelayed(t *testing.T) {
	rig := newAddressRig(t, unverifiedOwnSession())
	requireAddressRefusal(t, "неподтверждённый адрес · password-enroll", rig.present(http.MethodPost, passwordEnrollPath, true))
	if rig.reached[passwordEnrollPath] != 0 {
		t.Fatalf("неподтверждённая сессия дошла до ретрансляции %d раз", rig.reached[passwordEnrollPath])
	}

	twin := newAddressRig(t, liveOwnSession())
	if rec := twin.present(http.MethodPost, passwordEnrollPath, true); rec.Code != http.StatusOK || twin.reached[passwordEnrollPath] != 1 {
		t.Fatalf("близнец с подтверждённым адресом: %d %s, ретранслировано %d", rec.Code, rec.Body.String(), twin.reached[passwordEnrollPath])
	}
}
