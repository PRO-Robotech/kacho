// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// login_lane_paths.go — ЕДИНСТВЕННОЕ объявление глаголов полосы формы: четырёх
// глаголов Ф3 (приёмка Ф3 Р2, §8 инв. 7) и шести глаголов второго фактора Ф12
// (приёмка Ф12 Р4, kacho#1281) — десяти.
//
// # Кто это читает — трое, и второго объявления нет
//
//   - перечень путей без записи каталога (`isPublicHTTPPath`, Р8): глагол формы
//     освобождён от решения по каталогу прав, от пола уверенности и от полосы
//     привязки предъявителя — но НЕ от полосы сессии (§1.8);
//   - ветка полосы сессии (`tryOwnSession`, Р7): на этих путях «сессии нет»
//     РЕТРАНСЛИРУЕТСЯ службе, а не отвергается; отсечка отвергается как всюду;
//   - регистрация ретрансляции в композиционном корне: обработчик крепится на
//     каждый путь перечня под посадкой `own`.
//
// Второе объявление тех же путей разошлось бы молча: путь, освобождённый и не
// ретранслируемый, отвечал бы 404 краем; ретранслируемый и не освобождённый —
// отказом каталога до службы.
//
// # Совпадение ТОЧНОЕ
//
// Не приставка: `/iam/v1/auth/` уже однажды была приставкой, и всякий новый
// маршрут под ней наследовал освобождение, никем не решённое (`authz_util.go`).
// Параметры запроса (`?form=<вид>`) к пути не относятся.
package middleware

// Пути четырёх глаголов. Написание подпутём, а не суффиксом `:verb`, взято у
// существующего маршрута «кто я» того же семейства (Р2).
const (
	LoginLanePathLogin    = "/iam/v1/auth/login"
	LoginLanePathLogout   = "/iam/v1/auth/logout"
	LoginLanePathPassword = "/iam/v1/auth/password" // #nosec G101 -- путь глагола смены пароля, а не удостоверение
	LoginLanePathCSRF     = "/iam/v1/auth/csrf"
	// Второй фактор (Ф12 Р4, kacho#1281): четыре глагола семейства подпутями,
	// чтение состояния на корне семейства, церемония повышения своим подпутём.
	// Те же полоса, признак формы и ретрансляция, что у четырёх глаголов Ф3;
	// исход «сессии нет» ретранслируется — судит служба (401 у всех шести).
	LoginLanePathSecondFactor            = "/iam/v1/auth/second-factor"
	LoginLanePathSecondFactorEnroll      = "/iam/v1/auth/second-factor/enroll"
	LoginLanePathSecondFactorConfirm     = "/iam/v1/auth/second-factor/confirm"
	LoginLanePathSecondFactorRemove      = "/iam/v1/auth/second-factor/remove"
	LoginLanePathSecondFactorBackupCodes = "/iam/v1/auth/second-factor/backup-codes"
	LoginLanePathStepUp                  = "/iam/v1/auth/step-up"
)

// LoginLaneRoute — глагол формы: имя для счётчиков и путь на адресе консоли.
type LoginLaneRoute struct {
	// Verb — закрытое имя глагола; значение метки ретрансляции (Ф3-48).
	Verb string
	// Path — точный путь на origin консоли.
	Path string
}

// loginLaneRoutes — сам перечень. Порядок — порядок Р2; читатели по нему не
// ветвятся.
var loginLaneRoutes = []LoginLaneRoute{
	{Verb: "login", Path: LoginLanePathLogin},
	{Verb: "logout", Path: LoginLanePathLogout},
	{Verb: "password", Path: LoginLanePathPassword},
	{Verb: "csrf", Path: LoginLanePathCSRF},
	{Verb: "second-factor-status", Path: LoginLanePathSecondFactor},
	{Verb: "second-factor-enroll", Path: LoginLanePathSecondFactorEnroll},
	{Verb: "second-factor-confirm", Path: LoginLanePathSecondFactorConfirm},
	{Verb: "second-factor-remove", Path: LoginLanePathSecondFactorRemove},
	{Verb: "second-factor-backup-codes", Path: LoginLanePathSecondFactorBackupCodes},
	{Verb: "step-up", Path: LoginLanePathStepUp},
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
