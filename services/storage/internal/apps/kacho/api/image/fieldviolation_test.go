// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package image_test

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/image"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	"github.com/PRO-Robotech/kacho/services/storage/internal/ports/portmock"
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
// contract for Image.Create: an over-limit description / labels map must come back
// as INVALID_ARGUMENT whose details name the offending field.
//
// Companion to TestCreateBVADescriptionLabels, which asserts only the CODE. That
// weaker assertion stayed green while the field name was being thrown away.
func TestCreateOverLimitNamesTheField(t *testing.T) {
	base := func() *domain.Image {
		return &domain.Image{
			ProjectID: "prj-1", RegionID: "ru-central1", Name: "bva",
			SourceVolume: "vol00000000000000000",
		}
	}
	newUC := func() *image.UseCase {
		return image.New(&portmock.ImageReader{}, &portmock.ImageWriter{},
			&portmock.PeerClient{}, &portmock.PeerClient{}, nil, serviceerr.ToStatus)
	}

	cases := []struct {
		name  string
		mut   func(*domain.Image)
		field string
	}{
		{
			name:  "description over 256",
			mut:   func(i *domain.Image) { i.Description = strings.Repeat("x", 257) },
			field: "description",
		},
		{
			name: "labels over 64 pairs",
			mut: func(i *domain.Image) {
				labels := make(map[string]string, 65)
				for n := 0; n < 65; n++ {
					labels["k"+strings.Repeat("z", n)] = "v"
				}
				i.Labels = labels
			},
			field: "labels",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			img := base()
			tc.mut(img)
			_, err := newUC().Create(context.Background(), img)
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
