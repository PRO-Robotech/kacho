// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"google.golang.org/grpc"

	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
)

// principalExtractUnary — звенья цепочки, отвечающие на вопрос «чью личность несёт
// этот запрос». ОДИН И ТОТ ЖЕ набор на ОБОИХ листенерах: «internal = доверенный» —
// запрещённое допущение, а :9090 вдобавок открыт всему пространству имён.
//
// forwarders — круг отправителей, которым РАЗРЕШЕНО передавать личность конечного
// пользователя (`x-kacho-principal-*`). Пир, предъявивший сертификат вне этого
// круга, проходит транспортную аутентификацию как ОН САМ, но говорить за другого не
// может: переданная им личность снимается, и до модели прав она не доходит.
//
// Почему именно пара, а не одиночный извлекатель: сертификат доказывает, ЧЕЙ это
// пир, и ничего не говорит о праве представляться другим. Первое звено достаёт
// личность сертификата, второе решает по ней, принимать ли переданные заголовки.
// Одиночный `UnaryPrincipalExtract` этого решения не принимает вовсе — он читает
// заголовки безусловно, и его собственный godoc ограничивает применение листенером,
// куда неконтролируемый пир дозвониться не может.
//
// Пустой круг НЕ означает «никому»: contract corelib сужает его ТОЛЬКО на непустом
// списке, поэтому пустой означает «любой пир с сертификатом внутреннего CA». Именно
// поэтому производственная посадка не стартует, пока список пуст — см.
// config.Config.Validate (authz.trusted-forwarder-sans).
func principalExtractUnary(forwarders []string) []grpc.UnaryServerInterceptor {
	return []grpc.UnaryServerInterceptor{
		grpcsrv.UnaryCertIdentityExtract(),
		grpcsrv.UnaryTrustedPrincipalExtract(grpcsrv.WithTrustedForwarders(forwarders...)),
	}
}

// principalExtractStream — то же для stream RPC (тот же инвариант порядка).
func principalExtractStream(forwarders []string) []grpc.StreamServerInterceptor {
	return []grpc.StreamServerInterceptor{
		grpcsrv.StreamCertIdentityExtract(),
		grpcsrv.StreamTrustedPrincipalExtract(grpcsrv.WithTrustedForwarders(forwarders...)),
	}
}
