// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware

import (
	"strings"
	"testing"
)

// loginLaneWant — перечень глаголов формы, как его объявляет служба
// (`loginlanehttp.Paths()` в дереве службы — семнадцать путей одним объявлением):
// четыре глагола Ф3 (Р2), регистрация Ф4 (kacho#2699), два глагола
// восстановления Ф5 (kacho#2701), шесть глаголов второго фактора Ф12 (Р4,
// kacho#1281), два глагола подтверждения адреса почты (приёмка F6b, Р5; Р6
// службы) и два глагола входа ключом доступа (приёмка Ф13 Р1, kacho#3037). Перечень выписан здесь ДОСЛОВНО, а не прочитан
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
	// Вход ключом доступа (приёмка Ф13 Р1, Ф13-28; kacho#3037): форма запроса
	// и форма подтверждения — два глагола одной полосы.
	"access-key-begin": "/iam/v1/auth/access-key/begin",
	"access-key-login": "/iam/v1/auth/access-key/login",
}

// accessKeyFamily — приставка семейства входа ключом. Читается ТОЛЬКО пробой
// закрытости семейства ниже: объявление края совпадает с путём точно, и
// приставка здесь — способ сосчитать записи семейства, а не правило совпадения.
const accessKeyFamily = "/iam/v1/auth/access-key/"

