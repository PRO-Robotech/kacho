// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package image_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	"github.com/PRO-Robotech/kacho/services/storage/internal/ports/portmock"
	"github.com/PRO-Robotech/kacho/services/storage/internal/service/image"
	"github.com/PRO-Robotech/kacho/services/storage/internal/serviceerr"
)

// ── Update: description / labels are refused at the request edge, by name ────
//
// Create runs both fields through pkg/validate. Update ran only the name through
// the domain validator, so an over-limit description or labels map travelled to
// the UPDATE, was caught by images_description_check / images_labels_valid, and
// came back ASYNCHRONOUSLY inside the operation error as the generic "Illegal
// argument" — late, and with no way to tell which field was rejected.
//
// The observable is asserted: no Operation is handed back (the refusal is
// synchronous), the code is INVALID_ARGUMENT, and the offending field is named in
// the google.rpc.BadRequest DETAILS. The MESSAGE is deliberately not asserted —
// pkg/validate keeps it generic by contract (helpers violatedFields / hasField
// live in fieldviolation_test.go).

const imgUpdID = "img00000000000000000"

// updLabels builds a labels map with n pairs (the contract limit is 64). Keys stay
// short on purpose: a key longer than 63 chars is its own violation, and mixing it
// in would make the at-the-limit case fail for a reason that has nothing to do with
// the pair count.
func updLabels(n int) map[string]string {
	labels := make(map[string]string, n)
	for i := 0; i < n; i++ {
		labels[fmt.Sprintf("k%03d", i)] = "v"
	}
	return labels
}

func TestUpdateOverLimitNamesTheField(t *testing.T) {
	newUC := func(t *testing.T) *image.UseCase {
		t.Helper()
		writer := &portmock.ImageWriter{
			UpdateFunc: func(context.Context, string, image.ImageUpdate) (*domain.Image, error) {
				t.Error("writer.Update must not be reached: the request edge rejects the body")
				return &domain.Image{ID: imgUpdID}, nil
			},
		}
		return image.New(&portmock.ImageReader{}, writer,
			&portmock.PeerClient{}, &portmock.PeerClient{}, portmock.NewOpsRepo(), serviceerr.ToStatus)
	}

	cases := []struct {
		name        string
		mask        []string
		description string
		labels      map[string]string
		field       string
	}{
		{
			name:        "description over 256 in the mask",
			mask:        []string{"description"},
			description: strings.Repeat("x", 257),
			field:       "description",
		},
		{
			name:   "labels over 64 pairs in the mask",
			mask:   []string{"labels"},
			labels: updLabels(65),
			field:  "labels",
		},
		{
			// An empty update_mask is a full-object PATCH, not "change nothing":
			// every mutable field of the body is applied, so every mutable field
			// has to be validated.
			name:        "description over 256 under an empty mask (full-object PATCH)",
			mask:        nil,
			description: strings.Repeat("x", 257),
			field:       "description",
		},
		{
			name:   "labels over 64 pairs under an empty mask (full-object PATCH)",
			mask:   nil,
			labels: updLabels(65),
			field:  "labels",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			op, err := newUC(t).Update(context.Background(), imgUpdID, tc.mask,
				"", tc.description, tc.labels)
			if op != nil {
				t.Fatalf("Update returned operation %v — the refusal must be synchronous", op)
			}
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

// TestUpdateSkipsFieldsOutsideTheMask is the control for the test above: a field
// the mask does not list is NOT applied, so it must NOT be validated either.
// Rejecting a request over a value the service then ignores would be a new defect,
// not a fix.
func TestUpdateSkipsFieldsOutsideTheMask(t *testing.T) {
	var applied image.ImageUpdate
	writer := &portmock.ImageWriter{
		UpdateFunc: func(_ context.Context, _ string, u image.ImageUpdate) (*domain.Image, error) {
			applied = u
			return &domain.Image{ID: imgUpdID, Name: "renamed"}, nil
		},
	}
	ops := portmock.NewOpsRepo()
	uc := image.New(&portmock.ImageReader{}, writer,
		&portmock.PeerClient{}, &portmock.PeerClient{}, ops, serviceerr.ToStatus)

	op, err := uc.Update(context.Background(), imgUpdID, []string{"name"},
		"renamed", strings.Repeat("x", 257), updLabels(65))
	if err != nil {
		t.Fatalf("Update mask=[name] with an unapplied over-limit body: %v", err)
	}
	done := portmock.AwaitOpDone(t, ops, op.ID)
	if done.Error != nil {
		t.Fatalf("op error = %v, want success — the body outside the mask is ignored", done.Error)
	}
	if applied.Description != nil {
		t.Fatalf("description was applied (%q) although the mask does not list it", *applied.Description)
	}
	if applied.LabelsSet {
		t.Fatal("labels were applied although the mask does not list them")
	}
}

// TestUpdateAcceptsDescriptionAndLabelsAtTheLimit keeps the boundary open: exactly
// 256 characters and exactly 64 pairs are legal input, on the masked and the
// full-object path alike.
func TestUpdateAcceptsDescriptionAndLabelsAtTheLimit(t *testing.T) {
	atLimit := updLabels(64)

	for _, mask := range [][]string{{"description", "labels"}, nil} {
		writer := &portmock.ImageWriter{
			UpdateFunc: func(_ context.Context, _ string, _ image.ImageUpdate) (*domain.Image, error) {
				return &domain.Image{ID: imgUpdID}, nil
			},
		}
		ops := portmock.NewOpsRepo()
		uc := image.New(&portmock.ImageReader{}, writer,
			&portmock.PeerClient{}, &portmock.PeerClient{}, ops, serviceerr.ToStatus)

		op, err := uc.Update(context.Background(), imgUpdID, mask,
			"", strings.Repeat("x", 256), atLimit)
		if err != nil {
			t.Fatalf("Update mask=%v at the limit was refused: %v", mask, err)
		}
		if done := portmock.AwaitOpDone(t, ops, op.ID); done.Error != nil {
			t.Fatalf("Update mask=%v at the limit failed asynchronously: %v", mask, done.Error)
		}
	}
}
