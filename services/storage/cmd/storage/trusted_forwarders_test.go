// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// trusted_forwarders_test.go — поведенческие замки на измерение «кому разрешено
// передавать чужую личность».
//
// Дефект, который они закрывают: оба листенера строили правильную цепочку
// CertIdentityExtract → TrustedPrincipalExtract(WithTrustedForwarders(список)), но
// получали ЛИТЕРАЛЬНЫЙ пустой список, а поля в конфигурации не было вовсе. Контракт
// corelib (pkg/grpcsrv principalIsTrusted) сужает круг ТОЛЬКО на непустом списке —
// на пустом любой пир, прошедший проверку сертификата, присылал заголовки личности
// жертвы, и субъектом проверки прав становилась она.
//
// Замки утверждают НАБЛЮДАЕМОЕ на паре звеньев извлечения личности — той самой,
// которую носитель ставит в обе цепочки (`grpcsrv.PrincipalExtractUnary`), — с
// кругом, взятым из ПРИНЯТОГО ДЕСКРИПТОРА, а не с литералом, придуманным тестом, и
// не из конфигурации напрямую. Иначе тест доказывал бы работоспособность общего
// фундамента, а не посадку storage.
//
// # Что изменилось при переезде на носитель, и почему это НЕ ослабление
//
// Прежде круг брался из `cfg.TrustedForwarders()` и подавался в собственный
// сборщик цепочки storage. Собственного сборщика больше нет, и круг теперь
// читается там, куда он реально уезжает, — из `desc.Spec().Forwarders`. То есть
// звено между «настроили» и «поднялось» стало короче на одно посредническое
// значение: пройти этот замок с одним кругом и подняться с другим больше нельзя.
//
// Порядок звеньев (личность пира по сертификату ДО решения о доверии) держит
// носитель и его собственная проба
// `pkg/servicehost.TestForwardedIdentityIsHonouredOnlyFromTheCircle`; здесь предмет
// другой — ЧЕЙ круг доезжает до этой пары у storage.

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"log/slog"
	"net/url"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"

	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/storage/internal/config"
)

const (
	// Законные отправители: gateway передаёт личность пользователя на публичный
	// листенер, compute — на внутренний (привязка тома идёт под личностью того, кто
	// её инициировал, см. compute internal/clients/storage_client.go).
	gatewaySAN = "spiffe://kacho.cloud/ns/kacho/sa/kacho-api-gateway"
	computeSAN = "spiffe://kacho.cloud/ns/kacho/sa/kacho-compute"
	// Пир с законным сертификатом внутреннего центра, которому передавать чужую
	// личность НЕ разрешено. vpc берём как представителя класса: у него есть
	// валидный клиентский сертификат и сетевой доступ, но роли отправителя нет.
	neighbourSAN = "spiffe://kacho.cloud/ns/kacho/sa/kacho-vpc"

	victimUserID = "usr-victim"
)

// prodCfg — конфигурация, с которой процесс поднимается в боевом режиме. Путь
// «переменная окружения → срез» проверяется в пакете config (там же живёт
// test-only загрузчик); здесь предмет другой — что делает проводка с УЖЕ
// разобранным списком.
func prodCfg(forwarders ...string) config.Config {
	c := config.Config{
		AuthMode:                  "production",
		DBSSLMode:                 "require",
		AuthZIAMGRPCAddr:          "kaname-internal:9091",
		ListFilterEnabled:         true,
		AuthZTrustedForwarderSANs: forwarders,
		// Величины, которые конструктор дескриптора требует названными. Литералы
		// здесь законны: предмет этого файла — круг отправителей, а не выбор
		// величин (его судит describe_test.go на конфигурации из окружения).
		AuthZCacheTTL:         5 * time.Second,
		AuthZCheckTimeout:     2 * time.Second,
		AuthZDenyBudgetPerSec: 100,
		HandlingBudget:        30 * time.Second,
		// Срок жизни потока подписки — тоже требуемая конструктором величина: с
		// провязкой общего сервера потока (#1414) storage служит серверный стрим,
		// и «не применимо» дескриптор больше не принимает.
		SubscriptionStreamBudget: time.Hour,
	}
	// Ручки транспорта слушателей здесь НЕ взводятся намеренно: взведённая ручка
	// без файлов сертификата роняет сборку креденшелов, и проба падала бы на
	// предмете, который она не проверяет. Транспорт судит describe_test.go.
	return c
}

