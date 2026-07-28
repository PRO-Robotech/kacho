// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package snapshot_test

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/snapshot"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	"github.com/PRO-Robotech/kacho/services/storage/internal/ports/portmock"
	"github.com/PRO-Robotech/kacho/services/storage/internal/serviceerr"
)

// violatedFields returns the field names carried by the google.rpc.BadRequest
// detail of err. Empty result ⇒ the status carries no field violation at all.
//
// The status MESSAGE is deliberately not asserted here: by contract pkg/validate
// returns the generic "invalid argument" text and puts the field name in the
// DETAILS, so a message assertion would lock the wrong half of the contract.
func violatedFields(err error) []string {
	st, ok := status.FromError(err)
	if !ok {
		return nil
	}
	var out []string
	for _, d := range st.Details() {
		br, ok := d.(*errdetails.BadRequest)
		if !ok {
			continue
		}
		for _, v := range br.GetFieldViolations() {
			out = append(out, v.GetField())
		}
	}
	return out
}

// hasField reports whether fields contains want.
func hasField(fields []string, want string) bool {
	for _, f := range fields {
		if f == want {
			return true
		}
	}
	return false
}

// TestCreateOverLimitNamesTheField locks the observable half of the field-violation
// contract for Snapshot.Create: an over-limit description / labels map must come
// back as INVALID_ARGUMENT whose details name the offending field.
//
// The use-case used to rebuild pkg/validate's rich error from its TEXT, dropping
// the BadRequest detail and handing the caller gRPC's own wire framing as the
// message.
func TestCreateOverLimitNamesTheField(t *testing.T) {
	base := func() *domain.Snapshot {
		return &domain.Snapshot{
			ProjectID: "prj-1", SourceVolumeID: "vol00000000000000000", Name: "bva",
		}
	}
	newUC := func() *snapshot.UseCase {
		return snapshot.New(&portmock.SnapshotRepo{}, &portmock.PeerClient{}, nil, serviceerr.ToStatus)
	}

	cases := []struct {
		name  string
		mut   func(*domain.Snapshot)
		field string
	}{
		{
			name:  "description over 256",
			mut:   func(s *domain.Snapshot) { s.Description = strings.Repeat("x", 257) },
			field: "description",
		},
		{
			name: "labels over 64 pairs",
			mut: func(s *domain.Snapshot) {
				labels := make(map[string]string, 65)
				for i := 0; i < 65; i++ {
					labels["k"+strings.Repeat("z", i)] = "v"
				}
				s.Labels = labels
			},
			field: "labels",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := base()
			tc.mut(s)
			_, err := newUC().Create(context.Background(), s)
			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("code = %v, want InvalidArgument (err=%v)", status.Code(err), err)
			}
			got := violatedFields(err)
			if !hasField(got, tc.field) {
				t.Fatalf("field violations = %v, want one naming %q (message was %q)",
					got, tc.field, status.Convert(err).Message())
			}
		})
	}
}
