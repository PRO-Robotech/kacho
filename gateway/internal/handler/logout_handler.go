// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package handler — HTTP handlers owned by api-gateway directly (not proxied
// to a backend): the logout endpoint and the relay of the sign-in form's verbs.
package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	iamv1 "github.com/PRO-Robotech/kaname/pkg/api/kaname/cloud/iam/v1"

	"github.com/PRO-Robotech/kacho/gateway/internal/authnrefusal"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// SessionRevocationsClient — minimal port the logout handler needs to push
// revocations into kaname. Implemented by an adapter around the generated
// gRPC stub `InternalSessionRevocationsServiceClient.Revoke`. Declared here
// so the handler is unit-testable without spinning up a gRPC server.
//
// The adapter discards the Operation envelope returned by the gRPC stub —
// the logout handler does not poll for completion (Revoke writes
// session_revocations row in the same TX as Operation insert, so by the time
// Revoke returns, downstream pods receiving LISTEN/NOTIFY will already
// invalidate the token).
type SessionRevocationsClient interface {
	Revoke(ctx context.Context, in *iamv1.RevokeRequest) error
}

// VerifiedCaller — identity derived from a cryptographically validated access
// token. It is the ONLY trusted source of the logout subject: a caller may
// revoke exactly their own session(s), never a subject named in the request
// body. Client-supplied `subject`/`token_jti` form fields are ignored.
type VerifiedCaller struct {
	Subject string // token `sub` — the authenticated caller
	JTI     string // token `jti` — the specific session token being surrendered
}

// CallerVerifier — port that validates a presented access token (JWKS
// signature, issuer, audience, expiry) and returns the caller's identity.
// Implemented by an adapter over the gateway's JWKS verifier (the same
// instance used on the principal path). Declared here so the handler is
// unit-testable without a live JWKS endpoint.
//
// Ошибка проверяющего различает ДВА исхода, и обработчик их не склеивает:
// ошибка, для которой [middleware.KeySourceUnanswerable] истинна (набор
// проверочных ключей не добыт), значит «проверяющий не решил» — выход «не
// выполнен» (`503`); любая другая — «токен негоден», единый отказ края (`401`).
// Адаптер обязан пропускать ошибку проверяющего края обёрнутой, не подменяя её.
//
// nil verifier ⇒ проверять предъявителя нечем: выход «не выполнен» (`503`).
type CallerVerifier interface {
	Verify(ctx context.Context, token string) (*VerifiedCaller, error)
}

// LogoutHandler — POST /oauth/logout: выход ПУТИ ТОКЕНОВ. Гасит на сервере
// предъявленный токен доступа — пишет отзыв в НАШУ запись, ту, что полоса отзыва
// края читает на каждом предъявлении. Браузерную сессию этот путь не гасит и не
// делает вида, что гасит: её выход — `POST /iam/v1/auth/logout` полосы входа
// (служба; край ретранслирует), и печенье сессии здесь не гасится никогда.
//
// Исходы — ТЕ ЖЕ, что у выхода полосы входа (kaname `docs/content/api/auth-lane.mdx`,
// «Выход»; приёмка Ф3, Р4), на каждом предъявлении, которое путь принимает
// (`Authorization: Bearer|DPoP <token>`, форма `token` по RFC 7009 §2.1):
//
//   - выход выполнен — `200` `{}`; сюда же относится запрос без всякого
//     носителя (Ф3-18: различимый ответ сказал бы держателю чужой копии, жива ли она);
//   - выход не выполнен — `503` `{"code":14,"message":"logout not performed; try again later","details":[]}`:
//     отзыв не ответил в бюджете, источник проверочных ключей не ответил
//     (проверяющий не решил, годен ли токен), проверять предъявителя нечем либо
//     писать отзыв некуда. Удостоверение цело и по-прежнему годно, повторить можно. «Вышли»
//     при живом удостоверении — дефект, который Ф3 Р4 отвергла поимённо: прежде
//     здесь при неответившем отзыве стоял `200`, и повторить выход клиенту было
//     не с чего.
//
// Отказы — в форме `google.rpc.Status`:
//
//   - неверный метод — `405` `{"code":12,"message":"method not allowed","details":[]}`
//     с `Allow: POST` (решение R36 п. 3);
//   - предъявлено печенье НАШЕЙ сессии — `400` `{"code":9,"message":"this path ends access tokens; end the browser session with POST /iam/v1/auth/logout","details":[]}`.
//     Ничего не отзывается: путь, получивший носитель, которого он не гасит,
//     ответив «вышли», оставил бы сессию живой на сервере при погашенном у
//     клиента печенье (kacho#2959);
//   - предъявитель не принят проверяющим либо отзыв (`subject`, `token_jti`,
//     `revoke_all`) запрошен без предъявителя — единый отказ края `401` (приёмка
//     KA1, Р2; пакет `authnrefusal`).
//
// Субъект и jti берутся ТОЛЬКО из проверенного токена; поля `subject` и
// `token_jti` формы не читаются как цель — путь нельзя обратить против чужой
// сессии. `revoke_all=true` гасит все удостоверения вызывающего.
//
// Сессии у прежнего поставщика больше нет (#2734): обработчик говорит только с
// нашей службой доступа.
type LogoutHandler struct {
	logger      *slog.Logger
	verifier    CallerVerifier
	revocations SessionRevocationsClient
	callBudget  time.Duration
}

