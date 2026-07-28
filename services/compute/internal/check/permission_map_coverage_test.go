// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package check_test

import (
	"fmt"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	computev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"
	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"

	"github.com/PRO-Robotech/kacho/services/compute/internal/check"
)

// computeProtoPackage — proto-пакет compute. В отличие от storage (где ВСЕ сервисы
// пакета сервятся) compute объявляет в proto заметно больше сервисов, чем поднимает
// в рантайме, поэтому «весь пакет» ≠ «выставленная поверхность» и одного
// descriptor-обхода мало: нужен явный served-список (ниже) + проверка, что
// классификация пакета исчерпывающа.
const computeProtoPackage = protoreflect.FullName("kacho.cloud.compute.v1")

// servedPublicServiceDescs — gRPC-сервисы, зарегистрированные на public-листенере
// (:9090). Источник истины — composition root `cmd/compute/main.go` (RegisterXxx…).
//
// ВАЖНО: регистрация ServiceServer поднимает ВЕСЬ ServiceDesc, включая RPC, которые
// handler не переопределил и унаследовал из `UnimplementedXxxServer`. Такие RPC
// ОСТАЮТСЯ выставленными (grpc-go их диспатчит) и потому проходят через тот же
// authz-интерсептор → они обязаны быть в PermissionMap.
var servedPublicServiceDescs = []grpc.ServiceDesc{
	computev1.DiskTypeService_ServiceDesc,
	computev1.MachineTypeService_ServiceDesc,
	computev1.InstanceService_ServiceDesc,
	operationpb.OperationService_ServiceDesc,
}

// servedInternalServiceDescs — gRPC-сервисы cluster-internal листенера (:9091).
// Internal НЕ освобождён от authz (security.md «authN+authZ на ОБОИХ listener'ах»;
// «Internal = trusted» — запрещённое допущение), поэтому их RPC тоже обязаны быть
// в карте — с Relation либо с явным Public=true.
var servedInternalServiceDescs = []grpc.ServiceDesc{
	computev1.InternalWatchService_ServiceDesc,
	computev1.InternalDiskTypeService_ServiceDesc,
	computev1.InternalMachineTypeService_ServiceDesc,
}

// notServedServiceNames — сервисы `kacho.cloud.compute.v1`, объявленные в proto, но
// НЕ поднятые ни на одном листенере (нет RegisterXxxServer в composition root).
// Их RPC недостижимы, поэтому записи в PermissionMap им не нужны.
//
// Список нужен не для документации, а как вторая половина исчерпывающей
// классификации: TestPermissionMap_EveryComputeServiceClassified падает, если в
// proto появился сервис, не попавший ни сюда, ни в served-списки, — иначе
// «забыли зарегистрировать сервис в тесте» бесшумно вернуло бы ту самую дыру,
// которую этот файл закрывает.
var notServedServiceNames = map[string]struct{}{
	"kacho.cloud.compute.v1.InternalResourceLifecycleService": {},
	"kacho.cloud.compute.v1.MaintenanceService":               {},
}

func servedServiceDescs() []grpc.ServiceDesc {
	out := make([]grpc.ServiceDesc, 0, len(servedPublicServiceDescs)+len(servedInternalServiceDescs))
	out = append(out, servedPublicServiceDescs...)
	out = append(out, servedInternalServiceDescs...)
	return out
}

func computeFullMethod(sd grpc.ServiceDesc, method string) string {
	return "/" + sd.ServiceName + "/" + method
}

