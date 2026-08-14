// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

// Приёмка ключа служебной учётки — ресурса, несущего СЕКРЕТ.
//
// Три свойства, которых нет ни у одного обычного ресурса, и каждое проверяется здесь:
//
//  1. закрытый ключ выдаётся ОДИН раз, в ответе операции, и повторно не читается ничем;
//  2. поэтому обратное чтение подтверждает только СУЩЕСТВОВАНИЕ ключа и НИКОГДА не
//     трогает материал — иначе обычный refresh уничтожил бы единственный экземпляр
//     секрета руками того, кто ничего не менял;
//  3. импорта нет намеренно: импортировать можно то, что читается, а материал край не
//     отдаёт повторно.
//
// Первое из трёх — единственное, что проверяется здесь ЛУЧШЕ, чем на живом стенде: на
// стенде «ключ уцелел после refresh» подтверждается взглядом в файл состояния, и никто не
// заметит, если однажды он перестанет уцелевать.

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	tfstate "github.com/hashicorp/terraform-plugin-testing/terraform"
)

const (
	accSAID = "sva-acceptance000001"
	// saKeysCollection — объявление пути с подстановочным сегментом. Он ОБЯЗАН совпадать
	// с тем, что строит saKeysPath: разойдись они — поддельный край ответил бы отказом
	// «путь не обслуживается», и проба упала бы по причине, к предмету не относящейся.
	saKeysCollection = "/iam/v1/serviceAccounts/{}/keys"
)

// edgeKindSAKey — как поддельный край обслуживает ключ служебной учётки.
//
// Коллекция ВЛОЖЕНА в владельца, и это не украшение пути: область здесь задаёт сам путь,
// поэтому параметра области у списка нет, а два владельца не видят ключей друг друга.
func edgeKindSAKey() *edgeKind {
	return &edgeKind{
		Path:        saKeysCollection,
		Name:        "SAKey",
		IDPrefix:    "soc",
		MetadataKey: "keyId",
		ListKey:     "keys",

		Create: func(_ *fakeEdge, id string, req edgeObject) (edgeObject, error) {
			if edgeStr(req, "createdByUserId") == "" {
				// Край требует названного ответственного явно: выводить его из
				// предъявителя токена значило бы терять связь.
				return nil, fmt.Errorf("createdByUserId: required")
			}
			ttl := edgeInt(req, "ttlSeconds")
			expires := ""
			if ttl > 0 {
				expires = edgeNow()
			}
			return edgeObject{
				"id":          id,
				"name":        edgeStr(req, "name"),
				"description": edgeStr(req, "description"),
				"labels":      edgeMap(req, "labels"),
				"createdAt":   edgeNow(),
				"expiresAt":   expires,
				// Материал чеканится ОДИН раз и уезжает только в ответе операции: в самой
				// строке его нет, поэтому списочное чтение его вернуть не может даже по
				// ошибке. Так и у настоящего края.
				"__secret":    "-----BEGIN PRIVATE KEY-----\n" + id + "\n-----END PRIVATE KEY-----",
				"__clientId":  "uoc" + id[3:],
				"__audiences": edgeToAny(edgeStrings(req, "audience")),
			}, nil
		},

		// Изменяющей операции у выпущенного ключа нет: любое поле входа пересоздаёт его.
		// Провайдер до этого метода не доходит, и отказ здесь — страж на случай, если
		// пометка «пересоздаёт» однажды исчезнет со схемы.
		Update: func(_ *fakeEdge, _, _ edgeObject, field string) error {
			return fmt.Errorf("%s is immutable: an issued key has no update operation", field)
		},

		// Полезная нагрузка успешной операции. Другого случая получить материал не будет.
		//
		// keyId РАВЕН key.id, и это не упрощение подделки, а измеренный контракт: край
		// чеканит один идентификатор и кладёт его И в строку, И в поле подписи
		// (services/iam/internal/apps/kacho/api/sa_keys/usecases.go — keyID заводится
		// один раз и уезжает обоими путями; та же мысль записана в самом контракте, у
		// ServiceAccountOAuthClient.id: «credential-id/JWK kid»).
		//
		// Первая редакция этой подделки выдумала здесь ОТДЕЛЬНЫЙ идентификатор подписи —
		// по комментарию провайдера, который называет их разными вещами. Пробы покраснели
		// сразу и громко: провайдер берёт keyId, и по выдуманному идентификатору ключ не
		// отзывался. Красным был не провайдер, а подделка; комментарий провайдера при
		// этом остаётся неверным — на дереве это ОДНО значение.
		OpResponse: func(row *edgeRow) edgeObject {
			return edgeObject{
				"clientId":      row.obj["__clientId"],
				"keyId":         row.id,
				"algorithm":     "RS256",
				"publicKeyPem":  "-----BEGIN PUBLIC KEY-----\n" + row.id + "\n-----END PUBLIC KEY-----",
				"privateKeyPem": row.obj["__secret"],
				"audiences":     row.obj["__audiences"],
				"key": map[string]any{
					"id":        row.id,
					"expiresAt": row.obj["expiresAt"],
					"createdAt": row.obj["createdAt"],
				},
			}
		},

		// Выдача СРЕЗАЕТ служебные поля: наружу край отдаёт только то, что читается
		// повторно. Секрет в списочном ответе был бы подделкой, оправдывающей провайдер,
		// который его оттуда читает.
		ReadView: func(obj edgeObject) edgeObject {
			out := edgeObject{}
			for k, v := range obj {
				if len(k) > 2 && k[:2] == "__" {
					continue
				}
				out[k] = v
			}
			return out
		},
	}
}

