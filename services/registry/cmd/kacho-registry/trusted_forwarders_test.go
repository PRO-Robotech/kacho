// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// trusted_forwarders_test.go — замки на измерение «кому разрешено передавать
// личность конечного пользователя» в kacho-registry.
//
// Что было не так. Публичный листенер (:9090) монтировал
// grpcsrv.UnaryPrincipalExtract — извлекатель, который читает заголовки
// x-kacho-principal-* БЕЗУСЛОВНО, не глядя ни на транспорт, ни на личность
// сертификата пира. Его собственный godoc предупреждает: монтировать ТОЛЬКО туда,
// куда не дозвонится неконтролируемый пир. Публичный листенер — обычный Service
// внутри пространства имён, сетевой политики у registry нет вовсе, а клиентский
// сертификат всем соседям выдаёт один и тот же внутренний центр. Внутренний
// листенер (:9091) при этом уже строил доверенную пару CertIdentityExtract →
// TrustedPrincipalExtract, но с ПУСТЫМ списком отправителей, что по контракту
// corelib (pkg/grpcsrv principalIsTrusted) означает не «никому», а «любому пиру,
// прошедшему проверку сертификата».
//
// Итог обоих дефектов один: сосед предъявляет свой законный сертификат, шлёт
// заголовки личности жертвы — и решение о правах (pkg/authz subject_extract читает
// ровно operations.PrincipalFromContextOK) принимается от её имени.
//
// Замки утверждают НАБЛЮДАЕМОЕ: какую личность увидит обработчик за цепочкой,
// которую собирает боевая проводка, со списком из боевой конфигурации. Что эту
// цепочку получают ОБА листенера — предмет trusted_forwarders_wiring_test.go.

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/url"
	"strings"
	"testing"

	corequota "github.com/PRO-Robotech/kacho/pkg/quota"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"

	"github.com/PRO-Robotech/kacho/pkg/grpcclient"
	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/registry/internal/apps/kacho/config"
)

const (
	// gatewaySAN — единственный законный отправитель чужой личности в registry.
	// Установлено по графу импортов: заглушки pkg/api/kacho/cloud/registry/v1 вне
	// самого сервиса импортирует ТОЛЬКО gateway/internal/restmux, и он же держит
	// адреса обоих листенеров (KACHO_API_GATEWAY_REGISTRY_GRPC :9090 и
	// ..._REGISTRY_INTERNAL_GRPC :9091).
	gatewaySAN = "spiffe://kacho.cloud/ns/kacho/sa/kacho-api-gateway"
	// neighbourSAN — пир с законным сертификатом внутреннего центра, которому
	// передавать чужую личность НЕ разрешено. compute берём представителем класса:
	// сертификат валиден, сетевого барьера до registry нет, роли отправителя нет.
	neighbourSAN = "spiffe://kacho.cloud/ns/kacho/sa/kacho-compute"

	victimUserID = "usr-victim"
)

// prodCfg — конфигурация, с которой процесс поднимается в боевом режиме: каждое
// уже гейтящееся измерение выставлено безопасно. Тесты ослабляют РОВНО ОДНО —
// список отправителей.
func prodCfg(forwarders ...string) config.Config {
	return config.Config{
		AuthMode:           "production",
		DBSSLMode:          "require",
		AuthZIAMGRPCAddr:   "kaname-internal.kacho.svc:9091",
		PublicServerMTLS:   grpcsrv.TLSServer{Enable: true},
		InternalServerMTLS: grpcsrv.TLSServer{Enable: true},
		// Транспорт поднимаемого ребра registry→iam: с тех пор как страж требует
		// его в боевом режиме, конфигурация без этой ручки боевой не является.
		// Фикстура, снисходительнее продукта, делает невидимым ровно тот дефект,
		// ради которого её подставляют; измерение ослабляется только в своей
		// пробе (peer_transport_test.go). Рёбра project/geo здесь не подняты
		// (адреса пусты) — по тому же предикату, что читает проводка.
		IAMAuthzMTLS: grpcclient.TLSClient{Enable: true},
		// Объявление домена величин — часть законной посадки: у ручки ровно два
		// законных значения, и незаданное среди них не значится. Здесь стоит
		// «не развёрнут», потому что эта отправная точка ребра величин не
		// поднимает: адрес без удостоверения был бы ВТОРЫМ ослаблением.
		QuotaAuthority:            corequota.NotDeployed,
		AuthZTrustedForwarderSANs: forwarders,
		// Домен доверия — величина установки, и конструктор дескриптора требует её
		// названной: процесс, не назвавший домена, своим не признаёт никого.
		AuthZTrustDomain: "kacho.cloud",
	}
}

