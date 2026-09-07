// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package kacho — CQRS-разделенный корневой контракт репозитория VPC.
//
// Repository / RepositoryReader / RepositoryWriter явно разделяют read-path и
// write-path: use-case-слой по типу вызова видит, читает он ресурс или меняет.
// Это позволяет роутить чтение на slave-реплику и фиксирует точку открытия
// транзакции.
//
// Адаптеры:
//   - internal/repo/kacho/pg/ — pgxpool-impl (read-only TX vs RW TX).
//   - internal/repo/kacho/kachomock/ — in-memory implementation для unit-тестов
//     use-case'ов.
package kacho

import (
	"context"
	"time"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/fgaregister"
)

// Repository — корневой контракт repo-слоя VPC.
//
// Reader(ctx) открывает read-only TX (read-committed) на slave-pool'е, если он
// настроен в pg-импл (разгрузка master от чтения); иначе на master (fallback).
// Caller обязан вызвать Close() после использования (rollback read-only TX).
//
// Writer(ctx) открывает RW TX на master-pool'е. Caller обязан вызвать либо
// Commit(), либо Abort() (Abort идемпотентен — безопасно ставить через defer).
type Repository interface {
	Reader(ctx context.Context) (RepositoryReader, error)
	Writer(ctx context.Context) (RepositoryWriter, error)
	Close()
}

// RepositoryReader — read-only проекция репозитория. Каждый ресурс-специфичный
// reader возвращается через свой метод на текущей read-TX. SecurityGroups()
// нужен, чтобы Network.Create мог inline создать default-SG в одной writer-TX.
type RepositoryReader interface {
	Networks() NetworkReaderIface
	SecurityGroups() SecurityGroupReaderIface
	Addresses() AddressReaderIface
	RouteTables() RouteTableReaderIface
	NetworkInterfaces() NetworkInterfaceReaderIface
	Subnets() SubnetReaderIface
	Gateways() GatewayReaderIface
	// AddressPools — admin-only глобальный infra-ресурс. Read-методы для
	// cascade-resolve / Check / GetUtilization / ListAddresses.
	AddressPools() AddressPoolReaderIface
	// AddressPoolBindings — admin-only explicit-биндинги (network_default).
	// Read для cascade-resolve.
	AddressPoolBindings() AddressPoolBindingReaderIface
	// CidrGroups — именованные наборы префиксов, на которые ссылаются правила
	// групп безопасности.
	CidrGroups() CidrGroupReaderIface
	// Quotas — совещательная полоса учёта числа ресурсов: спрашивает про место,
	// не занимая его. Решение принимает списание триггера в writer-TX.
	Quotas() QuotaReaderIface
	// Close завершает read-TX (rollback). Идемпотентно.
	Close() error
}

// RepositoryWriter — RW проекция репозитория. Writer видит свои writes (writer
// extends reader). Outbox-emit живет здесь же — это гарантирует атомарность DML +
// outbox в одной TX.
//
// Атомарность IPAM-flow: Insert + Allocate (SetInternalIPv4/AllocateIPFromFreelist/
// AllocateExternalIPv6/…) + outbox-emit идут через единую pgx.Tx writer'а.
// NetworkInterfaces() — NIC-writer (Insert с возможным MAC-collision sentinel).
type RepositoryWriter interface {
	Networks() NetworkWriterIface
	SecurityGroups() SecurityGroupWriterIface
	Addresses() AddressWriterIface
	RouteTables() RouteTableWriterIface
	NetworkInterfaces() NetworkInterfaceWriterIface
	Subnets() SubnetWriterIface
	Gateways() GatewayWriterIface
	// AddressPools — admin-only write-iface (CRUD + PopulateFreelistForPool).
	// Atomic DML+outbox через одну writer-TX.
	AddressPools() AddressPoolWriterIface
	// AddressPoolBindings — admin-only write для explicit-биндингов
	// (Set/Unset Network default).
	AddressPoolBindings() AddressPoolBindingWriterIface
	// CidrGroups — write-iface именованных наборов префиксов. Потолок состава и
	// отсутствие затирания держатся конструкцией базы внутри этих методов.
	CidrGroups() CidrGroupWriterIface
	// Quotas — материализация строк учёта плюс та же совещательная полоса.
	// Списания и возврата здесь НЕТ и быть не должно: их делает триггер той же
	// транзакцией, что мутацию строки ресурса.
	Quotas() QuotaWriterIface
	// Outbox — emit события в vpc_outbox в той же tx-области writer'а.
	Outbox() OutboxEmitter
	// FGARegister — emit FGA owner-tuple register/unregister intent в
	// fga_register_outbox в той же tx-области writer'а. DML ресурса +
	// register-intent коммитятся атомарно (один commit, без dual-write):
	// orphan-tuple-окно закрыто. register-drainer применяет intent через
	// kaname InternalIAMService.RegisterResource/Unregister.
	FGARegister() FGARegisterEmitter
	// Commit финализирует tx. После Commit вызов Abort — no-op.
	Commit() error
	// Abort откатывает tx. Идемпотентен — после Commit no-op, можно ставить
	// через `defer w.Abort()` сразу после открытия writer'а.
	Abort()
}

// OutboxEmitter — emit одного outbox-события (vpc_outbox row + trigger pg_notify).
// Использует pgx.Tx writer'а, поэтому DML + outbox commit'ятся атомарно: либо
// resource + event оба видны watcher'у, либо ничего (Abort).
//
// payload — произвольная map (snapshot resource'а после DML). nil → пустой JSON.
type OutboxEmitter interface {
	Emit(ctx context.Context, resource, id, projectID, action string, payload map[string]any) error
}

// FGARegisterEmitter — emit FGA owner-tuple intent в fga_register_outbox через
// pgx.Tx writer'а. DML ресурса + intent commit'ятся атомарно в одной TX: либо
// ресурс + intent оба видны (Commit), либо ничего (Abort/crash) —
// orphan-tuple-окно закрыто.
//
// Каждый fgaregister.Tuple из Intent → одна outbox-строка (one row per tuple),
// чтобы register-drainer клеймил/применял их независимо (poison/transient одного
// tuple не блокирует остальные). Пустой Intent (Tuples == nil) — no-op.
type FGARegisterEmitter interface {
	// EmitRegister пишет event_type='fga.register' строку на каждый tuple и
	// возвращает монотонную версию, которой БД проштамповала эти строки.
	//
	// Версия одна на весь вызов: `now()` в PostgreSQL — время НАЧАЛА транзакции,
	// поэтому все строки одной writer-TX получают идентичный source_version.
	// Она нужна синхронному регистратору (fgaregister.Registrar): обе доставки
	// одной регистрации обязаны нести одну версию, иначе гашение повторной
	// доставки на приёмной стороне зависит от того, кто выиграл гонку.
	// Пустой Intent → нулевое время (штамповать было нечего).
	EmitRegister(ctx context.Context, intent fgaregister.Intent) (time.Time, error)
	// EmitUnregister пишет event_type='fga.unregister' строку на каждый tuple.
	EmitUnregister(ctx context.Context, intent fgaregister.Intent) error
}
