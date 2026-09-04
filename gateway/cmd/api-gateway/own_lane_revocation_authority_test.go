// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// own_lane_revocation_authority_test.go — на полосе `own` край поднимается БЕЗ
// внешнего поставщика, и отзыв при этом читается У НАС.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ: возможность, объявленная и неисполнимая ни при каком входе
//
// Ручка посадки принимает `own`, а страж старта требовал на ней адрес
// ИНТРОСПЕКЦИИ ВНЕШНЕГО ПОСТАВЩИКА — безусловно, и требовал, чтобы путь адреса
// был ровно административным путём поставщика. На полосе `own` поставщика нет
// вовсе, поэтому законного значения у поля не существовало: пусто — отказ,
// наш собственный авторитет — отказ по пути. Два правила об одном поле, и
// исполнимого входа между ними нет (`api-conventions.md` §«Неисполнимая
// возможность»).
//
// Следствие для арендатора, ставящего службу прав в СВОЁМ облаке: не
// поднимается ни `external` (нет трёх адресов поставщика), ни `own` (нет
// законного значения) — и единственный оставшийся выход есть режим
// разработчика, то есть посадка, запрещённая ban #16.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЛОСА ЗАМЕЩАЕТ ТРЕБОВАНИЕ, А НЕ СНИМАЕТ ЕГО
//
// Это несущее свойство, и половина случаев ниже стоит ради него. Отзыв,
// действующий на выдаче и не действующий на предъявлении, отзывом не является
// (`security.md` §«Контроль, действующий на ВЫДАЧЕ, но не на ПРЕДЪЯВЛЕНИИ»).
// Поэтому под `own` край обязан требовать НАШЕГО авторитета — иначе смена
// посадки стала бы способом выключить проверку отзыва, ничего не объявляя.
//
// ─────────────────────────────────────────────────────────────────────────────
// КАЖДЫЙ ОТРИЦАТЕЛЬНЫЙ СЛУЧАЙ ИМЕЕТ ПОЛОЖИТЕЛЬНОГО БЛИЗНЕЦА, И РАЗЛИЧИЕ ОДНО
//
// Без близнеца «отвергнуто» неотличимо от стража, отвергающего всё: он зеленел
// бы на страже, который не пускает ни одну посадку.
package main

import (
	"regexp"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
	"github.com/PRO-Robotech/kacho/pkg/identityposture"
)

// Фикстура НАШЕГО авторитета отзыва: годная во всех четырёх осях. Случаи ниже
// портят РОВНО ОДНУ.
const (
	ourAuthorityURL  = "https://kacho-iam-internal.kacho.svc:9097/internal/tokens/introspect"
	ourAuthorityCA   = "/etc/api-gateway/platform-revocation-ca/ca.crt"
	ourAuthorityCert = "/etc/api-gateway/platform-revocation-identity/tls.crt"
	ourAuthorityKey  = "/etc/api-gateway/platform-revocation-identity/tls.key"
)

// ourAuthorityWired — годная полоса нашего авторитета целиком.
func ourAuthorityWired() RevocationConfig {
	return RevocationConfig{
		PlatformRevocationURL:      ourAuthorityURL,
		PlatformRevocationCAFile:   ourAuthorityCA,
		PlatformRevocationCertFile: ourAuthorityCert,
		PlatformRevocationKeyFile:  ourAuthorityKey,
	}
}

// ownLane — посадка `own` с полностью провязанным нашим авторитетом. Это
// ПОЛОЖИТЕЛЬНЫЙ БЛИЗНЕЦ всех отрицательных случаев полосы.
func ownLane() RevocationConfig {
	c := ourAuthorityWired()
	c.IdentityProvider = identityposture.Own
	return c
}

// ─────────────────────────────────────────────────────────────────────────────
// Ось ПОСТАВЩИКА: требуется под `external`, не требуется под `own`.

