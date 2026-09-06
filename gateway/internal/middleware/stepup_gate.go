// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// stepup_gate.go — RFC 9470 (OAuth 2.0 Step-up Authentication Challenge) +
// OpenID Connect Core 1.0 §3.1.2.1 ACR enforcement, PUBLIC front-door arm.
//
// Given a permission catalog (`<rpc-fqn> → required_acr_min` + optional
// `mfa_max_age`), the gate decides whether to allow a verified token through.
// On failure it emits an RFC 6750-style challenge that signals the UI to run
// a re-authentication ceremony with elevated `acr_values`.
//
// THE RULE ITSELF IS NOT HERE. This file extracts the inputs from a verified
// JWT (claim plumbing) and renders the verdict as an RFC 6750 challenge
// (protocol plumbing); the DECISION — the ACR ranking, the machine-principal
// exemption and the MFA-freshness window — lives once in
// grpcsrv.EvaluateStepUp, which the cluster-internal arm (kaname
// authzguard.ACRFloor) calls too. This gate must never re-derive any arm of it:
// a local ranking table or a local exemption is exactly the divergence that let
// a machine principal pass the front door and then be denied forever inside.
// stepup_verdict_parity_test.go drives THIS entrypoint — machine token included
// — against the shared rule.
//
// ACR ordering (grpcsrv.ACRRank):
//
//	"0" (anonymous) < "1" (password-only) < "2" (Passkey/UV-preferred) <
//	"3" (Passkey/UV-required, hardware-bound)
//
// `mfa_max_age` enforces a sliding freshness window on `auth_time` — a token
// older than the window is rejected even if its `acr` is high enough,
// forcing a re-auth.
package middleware

import (
	"errors"
	"fmt"
	"time"

	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
)

// Sentinel errors — mapped to WWW-Authenticate by the HTTP handler.
var (
	ErrStepUpRequired = errors.New("insufficient_user_authentication: higher ACR required")
	ErrMFAStale       = errors.New("insufficient_user_authentication: MFA assertion is stale")
)

// PermissionRequirement — per-RPC authentication policy. Resolved by the
// permission catalog.
//
// An empty RequiredACRMin means NO step-up requirement: Check fails OPEN on it
// (grpcsrv.ACRSatisfies treats ""/"0" as no floor). This is
// intentional — catalog COMPLETENESS (every RPC has an entry) and the catalog's
// per-RPC authz Check are enforced upstream by the authz middleware
// ("no entry for method" → AUTHZ_DENIED), so the step-up layer need not
// re-derive a default here. The generator injects an explicit required_acr_min
// for every NON-exempt RPC (default "2"), so a genuine empty value at runtime is
// either an exempt RPC (gated by in-handler ReBAC + FGA-exempt posture) or an
// explicit routine downgrade — never an accidental un-gated privileged RPC.
type PermissionRequirement struct {
	RequiredACRMin string        // "" → no step-up requirement (Check fails open on it)
	MFAMaxAge      time.Duration // 0 → no requirement
}

// StepUpGate — stateless evaluator.
type StepUpGate struct {
	now func() time.Time
}

// NewStepUpGate constructs an evaluator with the given clock (defaults to
// time.Now). Inject a synthetic clock for tests.
func NewStepUpGate(now func() time.Time) *StepUpGate {
	if now == nil {
		now = time.Now
	}
	return &StepUpGate{now: now}
}

// StepUpAssurance — то, ЧТО предъявил носитель личности, извлечённое из своего
// транспорта и пригодное для общего правила.
//
// ЗАЧЕМ ОТДЕЛЬНЫЙ ТИП. Носителей личности у этого края ДВА — подписанный
// предъявитель и сессия развёрнутого провайдера, — и пол обязан спрашиваться на
// обеих (#1201). У сессии нет ни удостоверения, ни claim'ов, поэтому вход пола
// нельзя было выразить `*VerifiedToken`, а второй строитель grpcsrv.StepUpInput
// рядом с первым и есть то расхождение, которое сторожат пробы паритета. Тип
// вводит ОДНУ форму входа: каждая полоса извлекает свои три величины (переноска
// её транспорта), а решает по ним по-прежнему одна функция.
//
// Все три величины обязаны быть уже отфильтрованы доверием вызывающим — см.
// grpcsrv.StepUpInput: подставленный клиентом уровень или тип принципала купил
// бы освобождение.
type StepUpAssurance struct {
	// PrincipalType — `kacho_principal_type` предъявителя либо тип субъекта,
	// резолвленного полосой сессии. Единственное значение, снимающее пол, —
	// grpcsrv.PrincipalTypeServiceAccount; решает это общее правило, не полоса.
	PrincipalType string
	// ACR — уровень уверенности НА ОСИ КАТАЛОГА ("0".."3"). Пустое / нераспознанное
	// ранжируется нулём и не удовлетворяет ни одному положительному полу.
	ACR string
	// AuthTime — момент аутентификации. У предъявителя это `auth_time` токена, у
	// сессии — `authenticated_at` провайдера. Читается только при MFAMaxAge > 0.
	AuthTime time.Time
}

