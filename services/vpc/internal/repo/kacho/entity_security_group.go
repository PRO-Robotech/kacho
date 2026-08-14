// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package kacho

import (
	"time"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
)

// SecurityGroupRecord — repo-проекция SecurityGroup: domain.SecurityGroup +
// CreatedAt (DB-managed). CreatedAt живет в repo-проекции, а не в domain, чтобы
// domain-сущность оставалась чистой бизнес-логикой без DB-managed полей.
//
// Use-case-слой читает *SecurityGroupRecord из репозитория (CQRS-iface
// SecurityGroupReaderIface / SecurityGroupWriterIface), а пишет в репо
// *domain.SecurityGroup (без CreatedAt — его проставляет БД).
type SecurityGroupRecord struct {
	domain.SecurityGroup
	CreatedAt time.Time
	// UsedBy — потребители группы, выведенные ЧТЕНИЕМ из отношений, которые база
	// уже держит. Держится здесь, а не в domain, по той же причине, что и
	// `CreatedAt`: это не бизнес-состояние группы, а ответ базы на вопрос о ней.
	//
	// Заполняется ТОЛЬКО на путях чтения (`Get` / `List` соответствующих
	// use-case'ов) — ровно как одноимённое поле адреса. Резолверы
	// (`GetMany`/`GetForUpdate`, проверка целей правил, предусловие удаления) его
	// не заполняют и не читают: обратная ссылка стоит запроса, а платить за неё
	// там, где её никто не смотрит, значит платить ни за что.
	UsedBy []SecurityGroupReferrer
}

// Виды потребителей группы правил. Значения уезжают в `Referrer.type` наружу,
// поэтому это часть контракта, а не внутреннее имя: консоль ключуется на них,
// сопоставляя вид со своей карточкой.
const (
	// SecurityGroupReferrerNIC — интерфейс, чей набор групп содержит эту группу.
	SecurityGroupReferrerNIC = "network_interface"
	// SecurityGroupReferrerNetwork — сеть, у которой эта группа объявлена группой
	// по умолчанию.
	SecurityGroupReferrerNetwork = "network"
)

// SecurityGroupUsedByLimit — сколько потребителей группы показывается.
//
// Предел существует не ради размера ответа, а ради СТОИМОСТИ: интерфейсов,
// держащих одну группу, может быть сколько угодно (потолков на число ресурсов у
// платформы сегодня нет), и «отдать всех» означало бы неограниченный запрос на
// каждое чтение карточки.
//
// Признак «есть ещё» несёт САМА ДЛИНА ответа: запрос читает на одну строку
// больше предела (`SecurityGroupUsedByFetch`), поэтому
//
//	len(UsedBy) <= SecurityGroupUsedByLimit  ⟺  список полон;
//	len(UsedBy) == SecurityGroupUsedByLimit+1 ⟺ потребителей БОЛЬШЕ предела.
//
// Отдельного булева поля под этот признак в контракте нет, и завести его здесь
// нельзя — сообщение принадлежит другому владельцу. Форма «предел плюс одна
// строка» выбрана именно потому, что она однозначна БЕЗ такого поля: усечение
// без признака было бы молчаливым, а «показать всё» — неограниченным запросом.
const SecurityGroupUsedByLimit = 32

// SecurityGroupUsedByFetch — сколько строк читает запрос: предел плюс одна.
const SecurityGroupUsedByFetch = SecurityGroupUsedByLimit + 1

// SecurityGroupReferrer — один потребитель группы правил.
//
// Наружу уезжают вид, идентификатор и имя потребителя — и ничего сверх: ни
// подсеть, ни сеть интерфейса, ни его состояние. Обратная ссылка отвечает на
// вопрос «кем используется», а не служит вторым списком чужих ресурсов.
type SecurityGroupReferrer struct {
	// Type — один из SecurityGroupReferrer* выше.
	Type string
	ID   string
	// Name — имя потребителя на момент чтения (пустое, если у него нет имени).
	Name string
}
