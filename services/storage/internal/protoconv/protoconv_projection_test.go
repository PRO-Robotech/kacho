// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package protoconv_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	"github.com/PRO-Robotech/kacho/services/storage/internal/protoconv"
)

// infraTokens — инфра-чувствительные поля (security.md), которых публичная проекция
// Volume/Image НЕСТИ НЕ ДОЛЖНА (они живут только в internal :9091 reserved-проекции).
var infraTokens = []string{
	"backend", "lun", "nvme", "namespace", "storagenode",
	"poolid", "numericinfra", "bloblayout",
	"bucket", "enginenamespace",
}

// assertNoInfraFields перечисляет поля дескриптора proto-сообщения и требует, чтобы
// ни одно имя не содержало инфра-токен (field-absence, two-projection).
func assertNoInfraFields(t *testing.T, resource string, fields protoreflect.FieldDescriptors) {
	t.Helper()
	for idx := 0; idx < fields.Len(); idx++ {
		name := strings.ToLower(strings.ReplaceAll(string(fields.Get(idx).Name()), "_", ""))
		for _, tok := range infraTokens {
			require.NotContains(t, name, tok,
				"public %s must not carry infra field %q (token %q)", resource, fields.Get(idx).Name(), tok)
		}
	}
}

// TestImagePublicProjectionNoInfra — STOR-1-25: public Image НЕ несёт infra/blob-layout
// (field-absence, two-projection security-инвариант, НЕ gated).
func TestImagePublicProjectionNoInfra(t *testing.T) {
	i := protoconv.Image(&domain.Image{
		ID: "img00000000000000000", ProjectID: "prj-1", Name: "ubuntu",
		RegionID: "ru-central1", Placement: domain.ImagePlacementRegional,
		SourceSnapshot: "snp00000000000000000", SizeBytes: 1 << 30, MinDiskBytes: 1 << 30,
		Format: domain.ImageFormatStandard, Status: domain.ImageStatusReady,
	})
	assertNoInfraFields(t, "Image", i.ProtoReflect().Descriptor().Fields())
	require.Equal(t, "ru-central1", i.GetRegionId())
}

// TestVolumePublicProjectionNoInfra — STOR-1-16: public Volume НЕ несёт infra-токенов;
// source_image_id прокинут (delta F9).
func TestVolumePublicProjectionNoInfra(t *testing.T) {
	v := protoconv.Volume(&domain.Volume{
		ID: "vol00000000000000000", ProjectID: "prj-1", Name: "data",
		ZoneID: "region-1-a", DiskTypeID: "block-balanced", SizeBytes: 1 << 30,
		SourceImage: "img00000000000000000", Status: domain.VolumeStatusAvailable,
	})
	assertNoInfraFields(t, "Volume", v.ProtoReflect().Descriptor().Fields())
	require.Equal(t, "img00000000000000000", v.GetSourceImageId())
}
