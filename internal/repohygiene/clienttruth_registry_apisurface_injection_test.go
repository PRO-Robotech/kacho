// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// clienttruth_registry_apisurface_injection_test.go — доказательство, что гейт
// СПОСОБЕН упасть, и что он молчит на законном близнеце.
//
// Проверка, которую не заставили покраснеть, свойства не измеряет: на чистом дереве
// мёртвый гейт выглядит ровно как живой. Здесь дефект возвращается — и по НАСТОЯЩЕМУ
// дереву (снимаем реальное объявление с реальной страницы), и по синтетике, где
// проверяются механики, которых в дереве сегодня нет ни одним экземпляром.
//
// Каждое утверждение парное: рядом с «краснеет на дефекте» стоит «молчит на законном
// близнеце». Отрицание без положительного контроля зеленеет на всём сломанном.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// realTreeInputs — вход анализатора, взятый из дерева. Инъекция настоящим входом
// (а не только синтетикой) нужна затем, что синтетика доказывает механику, а дерево —
// применимость: разбор, верный на выдуманном протофайле, мог бы не найти в настоящем
// ни одного метода и оставаться зелёным.
func realTreeInputs(t *testing.T) ([]ContractOperation, map[string]string) {
	t.Helper()
	root := repoRoot(t)
	protoBytes, err := os.ReadFile(filepath.Join(root, registryServiceProto))
	if err != nil {
		t.Fatalf("контракт не прочитан: %v", err)
	}
	ops, _, _ := ParseContractOperations(string(protoBytes), registryPublicService)
	if len(ops) == 0 {
		t.Fatalf("в контракте не разобрано ни одного метода — инъекция беспредметна")
	}
	pages, err := readClientPages(filepath.Join(root, registryClientAPIDir))
	if err != nil || len(pages) == 0 {
		t.Fatalf("клиентские страницы не прочитаны (%v) — инъекция беспредметна", err)
	}
	return ops, pages
}

// TestApiSurfaceInjection_RealTreeIsSilent — ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ инъекции ниже.
//
// Без него «дефект найден» неотличимо от «анализатор находит всё подряд».
func TestApiSurfaceInjection_RealTreeIsSilent(t *testing.T) {
	ops, pages := realTreeInputs(t)
	missing, census := UndocumentedOperations(ops, pages)
	if len(missing) != 0 {
		t.Fatalf("на нетронутом дереве находок быть не должно, получено %d: %v (%s)",
			len(missing), missing, census)
	}
}

// TestApiSurfaceInjection_RemovingARealOperationIsFound — возвращаем дефект в
// НАСТОЯЩИЙ вход: снимаем со страницы объявление `:rename` — ровно тот глагол,
// умолчание о котором и завело #1600.
func TestApiSurfaceInjection_RemovingARealOperationIsFound(t *testing.T) {
	ops, pages := realTreeInputs(t)

	const victim = `endpoint="/registry/v1/registries/{registryId}/repositories/{repository}:rename"`
	injected := map[string]string{}
	removed := 0
	for name, body := range pages {
		if strings.Contains(body, victim) {
			body = strings.Replace(body, victim, `endpoint="/dev/null"`, 1)
			removed++
		}
		injected[name] = body
	}
	if removed != 1 {
		t.Fatalf("инъекция беспредметна: объявление %s встречено %d раз, ожидался ровно один",
			victim, removed)
	}

	missing, _ := UndocumentedOperations(ops, injected)
	if len(missing) != 1 {
		t.Fatalf("снятое объявление обязано дать РОВНО одну находку, получено %d: %v", len(missing), missing)
	}
	if missing[0].RPC != "RenameRepository" {
		t.Fatalf("находка обязана назвать метод контракта, получено %q", missing[0].RPC)
	}
	if !strings.Contains(missing[0].String(), ":rename") {
		t.Fatalf("находка обязана назвать адрес, по которому её искать; получено %q", missing[0].String())
	}
}

