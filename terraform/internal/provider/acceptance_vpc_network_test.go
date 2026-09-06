// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

// Приёмка сети VPC — ресурса, у которого часть желаемого состояния приводится НЕ обычной
// правкой, а парой суффикс-действий края, и провайдер сам считает разницу набора.
//
// Что здесь проверяется такого, чего не проверить пробой отдельной функции: разница
// считается по СОСТОЯНИЮ против ПЛАНА, а состояние приносит обратное чтение. Проба
// функции берёт оба набора из своих рук и потому молчит о том, верно ли провайдер их
// раздобыл.

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	tfstate "github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/PRO-Robotech/kacho/pkg/ids"
)

// edgeKindNetwork — как поддельный край обслуживает сеть.
func edgeKindNetwork() *edgeKind {
	return &edgeKind{
		Path:        networksPath,
		Name:        "Network",
		IDPrefix:    ids.PrefixNetwork,
		MetadataKey: "networkId",
		ListKey:     "networks",
		Scope:       "projectId",

		Create: func(_ *fakeEdge, id string, req edgeObject) (edgeObject, error) {
			if edgeStr(req, "projectId") == "" {
				return nil, fmt.Errorf("projectId: required")
			}
			return edgeObject{
				"id":        id,
				"projectId": edgeStr(req, "projectId"),
				"createdAt": edgeNow(),
				"name":      edgeStr(req, "name"),
				// Незаданные поля — нулями, как у protojson: пустая строка и пустая карта,
				// а не отсутствие ключа.
				"description":            edgeStr(req, "description"),
				"labels":                 edgeMap(req, "labels"),
				"defaultSecurityGroupId": ids.NewID(ids.PrefixSecurityGroup),
				"defaultRouteTableId":    ids.NewID(ids.PrefixRouteTable),
				"ipv4CidrBlocks":         edgeToAny(edgeStrings(req, "ipv4CidrBlocks")),
				"ipv6CidrBlocks":         edgeToAny(edgeStrings(req, "ipv6CidrBlocks")),
			}, nil
		},

		Update: func(_ *fakeEdge, obj, req edgeObject, field string) error {
			switch field {
			case "name":
				obj["name"] = edgeStr(req, "name")
			case "description":
				obj["description"] = edgeStr(req, "description")
			case "labels":
				obj["labels"] = edgeMap(req, "labels")
			default:
				// Блоки адресов в маске изменения край не принимает — у них свои действия.
				// Отказ здесь не выдумка ради строгости: провайдер, положивший их в маску,
				// обязан упасть, а не тихо не применить.
				return fmt.Errorf("%s is immutable after Network.Create", field)
			}
			return nil
		},

		Verbs: map[string]func(*fakeEdge, *edgeRow, edgeObject) error{
			verbAddCidrBlocks:    edgeNetworkAddCidr,
			verbRemoveCidrBlocks: edgeNetworkRemoveCidr,
		},
	}
}

// edgeNetworkAddCidr — действие «добавить блоки». Обе семьи приходят ОДНИМ запросом, и
// пустой вызов край отвергает: провайдер обязан не отправлять действие, которому нечего
// делать.
func edgeNetworkAddCidr(_ *fakeEdge, row *edgeRow, req edgeObject) error {
	v4, v6 := edgeStrings(req, "ipv4CidrBlocks"), edgeStrings(req, "ipv6CidrBlocks")
	if len(v4) == 0 && len(v6) == 0 {
		return fmt.Errorf("ipv4_cidr_blocks or ipv6_cidr_blocks is required")
	}
	row.obj["ipv4CidrBlocks"] = edgeToAny(edgeUnion(edgeAnyStrings(row.obj["ipv4CidrBlocks"]), v4))
	row.obj["ipv6CidrBlocks"] = edgeToAny(edgeUnion(edgeAnyStrings(row.obj["ipv6CidrBlocks"]), v6))
	return nil
}