// ---- пробы ------------------------------------------------------------------------------

func accSAKeyConfig(e *fakeEdge, name string, ttl int64) string {
	return accProvider(e) + fmt.Sprintf(`
resource "kacho_iam_service_account_key" "t" {
  service_account_id = %q
  created_by_user_id = "usr-acceptance00000001"
  name               = %q
  ttl_seconds        = %d
}
`, accSAID, name, ttl)
}

// Секрет выдаётся один раз, переживает обновление состояния и меняется вместе с ключом.
func TestAcceptanceIAMServiceAccountKey_SecretSurvivesRefreshAndDiesWithTheKey(t *testing.T) {
	e := newFakeEdge(t, edgeKindSAKey())
	var keyID, secret string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: accProviderFactories(t),
		CheckDestroy: func(*tfstate.State) error {
			if n := e.CountOf(saKeysCollection); n != 0 {
				return fmt.Errorf("после destroy у края осталось ключей: %d", n)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: accSAKeyConfig(e, "acc-key", 3600),
				Check: resource.ComposeAggregateTestCheckFunc(
					accCaptureAttr("kacho_iam_service_account_key.t", "id", &keyID),
					accCaptureAttr("kacho_iam_service_account_key.t", "private_key_pem", &secret),
					resource.TestCheckResourceAttr("kacho_iam_service_account_key.t", "algorithm", "RS256"),
					resource.TestCheckResourceAttrSet("kacho_iam_service_account_key.t", "public_key_pem"),
					resource.TestCheckResourceAttrSet("kacho_iam_service_account_key.t", "client_id"),
					func(*tfstate.State) error {
						// Секрет в состоянии — ТОТ, что край выдал, а не пустая строка,
						// которая тоже прошла бы проверку «атрибут задан».
						if want := e.Row(keyID)["__secret"]; secret != want {
							return fmt.Errorf("в состоянии не тот материал: %q против %q", secret, want)
						}
						return nil
					},
				),
			},
			{
				// ГЛАВНОЕ утверждение ресурса: обновление состояния подтверждает
				// существование ключа и НЕ трогает материал. Обычное «прочитать и
				// разложить» затёрло бы его пустотой.
				RefreshState: true,
				Check: resource.ComposeAggregateTestCheckFunc(
					accCheckAttrLate("kacho_iam_service_account_key.t", "id", &keyID),
					func(s *tfstate.State) error {
						rs := s.RootModule().Resources["kacho_iam_service_account_key.t"]
						if got := rs.Primary.Attributes["private_key_pem"]; got != secret {
							return fmt.Errorf("обновление состояния изменило материал: было %q, стало %q",
								secret, got)
						}
						return nil
					},
				),
			},
			{
				// Правка входа ПЕРЕСОЗДАЁТ ключ: изменяющей операции у выпущенного нет.
				// Новый ключ несёт ДРУГОЙ материал — тихая подмена под тем же адресом
				// ресурса сломала бы всех, кто им пользуется, и потому обязана быть
				// пересозданием, а не правкой.
				Config: accSAKeyConfig(e, "acc-key", 7200),
				Check: resource.ComposeAggregateTestCheckFunc(
					func(s *tfstate.State) error {
						rs := s.RootModule().Resources["kacho_iam_service_account_key.t"]
						if rs.Primary.Attributes["id"] == keyID {
							return fmt.Errorf("идентификатор не сменился: правка не обернулась пересозданием")
						}
						if rs.Primary.Attributes["private_key_pem"] == secret {
							return fmt.Errorf("материал не сменился: пересоздание выдало прежний секрет")
						}
						return nil
					},
					func(*tfstate.State) error {
						// Прежний ключ отозван, а не оставлен жить рядом.
						if e.Row(keyID) != nil {
							return fmt.Errorf("прежний ключ %s у края остался: пересоздание не отозвало его", keyID)
						}
						return nil
					},
				),
			},
		},
	})
}

