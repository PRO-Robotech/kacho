// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package middleware — the edge's session-identity endpoint for the SPA consoles.
//
// Route:
//
//	GET /iam/v1/auth/me → the caller behind the current identity-provider session,
//	                      or {"user":null} when there is none.
//
// WHY THIS FILE HOLDS ONE ROUTE AND NOT FOUR. It used to register four: a
// sign-in redirect, an authorization-code callback, this one, and a cookie-clear.
// The first two conducted an interactive sign-in ceremony against a DIFFERENT
// identity provider than the one the platform deploys, and they addressed that
// provider's path shapes. They could not be switched on: the config keys that
// enabled them were declared by ZERO deployment profiles, and the client secret
// they needed was to be filled by a provisioning Job that KAC-127 removed. The
// refusal they returned named that Job and its Secret — pointing an operator at
// objects that no longer exist.
//
// So they were a gate whose input had no producer: registered unconditionally,
// looking wired, incapable of doing anything but refuse — on every stand, for
// their whole life. `api-conventions.md` §Принято-и-проигнорировано allows three
// outcomes and not a fourth; "leave it as is" is not one of them. The outcome
// chosen was RETIREMENT, because the ceremony is conducted by the identity
// provider's own sign-in console, which IS deployed and configured, and because
// the acceptance for this sub-phase already placed a Kachō-owned sign-in console
// out of scope. Implementing them would have been NEW work under an approved
// document that says the opposite — not the repair of something broken.
//
// The cookie those two produced (a session bearer minted by the retired
// provider's token endpoint) had exactly one producer — them — and one reader at
// the edge. Producer, carrier, reader and its logout-time cleanup were retired
// together, so no reader is left addressing an input nobody can emit.
//
// WHAT SURVIVED, AND WHY IT IS NOT PART OF THAT CEREMONY. `/iam/v1/auth/me`
// reads the session of the provider the platform ACTUALLY deploys and is called
// by four consoles plus the shared console library. It never took part in the
// retired flow: it resolves a live session cookie to a Kachō principal. The
// consoles' own sign-in already goes to the deployed provider's self-service
// flow, so nothing here is the entry point of a ceremony.
package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

// AdminChecker — port для проверки system-admin.
type AdminChecker interface {
	IsSystemAdmin(ctx context.Context, subject string) (bool, error)
}

// SessionIdentityHandler serves the edge's single session-identity route.
type SessionIdentityHandler struct {
	logger *slog.Logger
	// ЧИТАТЕЛЬ ПЕЧЕНЬЯ ЧУЖОЙ СЛУЖБЫ ЛИЧНОСТИ СНЯТ вместе со своей полосой:
	// носитель на этом маршруте один и он наш.
	subjectLookup SubjectLookuper // resolves identity.id → User/SA mirror in kaname
	adminCheck    AdminChecker    // optional admin-tuple lookup
	// sessionCutoff — НАШ авторитет отзыва. См. WithSessionCutoff.
	sessionCutoff SessionCutoffReader
	// humanSession — читатель НАШЕЙ сессии (посадка `own`, Ф3 Р7). Читатель на
	// этом маршруте ОДИН: второго, читавшего печенье чужой службы личности,
	// больше нет.
	humanSession HumanSessionReader
}

func NewSessionIdentityHandler(logger *slog.Logger) *SessionIdentityHandler {
	return &SessionIdentityHandler{logger: logger}
}

// WithSubjectLookup — подключает разбор личности в зеркало субъекта для «кто я».
func (h *SessionIdentityHandler) WithSubjectLookup(lookup SubjectLookuper) *SessionIdentityHandler {
	h.subjectLookup = lookup
	return h
}

// WithSessionCutoff — подключает читателя НАШЕЙ отсечки отзыва.
//
// ПОЧЕМУ ЭТОТ МАРШРУТ ТОЖЕ СПРАШИВАЕТ. Полос, читающих одну и ту же сессию, две:
// эта и полоса личности на пути запроса (`auth_session_cutoff.go`). Свойство,
// обязательное для одной, обязано быть проверено СРАВНЕНИЕМ полос, а не по
// каждой отдельно: консоль решает «вошёл ли я» именно отсюда, и маршрут,
// продолжающий называть человека вошедшим после принудительного выхода,
// оставляет его в системе с точки зрения того, кто смотрит на экран, — при том
// что каждый его вызов API уже отвергается.
//
// nil оставляет маршрут как прежде.
func (h *SessionIdentityHandler) WithSessionCutoff(r SessionCutoffReader) *SessionIdentityHandler {
	h.sessionCutoff = r
	return h
}

// WithHumanSession — подключает читателя НАШЕЙ сессии (посадка `own`).
//
// Маршрут «кто я» стоит ЗА полосой личности (Д13): отвергнутую сессию полоса
// гасит F4d-22 до этого обработчика. Но читатель здесь СВОЙ — вложенная точка
// предъявления (Ф3-52, F4d-28): обработчику нужны поля сессии (срок, уровень,
// подтверждённость адреса), которых полоса в запрос не кладёт, и
// свой вопрос об отсечке он задаёт сам — снимать его ради одного вызова
// запрещает гейт.
func (h *SessionIdentityHandler) WithHumanSession(r HumanSessionReader) *SessionIdentityHandler {
	h.humanSession = r
	return h
}

