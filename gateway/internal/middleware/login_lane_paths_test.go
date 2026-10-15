// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware

import (
	"strings"
	"testing"
)

// loginLaneWant — перечень глаголов формы, как его объявляет служба
// (`loginlanehttp.Paths()` в дереве службы — двадцать четыре пути одним объявлением):
// четыре глагола Ф3 (Р2), регистрация Ф4 (kacho#2699), два глагола
// восстановления Ф5 (kacho#2701), шесть глаголов второго фактора Ф12 (Р4,
// kacho#1281), два глагола подтверждения адреса почты (приёмка F6b, Р5; Р6
// службы), два глагола входа ключом доступа (приёмка Ф13 Р1, kacho#3037),
// заведение первого пароля из живой сессии (kaname#213, kacho#3056), три
// глагола своих сессий (приёмка F8-S5 Р1, kacho#3061), два глагола смены
// адреса почты (приёмка F8-S6 Р1, kacho#3062) и выдача испытания повышения
// ключом доступа (приёмка F8-S7 Р10, kacho#3230). Перечень выписан здесь ДОСЛОВНО, а не прочитан
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
	// Выдача испытания повышения ключом доступа (приёмка F8-S7 Р10; служба —
	// SUA Р2): подпуть церемонии повышения, под живой сессией.
	"step-up-access-key-begin": "/iam/v1/auth/step-up/access-key/begin",
	// Подтверждение адреса почты (приёмка F6b, Р5): запрос письма и предъявление
	// кода, оба под сессией человека.
	"verify-email":         "/iam/v1/auth/verify-email",
	"verify-email-confirm": "/iam/v1/auth/verify-email/confirm",
	// Вход ключом доступа (приёмка Ф13 Р1, Ф13-28; kacho#3037): форма запроса
	// и форма подтверждения — два глагола одной полосы.
	"access-key-begin": "/iam/v1/auth/access-key/begin",
	"access-key-login": "/iam/v1/auth/access-key/login",
	// Заведение первого пароля из живой сессии (kaname#213, приёмка службы
	// first-password-from-a-live-session; kacho#3056): подпуть семейства пароля.
	"password-enroll": "/iam/v1/auth/password/enroll",
	// Свои сессии (приёмка F8-S5 Р1, kacho#3061): перечень, снятие одной и
	// снятие прочих — три глагола под живой сессией, все читают носитель.
	"sessions":            "/iam/v1/auth/sessions",
	"sessions-end":        "/iam/v1/auth/sessions/end",
	"sessions-end-others": "/iam/v1/auth/sessions/end-others",
	// Смена адреса почты (приёмка F8-S6 Р1, kacho#3062): запрос смены и
	// предъявление кода с нового адреса — два глагола под живой сессией.
	"email-change":         "/iam/v1/auth/email-change",
	"email-change-confirm": "/iam/v1/auth/email-change/confirm",
}

// ownSessionsWant — три записи Р1 приёмки F8-S5, выписанные ДОСЛОВНО по её
// таблице, а не по объявлению края.
var ownSessionsWant = map[string]string{
	"sessions":            "/iam/v1/auth/sessions",
	"sessions-end":        "/iam/v1/auth/sessions/end",
	"sessions-end-others": "/iam/v1/auth/sessions/end-others",
}

// emailChangeWant — две записи Р1 приёмки F8-S6, выписанные ДОСЛОВНО по её
// решению, а не по объявлению края.
var emailChangeWant = map[string]string{
	"email-change":         "/iam/v1/auth/email-change",
	"email-change-confirm": "/iam/v1/auth/email-change/confirm",
}

// accessKeyFamily — приставка семейства входа ключом. Читается ТОЛЬКО пробой
// закрытости семейства ниже: объявление края совпадает с путём точно, и
// приставка здесь — способ сосчитать записи семейства, а не правило совпадения.
const accessKeyFamily = "/iam/v1/auth/access-key/"

