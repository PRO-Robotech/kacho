// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware

// auth_public_allowlist_test.go — аутентификация читает ТОТ ЖЕ список публичных
// методов, что модель прав (kacho#3033).
//
// Комментарий к DefaultPublicAllowlist обещает: член списка снимает и
// аутентификацию, и проверку прав. Интерцептор аутентификации стоит в цепочке
// раньше интерцептора прав, и пока он списка не читал, обещание держала только
// вторая половина: проба Health/Check без токена в production получала
// Unauthenticated, не дойдя до шага, который её пропускает.
//
// Что снимается — ровно одно: отказ «удостоверение не предъявлено». Предъявленное
// удостоверение по-прежнему судится — негодный токен на публичном методе остаётся
// отказом, иначе список стал бы полосой обхода проверки подписи.

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/corelib/operations"

	"github.com/PRO-Robotech/kacho/gateway/internal/authnrefusal"
)

type publicAllowlistLookup struct{ subj Subject }

func (l publicAllowlistLookup) LookupByExternalID(context.Context, string) (Subject, error) {
	return l.subj, nil
}

func publicAllowlistLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// callUnary runs the interceptor and reports whether the handler ran, the
// context it ran with, and the refusal.
func callUnary(t *testing.T, a *AuthInterceptor, ctx context.Context, fullMethod string) (bool, context.Context, error) {
	t.Helper()
	var (
		called bool
		seen   context.Context
	)
	_, err := a.Unary()(ctx, nil, &grpc.UnaryServerInfo{FullMethod: fullMethod},
		func(c context.Context, _ any) (any, error) {
			called, seen = true, c
			return nil, nil
		})
	return called, seen, err
}

func requireAuthnRefusal(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unauthenticated, st.Code())
	assert.Equal(t, authnrefusal.Message, st.Message())
}

var productionModes = []AuthMode{AuthModeProduction, AuthModeProductionStrict}

// Every entry of the list the authz middleware reads is admitted tokenless —
// iterated from the source itself, so an entry added there is covered here
// without a second copy of the name.
func TestAuth_Production_PublicAllowlistEntryWithoutTokenIsAdmitted(t *testing.T) {
	entries := DefaultPublicAllowlist()
	require.NotEmpty(t, entries, "premise: the public list is non-empty")
	for _, mode := range productionModes {
		for _, fqn := range entries {
			a := NewAuthInterceptor(mode, "", publicAllowlistLookup{}, publicAllowlistLogger())
			called, ctx, err := callUnary(t, a, context.Background(), "/"+fqn)
			require.NoError(t, err, "mode=%v fqn=%s", mode, fqn)
			require.True(t, called, "mode=%v fqn=%s: handler must run", mode, fqn)

			// No name is invented for a caller that presented none: the
			// anonymous principal reads as an identity downstream.
			_, hasPrincipal := operations.PrincipalFromContextOK(ctx)
			assert.False(t, hasPrincipal, "mode=%v fqn=%s: no principal is injected", mode, fqn)
			md, _ := metadata.FromIncomingContext(ctx)
			assert.Empty(t, md.Get(a.mdKeyPrincipalID), "mode=%v fqn=%s: no principal metadata", mode, fqn)
		}
	}
	t.Logf("census: modes=%d entries=%d", len(productionModes), len(entries))
}

// Twin, one fact changed — the method. Health/Watch is the same service, one
// method over, and is NOT on the list: it stays refused without a token.
func TestAuth_Production_MethodOffPublicAllowlistWithoutTokenIsRefused(t *testing.T) {
	for _, mode := range productionModes {
		for _, m := range []string{"/grpc.health.v1.Health/Watch", "/test/Method"} {
			a := NewAuthInterceptor(mode, "", publicAllowlistLookup{}, publicAllowlistLogger())
			called, _, err := callUnary(t, a, context.Background(), m)
			requireAuthnRefusal(t, err)
			assert.False(t, called, "mode=%v method=%s", mode, m)
		}
	}
}

// One source: a method that reaches the list the authz middleware is built
// from passes authentication too. The interceptor's set is DERIVED from
// DefaultPublicAllowlist, and an injected entry is admitted by the same reader.
func TestAuth_PublicAllowlistHasOneSource(t *testing.T) {
	a := NewAuthInterceptor(AuthModeProduction, "", publicAllowlistLookup{}, publicAllowlistLogger())
	want := publicMethodSet(DefaultPublicAllowlist())
	assert.Equal(t, want, a.publicMethods, "the authN reader is built from DefaultPublicAllowlist, not a copy")

	const injected = "kacho.cloud.injected.v1.ProbeService/Ping"
	called, _, err := callUnary(t, a, context.Background(), "/"+injected)
	requireAuthnRefusal(t, err)
	require.False(t, called, "control: off the list, refused")

	a.publicMethods = publicMethodSet(append(DefaultPublicAllowlist(), injected))
	called, _, err = callUnary(t, a, context.Background(), "/"+injected)
	require.NoError(t, err)
	assert.True(t, called, "injection: on the list, admitted")
}
