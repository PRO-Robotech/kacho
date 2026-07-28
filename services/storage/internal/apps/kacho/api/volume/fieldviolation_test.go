// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package volume_test

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/volume"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	"github.com/PRO-Robotech/kacho/services/storage/internal/repo/repomock"
)

// violatedFields returns the field names carried by the google.rpc.BadRequest
// detail of err, in order. An empty result means the status carries no field
// violation at all.
//
// The status MESSAGE is deliberately NOT asserted in this file. By contract
// pkg/validate returns the generic "invalid argument" text and puts the offending
// field name in the DETAILS — asserting the message would lock the wrong half of
// the contract and would keep passing while the details stayed empty.
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
// contract for Volume.Create: an over-limit description / labels map must come back
// as INVALID_ARGUMENT whose details name the offending field.
//
// The use-case used to rebuild pkg/validate's rich error from its TEXT
// (`fmt.Errorf("%w: %s", storageerr.ErrInvalidArg, err.Error())`). That threw the
// BadRequest detail away and left the caller with a correct code, a message quoting
// gRPC's own wire framing, and no way to tell WHICH field was rejected.
func TestCreateOverLimitNamesTheField(t *testing.T) {
	base := func() *domain.Volume {
		return &domain.Volume{
			ProjectID: "prj-1", ZoneID: "zone-a", DiskTypeID: "block-balanced",
			Name: "bva", SizeBytes: 1 << 30,
		}
	}
	newUC := func() *volume.UseCase {
		return volume.New(&repomock.VolumeReader{}, &repomock.VolumeWriter{},
			&repomock.PeerClient{}, &repomock.PeerClient{}, nil, serviceerr.ToStatus)
	}

	cases := []struct {
		name  string
		mut   func(*domain.Volume)
		field string
	}{
		{
			name:  "description over 256",
			mut:   func(v *domain.Volume) { v.Description = strings.Repeat("x", 257) },
			field: "description",
		},
		{
			name: "labels over 64 pairs",
			mut: func(v *domain.Volume) {
				labels := make(map[string]string, 65)
				for i := 0; i < 65; i++ {
					labels["k"+strings.Repeat("z", i)] = "v"
				}
				v.Labels = labels
			},
			field: "labels",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := base()
			tc.mut(v)
			_, err := newUC().Create(context.Background(), v)
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
