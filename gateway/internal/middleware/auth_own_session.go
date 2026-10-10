// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// auth_own_session.go — полоса личности под посадкой `own`: НАША сессия,
// прочитанная по носителю (приёмка Ф3, Р7; §4.1 п.9).
//
// # Два вопроса службе, и ответ о сессии отсечку не применяет
//
// На каждом предъявлении полоса спрашивает службу ДВАЖДЫ: о сессии по носителю
// (`Resolve`) и об отсечке субъекта (`SessionCutoffOf`), и сравнивает момент
// аутентификации с отсечкой сама — включающе, на микросекундах, общим
// читателем отсечки (`sessionCutoffCheck`). Один вопрос, применивший
// отсечку внутри службы, оставил бы второго читателя отсечки в службе и сделал
// бы неконструируемым «UNIMPLEMENTED только об отсечке — проход громко»
// (Ф1-56, F4d-29).
//
// # Что полоса делает на каждом исходе — на путях платформы и на «кто я»
//
//   - носителя нет → анонимно дальше, как сегодня (судит следующее звено);
//   - «сессии нет» при носителе → F4d-22: единый отказ 401 края (приёмка KA1,
//     Р2), носитель гасится. Пять причин (неизвестен · снят выходом · истёк · заблокирована ·
//     отсечён) — один отказ (Ф1-17, Ф3-10). ЭТО СМЕНА ПОВЕДЕНИЯ полосы: прежняя
//     полоса пропускала такой носитель анонимно с целым печеньем (§1.8);
//   - служба не ответила ни на один из двух вопросов, ответила UNAVAILABLE либо
//     UNIMPLEMENTED о сессии → ответ Р1 приёмки KA1 (`503`, один на все полосы,
//     credential_state_unknown.go), носитель цел. Он заменяет F4d-23 («тот же
//     код и текст, что у отсечки»): `401` на нашу неисправность уводил человека
//     на вход при живой сессии;
//   - UNIMPLEMENTED только об отсечке при живой сессии → проход, громко, со
//     счётчиком (окно раската);
//   - неклассифицированный ответ → отказ. Корзины «прочее» нет;
//   - живая неотсечённая сессия с НЕПОДТВЕРЖДЁННЫМ адресом почты на пути вне
//     перечня прохода → отказ адреса (приёмка F6b, Р3, Р4): 403, значение
//     службы, носитель цел, вызова на повышение уровня нет.
//
// # Рубеж адреса — после годности сессии, до пола уровня и личности (F6b Р4)
//
// Порядок: сессия → отсечка → АДРЕС → пол уровня → личность. Годность носителя
// решается раньше его свойств: снятая или отсечённая сессия получает прежний
// отказ F4d-22, неответ службы — ответ Р1 (KA1), и «адрес неизвестен» проходом
// не бывает. Пол уровня — позже адреса: человеку без подтверждения повышать
// уровень не нужно, ему нечего получать. Перечень прохода —
// `openBeforeAddressConfirmation` (address_refusal.go). У рубежа нет ни ручки,
// ни режима, ни исключения для стенда (Р11).
//
// # На записях объявления полоса отличается двумя исходами (Р7, Р16)
//
// Записи объявления — глаголы формы и три координаты церемонии авторизации
// (`login_lane_paths.go`). «Сессии нет» полоса РЕТРАНСЛИРУЕТ на каждой — исход
// судит служба по записи (выход идемпотентен, смена пароля и глаголы второго
// фактора отвергают, вход выдаёт, признак выдаётся; церемония, не найдя сессии,
// отправляет человека на вход, а не выводит его из системы — замысел LINE-A-1
// §5.1б п. 4). Отсечку она отвергает и здесь:
// носитель отсечённой сессии до службы не доходит (Ф3-51), потому что читатель
// отсечки ОДИН и он на крае.
//
// Недоступность службы решается по тому, ЧИТАЕТ ЛИ глагол носитель. Глагол,
// читающий сессию носителя, получает ответ Р1 (KA1): запрос с носителем, чью
// отсечку установить не удалось, до него не доходит. Таковы смена пароля (Ф3-20 «д») и
// шесть глаголов второго фактора, включая чтение состояния (Ф12 Р4, Ф12-38).
// Ретранслируются (служба ответит своим 503) глаголы, носителя не читающие, —
// вход и признак формы (Ф3 Р7), регистрация (Ф4), запрос и предъявление кода
// восстановления (Ф5: ключуются адресом и кодом), — и выход, который сессию
// оканчивает (Ф3-17). Решение стоит в записи глагола (`relayWhenUnanswered` в
// `login_lane_paths.go`), и нулевое значение означает отказ: глагол, дописанный
// без решения, получает ответ Р1 (KA1). Ветка читает то же объявление путей, которым
// регистрируется ретрансляция, а не `isPublicHTTPPath`: «кто я» стоит в
// последнем и точкой предъявления остаётся.
package middleware

