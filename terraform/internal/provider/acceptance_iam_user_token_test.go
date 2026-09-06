// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

// Приёмка личного токена пользователя — ресурса, несущего СЕКРЕТ и не имевшего до сих
// пор ни одной пробы, исполняющей цикл terraform.
//
// # Почему оснастка заведена вместе с правкой текстов, а не до неё
//
// Предмет правки — ТЕКСТ, который читает оператор, получив отказ. Утверждение о форме
// схемы такой текст не проверяет by construction: он рождается только на пути, где край
// уже ответил отказом, — то есть его нужно ВЫЗВАТЬ. Правка текста без пробы, которая его
// читает, была бы той же формой без содержания, что и сам неверный текст: следующая
// редакция вернула бы прежнее утверждение, и никто бы не заметил.
//
// # Что здесь проверяется сверх текстов
//
//  1. МАШИННАЯ ПОЛОСА ИСПОЛНИМА. Конвейер, ходящий служебной учёткой, выпускает токен,
//     не называя ответственного, — и ответственный приезжает в состояние из ответа края.
//  2. ОБРАТНОЕ ЧТЕНИЕ НЕ ТРОГАЕТ МАТЕРИАЛ. Закрытый ключ выдаётся один раз; обычное
//     «прочитать и разложить» затёрло бы его пустотой руками того, кто сделал refresh.
//  3. ЧТЕНИЕ ИДЁТ ПЕРЕЧИСЛЕНИЕМ. Чтения по идентификатору у токена нет вовсе, и
//     отсутствие в УСПЕШНОМ списке владельца — единственный путь, на котором ресурс
//     снимается из состояния.

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	tfstate "github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/PRO-Robotech/kacho/terraform/internal/client"
)

const (
	// userTokensCollection — объявление пути с подстановочным сегментом. Он ОБЯЗАН
	// совпадать с тем, что строит userTokensPath: разойдись они — поддельный край
	// ответил бы «путь не обслуживается», и проба упала бы по причине, к предмету не
	// относящейся.
	userTokensCollection = "/iam/v1/users/{}/tokens"

	// accTokenUser — человек, которому выпускается токен. Он же — вызывающий на
	// ЧЕЛОВЕЧЕСКОЙ полосе, и это не упрощение подделки: право выпускать личные токены
	// человека есть свойство его строки личности, а не выдаваемое право (модель прав,
	// `token_issuer: subject`). Взять сюда другого человека значило бы описать полосу,
	// которой у края не бывает.
	accTokenUser = "usr-acceptance00000011"

	// accTokenStranger — посторонний, которого настройка называет ответственным. Край
	// такое отвергает синхронно; без этой полосы ветвь стража в подделке не
	// исполнялась бы ни разу.
	accTokenStranger = "usr-acceptance00000012"
)

// edgeKindUserToken — как поддельный край обслуживает личный токен ЧЕЛОВЕКУ.
func edgeKindUserToken() *edgeKind { return edgeKindUserTokenAs(false) }

// edgeKindUserTokenMachineCaller — тот же вид, но запрос пришёл от СЛУЖЕБНОЙ УЧЁТКИ.
//
// Полоса вызывающего — не украшение подделки: у человека и у машины край принимает
// РАЗНОЕ, и вход, законный одному, у другого обрабатывается иначе. Без этой полосы
// описание ресурса нечем сверить с тем, что край примет от конвейера.
func edgeKindUserTokenMachineCaller() *edgeKind { return edgeKindUserTokenAs(true) }