// edgeNetworkRemoveCidr — действие «снять блоки». Снятие того, чего нет, край отвергает:
// иначе ошибка в вычислении разницы прошла бы молча и осталась бы невидимой.
func edgeNetworkRemoveCidr(_ *fakeEdge, row *edgeRow, req edgeObject) error {
	v4, v6 := edgeStrings(req, "ipv4CidrBlocks"), edgeStrings(req, "ipv6CidrBlocks")
	if len(v4) == 0 && len(v6) == 0 {
		return fmt.Errorf("ipv4_cidr_blocks or ipv6_cidr_blocks is required")
	}
	next4, err := edgeWithout(edgeAnyStrings(row.obj["ipv4CidrBlocks"]), v4)
	if err != nil {
		return err
	}
	next6, err := edgeWithout(edgeAnyStrings(row.obj["ipv6CidrBlocks"]), v6)
	if err != nil {
		return err
	}
	row.obj["ipv4CidrBlocks"] = edgeToAny(next4)
	row.obj["ipv6CidrBlocks"] = edgeToAny(next6)
	return nil
}

func edgeUnion(have, add []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(have)+len(add))
	for _, v := range append(append([]string{}, have...), add...) {
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

func edgeWithout(have, drop []string) ([]string, error) {
	kill := map[string]bool{}
	for _, v := range drop {
		kill[v] = true
	}
	out := make([]string, 0, len(have))
	for _, v := range have {
		if kill[v] {
			delete(kill, v)
			continue
		}
		out = append(out, v)
	}
	for v := range kill {
		return nil, fmt.Errorf("cidr block %s is not declared on this network", v)
	}
	return out, nil
}

// accSeedNetwork кладёт сеть в край, минуя Terraform.
func accSeedNetwork(t *testing.T, e *fakeEdge, projectID, name string) string {
	t.Helper()
	return e.Insert(edgeKindNetwork(), "", edgeObject{
		"projectId": projectID, "createdAt": edgeNow(), "name": name,
		"description": "", "labels": map[string]any{},
		"defaultSecurityGroupId": ids.NewID(ids.PrefixSecurityGroup),
		"defaultRouteTableId":    ids.NewID(ids.PrefixRouteTable),
		"ipv4CidrBlocks":         []any{}, "ipv6CidrBlocks": []any{},
	})
}

// ---- пробы ----------------------------------------------------------------------------

const accNetworkProject = "prj-acceptance-vpc"

func accNetworkConfig(e *fakeEdge, name, description, v4, v6 string) string {
	return accProvider(e) + fmt.Sprintf(`
resource "kacho_vpc_network" "t" {
  project_id       = %q
  name             = %q
  description      = %q
  labels           = { purpose = "acceptance" }
  ipv4_cidr_blocks = %s
  ipv6_cidr_blocks = %s
}
`, accNetworkProject, name, description, v4, v6)
}

// Полный цикл: создание → пустой план → импорт → правка → пустой план → приведение
// супернета парой действий → удаление.
//
// Пустой план после каждого шага проверяет сам terraform-plugin-testing: он прогоняет
// план заново и роняет шаг на любом расхождении. Это и есть то, чего не даёт ручной
// прогон — там расхождение видно только тому, кто на него посмотрел.
func TestAcceptanceVPCNetwork_Lifecycle(t *testing.T) {
	e := newFakeEdge(t, edgeKindNetwork())
	var netID string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: accProviderFactories(t),
		CheckDestroy: func(*tfstate.State) error {
			if n := e.CountOf(networksPath); n != 0 {
				return fmt.Errorf("после destroy у края осталось сетей: %d", n)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: accNetworkConfig(e, "acc-net", "первая", `["10.10.0.0/16"]`, `[]`),
				Check: resource.ComposeAggregateTestCheckFunc(
					accCaptureAttr("kacho_vpc_network.t", "id", &netID),
					resource.TestCheckResourceAttr("kacho_vpc_network.t", "name", "acc-net"),
					resource.TestCheckResourceAttr("kacho_vpc_network.t", "description", "первая"),
					resource.TestCheckResourceAttr("kacho_vpc_network.t", "ipv4_cidr_blocks.#", "1"),
					resource.TestCheckResourceAttr("kacho_vpc_network.t", "ipv4_cidr_blocks.0", "10.10.0.0/16"),
					// Вычисляемое приходит от края, а не выдумывается провайдером.
					resource.TestCheckResourceAttrSet("kacho_vpc_network.t", "default_security_group_id"),
					resource.TestCheckResourceAttrSet("kacho_vpc_network.t", "created_at"),
					func(*tfstate.State) error {
						e.AssertEveryCreateCarriedIdempotencyKey(t)
						return nil
					},
				),
			},
			{
				// Импорт по идентификатору: состояние, собранное ЧТЕНИЕМ, обязано совпасть
				// с состоянием, собранным применением. Расхождение здесь означает поле,
				// которое провайдер записывает из настройки, а не из ответа края.
				ResourceName:      "kacho_vpc_network.t",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				// Правка обычных полей + добавление блоков обеих семей.
				Config: accNetworkConfig(e, "acc-net-2", "вторая",
					`["10.10.0.0/16", "10.20.0.0/16"]`, `["2001:db8::/32"]`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kacho_vpc_network.t", "name", "acc-net-2"),
					resource.TestCheckResourceAttr("kacho_vpc_network.t", "ipv4_cidr_blocks.#", "2"),
					resource.TestCheckResourceAttr("kacho_vpc_network.t", "ipv6_cidr_blocks.#", "1"),
					func(*tfstate.State) error {
						// Идентификатор не сменился: правка имени — не пересоздание.
						return accSameID(e, netID)
					},
				),
			},
			{
				// Замена блока: один добавляется, другой снимается. Порядок пары
				// ЗАФИКСИРОВАН — сначала добавление, потом снятие: обратный проводит сеть
				// через состояние без нужного супернета.
				Config: accNetworkConfig(e, "acc-net-2", "вторая",
					`["10.20.0.0/16", "10.30.0.0/16"]`, `["2001:db8::/32"]`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kacho_vpc_network.t", "ipv4_cidr_blocks.#", "2"),
					func(*tfstate.State) error {
						v := e.Verbs()
						if len(v) < 2 {
							return fmt.Errorf("действий над супернетом было %d, ожидалось не меньше двух: %v", len(v), v)
						}
						last := v[len(v)-2:]
						if last[0] != verbAddCidrBlocks || last[1] != verbRemoveCidrBlocks {
							return fmt.Errorf("порядок пары действий %v, ожидалось [%s %s]",
								last, verbAddCidrBlocks, verbRemoveCidrBlocks)
						}
						return nil
					},
				),
			},
		},
	})
}