import (
	"errors"
	"net/http"

	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
)

// tryOwnSession — полоса нашей сессии. Возвращает запрос, с которым цепочка
// продолжается (тот же либо с помеченным контекстом), и пару исходов:
// injected — личность выставлена; handled — полоса ответила сама и вызывающий
// обязан вернуться.
func (a *AuthInterceptor) tryOwnSession(w http.ResponseWriter, r *http.Request) (next *http.Request, injected, handled bool) {
	if a.humanSession == nil {
		return r, false, false
	}
	// Носитель читается по имени печенья, а не подстрокой заголовка: значение
	// уходит службе КАК ЕСТЬ, и его границы обязаны быть теми, что провёл
	// браузер. Чужое печенье сессии без нашего носителем не является (Ф1-52).
	//
	// Предикат ОБЩИЙ с маршрутом «кто я» (`ourSessionCarrierOf`): две полосы,
	// читающие одну сессию, обязаны одинаково отвечать на вопрос «наш ли это
	// запрос».
	bearer, ok := ourSessionCarrierOf(r)
	if !ok {
		return r, false, false
	}
	route := r.URL.Path
	formVerb := IsLoginLanePath(route)
	// Недоступность на форме ретранслируется ровно на глаголах, чья запись это
	// разрешает; на всех прочих, включая дописанные без решения, — ответ Р1
	// (KA1), как на путях платформы (Р7, круг 2 Б-4; Ф12 Р4).
	relayOnUnavailable := loginLaneRelaysWhenUnanswered(route)

	sess, found, err := a.humanSession.ResolveHumanSession(r.Context(), bearer)
	if err != nil {
		if relayOnUnavailable {
			return r, false, false
		}
		// Ответ Р1 (KA1): `UNIMPLEMENTED` о сессии — тот же ответ, что молчание:
		// годность носителя не подтверждена ничем, и «проход громко» означал бы
		// личность из ниоткуда. Носитель цел — гасить живую сессию из-за своей
		// заминки значило бы выкидывать всех при первом перебое.
		a.sessionLane.recordUnavailable()
		a.reportOwnSessionUnavailable(err, route)
		writeCredentialStateUnknown(w)
		return r, false, true
	}
	if !found {
		if formVerb {
			// Исход судит служба по записи (Ф1-18, Ф3-20 «в», Ф3-01, Ф3-35).
			return r, false, false
		}
		a.sessionLane.recordNoSession()
		EndSessionCarriers(w)
		writeAuthnRefusal(w)
		return r, false, true
	}

	subj := Subject{Type: "user", ID: sess.UserID, DisplayName: sess.DisplayName}
	switch a.sessionCutoffCheck(r.Context(), subj, sess.AuthenticatedAt, route) {
	case sessionCutoffEnded:
		// На ЛЮБОМ пути, включая глаголы формы (Ф3-51).
		a.sessionLane.recordCutoffDenied()
		EndSessionCarriers(w)
		writeAuthnRefusal(w)
		return r, false, true
	case sessionCutoffUnanswered:
		if relayOnUnavailable {
			return r, false, false
		}
		// Ответ Р1 (KA1), носитель цел.
		a.sessionLane.recordUnavailable()
		writeCredentialStateUnknown(w)
		return r, false, true
	case sessionCutoffUnsupported:
		a.sessionLane.recordRolloutWindow()
	case sessionCutoffNotAsked, sessionCutoffLive:
	}

	// Рубеж адреса (F6b Р4): после отсечки, до пола уверенности и личности.
	// Строка журнала называет субъекта, а не адрес почты.
	if !sess.EmailVerified && !openBeforeAddressConfirmation(r) {
		a.sessionLane.recordAddressNotVerified()
		a.logger.Info("auth.HTTP: own session refused — email address not verified",
			"id", subj.ID, "route", route)
		writeHTTPAddressRefusal(w)
		return r, false, true
	}

	// Пол уверенности — тот же вопрос, что на полосах предъявителя; уровень нашей
	// сессии уже на оси каталога (Ф11 Р7): перевода нет, есть проверка оси
	// сессии, и значение вне неё уезжает пустым — громко (Ф11-19).
	assurance := a.ownSessionAssurance(subj, sess, route)
	if a.enforceStepUpHTTP(w, r, assurance, stepUpLaneSession) {
		return r, false, true
	}
	// Множества предъявленного наша сессия не несёт (Ф11 Р7): довод условия
	// `mfa_fresh` о ВИДЕ способа отсутствует, о свежести — момент аутентификации.
	setSessionAssuranceHeaders(r, assurance, nil)
	setPrincipalHeaders(r, subj.Type, subj.ID, subj.DisplayName)
	setSessionRecordHeader(r, sess.SessionID)
	// Носитель — для ТОГО ЖЕ вопроса с открытого соединения (kacho#2900): отметку
	// адреса служба называет только в ответе о сессии по носителю, и перепрос
	// потоков спрашивает её им же. Записывается после всех вердиктов полосы.
	r = r.WithContext(principalmeta.WithPresented(r.Context(), principalmeta.PresentedSession(bearer)))
	a.logger.Info("auth.HTTP: Principal injected (own session)", "type", subj.Type, "id", subj.ID)
	return r, true, false
}

