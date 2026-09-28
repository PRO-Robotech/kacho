// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware

import (
	"testing"
)

// loginLaneWant — перечень глаголов формы, как его объявляет служба
// (`loginlanehttp.Paths()` в дереве службы — пятнадцать путей одним объявлением):
// четыре глагола Ф3 (Р2), регистрация Ф4 (kacho#2699), два глагола
// восстановления Ф5 (kacho#2701), шесть глаголов второго фактора Ф12 (Р4,
// kacho#1281) и два глагола подтверждения адреса почты (приёмка F6b, Р5; Р6
// службы). Перечень выписан здесь ДОСЛОВНО, а не прочитан
// из модуля службы: у края СВОЁ объявление (§8 инв. 7 Ф3), и проба сверяет его с
// тем, что обязано быть верно по приёмке, — иначе она зеленела бы на любом
// перечне, который край взял бы у службы как есть.
var loginLaneWant = map[string]string{
	"login":             "/iam/v1/auth/login",
	"logout":            "/iam/v1/auth/logout",
	"password":          "/iam/v1/auth/password",
	"csrf":              "/iam/v1/auth/csrf",
	"register":          "/iam/v1/auth/register",
	"recovery":          "/iam/v1/auth/recovery",
	"recovery-complete": "/iam/v1/auth/recovery/complete",
	// Второй фактор (Ф12 Р4): четыре глагола семейства подпутями, чтение
	// состояния на корне семейства, церемония повышения своим подпутём.
	"second-factor-status":       "/iam/v1/auth/second-factor",
	"second-factor-enroll":       "/iam/v1/auth/second-factor/enroll",
	"second-factor-confirm":      "/iam/v1/auth/second-factor/confirm",
	"second-factor-remove":       "/iam/v1/auth/second-factor/remove",
	"second-factor-backup-codes": "/iam/v1/auth/second-factor/backup-codes",
	"step-up":                    "/iam/v1/auth/step-up",
	// Подтверждение адреса почты (приёмка F6b, Р5): запрос письма и предъявление
	// кода, оба под сессией человека.
	"verify-email":         "/iam/v1/auth/verify-email",
	"verify-email-confirm": "/iam/v1/auth/verify-email/confirm",
}

// openBeforeAddressConfirmationWant — глаголы, доступные до подтверждения адреса
// почты (приёмка F6b, Р5): ровно шесть. Прочие девять неподтверждённой сессии
// край отвергает значением Р3. Выписано ДОСЛОВНО по приёмке — проба сверяет
// объявление с решением, а не с самим объявлением.
var openBeforeAddressConfirmationWant = map[string]bool{
	"csrf":                 true,
	"login":                true,
	"logout":               true,
	"register":             true,
	"verify-email":         true,
	"verify-email-confirm": true,
}

