// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	subscriptionv1 "github.com/PRO-Robotech/corelib/api/corelib/subscription"
	"github.com/PRO-Robotech/corelib/authz"
	"github.com/PRO-Robotech/corelib/authz/proxytuple"
	"github.com/PRO-Robotech/corelib/grpcclient"
	"github.com/PRO-Robotech/corelib/grpcsrv"
	"github.com/PRO-Robotech/corelib/servicecontract"
	"github.com/PRO-Robotech/kacho/pkg/authz/authziam"

	"github.com/PRO-Robotech/kacho/services/notify/cmd/notify-probe/internal/config"
)

// serviceName — имя процесса в дескрипторе, самоотчёте и метках.
const serviceName = "notify-probe"

// servePorts — то, что корень приносит в дескриптор уже собранным: лента,
// звено идентичности, сужатель потока и приёмники величин.
type servePorts struct {
	parts        feedParts
	identity     servicecontract.Axis[grpcsrv.ServiceIdentity]
	narrower     servicecontract.ListNarrower
	authzObserve func(read func() authz.Metrics)
	metrics      prometheus.Registerer
}

// describe собирает ОБЪЯВЛЕНИЕ пробы о себе. Стражей старта здесь нет: их
// исполняют конструктор дескриптора и носитель.
func describe(cfg config.Config, logger *slog.Logger, ports servePorts) (servicecontract.Descriptor, error) {
	mode, err := servicecontract.ParseMode(cfg.AuthMode)
	if err != nil {
		return servicecontract.Descriptor{}, fmt.Errorf("KACHO_NOTIFYPROBE_AUTH_MODE: %w", err)
	}
	checkCreds, err := grpcclient.TLSClientTransportCreds(cfg.IAMAuthzMTLS)
	if err != nil {
		return servicecontract.Descriptor{}, fmt.Errorf("notify-probe→iam Check mTLS creds: %w", err)
	}
	publicCreds, err := grpcsrv.TLSServerTransportCreds(cfg.PublicServerMTLS)
	if err != nil {
		return servicecontract.Descriptor{}, fmt.Errorf("public listener tls creds: %w", err)
	}
	internalCreds, err := grpcsrv.TLSServerTransportCreds(cfg.InternalServerMTLS)
	if err != nil {
		return servicecontract.Descriptor{}, fmt.Errorf("internal listener tls creds: %w", err)
	}
	admission, err := servicecontract.AdmissionFromPosture(cfg.AdmissionPublic, cfg.AdmissionInternal)
	if err != nil {
		return servicecontract.Descriptor{}, fmt.Errorf("KACHO_NOTIFYPROBE_ADMISSION_*: %w", err)
	}

	spec := servicecontract.Spec{
		Service: serviceName,
		Mode:    mode,
		Logger:  logger,

		Forwarders: servicecontract.Value(cfg.TrustedForwarders()),
		ForwarderKnobs: servicecontract.ForwarderKnobs{
			SANs:     "KACHO_NOTIFYPROBE_AUTHZ_TRUSTED_FORWARDER_SANS",
			TrustAny: "KACHO_NOTIFYPROBE_AUTHZ_TRUST_ANY_FORWARDER",
			OptIn:    cfg.AuthZTrustAnyForwarder,
		},
		TrustDomain:     servicecontract.Value(cfg.TrustDomain()),
		TrustDomainKnob: "KACHO_NOTIFYPROBE_AUTHZ_TRUST_DOMAIN",

		ServiceIdentity: ports.identity,

		Authz:        servicecontract.AuthzViaIAM,
		CheckEdge:    servicecontract.NewPeerEdge(cfg.AuthZIAMGRPCAddr, checkCreds),
		PeerCheck:    authziam.NewCheckClient,
		CacheWindow:  cfg.AuthZCacheTTL,
		ClientBudget: cfg.AuthZCheckTimeout,
		AuthzObserve: ports.authzObserve,
		// Бюджет отказов — величина: решение о доступе проба принимает вопросом
		// к владельцу модели, и шторм отказов уронил бы его, а не пробу.
		DenyBudget: servicecontract.Value(cfg.AuthZDenyBudgetPerSec),

		Metrics:        ports.metrics,
		HandlingBudget: cfg.HandlingBudget,
		Admission:      servicecontract.Value(admission),

		DBSSLMode:     servicecontract.Value(cfg.DBSSLMode),
		PublicAddr:    ":" + cfg.GrpcPort,
		InternalAddr:  ":" + cfg.InternalGrpcPort,
		PublicCreds:   publicCreds,
		InternalCreds: internalCreds,

		Emits: servicecontract.NotApplicable[[]proxytuple.Relation](
			"кортежей владельцу прав проба не эмитит: объект notification_feed:notify-probe и " +
				"право notify на него заводит применитель манифеста kaname по строке " +
				"`notifications: {namespace: notify-probe, readers: [notify]}`, а не служба"),
		Registers: servicecontract.NotApplicable[[]servicecontract.ObjectType](
			"своих типов объектов модели прав проба не заводит: notification_feed — тип " +
				"платформы, его экземпляр объявляет манифест модуля"),
		HideExistence: servicecontract.NotApplicable[map[servicecontract.ObjectType]servicecontract.NotFoundFormat](
			"скрывать нечего: пообъектных методов, отвечающих «нет такого», у пробы нет — " +
				"объект Claim и Ack один и привязан к серверу (ScopeBound)"),
		Delivery: servicecontract.NotApplicable[servicecontract.DeliveryProvenance](
			"намерений регистрации проба не эмитит (см. Emits), доставлять нечего"),
		BootGate: servicecontract.NotApplicable[servicecontract.BootGate](
			"очереди регистраций у пробы нет (см. Emits), а её службы — Internal*, " +
				"которые под загрузочный гейт мутаций не подпадают by construction"),
	}

	if ports.parts.serving() {
		// Привязку приносит сам сервер ленты: notification_feed:notify-probe.
		// Второго написания имени модуля в корне нет.
		spec.Bound = []servicecontract.Bound{ports.parts.server.Bound()}
		spec.Narrowers = servicecontract.Value(map[servicecontract.MethodFQN]servicecontract.ListNarrower{
			servicecontract.MethodFQN(subscriptionv1.InternalSubscriptionService_Subscribe_FullMethodName): ports.narrower,
		})
		spec.StreamBudget = servicecontract.Value(cfg.SubscriptionStreamBudget)
	} else {
		spec.Narrowers = servicecontract.NotApplicable[map[servicecontract.MethodFQN]servicecontract.ListNarrower](
			"доставка выключена: поток подписки не объявлен, сужать нечего")
		spec.StreamBudget = servicecontract.NotApplicable[time.Duration](
			"доставка выключена: серверных стримов процесс не служит — журнал подписки без " +
				"ключа ленты пуст и не собирается")
	}
	return servicecontract.New(spec)
}
