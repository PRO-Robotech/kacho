// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package check_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	storagev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/storage/v1"
	"github.com/PRO-Robotech/kacho/services/storage/internal/check"
)

// storageProtoPackage — пакет, ВСЕ сервисы которого storage реально сервит
// (serve.go/registerServices: Volume/Snapshot/Image/DiskType на :9090,
// InternalVolume/InternalImage/InternalDiskType на :9091). Других сервисов в
// `kacho.cloud.storage.v1` нет, поэтому «весь пакет» == «вся выставленная
// поверхность» — descriptor-обход даёт исчерпывающий список без ручного перечня,
// который сам по себе дрейфует.
const storageProtoPackage = protoreflect.FullName("kacho.cloud.storage.v1")

// TestPermissionMap_CoversEveryServedStorageRPC — гейт КЛАССА gateway-catalog ↔
// backend-PermissionMap drift (security.md инв. 4 «permission-catalog полон и в
// синхроне»), а не одного экземпляра.
//
// corelib authz-интерсептор fail-closed: RPC без записи в карте → DecisionUnmapped →
// `PermissionDenied "permission denied (rpc not mapped)"` на КАЖДЫЙ вызов, независимо
// от грантов и mTLS. Класс уже стрелял дважды: публичный ImageService (108 newman-
// падений) и InternalImageService/GetInternal (:9091 зарегистрирован в serve.go,
// запись есть в gateway permission_catalog.json, в backend-карте — нет).
//
// Тест перечисляет RPC из protobuf-дескрипторов (источник истины о выставленной
// поверхности), поэтому НОВЫЙ storage-RPC валит его автоматически, пока не заведена
// запись в PermissionMap — ручной список поддерживать не нужно.
func TestPermissionMap_CoversEveryServedStorageRPC(t *testing.T) {
	m := check.PermissionMap()

	var methods []string
	protoregistry.GlobalFiles.RangeFilesByPackage(storageProtoPackage, func(fd protoreflect.FileDescriptor) bool {
		svcs := fd.Services()
		for i := 0; i < svcs.Len(); i++ {
			svc := svcs.Get(i)
			mds := svc.Methods()
			for j := 0; j < mds.Len(); j++ {
				methods = append(methods, fmt.Sprintf("/%s/%s", svc.FullName(), mds.Get(j).Name()))
			}
		}
		return true
	})

	require.NotEmpty(t, methods, "storage/v1 descriptors must be linked into protoregistry")

	for _, fullMethod := range methods {
		entry, ok := m[fullMethod]
		require.Truef(t, ok,
			"%s is served but missing from PermissionMap: corelib authz fail-closes it with "+
				"\"permission denied (rpc not mapped)\" for every caller", fullMethod)
		require.NotEmptyf(t, entry.Permission, "%s: permission string must mirror the gateway catalog", fullMethod)

		// ScopeFiltered — авторизация ПЕРЕЕХАЛА на данные (per-object сужение
		// прочитанной страницы), а не исчезла: единичного объекта, про который можно
		// спросить заранее, у такого RPC нет. Поэтому relation/extractor у него не
		// просто «не заполнены» — их наличие означало бы возврат к единичному Check'у.
		// Требуем обратного явно, чтобы отсутствие полей нельзя было списать на
		// недосмотр, и запрещаем совмещение с Public (это разные исходы: exempt vs
		// «решает handler»).
		if entry.ScopeFiltered {
			require.Falsef(t, entry.Public, "%s: ScopeFiltered entry must not also be Public", fullMethod)
			require.Emptyf(t, entry.Relation,
				"%s: ScopeFiltered entry must not carry a Relation — the per-RPC Check is skipped, "+
					"so a relation here would document a question nobody asks", fullMethod)
			require.Nilf(t, entry.Extract,
				"%s: ScopeFiltered entry must not carry an ObjectExtractor", fullMethod)
			continue
		}

		require.NotEmptyf(t, entry.Relation, "%s: required_relation must be set", fullMethod)
		require.NotNilf(t, entry.Extract, "%s: must carry an ObjectExtractor", fullMethod)
	}
}

// TestPermissionMap_InternalImageGetInternal_MirrorsGatewayCatalog — точечный lock на
// наблюдаемое решение для InternalImageService/GetInternal: gateway-каталог несёт
// {viewer, storage_image, image_id, storage.images.getInternal}; оба листенера обязаны
// выносить ОДНО И ТО ЖЕ решение (defense-in-depth), иначе :9091 либо fail-closed'ит
// легитимный infra-read, либо (при расхождении relation) гейтит слабее gateway.
func TestPermissionMap_InternalImageGetInternal_MirrorsGatewayCatalog(t *testing.T) {
	const fullMethod = "/kacho.cloud.storage.v1.InternalImageService/GetInternal"

	entry, ok := check.PermissionMap()[fullMethod]
	require.Truef(t, ok, "%s must be present in PermissionMap", fullMethod)
	require.Equal(t, "viewer", entry.Relation, "required_relation must mirror the gateway catalog")
	require.Equal(t, "storage.images.getInternal", entry.Permission, "permission must mirror the gateway catalog")
	require.NotNil(t, entry.Extract, "must carry an ObjectExtractor")

	objType, objID, err := entry.Extract(&storagev1.GetInternalImageRequest{ImageId: "img_1"})
	require.NoError(t, err, "extractor error")
	require.Equal(t, "storage_image", objType, "object_type must mirror the gateway catalog")
	require.Equal(t, "img_1", objID, "object id must come from image_id")
}
