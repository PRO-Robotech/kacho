// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

// Приёмка группы целей — ресурса с НАБОРОМ вместо списка, состав которого приводится
// своими действиями края, а следствие этих действий видно не сразу.
//
// Три механики, которых нет ни у одного простого ресурса, и каждая ломала провайдер:
//
//  1. набор, а не список: край не сохраняет порядок целей, поэтому перестановка не
//     является изменением, а сравнение по индексу давало бы вечный дрейф плана;
//  2. состав меняется парой действий, и порядок пары ОБРАТЕН сетевому: сначала снятие,
//     потом добавление, потому что добавление на существующую личность край принимает как
//     no-op и вес НЕ меняет;
//  3. завершённость операции не означает видимость её следствия: цель исчезает из чтения
//     позже, чем завершается снятие, и провайдер обязан дождаться сходимости.

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	tfstate "github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/PRO-Robotech/kacho/pkg/ids"
)

// edgeKindTargetGroup — как поддельный край обслуживает группу целей.
func edgeKindTargetGroup() *edgeKind {
	return &edgeKind{
		Path:        targetGroupsPath,
		Name:        "TargetGroup",
		IDPrefix:    ids.PrefixTargetGroup,
		MetadataKey: "targetGroupId",
		ListKey:     "targetGroups",
		Scope:       "projectId",

		Create: func(_ *fakeEdge, id string, req edgeObject) (edgeObject, error) {
			if edgeStr(req, "regionId") == "" {
				return nil, fmt.Errorf("regionId: required")
			}
			port := edgeInt(req, "port")
			if port <= 0 || port > 65535 {
				return nil, fmt.Errorf("port: must be within 1..65535")
			}
			hc, err := edgeTGHealth(edgeMap(req, "healthCheck"), port)
			if err != nil {
				return nil, err
			}
			return edgeObject{
				"id":        id,
				"projectId": edgeStr(req, "projectId"),
				"regionId":  edgeStr(req, "regionId"),
				"name":      edgeStr(req, "name"),
				// Незаданные поля — нулями: пустая строка и пустая карта, а не отсутствие.
				"description": edgeStr(req, "description"),
				"labels":      edgeMap(req, "labels"),
				// 32-разрядное целое — ЧИСЛОМ (protojson), в отличие от порогов ниже.
				"port":      port,
				"status":    "ACTIVE",
				"createdAt": edgeNow(),
				// Умолчания края, а не провайдера: он их не подставляет и обязан взять
				// обратным чтением.
				"deregistrationDelay": edgeDefaultDuration(req, "deregistrationDelay", "300s"),
				"slowStart":           edgeDefaultDuration(req, "slowStart", "0s"),
				"healthCheck":         hc,
				"targets":             edgeTGTargets(req["targets"]),
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
			case "port":
				port := edgeInt(req, "port")
				obj["port"] = port
				hc, err := edgeTGHealth(edgeMap(obj, "healthCheck"), port)
				if err != nil {
					return err
				}
				obj["healthCheck"] = hc
			case "deregistrationDelay":
				obj["deregistrationDelay"] = edgeDefaultDuration(req, "deregistrationDelay", "300s")
			case "slowStart":
				obj["slowStart"] = edgeDefaultDuration(req, "slowStart", "0s")
			case "healthCheck":
				hc, err := edgeTGHealth(edgeMap(req, "healthCheck"), edgeInt(obj, "port"))
				if err != nil {
					return err
				}
				obj["healthCheck"] = hc
			case "targets":
				// Дословно то, чем отвечает край: состав правится своими действиями, и у
				// них отдельные права. Молчаливое применение здесь скрыло бы, что
				// провайдер пошёл не той дорогой.
				return fmt.Errorf("targets must be modified via AddTargets / RemoveTargets")
			default:
				return fmt.Errorf("%s is immutable after TargetGroup.Create", field)
			}
			return nil
		},

		Verbs: map[string]func(*fakeEdge, *edgeRow, edgeObject) error{
			"addTargets":    edgeTGAdd,
			"removeTargets": edgeTGRemove,
		},
		// Следствие действия над составом видно НЕ сразу — единственный вид, у которого
		// лаг измерен. Ставить его прочим значило бы выдумать свойство края.
		LagAfterVerb: edgeLagTargetComposition,

		Delete: func(_ *fakeEdge, row *edgeRow) error {
			if n := len(edgeTGList(row.obj)); n > 0 {
				return fmt.Errorf("TargetGroup has %d target(s); remove them first", n)
			}
			return nil
		},

		// Край НЕ сохраняет порядок целей: он читает их упорядоченными по времени
		// создания и идентификатору. Разворот здесь — самый дешёвый способ утверждать
		// это свойство на каждом чтении: настройка, закладывающаяся на порядок,
		// покраснеет сразу, а не однажды в проде.
		ReadView: edgeTGReadView,
	}
}

