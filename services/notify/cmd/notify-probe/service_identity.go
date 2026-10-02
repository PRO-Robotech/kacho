// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"fmt"

	notifyv1 "github.com/PRO-Robotech/corelib/api/corelib/notify"
	subscriptionv1 "github.com/PRO-Robotech/corelib/api/corelib/subscription"
	"github.com/PRO-Robotech/corelib/grpcsrv"
	"github.com/PRO-Robotech/corelib/servicecontract"

	"github.com/PRO-Robotech/kacho/services/notify/cmd/notify-probe/internal/config"
)

// notifyService — имя службы-читателя ленты в служебном субъекте
// `service:notify`. Читатель у ленты пробы один (манифест: readers: [notify]).
const notifyService grpcsrv.ServiceName = "notify"

// identityMethods — закрытый перечень методов звена идентичности служб (Р2):
// ровно то, что notify зовёт у источника под своим именем.
func identityMethods() []string {
	return []string{
		subscriptionv1.InternalSubscriptionService_Subscribe_FullMethodName,
		notifyv1.InternalNotificationFeedService_Claim_FullMethodName,
		notifyv1.InternalNotificationFeedService_Ack_FullMethodName,
	}
}

// serviceIdentityAxis — ось звена идентичности служб дескриптора.
//
// Выводится из того же условия, что служимая лента: процесс служит ленту —
// звено несёт перечень `{Subscribe, Claim, Ack}` и таблицу `{SAN notify →
// notify}`; не служит — изъятие с причиной. Перечень вне служимого набора
// носитель отверг бы стартом (метод вне каталога прав процесса), и это было бы
// верно: звено, объявленное для методов, которых нет, — проводка без предмета.
//
// Отказ сборки называет ручку и значение (NTF1-M09): таблицу строит оператор,
// и ему нужно знать, какое из его значений не принято.
func serviceIdentityAxis(cfg config.Config, parts feedParts) (servicecontract.Axis[grpcsrv.ServiceIdentity], error) {
	if !parts.serving() {
		return servicecontract.NotApplicable[grpcsrv.ServiceIdentity](
			"доставка извещений выключена (" + config.FlagKnob + "=false): ни ленты, ни подписки " +
				"процесс не служит, и методов, которые служба зовёт у него под своим именем, нет"), nil
	}
	id, err := grpcsrv.NewServiceIdentity(identityMethods(),
		map[string]grpcsrv.ServiceName{cfg.NotifySAN: notifyService})
	if err != nil {
		return servicecontract.Axis[grpcsrv.ServiceIdentity]{},
			fmt.Errorf("%s=%q: %w", config.NotifySANKnob, cfg.NotifySAN, err)
	}
	return servicecontract.Value(id), nil
}

// serviceIdentityReport — строка самоотчёта посадки о звене, уехавшем в
// дескриптор: метка неприменимости фундамента у изъятия, иначе — перечень и
// строки таблицы.
func serviceIdentityReport(axis servicecontract.Axis[grpcsrv.ServiceIdentity]) string {
	id, _ := axis.Get()
	return id.Report()
}
