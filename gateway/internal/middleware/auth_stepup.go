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
// (kaname authzguard.ACRFloor) decides on the `acr` this gateway forwards, and
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
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

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
	stepUpLaneBasic   = "basic"   // базовое удостоверение (однострочный секрет)
)

// enforceStepUpHTTP выносит вердикт о поле на REST-поверхности И РЕНДЕРИТ вызов
// RFC 9470. Возвращает true, когда запрос отвергнут и ответ уже написан.
//
// `as` — то, что предъявила ЭТА полоса; `lane` — которая именно. ПОЛ обязаны
// спрашивать ВСЕ полосы: он свойство ВСЯКОГО обращения, а не свойство того, чем
// его подписали. А вот СЮДА приходят не все — только те, кому вызов RFC 9470
// адресуем; остальные зовут `stepUpVerdictHTTP` и рендерят свой отказ сами
// (`tryBasicCredential`). Разделение вердикта и рендеринга введено #1215, и
// довод — в auth_basic_stepup.go.
//
// Здесь стояло «полос две, и обе обязаны сюда приходить». Полос стало три, и
// третья приходить сюда не должна — утверждение пережило свой предмет ровно за
// то время, пока две работы шли параллельно.
func (a *AuthInterceptor) enforceStepUpHTTP(w http.ResponseWriter, r *http.Request, as StepUpAssurance, lane string) bool {
	req, err := a.stepUpVerdictHTTP(r, as, lane)
	if err == nil {
		return false
	}
	if !stepUpCeremonyReachable(lane) {
		// ЗАДВИЖКА, А НЕ ВЕЖЛИВОСТЬ. Вызов RFC 9470 сообщает, что предъявленное
		// ГОДНО и лишь недостаточно сильно, — по глаголу, который вызывающему
		// недоступен. Полосе, у носителя которой церемонии повышения нет, такой
		// ответ запрещён: он подтверждает годность строки и советует
		// невозможное (разбор — auth_basic_stepup.go).
		//
		// Полоса, ведущая СВОЙ единый отказ, обязана отрендерить его сама — так
		// байт-идентичность держится построением (см. tryBasicCredential).
		// Сюда попадает лишь та, что этого не сделала: ей достаётся общий
		// отказ, ничего не подтверждающий. Умолчание в сторону выдачи вызова
		// сделало бы следующую полосу оракулом молча.
		writeHTTPUnauthorized(w, authFailedMsg)
		return true
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("WWW-Authenticate", BuildStepUpChallenge(req, as.ACR))
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"code":16,"message":"` + stepUpDenyMessage + `"}`))
	return true
}

// errStepUpNotMet — ВЕРДИКТ «пол не пройден», отделённый от его РЕНДЕРИНГА.
//
// Разделение введено #1215 и оно несущее. Вердикт обязан быть один на все
// полосы — иначе заводится второе ранжирование, второе освобождение и второе
// умолчание, что и сторожат пробы паритета вердикта. А вот ТЕКСТ отказа
// принадлежит полосе: полоса, для носителя которой церемонии повышения не
// существует, обязана отвечать СВОИМ единым отказом, иначе вызов RFC 9470
// подтверждает годность предъявленного (см. auth_basic_stepup.go, разбор
// оракула).
var errStepUpNotMet = errors.New("stepup: authentication floor not met")

// stepUpVerdictHTTP выносит вердикт о поле для REST-обращения и НИЧЕГО не пишет
// в ответ. Возвращает требование каталога (нужно рендерящему) и ошибку.
//
// The pre-auth allow-list is exempt for the same reason the revocation check
// exempts it: those endpoints act on nobody's authority, and a caller who cannot
// re-authenticate must still be able to complete a sign-out.
func (a *AuthInterceptor) stepUpVerdictHTTP(r *http.Request, as StepUpAssurance, lane string) (PermissionRequirement, error) {
	if !a.StepUpMounted() || isPublicHTTPPath(r.URL.Path) {
		return PermissionRequirement{}, nil
	}
	req := a.stepUpRequirementForHTTP(r)
	if err := a.stepUp.CheckAssurance(as, req); err != nil {
		a.logger.Info("auth: authentication floor not met",
			"lane", lane, "path", r.URL.Path, "presented_acr", as.ACR, "required", req.RequiredACRMin)
		return req, errStepUpNotMet
	}
	return req, nil
}

// enforceStepUpGRPC applies the same floor on the native gRPC arm, where the
// method is named by the transport and needs no route resolution — для полосы
// ПОДПИСАННОГО ПРЕДЪЯВИТЕЛЯ.
//
// Сессии развёрнутого провайдера на этом транспорте нет by construction (cookie
// сюда не приходит), а вот полоса базового удостоверения есть: она зовёт
// `stepUpVerdictGRPC` напрямую и рендерит свой отказ сама (`authorize`). Здесь
// стояло «полоса всегда одна» — верно было до #1142 и перестало быть верным,
// не покраснев.
func (a *AuthInterceptor) enforceStepUpGRPC(fullMethod string, vt *VerifiedToken) error {
	if !a.StepUpMounted() {
		return nil
	}
	if vt == nil {
		// Пустой предъявитель — не «пол не применим», а отсутствующий вход
		// решения. Общая форма ниже отвергнет его пустым уровнем; собирать
		// здесь второе умолчание нельзя.
		return a.enforceStepUpGRPCAssurance(fullMethod, StepUpAssurance{}, stepUpLaneBearer)
	}
	return a.enforceStepUpGRPCAssurance(fullMethod, assuranceFromVerifiedToken(vt), stepUpLaneBearer)
}

// enforceStepUpGRPCAssurance — та же полоса решения для транспорта, у которого
// носитель личности НЕ подписанный токен (базовое удостоверение).
//
// Существует, чтобы у нативной поверхности была ОДНА точка применения пола на
// все полосы: второй вызов `CheckAssurance` из другого места завёл бы второй
// текст отказа и второе место, где можно забыть про полосу, — ровно тот
// механизм, который и породил #1215.
func (a *AuthInterceptor) enforceStepUpGRPCAssurance(fullMethod string, as StepUpAssurance, lane string) error {
	if err := a.stepUpVerdictGRPC(fullMethod, as, lane); err != nil {
		return status.Error(codes.Unauthenticated, stepUpDenyMessage)
	}
	return nil
}

// stepUpVerdictGRPC — тот же вердикт для нативной поверхности, без рендеринга.
func (a *AuthInterceptor) stepUpVerdictGRPC(fullMethod string, as StepUpAssurance, lane string) error {
	if !a.StepUpMounted() {
		return nil
	}
	req := a.stepUpRequirement(fullMethod)
	if err := a.stepUp.CheckAssurance(as, req); err != nil {
		a.logger.Info("auth: authentication floor not met",
			"lane", lane, "method", fullMethod, "presented_acr", as.ACR, "required", req.RequiredACRMin)
		return errStepUpNotMet
	}
	return nil
}

// stepUpDenyMessage — the refusal a caller sees. It names the outcome (a stronger
// authentication is required) and nothing about the object: WHICH ceremony to run
// travels in the RFC 9470 challenge, which carries no information about the
// resource either.
const stepUpDenyMessage = "insufficient_user_authentication"

// setTokenContextHeaders writes the credential's own context — acr, jti, scope,
// exp — and the two arguments the rights model's freshness condition asks of the
// caller: the methods the presenter authenticated with and the instant they did.
//
// It describes the CREDENTIAL, never who anyone is, which is why it travels
// independently of how the principal was resolved. The cluster-internal floor
// decides on the acr it finds here; this is the single producer, and it now sits
// on a layer that runs.
//
// ДОВОДЫ УСЛОВИЯ ЕДУТ БЕЗ МОСТОВОЙ ФОРМЫ, и это решение (см. principalmeta):
// их читает страж прав ЭТОГО ЖЕ края, восстанавливая удостоверение из
// заголовков, а за краем их не читает никто.
//
// Возвращает способы, которые край передать не смог: молча выброшенное значение
// поставщика — «принято-и-проигнорировано», и вызывающий обязан о нём доложить.
func setTokenContextHeaders(r *http.Request, t *VerifiedToken) (unusableMethods []string) {
	if t == nil {
		return nil
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
	amr, unusable := principalmeta.EncodeAuthMethods(t.AMR)
	if amr != "" {
		r.Header.Set(principalmeta.HeaderTokenAMR, amr)
	}
	// Момент подтверждения берётся ОТТУДА ЖЕ, откуда его читает сборка доводов
	// (`kacho_mfa_at`), а не из соседнего утверждения о времени входа: два
	// источника одной величины разошлись бы молча, и разошлись бы они на
	// удостоверении, где значения не совпадают.
	if at, ok := coerceUnixSeconds(t.ExtClaims["kacho_mfa_at"]); ok && at > 0 {
		r.Header.Set(principalmeta.HeaderTokenMfaAt, strconv.FormatInt(at, 10))
	}
	return unusable
}

// setSessionAssuranceHeaders writes the SESSION lane's contribution to the same
// token-context family: the assurance level, the methods that produced it and
// the instant it was produced.
//
// Почему не всё семейство. Заголовки этого семейства описывают ПРЕДЪЯВЛЕННОЕ, и
// у браузерной сессии нет ни `jti`, ни `scope`, ни `exp` — писать их пустыми
// значило бы утверждать про сессию то, чего про неё не знают. Уровень же
// внутреннему замку (`authzguard.ACRFloor`) нужен, и до #1201 эта полоса не
// выставляла его никогда: замок читал отсутствующее значение на каждом
// браузерном обращении. Способ и момент нужны условию модели прав — и до #1252
// их не выставлял никто.
//
// Подделать заголовок клиент не может: `stripForgeableIdentityHeaders` сносит
// весь namespace `x-kacho-` в обеих поверхностных формах ДО того, как выберется
// полоса.
//
// Нераспознанный уровень приезжает сюда пустой строкой — и это верно: пустое
// ранжируется нулём и не удовлетворяет ни одному положительному полу, то есть
// второй замок отказывает по той же причине, по которой отказал бы первый.
func setSessionAssuranceHeaders(r *http.Request, as StepUpAssurance, methods []string) (unusableMethods []string) {
	r.Header.Set(principalmeta.HeaderTokenACR, as.ACR)
	r.Header.Set(principalmeta.HeaderGRPCMetaTokenACR, as.ACR)

	// ДОВОДЫ УСЛОВИЯ. Ступени уверенности условию `mfa_fresh` мало: оно
	// спрашивает ещё и ВИД способа, и свежесть подтверждения. До #1252 браузерная
	// полоса не давала ни того, ни другого — условие было объявлено и
	// неисполнимо при любом входе.
	amr, unusable := principalmeta.EncodeAuthMethods(methods)
	if amr != "" {
		r.Header.Set(principalmeta.HeaderTokenAMR, amr)
	}
	// Момент подтверждения у браузерной сессии — тот же, что читает полоса
	// отзыва и арм свежести: момент, в который эта сессия аутентифицировалась.
	// Нулевой не ставится вовсе: «провайдер момента не назвал» обязано остаться
	// отсутствием довода, а не подтверждением в 1970 году.
	if !as.AuthTime.IsZero() {
		r.Header.Set(principalmeta.HeaderTokenMfaAt,
			strconv.FormatInt(as.AuthTime.UTC().Truncate(time.Second).Unix(), 10))
	}
	return unusable
}

// withTokenContextMetadata carries the same context onto the native gRPC arm,
// where there is no request to stamp. Written to BOTH directions for the reason
// injectPrincipal states: the proxy hop rebuilds outgoing from incoming, while a
// native handler forwards its own outgoing context.
func withTokenContextMetadata(ctx context.Context, vt *VerifiedToken) (context.Context, []string) {
	if vt == nil {
		return ctx, nil
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
	// Доводы условия кладутся ТОЛЬКО во входящие: их читает страж прав этого же
	// края, а за краем их не читает никто (см. principalmeta — мостовой формы у
	// них нет по той же причине). Та же посадка, что у уровня базового
	// удостоверения (`withBasicCredentialLevel`).
	amr, unusable := principalmeta.EncodeAuthMethods(vt.AMR)
	if amr != "" {
		inMD.Set(principalmeta.MetaTokenAMR, amr)
	}
	if at, ok := coerceUnixSeconds(vt.ExtClaims["kacho_mfa_at"]); ok && at > 0 {
		inMD.Set(principalmeta.MetaTokenMfaAt, strconv.FormatInt(at, 10))
	}
	return metadata.NewOutgoingContext(metadata.NewIncomingContext(ctx, inMD), outMD), unusable
}

// reportUnusableAuthMethods докладывает о способах подтверждения, которые край
// НЕ СМОГ передать дальше.
//
// Почему громко. Способ, отброшенный формой провода, — это НАСТРОЙКА поставщика
// личности, а не заминка: он не исчезает сам и повторяется на каждом запросе.
// Молчание здесь дало бы условие модели прав, которое не выполняется по
// причине, не записанной нигде, — и разбирались бы с ним по чужим отказам.
//
// Почему с нарастающим итогом, а не строкой на запрос. Состояние постоянное;
// строка на каждый запрос вытеснила бы из журнала всё остальное, а одна строка
// в начале ничего не сказала бы о длительности.
//
// Отброшенные значения печатаются как есть: они пришли от нашего же поставщика
// и не являются ни секретом, ни персональными данными, а без них диагноз
// «почему условие не выполняется» пришлось бы добывать заново.
func reportUnusableAuthMethods(
	lg *slog.Logger, rep *introspectionFailureReporter, lane, route string, dropped []string,
) {
	if len(dropped) == 0 || lg == nil || rep == nil {
		return
	}
	if report, total, represents := rep.observe(); report {
		lg.Error("authentication method(s) could not be carried to the rights model; "+
			"a condition asking for the method will not be satisfied on this lane",
			"lane", lane,
			"route", route,
			"dropped", dropped,
			"unusable_auth_methods_total", total,
			"occurrences_since_last_report", represents,
			"predicate", "исчезает, когда словарь поставщика и форма провода сойдутся")
	}
}
