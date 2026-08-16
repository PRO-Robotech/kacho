// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package image_test

// list_filter_operator_test.go — #460, the layer-boundary half. The use-case
// parsed the expression and then handed the repo `ast.Value` — a bare string. The
// OPERATOR died at that boundary, so `name CONTAINS "prod"` and `name="prod"`
// arrived indistinguishable and the repo built equality for both.
//
// The predicate itself is proved in internal/repo/pg/list_where_test.go.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/filter"
	"github.com/PRO-Robotech/kacho/pkg/listnarrow/narrowtest"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/image"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	"github.com/PRO-Robotech/kacho/services/storage/internal/repo/repomock"
)

func capturingImageReader(got *image.Pagination) *repomock.ImageReader {
	return &repomock.ImageReader{
		ListFunc: func(_ context.Context, p image.Pagination) ([]*domain.Image, string, error) {
			*got = p
			return nil, "", nil
		},
	}
}

// TestList_460_FilterOperatorReachesTheRepo — both operators cross the boundary as
// themselves; the equality row is the PAIRED positive control.
func TestList_460_FilterOperatorReachesTheRepo(t *testing.T) {
	for _, tc := range []struct {
		name   string
		expr   string
		wantOp string
	}{
		{"equality", `name="prod"`, filter.OpEquals},
		{"substring", `name CONTAINS "prod"`, filter.OpContains},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got image.Pagination
			uc := newListUC(capturingImageReader(&got), narrowtest.Breakglass())

			_, _, err := uc.List(aliceCtx(), image.Pagination{
				ProjectID: "prj-1", PageSize: 50, Filter: tc.expr,
			})
			require.NoError(t, err)

			require.NotNil(t, got.FilterAST, "the parsed node must reach the repo, not just its value")
			assert.Equal(t, tc.wantOp, got.FilterAST.Op,
				"the operator must survive the use-case→repo boundary")
			assert.Equal(t, "name", got.FilterAST.Field)
			assert.Equal(t, "prod", got.FilterAST.Value)
		})
	}
}

// TestList_460_NoFilterCarriesNoNode — an empty expression means "no filter".
func TestList_460_NoFilterCarriesNoNode(t *testing.T) {
	var got image.Pagination
	uc := newListUC(capturingImageReader(&got), narrowtest.Breakglass())

	_, _, err := uc.List(aliceCtx(), image.Pagination{ProjectID: "prj-1", PageSize: 50})
	require.NoError(t, err)
	assert.Nil(t, got.FilterAST)
}

// TestList_460_MalformedFilterStillInvalidArgument — the refusal on a bad
// expression did not move, and still precedes the repo.
func TestList_460_MalformedFilterStillInvalidArgument(t *testing.T) {
	reader := &repomock.ImageReader{
		ListFunc: func(context.Context, image.Pagination) ([]*domain.Image, string, error) {
			t.Fatal("repo.List must not run on a malformed filter")
			return nil, "", nil
		},
	}
	uc := newListUC(reader, narrowtest.Breakglass())

	_, _, err := uc.List(aliceCtx(), image.Pagination{
		ProjectID: "prj-1", PageSize: 50, Filter: `bogus="x"`,
	})
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}
