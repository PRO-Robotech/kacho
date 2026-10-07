// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"fmt"
	"time"

	"google.golang.org/grpc/credentials"

	"github.com/PRO-Robotech/corelib/authz"
	"github.com/PRO-Robotech/corelib/authz/proxytuple"
	"github.com/PRO-Robotech/corelib/grpcsrv"
	"github.com/PRO-Robotech/corelib/servicecontract"
	"github.com/PRO-Robotech/kacho/pkg/authz/authziam"

	"github.com/PRO-Robotech/kacho/services/notify/cmd/notify-api/internal/config"
)

// serviceName — имя процесса в дескрипторе, самоотчёте и метках.
const serviceName = "notify-api"

// Имена ручек, которые называют тексты отказов конструктора дескриптора.
const (
	trustDomainKnob     = "KACHO_NOTIFY_AUTHZ_TRUST_DOMAIN"
	forwarderSANsKnob   = "KACHO_NOTIFY_AUTHZ_TRUSTED_FORWARDER_SANS"
	trustAnyKnob        = "KACHO_NOTIFY_AUTHZ_TRUST_ANY_FORWARDER"
	hostFormReason      = "notify публичного слушателя не имеет по построению (приёмка NTF-5 Р2, решение Д17): все поверхности notify-api — внутренний сервис оператора и сервисы чтения арендатором — край маршрутизирует на единственный внутренний mTLS-слушатель по одному соединению notifyInternal, и граница ban06 держится таблицами маршрутов края"
	notApplicableEmits  = "notify-api не эмитит владельцу прав намерений регистрации: права на методы InternalNoticeService спрашиваются на объекте cluster, на чтение арендатором — на области запроса (проект либо аккаунт), своих объектов модели прав у извещения нет"
	notApplicableStream = "серверных стримов notify-api не служит: InternalNoticeService, NoticeService и OperationService отдают единичный ответ"
)

// describe собирает ОБЪЯВЛЕНИЕ notify-api о себе в форме носителя «только
// внутренний слушатель» (Х5; приёмка NTF-4 Р20, таблица полей). Стражей старта
// здесь нет: их исполняют конструктор дескриптора и носитель.
func describe(cfg config.API, mode servicecontract.Mode, internalCreds, kanameCreds credentials.TransportCredentials,
	rt apiRuntime, observe func(read func() authz.Metrics)) (servicecontract.Descriptor, error) {
	d, err := servicecontract.New(servicecontract.Spec{
		Service: serviceName,
		Mode:    mode,
		Logger:  rt.Logger,

		HostForm:       servicecontract.HostInternalOnly,
		HostFormReason: hostFormReason,

		Forwarders: servicecontract.Value(cfg.TrustedForwarders()),
		// Опт-ин «доверять любому пересылающему» — ручка вне боевой посадки: в
		// боевой страж круга его не читает (corelib grpcsrv.ForwarderGate), а
		// дескриптор требует имя обеих ручек для текста отказа.
		ForwarderKnobs: servicecontract.ForwarderKnobs{
			SANs: forwarderSANsKnob, TrustAny: trustAnyKnob, OptIn: cfg.AuthzTrustAnyForwarder,
		},
		TrustDomain:     servicecontract.Value(cfg.TrustDomain()),
		TrustDomainKnob: trustDomainKnob,

		ServiceIdentity: servicecontract.NotApplicable[grpcsrv.ServiceIdentity](
			"служб, зовущих notify-api под своим именем, нет: вызывающие — край с пересланным " +
				"принципалом оператора либо арендатора; служебного принципала notify-api не принимает"),

		Authz:        servicecontract.AuthzViaIAM,
		CheckEdge:    servicecontract.NewPeerEdge(cfg.AuthzIAMGRPCAddr, kanameCreds),
		PeerCheck:    authziam.NewCheckClient,
		CacheWindow:  cfg.AuthzCacheTTL,
		ClientBudget: cfg.AuthzCheckTimeout,
		AuthzObserve: observe,
		DenyBudget:   servicecontract.Value(cfg.AuthzDenyBudgetPerSec),

		Metrics:        rt.Metrics,
		HandlingBudget: cfg.HandlingBudget,
		StreamBudget:   servicecontract.NotApplicable[time.Duration](notApplicableStream),
		// Потолок на вызывающего — пол платформы внутреннего слушателя: своей
		// величины у notify-api нет, публичной половины нет по форме хоста.
		Admission: servicecontract.Value(servicecontract.Admission{Internal: grpcsrv.PlatformInternalAdmission()}),

		DBSSLMode:     servicecontract.Value(cfg.DBSSLMode),
		InternalAddr:  ":" + cfg.InternalPort,
		InternalCreds: internalCreds,

		Emits:     servicecontract.NotApplicable[[]proxytuple.Relation](notApplicableEmits),
		Registers: servicecontract.NotApplicable[[]servicecontract.ObjectType](notApplicableEmits),
		Delivery: servicecontract.NotApplicable[servicecontract.DeliveryProvenance](
			"намерений регистрации notify-api не эмитит (см. Emits), доставлять нечего"),
		BootGate: servicecontract.NotApplicable[servicecontract.BootGate](
			"очереди регистраций у notify-api нет (см. Emits): создание извещения не ждёт " +
				"видимости объекта у владельца прав"),
		Narrowers: servicecontract.NotApplicable[map[servicecontract.MethodFQN]servicecontract.ListNarrower](
			"сужаемых каталогом методов (`scope_filtered`) у notify-api нет: списки арендатора судятся " +
				"правом на область запроса, а затронутые ресурсы Get сужает use-case"),
		HideExistence: servicecontract.NotApplicable[map[servicecontract.ObjectType]servicecontract.NotFoundFormat](
			"права спрашиваются на объекте cluster либо на области запроса до выборки, а невидимое из " +
				"области извещение отвечает тем же NOT_FOUND, что несуществующее, — отказ use-case"),
	})
	if err != nil {
		return servicecontract.Descriptor{}, fmt.Errorf("notify-api: дескриптор посадки: %w", err)
	}
	return d, nil
}
