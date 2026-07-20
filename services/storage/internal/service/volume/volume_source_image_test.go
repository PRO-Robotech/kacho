// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package volume_test

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	"github.com/PRO-Robotech/kacho/services/storage/internal/ports/portmock"
	"github.com/PRO-Robotech/kacho/services/storage/internal/service/volume"
	"github.com/PRO-Robotech/kacho/services/storage/internal/serviceerr"
)

// TestCreateSourceMutualExclusion — STOR-1-19 (NET-NEW): Volume нельзя засеять
// одновременно из snapshot и image → sync InvalidArgument (domain mutual-exclusion,
// ДО peer-вызовов: geo/iam mocks с nil-func паникнули бы, если бы дошло).
func TestCreateSourceMutualExclusion(t *testing.T) {
	uc := volume.New(&portmock.VolumeReader{}, &portmock.VolumeWriter{},
		&portmock.PeerClient{}, &portmock.PeerClient{}, nil, serviceerr.ToStatus)
	v := &domain.Volume{
		ProjectID: "prj-1", ZoneID: "region-1-a", DiskTypeID: "block-balanced", SizeBytes: 1 << 30,
		SourceSnapshot: "snp00000000000000000", SourceImage: "img00000000000000000",
	}
	_, err := uc.Create(context.Background(), v)
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("both sources code = %v, want InvalidArgument", status.Code(err))
	}
}

// TestUpdateSourceImageImmutable — STOR-1-03/18: source_image_id (и attachments) в
// маске → sync InvalidArgument "<field> is immutable after Volume.Create".
func TestUpdateSourceImageImmutable(t *testing.T) {
	uc := volume.New(&portmock.VolumeReader{}, &portmock.VolumeWriter{},
		&portmock.PeerClient{}, &portmock.PeerClient{}, nil, serviceerr.ToStatus)
	for _, f := range []string{"source_image_id", "attachments"} {
		_, err := uc.Update(context.Background(), "vol00000000000000000", []string{f}, "", "", nil, 0)
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("Update mask=%s code=%v, want InvalidArgument", f, status.Code(err))
		}
		want := f + " is immutable after Volume.Create"
		if got := status.Convert(err).Message(); got != want {
			t.Fatalf("Update mask=%s message=%q, want %q", f, got, want)
		}
	}
}