func edgeKindUserTokenAs(machineCaller bool) *edgeKind {
	return &edgeKind{
		Path:        userTokensCollection,
		Name:        "UserToken",
		IDPrefix:    "uoc",
		MetadataKey: "keyId",
		ListKey:     "tokens",

		Create: func(_ *fakeEdge, id string, req edgeObject) (edgeObject, error) {
			// СТРАЖ ЛИЧНОСТИ КРАЯ — воспроизведён дословно, включая расхождение полос
			// (services/iam/internal/apps/kaname/api/user_tokens/handler.go, Issue).
			by, err := edgeUserTokenIssuer(machineCaller, edgeStr(req, "userId"),
				edgeStr(req, "createdByUserId"))
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
				"userId":          edgeStr(req, "userId"),
				"createdByUserId": by,
				"name":            edgeStr(req, "name"),
				"description":     edgeStr(req, "description"),
				"labels":          edgeMap(req, "labels"),
				"createdAt":       edgeNow(),
				"expiresAt":       expires,
				"keyAlgorithm":    "ES256",
				"publicKeyPem":    "-----BEGIN PUBLIC KEY-----\n" + id + "\n-----END PUBLIC KEY-----",
				// Материал чеканится ОДИН раз и уезжает только в ответе операции: в самой
				// строке его нет, поэтому списочное чтение вернуть его не может даже по
				// ошибке. Так и у настоящего края.
				"__secret": "-----BEGIN PRIVATE KEY-----\n" + id + "\n-----END PRIVATE KEY-----",
			}, nil
		},

		// Изменяющей операции у выпущенного токена нет: край несёт только выпуск,
		// перечисление и отзыв. Провайдер до этого метода не доходит, и отказ здесь —
		// страж на случай, если пометка «пересоздаёт» однажды исчезнет со схемы.
		Update: func(_ *fakeEdge, _, _ edgeObject, field string) error {
			return fmt.Errorf("%s is immutable: an issued token has no update operation", field)
		},

		// Полезная нагрузка успешной операции. Другого случая получить материал не будет.
		//
		// Идентификатор клиента и идентификатор ключа РАВНЫ `id` строки, и это не
		// упрощение подделки, а измеренный контракт: у удостоверения одно имя, а не три
		// (proto/kacho/cloud/iam/v1/user_token_service.proto, IssueUserTokenResponse —
		// `client_id` совпадает с `key_id` дословно).
		OpResponse: func(row *edgeRow) edgeObject {
			return edgeObject{
				"clientId":      row.id,
				"keyId":         row.id,
				"algorithm":     "ES256",
				"publicKeyPem":  row.obj["publicKeyPem"],
				"privateKeyPem": row.obj["__secret"],
				"token": map[string]any{
					"id":     row.id,
					"userId": row.obj["userId"],
					// Ответственный уезжает ЗДЕСЬ, и другого источника у состояния нет:
					// на машинной полосе оператор его не называл, а знать, кто числится
					// выпустившим живой токен, он вправе.
					"createdByUserId": row.obj["createdByUserId"],
					"name":            row.obj["name"],
					"description":     row.obj["description"],
					"labels":          row.obj["labels"],
					"expiresAt":       row.obj["expiresAt"],
					"createdAt":       row.obj["createdAt"],
					"keyAlgorithm":    row.obj["keyAlgorithm"],
					"publicKeyPem":    row.obj["publicKeyPem"],
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

// edgeUserTokenIssuer — кого край запишет ответственным и что он отвергнет.
//
// Полосы РАЗНЫЕ, и различие воспроизведено таким, какое оно в дереве, а не таким, каким
// его было бы удобно иметь:
//
//   - человек промолчал — ответственным становится он сам; назвал чужого — ОТКАЗ;
//   - машина — ответственным становится САМ ВЛАДЕЛЕЦ токена (служебная учётка строкой
//     пользователей не является, назначить ответственным её нечем), и присланное
//     значение при этом НЕ отвергается, а молча замещается.
//
// Последнее — расхождение с соседней полосой того же механизма: у ключа служебной учётки
// край на машинной полосе присланное ответственное ОТВЕРГАЕТ синхронно (sa_keys/handler.go),
// а здесь принимает и выбрасывает. Подделка обязана быть не снисходительнее края и не
// строже него, поэтому расхождение воспроизведено дословно; ни одна проба его не
// утверждает как желаемое — это находка, заведённая отдельно, а не контракт.
func edgeUserTokenIssuer(machineCaller bool, userID, supplied string) (string, error) {
	if machineCaller {
		return userID, nil
	}
	switch supplied {
	case "", accTokenUser:
		return accTokenUser, nil
	default:
		return "", fmt.Errorf(
			"Illegal argument created_by_user_id: must match authenticated principal or be empty")
	}
}

// ---- настройки ---------------------------------------------------------------------------

// accTokenConfig — токен, о котором настройка называет только необходимое.
func accTokenConfig(e *fakeEdge, name string, ttl int64) string {
	return accTokenConfigIssuedBy(e, name, ttl, "")
}

// accTokenConfigIssuedBy — то же с ЛЮБЫМ ответственным; пустая строка означает, что
// настройка его не называет вовсе.
func accTokenConfigIssuedBy(e *fakeEdge, name string, ttl int64, issuer string) string {
	line := ""
	if issuer != "" {
		line = fmt.Sprintf("\n  created_by_user_id = %q", issuer)
	}
	return accProvider(e) + fmt.Sprintf(`
resource "kaname_user_token" "t" {
  user_id     = %q%s
  name        = %q
  ttl_seconds = %d
}
`, accTokenUser, line, name, ttl)
}

// accTokenForeignIssuerRefusal — отказ края на названного чужого ответственного.
//
// Пробелы — через \s+: отказ приезжает переносимым по строкам, и точная подстрока
// разошлась бы с ним от одной лишь ширины вывода.
var accTokenForeignIssuerRefusal = regexp.MustCompile(
	`must\s+match\s+authenticated\s+principal\s+or\s+be\s+empty`)

// ---- чтение текста, дошедшего до оператора ------------------------------------------------

// accTokenDenialTextIsHonest читает ТЕКСТ отказа и утверждает о его СОДЕРЖАНИИ.
//
// Проверяется ЧЕТЫРЕ вещи, и это не педантизм — каждая закрывает свой способ солгать:
//
//  1. текст НЕ утверждает, что служебная учётка порога не проходит. Утверждала прежняя
//     редакция всех трёх текстов ресурса; машинный принципал от порога ОСВОБОЖДЁН первым
//     же условием общего правила (pkg/grpcsrv/acr.go, EvaluateStepUp), к которому оба
//     места энфорсмента приходят через одну функцию;
//  2. названо ПРАВО — настоящая причина отказа машинному вызывающему;
//  3. названо, что порог требуется ЧЕЛОВЕКУ;
//  4. названо освобождение машины, и названо ПОСЛЕ первых двух: перечень причин, в
//     котором освобождение стоит раньше самих причин, читается наоборот.
//
// Порядок утверждается индексами, а не одним выражением: несовпадение обязано называть
// ИМЕННО ТУ часть, которой не хватило, — иначе разбор красного начинается с чтения
// регулярного выражения вместо чтения текста.
func accTokenDenialTextIsHonest(err error) error {
	if err == nil {
		// Недостижимо через ErrorCheck (её зовут только на ошибке), но обязано быть
		// названо: молчаливое «нет ошибки — значит хорошо» превратило бы пробу в
		// утверждение, которое нечем опровергнуть.
		return fmt.Errorf("отказа не было вовсе — читать нечего")
	}
	text := strings.ToLower(err.Error())

	// Прежние утверждения, дословно. Список закрытый и назван поимённо: он существует
	// ровно затем, чтобы возврат снятого текста краснел, а не проходил незамеченным.
	for _, retired := range []string{
		"служебная учётка его не проходит",
		"машина не проходит",
		"не проходит второй фактор",
		"его не несёт",
	} {
		if strings.Contains(text, retired) {
			return fmt.Errorf("текст утверждает про служебную учётку «%s», хотя машинный "+
				"принципал от порога освобождён; текст целиком:\n%s", retired, err.Error())
		}
	}

	if !strings.Contains(text, "человек") {
		return fmt.Errorf("текст не называет, кому порог требуется; текст целиком:\n%s", err.Error())
	}

	ordered := []struct{ what, needle string }{
		{"право как причину отказа", "право"},
		{"порог повышенного подтверждения", "step-up"},
		{"освобождение служебной учётки от порога", "освобожд"},
	}
	prev := -1
	for _, want := range ordered {
		at := strings.Index(text, want.needle)
		if at < 0 {
			return fmt.Errorf("текст не называет %s (искали %q); текст целиком:\n%s",
				want.what, want.needle, err.Error())
		}
		if at < prev {
			return fmt.Errorf("текст называет %s раньше предыдущей причины — перечень "+
				"читается наоборот; текст целиком:\n%s", want.what, err.Error())
		}
		prev = at
	}
	return nil
}

// ---- пробы ------------------------------------------------------------------------------

// Отказ в ВЫПУСКЕ объясняется тем, чего из самого отказа не видно, — и обеими причинами.
//
// Причин ровно две, и они разные: невыданное право и неподтверждённая повышенным уровнем
// личность ЧЕЛОВЕКА. Голое «permission denied» не различает их вовсе, а прежняя редакция
// этого текста закрепляла ОДНУ и притом несуществующую: она утверждала, что служебная
// учётка второй фактор не проходит и личные токены заводятся под человеческой личностью.
// Машинный принципал от порога ОСВОБОЖДЁН, поэтому текст отправлял конвейер отказываться
// от полосы по причине, которой нет, — а его настоящая причина не называлась.
//
// Проба ВЫЗЫВАЕТ ресурс полным циклом и читает текст, дошедший до оператора. Утверждение
// о форме схемы этот класс не ловит by construction: текст рождается только на том пути,
// где край уже ответил отказом.
func TestAcceptanceIAMUserToken_DeniedIssueNamesTheRealCauses(t *testing.T) {
	e := newFakeEdge(t, edgeKindUserToken())
	e.RejectCreate(userTokensCollection, edgeStatus{
		HTTP: 403, Code: 7, Message: "permission denied on iam.issue_user_tokens.issue",
	})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: accProviderFactories(t),
		ErrorCheck:               accTokenDenialTextIsHonest,
		Steps: []resource.TestStep{{
			Config: accTokenConfig(e, "acc-token-denied", 3600),
			// Положительный контроль к ErrorCheck: она зовётся ТОЛЬКО на ошибке, поэтому
			// прошедший apply остался бы незамеченным — и проба про текст отказа была бы
			// зелёной на крае, который ни в чём не отказал.
			Check: func(*tfstate.State) error {
				return fmt.Errorf("край отверг выпуск, а apply прошёл: отказ до оператора не дошёл")
			},
		}},
	})
}

// Отказ в ОТЗЫВЕ объясняется тем же — текст у обоих путей ОДИН.
//
// Отдельная проба нужна, потому что путь другой: отзыв уходит из Delete, и до правки его
// текст объявлял порог «как и выпуск», унаследовав снятое утверждение ссылкой. Общий
// источник текста этого сам по себе не гарантирует: провязать его можно и мимо.
//
// Порядок шагов задан отказом уборки: набор убирает за собой тем же удалением, и
// невзятый обратно отказ уронил бы пробу на её собственной уборке.
func TestAcceptanceIAMUserToken_FailedRevokeNamesTheRealCauses(t *testing.T) {
	e := newFakeEdge(t, edgeKindUserToken())

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: accProviderFactories(t),
		ErrorCheck:               accTokenDenialTextIsHonest,
		CheckDestroy: func(*tfstate.State) error {
			if n := e.CountOf(userTokensCollection); n != 0 {
				return fmt.Errorf("после destroy у края осталось токенов: %d", n)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{Config: accTokenConfig(e, "acc-token-revoke-denied", 3600)},
			{
				PreConfig: func() {
					e.RejectDelete(userTokensCollection, edgeStatus{
						HTTP: 403, Code: 7, Message: "permission denied on iam.revoke_user_tokens.revoke",
					})
				},
				Config:  accTokenConfig(e, "acc-token-revoke-denied", 3600),
				Destroy: true,
			},
			{
				PreConfig: func() { e.AllowDelete(userTokensCollection) },
				Config:    accTokenConfig(e, "acc-token-revoke-denied", 3600),
				Destroy:   true,
			},
		},
	})
}

// Конвейер ходит СЛУЖЕБНОЙ УЧЁТКОЙ, и токен выпускается без названного ответственного.
//
// Это та полоса, ради которой машинная личность заводится, и прежние тексты ресурса от
// неё отговаривали. Проба ВЫЗЫВАЕТ ресурс минимально-законным для этой полосы входом и
// требует успеха: законность входа решают два правила сразу — что требует описание и что
// принимает край, — и разойтись они могут молча.
func TestAcceptanceIAMUserToken_MachineCallerIssuesWithoutNamingTheIssuer(t *testing.T) {
	e := newFakeEdge(t, edgeKindUserTokenMachineCaller())

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: accProviderFactories(t),
		CheckDestroy: func(*tfstate.State) error {
			if n := e.CountOf(userTokensCollection); n != 0 {
				return fmt.Errorf("после destroy у края осталось токенов: %d", n)
			}
			return nil
		},
		Steps: []resource.TestStep{{
			Config: accTokenConfig(e, "acc-token-machine", 3600),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrSet("kaname_user_token.t", "id"),
				resource.TestCheckResourceAttrSet("kaname_user_token.t", "private_key_pem"),
				// КТО заполняет поле, которого настройка не называла: край, и его ответ
				// доезжает до состояния. Без этого утверждения проба зеленела бы и на
				// провайдере, который поле принял, но потерял.
				resource.TestCheckResourceAttr("kaname_user_token.t",
					"created_by_user_id", accTokenUser),
			),
		}},
	})
}

