// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"github.com/PRO-Robotech/corelib/grpcsrv"
	"github.com/PRO-Robotech/corelib/servicecontract"
)

// serviceIdentityAxis — ось звена идентичности служб дескриптора
// (corelib servicecontract.Spec.ServiceIdentity, NTF-1 Р2).
//
// Звено опознаёт пира, НЕ говорящего за другого, как службу `service:<имя>` —
// и только на закрытом перечне методов. Такой перечень есть у процесса,
// который служит ленту уведомлений службам-подписчикам (Subscribe, Claim, Ack);
// kacho-geo ленты не служит, и ни одного метода, который служба зовёт у него под
// своим именем, у процесса нет. «Звена нет» пишется ровно одним способом —
// изъятием с причиной; пустое звено значением конструктор отвергает.
//
// Предикат снятия изъятия — внешний и механический: процесс поднимает сервер
// ленты уведомлений. Тем же изменением ось становится значением
// grpcsrv.NewServiceIdentity с перечнем его методов, а носитель сверяет перечень
// с каталогом прав процесса и на расхождении отказывает в старте.
//
// Функция одна на дескриптор и на самоотчёт посадки (serviceIdentityReport):
// два места об одном звене читают одно значение и разойтись не могут.
func serviceIdentityAxis() servicecontract.Axis[grpcsrv.ServiceIdentity] {
	return servicecontract.NotApplicable[grpcsrv.ServiceIdentity](
		"службы-подписчики сюда не ходят: kacho-geo не служит ленты уведомлений, и методов, " +
			"которые служба зовёт у процесса под своим именем, у него нет")
}

// serviceIdentityReport — строка самоотчёта посадки о звене, которое уехало в
// дескриптор. У процесса без звена это метка неприменимости фундамента
// (grpcsrv.ServiceIdentityNotApplicable), а не пустая строка: пустое значение
// неотличимо от «самоотчёт не заполнили».
func serviceIdentityReport() string {
	id, _ := serviceIdentityAxis().Get()
	return id.Report()
}