// Отказ края доезжает до пользователя ВМЕСТЕ С ИМЕНЕМ ПОЛЯ.
//
// Без блока details отказ звучит «Illegal argument» и не даёт ни одного основания для
// правки настройки. Проба утверждает именно имя поля, а не факт отказа: «apply упал»
// зеленеет и на обрыве соединения.
func TestAcceptanceVPCNetwork_EdgeRejectionNamesTheField(t *testing.T) {
	e := newFakeEdge(t, edgeKindNetwork())
	e.RejectCreate(networksPath, edgeStatus{
		HTTP: 400, Code: 3,
		Message:  "Illegal argument ipv4CidrBlocks",
		Field:    "ipv4_cidr_blocks",
		FieldWhy: "10.10.0.0/33 is not a valid CIDR block",
	})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: accProviderFactories(t),
		Steps: []resource.TestStep{{
			Config:      accNetworkConfig(e, "acc-net-bad", "негодная", `["10.10.0.0/33"]`, `[]`),
			ExpectError: regexp.MustCompile(`ipv4_cidr_blocks`),
		}},
	})
}

// Одиночное «не найдено» НЕ снимает ресурс из состояния.
//
// Тот же ответ приходит при отказе в доступе, и снять по нему значит предложить
// пересоздать целую инфраструктуру. Подтверждение — отдельный вопрос к списку, и здесь
// список ресурс показывает: значит первый ответ был окном прав.
func TestAcceptanceVPCNetwork_SingleNotFoundKeepsTheResource(t *testing.T) {
	e := newFakeEdge(t, edgeKindNetwork())
	var netID string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: accProviderFactories(t),
		Steps: []resource.TestStep{
			{
				Config: accNetworkConfig(e, "acc-net-keep", "жива", `["10.40.0.0/16"]`, `[]`),
				Check:  accCaptureAttr("kacho_vpc_network.t", "id", &netID),
			},
			{
				PreConfig: func() { e.HideFromRead(netID) },
				// Обновление состояния обязано пройти БЕЗ отказа и БЕЗ расхождения:
				// пустой план тут — часть утверждения, а не побочный эффект.
				RefreshState: true,
				Check: resource.ComposeAggregateTestCheckFunc(
					accCheckAttrLate("kacho_vpc_network.t", "id", &netID),
					resource.TestCheckResourceAttr("kacho_vpc_network.t", "name", "acc-net-keep"),
				),
			},
		},
	})
}

