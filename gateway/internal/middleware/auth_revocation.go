// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// auth_revocation.go — asking whether a presented token is still live, on the
// authN layer that always runs.
//
// Why it lives HERE, next to the signature check, and not with the
// sender-constrained-token machinery it used to sit inside:
//
// Revocation is a property of ANY presented token. Proof-of-possession is a
// property of tokens that were minted bound to a key. They are independent
// questions, and tying the first to the second meant the first was never asked:
// the binding machinery mounts behind a toggle that no profile sets, so the
// revocation check had a config guard, deploy wiring, tests — and no reachable
// code path on any stand. So the check is untied instead.
//
// The signature is verified exactly once, by the caller, and the verified token
// is handed here: revocation must not pay for a second parse of the same bearer.
//
// # Two lanes, one authority, one semantics
//
// The lane is chosen by the ISSUER RECORD the verifier marked on the token
// (`VerifiedToken.ReadRevocation`): a token of our own minting is asked of our
// revocation authority; a token of any other accepted record is asked of our
// revocation RECORD, by its identifier — the record the sign-out writes. Both
// answer about something WE revoked, and on both an answer that is not «live»
// refuses the request.
//
// The second lane used to ask the previous identity provider instead, and it
// carried a documented soft pass: «the provider did not answer» let the request
// through, because a third party's availability is not ours to control. The
// provider is retired (#2734), and nobody is left to ask on that lane but us: a
// soft pass there would mean «we revoke and do not enforce our own revocation»
// — a control that holds at issuance and not at presentation.
//
// Scope, stated plainly: this asks about BEARER credentials. A service→service
// caller authenticated by its client certificate presents no token at all, and a
// browser is authenticated on our session cookie instead — that lane asks its
// OWN revocation question, in auth_session_cutoff.go.
//
// Урок, ради которого абзац о браузерной полосе не удалён, а переписан:
// комментарий, объясняющий ОТСУТСТВИЕ проверки, живёт дольше своего основания и
// читается как решение. Свойство, обязательное для одной полосы, проверяется
// СРАВНЕНИЕМ полос (session_lanes_agree_test.go), а не доводом в шапке.
package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"google.golang.org/grpc/codes"
)

// TokenRevocationChecker — port: is this token still live? Implemented by
// *IntrospectionCache over our revocation authority (which bounds both the cost —
// one round-trip per token per cache window — and the wait) and by
// *OwnRevocationSource over our revocation record.
//
// The outcomes the caller must tell apart are carried by the error, not by its
// text: nil / ErrTokenInactive / ErrIntrospectionMisconfigured, with anything
// else meaning "the source did not answer this time".
type TokenRevocationChecker interface {
	Introspect(ctx context.Context, jti, rawToken string) (IntrospectionResult, error)
}

// revocationVerdict — the four answers. Two of them change what the caller does;
// the other two mean "carry on", and they are kept apart because they mean
// different things to whoever reads the log: asked-and-fine and nothing-to-ask
// are two different states of the same control.
type revocationVerdict int

