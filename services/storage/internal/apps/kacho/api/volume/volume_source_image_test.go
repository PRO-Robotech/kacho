// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package volume_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/volume"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	"github.com/PRO-Robotech/kacho/services/storage/internal/repo/repomock"
)

// A boot volume resolves the region of its zone from the owner of Geography, and
// a mutation whose precondition cannot be established fails closed rather than
// proceeding with an unknown region.
func TestCreateBootVolume_ZoneRegionUnavailable_FailsClosed(t *testing.T) {
	geo := &repomock.PeerClient{
		EnsureZoneFunc: func(context.Context, string) error { return nil },
		RegionOfZoneFunc: func(context.Context, string) (string, error) {
			return "", status.Error(codes.Unavailable, "geo zone validation unavailable")
		},
	}
	uc := volume.New(
		&repomock.VolumeReader{},
		&repomock.VolumeWriter{InsertFunc: func(_ context.Context, v *domain.Volume) (*domain.Volume, error) {
			t.Fatal("writer must not be reached when the zone region is unresolved")
			return nil, nil
		}},
		geo,
		&repomock.PeerClient{EnsureProjectFunc: func(context.Context, string) error { return nil }},
		repomock.NewOpsRepo(),
		serviceerr.ToStatus,
	)

	_, err := uc.Create(context.Background(), &domain.Volume{
		ProjectID: "prj-1", Name: "boot-geo-down", ZoneID: "region-1-a",
		DiskTypeID: "block-balanced", SizeBytes: 1 << 30, SourceImage: "img00000000000000000",
	})
	require.Error(t, err)
	require.Equal(t, codes.Unavailable, status.Code(err))
}

// A source-less volume needs no region at all — geo is never asked for one.
func TestCreatePlainVolume_DoesNotResolveZoneRegion(t *testing.T) {
	asked := false
	geo := &repomock.PeerClient{
		EnsureZoneFunc: func(context.Context, string) error { return nil },
		RegionOfZoneFunc: func(context.Context, string) (string, error) {
			asked = true
			return "region-1", nil
		},
	}
	uc := volume.New(
		&repomock.VolumeReader{},
		&repomock.VolumeWriter{InsertFunc: func(_ context.Context, v *domain.Volume) (*domain.Volume, error) {
			return v, nil
		}},
		geo,
		&repomock.PeerClient{EnsureProjectFunc: func(context.Context, string) error { return nil }},
		repomock.NewOpsRepo(),
		serviceerr.ToStatus,
	)

	_, err := uc.Create(context.Background(), &domain.Volume{
		ProjectID: "prj-1", Name: "plain-vol", ZoneID: "region-1-a",
		DiskTypeID: "block-balanced", SizeBytes: 1 << 30,
	})
	require.NoError(t, err)
	require.False(t, asked, "no image source → no region to compare, geo must not be asked")
}

// TestCreateSourceMutualExclusion — STOR-1-19 (NET-NEW): Volume нельзя засеять
// одновременно из snapshot и image → sync InvalidArgument (domain mutual-exclusion,
// ДО peer-вызовов: geo/iam mocks с nil-func паникнули бы, если бы дошло).
func TestCreateSourceMutualExclusion(t *testing.T) {
	uc := volume.New(&repomock.VolumeReader{}, &repomock.VolumeWriter{},
		&repomock.PeerClient{}, &repomock.PeerClient{}, nil, serviceerr.ToStatus)
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
	uc := volume.New(&repomock.VolumeReader{}, &repomock.VolumeWriter{},
		&repomock.PeerClient{}, &repomock.PeerClient{}, nil, serviceerr.ToStatus)
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
