// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package check_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	storagev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/storage/v1"
	"github.com/PRO-Robotech/kacho/services/storage/internal/check"
)

// TestPermissionMap_ObjectAndProjectScope — regression against
// https://github.com/PRO-Robotech/kacho/issues/62.
//
// The storage in-service authz floor gated EVERY tenant RPC on the cluster
// singleton (`cluster:cluster_kacho_root`) via a static extractor, so a
// project-scoped `editor` principal — который HAS `project:<p>#viewer/editor` but no
// cluster-level grant — was denied 403 on List/Get/Create/Update/Delete of its OWN
// project's resources (the gateway already ALLOWED them at project/object scope).
// The bootstrap (cluster system_admin) suites masked it via the iam cluster-admin
// short-circuit.
//
// The fix mirrors the api-gateway permission-catalog (defense-in-depth: both
// listeners resolve the SAME scope object): List/Create → `project:<project_id>`;
// object-self Get/Update/Delete/ListOperations → `storage_<res>:<res_id>`. The
// tier relation (viewer/editor) is unchanged — only the scope object moves off the
// cluster singleton onto the request's project / target object.
//
// DiskType (global read-only catalog) and InternalDiskType (admin CRUD) legitimately
// stay on the cluster singleton and are asserted so a future edit does not
// accidentally object-scope a catalog RPC.
func TestPermissionMap_ObjectAndProjectScope(t *testing.T) {
	m := check.PermissionMap()

	type want struct {
		objType string
		objID   string
	}
	cases := map[string]struct {
		req  any
		want want
	}{
		// ---- VolumeService: List/Create → project; object-self → storage_volume ----
		"/kacho.cloud.storage.v1.VolumeService/List": {
			req: &storagev1.ListVolumesRequest{ProjectId: "prj_a"}, want: want{"project", "prj_a"},
		},
		"/kacho.cloud.storage.v1.VolumeService/Create": {
			req: &storagev1.CreateVolumeRequest{ProjectId: "prj_a"}, want: want{"project", "prj_a"},
		},
		"/kacho.cloud.storage.v1.VolumeService/Get": {
			req: &storagev1.GetVolumeRequest{VolumeId: "vol_1"}, want: want{"storage_volume", "vol_1"},
		},
		"/kacho.cloud.storage.v1.VolumeService/Update": {
			req: &storagev1.UpdateVolumeRequest{VolumeId: "vol_1"}, want: want{"storage_volume", "vol_1"},
		},
		"/kacho.cloud.storage.v1.VolumeService/Delete": {
			req: &storagev1.DeleteVolumeRequest{VolumeId: "vol_1"}, want: want{"storage_volume", "vol_1"},
		},
		"/kacho.cloud.storage.v1.VolumeService/ListOperations": {
			req: &storagev1.ListVolumeOperationsRequest{VolumeId: "vol_1"}, want: want{"storage_volume", "vol_1"},
		},

		// ---- SnapshotService ----
		"/kacho.cloud.storage.v1.SnapshotService/List": {
			req: &storagev1.ListSnapshotsRequest{ProjectId: "prj_a"}, want: want{"project", "prj_a"},
		},
		"/kacho.cloud.storage.v1.SnapshotService/Create": {
			req: &storagev1.CreateSnapshotRequest{ProjectId: "prj_a"}, want: want{"project", "prj_a"},
		},
		"/kacho.cloud.storage.v1.SnapshotService/Get": {
			req: &storagev1.GetSnapshotRequest{SnapshotId: "snp_1"}, want: want{"storage_snapshot", "snp_1"},
		},
		"/kacho.cloud.storage.v1.SnapshotService/Update": {
			req: &storagev1.UpdateSnapshotRequest{SnapshotId: "snp_1"}, want: want{"storage_snapshot", "snp_1"},
		},
		"/kacho.cloud.storage.v1.SnapshotService/Delete": {
			req: &storagev1.DeleteSnapshotRequest{SnapshotId: "snp_1"}, want: want{"storage_snapshot", "snp_1"},
		},

		// ---- ImageService ----
		"/kacho.cloud.storage.v1.ImageService/List": {
			req: &storagev1.ListImagesRequest{ProjectId: "prj_a"}, want: want{"project", "prj_a"},
		},
		"/kacho.cloud.storage.v1.ImageService/Create": {
			req: &storagev1.CreateImageRequest{ProjectId: "prj_a"}, want: want{"project", "prj_a"},
		},
		"/kacho.cloud.storage.v1.ImageService/Get": {
			req: &storagev1.GetImageRequest{ImageId: "img_1"}, want: want{"storage_image", "img_1"},
		},
		"/kacho.cloud.storage.v1.ImageService/Update": {
			req: &storagev1.UpdateImageRequest{ImageId: "img_1"}, want: want{"storage_image", "img_1"},
		},
		"/kacho.cloud.storage.v1.ImageService/Delete": {
			req: &storagev1.DeleteImageRequest{ImageId: "img_1"}, want: want{"storage_image", "img_1"},
		},
		"/kacho.cloud.storage.v1.ImageService/ListOperations": {
			req: &storagev1.ListImageOperationsRequest{ImageId: "img_1"}, want: want{"storage_image", "img_1"},
		},

		// ---- InternalVolumeService (:9091 attach-coordination) — object-scoped ----
		"/kacho.cloud.storage.v1.InternalVolumeService/Attach": {
			req: &storagev1.AttachVolumeRequest{VolumeId: "vol_1"}, want: want{"storage_volume", "vol_1"},
		},
		"/kacho.cloud.storage.v1.InternalVolumeService/Detach": {
			req: &storagev1.DetachVolumeRequest{VolumeId: "vol_1"}, want: want{"storage_volume", "vol_1"},
		},
		"/kacho.cloud.storage.v1.InternalVolumeService/GetInternal": {
			req: &storagev1.GetInternalVolumeRequest{VolumeId: "vol_1"}, want: want{"storage_volume", "vol_1"},
		},
	}

	for fullMethod, tc := range cases {
		entry, ok := m[fullMethod]
		require.Truef(t, ok, "%s must be present in PermissionMap", fullMethod)
		require.NotNilf(t, entry.Extract, "%s: must carry an ObjectExtractor", fullMethod)
		objType, objID, err := entry.Extract(tc.req)
		require.NoErrorf(t, err, "%s: extractor error", fullMethod)
		require.Equalf(t, tc.want.objType, objType,
			"%s: object_type must be %q (mirror api-gateway catalog), not cluster", fullMethod, tc.want.objType)
		require.Equalf(t, tc.want.objID, objID, "%s: object_id extraction", fullMethod)
	}
}

