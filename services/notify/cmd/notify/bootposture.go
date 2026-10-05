// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	coredb "github.com/PRO-Robotech/corelib/db"
	"github.com/PRO-Robotech/corelib/grpcsrv"
	"github.com/PRO-Robotech/corelib/observability"
	"github.com/PRO-Robotech/corelib/servicecontract"
	"github.com/PRO-Robotech/corelib/servicehost"

	"github.com/PRO-Robotech/kacho/services/notify/internal/config"
)

// bootPosture — самоотчёт посадки notify. Форма слушателя и признак «сервисов
// нет» берутся одной функцией `servicehost.PostureOf` из ПРИНЯТОГО
// дескриптора, второго литерала нет (З15); запись собирает конструктор
// `observability.NewBootPosture`, отвергающий противоречие о форме.
//
// Слушателей нет, поэтому оси слушателей отвечают «неприменимо», а не
// «выключено»: процесс без входящего пути не может быть ни защищён, ни
// открыт по ним.
func bootPosture(cfg config.Config, d servicecontract.Descriptor) (observability.BootPosture, error) {
	form, noServed := servicehost.PostureOf(&d)
	return observability.NewBootPosture(observability.BootPosture{
		Service:            "notify",
		AuthMode:           d.Spec().Mode.String(),
		DBSSLMode:          coredb.SSLModeFromDSN(cfg.DSN()),
		PublicMTLS:         false,
		InternalMTLS:       observability.InternalMTLSNotApplicable,
		AuthZCheck:         false,
		TrustedForwarders:  false,
		IdentityProvider:   observability.IdentityProviderNotApplicable,
		OwnRESTPublicTLS:   observability.OwnRESTFrontNotRaised,
		OwnRESTInternalTLS: observability.OwnRESTFrontNotRaised,
		ListenerForm:       form,
		NoServedServices:   noServed,
		ServiceIdentity:    grpcsrv.ServiceIdentityNotApplicable,
	})
}