// LogoutHandlerConfig — DI bag.
type LogoutHandlerConfig struct {
	Logger      *slog.Logger
	Verifier    CallerVerifier           // проверяет токен вызывающего; nil ⇒ проверять нечем, выход «не выполнен» (503)
	Revocations SessionRevocationsClient // пишет отзыв в нашу запись; nil ⇒ писать некуда, выход «не выполнен» (503)
	// CallBudget — бюджет вызова отзыва при выходе: ручка
	// KACHO_API_GATEWAY_IDENTITY_CALL_BUDGET (приёмка KA1, Р4), та же, что у
	// прочих вопросов края службе доступа. Обязателен: отзыв, не ответивший в
	// бюджете, есть выход «не выполнен» (`503`, Ф3 Р4), и срок, через который
	// клиент это узнаёт, выбирает профиль, а не константа. Проверку предъявителя
	// он не ограничивает (Р4, редакция 9; KA1-37) — её срок несёт проверяющий.
	CallBudget time.Duration
}

// NewLogoutHandler constructs the handler. Logger is required (we never want
// silent failures on a security-critical path).
func NewLogoutHandler(cfg LogoutHandlerConfig) (*LogoutHandler, error) {
	if cfg.Logger == nil {
		return nil, errors.New("logout handler: logger is required")
	}
	if cfg.CallBudget <= 0 {
		return nil, errors.New("logout handler: CallBudget (KACHO_API_GATEWAY_IDENTITY_CALL_BUDGET) is required and must be positive")
	}
	return &LogoutHandler{
		logger:      cfg.Logger,
		verifier:    cfg.Verifier,
		revocations: cfg.Revocations,
		callBudget:  cfg.CallBudget,
	}, nil
}

// Тела ответов — дословно те, что названы в шапке [LogoutHandler]; проба
// `logout_outcome_test.go` сверяет шапку с ними.
const (
	logoutDoneBody          = `{}`
	logoutNotPerformedBody  = `{"code":14,"message":"logout not performed; try again later","details":[]}`
	logoutMethodBody        = `{"code":12,"message":"method not allowed","details":[]}`
	logoutSessionOnPathBody = `{"code":9,"message":"this path ends access tokens; end the browser session with POST /iam/v1/auth/logout","details":[]}`
)

