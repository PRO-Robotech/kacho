// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package clientaddress — ЕДИНСТВЕННАЯ сборка оператора адреса клиента края
// (kacho#3028).
//
// Её зовут композиционный корень (cmd/api-gateway/main.go) до первого
// слушателя и проба старта на окружении рендера каждой цепочки
// (gateway/deploy/edge_client_address_start_render_test.go). Второй сборки нет:
// проба, судящая свою копию, зеленела бы на крае, который не стартует.
//
// Один оператор на три читателя: условие `client_ip` модели прав (HTTP и
// нативный gRPC) и `X-Forwarded-For` ретрансляции полосы формы, по которому
// служба доступа ведёт ограничение частоты «на источник». Ручки: доверять ли
// заголовкам пересылки, сколько доверенных прыжков стоит перед краем (адрес
// берётся СПРАВА), круг сетей, звенья фронта поимённо (адреса подов безголовых
// служб фронта), имена звеньев в сертификате и якорь звеньев — удостоверяющий
// центр, который выпускает сертификаты ТОЛЬКО звеньям (круг 5). Разбор и отказ
// старта по каждой — config.TrustedProxyCircle, TrustedProxyPeers,
// TrustedProxyLinkSANs, TrustedProxyLinkAnchor.
//
// БОЕВОЙ ПРОФИЛЬ НЕ ВПРАВЕ НЕ ДОВЕРЯТЬ НИКОМУ: край за звеном фронта, не
// принимающий заголовок ни от кого, видит всех клиентов одним адресом звена, и
// ограничение частоты «на источник» становится общим на всех. Это отказ в
// старте, а не предупреждение: предупреждение такого края не останавливает.
//
// Обновление перечня звеньев запускается здесь же, на контексте процесса
// (frontpeers.Set.Run): сборка, вернувшая перечень, который никто не
// обновляет, через три периода перестала бы признавать звенья молча.
package clientaddress

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"time"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
	"github.com/PRO-Robotech/kacho/gateway/internal/frontpeers"
	"github.com/PRO-Robotech/kacho/gateway/internal/linktls"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// Operator — собранный оператор адреса клиента.
type Operator struct {
	// Extractor — оператор: ClientIP и условие client_ip.
	Extractor *middleware.ContextExtractor
	// Links — перечень звеньев поимённо; nil, если звенья не объявлены.
	Links *frontpeers.Set
	// Anchor — якорь звеньев. Его же получает полоса личности по сертификату
	// (middleware.AuthInterceptor.WithMTLSPrincipal): лист звена личностью не
	// становится. Одно значение на обоих читателей — разойтись им нечем.
	Anchor linktls.Anchor
}

// Start собирает оператор и запускает обновление перечня звеньев на ctx.
func Start(ctx context.Context, cfg config.Config, resolve frontpeers.Resolver, logger *slog.Logger) (Operator, error) {
	circle, err := cfg.TrustedProxyCircle()
	if err != nil {
		return Operator{}, err
	}
	names, err := cfg.TrustedProxyPeers()
	if err != nil {
		return Operator{}, err
	}
	sans, err := cfg.TrustedProxyLinkSANs()
	if err != nil {
		return Operator{}, err
	}
	roots, err := cfg.TrustedProxyLinkAnchor()
	if err != nil {
		return Operator{}, err
	}
	anchor := linktls.NewAnchor(roots...)
	opts := []middleware.ExtractorOption{
		middleware.WithTrustedProxyHops(cfg.AuthZTrustedProxyCount),
		middleware.WithTrustedProxies(circle...),
		middleware.WithTrustedLinkSANs(sans...),
		middleware.WithTrustedLinkAnchor(anchor),
	}
	var links *frontpeers.Set
	if len(names) > 0 {
		links, err = frontpeers.New(frontpeers.Options{
			Names: names, Refresh: cfg.AuthZTrustedProxyPeersRefresh, Resolve: resolve, Logger: logger,
		})
		if err != nil {
			return Operator{}, err
		}
		opts = append(opts, middleware.WithTrustedPeers(links))
	}
	op := middleware.NewContextExtractor(time.Now, cfg.AuthZTrustedXForwardedFor, opts...)
	if cfg.ProductionPosture() && op.TrustsNobody() {
		return Operator{}, fmt.Errorf("боевой профиль (KACHO_APP_ENV=%q): заголовок адреса клиента не принимается ни от "+
			"одного звена — за звеном фронта все клиенты были бы одним адресом и делили бы одно ограничение частоты "+
			"входа; объявите круг %s, звенья поимённо %s, имена звеньев в сертификате %s и якорь звеньев %s "+
			"(доверие пересылке KACHO_API_GATEWAY_AUTHZ_TRUSTED_XFF=%v, прыжков %d)", cfg.AppEnv,
			config.TrustedProxyCIDRsKnob, config.TrustedProxyPeersKnob, config.TrustedProxySANsKnob,
			config.TrustedProxyCAFileKnob, cfg.AuthZTrustedXForwardedFor, cfg.AuthZTrustedProxyCount)
	}
	if links != nil {
		go links.Run(ctx)
	}
	return Operator{Extractor: op, Links: links, Anchor: anchor}, nil
}

// LookupFrontLinks — разрешение имени службы звена в адреса её подов
// разрешателем процесса (поиск по пространству имён пода — из его resolv.conf).
func LookupFrontLinks(ctx context.Context, name string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, "ip", name)
}