// TestLoginLanePaths_F3_51_OneDeclarationFeedsThePublicListAndTheLane — глаголы
// формы объявлены ОДИН раз, и это объявление читают все, кому нужны эти пути:
// перечень путей без записи каталога (`isPublicHTTPPath`, Р8), ветка полосы
// сессии (Р7) и регистрация ретрансляции (композиционный корень).
//
// Второе объявление тех же путей разошлось бы с первым молча — путь,
// освобождённый от каталога, но не ретранслируемый, отвечал бы 404 краем; путь
// ретранслируемый, но не освобождённый, отвергался бы каталогом до службы.
func TestLoginLanePaths_F3_51_OneDeclarationFeedsThePublicListAndTheLane(t *testing.T) {
	routes := LoginLaneRoutes()
	if len(routes) != len(loginLaneWant) {
		t.Fatalf("глаголов формы объявлено %d, ожидалось %d (Р2 + Ф4 + Ф5 + Ф12 + F6b)", len(routes), len(loginLaneWant))
	}
	for _, rt := range routes {
		if loginLaneWant[rt.Verb] != rt.Path {
			t.Errorf("глагол %q объявлен на пути %q, ожидалось %q", rt.Verb, rt.Path, loginLaneWant[rt.Verb])
		}
		if !IsLoginLanePath(rt.Path) {
			t.Errorf("ветка полосы не узнаёт объявленный путь %q", rt.Path)
		}
		if got := LoginLaneVerb(rt.Path); got != rt.Verb {
			t.Errorf("имя глагола по пути %q — %q, объявлено %q", rt.Path, got, rt.Verb)
		}
		if !isPublicHTTPPath(rt.Path) {
			t.Errorf("перечень путей без записи каталога не несёт глагол формы %q (Р8)", rt.Path)
		}
	}
	// Прежние четыре освобождения на месте — расширение, а не замена.
	for _, p := range []string{"/healthz", "/readyz", "/oauth/logout", "/iam/v1/auth/me"} {
		if !isPublicHTTPPath(p) {
			t.Errorf("прежнее освобождение %q снято", p)
		}
	}
	// Отрицательный контроль: соседний путь того же семейства НЕ освобождён и
	// НЕ глагол формы — точное совпадение, не приставка (§1.8: приставка была
	// снята ровно из-за наследования освобождения).
	for _, p := range []string{"/iam/v1/auth/login/", "/iam/v1/auth/loginx", "/iam/v1/auth", "/iam/v1/auth/csrf/x",
		"/iam/v1/auth/second-factor/", "/iam/v1/auth/second-factor/enrollx", "/iam/v1/auth/step-up/x"} {
		if IsLoginLanePath(p) || isPublicHTTPPath(p) {
			t.Errorf("путь %q признан глаголом формы или освобождённым — совпадение обязано быть точным", p)
		}
	}
	t.Logf("перепись: глаголов формы %d · освобождённых путей всего %d", len(routes), 4+len(routes))
}

// TestLoginLanePaths_F4_F5_RegistrationAndRecoveryAreVerbsOfTheSameLane —
// регистрация (Ф4, kacho#2699) и два глагола восстановления доступа (Ф5,
// kacho#2701) стоят в ТОМ ЖЕ объявлении, что четыре глагола Ф3: служба
// обслуживает их на одном слушателе формы, и край, ретранслирующий четыре и не
// ретранслирующий три, отвечал бы на них 404 — при том что каждый из трёх
// объявлен ею тем же перечнем (`loginlanehttp.Paths()` → 7).
//
// Путь «завершение восстановления» — ПОДПУТЬ пути «запрос кода», и это несущее
// для отрицательного контроля: точное совпадение обязано различать
// `/iam/v1/auth/recovery` и `/iam/v1/auth/recovery/complete` как два глагола, а
// `/iam/v1/auth/recovery/` и `/iam/v1/auth/recovery/completex` — не признавать
// вовсе. Приставочное совпадение зеленело бы на первом и не различало второго.
func TestLoginLanePaths_F4_F5_RegistrationAndRecoveryAreVerbsOfTheSameLane(t *testing.T) {
	added := map[string]string{
		"register":          LoginLanePathRegister,
		"recovery":          LoginLanePathRecovery,
		"recovery-complete": LoginLanePathRecoveryComplete,
	}
	for verb, path := range added {
		if loginLaneWant[verb] != path {
			t.Errorf("глагол %q объявлен на пути %q, по приёмке — %q", verb, path, loginLaneWant[verb])
		}
		if !IsLoginLanePath(path) {
			t.Errorf("ветка полосы сессии не узнаёт %q — «сессии нет» на нём отвергалось бы краем вместо ретрансляции", path)
		}
		if got := LoginLaneVerb(path); got != verb {
			t.Errorf("счётчик ретрансляции получил бы метку %q для %q, объявлено %q", got, path, verb)
		}
		if !isPublicHTTPPath(path) {
			t.Errorf("%q не освобождён от решения по каталогу — глагол формы отвергался бы каталогом до службы", path)
		}
	}
	// Положительный контроль различения: два глагола восстановления — РАЗНЫЕ
	// глаголы, один не приставка другого.
	if LoginLaneVerb(LoginLanePathRecovery) == LoginLaneVerb(LoginLanePathRecoveryComplete) {
		t.Errorf("запрос кода и его предъявление получили одно имя глагола %q", LoginLaneVerb(LoginLanePathRecovery))
	}
	for _, p := range []string{
		"/iam/v1/auth/register/", "/iam/v1/auth/registerx", "/iam/v1/auth/registration",
		"/iam/v1/auth/recovery/", "/iam/v1/auth/recovery/completex", "/iam/v1/auth/recovery/complete/",
		"/iam/v1/auth/recovery/x",
	} {
		if IsLoginLanePath(p) || isPublicHTTPPath(p) {
			t.Errorf("путь %q признан глаголом формы или освобождённым — совпадение обязано быть точным", p)
		}
	}
	t.Logf("перепись: глаголов Ф4/Ф5 %d из %d объявленных", len(added), len(LoginLaneRoutes()))
}