// TestPermissionMap_CoversEveryServedComputeRPC — гейт КЛАССА gateway-catalog ↔
// backend-PermissionMap drift (security.md инв. 4 «permission-catalog полон и в
// синхроне»), а не одного экземпляра. Портирован из storage, где такой гейт был
// единственным в репозитории.
//
// corelib authz-интерсептор fail-closed: RPC без записи в карте → DecisionUnmapped →
// `PermissionDenied "permission denied (rpc not mapped)"` на КАЖДЫЙ вызов, независимо
// от грантов и mTLS. Класс уже стрелял в storage (публичный ImageService, 108 newman-
// падений) и в compute (12 RPC {Disk,Image,Instance,Snapshot}Service/*AccessBindings:
// зарегистрированы, аннотированы в permission_catalog.json, в backend-карте — нет →
// вместо честного Unimplemented каждый вызов получал ложный 403 «rpc not mapped»).
//
// Список RPC берётся из proto-generated ServiceDesc (источник истины о выставленной
// поверхности), поэтому НОВЫЙ compute-RPC валит тест автоматически, пока не заведена
// запись в PermissionMap — ручной перечень методов поддерживать не нужно.
func TestPermissionMap_CoversEveryServedComputeRPC(t *testing.T) {
	m := check.PermissionMap()

	var missing []string
	for _, sd := range servedServiceDescs() {
		for _, mi := range sd.Methods {
			fm := computeFullMethod(sd, mi.MethodName)
			if _, ok := m[fm]; !ok {
				missing = append(missing, fm)
			}
		}
		for _, si := range sd.Streams {
			fm := computeFullMethod(sd, si.StreamName)
			if _, ok := m[fm]; !ok {
				missing = append(missing, fm)
			}
		}
	}
	sort.Strings(missing)

	require.Emptyf(t, missing,
		"PermissionMap drift: %d served RPC(s) missing from permission_map.go.\n"+
			"corelib authz fail-closes each of them with \"permission denied (rpc not mapped)\" "+
			"for every caller — including callers who legitimately hold the grant.\n"+
			"Add an RPCEntry (Relation/Permission/Extract) mirroring the gateway "+
			"permission_catalog.json entry, or `{Public: true}` for an explicit exempt.\nmissing: %v",
		len(missing), missing)
}

// TestPermissionMap_ServedEntriesWellFormed — запись мало «иметь»: интерсептор
// разыменовывает Extract и Check'ает по Relation. Полузаполненная запись
// (Relation без Extract) роняет RPC в рантайме на nil-deref, а не на честный отказ.
func TestPermissionMap_ServedEntriesWellFormed(t *testing.T) {
	m := check.PermissionMap()

	for _, sd := range servedServiceDescs() {
		for _, mi := range sd.Methods {
			fm := computeFullMethod(sd, mi.MethodName)
			entry, ok := m[fm]
			if !ok {
				continue // покрыто TestPermissionMap_CoversEveryServedComputeRPC
			}
			if entry.Public {
				require.Emptyf(t, entry.Relation, "%s: Public entry must not carry a Relation", fm)
				require.Nilf(t, entry.Extract, "%s: Public entry must not carry an Extract", fm)
				continue
			}
			if entry.ScopeFiltered {
				continue // авторизуется на data-уровне (ListObjects), single-object Check не делается
			}
			require.NotEmptyf(t, entry.Relation, "%s: required relation must be set", fm)
			require.NotNilf(t, entry.Extract, "%s: must carry an ObjectExtractor", fm)
		}
	}
}

// TestPermissionMap_EveryComputeServiceClassified — исчерпывающая классификация
// пакета: каждый сервис `kacho.cloud.compute.v1` либо в served-списках, либо в
// notServedServiceNames. Без этого гейта новый сервис, поднятый в main.go, но не
// добавленный в served-список выше, прошёл бы coverage-тест мимо — и дыра
// «RPC выставлен, но не в карте» вернулась бы бесшумно.
func TestPermissionMap_EveryComputeServiceClassified(t *testing.T) {
	served := make(map[string]struct{})
	for _, sd := range servedServiceDescs() {
		served[sd.ServiceName] = struct{}{}
	}

	var unclassified []string
	var declared int
	protoregistry.GlobalFiles.RangeFilesByPackage(computeProtoPackage, func(fd protoreflect.FileDescriptor) bool {
		svcs := fd.Services()
		for i := 0; i < svcs.Len(); i++ {
			name := string(svcs.Get(i).FullName())
			declared++
			_, isServed := served[name]
			_, isNotServed := notServedServiceNames[name]
			if isServed && isNotServed {
				unclassified = append(unclassified, fmt.Sprintf("%s (in BOTH served and notServed lists)", name))
				continue
			}
			if !isServed && !isNotServed {
				unclassified = append(unclassified, name)
			}
		}
		return true
	})

	require.NotZero(t, declared, "compute/v1 descriptors must be linked into protoregistry")
	sort.Strings(unclassified)
	require.Emptyf(t, unclassified,
		"compute/v1 service(s) not classified: %v\n"+
			"Add each to servedPublicServiceDescs/servedInternalServiceDescs (if cmd/compute/main.go "+
			"registers it — then every RPC needs a PermissionMap entry) or to notServedServiceNames.",
		unclassified)
}