// Положительный контроль к ЧЕЛОВЕЧЕСКОЙ полосе: чужой ответственный отвергается.
//
// Без него проба выше зеленела бы и на подделке, которая поле просто не смотрит, — то
// есть не утверждала бы о полосах ничего.
func TestAcceptanceIAMUserToken_HumanCallerNamingSomeoneElseIsRefused(t *testing.T) {
	e := newFakeEdge(t, edgeKindUserToken())

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: accProviderFactories(t),
		Steps: []resource.TestStep{{
			Config:      accTokenConfigIssuedBy(e, "acc-token-foreign", 3600, accTokenStranger),
			ExpectError: accTokenForeignIssuerRefusal,
		}},
	})
}

// Секрет выдаётся один раз и переживает обновление состояния.
//
// Это ГЛАВНОЕ свойство ресурса и единственное, что проверяется здесь лучше, чем на живом
// стенде: там «ключ уцелел после refresh» подтверждается взглядом в файл состояния, и
// никто не заметит, если однажды он перестанет уцелевать. Обратное чтение у токена идёт
// ПЕРЕЧИСЛЕНИЕМ — чтения по идентификатору у него нет вовсе, — и в списочном ответе
// материала нет; провайдер, разложивший список «как есть», затёр бы его пустотой.
func TestAcceptanceIAMUserToken_SecretSurvivesRefresh(t *testing.T) {
	e := newFakeEdge(t, edgeKindUserToken())
	var tokenID, secret string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: accProviderFactories(t),
		CheckDestroy: func(*tfstate.State) error {
			if n := e.CountOf(userTokensCollection); n != 0 {
				return fmt.Errorf("после destroy у края осталось токенов: %d — "+
					"отзыв ушёл не по тому идентификатору", n)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: accTokenConfig(e, "acc-token-secret", 3600),
				Check: resource.ComposeAggregateTestCheckFunc(
					accCaptureAttr("kaname_user_token.t", "id", &tokenID),
					accCaptureAttr("kaname_user_token.t", "private_key_pem", &secret),
					resource.TestCheckResourceAttr("kaname_user_token.t", "algorithm", "ES256"),
					resource.TestCheckResourceAttrSet("kaname_user_token.t", "public_key_pem"),
					func(s *tfstate.State) error {
						// Секрет в состоянии — ТОТ, что край выдал, а не пустая строка,
						// которая тоже прошла бы проверку «атрибут задан».
						if want := e.Row(tokenID)["__secret"]; secret != want {
							return fmt.Errorf("в состоянии не тот материал: %q против %q", secret, want)
						}
						// Идентификатор клиента и ключа — АДРЕСУЕМЫЙ идентификатор строки,
						// а не что-то похожее: по нему токен и отзывается.
						rs := s.RootModule().Resources["kaname_user_token.t"]
						for _, attr := range []string{"client_id", "key_id"} {
							if got := rs.Primary.Attributes[attr]; got != tokenID {
								return fmt.Errorf("%s = %q, а строка токена — %q", attr, got, tokenID)
							}
						}
						return nil
					},
				),
			},
			{
				RefreshState: true,
				Check: resource.ComposeAggregateTestCheckFunc(
					accCheckAttrLate("kaname_user_token.t", "id", &tokenID),
					func(s *tfstate.State) error {
						rs := s.RootModule().Resources["kaname_user_token.t"]
						if got := rs.Primary.Attributes["private_key_pem"]; got != secret {
							return fmt.Errorf("обновление состояния изменило материал: было %q, стало %q",
								secret, got)
						}
						return nil
					},
				),
			},
		},
	})
}

