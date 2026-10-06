// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package domain_test

import (
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/PRO-Robotech/corelib/authz/catalogderive"
	registryv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/registry/v1"
	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
)

// namedActions — константа действия домена → полное имя глагола, чьё право она
// называет.
var namedActions = map[string]string{
	domain.ActionRegistryList:   "/kacho.cloud.registry.v1.RegistryService/List",
	domain.ActionRepositoryList: "/kacho.cloud.registry.v1.RegistryService/ListRepositories",
}

// TestActionConstantsAreDerivedFromTheContractAnnotation — строка действия
// сверяется с АННОТАЦИЕЙ КОНТРАКТА, а не со вторым рукописным перечнем.
//
// # Зачем константа вообще нужна
//
// Объявление журнала подписки обязано брать тип объекта и действие
// КВАЛИФИЦИРОВАННЫМ ИМЕНЕМ чужого пакета: литерал есть второе написание чужого
// словаря, и расходится оно молча — вопрос о видимости уходит про несуществующее
// действие, а строка перестаёт доставляться без отказа и без пропуска в
// нумерации (гейт `internal/repohygiene`, `KIND-VOCABULARY-LITERAL`).
//
// # Почему константа, а не чтение аннотации на пути запроса
//
// Аннотацию можно прочитать и в рантайме, но тогда объявление журнала перестало
// бы быть ЗНАЧЕНИЯМИ: у него появился бы отказ сборки, зависящий от реестра
// дескрипторов. Здесь выбрана константа плюс ЭТА проба — она читает подлинник
// (аннотацию метода) и требует дословного совпадения. Расхождение краснеет на
// прогоне, а не в бою.
//
// Пустой обход — отказ: ноль осмотренных глаголов означает, что реестр
// дескрипторов пуст, и «расхождений нет» получено даром.
func TestActionConstantsAreDerivedFromTheContractAnnotation(t *testing.T) {
	pkg := string(registryv1.File_kacho_cloud_registry_v1_registry_service_proto.Package())

	seen := 0
	permissionOf := map[string]string{}
	catalogderive.RangeAnnotated([]string{pkg},
		func(fullMethod string, _ protoreflect.MethodDescriptor, a catalogderive.Annotations) {
			seen++
			permissionOf[fullMethod] = a.Permission
		})

	if seen == 0 {
		t.Fatalf("осмотрено глаголов пакета %s: 0 — реестр дескрипторов пуст, и сверка ничего не утверждает", pkg)
	}
	for action, method := range namedActions {
		permission, found := permissionOf[method]
		if !found {
			t.Errorf("глагол %s не найден среди %d осмотренных: имя уехало, и константа сверена не с тем", method, seen)
			continue
		}
		if permission == "" {
			t.Errorf("у глагола %s аннотация права пуста — сверять не с чем", method)
			continue
		}
		if action != permission {
			t.Errorf("действие в домене %q, аннотация контракта %q (%s): два написания одного предмета "+
				"разошлись, и вопрос о видимости в потоке ушёл бы не тем действием, "+
				"что задаёт список", action, permission, method)
		}
	}
	t.Logf("осмотрено глаголов пакета %s: %d; сверено констант действия %d", pkg, seen, len(namedActions))
}
