// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// login_lane_paths.go — ЕДИНСТВЕННОЕ объявление глаголов полосы формы
// (приёмка Ф3 Р2, §8 инв. 7): четыре глагола Ф3, регистрация Ф4 (kacho#2699),
// два глагола восстановления доступа Ф5 (kacho#2701), шесть глаголов второго
// фактора Ф12 (приёмка Ф12 Р4, kacho#1281) и два глагола подтверждения адреса
// почты (приёмка F6b Р5, kacho#2900) — пятнадцать путей, тот же перечень, что
// служба объявляет у своего слушателя (`loginlanehttp.Paths()`).
//
// # Кто это читает — трое, и второго объявления нет
//
//   - перечень путей без записи каталога (`isPublicHTTPPath`, Р8): глагол формы
//     освобождён от решения по каталогу прав, от пола уверенности и от полосы
//     привязки предъявителя — но НЕ от полосы сессии (§1.8);
//   - ветка полосы сессии (`tryOwnSession`, Р7): на этих путях «сессии нет»
//     РЕТРАНСЛИРУЕТСЯ службе, а не отвергается; отсечка отвергается как всюду;
//     недоступность службы ретранслируется ровно на глаголах, чья запись это
//     разрешает (`relayWhenUnanswered`), на остальных — F4d-23; сессия с
//     неподтверждённым адресом почты доходит ровно до глаголов, чья запись это
//     объявила (`openBeforeAddressConfirmation`, приёмка F6b Р5), на остальных —
//     отказ адреса;
//   - регистрация ретрансляции в композиционном корне: обработчик крепится на
//     каждый путь перечня под посадкой `own`.
//
// Второе объявление тех же путей разошлось бы молча: путь, освобождённый и не
// ретранслируемый, отвечал бы 404 краем; ретранслируемый и не освобождённый —
// отказом каталога до службы. Ровно так три глагола Ф4/Ф5 и выглядели до
// расширения: служба их обслуживала, край отвечал 404 (kacho#2699, kacho#2701).
//
// # Совпадение ТОЧНОЕ
//
// Не приставка: `/iam/v1/auth/` уже однажды была приставкой, и всякий новый
// маршрут под ней наследовал освобождение, никем не решённое (`authz_util.go`).
// Параметры запроса (`?form=<вид>`) к пути не относятся. Путь завершения
// восстановления — подпуть пути запроса кода, и точное совпадение здесь несущее:
// приставочное не различало бы два глагола.
package middleware

// Пути глаголов. Написание подпутём, а не суффиксом `:verb`, взято у
// существующего маршрута «кто я» того же семейства (Р2).
const (
	LoginLanePathLogin    = "/iam/v1/auth/login"
	LoginLanePathLogout   = "/iam/v1/auth/logout"
	LoginLanePathPassword = "/iam/v1/auth/password" // #nosec G101 -- путь глагола смены пароля, а не удостоверение
	LoginLanePathCSRF     = "/iam/v1/auth/csrf"
	// LoginLanePathRegister — регистрация паролем (Ф4): та же форма ответа, то
	// же печенье, свой вид признака формы.
	LoginLanePathRegister = "/iam/v1/auth/register"
	// Восстановление доступа (Ф5): запрос кода и его предъявление с новым
	// паролем — два глагола, две формы, два вида признака.
	LoginLanePathRecovery         = "/iam/v1/auth/recovery"
	LoginLanePathRecoveryComplete = "/iam/v1/auth/recovery/complete"
	// Второй фактор (Ф12 Р4, kacho#1281): четыре глагола семейства подпутями,
	// чтение состояния на корне семейства, церемония повышения своим подпутём.
	// Те же полоса, признак формы и ретрансляция, что у четырёх глаголов Ф3;
	// исход «сессии нет» ретранслируется — судит служба (401 у всех шести).
	// Недоступность службы — F4d-23 на крае, как на смене пароля: все шесть
	// читают сессию носителя (Ф12 Р4, Ф12-38).
	LoginLanePathSecondFactor            = "/iam/v1/auth/second-factor"
	LoginLanePathSecondFactorEnroll      = "/iam/v1/auth/second-factor/enroll"
	LoginLanePathSecondFactorConfirm     = "/iam/v1/auth/second-factor/confirm"
	LoginLanePathSecondFactorRemove      = "/iam/v1/auth/second-factor/remove"
	LoginLanePathSecondFactorBackupCodes = "/iam/v1/auth/second-factor/backup-codes"
	LoginLanePathStepUp                  = "/iam/v1/auth/step-up"
	// Подтверждение адреса почты (приёмка F6b Р5; Р6 службы): запрос письма с
	// кодом и предъявление кода — два глагола под сессией человека, оба доступны
	// до подтверждения, оба читают носитель.
	LoginLanePathVerifyEmail        = "/iam/v1/auth/verify-email"
	LoginLanePathVerifyEmailConfirm = "/iam/v1/auth/verify-email/confirm"
)