// Токена нет в УСПЕШНОМ списке владельца — он отозван, и это ПОЛНОЕ подтверждение.
//
// Единственный путь, на котором ресурс снимается из состояния: список прочитан целиком и
// принадлежит ровно тому владельцу, у которого токен и висел. Отрицание здесь стоит в
// паре с положительным контролем выше (после refresh ресурс ОСТАЁТСЯ), иначе оно
// зеленело бы и на провайдере, снимающем из состояния всё подряд.
func TestAcceptanceIAMUserToken_RevokedOutsideIsDroppedFromState(t *testing.T) {
	e := newFakeEdge(t, edgeKindUserToken())
	var tokenID string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: accProviderFactories(t),
		Steps: []resource.TestStep{
			{
				Config: accTokenConfig(e, "acc-token-revoked", 3600),
				Check:  accCaptureAttr("kaname_user_token.t", "id", &tokenID),
			},
			{
				PreConfig:          func() { e.Forget(tokenID) },
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
				Check:              accAbsentFromState("kaname_user_token.t"),
			},
		},
	})
}

// ОПИСАНИЕ ресурса утверждает то же, что и оба отказа, — и читается оно ТЕМ ЖЕ
// протоколом, каким его получает terraform.
//
// Почему не чтением поля структуры: описание доезжает до оператора через
// GetProviderSchema, и предмет утверждения — то, что придёт ПО ПРОВОДУ, а не то, что
// лежит в схеме до перевода. Разойтись эти две вещи могут молча (пометка кладётся в
// Description только когда объявлена как markdown-описание).
//
// Содержание судит ТА ЖЕ функция, что и оба отказа: три места об одном предмете
// разошлись бы на первом же уточнении, и разошлись бы незаметно — описание читают в
// документации, а отказ видят в терминале, и рядом их никто не кладёт.
//
// Исполнителя цикла terraform проба не требует: она разговаривает с провайдером
// напрямую и потому остаётся полезной и под `-short`.
func TestAcceptanceIAMUserTokenSchemaSaysTheSameAsItsRefusals(t *testing.T) {
	srv, err := providerserver.NewProtocol6WithError(New())()
	if err != nil {
		t.Fatalf("провайдер не поднялся: %v", err)
	}
	schemas, err := srv.GetProviderSchema(t.Context(), &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatalf("схема не получена: %v", err)
	}
	res, ok := schemas.ResourceSchemas["kaname_user_token"]
	if !ok {
		t.Fatal("в схеме провайдера нет kaname_user_token — ресурс не зарегистрирован")
	}
	if res.Block == nil || strings.TrimSpace(res.Block.Description) == "" {
		t.Fatal("у ресурса нет описания: судить не о чем")
	}
	if bad := accTokenDenialTextIsHonest(errors.New(res.Block.Description)); bad != nil {
		t.Fatalf("описание ресурса, которое получает terraform: %v", bad)
	}
}