// Под `own` край стартует БЕЗ адреса интроспекции внешнего поставщика.
// Это тот самый вход, у которого сегодня нет законного значения.
func TestOwnLaneStartsWithoutTheProviderIntrospectionAddress(t *testing.T) {
	cfg := ownLane()
	cfg.IntrospectionURL = ""
	if err := validateProductionRevocationConfig("production", cfg); err != nil {
		t.Fatalf("под own адрес интроспекции внешнего поставщика не требуется, получено: %v", err)
	}
}

// ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ той же оси: под `external` требование остаётся.
// Без него случай выше зеленел бы на страже, снявшем проверку у всех.
func TestExternalLaneStillDemandsTheProviderIntrospectionAddress(t *testing.T) {
	err := validateProductionRevocationConfig("production", RevocationConfig{
		IdentityProvider: identityposture.External,
		IntrospectionURL: "",
		AdminURL:         tlsAdminURL,
		AdminCAFile:      testAdminCA,
	})
	if err == nil {
		t.Fatal("под external незаданный адрес интроспекции обязан отвергать старт")
	}
	if !strings.Contains(err.Error(), "KACHO_HYDRA_INTROSPECTION_URL is empty") {
		t.Fatalf("отказ обязан называть ручку поставщика, получено: %q", err.Error())
	}
}