// openBeforeAddressConfirmationWant — записи, доступные до подтверждения адреса
// почты (приёмка F6b, Р5): ровно девять — шесть глаголов формы и три координаты
// церемонии (на них отвечает служба протоколом). Прочие шестнадцать глаголов формы
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
		t.Fatalf("записей объявления %d, ожидалось %d: глаголов формы %d (Р2 + Ф4 + Ф5 + Ф12 + F6b + Ф13 + F8-S5 + F8-S6 + F8-S7) и координат церемонии %d (LINE-A-1 §5.1а)",
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
		"/iam/v1/auth/second-factor/", "/iam/v1/auth/second-factor/enrollx", "/iam/v1/auth/step-up/x",
		"/iam/v1/auth/step-up/access-key", "/iam/v1/auth/step-up/access-key/", "/iam/v1/auth/step-up/access-key/begin/",
		"/iam/v1/auth/step-up/access-key/beginx", "/iam/v1/auth/step-up/access-key/login",
		"/iam/v1/auth/password/", "/iam/v1/auth/password/enrollx", "/iam/v1/auth/password/enroll/",
		"/iam/v1/auth/sessions/", "/iam/v1/auth/sessions/other", "/iam/v1/auth/sessions/end/", "/iam/v1/auth/sessions/endx",
		"/iam/v1/auth/session", "/iam/v1/auth/sessions/end-others/",
		"/iam/v1/auth/email-change/", "/iam/v1/auth/email-changex", "/iam/v1/auth/email-change/confirm/",
		"/iam/v1/auth/email-change/confirmx", "/iam/v1/auth/email"} {
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

// TestLoginLanePaths_F6b_11_EveryRecordCountedAndNineOpenBeforeAddressConfirmation —
// объявление несёт 27 записей: 24 глагола формы с целью «слушатель формы» и 3
// координаты церемонии с целью «слушатель выдачи»; у каждой — решение «доступна
// до подтверждения адреса», и доступных ровно девять (приёмка F6b, Р5, F6b-11).
// Два глагола входа ключом (Ф13, kacho#3037) решения F6b не получили: перечень
// девяти закрыт приёмкой F6b, и запись, дописанная без решения, до
// подтверждения недоступна — умолчание отказа, а не наследование от `login`.
// Так же стоит заведение первого пароля (kacho#3056): решения F6b о нём нет, и
// служба держит то же — в положении подтверждения адреса отказ (A7 Р2). Так же
// стоят три глагола своих сессий (приёмка F8-S5 Р1, F8-S5-09): закрыты до
// подтверждения, как у службы (OS Р8). Так же стоят два глагола смены адреса
// (приёмка F8-S6 Р1, F8S6-10): закрыты до подтверждения (EC Р2 службы). Так
// же стоит выдача испытания повышения ключом (приёмка F8-S7 Р10, F8-S7-80):
// закрыта до подтверждения парой со `step-up` (SUA Р2 службы), и открытых
// остаётся девять.
//
// Число записей в имени пробы не стоит: оно растёт с каждой записью
// объявления, и имя, несущее число, лгало бы после следующей.
//
// Нулевое значение решения — ОТКАЗ: запись, дописанная без решения, до
// подтверждения недоступна. Оба глагола подтверждения носитель читают (Р6
// службы: «под сессией человека»), поэтому при службе, не ответившей о сессии,
// край отвечает F4d-23, а не ретранслирует: `relayWhenUnanswered` у них нулевой.
func TestLoginLanePaths_F6b_11_EveryRecordCountedAndNineOpenBeforeAddressConfirmation(t *testing.T) {
	routes := LoginLaneRoutes()
	if len(routes) != 27 || len(loginLaneWant) != 24 || len(ceremonyWant) != 3 {
		t.Fatalf("записей объявлено %d, по приёмке — 27: глаголов формы 24 (%d выписано), координат церемонии 3 (%d выписано)",
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
	if form != 24 || issuance != 3 {
		t.Errorf("записей со слушателем формы %d, со слушателем выдачи %d — по приёмке 24 и 3", form, issuance)
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

// TestLoginLanePaths_F8_S7_80_StepUpAccessKeyBeginPairsWithStepUp — запись
// Р10 приёмки F8-S7 (kacho#3230; служба — SUA Р2, kaname#827) стоит в ТОМ ЖЕ
// объявлении, что прочие глаголы формы, и каждое её решение — ПАРОЙ со
// `step-up`, а не литералом: выдача испытания повышения — часть той же
// церемонии под живой сессией. Цель — слушатель формы; путь освобождён от
// каталога (Р8) и узнаётся веткой полосы сессии (Р7); недоступность службы —
// как у `step-up` (ответ края KA1: носитель читается); до подтверждения адреса
// — как у `step-up` (закрыта, служба держит то же — SUA Р2
// `PathRefusedInVerification`); удостоверение клиента базовой схемой не
// несётся; кеш решений исходом не гасится.
//
// Сверка значения с соседом, а не с литералом, держит пару в обе стороны:
// решение `step-up`, переменённое одно, краснит пробу на расхождении. Литерал
// стоит рядом ровно один — ожидание приёмки F8-S7 Р10 «закрыта», чтобы пара
// не согласилась на открытие обеих.
//
// Статической сверки «край ↔ служба» здесь нет (перечень службы кодом не
// читается, §8 инв. 7 Ф3): её держит сквозная проба края F8-S7-80.
func TestLoginLanePaths_F8_S7_80_StepUpAccessKeyBeginPairsWithStepUp(t *testing.T) {
	const verb, path = "step-up-access-key-begin", "/iam/v1/auth/step-up/access-key/begin"
	if loginLaneWant[verb] != path {
		t.Errorf("дословный перечень пробы: глагол %q на пути %q, по приёмке F8-S7 Р10 — %q", verb, loginLaneWant[verb], path)
	}
	rt, ok := LoginLaneRouteFor(path)
	if !ok {
		t.Fatalf("%s (%s) в объявлении края нет — путь судится как путь платформы и отвечает отказом каталога, служба его не получает", path, verb)
	}
	pair, ok := LoginLaneRouteFor(LoginLanePathStepUp)
	if !ok {
		t.Fatalf("пары нет: %s в объявлении края отсутствует", LoginLanePathStepUp)
	}
	if rt.Verb != verb {
		t.Errorf("запись на пути %s названа %q, по приёмке — %q (значение метки ретрансляции)", path, rt.Verb, verb)
	}
	if rt.Target != RelayTargetForm || rt.Target != pair.Target {
		t.Errorf("%s ретранслируется на цель %q, у %s — %q; по приёмке — слушатель формы", path, rt.Target, pair.Path, pair.Target)
	}
	if !IsLoginLanePath(path) || !isPublicHTTPPath(path) {
		t.Errorf("%s: ветка полосы сессии узнаёт = %v, освобождён от каталога = %v — обязаны оба", path, IsLoginLanePath(path), isPublicHTTPPath(path))
	}
	if LoginLaneVerb(path) == LoginLaneVerb(LoginLanePathStepUp) {
		t.Errorf("подпуть и церемония повышения получили одно имя %q — точное совпадение обязано их различать", LoginLaneVerb(path))
	}
	if got, want := loginLaneRelaysWhenUnanswered(path), loginLaneRelaysWhenUnanswered(LoginLanePathStepUp); got != want {
		t.Errorf("%s: ретрансляция при неответе службы = %v, у %s — %v; запись обязана вести себя парой", path, got, LoginLanePathStepUp, want)
	}
	if got, want := loginLaneOpenBeforeAddressConfirmation(path), loginLaneOpenBeforeAddressConfirmation(LoginLanePathStepUp); got != want || got {
		t.Errorf("%s: доступна до подтверждения адреса = %v, у %s — %v; по приёмке F8-S7 Р10 закрыта парой", path, got, LoginLanePathStepUp, want)
	}
	if rt.CarriesClientBasic() || rt.flushesDecisionCacheOnSuccess {
		t.Errorf("%s: удостоверение клиента = %v, гашение кеша = %v — решений ни о том, ни о другом приёмка не давала", path, rt.CarriesClientBasic(), rt.flushesDecisionCacheOnSuccess)
	}
	t.Logf("перепись: записей объявления %d · запись %q на %s парой с %q", len(LoginLaneRoutes()), verb, path, pair.Verb)
}

// carriesClientBasicFor — решение ретранслятора об удостоверении клиента по пути
// (вспомогательное чтение пробы; путь вне перечня — false).
func carriesClientBasicFor(path string) bool {
	rt, ok := LoginLaneRouteFor(path)
	return ok && rt.CarriesClientBasic()
}

// TestLoginLanePaths_F8_S5_09_ThreeOwnSessionVerbsAreFormRecordsClosedBeforeAddressConfirmation —
// три записи Р1 приёмки F8-S5 (kacho#3061) стоят в ТОМ ЖЕ объявлении, что
// прочие глаголы формы: цель — слушатель формы; недоступность службы — ответ
// края (KA1), а не ретрансляция: все три читают носитель живой сессии; до
// подтверждения адреса — закрыты (служба держит то же, OS Р8); удостоверение
// клиента базовой схемой не несётся. Совпадение точное: снятие одной — подпуть
// перечня, и приставочное совпадение их не различило бы.
func TestLoginLanePaths_F8_S5_09_ThreeOwnSessionVerbsAreFormRecordsClosedBeforeAddressConfirmation(t *testing.T) {
	for verb, path := range ownSessionsWant {
		if loginLaneWant[verb] != path {
			t.Errorf("дословный перечень пробы: глагол %q на пути %q, по приёмке F8-S5 Р1 — %q", verb, loginLaneWant[verb], path)
		}
		rt, ok := LoginLaneRouteFor(path)
		if !ok {
			t.Errorf("%s (%s) в объявлении края нет — путь судится как путь платформы, служба его не получает", path, verb)
			continue
		}
		if rt.Verb != verb {
			t.Errorf("запись на пути %s названа %q, по приёмке — %q (значение метки ретрансляции)", path, rt.Verb, verb)
		}
		if rt.Target != RelayTargetForm {
			t.Errorf("%s ретранслируется на цель %q, по приёмке — слушатель формы", path, rt.Target)
		}
		if !IsLoginLanePath(path) || !isPublicHTTPPath(path) {
			t.Errorf("%s: ветка полосы сессии узнаёт = %v, освобождён от каталога = %v — обязаны оба", path, IsLoginLanePath(path), isPublicHTTPPath(path))
		}
		if loginLaneRelaysWhenUnanswered(path) {
			t.Errorf("%s читает носитель — при службе, не ответившей о сессии, край обязан отвечать сам (KA1), а не ретранслировать", path)
		}
		if loginLaneOpenBeforeAddressConfirmation(path) {
			t.Errorf("%s доступен до подтверждения адреса — по приёмке F8-S5 Р1 закрыт, перечень девяти закрыт приёмкой F6b", path)
		}
		if rt.CarriesClientBasic() {
			t.Errorf("%s несёт удостоверение клиента базовой схемой — исключение одно, у обмена кода", path)
		}
	}
	// Различение подпутей: три РАЗНЫХ имени.
	names := map[string]bool{}
	for _, path := range ownSessionsWant {
		names[LoginLaneVerb(path)] = true
	}
	if len(names) != len(ownSessionsWant) {
		t.Errorf("три пути своих сессий получили %d различных имён — точное совпадение обязано их различать", len(names))
	}
	// Отрицательный контроль: соседи по имени — не глаголы и не освобождены.
	for _, p := range []string{"/iam/v1/auth/sessions/", "/iam/v1/auth/sessions/other", "/iam/v1/auth/sessions/end/",
		"/iam/v1/auth/sessions/endx", "/iam/v1/auth/sessions/end-others/", "/iam/v1/auth/session", "/iam/v1/auth/sessionsx"} {
		if IsLoginLanePath(p) || isPublicHTTPPath(p) {
			t.Errorf("путь %q признан глаголом формы или освобождённым — совпадение обязано быть точным", p)
		}
	}
	t.Logf("перепись: записей объявления %d · своих сессий по приёмке %d", len(LoginLaneRoutes()), len(ownSessionsWant))
}

// TestLoginLanePaths_F8S6_10_TwoEmailChangeVerbsAreFormRecordsClosedBeforeAddressConfirmation —
// две записи Р1 приёмки F8-S6 (kacho#3062) стоят в ТОМ ЖЕ объявлении, что
// прочие глаголы формы: цель — слушатель формы; недоступность службы — ответ
// края (KA1), а не ретрансляция: оба глагола читают носитель живой сессии; до
// подтверждения адреса — закрыты (EC Р2 службы); удостоверение клиента
// базовой схемой не несётся. Предъявление — подпуть запроса, и совпадение
// обязано их различать.
func TestLoginLanePaths_F8S6_10_TwoEmailChangeVerbsAreFormRecordsClosedBeforeAddressConfirmation(t *testing.T) {
	for verb, path := range emailChangeWant {
		if loginLaneWant[verb] != path {
			t.Errorf("дословный перечень пробы: глагол %q на пути %q, по приёмке F8-S6 Р1 — %q", verb, loginLaneWant[verb], path)
		}
		rt, ok := LoginLaneRouteFor(path)
		if !ok {
			t.Errorf("%s (%s) в объявлении края нет — путь судится как путь платформы, служба его не получает", path, verb)
			continue
		}
		if rt.Verb != verb {
			t.Errorf("запись на пути %s названа %q, по приёмке — %q (значение метки ретрансляции)", path, rt.Verb, verb)
		}
		if rt.Target != RelayTargetForm {
			t.Errorf("%s ретранслируется на цель %q, по приёмке — слушатель формы", path, rt.Target)
		}
		if !IsLoginLanePath(path) || !isPublicHTTPPath(path) {
			t.Errorf("%s: ветка полосы сессии узнаёт = %v, освобождён от каталога = %v — обязаны оба", path, IsLoginLanePath(path), isPublicHTTPPath(path))
		}
		if loginLaneRelaysWhenUnanswered(path) {
			t.Errorf("%s читает носитель — при службе, не ответившей о сессии, край обязан отвечать сам (KA1), а не ретранслировать", path)
		}
		if loginLaneOpenBeforeAddressConfirmation(path) {
			t.Errorf("%s доступен до подтверждения адреса — по приёмке F8-S6 Р1 закрыт, перечень девяти закрыт приёмкой F6b", path)
		}
		if rt.CarriesClientBasic() {
			t.Errorf("%s несёт удостоверение клиента базовой схемой — исключение одно, у обмена кода", path)
		}
	}
	if LoginLaneVerb(emailChangeWant["email-change"]) == LoginLaneVerb(emailChangeWant["email-change-confirm"]) {
		t.Errorf("запрос смены и предъявление кода получили одно имя — точное совпадение обязано их различать")
	}
	// Отрицательный контроль: соседи по имени — не глаголы и не освобождены.
	for _, p := range []string{"/iam/v1/auth/email-change/", "/iam/v1/auth/email-changex", "/iam/v1/auth/email-change/confirm/",
		"/iam/v1/auth/email-change/confirmx", "/iam/v1/auth/email-change/other", "/iam/v1/auth/email"} {
		if IsLoginLanePath(p) || isPublicHTTPPath(p) {
			t.Errorf("путь %q признан глаголом формы или освобождённым — совпадение обязано быть точным", p)
		}
	}
	t.Logf("перепись: записей объявления %d · смены адреса по приёмке %d", len(LoginLaneRoutes()), len(emailChangeWant))
}
