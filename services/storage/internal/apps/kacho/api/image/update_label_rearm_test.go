// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package image_test

// update_label_rearm_test.go — the switch that arms the label re-emit.
//
// See the volume file of the same name for the full reasoning. In short: access
// handed out through a label selector is taken back by removing the label, and
// that only works because an Update carrying labels re-tells the authority what
// the labels are now. The whole mechanism hangs off one derived flag,
// ImageUpdate.LabelsSet — the writer re-emits when it is set and stays silent
// when it is not. Every test of the re-emit sets that flag by hand, so the
// derivation itself was unlocked, and the path that would go quiet first is the
// full-object PATCH (an empty update_mask names no field).
//
// The control for the other direction — a mask that does not list labels must
// leave the flag down — is TestUpdateSkipsFieldsOutsideTheMask in
// update_fieldviolation_test.go; without it "always armed" would satisfy this file.

import (
	"context"
	"testing"

	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/image"
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
			name:   "cleared to nothing under an empty mask (full-object PATCH)",
			mask:   nil,
			labels: map[string]string{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var applied image.ImageUpdate
			writer := &repomock.ImageWriter{
				UpdateFunc: func(_ context.Context, _ string, u image.ImageUpdate) (*domain.Image, error) {
					applied = u
					return &domain.Image{ID: imgUpdID}, nil
				},
			}
			ops := repomock.NewOpsRepo()
			uc := image.New(&repomock.ImageReader{}, writer,
				&repomock.PeerClient{}, &repomock.PeerClient{}, ops, serviceerr.ToStatus)

			op, err := uc.Update(context.Background(), imgUpdID, tc.mask, "", "", tc.labels)
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