// setSessionRecordHeader — номер записи текущей сессии для службы
// (kaname#677): по нему снятие ключа доступа щадит сессию, из которой ключ
// снят (Ф13 Р8).
//
// Канал — тот же, что у личности: заголовок подсемейства `x-kacho-token-`,
// клиентские значения которого вычищены до выбора полосы
// (`stripForgeableIdentityHeaders`), а за мост его пускает
// `principalHeaderMatcher`; служба читает его только за вердиктом о
// доверенном отправителе.
//
// Ставится ОДНА форма — мостовая. Мост снимает приставку сам и голую форму
// пропустил бы тоже, а служба принимает номер, только когда значение одно:
// две формы дали бы два значения, и текущая осталась бы неназванной.
//
// Номера нет в ответе службы — заголовок не ставится вовсе: пустое значение
// служба прочла бы как «не названо» и так, но ключ без значения — запись,
// которую никто не производил.
func setSessionRecordHeader(r *http.Request, record string) {
	if record == "" {
		return
	}
	r.Header.Set(principalmeta.HeaderGRPCMetaTokenSessionID, record)
}

// reportOwnSessionUnavailable докладывает о службе, не ответившей о сессии, с
// тем же ограничением частоты, что у отсечки: два вопроса одному соседу — одно
// окно доклада, иначе всплеск одного подавлял бы первый доклад другого.
func (a *AuthInterceptor) reportOwnSessionUnavailable(err error, route string) {
	report, total, represents := a.sessionCutoffFailures.observe()
	if !report {
		return
	}
	msg := "human session lookup unanswered; refusing browser session"
	switch {
	case errors.Is(err, ErrIntrospectionMisconfigured):
		// kacho#2741: спрошен не тот слушатель — настройка, раскат не лечит.
		msg = "human session lookup: the asked listener does not serve the identity service " +
			"(misaddressed — fix the address); refusing browser session"
	case errors.Is(err, ErrHumanSessionUnsupported):
		msg = "human session lookup not offered by the authority; refusing browser session (image skew)"
	}
	a.logger.Error(msg, "err", err, "route", route,
		"session_lane_failures_total", total, "occurrences_since_last_report", represents)
}
