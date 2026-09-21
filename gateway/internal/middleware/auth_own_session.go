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
// читателем (`sessionCutoffCheck`). Один вопрос, применивший
// отсечку внутри службы, оставил бы второго читателя отсечки в службе и сделал
// бы неконструируемым «UNIMPLEMENTED только об отсечке — проход громко»
// (Ф1-56, F4d-29).
//
// # Что полоса делает на каждом исходе — на путях платформы и на «кто я»
//
//   - носителя нет → анонимно дальше, как сегодня (судит следующее звено);
//   - «сессии нет» при носителе → F4d-22: 401 текстом отсечки, носитель
//     гасится. Пять причин (неизвестен · снят выходом · истёк · заблокирована ·
//     отсечён) — один отказ (Ф1-17, Ф3-10). ЭТО СМЕНА ПОВЕДЕНИЯ полосы: прежняя
//     полоса пропускала такой носитель анонимно с целым печеньем (§1.8);
//   - служба не ответила ни на один из двух вопросов, ответила UNAVAILABLE либо
//     UNIMPLEMENTED о сессии → F4d-23: ТОТ ЖЕ код и ТОТ ЖЕ текст, носитель цел;
//   - UNIMPLEMENTED только об отсечке при живой сессии → проход, громко, со
//     счётчиком (окно раската);
//   - неклассифицированный ответ → отказ. Корзины «прочее» нет.
//
// # На глаголах формы полоса отличается двумя исходами (Р7, Р16)
//
// «Сессии нет» она РЕТРАНСЛИРУЕТ на каждом глаголе формы — исход судит служба по
// записи (выход идемпотентен, смена пароля и глаголы второго фактора
// отвергают, вход выдаёт, признак выдаётся). Отсечку она отвергает и здесь:
// носитель отсечённой сессии до службы не доходит (Ф3-51), потому что читатель
// отсечки ОДИН и он на крае.
//
// Недоступность службы решается по тому, ЧИТАЕТ ЛИ глагол носитель. Глагол,
// читающий сессию носителя, получает F4d-23: запрос с носителем, чью отсечку
// установить не удалось, до него не доходит. Таковы смена пароля (Ф3-20 «д») и
// шесть глаголов второго фактора, включая чтение состояния (Ф12 Р4, Ф12-38).
// Ретранслируются (служба ответит своим 503) глаголы, носителя не читающие, —
// вход и признак формы (Ф3 Р7), регистрация (Ф4), запрос и предъявление кода
// восстановления (Ф5: ключуются адресом и кодом), — и выход, который сессию
// оканчивает (Ф3-17). Решение стоит в записи глагола (`relayWhenUnanswered` в
// `login_lane_paths.go`), и нулевое значение означает отказ: глагол, дописанный
// без решения, получает F4d-23. Ветка читает то же объявление путей, которым
// регистрируется ретрансляция, а не `isPublicHTTPPath`: «кто я» стоит в
// последнем и точкой предъявления остаётся.
package middleware

import (
	"errors"
	"net/http"
)

// tryOwnSession — полоса нашей сессии, и единственная полоса носителя
// браузерной сессии на этом крае. Возвращает запрос, с которым цепочка
// продолжается (тот же либо с помеченным контекстом), и пару признаков:
// injected — личность выставлена; handled — полоса ответила сама и вызывающий
// обязан вернуться.
func (a *AuthInterceptor) tryOwnSession(w http.ResponseWriter, r *http.Request) (next *http.Request, injected, handled bool) {
	if a.humanSession == nil {
		return r, false, false
	}
	// Носитель читается по имени печенья, а не подстрокой заголовка: значение
	// уходит службе КАК ЕСТЬ, и его границы обязаны быть теми, что провёл
	// браузер. Печенье, выписанное не нами, носителем не является (Ф1-52).
	carrier, err := r.Cookie(OurSessionCarrierName)
	if err != nil || carrier.Value == "" {
		return r, false, false
	}
	route := r.URL.Path
	formVerb := IsLoginLanePath(route)
	// Недоступность на форме ретранслируется ровно на глаголах, чья запись это
	// разрешает; на всех прочих, включая дописанные без решения, — F4d-23, как на
	// путях платформы (Р7, круг 2 Б-4; Ф12 Р4).
	relayOnUnavailable := loginLaneRelaysWhenUnanswered(route)

	sess, found, err := a.humanSession.ResolveHumanSession(r.Context(), carrier.Value)
	if err != nil {
		if relayOnUnavailable {
			return r, false, false
		}
		// F4d-23. `UNIMPLEMENTED` о сессии — тот же отказ: годность носителя не
		// подтверждена ничем, и «проход громко» означал бы личность из ниоткуда.
		a.sessionLane.recordUnavailable()
		a.reportOwnSessionUnavailable(err, route)
		writeHTTPUnauthorized(w, sessionCutoffDenyDescription)
		return r, false, true
	}
	if !found {
		if formVerb {
			// Исход судит служба по записи (Ф1-18, Ф3-20 «в», Ф3-01, Ф3-35).
			return r, false, false
		}
		a.sessionLane.recordNoSession()
		EndSessionCarriers(w)
		writeHTTPUnauthorized(w, sessionCutoffDenyDescription)
		return r, false, true
	}

	subj := Subject{Type: "user", ID: sess.UserID, DisplayName: sess.DisplayName}
	switch a.sessionCutoffCheck(r.Context(), subj, sess.AuthenticatedAt, route) {
	case sessionCutoffEnded:
		// На ЛЮБОМ пути, включая глаголы формы (Ф3-51).
		a.sessionLane.recordCutoffDenied()
		EndSessionCarriers(w)
		writeHTTPUnauthorized(w, sessionCutoffDenyDescription)
		return r, false, true
	case sessionCutoffUnanswered:
		if relayOnUnavailable {
			return r, false, false
		}
		a.sessionLane.recordUnavailable()
		writeHTTPUnauthorized(w, sessionCutoffDenyDescription)
		return r, false, true
	case sessionCutoffUnsupported:
		a.sessionLane.recordRolloutWindow()
	case sessionCutoffNotAsked, sessionCutoffLive:
	}

	// Пол уверенности — тот же вопрос, что на полосе предъявителя; уровень нашей
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
	a.logger.Info("auth.HTTP: Principal injected (own session)", "type", subj.Type, "id", subj.ID)
	return r, true, false
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
	if errors.Is(err, ErrHumanSessionUnsupported) {
		msg = "human session lookup not offered by the authority; refusing browser session (image skew)"
	}
	a.logger.Error(msg, "err", err, "route", route,
		"session_lane_failures_total", total, "occurrences_since_last_report", represents)
}
