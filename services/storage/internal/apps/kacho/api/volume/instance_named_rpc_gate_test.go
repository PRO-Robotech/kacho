// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package volume_test

import (
	"fmt"
	"sort"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	"github.com/PRO-Robotech/kacho/services/storage/internal/authzfilter"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
)

// Гейт КЛАССА, а не экземпляра: RPC storage, чей запрос НАЗЫВАЕТ машину, обязан
// спросить модель прав про эту машину.
//
// Класс уже стрелял дважды на одном и том же RPC-семействе: сначала перечисление
// привязок отвечало любому аутентифицированному субъекту (гейтилось отношением
// глобального справочника), потом обнаружилось, что письменная половина —
// привязка и отвязка — вообще не спрашивала про машину, хотя пишет строку в её
// набор. Оба раза чинили найденный экземпляр; зеркальный жил дальше. Здесь запрет
// выражен по СВОЙСТВУ («в запросе есть адрес машины»), поэтому новый такой RPC
// красит гейт сам, до того как кто-то вспомнит про этот разбор.
//
// # Предпосылка гейта и её проверка
//
// Предпосылка: адрес машины в запросах storage носят РОВНО два имени поля —
// `instance_id` и `instance_ids`. Предпосылка проверяется здесь же
// (TestInstanceNamingPredicate_HasNoBlindSpot): любое НОВОЕ имя поля, содержащее
// «instance», валит тест и требует решения — адрес это или самоописывающийся
// payload. Без такой проверки запрет молча сузился бы до вчерашних имён.
//
// # Что именно утверждается
//
// Не «в коде есть вызов гейта» (это форма), а наблюдаемое: вызывающий, у которого
// на названной машине нет НИ ОДНОГО отношения, не получает ни строки и не меняет
// ни одной. Поэтому каждая запись таблицы — исполняемая проба, а не галочка.

const storageProtoPackage = protoreflect.FullName("kacho.cloud.storage.v1")

// instanceAddressFields — имена полей, которыми запрос АДРЕСУЕТ машину. Ровно они
// делают RPC двухобъектным.
var instanceAddressFields = map[string]bool{
	"instance_id":  true,
	"instance_ids": true,
}

// instanceDescriptiveFields — имена полей, которые про машину РАССКАЗЫВАЮТ, но её не
// адресуют: самоописывающийся payload привязки (имя и зона машины идут в строку
// привязки как зеркало и как предикат CAS-когерентности). Права на них не вешаются —
// решение принимается по адресу.
var instanceDescriptiveFields = map[string]bool{
	"instance_name":    true,
	"instance_zone_id": true,
}

// instanceNamingRPCProbe — исполняемая проба: прогнать путь под вызывающим, у
// которого на названной машине нет ни одного отношения, и убедиться, что ответ
// пустой/отказный И что ни одна строка не тронута.
type instanceNamingRPCProbe func(t *testing.T)

