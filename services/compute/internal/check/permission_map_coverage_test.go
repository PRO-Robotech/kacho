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
	computev1.MachineTypeService_ServiceDesc,
	computev1.InstanceService_ServiceDesc,
	computev1.GuestAccessKeyService_ServiceDesc,
	computev1.PlacementGroupService_ServiceDesc,
	// Чтение квот арендатором. Публичная поверхность и ТОЛЬКО чтение: величины
	// назначает администратор облака на внутреннем слушателе владельца величин.
	computev1.QuotaService_ServiceDesc,
	operationpb.OperationService_ServiceDesc,
}

// servedInternalServiceDescs — gRPC-сервисы cluster-internal листенера (:9091).
// Internal НЕ освобождён от authz (security.md «authN+authZ на ОБОИХ listener'ах»;
// «Internal = trusted» — запрещённое допущение), поэтому их RPC тоже обязаны быть
// в карте — с Relation либо с явным Public=true.
var servedInternalServiceDescs = []grpc.ServiceDesc{
	computev1.InternalMachineTypeService_ServiceDesc,
	computev1.InternalRealizationService_ServiceDesc,
	computev1.InternalNodeOwnershipService_ServiceDesc,
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
// СЕЙЧАС ПУСТ: единственная запись — InternalResourceLifecycleService — снята с
// контракта целиком (объявление без единой реализации; живой фид остался только в
// loadbalancer.v1). Пустая карта означает, что каждый объявленный compute/v1
// сервис обслуживается и потому обязан быть в PermissionMap.
var notServedServiceNames = map[string]struct{}{}

func servedServiceDescs() []grpc.ServiceDesc {
	out := make([]grpc.ServiceDesc, 0, len(servedPublicServiceDescs)+len(servedInternalServiceDescs))
	out = append(out, servedPublicServiceDescs...)
	out = append(out, servedInternalServiceDescs...)
	return out
}

func computeFullMethod(sd grpc.ServiceDesc, method string) string {
	return "/" + sd.ServiceName + "/" + method
}

// servedFullMethods — ЕДИНАЯ перепись выставленной RPC-поверхности: unary
// (`sd.Methods`) И стримы (`sd.Streams`). Обе половины гейта — «карта покрывает
// каждый RPC» и «запись правильной формы» — обязаны обходить ОДИН И ТОТ ЖЕ
// набор. Разойдясь, они дают ровно тот дефект, ради которого гейт заведён:
// половина «покрытие» видит стримовый RPC и рапортует запись присутствующей, а
// половина «форма» стримы не обходит — полузаполненная запись для стрима ей
// невидима и роняет RPC в рантайме на nil-deref вместо честного отказа. Общая
// перепись делает это расхождение невозможным by construction, а не чинит один
// его экземпляр.
//
// grpc-go диспатчит стрим через тот же authz-интерсептор (`authzIntr.Stream()` в
// composition root), поэтому запись для стрима читается интерсептором ровно так
// же, как для unary — разного обращения они не заслуживают.
func servedFullMethods() []string {
	var out []string
	for _, sd := range servedServiceDescs() {
		for _, mi := range sd.Methods {
			out = append(out, computeFullMethod(sd, mi.MethodName))
		}
		out = append(out, streamFullMethods(sd)...)
	}
	sort.Strings(out)
	return out
}

// streamFullMethods — стримовая часть переписи одного сервиса.
func streamFullMethods(sd grpc.ServiceDesc) []string {
	var out []string
	for _, si := range sd.Streams {
		out = append(out, computeFullMethod(sd, si.StreamName))
	}
	return out
}

// servedStreamFullMethods — стримовая часть переписи целиком. Нужна как проверка
// СВОЕЙ предпосылки: обход стримов имеет смысл, только пока compute хоть один
// стрим поднимает. Если стримов не станет, «ноль находок» будет неотличим от
// «ноль прочитанного».
func servedStreamFullMethods() []string {
	var out []string
	for _, sd := range servedServiceDescs() {
		out = append(out, streamFullMethods(sd)...)
	}
	sort.Strings(out)
	return out
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

	served := servedFullMethods()
	require.NotEmpty(t, served,
		"served RPC census is empty — the walk found nothing, so it would have asserted nothing")

	var missing []string
	for _, fm := range served {
		if _, ok := m[fm]; !ok {
			missing = append(missing, fm)
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
//
// Обходится ВСЯ выставленная поверхность (servedFullMethods): стрим — такой же
// вход в интерсептор, как unary, и кривая запись для него так же разыменовывается.
func TestPermissionMap_ServedEntriesWellFormed(t *testing.T) {
	m := check.PermissionMap()

	served := servedFullMethods()
	require.NotEmpty(t, served,
		"served RPC census is empty — the walk found nothing, so it would have asserted nothing")

	for _, fm := range served {
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

// Здесь стоял гейт `TestPermissionMap_WellFormedGateSeesStreams` — он требовал,
// чтобы стримовая часть переписи была НЕПУСТА, и сам оговаривал исход: «если это
// теперь верно по замыслу, сними гейт осознанно».
//
// Именно это и произошло: единственный серверный стрим сервиса снят вместе со
// своей поверхностью, потому что подписчика у него не было ни одного дня.
// Гейт защищал от тихого возврата слепой зоны в переписи; предмета у него больше
// нет, и оставленный он краснел бы вечно на достижении собственной цели.
//
// Свойство «стримов нет» держится не молчанием, а утверждением: обход обоих
// слушателей в `cmd/compute` (`TestComputeServesNoServerStreams`) и изъятая ось
// срока жизни стрима у носителя, которая роняет старт, будучи необъявленной.
// Появится стрим — обе стороны заговорят, и этот гейт вернётся вместе с ними.


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
	declaredNames := make(map[string]struct{})
	protoregistry.GlobalFiles.RangeFilesByPackage(computeProtoPackage, func(fd protoreflect.FileDescriptor) bool {
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

	require.NotZero(t, declared, "compute/v1 descriptors must be linked into protoregistry")
	sort.Strings(unclassified)
	require.Emptyf(t, unclassified,
		"compute/v1 service(s) not classified: %v\n"+
			"Add each to servedPublicServiceDescs/servedInternalServiceDescs (if cmd/compute/main.go "+
			"registers it — then every RPC needs a PermissionMap entry) or to notServedServiceNames.",
		unclassified)

	// Вторая половина исчерпывающей классификации: запись, которой больше нечего
	// исключать, тоже обязана ронять этот тест. `MaintenanceService` пережил
	// снятие своего сервиса с контракта и лежал здесь мёртвым: перечень, не
	// истекающий сам, передаёт слепое пятно следующему сервису, которому
	// достанется это имя.
	var orphanNotServed []string
	for name := range notServedServiceNames {
		if _, ok := declaredNames[name]; !ok {
			orphanNotServed = append(orphanNotServed, name)
		}
	}
	sort.Strings(orphanNotServed)
	require.Emptyf(t, orphanNotServed,
		"notServedServiceNames names %d service(s) that compute/v1 no longer declares: %v\n"+
			"An exclusion lives only while it has a subject — drop the entry.",
		len(orphanNotServed), orphanNotServed)
}
