// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"github.com/PRO-Robotech/corelib/grpcsrv"
	"github.com/PRO-Robotech/corelib/observability"
	"github.com/PRO-Robotech/corelib/servicecontract"
	"github.com/PRO-Robotech/corelib/servicehost"

	"github.com/PRO-Robotech/kacho/services/notify/cmd/notify-api/internal/config"
)

// postureService — имя службы в самоотчёте посадки: у обоих развёртываний
// notify оно одно, различает записи развёртывание пода (приёмка NTF-4 Р20, Д20 (3)).
const postureService = "notify"

// bootPosture — самоотчёт о посадке notify-api, выведенный из ПРИНЯТОГО
// дескриптора: форма слушателя — та, что поднимет носитель (internal_only),
// публичного mTLS нет по форме, внутренний — всегда, круг пересылающих сужен.
func bootPosture(cfg config.Config, d servicecontract.Descriptor) (observability.BootPosture, error) {
	form, noServed := servicehost.PostureOf(&d)
	return observability.NewBootPosture(observability.BootPosture{
		Service:            postureService,
		AuthMode:           d.Spec().Mode.String(),
		DBSSLMode:          cfg.DBSSLMode,
		PublicMTLS:         false,
		InternalMTLS:       observability.InternalMTLSFrom(cfg.InternalServerTLS().Enable),
		AuthZCheck:         cfg.AuthzIAMGRPCAddr != "",
		TrustedForwarders:  cfg.TrustedForwarders().IsNarrowed(),
		IdentityProvider:   observability.IdentityProviderNotApplicable,
		OwnRESTPublicTLS:   observability.OwnRESTFrontNotRaised,
		OwnRESTInternalTLS: observability.OwnRESTFrontNotRaised,
		ListenerForm:       form,
		NoServedServices:   noServed,
		ServiceIdentity:    grpcsrv.ServiceIdentityNotApplicable,
	})
}
