// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// login_lane_paths.go — ЕДИНСТВЕННОЕ объявление путей, которые край
// РЕТРАНСЛИРУЕТ службе доступа сырым HTTP (род I краевой записи — замысел
// LINE-A-1 §5.1а). Записей два вида, и у каждой своя цель ретрансляции:
//
//   - глаголы полосы формы (приёмка Ф3 Р2, §8 инв. 7): четыре глагола Ф3,
//     регистрация Ф4 (kacho#2699), два глагола восстановления доступа Ф5
//     (kacho#2701) и шесть глаголов второго фактора Ф12 (приёмка Ф12 Р4,
//     kacho#1281) — тринадцать путей, тот же перечень, что служба объявляет у
//     своего слушателя формы (`loginlanehttp.Paths()`); цель — слушатель формы;
//   - координаты церемонии авторизации (замысел LINE-A-1 §5.1, полоса L13,
//     kacho#2817): навигация на эндпоинт авторизации и обмен кода — две записи,
//     не три (обнаружение в A-1 наружу не публикуется, §5.1б п. 5); цель —
//     слушатель выдачи службы, ОДНА на обе координаты: церемония целиком живёт
//     на одном слушателе (§5.1б п. 2).
//
// # Кто это читает — трое, и второго объявления нет
//
//   - перечень путей без записи каталога (`isPublicHTTPPath`, Р8): запись
//     освобождена от решения по каталогу прав, от пола уверенности, от полосы
//     привязки предъявителя и от вопроса об отзыве — но НЕ от полосы сессии
//     (§1.8);
//   - ветка полосы сессии (`tryOwnSession`, Р7): на этих путях «сессии нет»
//     РЕТРАНСЛИРУЕТСЯ службе, а не отвергается; отсечка отвергается как всюду;
//     недоступность службы ретранслируется ровно на записях, которые это
//     разрешают (`relayWhenUnanswered`), на остальных — F4d-23;
//   - монтаж в композиционном корне (`handler.MountLoginLaneRoutes`): на каждый
//     путь перечня крепится ретранслятор ЕГО цели под посадкой `own`.
//
// Второе объявление тех же путей разошлось бы молча: путь, освобождённый и не
// ретранслируемый, отвечал бы 404 краем; ретранслируемый и не освобождённый —
// отказом каталога до службы. Ровно так три глагола Ф4/Ф5 и выглядели до
// расширения: служба их обслуживала, край отвечал 404 (kacho#2699, kacho#2701).
//
// # Координатам церемонии здесь место, а не в таблицах маршрутов прав
//
// Сгенерированная таблица маршрутов прав выводится из аннотаций контракта и
// помечена «не править», а контракта церемония не заводит (З1): вписанная руками
// строка исчезла бы при следующей генерации молча и по чужому поводу. Таблица
// собственных ручек края (`rest_route_edge.go`) переводит путь в ИМЯ МЕТОДА,
// чтобы полоса прав нашла запись каталога; имени метода у церемонии нет, и
// промах мимо каталога есть отказ — путь был бы не открытым, а мёртвым.
//
// # Состав освобождения координат церемонии — РЕШЕНИЕ, а не побочный эффект
//
// Членство в перечне снимает четыре вещи (§5.1б п. 3), и для церемонии каждая
// снята потому, что на ней нечего решать: записи каталога нет и не будет (имени
// метода нет); шаг вверх обслуживает сама церемония (второе решение о том же
// полу разошлось бы с первым); предъявителя на этих путях нет — навигация несёт
// сессию, обмен — код и удостоверение клиента; вопрос об отзыве задаётся о
// предъявителе, которого ни одно из двух решений не читает, и отказ по нему был
// бы отказом по случайности. Радиус освобождения ограничен безусловным снятием
// `Authorization` в обеих формах имени ретранслятором (`login_lane_relay.go`):
// неспрошенный предъявитель до службы не доезжает и полномочием ниже по течению
// не становится. ЭТО ПАРА (§7 инв. 37): исключение в составе ретранслированного
// запроса, проносящее `Authorization` на координату обмена, переоткрывает
// освобождение от вопроса об отзыве — оно принимается заново, а не наследуется.
//
// # Совпадение ТОЧНОЕ
//
// Не приставка: `/iam/v1/auth/` уже однажды была приставкой, и всякий новый
// маршрут под ней наследовал освобождение, никем не решённое (`authz_util.go`).
// Параметры запроса (`?form=<вид>`, строка запроса церемонии) к пути не
// относятся. Путь завершения восстановления — подпуть пути запроса кода, и
// точное совпадение здесь несущее: приставочное не различало бы два глагола. У
// навигации церемонии соседи по имени — четыре глагола проверки доступа
// `/iam/v1/authorize:*`: суффикс `:verb` образует другой путь, и его обслуживает
// транскодер REST→gRPC.
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
)

// Координаты церемонии авторизации (замысел LINE-A-1 §5.1). Навигация — бэрый
// путь без суффикса: его сосед `/iam/v1/authorize:check` принадлежит проверке
// доступа. Обмен кода — путь существующего токен-эндпоинта службы: ветви
// церемонии ложатся в его же обработчик, второго пути не заводится.
const (
	CeremonyPathAuthorize = "/iam/v1/authorize"
	CeremonyPathToken     = "/iam/v1/token" // #nosec G101 -- путь эндпоинта обмена, а не удостоверение
)

