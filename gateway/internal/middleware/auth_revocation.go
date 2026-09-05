// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// auth_revocation.go — asking the identity provider whether a presented token is
// still live, on the authN layer that always runs.
//
// Why it lives HERE, next to the signature check, and not with the
// sender-constrained-token machinery it used to sit inside:
//
// Revocation is a property of ANY presented token. Proof-of-possession is a
// property of tokens that were minted bound to a key. They are independent
// questions, and tying the first to the second meant the first was never asked:
// the binding machinery mounts behind a toggle that no profile sets, so the
// revocation check had a config guard, deploy wiring, tests — and no reachable
// code path on any stand. Turning that toggle on is not the fix, because it also
// starts DEMANDING proof-of-possession, and machine credentials are not issued
// bound yet (see AuthNRequireMachineTokenBinding: issuance first, enforcement
// second). Enabling it before issuance lands refuses every service account —
// an outage, not a hardening. So the check is untied instead.
//
// The signature is verified exactly once, by the caller, and the verified token
// is handed here: revocation must not pay for a second parse of the same bearer.
//
// Scope, stated plainly: this asks about BEARER credentials. A service→service
// caller authenticated by its client certificate presents no token at all, and a
// browser is authenticated on the provider's session cookie instead — that lane
// asks its OWN revocation question, in auth_session_cutoff.go.
//
// ЗДЕСЬ СТОЯЛО, ЧТО БРАУЗЕРНОЙ ПОЛОСЕ ОТДЕЛЬНЫЙ ВОПРОС НЕ НУЖЕН, — и это было
// неверно (#1122). Довод звучал: сессия перепроверяется у провайдера на каждом
// запросе. Про отзывы САМОГО провайдера он верен. Про запись, которую делает наш
// глагол выхода и административный принудительный выход, он неверен и не может
// быть верным: тот, у кого спрашивают про сессию, о нашей записи не знает by
// construction. Следствие было наблюдаемым — администратор получал успех, а
// человек продолжал работать в консоли.
//
// Урок, ради которого абзац не удалён, а переписан: комментарий, объясняющий
// ОТСУТСТВИЕ проверки, живёт дольше своего основания и читается как решение.
// Свойство, обязательное для одной полосы, проверяется СРАВНЕНИЕМ полос
// (session_lanes_agree_test.go), а не доводом в шапке.
package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"google.golang.org/grpc/codes"
)

// TokenRevocationChecker — port: does the identity provider still consider this
// token live? Implemented by *IntrospectionCache, which bounds both the cost (one
// round-trip per token per cache window) and the wait (a per-call budget).
//
// The three outcomes the caller must tell apart are carried by the error, not by
// its text: nil / ErrTokenInactive / ErrIntrospectionMisconfigured, with anything
// else meaning "the provider did not answer this time".
type TokenRevocationChecker interface {
	Introspect(ctx context.Context, jti, rawToken string) (IntrospectionResult, error)
}

// revocationVerdict — the five answers. Only two of them change what the caller
// does; the other three all mean "carry on", and they are kept apart because they
// mean different things to whoever reads the log: asked-and-fine, could-not-ask,
// and asked-but-nobody-answered are three different states of the same control.
type revocationVerdict int

const (
	// revocationNotAsked — no checker wired, an exempt path, or a token with no
	// identifier to ask about. The request proceeds unchecked.
	revocationNotAsked revocationVerdict = iota
	// revocationLive — the provider says the token is still good.
	revocationLive
	// revocationRevoked — the provider says it is not. Reject the credential.
	revocationRevoked
	// revocationUnanswerable — what answered is not an introspection endpoint.
	// Permanent until someone changes configuration; refuse to serve.
	revocationUnanswerable
	// revocationUnanswered — the provider did not answer this time. Passes on its
	// own, so the request continues and the process reports that it is not
	// currently enforcing.
	revocationUnanswered
)

// revocationDenyDescription — the client-visible reason on a revoked credential,
// on the REST surface. It names the caller's OWN token state and nothing about
// anyone else's, so it is not an oracle; it tells a client to re-authenticate
// rather than retry.
//
// The gRPC surface deliberately does NOT carry it: that path answers every
// authN failure with one constant message, so a caller cannot tell a revoked
// token from a bad signature or an unprovisioned subject (see authFailedMsg —
// varying the text there is the enumeration oracle it exists to prevent). A
// machine-readable reason for gRPC belongs in ErrorInfo.details, which is a
// contract change, not a message tweak.
const revocationDenyDescription = "token revoked"

// revocationUnavailableReason — what a caller is told when the check cannot
// answer. Deliberately thin: which of this deployment's addresses is wrong is
// the operator's business, and it goes to the log, not to the wire.
const revocationUnavailableReason = "revocation check unavailable"

