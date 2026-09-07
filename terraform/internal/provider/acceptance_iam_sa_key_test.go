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

	// accSAKeyPrincipal — личность, от чьего имени поддельный край считает запрос
	// пришедшим на ЧЕЛОВЕЧЕСКОЙ полосе.
	accSAKeyPrincipal = "usr-acceptance00000001"

	// accSAKeyOwner — владелец аккаунта учётки. На МАШИННОЙ полосе ответственным
	// записывается он: служебная учётка строкой пользователей не является, и назначить
	// ответственным её нельзя.
	accSAKeyOwner = "usr-acceptance00000002"
)

// edgeKindSAKey — как поддельный край обслуживает ключ служебной учётки ЧЕЛОВЕКУ.
//
// Коллекция ВЛОЖЕНА в владельца, и это не украшение пути: область здесь задаёт сам путь,
// поэтому параметра области у списка нет, а два владельца не видят ключей друг друга.
func edgeKindSAKey() *edgeKind { return edgeKindSAKeyAs(false) }

// edgeKindSAKeyMachineCaller — тот же вид, но запрос пришёл от СЛУЖЕБНОЙ УЧЁТКИ.
//
// Полоса вызывающего — не украшение подделки: у человека и у машины край принимает
// РАЗНОЕ, и вход, законный одному, неисполним у другого. Без этой полосы описание
// ресурса нечем сверить с тем, что край примет от конвейера, — а конвейер и есть тот,
// ради кого машинная личность заводится.
func edgeKindSAKeyMachineCaller() *edgeKind { return edgeKindSAKeyAs(true) }

