// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

func TestSubjectExtractor_UnifiedPrincipal_User(t *testing.T) {
	e := middleware.NewSubjectExtractor(false)
	tok := &middleware.VerifiedToken{
		Subject: "hydra-sub-abc",
		ExtClaims: map[string]any{
			"kaname_principal_type": "user",
			"kaname_principal_id":   "usr_alice",
		},
	}
	r, ok := e.Extract(tok)
	require.True(t, ok)
	assert.Equal(t, "user:usr_alice", r.FGA)
	assert.Equal(t, middleware.SubjectKindUser, r.Kind)
	assert.Equal(t, "usr_alice", r.ID)
}

func TestSubjectExtractor_UnifiedPrincipal_ServiceAccount(t *testing.T) {
	e := middleware.NewSubjectExtractor(false)
	tok := &middleware.VerifiedToken{
		ExtClaims: map[string]any{
			"kaname_principal_type": "service_account",
			"kaname_principal_id":   "sva_robot",
		},
	}
	r, ok := e.Extract(tok)
	require.True(t, ok)
	assert.Equal(t, "service_account:sva_robot", r.FGA)
	assert.Equal(t, middleware.SubjectKindServiceAccount, r.Kind)
}

func TestSubjectExtractor_UnifiedPrincipal_WorkloadAlias(t *testing.T) {
	e := middleware.NewSubjectExtractor(false)
	tok := &middleware.VerifiedToken{
		ExtClaims: map[string]any{
			"kaname_principal_type": "workload",
			"kaname_principal_id":   "wid_pod1",
		},
	}
	r, ok := e.Extract(tok)
	require.True(t, ok)
	assert.Equal(t, "workload:wid_pod1", r.FGA)
	assert.Equal(t, middleware.SubjectKindWorkload, r.Kind)
}

func TestSubjectExtractor_FallbackKachoUserID(t *testing.T) {
	e := middleware.NewSubjectExtractor(false)
	tok := &middleware.VerifiedToken{
		ExtClaims: map[string]any{
			"kaname_user_id": "usr_legacy",
		},
	}
	r, ok := e.Extract(tok)
	require.True(t, ok)
	assert.Equal(t, "user:usr_legacy", r.FGA)
	assert.Equal(t, "ext_claims.kaname_user_id", r.Source)
}

// TestSubjectExtractor_UnmintedIdClaimResolvesNothing — здесь стояли две пробы
// полос `kaname_sa_id` и `kaname_workload_id`. Полосы сняты вместе со своим
// предметом: ни одно из двух имён не чеканит ни один путь выпуска, поэтому
// сработать они не могли ни на одном токене этого продукта.
//
// Проба ЗАМЕНЕНА, а не удалена: она запирает снятие. Состав, называющий машину
// только снятым именем, обязан не резолвить НИЧЕГО — иначе полоса вернулась бы
// незамеченной, а вернувшаяся `workload` ещё и подменила бы опознаваемый в
// журнале `external:<sub>` на субъект, которого модель прав назвать не может
// (тип `workload` в ней не объявлен).
//
// Рядом — ПОЛОЖИТЕЛЬНЫЙ близнец: тот же вызывающий, названный единой формой,
// резолвится. Без него отрицание зеленело бы и на разборе, переставшем работать
// вовсе.
func TestSubjectExtractor_UnmintedIdClaimResolvesNothing(t *testing.T) {
	e := middleware.NewSubjectExtractor(false)
	for _, claim := range []string{"kaname_sa_id", "kaname_workload_id"} {
		tok := &middleware.VerifiedToken{
			Subject:   "hydra-sub-xyz",
			ExtClaims: map[string]any{claim: "sva_old"},
		}
		_, ok := e.Extract(tok)
		assert.False(t, ok, "%s полосой резолва больше не является", claim)
	}

	tok := &middleware.VerifiedToken{
		ExtClaims: map[string]any{
			"kaname_principal_type": "service_account",
			"kaname_principal_id":   "sva_old",
		},
	}
	r, ok := e.Extract(tok)
	require.True(t, ok, "единая форма обязана резолвиться — иначе отрицание выше "+
		"зеленеет на разборе, который перестал работать вовсе")
	assert.Equal(t, "service_account:sva_old", r.FGA)
}

func TestSubjectExtractor_NoFallback_NoExtClaims_Rejects(t *testing.T) {
	e := middleware.NewSubjectExtractor(false)
	tok := &middleware.VerifiedToken{Subject: "hydra-sub-xyz"}
	_, ok := e.Extract(tok)
	assert.False(t, ok)
}

func TestSubjectExtractor_AllowFallback_NoExtClaims_External(t *testing.T) {
	e := middleware.NewSubjectExtractor(true)
	tok := &middleware.VerifiedToken{Subject: "hydra-sub-xyz"}
	r, ok := e.Extract(tok)
	require.True(t, ok)
	assert.Equal(t, "external:hydra-sub-xyz", r.FGA)
	assert.Equal(t, middleware.SubjectKindExternal, r.Kind)
	assert.Equal(t, "jwt.sub", r.Source)
}

func TestSubjectExtractor_NilToken(t *testing.T) {
	e := middleware.NewSubjectExtractor(true)
	_, ok := e.Extract(nil)
	assert.False(t, ok)
}

func TestSubjectExtractor_UnknownPrincipalType_FallsThrough(t *testing.T) {
	e := middleware.NewSubjectExtractor(false)
	tok := &middleware.VerifiedToken{
		ExtClaims: map[string]any{
			"kaname_principal_type": "alien",
			"kaname_principal_id":   "x",
			"kaname_user_id":        "usr_fallback",
		},
	}
	r, ok := e.Extract(tok)
	require.True(t, ok)
	assert.Equal(t, "user:usr_fallback", r.FGA)
}

func TestSubjectExtractor_AliasFormats(t *testing.T) {
	tests := []struct {
		raw  string
		want middleware.SubjectKind
	}{
		{"user", middleware.SubjectKindUser},
		{"USR", middleware.SubjectKindUser},
		{"USER", middleware.SubjectKindUser},
		{"service_account", middleware.SubjectKindServiceAccount},
		{"service-account", middleware.SubjectKindServiceAccount},
		{"serviceaccount", middleware.SubjectKindServiceAccount},
		{"sva", middleware.SubjectKindServiceAccount},
		{"workload", middleware.SubjectKindWorkload},
		{"wid", middleware.SubjectKindWorkload},
	}
	e := middleware.NewSubjectExtractor(false)
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			tok := &middleware.VerifiedToken{
				ExtClaims: map[string]any{
					"kaname_principal_type": tt.raw,
					"kaname_principal_id":   "abc",
				},
			}
			r, ok := e.Extract(tok)
			require.True(t, ok)
			assert.Equal(t, tt.want, r.Kind)
		})
	}
}

func TestResolvedSubject_String(t *testing.T) {
	r := middleware.ResolvedSubject{FGA: "user:usr_x"}
	assert.Equal(t, "user:usr_x", r.String())
	assert.Equal(t, "<unknown>", middleware.ResolvedSubject{}.String())
}

func TestSubjectExtractor_EmptyPrincipalFields_FallsThrough(t *testing.T) {
	e := middleware.NewSubjectExtractor(true)
	// Both empty → should fall through to next rule (kaname_user_id) then sub fallback.
	tok := &middleware.VerifiedToken{
		Subject: "hydra-sub",
		ExtClaims: map[string]any{
			"kaname_principal_type": "",
			"kaname_principal_id":   "",
		},
	}
	r, ok := e.Extract(tok)
	require.True(t, ok)
	assert.Equal(t, "external:hydra-sub", r.FGA)
}