// edgeTGHealth собирает эхо проверки живости.
//
// Пороги — 64-разрядные целые и уезжают СТРОКОЙ, порт пробы тоже; действующий порт —
// 32-разрядный и уезжает числом. Одна структура несёт оба вида: так и есть у protojson, и
// провайдер обязан разбирать оба.
func edgeTGHealth(in edgeObject, groupPort int64) (edgeObject, error) {
	out := edgeObject{
		"interval":           edgeDefaultDuration(in, "interval", "2s"),
		"timeout":            edgeDefaultDuration(in, "timeout", "1s"),
		"healthyThreshold":   fmt.Sprintf("%d", edgeDefaultInt(in, "healthyThreshold", 2)),
		"unhealthyThreshold": fmt.Sprintf("%d", edgeDefaultInt(in, "unhealthyThreshold", 2)),
	}
	probe, name := edgeObject(nil), ""
	for _, p := range []string{"tcp", "http", "https", "grpc"} {
		if v, ok := in[p].(map[string]any); ok {
			if name != "" {
				return nil, fmt.Errorf("health_check: exactly one probe of tcp, http, https, grpc is required")
			}
			probe, name = v, p
		}
	}
	if name == "" {
		return nil, fmt.Errorf("health_check: exactly one probe of tcp, http, https, grpc is required")
	}
	port := edgeInt(probe, "port")
	effective := port
	if effective == 0 {
		effective = groupPort
	}
	echo := edgeObject{"port": fmt.Sprintf("%d", port)}
	switch name {
	case "http", "https":
		echo["path"] = edgeStr(probe, "path")
		echo["expectedCodes"] = edgeStr(probe, "expectedCodes")
		echo["host"] = edgeStr(probe, "host")
		echo["headers"] = edgeMap(probe, "headers")
	case "grpc":
		echo["serviceName"] = edgeStr(probe, "serviceName")
	}
	out[name] = echo
	out["effectivePort"] = effective
	return out, nil
}

// edgeDefaultDuration — длительность из тела либо умолчание края. Отсутствие и пустота
// одно и то же: protojson опускает незаданное поле-сообщение.
func edgeDefaultDuration(in edgeObject, field, fallback string) string {
	if s, ok := in[field].(string); ok && s != "" {
		return s
	}
	return fallback
}

func edgeDefaultInt(in edgeObject, field string, fallback int64) int64 {
	if _, ok := in[field]; !ok {
		return fallback
	}
	if n := edgeInt(in, field); n != 0 {
		return n
	}
	return fallback
}

// edgeTGTargets приводит цели запроса к форме ответа: незаданные формы адресации
// приезжают НУЛЯМИ (пустая строка), вес по умолчанию — нулём.
func edgeTGTargets(v any) []any {
	list, _ := v.([]any)
	out := make([]any, 0, len(list))
	for _, item := range list {
		t, ok := item.(map[string]any)
		if !ok {
			continue
		}
		echo := edgeObject{
			"instanceId": edgeStr(t, "instanceId"),
			"nicId":      edgeStr(t, "nicId"),
			"weight":     edgeInt(t, "weight"),
		}
		if ip, ok := t["ipRef"].(map[string]any); ok {
			echo["ipRef"] = edgeObject{"subnetId": edgeStr(ip, "subnetId"), "address": edgeStr(ip, "address")}
		}
		if ip, ok := t["externalIp"].(map[string]any); ok {
			echo["externalIp"] = edgeObject{"address": edgeStr(ip, "address"), "zoneId": edgeStr(ip, "zoneId")}
		}
		out = append(out, map[string]any(echo))
	}
	return out
}