// assuranceFromVerifiedToken извлекает вход пола из подписанного предъявителя.
// Размещение claim'а (верхний уровень против вложенного `ext_claims`) — забота
// края и Hydra, поэтому разбор остаётся здесь; вердикт по нему — нет.
func assuranceFromVerifiedToken(token *VerifiedToken) StepUpAssurance {
	return StepUpAssurance{
		PrincipalType: verifiedClaim(token, "kacho_principal_type"),
		ACR:           token.ACR,
		AuthTime:      token.AuthTime,
	}
}

// Check enforces both `acr` floor and `auth_time` freshness for a verified
// bearer token. Returns nil on pass, a sentinel + descriptive error on fail.
//
// It decides nothing itself: it extracts this transport's inputs and hands them
// to CheckAssurance, which renders grpcsrv.EvaluateStepUp's verdict. The
// machine-principal exemption (O-1 / #58) is an arm of that shared rule — see
// grpcsrv.EvaluateStepUp for why a service principal is exempt and why the
// exemption grants no permission.
func (g *StepUpGate) Check(token *VerifiedToken, req PermissionRequirement) error {
	if token == nil {
		return errors.New("stepup: token required")
	}
	return g.CheckAssurance(assuranceFromVerifiedToken(token), req)
}

// CheckAssurance — единственная точка вычисления пола на этом крае, общая для
// ВСЕХ полос носителя личности (здесь стояло «обеих» — полос стало три, #1215).
//
// Она НЕ решает ничего сама: строит общий grpcsrv.StepUpInput и переводит его
// вердикт в сигнальные ошибки этого транспорта. Ни ранжирования, ни
// освобождения, ни умолчания здесь заводить нельзя — ровно это расхождение и
// сторожат пробы паритета вердикта (обе стороны привязаны к одной функции).
func (g *StepUpGate) CheckAssurance(a StepUpAssurance, req PermissionRequirement) error {
	switch grpcsrv.EvaluateStepUp(grpcsrv.StepUpInput{
		PrincipalType: a.PrincipalType,
		PresentedACR:  a.ACR,
		AuthTime:      a.AuthTime,
		RequiredACR:   req.RequiredACRMin,
		MFAMaxAge:     req.MFAMaxAge,
		Now:           g.now(),
	}) {
	case grpcsrv.StepUpDenyACR:
		return fmt.Errorf("%w: presented=%q required=%q", ErrStepUpRequired, a.ACR, req.RequiredACRMin)
	case grpcsrv.StepUpDenyAuthTimeMissing:
		return fmt.Errorf("%w: auth_time missing in token", ErrMFAStale)
	case grpcsrv.StepUpDenyMFAStale:
		return fmt.Errorf("%w: age=%s max=%s", ErrMFAStale, g.now().Sub(a.AuthTime), req.MFAMaxAge)
	case grpcsrv.StepUpAllow:
	}
	return nil
}

// BuildStepUpChallenge produces an RFC 9470 §3 / RFC 6750 challenge header
// value for the given requirement. Returned string is suitable for direct
// use as the value of `WWW-Authenticate`.
//
// Examples:
//
//	BuildStepUpChallenge(PermissionRequirement{RequiredACRMin: "3"})
//	  → `Bearer error="insufficient_user_authentication",
//	     error_description="Required ACR 3 for this resource; presented ACR 2",
//	     acr_values="3"`
//
// `presentedACR` may be empty (anonymous / no token).
func BuildStepUpChallenge(req PermissionRequirement, presentedACR string) string {
	desc := fmt.Sprintf("Required ACR %s for this resource; presented ACR %s",
		req.RequiredACRMin, defaultIfEmpty(presentedACR, "0"))
	out := `Bearer error="insufficient_user_authentication", error_description="` + desc + `"`
	if req.RequiredACRMin != "" {
		out += `, acr_values="` + req.RequiredACRMin + `"`
	}
	if req.MFAMaxAge > 0 {
		out += fmt.Sprintf(`, max_age="%d"`, int(req.MFAMaxAge.Seconds()))
	}
	return out
}

func defaultIfEmpty(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