// WithRevocationCheck mounts the revocation check on this interceptor, for both
// the REST and the gRPC surface. A nil checker leaves it unmounted (the dev-stand
// shape: no provider admin API to ask). reportInterval bounds how often a
// continuing failure is re-stated; zero takes the default.
//
// Production deployments cannot leave it unmounted by accident: the composition
// root refuses to start a production-class gateway whose introspection address is
// unset or aimed at the public API (cmd/api-gateway/revocation_validation.go).
func (a *AuthInterceptor) WithRevocationCheck(c TokenRevocationChecker, reportInterval time.Duration) *AuthInterceptor {
	if c == nil {
		return a
	}
	a.revocation = c
	// Two reporters, not one: "the provider is not answering" and "the token had
	// nothing to ask about" are different faults with different remedies and
	// different readers. Sharing a window would let a burst of one suppress the
	// first report of the other, and sharing a counter would produce a number that
	// answers neither question.
	a.revocationFailures = newIntrospectionFailureReporter(reportInterval, nil)
	a.revocationSkips = newIntrospectionFailureReporter(reportInterval, nil)
	return a
}

// WithPlatformRevocationCheck mounts the revocation reader for tokens OUR OWN
// issuer minted, on both surfaces.
//
// # Почему это ОТДЕЛЬНЫЙ читатель, а не тот же
//
// Полоса отзыва — свойство ЗАПИСИ издателя (token_acceptance.go), а не
// настройки процесса. Прежний провайдер о наших токенах не знает by
// construction: спрошенный про наш токен, он отвечает не «действует» и не
// «отозван», а утверждение о предмете, которого у него нет. Отзыв нашего токена
// знает только НАШ авторитет.
//
// # Почему на этой полосе «не ответил» означает ОТКАЗ
//
// Асимметрия с полосой прежнего издателя объявлена, а не выведена, и основание
// у неё — различие предмета, а не удобство:
//
//   - НАША полоса. Токен наш, отзыв наш, авторитет наш и живёт на том же
//     внутреннем слушателе, к которому край обращается на каждом запросе. Если
//     он молчит, край уже отказывает по правам. Мягкий проход означал бы:
//     чеканим, отзываем и СВОЙ ЖЕ отзыв не исполняем — ровно тот класс, где
//     контроль действует на выдаче и не действует на предъявлении;
//   - полоса ПРЕЖНЕГО издателя. Авторитет — третья сторона, её доступностью мы
//     не управляем. Мягкий проход там принят, задокументирован и несёт
//     собственный страж: «ответило не то» остаётся постоянной неисправностью и
//     даёт отказ. Менять его этой фазой значило бы завести новую поверхность
//     отказа, которой фаза не предусматривала.
//
// ПРЕДИКАТ СНЯТИЯ асимметрии: она уходит вместе с полосой прежнего издателя.
//
// A nil checker leaves it unmounted; в этом состоянии токен нашего издателя
// отвергается на предъявлении — объявленный контроль без читателя не отказал бы
// ни разу за свою жизнь, поэтому здесь он отказывает всегда.
func (a *AuthInterceptor) WithPlatformRevocationCheck(c TokenRevocationChecker, reportInterval time.Duration) *AuthInterceptor {
	if c == nil {
		return a
	}
	a.platformRevocation = c
	a.platformRevocationFailures = newIntrospectionFailureReporter(reportInterval, nil)
	return a
}

// revocationCheck asks about a verified token and reports the verdict, logging
// the two failure modes on the caller's behalf (rate-limited, with a running
// total) so both surfaces report identically.
//
// Полоса выбирается по ЗАПИСИ ИЗДАТЕЛЯ, которую проверяющий пометил на
// проверенном токене (`VerifiedToken.ReadRevocation`), — не по настройке
// процесса и не по адресу. Признак ставит тот же код, который выбрал запись для
// проверки подписи, поэтому разойтись им нечем.
//
// surface/route are log context only — they never change the verdict.
func (a *AuthInterceptor) revocationCheck(ctx context.Context, vt *VerifiedToken, surface, route string) revocationVerdict {
	if vt == nil {
		return revocationNotAsked
	}
	if vt.ReadRevocation {
		return a.platformRevocationCheck(ctx, vt, surface, route)
	}
	if a.revocation == nil {
		return revocationNotAsked
	}
	// A token with no identifier cannot be asked about: introspection is keyed on
	// the jti. Our provider mints JWT access tokens, which always carry one (the
	// profiles pin that strategy — see gateway/deploy/token_shape_test.go), and
	// the platform already depends on it end to end: sign-out revokes BY jti and
	// kaname refuses to refresh a token that has none. So this branch is not
	// reachable with a credential this deployment issued — but it is not silent
	// either, because "the control did not run" must never look like "the control
	// passed".
	if vt.JTI == "" {
		if report, total, represents := a.revocationSkips.observe(); report {
			a.logger.Error("revocation check skipped: token carries no identifier",
				"surface", surface, "route", route,
				"tokens_without_identifier_total", total,
				"occurrences_since_last_report", represents)
		}
		return revocationNotAsked
	}

	_, err := a.revocation.Introspect(ctx, vt.JTI, vt.Raw)
	switch {
	case err == nil:
		return revocationLive

	case errors.Is(err, ErrTokenInactive):
		return revocationRevoked

	case errors.Is(err, ErrIntrospectionMisconfigured):
		// What answered is not an introspection endpoint. That does not heal, so
		// continuing means every request from here on is served with the
		// revocation check silently absent. Refuse instead: the gateway cannot
		// establish that this token is still valid.
		if report, total, represents := a.revocationFailures.observe(); report {
			a.logger.Error("revocation check misconfigured; refusing requests",
				"err", err, "surface", surface, "route", route,
				"introspection_failures_total", total,
				"occurrences_since_last_report", represents,
				"hint", "KACHO_HYDRA_INTROSPECTION_URL must address the identity "+
					"provider's admin API path /admin/oauth2/introspect")
		}
		return revocationUnanswerable

	default:
		// The provider did not answer this time. That passes on its own, so the
		// request continues — and the process says so, because a control that
		// stops enforcing in silence is one nobody knows they lost.
		if report, total, represents := a.revocationFailures.observe(); report {
			a.logger.Error("revocation check unavailable; requests continue unchecked",
				"err", err, "surface", surface, "route", route,
				"introspection_failures_total", total,
				"occurrences_since_last_report", represents)
		}
		return revocationUnanswered
	}
}