// LoginLaneRoute — глагол формы: имя для счётчиков и путь на адресе консоли.
type LoginLaneRoute struct {
	// Verb — закрытое имя глагола; значение метки ретрансляции (Ф3-48).
	Verb string
	// Path — точный путь на origin консоли.
	Path string
	// relayWhenUnanswered — что полоса сессии делает на этом глаголе, когда
	// служба не ответила краю (`Resolve` не ответил либо отсечку установить не
	// удалось). Критерий один — читает ли глагол носитель:
	//
	//   - true — ретранслировать, служба ответит своим 503. Глагол носителя не
	//     читает (вход, признак формы — Ф3 Р7; регистрация; запрос и предъявление
	//     кода восстановления ключуются адресом и кодом) либо сессию оканчивает
	//     (выход — Ф3-17);
	//   - нулевое значение — отказ F4d-23 на крае, носитель цел. Глагол читает
	//     сессию носителя (смена пароля — Ф3-20 «д»; шесть глаголов второго
	//     фактора, включая чтение состояния, — Ф12 Р4; два глагола
	//     подтверждения адреса — Р6 службы), и запрос с носителем, чью отсечку
	//     установить не удалось, до него не доходит.
	//
	// Отказ — умолчание: глагол, дописанный без решения, получает F4d-23.
	// Поле не экспортируется: решение принадлежит полосе сессии, и прочие
	// читатели объявления его не видят.
	relayWhenUnanswered bool
	// openBeforeAddressConfirmation — доходит ли до глагола сессия, чей адрес
	// почты не подтверждён (приёмка F6b Р5). Доступных шесть: признак формы,
	// вход, выход, регистрация и оба глагола подтверждения — то, что нужно
	// человеку, чтобы войти, подтвердить адрес и выйти. Прочим сессия с
	// неподтверждённым адресом получает отказ адреса (Р3) и до службы не
	// доходит; без носителя сессии решение не действует вовсе — анонимный вызов
	// судится как прежде.
	//
	// Восстановление доступа закрыто здесь СТРОЖЕ службы и намеренно (Р5): оно
	// сессии не требует, код восстановления служба чеканит только подтверждённому
	// адресу, и человек с неподтверждённой сессией к нему не зовётся.
	//
	// Отказ — умолчание, как у `relayWhenUnanswered`: глагол, дописанный без
	// решения, до подтверждения адреса недоступен.
	openBeforeAddressConfirmation bool
}

// loginLaneRoutes — сам перечень. Порядок — порядок Р2, затем Ф4, Ф5, Ф12 и
// F6b; читатели по нему не ветвятся.
var loginLaneRoutes = []LoginLaneRoute{
	{Verb: "login", Path: LoginLanePathLogin, relayWhenUnanswered: true, openBeforeAddressConfirmation: true},
	{Verb: "logout", Path: LoginLanePathLogout, relayWhenUnanswered: true, openBeforeAddressConfirmation: true},
	{Verb: "password", Path: LoginLanePathPassword},
	{Verb: "csrf", Path: LoginLanePathCSRF, relayWhenUnanswered: true, openBeforeAddressConfirmation: true},
	{Verb: "register", Path: LoginLanePathRegister, relayWhenUnanswered: true, openBeforeAddressConfirmation: true},
	{Verb: "recovery", Path: LoginLanePathRecovery, relayWhenUnanswered: true},
	{Verb: "recovery-complete", Path: LoginLanePathRecoveryComplete, relayWhenUnanswered: true},
	{Verb: "second-factor-status", Path: LoginLanePathSecondFactor},
	{Verb: "second-factor-enroll", Path: LoginLanePathSecondFactorEnroll},
	{Verb: "second-factor-confirm", Path: LoginLanePathSecondFactorConfirm},
	{Verb: "second-factor-remove", Path: LoginLanePathSecondFactorRemove},
	{Verb: "second-factor-backup-codes", Path: LoginLanePathSecondFactorBackupCodes},
	{Verb: "step-up", Path: LoginLanePathStepUp},
	{Verb: "verify-email", Path: LoginLanePathVerifyEmail, openBeforeAddressConfirmation: true},
	{Verb: "verify-email-confirm", Path: LoginLanePathVerifyEmailConfirm, openBeforeAddressConfirmation: true},
}

// LoginLaneRoutes отдаёт КОПИЮ перечня глаголов формы.
func LoginLaneRoutes() []LoginLaneRoute {
	out := make([]LoginLaneRoute, len(loginLaneRoutes))
	copy(out, loginLaneRoutes)
	return out
}

// IsLoginLanePath — принадлежит ли путь перечню глаголов формы (точное
// совпадение).
func IsLoginLanePath(path string) bool {
	for _, rt := range loginLaneRoutes {
		if rt.Path == path {
			return true
		}
	}
	return false
}

// loginLaneRelaysWhenUnanswered — ретранслирует ли полоса сессии запрос на этот
// путь, когда служба не ответила краю. Путь вне перечня — false: на путях
// платформы недоступность всегда F4d-23.
func loginLaneRelaysWhenUnanswered(path string) bool {
	for _, rt := range loginLaneRoutes {
		if rt.Path == path {
			return rt.relayWhenUnanswered
		}
	}
	return false
}

// loginLaneOpenBeforeAddressConfirmation — доходит ли до этого глагола сессия с
// неподтверждённым адресом почты (приёмка F6b Р5). Путь вне перечня — false:
// решение не наследуется ни приставкой, ни соседом.
func loginLaneOpenBeforeAddressConfirmation(path string) bool {
	for _, rt := range loginLaneRoutes {
		if rt.Path == path {
			return rt.openBeforeAddressConfirmation
		}
	}
	return false
}

// LoginLaneVerb — имя глагола по пути; пустая строка, если путь не из перечня.
// Читатель — счётчик ретрансляции: значение метки берётся из объявления, а не
// выводится из строки пути.
func LoginLaneVerb(path string) string {
	for _, rt := range loginLaneRoutes {
		if rt.Path == path {
			return rt.Verb
		}
	}
	return ""
}