// ── стража старта ───────────────────────────────────────────────────────────

// TestValidateSecurityConfig_ProductionRefusesEmptyForwarderAllowList — сердце
// правки: боевой режим не стартует, пока круг отправителей не сужен.
//
// RED до правки: validateSecurityConfig возвращает nil — процесс поднимается и
// принимает переданную личность от любого пира.
func TestValidateSecurityConfig_ProductionRefusesEmptyForwarderAllowList(t *testing.T) {
	for _, mode := range []string{"production", "production-strict"} {
		t.Run(mode, func(t *testing.T) {
			cfg := prodCfg()
			cfg.AuthMode = mode
			err := cfg.Validate()
			if err == nil {
				t.Fatalf("%s mode started with an EMPTY trusted-forwarder allow-list: corelib "+
					"narrows the circle only when the list is non-empty, so an empty list lets ANY "+
					"certificate-verified peer forward someone else's identity", mode)
			}
			if !strings.Contains(err.Error(), "KACHO_REGISTRY_AUTHZ_TRUSTED_FORWARDER_SANS") {
				t.Fatalf("the refusal must name the knob the operator has to set, got: %v", err)
			}
		})
	}
}

// TestValidateSecurityConfig_ProductionRefusesBlankOnlyForwarderAllowList —
// список из одних пустых записей для corelib НЕ существует: WithTrustedForwarders
// отбрасывает "" и получает пустое множество, то есть снова «доверяем любому».
// Стража обязана считать так же, иначе `SANS=","` проходит гейт и молча
// возвращает дыру.
func TestValidateSecurityConfig_ProductionRefusesBlankOnlyForwarderAllowList(t *testing.T) {
	if err := prodCfg("", " ").Validate(); err == nil {
		t.Fatal("a list of blank entries passed the guard: corelib drops empty strings, " +
			"so the resulting allow-list is empty and trusts any verified peer")
	}
}

// TestValidateSecurityConfig_ProductionAcceptsPinnedForwarderAllowList —
// положительный путь: с закреплённым отправителем боевой режим стартует. Держит
// стражу от вырождения в «отказывать всегда».
func TestValidateSecurityConfig_ProductionAcceptsPinnedForwarderAllowList(t *testing.T) {
	if err := validateSecurityConfig(prodCfg(gatewaySAN)); err != nil {
		t.Fatalf("a pinned allow-list must boot, got refusal: %v", err)
	}
}

// TestValidate_DevRefusesAnUnnarrowedCircleWithoutTheOptIn — стража круга
// срабатывает на ЛЮБОМ non-breakglass старте, а не только в боевом режиме:
// контроль, чья ветка на локальном стенде не исполняется ни разу, находит «забыл
// выставить круг» только на боевом профиле, где цена ошибки максимальна.
func TestValidate_DevRefusesAnUnnarrowedCircleWithoutTheOptIn(t *testing.T) {
	cfg := prodCfg()
	cfg.AuthMode = "dev"
	err := cfg.Validate()
	if err == nil {
		t.Fatal("dev с несуженным кругом и без опт-ина обязан отказать в старте")
	}
	if !strings.Contains(err.Error(), "KACHO_REGISTRY_AUTHZ_TRUST_ANY_FORWARDER") {
		t.Fatalf("отказ обязан назвать ручку опт-ина, иначе стенд не поднять: %v", err)
	}
}

// TestValidate_DevToleratesAnUnnarrowedCircleWithTheExplicitOptIn —
// положительный контроль: без него отрицание выше зеленело бы и на «отказывать
// всегда».
func TestValidate_DevToleratesAnUnnarrowedCircleWithTheExplicitOptIn(t *testing.T) {
	cfg := prodCfg()
	cfg.AuthMode = "dev"
	cfg.AuthZTrustAnyForwarder = true
	if err := cfg.Validate(); err != nil {
		t.Fatalf("явный опт-ин обязан пропускать локальную фикстуру: %v", err)
	}
}

// TestValidate_ProductionIgnoresTheDevOptIn — опт-ин не действует в боевом
// режиме: иначе он был бы ручкой, снимающей защиту на развёрнутом стенде.
func TestValidate_ProductionIgnoresTheDevOptIn(t *testing.T) {
	cfg := prodCfg()
	cfg.AuthZTrustAnyForwarder = true
	if err := cfg.Validate(); err == nil {
		t.Fatal("боевой режим обязан отказать на несуженном круге даже с опт-ином")
	}
}

// ── поведение цепочки, которую получают оба листенера ───────────────────────