// TestPermissionMap_CatalogRPCsStayClusterScoped — DiskType (global read-only
// catalog) + InternalDiskType (admin CRUD) + InternalVolume.ListAttachments
// legitimately resolve on the cluster singleton; lock it so the #62 object-scoping
// does not accidentally spread onto a catalog/cluster-wide RPC.
func TestPermissionMap_CatalogRPCsStayClusterScoped(t *testing.T) {
	m := check.PermissionMap()
	for _, fullMethod := range []string{
		"/kacho.cloud.storage.v1.DiskTypeService/Get",
		"/kacho.cloud.storage.v1.DiskTypeService/List",
		"/kacho.cloud.storage.v1.InternalDiskTypeService/Create",
		"/kacho.cloud.storage.v1.InternalVolumeService/ListAttachments",
	} {
		entry, ok := m[fullMethod]
		require.Truef(t, ok, "%s must be present", fullMethod)
		require.NotNilf(t, entry.Extract, "%s: must carry an extractor", fullMethod)
		objType, objID, err := entry.Extract(struct{}{})
		require.NoErrorf(t, err, "%s: static cluster extractor never errors", fullMethod)
		require.Equalf(t, "cluster", objType, "%s: must stay cluster-scoped", fullMethod)
		require.Equalf(t, "cluster_kacho_root", objID, "%s: cluster singleton", fullMethod)
	}
}
