// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package image_test

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	"github.com/PRO-Robotech/kacho/services/storage/internal/ports/portmock"
	"github.com/PRO-Robotech/kacho/services/storage/internal/service/image"
	"github.com/PRO-Robotech/kacho/services/storage/internal/serviceerr"
)

// TestCreateBVADescriptionLabels — regression against
// https://github.com/PRO-Robotech/kacho/issues/61.
//
// Image.Create must SYNC-reject over-limit description (>256) and labels (>64) with
// INVALID_ARGUMENT — parity with VolumeService (BVA at the request edge, BEFORE any
// peer/DB call). Previously Image.Create accepted them and returned a 200 Operation.
// A valid source is supplied so the reject is provably the BVA, not "source required".
// Sync-reject happens before peer calls (geo/iam mocks with nil funcs would panic).
func TestCreateBVADescriptionLabels(t *testing.T) {
	uc := image.New(&portmock.ImageReader{}, &portmock.ImageWriter{}, &portmock.PeerClient{}, &portmock.PeerClient{}, nil, serviceerr.ToStatus)

	base := func() *domain.Image {
		return &domain.Image{
			ProjectID: "prj-1", RegionID: "ru-central1", Name: "bva",
			SourceVolume: "vol00000000000000000",
		}
	}

	// description = 257 (>256) → InvalidArgument.
	img := base()
	img.Description = strings.Repeat("x", 257)
	if _, err := uc.Create(context.Background(), img); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("desc=257 code = %v, want InvalidArgument", status.Code(err))
	}

	// labels = 65 pairs (>64) → InvalidArgument.
	img = base()
	labels := make(map[string]string, 65)
	for i := 0; i < 65; i++ {
		labels["k"+strings.Repeat("z", i)] = "v"
	}
	img.Labels = labels
	if _, err := uc.Create(context.Background(), img); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("labels=65 code = %v, want InvalidArgument", status.Code(err))
	}
}
