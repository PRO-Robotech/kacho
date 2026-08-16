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

	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/check"
)

// vpcProtoPackage — proto-пакет vpc. Не все объявленные в нём сервисы поднимаются
// в рантайме, поэтому «весь пакет» ≠ «выставленная поверхность»: нужен явный
// served-список (ниже) + проверка, что классификация пакета исчерпывающа.
const vpcProtoPackage = protoreflect.FullName("kacho.cloud.vpc.v1")

// servedPublicServiceDescs — gRPC-сервисы на public-листенере (:9090).
// Источник истины — composition root `cmd/vpc/main.go` (RegisterXxxServer…).
//
// ВАЖНО: регистрация ServiceServer поднимает ВЕСЬ ServiceDesc, включая RPC,
// которые handler не переопределил и унаследовал из `UnimplementedXxxServer`.
// Такие RPC остаются выставленными (grpc-go их диспатчит) и проходят через тот же
// authz-интерсептор → они обязаны быть в PermissionMap.
var servedPublicServiceDescs = []grpc.ServiceDesc{
	vpcv1.NetworkService_ServiceDesc,
	vpcv1.SubnetService_ServiceDesc,
	vpcv1.AddressService_ServiceDesc,
	vpcv1.RouteTableService_ServiceDesc,
	vpcv1.SecurityGroupService_ServiceDesc,
	vpcv1.GatewayService_ServiceDesc,
	vpcv1.NetworkInterfaceService_ServiceDesc,
	vpcv1.CidrGroupService_ServiceDesc,
	// Чтение квот арендатором (#365). Публичный слушатель, только чтение:
	// величины назначает администратор облака на внутреннем слушателе iam.
	vpcv1.QuotaService_ServiceDesc,
	operationpb.OperationService_ServiceDesc,
}

// servedInternalServiceDescs — gRPC-сервисы cluster-internal листенера (:9091).
// Internal НЕ освобождён от authz (security.md «authN+authZ на ОБОИХ listener'ах»;
// «Internal = trusted» — запрещённое допущение), поэтому их RPC тоже обязаны быть
// в карте — с Relation либо с явным Public=true.
var servedInternalServiceDescs = []grpc.ServiceDesc{
	vpcv1.InternalAddressService_ServiceDesc,
	vpcv1.InternalAddressPoolService_ServiceDesc,
	vpcv1.InternalNetworkService_ServiceDesc,
	vpcv1.InternalNetworkInterfaceService_ServiceDesc,
}

// notServedServiceNames — сервисы `kacho.cloud.vpc.v1`, объявленные в proto, но НЕ
// поднятые ни на одном листенере (нет RegisterXxxServer в composition root). Их RPC
// недостижимы, поэтому записи в PermissionMap им не нужны.
//
// Список — вторая половина исчерпывающей классификации: без него сервис, поднятый в
// main.go, но не добавленный в served-списки выше, прошёл бы coverage-тест мимо, и
// дыра «RPC выставлен, но не в карте» вернулась бы бесшумно.
// СЕЙЧАС ПУСТ: обе записи сняты с контракта целиком — это были объявления без
// единой реализации (живой фид жизненного цикла остался только в loadbalancer.v1,
// живой поток событий — только в compute.v1). Пустая карта означает, что каждый
// объявленный vpc/v1 сервис обслуживается и потому обязан быть в PermissionMap.
var notServedServiceNames = map[string]struct{}{}

func servedServiceDescs() []grpc.ServiceDesc {
	out := make([]grpc.ServiceDesc, 0, len(servedPublicServiceDescs)+len(servedInternalServiceDescs))
	out = append(out, servedPublicServiceDescs...)
	out = append(out, servedInternalServiceDescs...)
	return out
}

func vpcFullMethod(sd grpc.ServiceDesc, method string) string {
	return "/" + sd.ServiceName + "/" + method
}