// RelayTarget — слушатель службы доступа, на который край ретранслирует запись
// объявления. Закрытый перечень: нулевое значение и чужое слово целью не
// являются, и запись, дописанная без решения о цели, не ретранслируется никуда.
//
// Целей две и сводимы они не к одной (замысел LINE-A-1 §5.1б п. 2а): у них
// разные ручки адреса, разные порты и разный режим предъявления клиента.
type RelayTarget string

const (
	// RelayTargetForm — слушатель полосы формы: взаимный TLS, допускает ровно край.
	RelayTargetForm RelayTarget = "form"
	// RelayTargetIssuance — слушатель выдачи: на нём церемония живёт целиком.
	RelayTargetIssuance RelayTarget = "issuance"
)

// RelayTargets — закрытый перечень целей в порядке объявления.
func RelayTargets() []RelayTarget {
	return []RelayTarget{RelayTargetForm, RelayTargetIssuance}
}

// Valid — принадлежит ли цель закрытому перечню.
func (t RelayTarget) Valid() bool {
	for _, known := range RelayTargets() {
		if t == known {
			return true
		}
	}
	return false
}

// LoginLaneRoute — запись объявления: имя для счётчиков, путь на адресе
// консоли и цель ретрансляции.
type LoginLaneRoute struct {
	// Verb — закрытое имя записи; значение метки ретрансляции (Ф3-48).
	Verb string
	// Path — точный путь на origin консоли.
	Path string
	// Target — слушатель службы, на который запись ретранслируется.
	Target RelayTarget
	// relayWhenUnanswered — что полоса сессии делает на этом глаголе, когда
	// служба не ответила краю (`Resolve` не ответил либо отсечку установить не
	// удалось). Критерий один — читает ли глагол носитель:
	//
	//   - true — ретранслировать, служба ответит своим 503. Глагол носителя не
	//     читает (вход, признак формы — Ф3 Р7; регистрация; запрос и предъявление
	//     кода восстановления ключуются адресом и кодом; навигация церемонии —
	//     вопрос о сессии решает сама церемония своим швом; обмен кода — решает
	//     по коду и удостоверению клиента, замысел LINE-A-1 §5.1б п. 4) либо
	//     сессию оканчивает (выход — Ф3-17);
	//   - нулевое значение — отказ F4d-23 на крае, носитель цел. Глагол читает
	//     сессию носителя (смена пароля — Ф3-20 «д»; шесть глаголов второго
	//     фактора, включая чтение состояния, — Ф12 Р4), и запрос с носителем,
	//     чью отсечку установить не удалось, до него не доходит.
	//
	// Отказ — умолчание: глагол, дописанный без решения, получает F4d-23.
	// Поле не экспортируется: решение принадлежит полосе сессии, и прочие
	// читатели объявления его не видят.
	relayWhenUnanswered bool
}

// loginLaneRoutes — сам перечень. Порядок — порядок Р2, затем Ф4, Ф5, Ф12 и
// координаты церемонии; читатели по нему не ветвятся.
var loginLaneRoutes = []LoginLaneRoute{
	{Verb: "login", Path: LoginLanePathLogin, Target: RelayTargetForm, relayWhenUnanswered: true},
	{Verb: "logout", Path: LoginLanePathLogout, Target: RelayTargetForm, relayWhenUnanswered: true},
	{Verb: "password", Path: LoginLanePathPassword, Target: RelayTargetForm},
	{Verb: "csrf", Path: LoginLanePathCSRF, Target: RelayTargetForm, relayWhenUnanswered: true},
	{Verb: "register", Path: LoginLanePathRegister, Target: RelayTargetForm, relayWhenUnanswered: true},
	{Verb: "recovery", Path: LoginLanePathRecovery, Target: RelayTargetForm, relayWhenUnanswered: true},
	{Verb: "recovery-complete", Path: LoginLanePathRecoveryComplete, Target: RelayTargetForm, relayWhenUnanswered: true},
	{Verb: "second-factor-status", Path: LoginLanePathSecondFactor, Target: RelayTargetForm},
	{Verb: "second-factor-enroll", Path: LoginLanePathSecondFactorEnroll, Target: RelayTargetForm},
	{Verb: "second-factor-confirm", Path: LoginLanePathSecondFactorConfirm, Target: RelayTargetForm},
	{Verb: "second-factor-remove", Path: LoginLanePathSecondFactorRemove, Target: RelayTargetForm},
	{Verb: "second-factor-backup-codes", Path: LoginLanePathSecondFactorBackupCodes, Target: RelayTargetForm},
	{Verb: "step-up", Path: LoginLanePathStepUp, Target: RelayTargetForm},
	{Verb: "authorize", Path: CeremonyPathAuthorize, Target: RelayTargetIssuance, relayWhenUnanswered: true},
	{Verb: "token", Path: CeremonyPathToken, Target: RelayTargetIssuance, relayWhenUnanswered: true},
}

// LoginLaneRoutes отдаёт КОПИЮ перечня записей объявления.
func LoginLaneRoutes() []LoginLaneRoute {
	out := make([]LoginLaneRoute, len(loginLaneRoutes))
	copy(out, loginLaneRoutes)
	return out
}

// IsLoginLanePath — принадлежит ли путь объявлению (точное совпадение).
func IsLoginLanePath(path string) bool {
	_, ok := LoginLaneRouteFor(path)
	return ok
}

// LoginLaneRouteFor — запись объявления по пути (точное совпадение).
func LoginLaneRouteFor(path string) (LoginLaneRoute, bool) {
	for _, rt := range loginLaneRoutes {
		if rt.Path == path {
			return rt, true
		}
	}
	return LoginLaneRoute{}, false
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

// LoginLaneVerb — имя записи по пути; пустая строка, если путь не из перечня.
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
