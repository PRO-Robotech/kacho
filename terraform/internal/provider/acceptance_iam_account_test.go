// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

// Приёмка аккаунта — ресурса, у которого владелец НЕ является входом.
//
// Владелец аккаунта выводится краем из вызывающего и присланное значение отвергается
// синхронно, до чеканки операции. Значит вопрос к описанию ресурса ровно один: остаётся ли
// у него исполнимый вход. Проверить это утверждением о форме схемы нельзя — законность
// входа решают ДВА правила сразу (что требует описание и что принимает край), и разойтись
// они могут молча. Поэтому пробы ниже ВЫЗЫВАЮТ ресурс.

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	tfstate "github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/PRO-Robotech/kacho/terraform/internal/client"
)

const (
	accountsCollection = "/iam/v1/accounts"

	// accAccountOwner — вызывающий, которого край запишет владельцем. Настройка его не
	// называет и назвать не может: поле выходное.
	accAccountOwner = "usr-acceptance00000003"
)

// edgeKindAccount — как поддельный край обслуживает аккаунт.
func edgeKindAccount() *edgeKind {
	return &edgeKind{
		Path:        accountsCollection,
		Name:        "Account",
		IDPrefix:    "acc",
		MetadataKey: "accountId",
		ListKey:     "accounts",

		Create: func(_ *fakeEdge, id string, req edgeObject) (edgeObject, error) {
			// Отказ края воспроизведён дословно
			// (services/iam/internal/apps/kaname/api/account/create.go): владелец —
			// ВЫХОДНОЕ поле, и присланное значение отвергается, даже если это
			// собственный идентификатор вызывающего. Принять и выбросить его было бы
			// запрещённым третьим исходом: вызывающий получил бы успех на ввод, который
			// никуда не применён.
			if edgeStr(req, "ownerUserId") != "" {
				return nil, fmt.Errorf("Illegal argument ownerUserId (derived from caller)")
			}
			return edgeObject{
				"id":          id,
				"ownerUserId": accAccountOwner,
				"name":        edgeStr(req, "name"),
				"description": edgeStr(req, "description"),
				"labels":      edgeMap(req, "labels"),
				"createdAt":   edgeNow(),
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
				// Владелец неизменяем, и отказ здесь не строгость ради строгости:
				// провайдер, положивший его в маску, обязан упасть, а не тихо не
				// применить.
				return fmt.Errorf("%s is immutable after Account.Create", field)
			}
			return nil
		},
	}
}

// ---- пробы ------------------------------------------------------------------------------

func accAccountConfig(e *fakeEdge, name, owner string) string {
	line := ""
	if owner != "" {
		line = fmt.Sprintf("\n  owner_user_id = %q", owner)
	}
	return accProvider(e) + fmt.Sprintf(`
resource "kacho_iam_account" "t" {
  name = %q%s
}
`, name, line)
}

// Аккаунт заводится, НЕ называя владельца, — и владелец приезжает в состояние из ответа.
//
// Другого исполнимого входа у ресурса нет: любое непустое значение край отвергает
// синхронно, каким бы вызывающий ни был. Описание, требовавшее это поле, делало ресурс
// неприменимым ПРИ ЛЮБОМ входе — и заметить это утверждением о схеме нельзя, потому что
// сама по себе она исполнима.
func TestAcceptanceIAMAccount_CreatedWithoutNamingTheOwner(t *testing.T) {
	e := newFakeEdge(t, edgeKindAccount())

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: accProviderFactories(t),
		CheckDestroy: func(*tfstate.State) error {
			if n := e.CountOf(accountsCollection); n != 0 {
				return fmt.Errorf("после destroy у края осталось аккаунтов: %d", n)
			}
			return nil
		},
		Steps: []resource.TestStep{{
			Config: accAccountConfig(e, "acc-probe", ""),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrSet("kacho_iam_account.t", "id"),
				// КТО заполняет поле, которого настройка не называла: край. Без этого
				// утверждения проба зеленела бы и на провайдере, теряющем ответ, —
				// а владелец аккаунта это ровно то, ради чего поле в ресурсе есть.
				resource.TestCheckResourceAttr("kacho_iam_account.t", "owner_user_id", accAccountOwner),
			),
		}},
	})
}

// Что бывает, когда владельца всё-таки НАЗВАЛИ: отказ, и он приходит на plan.
//
// Это положительный контроль к пробе выше — без него та зеленела бы и на описании, которое
// поле принимает и молча выбрасывает. И одновременно это утверждение о ЦЕНЕ ошибки:
// названный владелец отвергается ДО единого обращения к краю, а не после того, как аккаунт
// уже создан. Проба покраснеет в тот день, когда поле снова станет входом.
func TestAcceptanceIAMAccount_NamingTheOwnerIsRefusedAtPlan(t *testing.T) {
	e := newFakeEdge(t, edgeKindAccount())

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: accProviderFactories(t),
		Steps: []resource.TestStep{{
			Config: accAccountConfig(e, "acc-probe-owned", accAccountOwner),
			// Пробелы — через \s+: отказ приезжает переносимым по строкам, и точная
			// подстрока разошлась бы с ним от одной лишь ширины вывода.
			ExpectError: regexp.MustCompile(`owner_user_id[\s\S]*?[Rr]ead-[Oo]nly`),
		}},
	})
}

// Предпосылка обеих проб выше: КРАЙ действительно отвергает названного владельца.
//
// Запрос уходит мимо провайдера — иначе ветвь отказа в подделке не исполнялась бы ни разу
// (провайдер поле не отправляет by construction), и подделка была бы снисходительнее края
// ровно там, где край защищает выходной характер владельца. Рядом — зеркальная сторона:
// тот же запрос БЕЗ владельца проходит, иначе «край отвергает всё» выглядело бы как
// соблюдение контракта.
func TestAcceptanceFakeEdgeRefusesASuppliedAccountOwner(t *testing.T) {
	e := newFakeEdge(t, edgeKindAccount())
	c := mustProviderClient(t, e.URL())
	ctx := t.Context()

	named, err := c.DoRaw(ctx, http.MethodPost, accountsCollection,
		[]byte(`{"name":"с владельцем","ownerUserId":"`+accAccountOwner+`"}`), nil)
	if err != nil {
		t.Fatalf("отправка: %v", err)
	}
	out := client.Classify(named)
	if out.Kind == client.OutcomeOK {
		t.Fatalf("край принял названного владельца: %s", named.Body)
	}
	if !strings.Contains(out.Message, "ownerUserId (derived from caller)") {
		t.Fatalf("отказ не называет ни поля, ни причины: %s", out.Message)
	}

	bare := accPostRaw(t, ctx, c, accountsCollection, []byte(`{"name":"без владельца"}`), "")
	if bare == "" {
		t.Fatal("запрос без владельца не дал операции")
	}
	if n := e.CountOf(accountsCollection); n != 1 {
		t.Fatalf("аккаунтов у края %d, ожидался 1: отвергнутый запрос всё-таки создал строку", n)
	}
}