// WithAdminChecker — system-admin tuple lookup для /me.
// Возвращает permissions:["*","admin"] если subject имеет соответствующий tuple.
func (h *SessionIdentityHandler) WithAdminChecker(a AdminChecker) *SessionIdentityHandler {
	h.adminCheck = a
	return h
}

// Register крепит handler на http.ServeMux. Должен вызываться ДО общего `/`.
func (h *SessionIdentityHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/iam/v1/auth/me", h.Me)
}

// Me — UI hook /me. Возвращает либо `{"user":null}` если не залогинен,
// либо `{"user":{...}}` с userinfo из НАШЕЙ сессии.
func (h *SessionIdentityHandler) Me(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if h.humanSession != nil {
		h.meFromOwnSession(w, r)
		return
	}

	// ЗДЕСЬ ЧИТАЛОСЬ ПЕЧЕНЬЕ ЧУЖОЙ СЛУЖБЫ ЛИЧНОСТИ. Ветка снята вместе с самим
	// поставщиком; вместе с ней ушёл и ленивый завод зеркала субъекта по его
	// идентификатору — зеркало заводит наша служба, когда человек у неё входит.

	// Никакой второй ветки нет: единственный носитель личности на этом маршруте —
	// НАША сессия. Не нашли её — отвечаем анонимом.
	_, _ = w.Write([]byte(`{"user":null}`))
}

// meFromOwnSession — «кто я» из НАШЕЙ сессии (Ф3-14).
//
// Форма ответа прежняя, объект `session` добавлен: срок (усечён до секунды —
// показывается, не сравнивается), уровень и подтверждённость адреса.
// Без носителя и с печеньем поставщика без нашего — `{"user":null}` побайтово
// (Ф1-52): под `own` печенье поставщика носителем не является.
//
// «Сессии нет», недоступность и отсечка отвечают анонимом: через боевую
// цепочку сюда доходит только запрос, который полоса уже пропустила, и эти
// исходы здесь — гонка между двумя вопросами одного запроса, а не отказ (его
// произвела бы полоса). Fail-closed в ту же сторону, что прежде: анонимный
// ответ и отказ на пути запроса суть одно состояние.
func (h *SessionIdentityHandler) meFromOwnSession(w http.ResponseWriter, r *http.Request) {
	carrier, err := r.Cookie(OurSessionCarrierName)
	if err != nil || carrier.Value == "" {
		_, _ = w.Write([]byte(`{"user":null}`))
		return
	}
	sess, found, err := h.humanSession.ResolveHumanSession(r.Context(), carrier.Value)
	if err != nil {
		h.logger.Error("/me: human session lookup unanswered; answering anonymous", "err", err.Error())
		_, _ = w.Write([]byte(`{"user":null}`))
		return
	}
	if !found {
		_, _ = w.Write([]byte(`{"user":null}`))
		return
	}
	subj := Subject{Type: "user", ID: sess.UserID, DisplayName: sess.DisplayName}
	if h.sessionRevoked(r.Context(), subj, sess.AuthenticatedAt) {
		_, _ = w.Write([]byte(`{"user":null}`))
		return
	}
	userObj := map[string]any{
		"id":          subj.ID,
		"email":       sess.Email,
		"displayName": subj.DisplayName,
		"subjectType": subj.Type,
		"permissions": []string{},
	}
	// `permissions` — как сегодня (`system_admin` на кластере), по НАШЕМУ субъекту.
	if h.adminCheck != nil {
		ok, _ := h.adminCheck.IsSystemAdmin(r.Context(), subj.Type+":"+subj.ID)
		if ok {
			userObj["permissions"] = []string{"*", "admin"}
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"user": userObj,
		"session": map[string]any{
			"expiresAt":      sess.ExpiresAt.UTC().Truncate(time.Second).Format(time.RFC3339),
			"assuranceLevel": sess.AssuranceLevel,
			"emailVerified":  sess.EmailVerified,
		},
	})
}

// sessionRevoked — отвергнута ли эта сессия НАШЕЙ отсечкой.
//
// Fail-closed по обоим неопределённым исходам, и это то же решение, что на
// полосе личности: авторитет наш, молчит он тогда же, когда край и так
// отказывает по правам, а сессия без момента аутентификации при живой отсечке
// доказать своё непревышение не может. Обратное — мягкий проход — означало бы
// «отзываем и свой же отзыв не исполняем».
func (h *SessionIdentityHandler) sessionRevoked(
	ctx context.Context, subj Subject, authenticatedAt time.Time,
) bool {
	if h.sessionCutoff == nil || subj.Type != "user" || subj.ID == "" {
		return false
	}
	cutoff, found, err := h.sessionCutoff.SessionCutoffOf(ctx, subj.ID)
	if errors.Is(err, ErrSessionCutoffUnsupported) {
		// Окно раската — та же посадка, что на полосе личности: проходим, громко.
		h.logger.Error("/me: session revocation not enforced — the authority does not " +
			"offer this question (image skew)")
		return false
	}
	if err != nil {
		h.logger.Error("/me: session revocation check unanswered; answering anonymous",
			"err", err.Error())
		return true
	}
	if !found {
		return false
	}
	if authenticatedAt.IsZero() {
		return true
	}
	return !authenticatedAt.After(cutoff)
}