func edgeTGList(obj edgeObject) []any {
	list, _ := obj["targets"].([]any)
	return list
}

// edgeTargetKey — личность И вес, тем же составом, что у провайдера: добавление на
// существующую личность край принимает как no-op и вес не меняет, поэтому смена веса —
// это снятие и добавление, а не правка.
func edgeTargetKey(t map[string]any) string {
	weight := fmt.Sprintf("|%d", edgeInt(t, "weight"))
	if ip, ok := t["ipRef"].(map[string]any); ok {
		return "ip|" + edgeStr(ip, "subnetId") + "|" + edgeStr(ip, "address") + weight
	}
	if ip, ok := t["externalIp"].(map[string]any); ok {
		return "ext|" + edgeStr(ip, "address") + "|" + edgeStr(ip, "zoneId") + weight
	}
	if s := edgeStr(t, "nicId"); s != "" {
		return "nic|" + s + weight
	}
	return "ins|" + edgeStr(t, "instanceId") + weight
}

// edgeTargetIdentity — ТОЛЬКО личность, без веса: по ней край решает, что цель уже есть.
func edgeTargetIdentity(t map[string]any) string {
	k := edgeTargetKey(t)
	if i := len(k) - len(fmt.Sprintf("|%d", edgeInt(t, "weight"))); i > 0 {
		return k[:i]
	}
	return k
}

// edgeTGAdd — добавление. Личность, которая уже есть, край принимает как no-op и вес НЕ
// меняет. Свойство измерено на живом крае и воспроизведено здесь дословно: без него
// обратный порядок пары действий выглядел бы рабочим.
func edgeTGAdd(_ *fakeEdge, row *edgeRow, req edgeObject) error {
	have := edgeTGList(row.obj)
	known := map[string]bool{}
	for _, item := range have {
		if t, ok := item.(map[string]any); ok {
			known[edgeTargetIdentity(t)] = true
		}
	}
	add := edgeTGTargets(req["targets"])
	if len(add) == 0 {
		return fmt.Errorf("targets: required")
	}
	for _, item := range add {
		t, _ := item.(map[string]any)
		if known[edgeTargetIdentity(t)] {
			continue
		}
		have = append(have, item)
	}
	row.obj["targets"] = have
	return nil
}

// edgeTGRemove — снятие по ЛИЧНОСТИ: край не спрашивает вес, чтобы снять цель.
func edgeTGRemove(_ *fakeEdge, row *edgeRow, req edgeObject) error {
	drop := map[string]bool{}
	for _, item := range edgeTGTargets(req["targets"]) {
		if t, ok := item.(map[string]any); ok {
			drop[edgeTargetIdentity(t)] = true
		}
	}
	if len(drop) == 0 {
		return fmt.Errorf("targets: required")
	}
	kept := make([]any, 0, len(edgeTGList(row.obj)))
	for _, item := range edgeTGList(row.obj) {
		t, ok := item.(map[string]any)
		if ok && drop[edgeTargetIdentity(t)] {
			continue
		}
		kept = append(kept, item)
	}
	row.obj["targets"] = kept
	return nil
}

// edgeTGReadView разворачивает порядок целей на каждом чтении.
func edgeTGReadView(obj edgeObject) edgeObject {
	out := edgeCopy(obj)
	list := edgeTGList(out)
	for i, j := 0, len(list)-1; i < j; i, j = i+1, j-1 {
		list[i], list[j] = list[j], list[i]
	}
	out["targets"] = list
	return out
}

