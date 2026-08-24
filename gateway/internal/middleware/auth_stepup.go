// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware

// auth_stepup.go — the per-RPC authentication floor, applied on the authN layer
// every request passes through.
//
// WHY HERE. The floor used to be applied only inside the sender-constrained-token
// middleware, which mounts behind a feature toggle. A toggle is the right home for
// verifying a proof of possession: that is a property of SOME tokens, and demanding
// it before issuance mints bound credentials would refuse every machine. Whether
// the caller authenticated strongly enough for THIS call is a property of EVERY
// token, so it belongs on the layer that always runs — the same reasoning that
// moved the revocation check here (see auth_revocation.go).
//
// The token context below travels for the same reason. The cluster-internal arm
// (kacho-iam authzguard.ACRFloor) decides on the `acr` this gateway forwards, and
// its only producer sat in that unmounted middleware — so the internal floor read
// an absent value on every request for its whole life. A control whose input has
// no producer that runs is not a strict control; it is an unread one.
//
// THE RULE ITSELF IS NOT HERE. StepUpGate.Check renders grpcsrv.EvaluateStepUp,
// which the internal arm calls too. This file resolves WHICH requirement applies
// to the call in hand and renders the refusal for each transport; it must never
// re-derive a ranking, an exemption or a default — that divergence is what the
// verdict-parity guards exist to catch.

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
)

// WithStepUp mounts the per-RPC authentication floor on this interceptor.
//
// All three collaborators are required for the arm to mean anything: the gate
// evaluates, the lookup says what the catalog demands of a method, and the router
// turns a REST (method, path) into the method name the catalog is keyed by.
// Passing nil for any of them leaves the arm unmounted, which the composition
// root's production guard refuses to start on — silently applying no floor while
// the catalog declares one is the state this whole file exists to end.
func (a *AuthInterceptor) WithStepUp(gate *StepUpGate, lookup PermissionLookup, routes RestRouteResolver) *AuthInterceptor {
	if gate == nil || lookup == nil || routes == nil {
		return a
	}
	a.stepUp = gate
	a.stepUpLookup = lookup
	a.stepUpRoutes = routes
	return a
}

// StepUpMounted reports whether the floor is actually applied by this
// interceptor. The composition root asks before deciding whether a production
// stand may start.
func (a *AuthInterceptor) StepUpMounted() bool {
	return a.stepUp != nil && a.stepUpLookup != nil && a.stepUpRoutes != nil
}

// stepUpRequirement resolves what the catalog demands of a gRPC full-method.
// An unmounted arm and an unknown method both yield the no-op requirement — the
// arm never fabricates a demand for a call the catalog does not name.
func (a *AuthInterceptor) stepUpRequirement(fullMethod string) PermissionRequirement {
	if !a.StepUpMounted() {
		return PermissionRequirement{}
	}
	return a.stepUpLookup.Lookup(strings.TrimPrefix(fullMethod, "/"))
}

// stepUpRequirementForHTTP resolves the requirement for a REST call. An
// unresolvable route yields the empty method name, which the lookup treats as no
// requirement: the authz layer is what refuses a route the catalog does not name,
// and inventing a floor here would deny on routing, not on assurance.
func (a *AuthInterceptor) stepUpRequirementForHTTP(r *http.Request) PermissionRequirement {
	if !a.StepUpMounted() {
		return PermissionRequirement{}
	}
	fqn, ok := a.stepUpRoutes.Resolve(r.Method, r.URL.Path)
	if !ok {
		return PermissionRequirement{}
	}
	return a.stepUpLookup.Lookup(fqn)
}

// Имена полос носителя личности. Они попадают в КАЖДУЮ строку отказа, и это не
// украшение: дефект #1201 был найден именно тем, что журнал не отвечал на
// вопрос «какая полоса отказала». За всю жизнь процесса личность вносилась
// полосой сессии 5252 раза, полосой предъявителя 0 раз, а отказов по полу было
// 0 — и это выглядело исправной работой, потому что перепись по полосам никем
// не велась.
const (
	stepUpLaneBearer  = "bearer"  // подписанный предъявитель (Authorization)
	stepUpLaneSession = "session" // сессия развёрнутого провайдера (cookie)
)

// enforceStepUpHTTP applies the floor on the REST arm. It reports true when the
// request was refused and the response already written.
//
// `as` — то, что предъявила ЭТА полоса; `lane` — которая именно. Полос две, и
// обе обязаны сюда приходить: пол — свойство ВСЯКОГО обращения человека, а не
// свойство того, чем он его подписал.
//
// The pre-auth allow-list is exempt for the same reason the revocation check
// exempts it: those endpoints act on nobody's authority, and a caller who cannot
// re-authenticate must still be able to complete a sign-out.
func (a *AuthInterceptor) enforceStepUpHTTP(w http.ResponseWriter, r *http.Request, as StepUpAssurance, lane string) bool {
	if !a.StepUpMounted() || isPublicHTTPPath(r.URL.Path) {
		return false
	}
	req := a.stepUpRequirementForHTTP(r)
	if err := a.stepUp.CheckAssurance(as, req); err != nil {
		a.logger.Info("auth: authentication floor not met",
			"lane", lane, "path", r.URL.Path, "presented_acr", as.ACR, "required", req.RequiredACRMin)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("WWW-Authenticate", BuildStepUpChallenge(req, as.ACR))
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":16,"message":"` + stepUpDenyMessage + `"}`))
		return true
	}
	return false
}