// writeHTTPServiceUnavailable answers a request the gateway cannot serve because
// of its OWN configuration. No WWW-Authenticate header: this is not an
// authentication challenge, and offering one would invite the caller to
// re-authenticate against a fault no credential of theirs can clear.
//
// Поле `code` — код gRPC (`google.rpc.Status.code`), а НЕ номер HTTP-статуса:
// клиент ключуется машинно именно на него, и оба числа тут разные по смыслу.
// Здесь стоял `http.StatusServiceUnavailable`, то есть 503 в поле, где кода 503
// не существует вовсе, — вызывающий вместо «повтори позже» (14, UNAVAILABLE)
// получал величину вне словаря. Соседние писатели отказа края поле заполняют
// верно и служат образцом: 401 → 16 (`writeHTTPUnauthorized`), 403 → 7
// (`writeHTTPDeny`), отказ слоя прав при недоступном источнике вердикта → 14
// (`authz.go`, ветвь `outcomeError`) — то же число, что и здесь.
// Закреплено `TestRefusalBodyCarriesTheGRPCCodeNotTheHTTPStatus`.
func writeHTTPServiceUnavailable(w http.ResponseWriter, reason string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"code":    int(codes.Unavailable),
		"message": reason,
	})
}

// platformRevocationCheck спрашивает НАШ авторитет о НАШЕМ токене, и на каждой
// развилке выбирает отказ.
//
// Три отличия от полосы прежнего издателя, и все три намеренные:
//
//  1. читателя нет ⇒ ОТКАЗ. Запись объявила чтение отзыва, а читателя не
//     провязали — это контроль, который не отказал бы ни разу; здесь он
//     отказывает всегда, и композиционный корень отказывает в старте раньше;
//  2. идентификатора отзыва нет ⇒ ОТКАЗ. Производитель идентификатора на этой
//     полосе МЫ САМИ, поэтому его отсутствие означает не «нечего спросить», а
//     «мы выпустили то, что не умеем отозвать»;
//  3. авторитет не ответил ⇒ ОТКАЗ. «Не дозвонился» не есть «разрешено».
func (a *AuthInterceptor) platformRevocationCheck(ctx context.Context, vt *VerifiedToken, surface, route string) revocationVerdict {
	if a.platformRevocation == nil {
		a.logger.Error("revocation reader for our own issuer is not wired; refusing",
			"surface", surface, "route", route,
			"hint", "KACHO_API_GATEWAY_PLATFORM_TOKEN_REVOCATION_URL must address our revocation authority")
		return revocationUnanswerable
	}
	if vt.JTI == "" {
		a.logger.Error("our own token carries no identifier to revoke by; refusing",
			"surface", surface, "route", route)
		return revocationUnanswerable
	}

	_, err := a.platformRevocation.Introspect(ctx, vt.JTI, vt.Raw)
	switch {
	case err == nil:
		return revocationLive

	case errors.Is(err, ErrTokenInactive):
		return revocationRevoked

	case errors.Is(err, ErrIntrospectionMisconfigured):
		if report, total, represents := a.platformRevocationFailures.observe(); report {
			a.logger.Error("our revocation authority is misconfigured; refusing requests",
				"err", err, "surface", surface, "route", route,
				"platform_revocation_failures_total", total,
				"occurrences_since_last_report", represents,
				"hint", "KACHO_API_GATEWAY_PLATFORM_TOKEN_REVOCATION_URL must address our "+
					"revocation authority on the cluster-internal listener")
		}
		return revocationUnanswerable

	default:
		// Отличается от полосы прежнего издателя ровно здесь, и это решение, а
		// не недосмотр: недоступность НАШЕГО сервиса не есть разрешение
		// пользоваться токеном, который мы, возможно, уже отозвали.
		if report, total, represents := a.platformRevocationFailures.observe(); report {
			a.logger.Error("our revocation authority did not answer; refusing requests",
				"err", err, "surface", surface, "route", route,
				"platform_revocation_failures_total", total,
				"occurrences_since_last_report", represents)
		}
		return revocationUnanswerable
	}
}