// TestPermissionMap_CoversEveryServedVPCRPC — гейт КЛАССА gateway-catalog ↔
// backend-PermissionMap drift (security.md инв. 4), портирован из storage.
//
// corelib authz-интерсептор fail-closed: RPC без записи в карте → DecisionUnmapped →
// `PermissionDenied "permission denied (rpc not mapped)"` на КАЖДЫЙ вызов, независимо
// от грантов и mTLS. Класс уже стрелял в storage (ImageService) и compute (12 RPC
// *AccessBindings) — тест не даёт ему воспроизвестись здесь.
func TestPermissionMap_CoversEveryServedVPCRPC(t *testing.T) {
	m := check.PermissionMap()

	var missing []string
	for _, sd := range servedServiceDescs() {
		for _, mi := range sd.Methods {
			fm := vpcFullMethod(sd, mi.MethodName)
			if _, ok := m[fm]; !ok {
				missing = append(missing, fm)
			}
		}
		for _, si := range sd.Streams {
			fm := vpcFullMethod(sd, si.StreamName)
			if _, ok := m[fm]; !ok {
				missing = append(missing, fm)
			}
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

// TestPermissionMap_VPCServedEntriesWellFormed — запись мало «иметь»: интерсептор
// разыменовывает Extract и Check'ает по Relation. Полузаполненная запись роняет RPC
// на nil-deref в рантайме вместо честного отказа.
func TestPermissionMap_VPCServedEntriesWellFormed(t *testing.T) {
	m := check.PermissionMap()

	for _, sd := range servedServiceDescs() {
		for _, mi := range sd.Methods {
			fm := vpcFullMethod(sd, mi.MethodName)
			entry, ok := m[fm]
			if !ok {
				continue // покрыто TestPermissionMap_CoversEveryServedVPCRPC
			}
			if entry.Public {
				require.Emptyf(t, entry.Relation, "%s: Public entry must not carry a Relation", fm)
				require.Nilf(t, entry.Extract, "%s: Public entry must not carry an Extract", fm)
				continue
			}
			if entry.ScopeFiltered {
				continue // авторизуется на data-уровне (per-page BatchCheck), single-object Check не делается
			}
			require.NotEmptyf(t, entry.Relation, "%s: required relation must be set", fm)
			require.NotNilf(t, entry.Extract, "%s: must carry an ObjectExtractor", fm)
		}
	}
}

// TestPermissionMap_EveryVPCServiceClassified — исчерпывающая классификация пакета:
// каждый сервис `kacho.cloud.vpc.v1` либо в served-списках, либо в
// notServedServiceNames.
func TestPermissionMap_EveryVPCServiceClassified(t *testing.T) {
	served := make(map[string]struct{})
	for _, sd := range servedServiceDescs() {
		served[sd.ServiceName] = struct{}{}
	}

	var unclassified []string
	var declared int
	declaredNames := make(map[string]struct{})
	protoregistry.GlobalFiles.RangeFilesByPackage(vpcProtoPackage, func(fd protoreflect.FileDescriptor) bool {
		svcs := fd.Services()
		for i := 0; i < svcs.Len(); i++ {
			name := string(svcs.Get(i).FullName())
			declared++
			declaredNames[name] = struct{}{}
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

	require.NotZero(t, declared, "vpc/v1 descriptors must be linked into protoregistry")
	sort.Strings(unclassified)
	require.Emptyf(t, unclassified,
		"vpc/v1 service(s) not classified: %v\n"+
			"Add each to servedPublicServiceDescs/servedInternalServiceDescs (if cmd/vpc/main.go "+
			"registers it — then every RPC needs a PermissionMap entry) or to notServedServiceNames.",
		unclassified)

	// Вторая половина исчерпывающей классификации: запись, которой больше нечего
	// исключать, — тоже находка. Перечень, не истекающий сам, передаёт слепое
	// пятно следующему сервису, которому достанется это имя (у compute такая
	// запись пережила снятие своего сервиса с контракта).
	var orphanNotServed []string
	for name := range notServedServiceNames {
		if _, ok := declaredNames[name]; !ok {
			orphanNotServed = append(orphanNotServed, name)
		}
	}
	sort.Strings(orphanNotServed)
	require.Emptyf(t, orphanNotServed,
		"notServedServiceNames names %d service(s) that vpc/v1 no longer declares: %v\n"+
			"An exclusion lives only while it has a subject — drop the entry.",
		len(orphanNotServed), orphanNotServed)
}