// Полосность снимает требование НАЛИЧИЯ, а не правила транспорта: адрес
// поставщика, объявленный под `own`, судится теми же правилами.
func TestADeclaredProviderIntrospectionIsJudgedTheSameOnBothLanes(t *testing.T) {
	for _, lane := range identityposture.Values() {
		t.Run(lane.String(), func(t *testing.T) {
			cfg := ownLane()
			cfg.IdentityProvider = lane
			cfg.AdminURL = tlsAdminURL
			cfg.AdminCAFile = testAdminCA
			cfg.IntrospectionURL = "http://provider-admin.kacho.svc:4445/admin/oauth2/introspect"
			err := validateProductionRevocationConfig("production", cfg)
			if err == nil {
				t.Fatal("незашифрованный адрес интроспекции обязан отвергаться на любой полосе")
			}
			if !strings.Contains(err.Error(), "KACHO_HYDRA_INTROSPECTION_URL is plaintext") {
				t.Fatalf("отказ обязан называть ручку и предмет, получено: %q", err.Error())
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Ось НАШЕГО авторитета: требуется под `own`; заданная — судится на любой полосе.

// Под `own` край обязан требовать НАШЕГО авторитета отзыва. Иначе смена посадки
// стала бы способом выключить чтение отзыва на предъявлении.
func TestOwnLaneDemandsOurOwnRevocationAuthority(t *testing.T) {
	cfg := ownLane()
	cfg.PlatformRevocationURL = ""
	err := validateProductionRevocationConfig("production", cfg)
	if err == nil {
		t.Fatal("под own незаданный НАШ авторитет отзыва обязан отвергать старт")
	}
	msg := err.Error()
	if !strings.Contains(msg, "KACHO_API_GATEWAY_PLATFORM_TOKEN_REVOCATION_URL is empty") {
		t.Fatalf("отказ обязан называть ручку нашего авторитета, получено: %q", msg)
	}
	if !strings.Contains(msg, config.IdentityProviderKnob+"=own") {
		t.Fatalf("отказ обязан назвать полосу, по которой требование действует, получено: %q", msg)
	}
}

// ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ той же оси: под `external` наш авторитет обязателен НЕ
// БЫВАЕТ — там отзыв читает поставщик. Без этого случая ось выше зеленела бы на
// страже, требующем нашего авторитета всегда.
func TestExternalLaneNeedsNoAuthorityOfOurOwn(t *testing.T) {
	err := validateProductionRevocationConfig("production", RevocationConfig{
		IdentityProvider: identityposture.External,
		IntrospectionURL: tlsIntrospectURL,
		AdminURL:         tlsAdminURL,
		AdminCAFile:      testAdminCA,
	})
	if err != nil {
		t.Fatalf("под external наш авторитет отзыва не требуется, получено: %v", err)
	}
}

// Хоп к нашему авторитету несёт предъявленный токен, поэтому открытым текстом
// он не идёт ни на одной полосе.
func TestOurAuthorityHopIsRefusedInPlaintext(t *testing.T) {
	cfg := ownLane()
	cfg.PlatformRevocationURL = "http://kacho-iam-internal.kacho.svc:9097/internal/tokens/introspect"
	err := validateProductionRevocationConfig("production", cfg)
	if err == nil {
		t.Fatal("открытый хоп к нашему авторитету обязан отвергать старт")
	}
	if !strings.Contains(err.Error(), "KACHO_API_GATEWAY_PLATFORM_TOKEN_REVOCATION_URL is plaintext") {
		t.Fatalf("отказ обязан называть ручку и предмет, получено: %q", err.Error())
	}
}

// Шифрование без якоря доверия проверяет не того: рукопожатие с внутренним
// удостоверяющим центром не сходится с системными корнями.
func TestOurAuthorityHopNeedsAPinnedAnchor(t *testing.T) {
	cfg := ownLane()
	cfg.PlatformRevocationCAFile = ""
	err := validateProductionRevocationConfig("production", cfg)
	if err == nil {
		t.Fatal("хоп без якоря доверия обязан отвергать старт")
	}
	if !strings.Contains(err.Error(), "KACHO_API_GATEWAY_PLATFORM_TOKEN_REVOCATION_CA_FILE") {
		t.Fatalf("отказ обязан называть ручку якоря, получено: %q", err.Error())
	}
}

// Наш авторитет СПРАШИВАЕТ клиентский сертификат. Хоп без пары — контроль,
// отказывающий всегда и по одной и той же причине: объявлен, провязан, и не
// отказал бы ни разу по существу.
func TestOurAuthorityHopNeedsAnIdentityToPresent(t *testing.T) {
	cfg := ownLane()
	cfg.PlatformRevocationCertFile = ""
	cfg.PlatformRevocationKeyFile = ""
	err := validateProductionRevocationConfig("production", cfg)
	if err == nil {
		t.Fatal("хоп без клиентской пары обязан отвергать старт")
	}
	msg := err.Error()
	if !strings.Contains(msg, "KACHO_API_GATEWAY_PLATFORM_TOKEN_REVOCATION_CERT_FILE") ||
		!strings.Contains(msg, "KACHO_API_GATEWAY_PLATFORM_TOKEN_REVOCATION_KEY_FILE") {
		t.Fatalf("отказ обязан называть обе ручки пары, получено: %q", msg)
	}
}

// Половина пары хуже отсутствия обеих: она выглядит настроенной.
func TestOurAuthorityHopRefusesHalfAnIdentity(t *testing.T) {
	t.Run("сертификат без ключа", func(t *testing.T) {
		cfg := ownLane()
		cfg.PlatformRevocationKeyFile = ""
		err := validateProductionRevocationConfig("production", cfg)
		if err == nil || !strings.Contains(err.Error(), "KACHO_API_GATEWAY_PLATFORM_TOKEN_REVOCATION_KEY_FILE") {
			t.Fatalf("отказ обязан называть недостающую половину, получено: %v", err)
		}
	})
	t.Run("ключ без сертификата", func(t *testing.T) {
		cfg := ownLane()
		cfg.PlatformRevocationCertFile = ""
		err := validateProductionRevocationConfig("production", cfg)
		if err == nil || !strings.Contains(err.Error(), "KACHO_API_GATEWAY_PLATFORM_TOKEN_REVOCATION_CERT_FILE") {
			t.Fatalf("отказ обязан называть недостающую половину, получено: %v", err)
		}
	})
}

// Заданный НАШ авторитет судится теми же правилами и под `external`: ось
// проверки транспорта полосой не разводится.
func TestADeclaredAuthorityOfOursIsJudgedOnTheExternalLaneToo(t *testing.T) {
	cfg := ourAuthorityWired()
	cfg.IdentityProvider = identityposture.External
	cfg.IntrospectionURL = tlsIntrospectURL
	cfg.AdminURL = tlsAdminURL
	cfg.AdminCAFile = testAdminCA
	cfg.PlatformRevocationCAFile = ""
	err := validateProductionRevocationConfig("production", cfg)
	if err == nil {
		t.Fatal("наш авторитет без якоря обязан отвергаться и под external")
	}
	if !strings.Contains(err.Error(), "KACHO_API_GATEWAY_PLATFORM_TOKEN_REVOCATION_CA_FILE") {
		t.Fatalf("отказ обязан называть ручку якоря, получено: %q", err.Error())
	}
}

// Незаданный НАШ авторитет под `external` находкой не является — там его
// предмета нет. Законный близнец случая выше.
func TestAnUnsetAuthorityOfOursIsSilentOnTheExternalLane(t *testing.T) {
	err := validateProductionRevocationConfig("production", RevocationConfig{
		IdentityProvider: identityposture.External,
		IntrospectionURL: tlsIntrospectURL,
		AdminURL:         tlsAdminURL,
		AdminCAFile:      testAdminCA,
	})
	if err != nil {
		t.Fatalf("незаданный наш авторитет под external находкой не является, получено: %v", err)
	}
}

// Дев-послабление соседа полосой не трогается: класс окружения решает раньше.
func TestDevClassEnvironmentIsUntouchedByTheLane(t *testing.T) {
	for _, env := range []string{"dev", "local", "test"} {
		if err := validateProductionRevocationConfig(env, RevocationConfig{
			IdentityProvider: identityposture.Own,
		}); err != nil {
			t.Fatalf("%s: класс окружения решает раньше полосы, получено: %v", env, err)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// СТРАЖУ ОБЯЗАНЫ ПОКАЗАТЬ ТО, ЧТО ОН СУДИТ
//
// Проверка выше судит четыре величины. Композиционный корень, собравший
// `RevocationConfig` без них, оставил бы их нулевыми — и страж отвечал бы
// «нашего авторитета нет» ПРИ ЛЮБОЙ настройке: чарт задал бы ручки, секрет был
// бы смонтирован, а старт отвергался. Ровно этот класс уже стоил выкатки на
// соседней оси (`admin_hop_wiring_test.go`), поэтому провязка утверждается
// отдельно от поведения.
//
// main() из пробы не исполним (он дозванивается до бэкендов и занимает порты),
// поэтому провязка утверждается ТАМ, ГДЕ ОНА ЖИВЁТ — в исходнике корня. Чтение
// исходника слабее исполнения и применяется намеренно ровно к тому свойству,
// которого «оно собирается» показать не может.

func TestCompositionRoot_ShowsOurRevocationAuthorityToTheGuard(t *testing.T) {
	src := compositionRoot(t)
	for _, want := range []struct{ field, source string }{
		{"PlatformRevocationURL", `cfg\.PlatformTokenRevocationURL`},
		{"PlatformRevocationCAFile", `cfg\.PlatformTokenRevocationCAFile`},
		{"PlatformRevocationCertFile", `cfg\.PlatformTokenRevocationCertFile`},
		{"PlatformRevocationKeyFile", `cfg\.PlatformTokenRevocationKeyFile`},
	} {
		re := regexp.MustCompile(`RevocationConfig\{(?s:.*?)` + want.field + `:\s*` + want.source)
		if !re.MatchString(src) {
			t.Errorf("страж не видит %s: композиционный корень обязан подать его из %s, иначе "+
				"величина остаётся нулевой и вердикт не зависит от настройки вовсе",
				want.field, want.source)
		}
	}
}