// listenerChain — ровно та пара звеньев, которую носитель ставит в ОБЕ цепочки,
// с кругом из ПРИНЯТОГО дескриптора.
//
// Звено решения о доступе не подключаем: оно стоит ПОСЛЕ извлечения личности и
// читает уже готовый контекст, а предмет проверки — именно то, какая личность в
// этот контекст попадает.
//
// Транспорт слушателей здесь взведён ручкой, но не файлами, поэтому боевая посадка
// отказала бы по непроверенному транспорту. Круг от посадки не зависит — он один и
// тот же на любой, — поэтому дескриптор собирается в dev-режиме, а боевую строгость
// судит describe_test.go.
func listenerChain(t *testing.T, cfg config.Config) grpc.UnaryServerInterceptor {
	t.Helper()
	cfg.AuthMode = "dev"
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	desc, err := describe(cfg, logger, buildListFilter(cfg, nil, logger), probeExistence{}, probeAuthzObserve, prometheus.NewRegistry())
	if err != nil {
		t.Fatalf("дескриптор не принят — круг до цепочки не доедет вовсе: %v", err)
	}
	circle, _ := desc.Spec().Forwarders.Get()
	return chainUnary(grpcsrv.PrincipalExtractUnary(grpcsrv.NewTrustDomain("kacho.cloud"), circle)...)
}

// seenIdentity прогоняет запрос через цепочку и возвращает личность, которую
// увидел бы обработчик, и признак доверия. Это и есть наблюдаемое: субъект
// проверки прав собирается ровно из неё (pkg/authz subject_extract).
func seenIdentity(t *testing.T, chain grpc.UnaryServerInterceptor, ctx context.Context) (id string, trusted bool, present bool) {
	t.Helper()
	final := func(c context.Context, _ any) (any, error) {
		p, ok := operations.PrincipalFromContextOK(c)
		id, present = p.ID, ok
		_, trusted = grpcsrv.TrustedPrincipalFromContext(c)
		return nil, nil
	}
	if _, err := chain(ctx, nil, &grpc.UnaryServerInfo{}, final); err != nil {
		t.Fatalf("chain returned error: %v", err)
	}
	return id, trusted, present
}

// TestListener_NeighbourWithValidCertCannotActAsSomeoneElse — главный замок.
//
// Сосед предъявляет ЗАКОННЫЙ сертификат внутреннего центра (проверку проходит) и
// присылает заголовки личности жертвы. Он обязан остаться собой: переданная
// личность снимается, носитель вычищается, и проверка прав не может выполниться от
// имени жертвы.
//
// RED при возврате пустого списка (`forwarders := []string{}`): личность жертвы
// доходит до обработчика и становится субъектом проверки прав.
func TestListener_NeighbourWithValidCertCannotActAsSomeoneElse(t *testing.T) {
	chain := listenerChain(t, prodCfg(gatewaySAN, computeSAN))

	id, trusted, present := seenIdentity(t, chain, forwarded(verifiedPeer(t, neighbourSAN), victimUserID))

	if id == victimUserID {
		t.Fatalf("a neighbouring service (%s) presented itself as %q: the authorization "+
			"decision would be made in the victim's name — read/update/delete of foreign "+
			"volumes, snapshots and images, and attach/detach on the internal listener",
			neighbourSAN, victimUserID)
	}
	if trusted {
		t.Fatalf("the forwarded identity from %s was marked trusted", neighbourSAN)
	}
	if present {
		t.Fatalf("the identity carrier survived for an untrusted sender: got %q "+
			"(it must be scrubbed, otherwise use-cases downstream see a forged owner)", id)
	}
}