// enforceStepUpGRPC applies the same floor on the native gRPC arm, where the
// method is named by the transport and needs no route resolution.
//
// Полоса здесь всегда одна: cookie на этот транспорт не приходит, у него нет
// сессии развёрнутого провайдера by construction.
func (a *AuthInterceptor) enforceStepUpGRPC(fullMethod string, vt *VerifiedToken) error {
	if !a.StepUpMounted() {
		return nil
	}
	req := a.stepUpRequirement(fullMethod)
	if err := a.stepUp.Check(vt, req); err != nil {
		a.logger.Info("auth: authentication floor not met",
			"lane", stepUpLaneBearer, "method", fullMethod, "presented_acr", vt.ACR, "required", req.RequiredACRMin)
		return status.Error(codes.Unauthenticated, stepUpDenyMessage)
	}
	return nil
}

// stepUpDenyMessage — the refusal a caller sees. It names the outcome (a stronger
// authentication is required) and nothing about the object: WHICH ceremony to run
// travels in the RFC 9470 challenge, which carries no information about the
// resource either.
const stepUpDenyMessage = "insufficient_user_authentication"

// setTokenContextHeaders writes the credential's own context — acr, jti, scope,
// exp — onto a REST request.
//
// It describes the CREDENTIAL, never who anyone is, which is why it travels
// independently of how the principal was resolved. The cluster-internal floor
// decides on the acr it finds here; this is the single producer, and it now sits
// on a layer that runs.
func setTokenContextHeaders(r *http.Request, t *VerifiedToken) {
	if t == nil {
		return
	}
	r.Header.Set(principalmeta.HeaderTokenACR, t.ACR)
	r.Header.Set(principalmeta.HeaderTokenJti, t.JTI)
	r.Header.Set(principalmeta.HeaderTokenScope, t.Scope)
	r.Header.Set(principalmeta.HeaderGRPCMetaTokenACR, t.ACR)
	r.Header.Set(principalmeta.HeaderGRPCMetaTokenJti, t.JTI)
	r.Header.Set(principalmeta.HeaderGRPCMetaTokenScope, t.Scope)
	if !t.ExpiresAt.IsZero() {
		r.Header.Set(principalmeta.HeaderTokenExp, strconv.FormatInt(t.ExpiresAt.Unix(), 10))
	}
}

// setSessionAssuranceHeaders writes the SESSION lane's contribution to the same
// token-context family: the assurance level, and only it.
//
// Почему только уровень. Заголовки этого семейства описывают ПРЕДЪЯВЛЕННОЕ, и у
// браузерной сессии нет ни `jti`, ни `scope`, ни `exp` — писать их пустыми
// значило бы утверждать про сессию то, чего про неё не знают. Уровень же
// внутреннему замку (`authzguard.ACRFloor`) нужен, и до #1201 эта полоса не
// выставляла его никогда: замок читал отсутствующее значение на каждом
// браузерном обращении.
//
// Подделать заголовок клиент не может: `stripForgeableIdentityHeaders` сносит
// весь namespace `x-kacho-` в обеих поверхностных формах ДО того, как выберется
// полоса.
//
// Нераспознанный уровень приезжает сюда пустой строкой — и это верно: пустое
// ранжируется нулём и не удовлетворяет ни одному положительному полу, то есть
// второй замок отказывает по той же причине, по которой отказал бы первый.
func setSessionAssuranceHeaders(r *http.Request, acr string) {
	r.Header.Set(principalmeta.HeaderTokenACR, acr)
	r.Header.Set(principalmeta.HeaderGRPCMetaTokenACR, acr)
}

// withTokenContextMetadata carries the same context onto the native gRPC arm,
// where there is no request to stamp. Written to BOTH directions for the reason
// injectPrincipal states: the proxy hop rebuilds outgoing from incoming, while a
// native handler forwards its own outgoing context.
func withTokenContextMetadata(ctx context.Context, vt *VerifiedToken) context.Context {
	if vt == nil {
		return ctx
	}
	pairs := [][2]string{
		{principalmeta.MetaTokenACR, vt.ACR},
		{principalmeta.MetaTokenJti, vt.JTI},
		{principalmeta.MetaTokenScope, vt.Scope},
	}
	inMD, _ := metadata.FromIncomingContext(ctx)
	if inMD == nil {
		inMD = metadata.MD{}
	} else {
		inMD = inMD.Copy()
	}
	outMD, _ := metadata.FromOutgoingContext(ctx)
	if outMD == nil {
		outMD = metadata.MD{}
	} else {
		outMD = outMD.Copy()
	}
	for _, p := range pairs {
		inMD.Set(p[0], p[1])
		outMD.Set(p[0], p[1])
	}
	return metadata.NewOutgoingContext(metadata.NewIncomingContext(ctx, inMD), outMD)
}