// TestApiSurfaceInjection_NormalizationMatchesTheTwoWritingStyles — ЗАКОННЫЙ БЛИЗНЕЦ.
//
// Контракт пишет `{registry_id}` и глубокий шаблон `=**`, страница — `{registryId}`
// без шаблона. Это ОДИН адрес, записанный двумя поверхностями по своим правилам;
// анализатор, не снимающий различия, объявил бы описанное неописанным — и его
// находки были бы ложными все до одной, то есть его перестали бы читать.
func TestApiSurfaceInjection_NormalizationMatchesTheTwoWritingStyles(t *testing.T) {
	const proto = `
service RegistryService {
  rpc GetRepository(GetRepositoryRequest) returns (Repository) {
    option (google.api.http) = {get: "/registry/v1/registries/{registry_id}/repositories/{repository=**}"};
  }
}
`
	ops, rpcs, rest := ParseContractOperations(proto, registryPublicService)
	if rpcs != 1 || rest != 1 || len(ops) != 1 {
		t.Fatalf("разбор контракта: методов %d, из них с адресом %d, операций %d — ожидалось 1/1/1", rpcs, rest, len(ops))
	}

	pages := map[string]string{"repository.mdx": `
<ApiOperation method="GET" endpoint="/registry/v1/registries/{registryId}/repositories/{repository}">
`}
	if missing, c := UndocumentedOperations(ops, pages); len(missing) != 0 {
		t.Fatalf("законный близнец обязан молчать, получено %v (%s)", missing, c)
	}

	// Обратная сторона того же утверждения: РАЗНЫЕ адреса нормализация склеивать
	// не вправе — иначе она объявляла бы описанным то, чего на странице нет.
	other := map[string]string{"repository.mdx": `
<ApiOperation method="GET" endpoint="/registry/v1/registries/{registryId}/repositories">
`}
	if missing, _ := UndocumentedOperations(ops, other); len(missing) != 1 {
		t.Fatalf("соседний адрес не описывает операцию — ожидалась 1 находка, получено %d", len(missing))
	}

	// И глагол — часть ключа: тот же адрес другим методом операцией не является.
	wrongVerb := map[string]string{"repository.mdx": `
<ApiOperation method="DELETE" endpoint="/registry/v1/registries/{registryId}/repositories/{repository}">
`}
	if missing, _ := UndocumentedOperations(ops, wrongVerb); len(missing) != 1 {
		t.Fatalf("другой глагол по тому же адресу — другая операция; ожидалась 1 находка, получено %d", len(missing))
	}
}

// TestApiSurfaceInjection_GrpcOnlyRPCIsJudgedByItsOwnForm — метод без REST-адреса
// судится gRPC-формой `Служба/Метод`. Пара: недокументированный — находка,
// документированный той формой, какой его пишут страницы, — молчание.
func TestApiSurfaceInjection_GrpcOnlyRPCIsJudgedByItsOwnForm(t *testing.T) {
	const proto = `
service RegistryService {
  rpc SomeStreamingThing(Req) returns (Resp);
}
`
	ops, _, rest := ParseContractOperations(proto, registryPublicService)
	if len(ops) != 1 || rest != 0 {
		t.Fatalf("метод без REST-адреса: операций %d, с адресом %d — ожидалось 1/0", len(ops), rest)
	}
	if ops[0].Verb != "gRPC" {
		t.Fatalf("метод без REST-адреса обязан судиться gRPC-формой, получено %q", ops[0].Verb)
	}

	if missing, _ := UndocumentedOperations(ops, map[string]string{"p.mdx": "текст без объявлений"}); len(missing) != 1 {
		t.Fatalf("неописанный gRPC-метод — находка; получено %d", len(missing))
	}
	documented := map[string]string{"p.mdx": `<ApiOperation method="gRPC" endpoint="RegistryService/SomeStreamingThing">`}
	if missing, _ := UndocumentedOperations(ops, documented); len(missing) != 0 {
		t.Fatalf("описанный gRPC-формой метод обязан молчать, получено %v", missing)
	}
}

// TestApiSurfaceInjection_EveryWritingFormOfADeclarationIsRead — распознаватель
// знает все законные формы записи объявления, а не только ту, что лежит в дереве.
//
// Первая редакция этого анализатора искала «тег плюс два атрибута в таком-то
// порядке» и была написана с прямо заявленной посылкой «форма в дереве одна».
// Посылка верна и НЕДОСТАТОЧНА: замер по формам показал, что мимо уходят три
// законные записи — обратный порядок атрибутов, одинарные кавычки и третий атрибут
// между `method` и `endpoint`. Записанное ими не даёт ни находки, ни зелени.
//
// Сегодняшнее дерево ни одной из них не содержит — то есть расширение не меняет ни
// одного вердикта СЕЙЧАС. Оно про завтра: автор, написавший иначе, узнал бы о
// слепоте только через пропущенную находку.
func TestApiSurfaceInjection_EveryWritingFormOfADeclarationIsRead(t *testing.T) {
	forms := map[string]string{
		"канон дерева":          `<ApiOperation method="GET" endpoint="/x">`,
		"перенос строк":         "<ApiOperation\n  method=\"GET\"\n  endpoint=\"/x\">",
		"обратный порядок":      `<ApiOperation endpoint="/x" method="GET">`,
		"одинарные кавычки":     `<ApiOperation method='GET' endpoint='/x'>`,
		"атрибут посередине":    `<ApiOperation method="GET" async endpoint="/x">`,
		"атрибут после (async)": `<ApiOperation method="GET" endpoint="/x" async>`,
	}
	for name, form := range forms {
		keys, tags := ParseDocumentedOperations(form)
		if tags != 1 {
			t.Errorf("%s: тегов встречено %d, ожидался 1", name, tags)
		}
		if len(keys) != 1 || keys[0] != "GET /x" {
			t.Errorf("%s: разобрано %v, ожидалось [\"GET /x\"]", name, keys)
		}
	}

	// Обратная сторона: тег БЕЗ обязательных атрибутов разобран быть не может —
	// он обязан остаться посчитанным, иначе «объявлений нет» станет неотличимо от
	// «объявления есть, но анализатор их не читает».
	keys, tags := ParseDocumentedOperations(`<ApiOperation someOtherAttr="1">`)
	if tags != 1 || len(keys) != 0 {
		t.Fatalf("тег без method/endpoint: тегов %d, разобрано %d — ожидалось 1/0", tags, len(keys))
	}
}

