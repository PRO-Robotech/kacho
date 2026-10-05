// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	coredb "github.com/PRO-Robotech/corelib/db"
	"github.com/PRO-Robotech/corelib/grpcsrv"
	"github.com/PRO-Robotech/corelib/observability"
	"github.com/PRO-Robotech/corelib/servicecontract"

	"github.com/PRO-Robotech/kacho/services/notify/cmd/notify-probe/internal/config"
)

// bootPosture — самоотчёт о посадке. Звено идентичности, форма слушателя и
// признак «сервисов нет» берутся из ТЕХ значений, что уехали в принятый
// дескриптор (форма — одной функцией фундамента `servicehost.PostureOf`):
// второго литерала нет. Флаг ленты — значение, разобранное загрузчиком
// (`cfg.Notifications`), а не сырое окружение.
func bootPosture(cfg config.Config, identity servicecontract.Axis[grpcsrv.ServiceIdentity],
	form observability.ListenerForm, noServedServices bool) observability.BootPosture {
	return observability.BootPosture{
		Service:           serviceName,
		AuthMode:          cfg.AuthMode,
		DBSSLMode:         coredb.SSLModeFromDSN(cfg.DSN()),
		PublicMTLS:        cfg.PublicServerMTLS.Enable,
		InternalMTLS:      observability.InternalMTLSFrom(cfg.InternalServerMTLS.Enable),
		AuthZCheck:        cfg.AuthZIAMGRPCAddr != "",
		TrustedForwarders: cfg.TrustedForwarders().IsNarrowed(),
		// Проба токенов не выпускает и не проверяет: личность приходит
		// сертификатом пира и пересылкой края.
		IdentityProvider:   observability.IdentityProviderNotApplicable,
		OwnRESTPublicTLS:   observability.OwnRESTFrontNotRaised,
		OwnRESTInternalTLS: observability.OwnRESTFrontNotRaised,
		ListenerForm:       form,
		NoServedServices:   noServedServices,
		ServiceIdentity:    serviceIdentityReport(identity),
		Notifications:      observability.NotificationsFlagOf(cfg.Notifications.On()),
	}
}