// edgeKindSAKeyAs — вид ключа под заданной полосой вызывающего.
func edgeKindSAKeyAs(machineCaller bool) *edgeKind {
	return &edgeKind{
		Path:        saKeysCollection,
		Name:        "SAKey",
		IDPrefix:    "soc",
		MetadataKey: "keyId",
		ListKey:     "keys",

		Create: func(_ *fakeEdge, id string, req edgeObject) (edgeObject, error) {
			// СТРАЖ ЛИЧНОСТИ КРАЯ — воспроизведён дословно, тексты отказов те же, что у
			// края (services/iam/internal/apps/kaname/api/sa_keys/handler.go, Issue).
			//
			// Прежняя редакция этой подделки требовала названного ответственного и
			// отвергала пустое — то есть закрепляла ровно тот дефект, ради которого
			// ресурс неприменим конвейером. Край НЕ требует его ни на одной полосе:
			// человеку он подставляет вызывающего, у машины непустое ОТВЕРГАЕТ.
			by, err := edgeSAKeyIssuer(machineCaller, edgeStr(req, "createdByUserId"))
			if err != nil {
				return nil, err
			}
			ttl := edgeInt(req, "ttlSeconds")
			expires := ""
			if ttl > 0 {
				expires = edgeNow()
			}
			return edgeObject{
				"id":              id,
				"createdByUserId": by,
				"name":            edgeStr(req, "name"),
				"description":     edgeStr(req, "description"),
				"labels":          edgeMap(req, "labels"),
				"createdAt":       edgeNow(),
				"expiresAt":       expires,
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
		// (services/iam/internal/apps/kaname/api/sa_keys/usecases.go — keyID заводится
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
					"id": row.id,
					// Ответственный уезжает ЗДЕСЬ, и другого источника у состояния нет:
					// на машинной полосе оператор его не называл, а знать его он вправе —
					// это ответ на вопрос «кто числится выпустившим этот ключ».
					"createdByUserId": row.obj["createdByUserId"],
					"expiresAt":       row.obj["expiresAt"],
					"createdAt":       row.obj["createdAt"],
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

// edgeSAKeyIssuer — кого край запишет ответственным и что он отвергнет.
//
// Три исхода, и они разные на двух полосах:
//
//   - машина назвала ответственного — ОТКАЗ: поле на этой полосе не читается вовсе,
//     принять и выбросить его значило бы отдать вызывающему успех на неприменённый ввод;
//   - машина промолчала — ответственным становится владелец аккаунта учётки;
//   - человек промолчал — ответственным становится он сам; назвал чужого — ОТКАЗ.
func edgeSAKeyIssuer(machineCaller bool, supplied string) (string, error) {
	if machineCaller {
		if supplied != "" {
			return "", fmt.Errorf("Illegal argument created_by_user_id: must be empty " +
				"for a service-account caller (created_by is resolved from the service " +
				"account's account owner)")
		}
		return accSAKeyOwner, nil
	}
	switch supplied {
	case "":
		return accSAKeyPrincipal, nil
	case accSAKeyPrincipal:
		return supplied, nil
	default:
		return "", fmt.Errorf(
			"Illegal argument created_by_user_id: must match authenticated principal or be empty")
	}
}

// ---- пробы ------------------------------------------------------------------------------

// accSAKeyConfig — ключ, выпускаемый ЧЕЛОВЕКОМ: ответственный назван, и назван он собой.
func accSAKeyConfig(e *fakeEdge, name string, ttl int64) string {
	return accSAKeyConfigIssuedBy(e, name, ttl, accSAKeyPrincipal)
}

// accSAKeyConfigIssuedBy — то же с ЛЮБЫМ ответственным; пустая строка означает, что
// настройка его не называет вовсе (машинная полоса).
func accSAKeyConfigIssuedBy(e *fakeEdge, name string, ttl int64, issuer string) string {
	line := ""
	if issuer != "" {
		line = fmt.Sprintf("\n  created_by_user_id = %q", issuer)
	}
	return accProvider(e) + fmt.Sprintf(`
resource "kaname_service_account_key" "t" {
  service_account_id = %q%s
  name               = %q
  ttl_seconds        = %d
}
`, accSAID, line, name, ttl)
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
					accCaptureAttr("kaname_service_account_key.t", "id", &keyID),
					accCaptureAttr("kaname_service_account_key.t", "private_key_pem", &secret),
					resource.TestCheckResourceAttr("kaname_service_account_key.t", "algorithm", "RS256"),
					resource.TestCheckResourceAttrSet("kaname_service_account_key.t", "public_key_pem"),
					resource.TestCheckResourceAttrSet("kaname_service_account_key.t", "client_id"),
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
					accCheckAttrLate("kaname_service_account_key.t", "id", &keyID),
					func(s *tfstate.State) error {
						rs := s.RootModule().Resources["kaname_service_account_key.t"]
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
						rs := s.RootModule().Resources["kaname_service_account_key.t"]
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

// Отказ в выпуске объясняется тем, чего из самого отказа не видно, — и обеими причинами.
//
// Причин ровно две, и они разные: невыданное право на учётку и неподтверждённая
// повышенным уровнем личность ЧЕЛОВЕКА. Голое «permission denied» не различает их вовсе,
// а прежняя редакция этой пробы закрепляла ОДНУ и притом неверную: она требовала от
// провайдера сказать, что служебная учётка второй фактор не проходит и ключи заводятся
// человеком. Служебная учётка от порога ОСВОБОЖДЕНА (pkg/grpcsrv/acr.go, EvaluateStepUp:
// машинный принципал разрешается первым же условием), поэтому текст отправлял конвейер
// отказываться от полосы, которая для него и предназначена.
func TestAcceptanceIAMServiceAccountKey_DeniedIssueExplainsBothCauses(t *testing.T) {
	e := newFakeEdge(t, edgeKindSAKey())
	e.RejectCreate(saKeysCollection, edgeStatus{
		HTTP: 403, Code: 7, Message: "permission denied on iam.serviceAccountKey.create",
	})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: accProviderFactories(t),
		Steps: []resource.TestStep{{
			Config: accSAKeyConfig(e, "acc-key-denied", 3600),
			// Обе причины ОБЯЗАНЫ быть названы: текст про одну из них уводит читателя
			// чинить не то. Утверждается и то, что порог назван требованием К ЧЕЛОВЕКУ,
			// и то, что машина от него освобождена.
			ExpectError: regexp.MustCompile(
				`(?s)право.*?(step-up|повышенн).*?освобожден`),
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
				Check:  accCaptureAttr("kaname_service_account_key.t", "id", &keyID),
			},
			{
				PreConfig:          func() { e.Forget(keyID) },
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
				Check:              accAbsentFromState("kaname_service_account_key.t"),
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
				rs := s.RootModule().Resources["kaname_service_account_key.t"]
				id := rs.Primary.Attributes["id"]
				if e.Row(id) == nil {
					return fmt.Errorf("в состоянии идентификатор %q, которого у края нет", id)
				}
				return nil
			},
		}},
	})
}

// Конвейер ходит СЛУЖЕБНОЙ УЧЁТКОЙ, и ключ ей выпускается без названного ответственного.
//
// Это и есть та полоса, ради которой машинная личность заводится: на ней край подставляет
// ответственного сам — служебная учётка строкой пользователей не является, назначить
// ответственным её нечем, — а непустое значение ОТВЕРГАЕТ синхронно.
//
// Проба ВЫЗЫВАЕТ ресурс минимально-законным для этой полосы входом и требует успеха.
// Утверждение о форме схемы («поле необязательно») этот класс не ловит by construction:
// схема бывает исполнимой на бумаге и неисполнимой у края, потому что законность входа
// решают ДВА правила сразу, и разойтись они могут молча.
func TestAcceptanceIAMServiceAccountKey_MachineCallerIssuesWithoutNamingTheIssuer(t *testing.T) {
	e := newFakeEdge(t, edgeKindSAKeyMachineCaller())

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: accProviderFactories(t),
		CheckDestroy: func(*tfstate.State) error {
			if n := e.CountOf(saKeysCollection); n != 0 {
				return fmt.Errorf("после destroy у края осталось ключей: %d", n)
			}
			return nil
		},
		Steps: []resource.TestStep{{
			Config: accSAKeyConfigIssuedBy(e, "acc-key-machine", 3600, ""),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrSet("kaname_service_account_key.t", "id"),
				resource.TestCheckResourceAttrSet("kaname_service_account_key.t", "private_key_pem"),
				// КТО заполняет поле, которого настройка не называла: край, и его ответ
				// доезжает до состояния. Без этого утверждения проба зеленела бы и на
				// провайдере, который поле принял, но потерял, — а состояние молчало бы
				// о том, кто числится выпустившим живой ключ.
				resource.TestCheckResourceAttr("kaname_service_account_key.t",
					"created_by_user_id", accSAKeyOwner),
			),
		}},
	})
}

