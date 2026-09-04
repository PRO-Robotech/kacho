// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package restmux

import (
	"strings"
	"testing"
)

// http_binding_resolution_test.go — резолв (метод, путь) → биндинг обязан
// выбирать ТОТ биндинг, который обслужит запрос, а не первый совпавший.
//
// # Почему это заведено отдельной пробой
//
// `resolveHTTPBinding` читают ДВА гейта — разбор тела запроса
// (`unknown_body_fields_test.go`) и примеры страниц арендатора
// (`docs_enum_names_test.go`, `docs_example_keys_test.go`). Оба судят пример
// против сообщения, которое им вернул резолв; ошибившийся резолв даёт им не
// красное и не зелёное, а НАХОДКУ НЕ О ТОМ: пример `GetRepository` сверялся с
// `ListReferrersResponse`, и все четырнадцать его ключей выглядели
// несуществующими, будучи объявленными.
//
// # Две оси, на которых резолв ошибался, и обе измерены
//
//  1. СОБСТВЕННЫЙ ГЛАГОЛ (`{id}:verb`). Шаблон `/instances/{instance_id}` и
//     шаблон `/instances/{instance_id}:serialPortOutput` совпадают с одним и тем
//     же путём: у первого подстановка свободна и берёт `x:serialPortOutput`
//     целиком. Различить их обязана СПЕЦИФИЧНОСТЬ — сужённая подстановка
//     (непустая приставка либо окончание) специфичнее свободной, — а она
//     считала только литеральные сегменты, поэтому спор решал порядок обхода
//     реестра. Сужённых подстановок в таблице 41 сегмент, то есть предмет
//     массовый, а не краевой;
//
//  2. ГЛУБОКАЯ ПОДСТАНОВКА С ХВОСТОМ (`{x=**}/referrers`). Ветвь `rest`
//     возвращала «совпало», как только оставался хотя бы один сегмент, и
//     ХВОСТ ШАБЛОНА не сверяла вовсе — ни литеральный `/referrers`, ни
//     окончание `:rename` у самой подстановки.
//
// Замер по таблице на момент правки: биндингов 308, сегментов `**` — 5, из них
// с хвостом 1, и он НЕ принадлежит Internal*-сервису; внутренних маршрутов 60,
// из них с таким хвостом — 0. То есть у классификатора «этот запрос
// внутренний» предмета этой ошибки сегодня нет ВОВСЕ, и его поведение правкой
// не меняется: правка сужает совпадение до верного, а сузить до неверного она
// не может — точное совпадение не пропускается никогда. Число названо, чтобы
// «ничего не сломалось» опиралось на замер, а не на впечатление.

// bindingResolutionCase — один путь и FQN, который обязан его обслужить.
type bindingResolutionCase struct {
	method string
	path   string
	// wantFQN — ожидаемый биндинг; пусто означает «не обслуживается никем».
	wantFQN string
	why     string
}

func TestHTTPBindingResolutionPicksTheServingTemplate(t *testing.T) {
	cases := []bindingResolutionCase{
		{
			method: "GET", path: "/compute/v1/instances/x:serialPortOutput",
			wantFQN: "kacho.cloud.compute.v1.InstanceService/GetSerialPortOutput",
			why:     "собственный глагол специфичнее свободной подстановки",
		},
		{
			method: "GET", path: "/compute/v1/instances/x",
			wantFQN: "kacho.cloud.compute.v1.InstanceService/Get",
			why:     "ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: свободная подстановка не должна быть вытеснена",
		},
		{
			method: "GET", path: "/iam/v1/groups/x:listMembers",
			wantFQN: "kacho.cloud.iam.v1.GroupService/ListMembers",
			why:     "тот же класс у второго сервиса — предмет не единичный",
		},
		{
			method: "GET", path: "/iam/v1/groups/x",
			wantFQN: "kacho.cloud.iam.v1.GroupService/Get",
			why:     "ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ к предыдущему",
		},
		{
			method: "GET", path: "/registry/v1/registries/x/repositories/x",
			wantFQN: "kacho.cloud.registry.v1.RegistryService/GetRepository",
			why:     "хвост `/referrers` чужого шаблона обязан отсечь его",
		},
		{
			method: "GET", path: "/registry/v1/registries/x/repositories/a/b/referrers",
			wantFQN: "kacho.cloud.registry.v1.RegistryService/ListReferrers",
			why:     "ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: глубокая подстановка с хвостом обязана совпадать",
		},
		{
			method: "POST", path: "/registry/v1/registries/x/repositories/a/b:rename",
			wantFQN: "kacho.cloud.registry.v1.RegistryService/RenameRepository",
			why:     "окончание у самой глубокой подстановки",
		},
		{
			method: "POST", path: "/registry/v1/registries/x/repositories/a/b",
			wantFQN: "",
			why:     "без глагола POST по этому пути не обслуживается — окончание `:rename` обязано отсечь",
		},
		{
			method: "POST", path: "/vpc/v1/addressPools",
			wantFQN: "kacho.cloud.vpc.v1.AddressPoolService/Create",
			why:     "пару объявляют ДВА сервиса; снаружи её обслуживает публичный, и спор решается этим, а не порядком обхода реестра",
		},
	}

	for _, c := range cases {
		b, ok := resolveHTTPBinding(c.method, c.path)
		got := ""
		if ok {
			got = b.fqn
		}
		if got != c.wantFQN {
			t.Errorf("%s %s → %q, ожидалось %q (%s)", c.method, c.path, got, c.wantFQN, c.why)
		}
	}
	t.Logf("путей проверено %d; биндингов в таблице %d", len(cases), len(loadedHTTPBindings()))
}

