// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// consoleoneofbranchprobe_injection_test.go — способность гейта упасть и смолчать,
// доказанная НАСТОЯЩИМ входом обеих сторон.
//
// Инъекция без законного близнеца доказывает только чувствительность к форме:
// гейт, краснеющий на всём, отключат при первом же ложном срабатывании. Поэтому
// каждая проба здесь идёт парой — дефект и та же конструкция без дефекта.

package repohygiene

import (
	"strings"
	"testing"
)

const injProto = `
syntax = "proto3";
package kacho.cloud.demo.v1;

message HealthProbe {
  message TcpOptions { int32 port = 1; }
  message HttpOptions { string path = 1; }
  // Здесь стояла строка групповой опции (option exactly_one) — её разбор
  // обязан был пропускать, не приняв за ветвь. Форма снята вместе с
  // семейством ограничений полей (kacho#1255): групповых опций в дереве не
  // осталось НИ ОДНОЙ, и производителя у такого входа больше нет. Фикстура,
  // вносящая непроизводимый признак, доказывает способность разбора по образцу,
  // а не по существу. Появится групповая опция — строка вернётся вместе с ней.
  oneof options {
    TcpOptions tcp = 1;
    HttpOptions http = 2;
  }
}

message CreateWidgetRequest {
  string project_id = 1;
  HealthProbe probe = 2;
  // Ветвь с хвостом опции: разбор без него молча вернул бы «ветвей нет».
  //
  // Хвост взят ЖИВОЙ формой из дерева. Прежде здесь стояли опции снятого
  // семейства (length); после его снятия такой хвост не
  // производится ничем, и проба доказывала бы разбор входа, которого не бывает.
  // Обе оставшиеся формы хвоста в контрактах — эти две.
  oneof anchor {
    string zone_id = 3 [(kacho.cloud.api.secret_bearing) = true];
    string region_id = 4 [deprecated = true];
  }
}

message ListWidgetsRequest { string project_id = 1; }

service WidgetService {
  rpc Create(CreateWidgetRequest) returns (Operation) {
    option (google.api.http) = { post: "/demo/v1/widgets" body: "*" };
  }
  rpc List(ListWidgetsRequest) returns (ListWidgetsResponse) {
    option (google.api.http) = { get: "/demo/v1/widgets" };
  }
}
`

const injRegistry = `
export const REGISTRY = {
  widgets: {
    id: "widgets",
    route: "widgets",
    apiPath: "/demo/v1/widgets",
    scope: "project",
    ops: { create: true, update: true, delete: true },
    fields: [],
  },
  gadgets: {
    id: "gadgets",
    route: "gadgets",
    apiPath: "/demo/v1/gadgets",
    scope: "project",
    ops: { create: false, update: false, delete: true },
    fields: [],
  },
};
`

func TestConsoleOneofProbeGateParsesContract(t *testing.T) {
	pf := parseProtoFile(injProto)

	if len(pf.Creates) != 1 || pf.Creates[0].Path != "/demo/v1/widgets" {
		t.Fatalf("мутирующий маршрут не распознан: %+v", pf.Creates)
	}

	got := branchingsReachable(pf.Messages, "kacho.cloud.demo.v1.CreateWidgetRequest")
	want := []string{
		"kacho.cloud.demo.v1.CreateWidgetRequest::anchor",
		"kacho.cloud.demo.v1.HealthProbe::options",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("ветвления, достижимые из создания: получили %v, ждали %v", got, want)
	}

	// Законный близнец: запрос БЕЗ группы не объявляется ветвлением.
	if b := branchingsReachable(pf.Messages, "kacho.cloud.demo.v1.ListWidgetsRequest"); len(b) != 0 {
		t.Fatalf("сообщение без группы объявлено ветвлением: %v — гейт краснел бы на всём", b)
	}
}

func TestConsoleOneofProbeGateParsesRegistry(t *testing.T) {
	specs := parseConsoleRegistry("demo", "demo/src/lib/resource-registry.tsx", injRegistry)
	if len(specs) != 2 {
		t.Fatalf("спеков распознано %d, ждали 2: %+v", len(specs), specs)
	}
	byID := map[string]consoleSpec{}
	for _, s := range specs {
		byID[s.ID] = s
	}
	if !byID["widgets"].Creatable {
		t.Fatalf("создаваемый спек прочитан как несоздаваемый — гейт молчал бы на живом предмете")
	}
	// Законный близнец: спек только для чтения формы не имеет, и требовать с
	// него пробу покрытия было бы ложным срабатыванием.
	if byID["gadgets"].Creatable {
		t.Fatalf("спек только для чтения прочитан как создаваемый — гейт краснел бы на законном")
	}
	if byID["widgets"].APIPath != "/demo/v1/widgets" {
		t.Fatalf("маршрут спека прочитан неверно: %q", byID["widgets"].APIPath)
	}
}

func TestConsoleOneofProbeGateDiscriminatesCoverage(t *testing.T) {
	// Дефект: проба модуля есть, но про ЭТОТ спек молчит.
	foreignProbe := `oneofBranches("demo/v1/other.proto", "X", "y"); REGISTRY["gadgets"]`
	if coveredBySomeProbe([]string{foreignProbe}, "widgets") {
		t.Fatalf("проба, не называющая спек, засчитана покрытием — гейт молчал бы на дефекте, " +
			"ради которого написан")
	}
	// Законный близнец: проба называет спек — гейт молчит.
	ownProbe := `oneofBranches("demo/v1/w.proto", "CreateWidgetRequest", "anchor"); REGISTRY["widgets"]`
	if !coveredBySomeProbe([]string{ownProbe}, "widgets") {
		t.Fatalf("проба, называющая спек, не засчитана — первый же ложный срабат отключил бы гейт")
	}
	// Ноль проб у модуля — тоже дефект, а не «нечего проверять».
	if coveredBySomeProbe(nil, "widgets") {
		t.Fatalf("отсутствие проб засчитано покрытием")
	}
}
