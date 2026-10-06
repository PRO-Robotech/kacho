// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

// auth_public_allowlist_token_test.go — близнец к
// auth_public_allowlist_test.go, изменён один факт: удостоверение ПРЕДЪЯВЛЕНО
// (kacho#3033). Список публичных методов снимает только отказ «удостоверение не
// предъявлено»; предъявленное судится как на любом методе — негодное отвергается,
// годное даёт своего субъекта. Иначе член списка стал бы полосой обхода проверки
// подписи.

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/corelib/operations"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

const publicCheckMethod = "/grpc.health.v1.Health/Check"

func TestAuth_Production_PublicAllowlistEntryWithTokenIsStillJudged(t *testing.T) {
	for _, mode := range []middleware.AuthMode{middleware.AuthModeProduction, middleware.AuthModeProductionStrict} {
		t.Run(fmt.Sprint(mode)+"/garbage token is refused", func(t *testing.T) {
			fix := newJWKSFixture(t, "RS256")
			auth := middleware.NewAuthInterceptor(mode, "", &fakeLookup{}, authTestLogger()).
				WithVerifier(rs256Verifier(t, fix))
			ctx := metadata.NewIncomingContext(context.Background(),
				metadata.Pairs("authorization", "Bearer garbage"))
			called := false
			_, err := auth.Unary()(ctx, nil, &grpc.UnaryServerInfo{FullMethod: publicCheckMethod},
				func(context.Context, any) (any, error) { called = true; return nil, nil })
			require.Error(t, err)
			st, _ := status.FromError(err)
			assert.Equal(t, codes.Unauthenticated, st.Code())
			assert.False(t, called, "a presented credential is judged on a public method too")
		})

		t.Run(fmt.Sprint(mode)+"/valid token yields its principal", func(t *testing.T) {
			fix := newJWKSFixture(t, "RS256")
			auth := middleware.NewAuthInterceptor(mode, "", &fakeLookup{}, authTestLogger()).
				WithVerifier(rs256Verifier(t, fix))
			token := fix.sign(t, issuerClaims("user", "usr_public_check"))
			ctx := metadata.NewIncomingContext(context.Background(),
				metadata.Pairs("authorization", "Bearer "+token))
			var p operations.Principal
			_, err := auth.Unary()(ctx, nil, &grpc.UnaryServerInfo{FullMethod: publicCheckMethod},
				func(c context.Context, _ any) (any, error) { p = operations.PrincipalFromContext(c); return nil, nil })
			require.NoError(t, err)
			assert.Equal(t, "usr_public_check", p.ID)
		})
	}
}