// accSeedTargetGroup кладёт группу целей в край, минуя Terraform.
func accSeedTargetGroup(t *testing.T, e *fakeEdge, projectID, name string) string {
	t.Helper()
	hc, err := edgeTGHealth(edgeObject{"http": map[string]any{}}, 80)
	if err != nil {
		t.Fatalf("посев проверки живости: %v", err)
	}
	return e.Insert(edgeKindTargetGroup(), "", edgeObject{
		"projectId": projectID, "regionId": "ru-central1", "name": name,
		"description": "", "labels": map[string]any{}, "port": int64(80),
		"status": "ACTIVE", "createdAt": edgeNow(),
		"deregistrationDelay": "0s", "slowStart": "0s",
		"healthCheck": hc, "targets": []any{},
	})
}

// ---- пробы -----------------------------------------------------------------------------

const accTGProject = "prj-acceptance-nlb"

func accTGConfig(e *fakeEdge, name, targets string) string {
	return accProvider(e) + fmt.Sprintf(`
resource "kacho_nlb_target_group" "t" {
  project_id           = %q
  region_id            = "ru-central1"
  name                 = %q
  port                 = 80
  deregistration_delay = "0s"
  slow_start           = "0s"

  health_check = {
    interval            = "2s"
    timeout             = "1s"
    healthy_threshold   = 2
    unhealthy_threshold = 2
    http = {
      path = "/healthz"
    }
  }

  targets = %s
}
`, accTGProject, name, targets)
}

const (
	accTGTargetsTwo = `[
    { external_ip = { address = "203.0.113.10", zone_id = "ru-central1-a" }, weight = 50 },
    { external_ip = { address = "203.0.113.11", zone_id = "ru-central1-a" } },
  ]`
	// Тот же состав, ПЕРЕСТАВЛЕННЫЙ. Набор порядка не несёт, поэтому план обязан остаться
	// пустым; список объявил бы перестановку изменением.
	accTGTargetsSwapped = `[
    { external_ip = { address = "203.0.113.11", zone_id = "ru-central1-a" } },
    { external_ip = { address = "203.0.113.10", zone_id = "ru-central1-a" }, weight = 50 },
  ]`
	// Настоящее изменение: у первой цели другой вес, второй нет вовсе.
	accTGTargetsChanged = `[
    { external_ip = { address = "203.0.113.10", zone_id = "ru-central1-a" }, weight = 70 },
  ]`
)

// Полный цикл: создание → пустой план → импорт → перестановка (НЕ изменение) → смена
// состава → удаление, которому предшествует опустошение состава.
func TestAcceptanceNLBTargetGroup_Lifecycle(t *testing.T) {
	e := newFakeEdge(t, edgeKindTargetGroup())
	var tgID string
	readsBefore := 0

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: accProviderFactories(t),
		CheckDestroy: func(*tfstate.State) error {
			if n := e.CountOf(targetGroupsPath); n != 0 {
				return fmt.Errorf("после destroy у края осталось групп целей: %d", n)
			}
			// Край не удаляет непустую группу, поэтому снятие целей обязано ПРЕДШЕСТВОВАТЬ
			// удалению. Утверждается порядок, а не факт вызова: «сняли» само по себе
			// зеленело бы и на снятии после удаления, которого не бывает.
			v := e.Verbs()
			if len(v) == 0 || v[len(v)-1] != "removeTargets" {
				return fmt.Errorf("последним действием перед удалением было %v, ожидалось removeTargets", v)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: accTGConfig(e, "acc-tg", accTGTargetsTwo),
				Check: resource.ComposeAggregateTestCheckFunc(
					accCaptureAttr("kacho_nlb_target_group.t", "id", &tgID),
					resource.TestCheckResourceAttr("kacho_nlb_target_group.t", "targets.#", "2"),
					// Действующий порт вычислил КРАЙ: проба живости порта не называла.
					resource.TestCheckResourceAttr("kacho_nlb_target_group.t", "health_check.effective_port", "80"),
					// 64-разрядный порог приехал строкой и стал числом.
					resource.TestCheckResourceAttr("kacho_nlb_target_group.t", "health_check.unhealthy_threshold", "2"),
					resource.TestCheckResourceAttr("kacho_nlb_target_group.t", "status", "ACTIVE"),
					func(*tfstate.State) error {
						e.AssertEveryCreateCarriedIdempotencyKey(t)
						return nil
					},
				),
			},
			{
				ResourceName:      "kacho_nlb_target_group.t",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				// Перестановка состава — НЕ изменение. Шаг обязан дать пустой план;
				// terraform-plugin-testing проверит это сам и упадёт на любом расхождении.
				Config: accTGConfig(e, "acc-tg", accTGTargetsSwapped),
				Check:  resource.TestCheckResourceAttr("kacho_nlb_target_group.t", "targets.#", "2"),
			},
			{
				// Настоящая смена состава: вес первой цели меняется, вторая уходит.
				PreConfig: func() { readsBefore = e.ReadsOf(tgID) },
				Config:    accTGConfig(e, "acc-tg", accTGTargetsChanged),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kacho_nlb_target_group.t", "targets.#", "1"),
					resource.TestCheckResourceAttr("kacho_nlb_target_group.t", "targets.0.weight", "70"),
					func(*tfstate.State) error {
						// Порядок пары ОБРАТЕН сетевому: сначала снятие, потом добавление.
						v := e.Verbs()
						if len(v) < 2 {
							return fmt.Errorf("действий над составом было %d, ожидалось не меньше двух: %v", len(v), v)
						}
						last := v[len(v)-2:]
						if last[0] != "removeTargets" || last[1] != "addTargets" {
							return fmt.Errorf("порядок пары действий %v, ожидалось [removeTargets addTargets]", last)
						}
						// Сходимость состава ждали чтением, а не верой в завершённость
						// операции. Меньше трёх чтений означало бы, что ожидания не было
						// и проба зеленеет на крае без лага.
						if got := e.ReadsOf(tgID) - readsBefore; got < 3 {
							return fmt.Errorf("чтений за шаг %d, ожидалось не меньше трёх: "+
								"сходимости состава не дожидались", got)
						}
						return nil
					},
				),
			},
		},
	})
}