// TestLoginLanePaths_F6b_11_FifteenVerbsAndSixOpenBeforeAddressConfirmation —
// объявление глаголов формы несёт 15 путей, у каждого — решение «доступен до
// подтверждения адреса», и доступных ровно шесть (приёмка F6b, Р5, F6b-11).
//
// Нулевое значение решения — ОТКАЗ: глагол, дописанный без решения, до
// подтверждения недоступен. Оба глагола подтверждения носитель читают (Р6
// службы: «под сессией человека»), поэтому при службе, не ответившей о сессии,
// край отвечает F4d-23, а не ретранслирует: `relayWhenUnanswered` у них нулевой.
func TestLoginLanePaths_F6b_11_FifteenVerbsAndSixOpenBeforeAddressConfirmation(t *testing.T) {
	routes := LoginLaneRoutes()
	if len(routes) != 15 || len(loginLaneWant) != 15 {
		t.Fatalf("глаголов формы объявлено %d, по приёмке — 15 (%d выписано)", len(routes), len(loginLaneWant))
	}
	open := 0
	for _, rt := range routes {
		want := openBeforeAddressConfirmationWant[rt.Verb]
		if got := loginLaneOpenBeforeAddressConfirmation(rt.Path); got != want {
			t.Errorf("глагол %q: доступен до подтверждения адреса = %v, по приёмке — %v", rt.Verb, got, want)
		}
		if loginLaneOpenBeforeAddressConfirmation(rt.Path) {
			open++
		}
	}
	if open != 6 {
		t.Fatalf("доступных до подтверждения адреса глаголов %d, по приёмке — 6", open)
	}
	for _, p := range []string{LoginLanePathVerifyEmail, LoginLanePathVerifyEmailConfirm} {
		if loginLaneRelaysWhenUnanswered(p) {
			t.Errorf("%q читает носитель — при службе, не ответившей о сессии, край обязан отвечать F4d-23, а не ретранслировать", p)
		}
		if !IsLoginLanePath(p) || !isPublicHTTPPath(p) {
			t.Errorf("%q не объявлен глаголом формы — ретрансляция и освобождение от каталога его не видят", p)
		}
	}
	// Близнец: регистрация носителя не читает и ретранслируется при неответе.
	if !loginLaneRelaysWhenUnanswered(LoginLanePathRegister) {
		t.Error("регистрация носителя не читает и обязана ретранслироваться при неответе службы")
	}
	// Путь вне перечня — не открыт: решение не наследуется ни приставкой, ни соседом.
	for _, p := range []string{"/iam/v1/auth/verify-email/", "/iam/v1/auth/verify-emailx", "/iam/v1/auth/verify-email/confirm/x", "/vpc/v1/networks"} {
		if loginLaneOpenBeforeAddressConfirmation(p) {
			t.Errorf("путь %q признан доступным до подтверждения — совпадение обязано быть точным", p)
		}
	}
	t.Logf("перепись: глаголов формы %d · доступных до подтверждения адреса %d", len(routes), open)
}
