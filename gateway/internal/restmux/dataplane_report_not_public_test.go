// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package restmux

import (
	"strings"
	"testing"

	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	// Слинковывает дескрипторы домена vpc в бинарь пробы: без него реестр о них
	// не знает, и «маршрутов не найдено» стало бы верно на пустоте.
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
)

// dataplane_report_not_public_test.go — приём отчёта исполнителя недостижим с
// внешнего края (APPLY-16).
//
// # Предмет
//
// Отчёт решает, что платформа считает применённым. Кто до него дотянулся — тот
// пишет арендатору «применено» на ресурс, которого сеть не видела, и «не
// применено» на исправный. Поток намерения — вторая половина того же сервиса —
// несёт намерение по ВСЕМ арендаторам сразу вместе с координатой изоляции сети.
// Обе поверхности живут на внутреннем слушателе.
//
// # Почему таблица маршрутов, а не запрос по выдуманному адресу
//
// Сквозной половины у этого утверждения нет и быть не может: запрос к пути,
// которого не существует, отвечает 404 ВСЕГДА — и когда метод не смонтирован, и
// когда его смонтировали по другому адресу. Такое утверждение зелено при любом
// состоянии дерева, то есть упасть не способно. Проверять надо ТАБЛИЦУ, а не
// ответ.
//
// # Две половины, и обе нужны
//
// Отрицательная — ни один REST-биндинг не ведёт в этот сервис, и ни один его
// метод не несёт HTTP-аннотации. Положительная — публичные маршруты сети найдены
// и аннотации у публичных методов есть: без неё «не найдено» было бы неотличимо
// от «таблица и дескрипторы не прочитаны».

// dataplaneServiceFQN — сервис шва с исполнителем датаплейна.
const dataplaneServiceFQN = "kacho.cloud.vpc.v1.InternalDataplaneService"

// TestDataplaneReportIsUnreachableFromThePublicEdge — обе половины.
func TestDataplaneReportIsUnreachableFromThePublicEdge(t *testing.T) {
	// ── половина 1: таблица REST-биндингов ──────────────────────────────────
	bindings := loadedHTTPBindings()
	if len(bindings) == 0 {
		t.Fatal("таблица REST-биндингов пуста — гейт читает не то дерево")
	}

	var offenders []string
	publicVPCRoutes := 0
	for _, b := range bindings {
		if strings.HasPrefix(b.fqn, dataplaneServiceFQN+"/") {
			offenders = append(offenders, b.method+" "+b.template+" → "+b.fqn)
		}
		// ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: публичные маршруты сети таблица знает.
		if strings.HasPrefix(b.fqn, "kacho.cloud.vpc.v1.NetworkService/") {
			publicVPCRoutes++
		}
	}
	if len(offenders) > 0 {
		t.Fatalf("на край выведен приём/поток намерения датаплейна: %s.\n"+
			"Отчёт решает, что платформа считает применённым, а поток несёт намерение "+
			"по всем арендаторам сразу вместе с координатой изоляции сети — обе "+
			"поверхности внутренние (запрет #6)", strings.Join(offenders, "; "))
	}
	if publicVPCRoutes == 0 {
		t.Fatal("в таблице не нашлось ни одного публичного маршрута сети — " +
			"«внутреннего не найдено» неотличимо от «таблица не прочитана»")
	}

	// ── половина 2: дескрипторы ─────────────────────────────────────────────
	svc := lookupService(t, dataplaneServiceFQN)
	methodsChecked := 0
	for i := 0; i < svc.Methods().Len(); i++ {
		m := svc.Methods().Get(i)
		methodsChecked++
		if rule, _ := proto.GetExtension(m.Options(), annotations.E_Http).(*annotations.HttpRule); rule != nil {
			t.Errorf("%s/%s несёт HTTP-аннотацию — метод внутреннего слушателя "+
				"получил адрес на краю", dataplaneServiceFQN, m.Name())
		}
	}
	if methodsChecked == 0 {
		t.Fatalf("у %s не нашлось ни одного метода — предпосылка гейта ложна", dataplaneServiceFQN)
	}

	// ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ дескрипторной половины: у публичного сервиса
	// аннотации есть, значит предикат их видеть умеет.
	pub := lookupService(t, "kacho.cloud.vpc.v1.NetworkService")
	annotated := 0
	for i := 0; i < pub.Methods().Len(); i++ {
		if rule, _ := proto.GetExtension(pub.Methods().Get(i).Options(), annotations.E_Http).(*annotations.HttpRule); rule != nil {
			annotated++
		}
	}
	if annotated == 0 {
		t.Fatal("у публичного сервиса сети не нашлось ни одной HTTP-аннотации — " +
			"предикат аннотаций не видит, и первая половина ничего не доказала")
	}

	t.Logf("осмотрено %d REST-биндинг(ов) (публичных маршрутов сети %d); "+
		"методов %s — %d, из них с HTTP-аннотацией 0; "+
		"аннотированных методов публичного сервиса сети — %d",
		len(bindings), publicVPCRoutes, dataplaneServiceFQN, methodsChecked, annotated)
}

// lookupService достаёт дескриптор сервиса из глобального реестра.
func lookupService(t *testing.T, fqn string) protoreflect.ServiceDescriptor {
	t.Helper()
	d, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName(fqn))
	if err != nil {
		t.Fatalf("дескриптор %s не найден в реестре: %v", fqn, err)
	}
	svc, ok := d.(protoreflect.ServiceDescriptor)
	if !ok {
		t.Fatalf("%s — не сервис", fqn)
	}
	return svc
}