// hostIdentityChain — пара звеньев извлечения личности В ТОМ ВИДЕ, в каком её
// ставит носитель контура, с кругом отправителей ИЗ ПРИНЯТОГО ДЕСКРИПТОРА.
//
// Прежде эти же пробы звали локальный сборщик композиционного корня
// (`identityUnary(cfg)`). Сборщика больше нет: цепочку строит носитель, и поля
// интерсепторного типа в дескрипторе не существует — принести сюда свою цепочку
// нельзя. Проба стала СТРОЖЕ ровно на один шаг: круг приезжает не из конфигурации
// напрямую, а через конструктор дескриптора, то есть через отказ старта (О1),
// который на несуженном круге просто не отдаст дескриптора.
//
// Что цепочку получают ОБА слушателя и что она у них одна — свойство ПОСТРОЕНИЯ
// носителя (`serverPair` строит её один раз на двоих) и предмет его собственных
// проб (`pkg/servicehost`: TestBothListenersRefuseIdenticallyOnTheWire,
// TestForwardedIdentityIsHonouredOnlyFromTheCircle). Здесь утверждается то, что
// принадлежит РЕЕСТРУ: какой круг он объявляет и кого этот круг пускает.
func hostIdentityChain(t *testing.T, forwarders ...string) []grpc.UnaryServerInterceptor {
	t.Helper()
	cfg := bootConfig(t, map[string]string{
		"KACHO_REGISTRY_AUTHZ_TRUSTED_FORWARDER_SANS": strings.Join(forwarders, ","),
	})
	desc, err := describe(cfg, probeMode(t, cfg), probeLogger(), probePorts())
	if err != nil {
		t.Fatalf("дескриптор с кругом %v отвергнут конструктором — процесс не поднялся бы:\n%v",
			forwarders, err)
	}
	circle, _ := desc.Spec().Forwarders.Get()
	return grpcsrv.PrincipalExtractUnary(grpcsrv.NewTrustDomain("kacho.cloud"), circle)
}

// seenIdentity прогоняет запрос через цепочку и возвращает личность, которую
// увидел бы обработчик, и признак доверия. Это и есть наблюдаемое: субъект
// проверки прав собирается ровно из неё (pkg/authz subject_extract →
// operations.PrincipalFromContextOK).
func seenIdentity(t *testing.T, chain []grpc.UnaryServerInterceptor, ctx context.Context) (id string, trusted, present bool) {
	t.Helper()
	final := func(c context.Context, _ any) (any, error) {
		p, ok := operations.PrincipalFromContextOK(c)
		id, present = p.ID, ok
		_, trusted = grpcsrv.TrustedPrincipalFromContext(c)
		return nil, nil
	}
	if _, err := chainUnary(chain...)(ctx, nil, &grpc.UnaryServerInfo{}, final); err != nil {
		t.Fatalf("chain returned error: %v", err)
	}
	return id, trusted, present
}

// TestListener_NeighbourWithValidCertCannotActAsSomeoneElse — главный замок.
//
// Сосед предъявляет ЗАКОННЫЙ сертификат внутреннего центра и присылает заголовки
// личности жертвы. Он обязан остаться собой: переданная личность снимается,
// носитель вычищается, и проверка прав не может выполниться от имени жертвы.
//
// RED до правки: цепочка публичного листенера читает заголовки безусловно —
// личность жертвы доходит до обработчика и становится субъектом проверки прав.
func TestListener_NeighbourWithValidCertCannotActAsSomeoneElse(t *testing.T) {
	chain := hostIdentityChain(t, gatewaySAN)

	id, trusted, present := seenIdentity(t, chain, forwarded(verifiedPeer(t, neighbourSAN), victimUserID))

	if id == victimUserID {
		t.Fatalf("a neighbouring service (%s) presented itself as %q — the authorization decision "+
			"would be made in the victim's name: read/update/delete of foreign registries and "+
			"repositories, and the admin RPCs on the internal listener", neighbourSAN, victimUserID)
	}
	if trusted {
		t.Fatalf("the forwarded identity from %s was marked trusted", neighbourSAN)
	}
	if present {
		t.Fatalf("the identity carrier survived for an untrusted sender: got %q "+
			"(it must be scrubbed, otherwise use-cases downstream see a forged owner)", id)
	}
}