// openBeforeAddressConfirmationWant — записи, доступные до подтверждения адреса
// почты (приёмка F6b, Р5): ровно девять — шесть глаголов формы и три координаты
// церемонии (на них отвечает служба протоколом). Прочие девять глаголов формы
// неподтверждённой сессии край отвергает значением Р3. Выписано ДОСЛОВНО по
// приёмке — проба сверяет объявление с решением, а не с самим объявлением.
var openBeforeAddressConfirmationWant = map[string]bool{
	"csrf":                 true,
	"login":                true,
	"logout":               true,
	"register":             true,
	"verify-email":         true,
	"verify-email-confirm": true,
	"authorize":            true,
	"token":                true,
	"discovery":            true,
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
	if len(routes) != len(loginLaneWant)+len(ceremonyWant) {
		t.Fatalf("записей объявления %d, ожидалось %d: глаголов формы %d (Р2 + Ф4 + Ф5 + Ф12 + F6b + Ф13) и координат церемонии %d (LINE-A-1 §5.1а)",
			len(routes), len(loginLaneWant)+len(ceremonyWant), len(loginLaneWant), len(ceremonyWant))
	}
	for _, rt := range routes {
		want, form := loginLaneWant[rt.Verb]
		if !form {
			want = ceremonyWant[rt.Verb]
		}
		if want != rt.Path {
			t.Errorf("запись %q объявлена на пути %q, ожидалось %q", rt.Verb, rt.Path, want)
		}
		// Цель ретрансляции — по роду записи: глагол формы уходит на слушатель
		// формы, координата церемонии — на слушатель выдачи (§5.1б п. 2).
		if wantTarget := map[bool]RelayTarget{true: RelayTargetForm, false: RelayTargetIssuance}[form]; rt.Target != wantTarget {
			t.Errorf("запись %q ретранслируется на цель %q, ожидалось %q", rt.Verb, rt.Target, wantTarget)
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
	t.Logf("перепись: записей объявления %d (глаголов формы %d · координат церемонии %d) · освобождённых путей всего %d",
		len(routes), len(loginLaneWant), len(ceremonyWant), 4+len(routes))
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
	t.Logf("перепись: глаголов Ф4/Ф5 %d из %d записей объявления", len(added), len(LoginLaneRoutes()))
}

// TestLoginLanePaths_F6b_11_TwentyRecordsAndNineOpenBeforeAddressConfirmation —
// объявление несёт 20 записей: 17 глаголов формы с целью «слушатель формы» и 3
// координаты церемонии с целью «слушатель выдачи»; у каждой — решение «доступна
// до подтверждения адреса», и доступных ровно девять (приёмка F6b, Р5, F6b-11).
// Два глагола входа ключом (Ф13, kacho#3037) решения F6b не получили: перечень
// девяти закрыт приёмкой F6b, и запись, дописанная без решения, до
// подтверждения недоступна — умолчание отказа, а не наследование от `login`.
//
// Нулевое значение решения — ОТКАЗ: запись, дописанная без решения, до
// подтверждения недоступна. Оба глагола подтверждения носитель читают (Р6
// службы: «под сессией человека»), поэтому при службе, не ответившей о сессии,
// край отвечает F4d-23, а не ретранслирует: `relayWhenUnanswered` у них нулевой.
func TestLoginLanePaths_F6b_11_TwentyRecordsAndNineOpenBeforeAddressConfirmation(t *testing.T) {
	routes := LoginLaneRoutes()
	if len(routes) != 20 || len(loginLaneWant) != 17 || len(ceremonyWant) != 3 {
		t.Fatalf("записей объявлено %d, по приёмке — 20: глаголов формы 17 (%d выписано), координат церемонии 3 (%d выписано)",
			len(routes), len(loginLaneWant), len(ceremonyWant))
	}
	open, form, issuance := 0, 0, 0
	for _, rt := range routes {
		switch rt.Target {
		case RelayTargetForm:
			form++
		case RelayTargetIssuance:
			issuance++
		}
		want := openBeforeAddressConfirmationWant[rt.Verb]
		if got := loginLaneOpenBeforeAddressConfirmation(rt.Path); got != want {
			t.Errorf("запись %q: доступна до подтверждения адреса = %v, по приёмке — %v", rt.Verb, got, want)
		}
		if loginLaneOpenBeforeAddressConfirmation(rt.Path) {
			open++
		}
	}
	if form != 17 || issuance != 3 {
		t.Errorf("записей со слушателем формы %d, со слушателем выдачи %d — по приёмке 17 и 3", form, issuance)
	}
	if open != 9 {
		t.Fatalf("доступных до подтверждения адреса записей %d, по приёмке — 9", open)
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
	for _, p := range []string{"/iam/v1/auth/verify-email/", "/iam/v1/auth/verify-emailx", "/iam/v1/auth/verify-email/confirm/x",
		"/iam/v1/authorize/", "/iam/v1/authorize:check", "/iam/v1/token/x", "/.well-known/openid-configuration", "/vpc/v1/networks"} {
		if loginLaneOpenBeforeAddressConfirmation(p) {
			t.Errorf("путь %q признан доступным до подтверждения — совпадение обязано быть точным", p)
		}
	}
	t.Logf("перепись: записей %d (формы %d · выдачи %d) · доступных до подтверждения адреса %d", len(routes), form, issuance, open)
}

// TestLoginLanePaths_F13_28_TwoAccessKeyVerbsAndNoThird — вход ключом доступа
// (приёмка Ф13 Р1, Ф13-28, §7 инв. 12; kacho#3037) несёт в объявлении края ровно
// ДВЕ записи: форма запроса и форма подтверждения. Обе — глаголы формы на
// слушателе формы, обе освобождены от каталога прав (Р8) и узнаются веткой
// полосы сессии (Р7).
//
// Семейство ЗАКРЫТО: третья запись под `access-key/` — освобождение от каталога,
// никем не решённое, и проба на ней краснеет, называя путь. Решение о
// недоступности службы — ПАРОЙ с входом паролем (Ф13-28 «И»): полосы входа ведут
// себя одинаково, и проба сверяет значение с `login`, а не с литералом.
func TestLoginLanePaths_F13_28_TwoAccessKeyVerbsAndNoThird(t *testing.T) {
	want := map[string]string{
		"access-key-begin": loginLaneWant["access-key-begin"],
		"access-key-login": loginLaneWant["access-key-login"],
	}
	seen := map[string]bool{}
	family := 0
	for _, rt := range LoginLaneRoutes() {
		if !strings.HasPrefix(rt.Path, accessKeyFamily) {
			continue
		}
		family++
		if want[rt.Verb] != rt.Path {
			t.Errorf("запись %q на пути %q семейства входа ключом приёмкой Ф13 не объявлена — освобождение от каталога никем не решено", rt.Verb, rt.Path)
			continue
		}
		seen[rt.Verb] = true
	}
	for verb, path := range want {
		if !seen[verb] {
			t.Errorf("глагол входа ключом %q (%s) в объявлении края отсутствует — после посадки Ф13 путь отвечал бы 404 краем", verb, path)
			continue
		}
		rt, _ := LoginLaneRouteFor(path)
		if rt.Target != RelayTargetForm {
			t.Errorf("%q ретранслируется на цель %q, по приёмке — слушатель формы", path, rt.Target)
		}
		if !isPublicHTTPPath(path) {
			t.Errorf("%q не освобождён от решения по каталогу (Р8) — глагол формы отвергался бы каталогом до службы", path)
		}
		if LoginLaneVerb(path) != verb {
			t.Errorf("метка ретрансляции для %q — %q, объявлено %q", path, LoginLaneVerb(path), verb)
		}
		if got, pair := loginLaneRelaysWhenUnanswered(path), loginLaneRelaysWhenUnanswered(LoginLanePathLogin); got != pair {
			t.Errorf("%q: ретрансляция при неответе службы = %v, у входа паролем — %v; полосы входа обязаны вести себя одинаково (Ф13-28)", path, got, pair)
		}
		if loginLaneOpenBeforeAddressConfirmation(path) {
			t.Errorf("%q доступен до подтверждения адреса — перечень девяти закрыт приёмкой F6b Р5, решения Ф13 об этом нет", path)
		}
		if carriesClientBasicFor(path) {
			t.Errorf("%q несёт удостоверение клиента базовой схемой — исключение одно, у обмена кода", path)
		}
	}
	if family != len(want) {
		t.Errorf("записей семейства входа ключом %d, по приёмке Ф13 — %d", family, len(want))
	}
	// Отрицательный контроль: соседи по имени — не глаголы и не освобождены.
	for _, p := range []string{"/iam/v1/auth/access-key", "/iam/v1/auth/access-key/", "/iam/v1/auth/access-key/begin/",
		"/iam/v1/auth/access-key/loginx", "/iam/v1/auth/access-key/login/x", "/iam/v1/auth/access-keys"} {
		if IsLoginLanePath(p) || isPublicHTTPPath(p) {
			t.Errorf("путь %q признан глаголом формы или освобождённым — совпадение обязано быть точным", p)
		}
	}
	t.Logf("перепись: записей объявления %d · семейства входа ключом %d (по приёмке %d)", len(LoginLaneRoutes()), family, len(want))
}

// carriesClientBasicFor — решение ретранслятора об удостоверении клиента по пути
// (вспомогательное чтение пробы; путь вне перечня — false).
func carriesClientBasicFor(path string) bool {
	rt, ok := LoginLaneRouteFor(path)
	return ok && rt.CarriesClientBasic()
}