// Край отвергает состав в маске изменения — и это доезжает до пользователя.
//
// Проба нужна не ради текста отказа, а ради того, что провайдер вообще НЕ КЛАДЁТ состав в
// маску: если бы клал, край ответил бы этим отказом и шаг покраснел бы. Здесь тот же
// отказ заказан на ДРУГОМ поле — регион неизменяем, — и его достаточно, чтобы утверждать
// доставку текста края целиком.
func TestAcceptanceNLBTargetGroup_EdgeRefusalOnImmutableReachesTheUser(t *testing.T) {
	e := newFakeEdge(t, edgeKindTargetGroup())
	e.RejectCreate(targetGroupsPath, edgeStatus{
		HTTP: 400, Code: 3,
		Message:  "Illegal argument port",
		Field:    "port",
		FieldWhy: "must be within 1..65535",
	})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: accProviderFactories(t),
		Steps: []resource.TestStep{{
			Config:      accTGConfig(e, "acc-tg-bad", accTGTargetsTwo),
			ExpectError: regexp.MustCompile(`must be within 1\.\.65535`),
		}},
	})
}

// Проба живости обязана быть ровно одна, и провайдер говорит это ДО обращения к краю.
//
// Отказ на этапе проверки настройки дешевле сетевого: он называет поле, а не приезжает
// «invalid argument» из глубины. Проба утверждает именно раннюю остановку — край в этом
// прогоне не получает ни одного запроса.
func TestAcceptanceNLBTargetGroup_TwoProbesRefusedBeforeTheEdge(t *testing.T) {
	e := newFakeEdge(t, edgeKindTargetGroup())

	cfg := accProvider(e) + fmt.Sprintf(`
resource "kacho_nlb_target_group" "t" {
  project_id = %q
  region_id  = "ru-central1"
  name       = "acc-tg-two-probes"
  port       = 80

  health_check = {
    tcp  = { port = 80 }
    http = { path = "/healthz" }
  }
}
`, accTGProject)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: accProviderFactories(t),
		Steps: []resource.TestStep{{
			Config:      cfg,
			ExpectError: regexp.MustCompile(`Проба живости задана не однозначно`),
		}},
	})
	if got := e.Methods(); len(got) != 0 {
		t.Fatalf("край получил %d запросов, ожидалось ноль: отказ обязан быть до сети: %v", len(got), got)
	}
}
