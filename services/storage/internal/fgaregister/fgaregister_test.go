// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package fgaregister_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/storage/internal/fgaregister"
)

// TestStorageVolumeTuple — owner-hierarchy tuple тома: subject project:<id>,
// relation project, object storage_volume:<id> (парити с iam permission-catalog
// scope_extractor {storage_volume, volume_id}).
func TestStorageVolumeTuple(t *testing.T) {
	tp := fgaregister.StorageVolume("prj-1", "vol-abc")
	require.Equal(t, "project:prj-1", tp.SubjectID)
	require.Equal(t, "project", tp.Relation)
	require.Equal(t, "storage_volume:vol-abc", tp.Object)
	require.True(t, tp.Valid())
}

// TestStorageSnapshotTuple — owner-hierarchy tuple снапшота: object
// storage_snapshot:<id> (scope_extractor {storage_snapshot, snapshot_id}).
func TestStorageSnapshotTuple(t *testing.T) {
	tp := fgaregister.StorageSnapshot("prj-2", "snp-xyz")
	require.Equal(t, "project:prj-2", tp.SubjectID)
	require.Equal(t, "project", tp.Relation)
	require.Equal(t, "storage_snapshot:snp-xyz", tp.Object)
	require.True(t, tp.Valid())
}

// TestPayloadEncodeDecodeRoundTrip — Payload (tuple + mirror-feed) сериализуется в
// JSONB и обратно без потери tuple/parent-project/labels.
func TestPayloadEncodeDecodeRoundTrip(t *testing.T) {
	p := fgaregister.Payload{
		Tuple:           fgaregister.StorageVolume("prj-1", "vol-1"),
		ParentProjectID: "prj-1",
		Labels:          map[string]string{"env": "prod"},
	}
	b, err := fgaregister.Encode(p)
	require.NoError(t, err)

	got, err := fgaregister.Decode(b)
	require.NoError(t, err)
	require.Equal(t, p.Tuple, got.Tuple)
	require.Equal(t, "prj-1", got.ParentProjectID)
	require.Equal(t, "prod", got.Labels["env"])
	require.True(t, got.Valid())
}

// TestTupleValid — неполный tuple невалиден (drainer-декодер отравляет такую строку).
func TestTupleValid(t *testing.T) {
	require.False(t, fgaregister.Tuple{}.Valid())
	require.False(t, fgaregister.Tuple{SubjectID: "project:p"}.Valid())
	require.True(t, fgaregister.Tuple{SubjectID: "project:p", Relation: "project", Object: "storage_volume:v"}.Valid())
}