// Предпосылка проб выше: материал КРАЯ уезжает только в ответе операции.
//
// Запрос уходит мимо провайдера — иначе утверждение «обратное чтение не трогает
// материал» опиралось бы на подделку, о которой неизвестно, есть ли ей что трогать.
// Рядом — зеркальная сторона: в ответе операции материал ЕСТЬ, иначе «в списке его нет»
// выглядело бы как соблюдение контракта у края, который его вовсе не чеканит.
func TestAcceptanceFakeEdgeKeepsUserTokenSecretOutOfTheList(t *testing.T) {
	e := newFakeEdge(t, edgeKindUserToken())
	c := mustProviderClient(t, e.URL())
	ctx := t.Context()

	base := "/iam/v1/users/" + accTokenUser + "/tokens"
	opID := accPostRaw(t, ctx, c, base, []byte(`{"userId":"`+accTokenUser+`","name":"проба"}`), "")
	if opID == "" {
		t.Fatal("выпуск не дал операции")
	}
	done, err := c.AwaitOperation(ctx, opID, client.AwaitOptions{})
	if err != nil {
		t.Fatalf("операция выпуска: %v", err)
	}
	issued, err := decodeIssuedUserToken(done.Response)
	if err != nil {
		t.Fatalf("разбор ответа операции: %v", err)
	}
	if issued.PrivateKeyPEM == "" {
		t.Fatal("в ответе операции нет материала — утверждать про его отсутствие в списке нечего")
	}

	listed, err := c.Do(ctx, http.MethodGet, base, nil, nil)
	if err != nil {
		t.Fatalf("список: %v", err)
	}
	if out := client.Classify(listed); out.Kind != client.OutcomeOK {
		t.Fatalf("список отвечает не успехом: %s", out.Message)
	}
	if body := string(listed.Body); strings.Contains(body, "PRIVATE KEY") {
		t.Fatalf("списочный ответ несёт материал — провайдер, читающий его оттуда, "+
			"выглядел бы исправным:\n%s", body)
	}
}
