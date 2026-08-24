// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package registrytokenwire — composition-root adapters binding the registry
// `/iam/token` shim use-case to iam infrastructure:
//
//   - HydraExchangeAdapter — brokers the client_credentials + private_key_jwt
//     exchange with Hydra's public token endpoint, mapping issuer-unavailability
//     to the use-case's fail-closed sentinel. Пользуется им АНОНИМНЫЙ поток на
//     контуре, ещё не переведённом на нашу чеканку.
//
// Обратного резолва ключа служебной учётки по client_id здесь больше нет: полоса
// предъявленного удостоверения принимает только базовый токен доступа, и ключевого
// материала в поле пароля не бывает (задача #1143).
//
// These are thin adapters over already-tested primitives; they carry no policy.
package registrytokenwire

import (
	"context"
	"errors"
	"fmt"

	registrytokenuc "github.com/PRO-Robotech/kacho/services/iam/internal/apps/kacho/api/registry_token"
	"github.com/PRO-Robotech/kacho/services/iam/internal/clients"
)

// ── Hydra token exchange ────────────────────────────────────────────────────

// hydraClientCredentials — the Hydra public token endpoint (satisfied by
// clients.HydraTokenClient).
type hydraClientCredentials interface {
	ClientCredentials(ctx context.Context, req clients.ClientCredentialsRequest) (clients.TokenResponse, error)
}

// HydraExchangeAdapter — the TokenExchanger backed by Hydra's public token
// endpoint. Issuer unavailability is surfaced as the use-case's fail-closed
// sentinel; a Hydra rejection is returned as-is (the use-case collapses it to a
// 401 challenge).
type HydraExchangeAdapter struct {
	client hydraClientCredentials
}

// NewHydraExchange — builder.
func NewHydraExchange(c hydraClientCredentials) *HydraExchangeAdapter {
	return &HydraExchangeAdapter{client: c}
}

var _ registrytokenuc.TokenExchanger = (*HydraExchangeAdapter)(nil)

// Exchange brokers the client_credentials + private_key_jwt exchange.
func (a *HydraExchangeAdapter) Exchange(ctx context.Context, in registrytokenuc.ExchangeInput) (registrytokenuc.ExchangeOutput, error) {
	out, err := a.client.ClientCredentials(ctx, clients.ClientCredentialsRequest{
		ClientAssertion: in.ClientAssertion,
		Audience:        in.Audience,
		Scope:           in.Scope,
	})
	if err != nil {
		if errors.Is(err, clients.ErrHydraUnavailable) {
			// Причина ОБОРАЧИВАЕТСЯ, а не подменяется: наружу отказ всё равно
			// уйдёт фиксированным текстом (собирает use-case), а в журнал
			// попадёт то, что ответила сеть. Голый sentinel здесь означал бы
			// пересказ собственного решения об отказе — ровно то, что стоило
			// двадцати минут разбора на живом стенде у соседней выдачи.
			return registrytokenuc.ExchangeOutput{}, fmt.Errorf("%w: %w",
				registrytokenuc.ErrIssuerUnavailable, err)
		}
		// Hydra rejection (invalid_client / invalid_grant) — collapsed to 401
		// upstream; no raw Hydra detail is propagated.
		return registrytokenuc.ExchangeOutput{}, registrytokenuc.ErrInvalidCredentials
	}
	return registrytokenuc.ExchangeOutput{AccessToken: out.AccessToken, ExpiresIn: out.ExpiresIn}, nil
}