// TestListener_PinnedSendersKeepWorking — НЕ замок на дыру (с пустым списком он тоже
// зелёный: там доверены все, включая этих двоих). Его предмет — обратная ошибка:
// сузить так, что перестанет работать рабочий путь. Без gateway встают все
// пользовательские запросы, без compute — привязка тома.
func TestListener_PinnedSendersKeepWorking(t *testing.T) {
	chain := listenerChain(t, prodCfg(gatewaySAN, computeSAN))

	for name, san := range map[string]string{
		"api-gateway (public :9090)": gatewaySAN,
		"compute (internal :9091)":   computeSAN,
	} {
		t.Run(name, func(t *testing.T) {
			id, trusted, present := seenIdentity(t, chain, forwarded(verifiedPeer(t, san), "usr-alice"))
			if !trusted || !present {
				t.Fatalf("%s: a legitimate sender was refused (trusted=%v present=%v) — "+
					"the change denies service instead of narrowing it", name, trusted, present)
			}
			if id != "usr-alice" {
				t.Fatalf("%s: forwarded identity not honoured: got %q, want %q", name, id, "usr-alice")
			}
		})
	}
}

// TestListener_UnverifiedPeerCannotForward — тоже НЕ замок на дыру: эта ветка
// срабатывает независимо от списка. Держит нижний слой инварианта, чтобы правка
// списка не увела внимание от него. Единственный замок на саму дыру —
// TestListener_NeighbourWithValidCertCannotActAsSomeoneElse выше.
func TestListener_UnverifiedPeerCannotForward(t *testing.T) {
	chain := listenerChain(t, prodCfg(gatewaySAN, computeSAN))

	id, trusted, _ := seenIdentity(t, chain, forwarded(unverifiedTLSPeer(), victimUserID))
	if trusted || id == victimUserID {
		t.Fatalf("a peer without a verified client certificate forwarded an identity: id=%q trusted=%v", id, trusted)
	}
}

// TestBootPosture_ReportsWhetherTheCircleIsNarrowed — самоотчёт живого процесса
// обязан нести это измерение: гейт посадки читает строку процесса, а не хранимые
// настройки, поэтому неотчитанное измерение для него не существует.
//
// Значение берётся из той же config.TrustedForwarders, что уходит в проводку, —
// отчёт не может разойтись с посадкой.
func TestBootPosture_ReportsWhetherTheCircleIsNarrowed(t *testing.T) {
	t.Run("pinned", func(t *testing.T) {
		requireFields(t, captureBootPosture(t, bootPosture(prodCfg(gatewaySAN))), map[string]any{
			"service":            "storage",
			"trusted_forwarders": true,
		})
	})

	t.Run("empty_is_reported_honestly", func(t *testing.T) {
		requireFields(t, captureBootPosture(t, bootPosture(prodCfg())), map[string]any{
			"trusted_forwarders": false,
		})
	})

	// Список из одних пустых записей corelib отбрасывает — значит круг НЕ сужен, и
	// отчёт обязан говорить это, а не пересказывать намерение оператора.
	t.Run("blank_entries_are_not_a_narrowing", func(t *testing.T) {
		requireFields(t, captureBootPosture(t, bootPosture(prodCfg("", ""))), map[string]any{
			"trusted_forwarders": false,
		})
	})
}

// TestTrustedForwarders_MatchesTheCorelibFilter — значение, отдаваемое в проводку,
// отфильтровано так же, как фильтрует corelib (пустые записи отбрасываются). Иначе
// стража считала бы список заполненным там, где corelib видит пустой.
func TestTrustedForwarders_MatchesTheCorelibFilter(t *testing.T) {
	got := prodCfg(" ", gatewaySAN+" ", "").TrustedForwarders().SANs()
	if len(got) != 1 || got[0] != gatewaySAN {
		t.Fatalf("TrustedForwarders() = %#v, want exactly [%q]", got, gatewaySAN)
	}
}

// --- helpers ---

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