// Отказ в выпуске объясняется тем, чего из самого отказа не видно.
//
// Выпуск ключа требует повышенного подтверждения личности, и токен служебной учётки его
// не несёт. Провайдер обязан сказать это словами: голое «permission denied» отправляет
// читателя чинить права, которых достаточно.
func TestAcceptanceIAMServiceAccountKey_DeniedIssueExplainsStepUp(t *testing.T) {
	e := newFakeEdge(t, edgeKindSAKey())
	e.RejectCreate(saKeysCollection, edgeStatus{
		HTTP: 403, Code: 7, Message: "permission denied on iam.serviceAccountKey.create",
	})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: accProviderFactories(t),
		Steps: []resource.TestStep{{
			Config:      accSAKeyConfig(e, "acc-key-denied", 3600),
			ExpectError: regexp.MustCompile(`step-up`),
		}},
	})
}

// Ключа нет в списке владельца — он отозван, и это ПОЛНОЕ подтверждение.
//
// Здесь «не найдено» действительно означает отсутствие, и подтверждать нечем не надо:
// список прочитан целиком и принадлежит ровно тому владельцу, у которого ключ и висел.
// Отличие от сети названо ради того, чтобы разницу не приняли за непоследовательность.
func TestAcceptanceIAMServiceAccountKey_RevokedOutsideIsDroppedFromState(t *testing.T) {
	e := newFakeEdge(t, edgeKindSAKey())
	var keyID string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: accProviderFactories(t),
		Steps: []resource.TestStep{
			{
				Config: accSAKeyConfig(e, "acc-key-revoked", 3600),
				Check:  accCaptureAttr("kacho_iam_service_account_key.t", "id", &keyID),
			},
			{
				PreConfig:          func() { e.Forget(keyID) },
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
				Check:              accAbsentFromState("kacho_iam_service_account_key.t"),
			},
		},
	})
}

// В состояние попадает АДРЕСУЕМЫЙ идентификатор — тот, которым ключ отзывается.
//
// Подмена его чем-то похожим выглядит рабочей ровно до первого destroy: отзыв уходит на
// строку, которой у края нет, а следующее обновление состояния тихо снимает ресурс, будто
// его отозвали. Проба утверждает адресуемость с двух сторон: край знает эту строку СЕЙЧАС,
// и удаление по ней действительно снимает ключ.
func TestAcceptanceIAMServiceAccountKey_StateHoldsTheAddressableID(t *testing.T) {
	e := newFakeEdge(t, edgeKindSAKey())

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: accProviderFactories(t),
		CheckDestroy: func(*tfstate.State) error {
			// Вторая сторона утверждения: отзыв ДОЕХАЛ. Без неё проба зеленела бы и на
			// идентификаторе, который край знает, но по которому ничего не удаляется.
			if n := e.CountOf(saKeysCollection); n != 0 {
				return fmt.Errorf("после destroy у края осталось ключей: %d — "+
					"отзыв ушёл не по тому идентификатору", n)
			}
			return nil
		},
		Steps: []resource.TestStep{{
			Config: accSAKeyConfig(e, "acc-key-id", 0),
			Check: func(s *tfstate.State) error {
				rs := s.RootModule().Resources["kacho_iam_service_account_key.t"]
				id := rs.Primary.Attributes["id"]
				if e.Row(id) == nil {
					return fmt.Errorf("в состоянии идентификатор %q, которого у края нет", id)
				}
				return nil
			},
		}},
	})
}