const (
	// revocationNotAsked — no record reader wired: the in-process fixture shape.
	// The composition root always wires it (the identity service's internal
	// listener is critical: without it the edge serves nothing at all).
	revocationNotAsked revocationVerdict = iota
	// revocationLive — the source says the token is still good.
	revocationLive
	// revocationRevoked — the source says it is not. Reject the credential.
	revocationRevoked
	// revocationUnanswerable — the question could not be answered: the source did
	// not answer, answered with something that is not an answer, or the token
	// carries nothing to ask about. Refuse to serve: «could not establish» is not
	// «live».
	revocationUnanswerable
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

// revocationSourceAuthority / revocationSourceRecord — which of OUR two sources
// gave the answer, as the refusal log names it. The lane is chosen by the issuer
// record the verifier marked on the token (revocationSourceOf), and the log names
// the source by that same mark.
const (
	revocationSourceAuthority = "our revocation authority"
	revocationSourceRecord    = "our revocation record"
)

// revocationSourceOf — which of our sources revocationCheck asks about vt, by the
// same mark it chooses the lane by.
func revocationSourceOf(vt *VerifiedToken) string {
	if vt != nil && vt.ReadRevocation {
		return revocationSourceAuthority
	}
	return revocationSourceRecord
}

// revocationUnavailableReason — what a caller is told when the check cannot
// answer. Deliberately thin: which of this deployment's addresses is wrong is
// the operator's business, and it goes to the log, not to the wire.
const revocationUnavailableReason = "revocation check unavailable"

// WithRevocationCheck mounts the RECORD lane of the revocation check — the
// question asked about a token of any accepted record our own minting did not
// mark — on both the REST and the gRPC surface. A nil checker leaves it
// unmounted (the in-process fixture shape). reportInterval bounds how often a
// continuing failure is re-stated; zero takes the default.
//
// The composition root mounts it unconditionally, over our revocation record on
// the identity service's internal listener (OwnRevocationSource).
func (a *AuthInterceptor) WithRevocationCheck(c TokenRevocationChecker, reportInterval time.Duration) *AuthInterceptor {
	if c == nil {
		return a
	}
	a.revocation = c
	// Two reporters, not one: "the source is not answering" and "the token had
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
// настройки процесса. Отзыв токена нашей чеканки знает наш авторитет, и
// спрашивается он по своему протоколу, со своим якорем доверия и своим окном;
// запись отзыва по идентификатору — другой источник с другим транспортом.
//
// # «Не ответил» означает ОТКАЗ — на обеих полосах
//
// Токен наш, отзыв наш, авторитет наш и живёт на том же внутреннем слушателе, к
// которому край обращается на каждом запросе. Мягкий проход означал бы:
// чеканим, отзываем и СВОЙ ЖЕ отзыв не исполняем — ровно тот класс, где
// контроль действует на выдаче и не действует на предъявлении. Асимметрия с
// полосой прежнего издателя, у которой мягкий проход был, снята вместе с ней
// (#2734): её предикат снятия исполнился.
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
	// A token with no identifier cannot be asked about: our record is keyed on the
	// jti — sign-out revokes BY jti, and the identity service refuses to refresh a
	// token that has none. A token this edge cannot ask about is refused rather
	// than waved through: «the control did not run» must never look like «the
	// control passed».
	if vt.JTI == "" {
		if report, total, represents := a.revocationSkips.observe(); report {
			a.logger.Error("revocation check impossible: token carries no identifier; refusing",
				"surface", surface, "route", route,
				"tokens_without_identifier_total", total,
				"occurrences_since_last_report", represents)
		}
		return revocationUnanswerable
	}

	_, err := a.revocation.Introspect(ctx, vt.JTI, vt.Raw)
	switch {
	case err == nil:
		return revocationLive

	case errors.Is(err, ErrTokenInactive):
		return revocationRevoked

	case errors.Is(err, ErrIntrospectionMisconfigured):
		// Проверка собрана неполно. Это не лечится повтором, и продолжить значило
		// бы обслуживать каждый следующий запрос с молча отсутствующей проверкой
		// отзыва. Подсказка называет ЖИВУЮ причину: читатель на этом пути один
		// (`OwnRevocationSource`), и этот признак он ставит ровно в одном случае —
		// его собрали без источника.
		if report, total, represents := a.revocationFailures.observe(); report {
			a.logger.Error("revocation check misconfigured; refusing requests",
				"err", err, "surface", surface, "route", route,
				"revocation_failures_total", total,
				"occurrences_since_last_report", represents,
				"hint", "the revocation reader was assembled without a source: the "+
					"composition root must build it over the identity service's internal listener")
		}
		return revocationUnanswerable

	default:
		// Источник не ответил. Недоступность НАШЕЙ записи не есть разрешение
		// пользоваться токеном, который мы, возможно, уже отозвали.
		if report, total, represents := a.revocationFailures.observe(); report {
			a.logger.Error("our revocation record did not answer; refusing requests",
				"err", err, "surface", surface, "route", route,
				"revocation_failures_total", total,
				"occurrences_since_last_report", represents)
		}
		return revocationUnanswerable
	}
}

// writeHTTPServiceUnavailable answers a request the gateway cannot serve because a
// check it must run could not be answered — the token key set was not fetched,
// or the revocation question went unanswered (see revocationUnanswerable). No
// WWW-Authenticate header: this is not an authentication challenge, and the
// refusal is not a verdict on the credential's signature or lifetime.
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
// Три развилки, и на каждой — отказ:
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
		// Недоступность НАШЕГО сервиса не есть разрешение пользоваться токеном,
		// который мы, возможно, уже отозвали.
		if report, total, represents := a.platformRevocationFailures.observe(); report {
			a.logger.Error("our revocation authority did not answer; refusing requests",
				"err", err, "surface", surface, "route", route,
				"platform_revocation_failures_total", total,
				"occurrences_since_last_report", represents)
		}
		return revocationUnanswerable
	}
}