// Положительный контроль к полосе выше: машина, НАЗВАВШАЯ ответственного, получает отказ.
//
// Без него предыдущая проба зеленела бы и на подделке, которая поле просто не смотрит, —
// то есть не утверждала бы о машинной полосе ничего. Заодно это ответ на вопрос «что
// бывает, когда прислали лишнее»: синхронный отказ с именем поля, а не тихо принятое и
// выброшенное значение.
func TestAcceptanceIAMServiceAccountKey_MachineCallerNamingTheIssuerIsRefused(t *testing.T) {
	e := newFakeEdge(t, edgeKindSAKeyMachineCaller())

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: accProviderFactories(t),
		Steps: []resource.TestStep{{
			Config: accSAKeyConfigIssuedBy(e, "acc-key-machine-named", 3600, accSAKeyPrincipal),
			// Пробелы — через \s+: отказ приезжает переносимым по строкам, и точная
			// подстрока разошлась бы с ним от одной лишь ширины вывода.
			ExpectError: regexp.MustCompile(`must\s+be\s+empty\s+for\s+a\s+service-account\s+caller`),
		}},
	})
}

// Положительный контроль к ЧЕЛОВЕЧЕСКОЙ полосе: чужой ответственный отвергается.
//
// Полоса принимает из тела ровно совпадающее с вызывающим — выпустить ключ «от чужого
// имени» нельзя. Без этой пробы ветвь стража в подделке не исполнялась бы ни разу, и
// подделка была бы снисходительнее края ровно там, где край защищает связь ответственного
// с выданным доступом.
func TestAcceptanceIAMServiceAccountKey_HumanCallerNamingSomeoneElseIsRefused(t *testing.T) {
	e := newFakeEdge(t, edgeKindSAKey())

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: accProviderFactories(t),
		Steps: []resource.TestStep{{
			Config: accSAKeyConfigIssuedBy(e, "acc-key-foreign", 3600, "usr-someone000000001"),
			ExpectError: regexp.MustCompile(
				`must\s+match\s+authenticated\s+principal\s+or\s+be\s+empty`),
		}},
	})
}

// Пустая строка не означает НИ «не назвал», НИ «назвал» — и отвергается на plan.
//
// Незаданное поле законно: край подставит ответственного сам. Заданная пустая строка
// уезжает на провод НЕОТЛИЧИМО от незаданного, поэтому край подставит своё, а настройка
// продолжит утверждать пустоту — и применение кончится отказом каркаса «провайдер выдал
// несогласованный результат», в котором вызывающему предлагают сообщить об ошибке
// ПРОВАЙДЕРА за собственную опечатку. Отказ обязан приходить раньше и называть поле.
func TestAcceptanceIAMServiceAccountKey_EmptyIssuerIsRefusedAtPlan(t *testing.T) {
	e := newFakeEdge(t, edgeKindSAKey())

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: accProviderFactories(t),
		Steps: []resource.TestStep{{
			Config: accProvider(e) + fmt.Sprintf(`
resource "kaname_service_account_key" "t" {
  service_account_id = %q
  created_by_user_id = ""
  name               = "acc-key-empty-issuer"
}
`, accSAID),
			ExpectError: regexp.MustCompile(`created_by_user_id[\s\S]*?пуст`),
		}},
	})
}
