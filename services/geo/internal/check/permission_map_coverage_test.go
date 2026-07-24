// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package check_test

import (
	"fmt"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	"github.com/PRO-Robotech/kacho/services/geo/internal/check"
)

// geoProtoPackage — пакет, ВСЕ сервисы которого geo реально сервит
// (cmd/kacho-geo/serve.go: Region/Zone на public :9090, InternalRegion/InternalZone
// на internal :9091). Других сервисов в `kacho.cloud.geo.v1` нет, поэтому «весь
// пакет» == «вся выставленная поверхность» — descriptor-обход даёт исчерпывающий
// список без ручного перечня, который сам по себе дрейфует.
const geoProtoPackage = protoreflect.FullName("kacho.cloud.geo.v1")

// TestPermissionMap_CoversEveryServedGeoRPC — гейт КЛАССА gateway-catalog ↔
// backend-PermissionMap drift (security.md инв. 4), портирован из storage.
//
// corelib authz-интерсептор fail-closed: RPC без записи в карте → DecisionUnmapped →
// `PermissionDenied "permission denied (rpc not mapped)"` на КАЖДЫЙ вызов, независимо
// от грантов и mTLS. Класс уже стрелял в storage (ImageService) и compute (12 RPC
// *AccessBindings) — тест не даёт ему воспроизвестись здесь.
//
// Список RPC берётся из protobuf-дескрипторов (источник истины о выставленной
// поверхности), поэтому НОВЫЙ geo-RPC валит тест автоматически, пока не заведена
// запись в PermissionMap.
func TestPermissionMap_CoversEveryServedGeoRPC(t *testing.T) {
	m := check.PermissionMap()

	var methods []string
	protoregistry.GlobalFiles.RangeFilesByPackage(geoProtoPackage, func(fd protoreflect.FileDescriptor) bool {
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
	// OperationService живёт в отдельном пакете (kacho.cloud.operation), но
	// поднимается ОБОИМИ листенерами geo (serve.go) → тот же интерсептор, та же
	// обязанность иметь запись.
	for _, mi := range operationpb.OperationService_ServiceDesc.Methods {
		methods = append(methods, "/"+operationpb.OperationService_ServiceDesc.ServiceName+"/"+mi.MethodName)
	}

	require.NotEmpty(t, methods, "geo/v1 descriptors must be linked into protoregistry")

	var missing []string
	for _, fullMethod := range methods {
		if _, ok := m[fullMethod]; !ok {
			missing = append(missing, fullMethod)
		}
	}
	sort.Strings(missing)
	require.Emptyf(t, missing,
		"PermissionMap drift: %d served RPC(s) missing from permission_map.go.\n"+
			"corelib authz fail-closes each of them with \"permission denied (rpc not mapped)\" "+
			"for every caller.\nAdd an RPCEntry mirroring the gateway permission_catalog.json entry, "+
			"or `{Public: true}` for an explicit exempt.\nmissing: %v",
		len(missing), missing)
}

// TestPermissionMap_GeoEntriesWellFormed — запись мало «иметь»: интерсептор
// разыменовывает Extract и Check'ает по Relation. Полузаполненная запись роняет RPC
// на nil-deref в рантайме вместо честного отказа.
func TestPermissionMap_GeoEntriesWellFormed(t *testing.T) {
	for fm, entry := range check.PermissionMap() {
		if entry.Public || entry.ScopeFiltered {
			continue
		}
		require.NotEmptyf(t, entry.Relation, "%s: required relation must be set", fm)
		require.NotNilf(t, entry.Extract, "%s: must carry an ObjectExtractor", fm)
	}
}