// TestListener_PinnedSenderKeepsWorking — НЕ замок на дыру (с пустым списком он
// тоже зелёный: там доверены все). Его предмет — обратная ошибка: сузить так, что
// перестанет работать рабочий путь. Без gateway встают ВСЕ пользовательские
// запросы к registry — он единственный, кто передаёт сюда чужую личность, и
// делает это на оба листенера.
func TestListener_PinnedSenderKeepsWorking(t *testing.T) {
	chain := hostIdentityChain(t, gatewaySAN)

	id, trusted, present := seenIdentity(t, chain, forwarded(verifiedPeer(t, gatewaySAN), "usr-alice"))

	if !trusted || !present {
		t.Fatalf("the legitimate sender was refused (trusted=%v present=%v) — "+
			"the change denies service instead of narrowing it", trusted, present)
	}
	if id != "usr-alice" {
		t.Fatalf("forwarded identity not honoured: got %q, want %q", id, "usr-alice")
	}
}

// TestListener_UnverifiedPeerCannotForward — тоже НЕ замок на дыру: после правки
// эта ветка срабатывает независимо от списка. Держит нижний слой инварианта, чтобы
// правка списка не увела внимание от него. Единственный замок на саму дыру —
// TestListener_NeighbourWithValidCertCannotActAsSomeoneElse выше.
func TestListener_UnverifiedPeerCannotForward(t *testing.T) {
	chain := hostIdentityChain(t, gatewaySAN)

	id, trusted, _ := seenIdentity(t, chain, forwarded(unverifiedTLSPeer(), victimUserID))

	if trusted || id == victimUserID {
		t.Fatalf("a peer without a verified client certificate forwarded an identity: id=%q trusted=%v", id, trusted)
	}
}

// ── самоотчёт о посадке ─────────────────────────────────────────────────────

// TestBootPosture_ReportsWhetherTheCircleIsNarrowed — самоотчёт живого процесса
// обязан нести это измерение: гейт посадки читает строку процесса, а не хранимые
// настройки, поэтому неотчитанное измерение для него не существует.
//
// Значение берётся из той же config.TrustedForwarders, что уходит в проводку, —
// отчёт не может разойтись с посадкой.
func TestBootPosture_ReportsWhetherTheCircleIsNarrowed(t *testing.T) {
	t.Run("pinned", func(t *testing.T) {
		if got := bootPosture(prodCfg(gatewaySAN)); !got.TrustedForwarders {
			t.Fatal("a pinned allow-list must be reported as a narrowing")
		}
	})
	t.Run("empty_is_reported_honestly", func(t *testing.T) {
		if got := bootPosture(prodCfg()); got.TrustedForwarders {
			t.Fatal("an empty allow-list reported as a narrowing — the report describes intent, not outcome")
		}
	})
	// Список из одних пустых записей corelib отбрасывает — значит круг НЕ сужен,
	// и отчёт обязан говорить это, а не пересказывать намерение оператора.
	t.Run("blank_entries_are_not_a_narrowing", func(t *testing.T) {
		if got := bootPosture(prodCfg("", " ")); got.TrustedForwarders {
			t.Fatal("blank entries reported as a narrowing: corelib drops them, so the circle stays open")
		}
	})
}

// ── helpers ─────────────────────────────────────────────────────────────────

func forwarded(ctx context.Context, userID string) context.Context {
	return metadata.NewIncomingContext(ctx, metadata.Pairs(
		grpcsrv.MDKeyPrincipalType, "user",
		grpcsrv.MDKeyPrincipalID, userID,
		grpcsrv.MDKeyPrincipalDisplay, userID,
	))
}

// verifiedPeer — пир, прошедший проверку клиентского сертификата, с указанной
// личностью сертификата.
func verifiedPeer(t *testing.T, san string) context.Context {
	t.Helper()
	u, err := url.Parse(san)
	if err != nil {
		t.Fatalf("parse SAN %q: %v", san, err)
	}
	leaf := &x509.Certificate{URIs: []*url.URL{u}}
	return peer.NewContext(context.Background(), &peer.Peer{AuthInfo: credentials.TLSInfo{
		State: tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{leaf}}},
	}})
}

// unverifiedTLSPeer — TLS есть, подтверждённого клиентского сертификата нет.
func unverifiedTLSPeer() context.Context {
	return peer.NewContext(context.Background(), &peer.Peer{
		AuthInfo: credentials.TLSInfo{State: tls.ConnectionState{}},
	})
}

// chainUnary композирует unary-интерсепторы слева направо (семантика
// grpc.ChainUnaryInterceptor).
func chainUnary(interceptors ...grpc.UnaryServerInterceptor) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		chained := handler
		for i := len(interceptors) - 1; i >= 0; i-- {
			ic, next := interceptors[i], chained
			chained = func(c context.Context, r any) (any, error) { return ic(c, r, info, next) }
		}
		return chained(ctx, req)
	}
}
