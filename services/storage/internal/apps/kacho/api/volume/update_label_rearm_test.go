// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package volume_test

// update_label_rearm_test.go — the switch that arms the label re-emit.
//
// Access handed out through a label selector is taken back by removing the label,
// and that only works because an Update carrying labels re-tells the authority
// what the labels are now. The whole mechanism hangs off ONE derived flag:
// VolumeUpdate.LabelsSet. The writer re-emits when it is set and stays silent
// when it is not, so whatever decides that flag decides whether a label removal
// is a revoke or a no-op.
//
// Everything downstream of the flag was locked; the flag itself was not. Every
// test of the re-emit builds VolumeUpdate{LabelsSet: true} by hand, so all of
// them stay green even if the request layer stops setting it — the machine is
// tested, the switch that turns it on is not. The path that would go quiet first
// is the full-object PATCH: an empty update_mask names no field, so a plausible
// reading ("only re-emit when the mask says labels") disarms revoke for every
// full PATCH while naming labels explicitly keeps working, and nothing goes red.
//
// Asserted here is that the flag comes back armed on EVERY way a label can be
// taken off a volume — cleared to nothing, one key dropped, the set replaced —
// under an explicit mask AND under the empty mask. The control for the other
// direction (a mask that does not list labels must leave the flag down) is
// TestUpdateSkipsFieldsOutsideTheMask in update_fieldviolation_test.go; without
// it "always armed" would satisfy this file.

import (
	"context"
	"testing"

	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/volume"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	"github.com/PRO-Robotech/kacho/services/storage/internal/repo/repomock"
)

func TestUpdateArmsLabelReEmitOnEveryRemovalPath(t *testing.T) {
	cases := []struct {
		name   string
		mask   []string
		labels map[string]string
	}{
		{
			name:   "cleared to nothing under an explicit labels mask",
			mask:   []string{"labels"},
			labels: map[string]string{},
		},
		{
			name:   "one key dropped under an explicit labels mask",
			mask:   []string{"labels"},
			labels: map[string]string{"keep": "yes"},
		},
		{
			name:   "the whole set replaced under an explicit labels mask",
			mask:   []string{"labels"},
			labels: map[string]string{"tier": "seld", "keep": "yes"},
		},
		{
			// An empty update_mask is a full-object PATCH: every mutable field of
			// the body is applied, labels included. A removal made this way is a
			// removal like any other and has to reach the mirror.
			name:   "cleared to nothing under an empty mask (full-object PATCH)",
			mask:   nil,
			labels: map[string]string{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var applied volume.VolumeUpdate
			writer := &repomock.VolumeWriter{
				UpdateFunc: func(_ context.Context, _ string, u volume.VolumeUpdate) (*domain.Volume, error) {
					applied = u
					return &domain.Volume{ID: volUpdID}, nil
				},
			}
			ops := repomock.NewOpsRepo()
			uc := volume.New(&repomock.VolumeReader{}, writer,
				&repomock.PeerClient{}, &repomock.PeerClient{}, ops, serviceerr.ToStatus).WithInstallPrefix(testInstallPrefix)

			op, err := uc.Update(context.Background(), volUpdID, tc.mask, "", "", tc.labels, 0)
			if err != nil {
				t.Fatalf("Update mask=%v labels=%v: %v", tc.mask, tc.labels, err)
			}
			if done := repomock.AwaitOpDone(t, ops, op.ID); done.Error != nil {
				t.Fatalf("Update mask=%v failed asynchronously: %v", tc.mask, done.Error)
			}
			if !applied.LabelsSet {
				t.Fatalf("LabelsSet = false for mask=%v labels=%v — the writer will not re-tell the "+
					"authority what the labels are now, so access granted through the removed label survives it",
					tc.mask, tc.labels)
			}
			if len(applied.Labels) != len(tc.labels) {
				t.Fatalf("labels applied = %v, want %v", applied.Labels, tc.labels)
			}
		})
	}
}
