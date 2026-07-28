// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package snapshot_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/snapshot"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	"github.com/PRO-Robotech/kacho/services/storage/internal/repo/repomock"
)

// ── Update: description / labels / name are refused at the request edge ──────
//
// Create runs description and labels through pkg/validate and the name through the
// domain validator. Update ran NONE of the three, so an over-limit description, an
// over-limit labels map or an illegal name travelled to the UPDATE, was caught by
// snapshots_description_check / snapshots_labels_valid / snapshots_name_check, and
// came back ASYNCHRONOUSLY inside the operation error as the generic "Illegal
// argument" — late, and with no way to tell which field was rejected.
//
// Snapshot is the widest of the three resources here: Volume and Image at least
// validated the name on the update path.
//
// The observable is asserted: no Operation is handed back (the refusal is
// synchronous), the code is INVALID_ARGUMENT, and for description/labels the
// offending field is named in the google.rpc.BadRequest DETAILS. Their MESSAGE is
// deliberately not asserted — pkg/validate keeps it generic by contract (helpers
// violatedFields / hasField live in fieldviolation_test.go). The name is the
// exception: its refusal is the domain's own fixed contract text, so that one IS
// asserted on the message.

const snapUpdID = "snp00000000000000000"

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
	newUC := func(t *testing.T) *snapshot.UseCase {
		t.Helper()
		repo := &repomock.SnapshotRepo{
			UpdateFunc: func(context.Context, string, snapshot.SnapshotUpdate) (*domain.Snapshot, error) {
				t.Error("repo.Update must not be reached: the request edge rejects the body")
				return &domain.Snapshot{ID: snapUpdID}, nil
			},
		}
		return snapshot.New(repo, &repomock.PeerClient{}, repomock.NewOpsRepo(), serviceerr.ToStatus)
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
			op, err := newUC(t).Update(context.Background(), snapUpdID, tc.mask,
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

// TestUpdateRejectsIllegalNameSynchronously closes the gap Volume and Image did not
// have: Snapshot.Update never ran the new name through the domain validator, so an
// uppercase / non-ASCII / over-long name reached snapshots_name_check and failed the
// operation instead of the call.
//
// Here the MESSAGE is asserted, because for the name the contract text itself names
// the field ("Illegal argument name") — the same text Create returns.
func TestUpdateRejectsIllegalNameSynchronously(t *testing.T) {
	repo := &repomock.SnapshotRepo{
		UpdateFunc: func(context.Context, string, snapshot.SnapshotUpdate) (*domain.Snapshot, error) {
			t.Error("repo.Update must not be reached: the request edge rejects the name")
			return &domain.Snapshot{ID: snapUpdID}, nil
		},
	}
	uc := snapshot.New(repo, &repomock.PeerClient{}, repomock.NewOpsRepo(), serviceerr.ToStatus)

	for _, tc := range []struct {
		label string
		mask  []string
		name  string
	}{
		{label: "uppercase in the mask", mask: []string{"name"}, name: "Snap_Upper"},
		{label: "non-ASCII in the mask", mask: []string{"name"}, name: "снимок"},
		{label: "over 63 chars in the mask", mask: []string{"name"}, name: "n" + strings.Repeat("a", 63)},
		{label: "uppercase under an empty mask", mask: nil, name: "Snap_Upper"},
	} {
		t.Run(tc.label, func(t *testing.T) {
			op, err := uc.Update(context.Background(), snapUpdID, tc.mask, tc.name, "", nil)
			if op != nil {
				t.Fatalf("Update returned operation %v — the refusal must be synchronous", op)
			}
			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("code = %v, want InvalidArgument (err=%v)", status.Code(err), err)
			}
			if got := status.Convert(err).Message(); got != "Illegal argument name" {
				t.Fatalf("message = %q, want %q", got, "Illegal argument name")
			}
		})
	}
}

// TestUpdateSkipsFieldsOutsideTheMask is the control for the tests above: a field
// the mask does not list is NOT applied, so it must NOT be validated either.
// Rejecting a request over a value the service then ignores would be a new defect,
// not a fix.
func TestUpdateSkipsFieldsOutsideTheMask(t *testing.T) {
	var applied snapshot.SnapshotUpdate
	repo := &repomock.SnapshotRepo{
		UpdateFunc: func(_ context.Context, _ string, u snapshot.SnapshotUpdate) (*domain.Snapshot, error) {
			applied = u
			return &domain.Snapshot{ID: snapUpdID, Description: "kept"}, nil
		},
	}
	ops := repomock.NewOpsRepo()
	uc := snapshot.New(repo, &repomock.PeerClient{}, ops, serviceerr.ToStatus)

	op, err := uc.Update(context.Background(), snapUpdID, []string{"description"},
		"Illegal_Name", "fine", updLabels(65))
	if err != nil {
		t.Fatalf("Update mask=[description] with an unapplied illegal name/labels: %v", err)
	}
	done := repomock.AwaitOpDone(t, ops, op.ID)
	if done.Error != nil {
		t.Fatalf("op error = %v, want success — the body outside the mask is ignored", done.Error)
	}
	if applied.Name != nil {
		t.Fatalf("name was applied (%q) although the mask does not list it", *applied.Name)
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
		repo := &repomock.SnapshotRepo{
			UpdateFunc: func(context.Context, string, snapshot.SnapshotUpdate) (*domain.Snapshot, error) {
				return &domain.Snapshot{ID: snapUpdID}, nil
			},
		}
		ops := repomock.NewOpsRepo()
		uc := snapshot.New(repo, &repomock.PeerClient{}, ops, serviceerr.ToStatus)

		op, err := uc.Update(context.Background(), snapUpdID, mask,
			"", strings.Repeat("x", 256), atLimit)
		if err != nil {
			t.Fatalf("Update mask=%v at the limit was refused: %v", mask, err)
		}
		if done := repomock.AwaitOpDone(t, ops, op.ID); done.Error != nil {
			t.Fatalf("Update mask=%v at the limit failed asynchronously: %v", mask, done.Error)
		}
	}
}