// ServeHTTP implements net/http.Handler.
func (h *LogoutHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeBody(w, http.StatusMethodNotAllowed, logoutMethodBody)
		return
	}

	// Браузерная сессия — не носитель этого пути (шапка). Предикат присутствия —
	// тот же, что у полос, читающих сессию.
	if middleware.OurSessionCarrierPresented(r) {
		writeBody(w, http.StatusBadRequest, logoutSessionOnPathBody)
		return
	}

	rawToken := extractAccessToken(r)
	_ = r.ParseForm()
	if rawToken == "" {
		rawToken = strings.TrimSpace(r.Form.Get("token"))
	}
	revokeAll := r.Form.Get("revoke_all") == "true"
	// Цель отзыва из тела не читается никогда; поля названы лишь затем, чтобы
	// запрос, ПРОСЯЩИЙ отзыв без предъявителя, получил отказ, а не «вышли».
	revokeRequested := revokeAll || strings.TrimSpace(r.Form.Get("subject")) != "" ||
		strings.TrimSpace(r.Form.Get("token_jti")) != ""

	if rawToken == "" {
		if revokeRequested {
			h.logger.Warn("logout: revocation requested without a presented access token — refused")
			authnrefusal.WriteHTTP(w)
			return
		}
		// Носителя нет — гасить нечего (Ф3-18).
		writeBody(w, http.StatusOK, logoutDoneBody)
		return
	}

	if h.verifier == nil || h.revocations == nil {
		h.logger.Error("logout: not performed — the handler is assembled without a verifier or a revocation writer",
			"verifier", h.verifier != nil, "revocations", h.revocations != nil)
		writeBody(w, http.StatusServiceUnavailable, logoutNotPerformedBody)
		return
	}

	// Своего срока вокруг проверки предъявителя обработчик не ставит (приёмка
	// KA1, Р4, редакция 9; KA1-37): её единственный внешний вызов — чтение набора
	// ключей, и он ограничен тем же объявленным бюджетом, что на пути запроса
	// (`KACHO_JWKS_FETCH_TIMEOUT_SECONDS`, клиент чтения проверяющего). Срок
	// короче на одном пути дал бы два ответа на один вопрос: путь запроса
	// принимает токен, а выход отвечает «не выполнен» на каждой попытке, и
	// прерванное чтение набор не наполняет. Ручка Р4 ограничивает только отзыв.
	caller, verr := h.verifier.Verify(r.Context(), rawToken)
	if verr != nil && middleware.KeySourceUnanswerable(verr) {
		// Набор ключей не добыт: проверяющий НЕ РЕШИЛ, годен ли токен (#1194).
		// Ответ `401` велел бы выбросить годный токен, а на сервере он жив, —
		// поэтому «не выполнен, повторите». Громко: сюда попадает и неверная
		// настройка источника (`security.md` §Hardening инв. 8).
		h.logger.Error("logout: not performed — the token key source did not answer", "err", verr)
		writeBody(w, http.StatusServiceUnavailable, logoutNotPerformedBody)
		return
	}
	if verr != nil {
		h.logger.Warn("logout: access-token verification failed", "err", verr)
		// Единый отказ края (приёмка KA1, Р2): тот же, что у слоя
		// аутентификации на любой причине.
		authnrefusal.WriteHTTP(w)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), h.callBudget)
	defer cancel()
	req := &iamv1.RevokeRequest{
		TokenJti:            caller.JTI,
		UserId:              caller.Subject,
		Reason:              "user-logout",
		RevokeAllUserTokens: revokeAll,
		TtlExpiresAt:        timestamppb.New(time.Now().Add(30 * 24 * time.Hour)),
	}
	if err := h.revocations.Revoke(ctx, req); err != nil {
		// Причина — в журнале оператора, не в ответе: текст ошибки вызова
		// называет внутреннюю службу и её адрес (kacho#3029).
		h.logger.Warn("logout: not performed — the revocation write did not succeed", "err", err, "subject", caller.Subject)
		writeBody(w, http.StatusServiceUnavailable, logoutNotPerformedBody)
		return
	}
	writeBody(w, http.StatusOK, logoutDoneBody)
}

// extractAccessToken pulls the bearer/DPoP token from the Authorization header.
// Returns "" if absent.
func extractAccessToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return ""
	}
	for _, scheme := range []string{"Bearer ", "DPoP ", "bearer ", "dpop "} {
		if strings.HasPrefix(auth, scheme) {
			return strings.TrimSpace(auth[len(scheme):])
		}
	}
	return ""
}

// writeBody пишет тело дословно — без перевода строки кодировщика, чтобы ответ
// побайтово совпадал с названным в шапке.
func writeBody(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}