// Подтверждённое отсутствие — снимает. Парный положительный к пробе выше.
//
// Сеть удалена мимо Terraform, а в проекте осталась соседняя: контрольная страница
// непуста, значит пообъектная выдача работает, и отсутствие настоящее.
func TestAcceptanceVPCNetwork_ConfirmedAbsenceDropsTheResource(t *testing.T) {
	e := newFakeEdge(t, edgeKindNetwork())
	var netID string

	cfg := func() string {
		return accProvider(e) + fmt.Sprintf(`
resource "kacho_vpc_network" "t" {
  project_id = %[1]q
  name       = "acc-net-gone"
  labels     = { purpose = "acceptance" }
}

resource "kacho_vpc_network" "neighbour" {
  project_id = %[1]q
  name       = "acc-net-neighbour"
  labels     = { purpose = "acceptance" }
}
`, accNetworkProject)
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: accProviderFactories(t),
		Steps: []resource.TestStep{
			{
				Config: cfg(),
				Check:  accCaptureAttr("kacho_vpc_network.t", "id", &netID),
			},
			{
				PreConfig:          func() { e.Forget(netID) },
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
				Check: resource.ComposeAggregateTestCheckFunc(
					accAbsentFromState("kacho_vpc_network.t"),
					// Соседняя сеть при этом на месте: снят ровно пропавший, а не всё
					// подряд. Без этого утверждения проба зеленела бы и на провайдере,
					// стирающем состояние целиком.
					resource.TestCheckResourceAttr("kacho_vpc_network.neighbour", "name", "acc-net-neighbour"),
				),
			},
		},
	})
}