// TestHTTPBindingTemplateShapesAreStillPresent — проверка ПРЕДПОСЫЛКИ пробы
// выше: обе формы, ради которых она заведена, в таблице есть.
//
// Без неё проба выродилась бы молча: сними кто-нибудь собственные глаголы или
// глубокую подстановку с хвостом — кейсы стали бы утверждать «не обслуживается»
// и остались бы зелёными, ничего не измеряя.
func TestHTTPBindingTemplateShapesAreStillPresent(t *testing.T) {
	constrainedWild, deepWithTail, deepWithSuffix := 0, 0, 0
	for _, b := range loadedHTTPBindings() {
		for i, s := range b.segs {
			switch {
			case s.rest && s.suffix != "":
				deepWithSuffix++
			case s.rest && i != len(b.segs)-1:
				deepWithTail++
			case s.wild && (s.prefix != "" || s.suffix != ""):
				constrainedWild++
			}
		}
	}
	t.Logf("биндингов %d; сегментов с сужённой подстановкой %d; глубоких подстановок с хвостом шаблона %d, с окончанием %d",
		len(loadedHTTPBindings()), constrainedWild, deepWithTail, deepWithSuffix)

	if constrainedWild == 0 {
		t.Fatal("сужённых подстановок в таблице нет: первая ось пробы резолва беспредметна")
	}
	if deepWithTail == 0 && deepWithSuffix == 0 {
		t.Fatal("глубоких подстановок с хвостом либо окончанием нет: вторая ось пробы резолва беспредметна")
	}

	// Третья ось — пары (метод, шаблон), объявленные НЕСКОЛЬКИМИ сервисами.
	// Пока они есть, спор решает `moreSpecificBinding`; исчезнут — кейс
	// тай-брейка станет утверждать про единственный биндинг и перестанет что-либо
	// измерять, оставаясь зелёным.
	byPair := map[string]int{}
	for _, b := range loadedHTTPBindings() {
		byPair[b.method+" "+b.template]++
	}
	contested := 0
	for _, n := range byPair {
		if n > 1 {
			contested++
		}
	}
	t.Logf("пар (метод, шаблон) %d, из них объявлены несколькими RPC %d", len(byPair), contested)
	if contested == 0 {
		t.Fatal("пар, объявленных несколькими RPC, нет: кейс тай-брейка «публичный обходит внутренний» беспредметен")
	}
}

// TestDeepWildcardMatchIsInjectable — способность правленого предиката ОТВЕРГАТЬ,
// доказанная на синтетическом шаблоне: одно-фактные отступления от совпадающего
// пути обязаны давать «не совпало».
//
// Синтетика здесь законна и необходима: формы, которых в таблице сегодня нет
// (несколько сегментов под `**`, пустое покрытие), обязаны судиться верно ДО
// того, как первый такой биндинг заведут.
func TestDeepWildcardMatchIsInjectable(t *testing.T) {
	route := internalRoute{method: "GET", segs: parsePathTemplate("/a/{x=**}/tail")}
	split := func(p string) []string { return strings.Split(strings.TrimPrefix(p, "/"), "/") }

	cases := []struct {
		path string
		want bool
		why  string
	}{
		{"/a/one/tail", true, "ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: один сегмент под подстановкой"},
		{"/a/one/two/tail", true, "ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: несколько сегментов под подстановкой"},
		{"/a/one/other", false, "ОДИН факт против первого: хвост не тот"},
		{"/a/tail", false, "ОДИН факт против первого: подстановке нечего покрыть"},
		{"/a/one/tail/extra", false, "ОДИН факт против первого: хвост не последний"},
	}
	for _, c := range cases {
		if got := route.matches("GET", split(c.path)); got != c.want {
			t.Errorf("GET %s против /a/{x=**}/tail → %v, ожидалось %v (%s)", c.path, got, c.want, c.why)
		}
	}
	t.Logf("шаблонов 1; путей проверено %d", len(cases))
}
