// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package domain_test

import (
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"

	registryv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/registry/v1"
	"github.com/PRO-Robotech/kacho/pkg/authz/catalogderive"
	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
)

// listRegistriesMethod — полное имя глагола, чьё право здесь названо.
const listRegistriesMethod = "/kacho.cloud.registry.v1.RegistryService/List"

// TestActionRegistryListIsDerivedFromTheContractAnnotation — строка действия
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
func TestActionRegistryListIsDerivedFromTheContractAnnotation(t *testing.T) {
	pkg := string(registryv1.File_kacho_cloud_registry_v1_registry_service_proto.Package())

	seen := 0
	var permission string
	found := false
	catalogderive.RangeAnnotated([]string{pkg},
		func(fullMethod string, _ protoreflect.MethodDescriptor, a catalogderive.Annotations) {
			seen++
			if fullMethod == listRegistriesMethod {
				permission, found = a.Permission, true
			}
		})

	if seen == 0 {
		t.Fatalf("осмотрено глаголов пакета %s: 0 — реестр дескрипторов пуст, и сверка ничего не утверждает", pkg)
	}
	if !found {
		t.Fatalf("глагол %s не найден среди %d осмотренных: имя уехало, и константа сверена не с тем", listRegistriesMethod, seen)
	}
	if permission == "" {
		t.Fatalf("у глагола %s аннотация права пуста — сверять не с чем", listRegistriesMethod)
	}
	if domain.ActionRegistryList != permission {
		t.Fatalf("действие в домене %q, аннотация контракта %q: два написания одного предмета "+
			"разошлись, и вопрос о видимости в потоке ушёл бы не тем действием, "+
			"что задаёт список", domain.ActionRegistryList, permission)
	}
	t.Logf("осмотрено глаголов пакета %s: %d; право глагола %s — %q", pkg, seen, listRegistriesMethod, permission)
}