// Список отвечает отказом — apply ОСТАНАВЛИВАЕТСЯ, а не пересоздаёт.
//
// Это событие ПРАВ, а не удаление: продолжить значит предложить пересоздать целую
// инфраструктуру, которая цела.
//
// Третий шаг — ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ к отказу второго. Без него проба состоит из одного
// отрицания: она утверждает, что ошибка возникла, и молчит о том, во что превратилось
// состояние. Права возвращаются — и ресурс обязан оказаться на месте ТЕМ ЖЕ
// идентификатором, а следующий план — пустым; ровно это и означает «остановка, а не
// пересоздание».
//
// Опровергнутая гипотеза, записанная здесь, чтобы её не выводили заново: сперва
// предполагалось, что шаг ловит ещё и ветку «снять с учёта И сообщить об ошибке». Не ловит,
// и ловить нечего — инъекция показала, что при диагностике-ошибке фреймворк отбрасывает
// запись состояния целиком, поэтому такая ветка не проявляется никак. Достижимый дефект
// здесь один: отказ, прочитанный как удаление БЕЗ ошибки, — его берёт ExpectError второго
// шага.
func TestAcceptanceVPCNetwork_DeniedListStopsInsteadOfRecreating(t *testing.T) {
	e := newFakeEdge(t, edgeKindNetwork())
	var netID string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: accProviderFactories(t),
		Steps: []resource.TestStep{
			{
				Config: accNetworkConfig(e, "acc-net-denied", "жива", `["10.50.0.0/16"]`, `[]`),
				Check:  accCaptureAttr("kacho_vpc_network.t", "id", &netID),
			},
			{
				PreConfig: func() {
					e.HideFromRead(netID)
					e.DenyList(networksPath)
				},
				RefreshState: true,
				ExpectError:  regexp.MustCompile(`Доступ к проекту утрачен`),
			},
			{
				// Права вернулись. Сеть цела всё это время — значит она обязана остаться
				// под управлением, а не приехать как новая.
				PreConfig: func() {
					e.Reveal(netID)
					e.AllowList(networksPath)
				},
				Config: accNetworkConfig(e, "acc-net-denied", "жива", `["10.50.0.0/16"]`, `[]`),
				Check:  accCheckAttrLate("kacho_vpc_network.t", "id", &netID),
			},
		},
	})
}

// Отсутствие, которое НЕЧЕМ подтвердить, — остановка, а не догадка.
//
// Единственная сеть проекта пропала: контрольная страница пуста целиком, и «в проекте
// больше ничего нет» неотличимо от «пообъектные права ушли». Цена названа честно:
// единственный ресурс, удалённый вручную, даст ложную остановку — она стоит одной команды
// оператора, а ложное пересоздание уничтожает инфраструктуру.
func TestAcceptanceVPCNetwork_UnconfirmedAbsenceStops(t *testing.T) {
	e := newFakeEdge(t, edgeKindNetwork())
	var netID string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: accProviderFactories(t),
		Steps: []resource.TestStep{
			{
				Config: accNetworkConfig(e, "acc-net-lonely", "одна", `["10.60.0.0/16"]`, `[]`),
				Check:  accCaptureAttr("kacho_vpc_network.t", "id", &netID),
			},
			{
				PreConfig:    func() { e.Forget(netID) },
				RefreshState: true,
				ExpectError:  regexp.MustCompile(`Отсутствие сети не подтверждено`),
			},
			{
				// Появился свидетель — и та же неопределённость разрешилась сама.
				//
				// Шаг нужен не только ради полноты рассказа: без него остановка выше
				// пережила бы свой предмет и до самого удаления, а уборка теста упала бы
				// на том же отказе. Останавливаться навсегда провайдер не должен —
				// достаточно одного ресурса того же вида в области.
				PreConfig:          func() { accSeedNetwork(t, e, accNetworkProject, "acc-net-witness") },
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
				Check:              accAbsentFromState("kacho_vpc_network.t"),
			},
		},
	})
}

// Импорт есть у сети и НАМЕРЕННО отсутствует у ключа служебной учётки и у машины.
//
// Утверждение парное: одностороннее «импорта нет» зеленело бы и на ресурсе, у которого
// его просто забыли.
func TestAcceptanceImportSurfaceIsDeliberate(t *testing.T) {
	accCheckImportable(t, "kacho_vpc_network", NewNetworkResource(), true)
	accCheckImportable(t, "kacho_nlb_target_group", NewNLBTargetGroupResource(), true)
	accCheckImportable(t, "kaname_service_account_key", NewIAMSAKeyResource(), false)
	accCheckImportable(t, "kacho_compute_instance", NewComputeInstanceResource(), false)
}

// accSameID — идентификатор ресурса у края не сменился: правка не обернулась
// пересозданием. Проверяется по КРАЮ, а не по состоянию: состояние пересоздание тоже
// переживёт, только с другим идентификатором внутри.
func accSameID(e *fakeEdge, id string) error {
	if e.Row(id) == nil {
		return fmt.Errorf("сети %s у края больше нет — правка обернулась пересозданием", id)
	}
	return nil
}