// probes — по одной на каждый RPC, чей запрос адресует машину. Полнота таблицы
// проверяется обходом дескрипторов ниже: и недостача, и лишняя запись — находка.
var probes = map[string]instanceNamingRPCProbe{
	"/kacho.cloud.storage.v1.InternalVolumeService/Attach": func(t *testing.T) {
		w := newCountingWriter()
		uc := newAttachUC(w, &fakeInstanceGate{}) // ни одного разрешённого отношения
		_, err := uc.Attach(aliceCtx(), attachSpec(gateForeignIns))
		if status.Code(err) != codes.PermissionDenied {
			t.Fatalf("Attach: err = %v, want PermissionDenied", err)
		}
		if w.attaches != 0 {
			t.Fatalf("Attach wrote %d rows for a caller with no relation on the named instance", w.attaches)
		}
	},
	"/kacho.cloud.storage.v1.InternalVolumeService/Detach": func(t *testing.T) {
		w := newCountingWriter()
		uc := newAttachUC(w, &fakeInstanceGate{})
		_, err := uc.Detach(aliceCtx(), gateVolumeID, gateForeignIns)
		if status.Code(err) != codes.PermissionDenied {
			t.Fatalf("Detach: err = %v, want PermissionDenied", err)
		}
		if w.detaches != 0 {
			t.Fatalf("Detach removed %d rows for a caller with no relation on the named instance", w.detaches)
		}
	},
	"/kacho.cloud.storage.v1.InternalVolumeService/ListAttachments": func(t *testing.T) {
		reader := newCountingReader([]*domain.VolumeAttachment{
			{VolumeID: gateVolumeID, InstanceID: gateForeignIns, ProjectID: "prj-theirs", DeviceName: "vda"},
		})
		uc := newListUC(reader, &fakeListFilter{}) // ни одного видимого инстанса
		got, err := uc.ListAttachments(aliceCtx(), []string{gateForeignIns})
		if err != nil {
			t.Fatalf("ListAttachments: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("ListAttachments returned %d rows about an instance the caller may not see", len(got))
		}
	},
}

// requestFieldNames — имена полей входного сообщения метода (плоско, верхний уровень:
// адрес машины в запросах storage лежит на верхнем уровне; вложенные payload-сообщения
// адреса не несут — это проверяет TestInstanceNamingPredicate_HasNoBlindSpot).
func requestFieldNames(md protoreflect.MethodDescriptor) []string {
	fields := md.Input().Fields()
	out := make([]string, 0, fields.Len())
	for i := 0; i < fields.Len(); i++ {
		out = append(out, string(fields.Get(i).Name()))
	}
	return out
}

// rangeStorageMethods обходит ВСЕ методы пакета storage/v1 (дескрипторы —
// источник истины о выставленной поверхности) и возвращает число осмотренного.
func rangeStorageMethods(t *testing.T, visit func(fullMethod string, md protoreflect.MethodDescriptor)) (methods int) {
	t.Helper()
	protoregistry.GlobalFiles.RangeFilesByPackage(storageProtoPackage, func(fd protoreflect.FileDescriptor) bool {
		svcs := fd.Services()
		for i := 0; i < svcs.Len(); i++ {
			svc := svcs.Get(i)
			mds := svc.Methods()
			for j := 0; j < mds.Len(); j++ {
				md := mds.Get(j)
				methods++
				visit(fmt.Sprintf("/%s/%s", svc.FullName(), md.Name()), md)
			}
		}
		return true
	})
	if methods == 0 {
		t.Fatal("storage/v1 descriptors are not linked into protoregistry — the sweep examined NOTHING; " +
			"a silent zero here would report \"no findings\" for \"nothing read\"")
	}
	return methods
}

// TestEveryStorageRPCNamingAnInstanceAsksTheModelAboutIt — сам гейт.
func TestEveryStorageRPCNamingAnInstanceAsksTheModelAboutIt(t *testing.T) {
	var naming []string
	examined := rangeStorageMethods(t, func(fullMethod string, md protoreflect.MethodDescriptor) {
		for _, name := range requestFieldNames(md) {
			if instanceAddressFields[name] {
				naming = append(naming, fullMethod)
				return
			}
		}
	})
	sort.Strings(naming)
	t.Logf("examined %d storage/v1 RPCs; %d of them address an instance in the request", examined, len(naming))

	if len(naming) == 0 {
		t.Fatal("no storage RPC addresses an instance — the predicate found nothing, which contradicts " +
			"the existence of the attachment surface; the field-name premise has drifted")
	}

	for _, fullMethod := range naming {
		probe, ok := probes[fullMethod]
		if !ok {
			t.Fatalf("%s addresses an instance in its request but carries no probe: the caller names an object "+
				"of another domain, so the request has no single subject a per-RPC Check could cover — "+
				"authorize it on the data and prove the refusal here", fullMethod)
		}
		t.Run(fullMethod, probe)
	}

	// Обратная сторона: запись, которой больше нечего защищать, — находка. Иначе
	// таблица переживёт свой предмет и станет описанием вчерашней поверхности.
	inNaming := make(map[string]bool, len(naming))
	for _, m := range naming {
		inNaming[m] = true
	}
	for fullMethod := range probes {
		if !inNaming[fullMethod] {
			t.Fatalf("%s carries a probe but its request no longer addresses an instance — "+
				"remove the entry (a probe without a subject documents a surface that is gone)", fullMethod)
		}
	}
}

// TestInstanceNamingPredicate_HasNoBlindSpot — проверка ПРЕДПОСЫЛКИ гейта.
//
// Гейт узнаёт «запрос адресует машину» по имени поля. Значит он слеп ко всякому
// НОВОМУ имени. Здесь перечисляются все поля запросов, чьё имя упоминает машину, и
// требуется, чтобы каждое было либо адресом (тогда его RPC обязан иметь пробу), либо
// заявленным payload-полем. Новое имя валит тест и заставляет решить, что оно значит.
func TestInstanceNamingPredicate_HasNoBlindSpot(t *testing.T) {
	type place struct{ method, field string }
	var unknown []place
	var addressed, descriptive int

	rangeStorageMethods(t, func(fullMethod string, md protoreflect.MethodDescriptor) {
		for _, name := range requestFieldNames(md) {
			switch {
			case instanceAddressFields[name]:
				addressed++
			case instanceDescriptiveFields[name]:
				descriptive++
			case containsInstance(name):
				unknown = append(unknown, place{fullMethod, name})
			}
		}
	})

	t.Logf("instance-mentioning request fields: %d addressing, %d descriptive", addressed, descriptive)
	if addressed == 0 {
		t.Fatal("no request field addresses an instance — the predicate has nothing to key on")
	}
	for _, p := range unknown {
		t.Errorf("%s: request field %q mentions an instance but is neither a declared address "+
			"(instanceAddressFields) nor declared payload (instanceDescriptiveFields) — decide which it is: "+
			"an address makes the RPC two-object and requires a probe", p.method, p.field)
	}
}

// containsInstance — «имя упоминает машину». Наивно и намеренно: гейт предпосылки
// обязан ловить ШИРЕ, чем сам запрет, иначе он не про слепое пятно.
func containsInstance(name string) bool {
	const needle = "instance"
	for i := 0; i+len(needle) <= len(name); i++ {
		if name[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// Компиляционная привязка к порту: проба-таблица опирается на fakeInstanceGate,
// который обязан оставаться реализацией ObjectGate.
var _ authzfilter.ObjectGate = (*fakeInstanceGate)(nil)
