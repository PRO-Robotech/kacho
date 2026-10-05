// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// external_grpc_server.go — полоса личности по сертификату на внешнем gRPC
// края (kacho#3028).
//
// Сборка — именованная функция, а не строки main(): чей сертификат на этом
// сервере становится личностью — свойство безопасности, и держит его проба на
// настоящем рукопожатии (external_grpc_cert_principal_test.go), которая
// строит полосу ЭТОЙ функцией. Строка внутри main() пробе недоступна: main()
// из пробы не исполним.
package main

import (
	"log/slog"

	"github.com/PRO-Robotech/corelib/grpcsrv"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
	"github.com/PRO-Robotech/kacho/gateway/internal/linktls"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// withCertPrincipalLane — полоса личности по сертификату
// (KACHO_API_GATEWAY_HYBRID_MTLS_EXTERNAL).
//
// Выключенная посадка — полоса не провязывается, личность только по токену.
//
// НА ВНЕШНЕМ gRPC КРАЯ ПОЛОСА НЕ ПРИЗНАЁТ НИКОГО — так же, как до kacho#3028:
// сервер корня (учётные данные linktls.ServerCredentials, их держит
// TestListenerOriginWiring_GRPCServerCarriesTheLinkCredentials) кладёт
// состояние TLS своим типом linktls.AuthInfo, а полоса читает
// credentials.TLSInfo. Лист установки выпускает кластерный
// выпускающий всякому, кто заводит запрос на сертификат в любом пространстве
// имён (перепись каналов #3028, строки 27 и 33), и служебная учётка по нему
// снаружи досталась бы всякому, кто такой лист завёл. Держит это
// TestExternalGRPC_NoClientCertificateBecomesAPrincipalWithoutAToken.
//
// Второй слой — якорь звеньев аргументом полосы: даже получи полоса состояние,
// лист звена фронта личностью не станет (строка 30); якорь — тот же, которым
// оператор адреса узнаёт звено.
func withCertPrincipalLane(a *middleware.AuthInterceptor, cfg config.Config, links linktls.Anchor, logger *slog.Logger) *middleware.AuthInterceptor {
	if !cfg.HybridMTLSEnabled() {
		return a
	}
	logger.Info("hybrid mTLS external listener: cert-principal path enabled")
	return a.WithMTLSPrincipal(grpcsrv.NewTrustDomain(cfg.AuthNTrustDomain), links)
}