// TestApiSurfaceInjection_CommentsAreNotDeclarations — адрес и метод встречаются в
// комментариях контракта (там их десятки), поэтому разбор по подстроке краснел бы
// на собственном объяснении.
func TestApiSurfaceInjection_CommentsAreNotDeclarations(t *testing.T) {
	const proto = `
service RegistryService {
  // Здесь был метод:
  //   rpc RetiredThing(Req) returns (Resp) {
  //     option (google.api.http) = {get: "/registry/v1/retired"};
  //   }
  rpc GetRegistry(GetRegistryRequest) returns (Registry) {
    option (google.api.http) = {get: "/registry/v1/registries/{registry_id}"};
  }
}
`
	ops, rpcs, _ := ParseContractOperations(proto, registryPublicService)
	if rpcs != 1 || len(ops) != 1 {
		t.Fatalf("закомментированный метод не является объявлением: методов %d, операций %d — ожидалось 1/1", rpcs, len(ops))
	}
	if ops[0].RPC != "GetRegistry" || ops[0].Address != "/registry/v1/registries/{registryId}" {
		t.Fatalf("разобран не тот метод: %s", ops[0])
	}
}

// TestApiSurfaceInjection_OtherServicesAreOutOfScope — внутренняя служба под гейт не
// подпадает: ban #6 держит её вне внешней поверхности, и требовать от неё клиентской
// страницы значило бы требовать документировать недостижимое.
func TestApiSurfaceInjection_OtherServicesAreOutOfScope(t *testing.T) {
	const proto = `
service InternalRegistryService {
  rpc GetRegistryStats(Req) returns (Resp) {
    option (google.api.http) = {get: "/internal/stats"};
  }
}
service RegistryService {
  rpc GetRegistry(Req) returns (Resp) {
    option (google.api.http) = {get: "/registry/v1/registries/{registry_id}"};
  }
}
`
	ops, rpcs, _ := ParseContractOperations(proto, registryPublicService)
	if rpcs != 1 || len(ops) != 1 || ops[0].RPC != "GetRegistry" {
		t.Fatalf("под гейт обязана попасть только публичная служба; методов %d, операций %v", rpcs, ops)
	}
}

// TestApiSurfaceInjection_EmptyInputIsNotSilentSuccess — беспредметный вход обязан
// быть отличим от чистого дерева: «ноль находок» и «ноль прочитанного» дают один и
// тот же пустой перечень, и именно на этом гейт становится мёртвым незаметно.
func TestApiSurfaceInjection_EmptyInputIsNotSilentSuccess(t *testing.T) {
	ops, rpcs, _ := ParseContractOperations("", registryPublicService)
	if len(ops) != 0 || rpcs != 0 {
		t.Fatalf("пустой контракт обязан дать ноль операций, получено %d/%d", len(ops), rpcs)
	}
	// Гейт на этом падает (t.Fatalf «обход пуст»); здесь фиксируется, что величина,
	// по которой он это решает, действительно нулевая.

	realOps, _ := realTreeInputs(t)
	missing, census := UndocumentedOperations(realOps, map[string]string{})
	if len(missing) != len(realOps) {
		t.Fatalf("без страниц описанным не является НИЧЕГО: находок %d при %d операциях", len(missing), len(realOps))
	}
	if census.Pages != 0 || census.OperationsRead != 0 {
		t.Fatalf("перепись обязана показать пустой обход, получено %s", census)
	}
}
