// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build integration

package repo

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jackc/pgx/v5/pgxpool"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	"github.com/PRO-Robotech/kacho/pkg/pgtest"
)

// auditTestPool — пул к развёрнутой на пробу базе с применёнными миграциями.
//
// Свой помощник, а не переиспользование соседнего: тот живёт во внешнем тестовом
// пакете, а эти пробы обязаны быть внутренними — они утверждают о ФУНКЦИИ записи
// журнала, которая приватна и должна такой остаться. Экспортировать её ради
// пробы значило бы расширить поверхность пакета под нужды теста.
func auditTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := pgtest.NewDB(t)
	// Строки учёта квоты: без них вставка ресурса отвергалась бы «потолок не
	// назван» и маскировала предмет пробы. Пробы самого учёта заводят свои
	// строки сами и в перечень фикстуры не входят.
	SeedFixtureQuotas(t, dsn)
	pool, err := coredb.NewPool(context.Background(), dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	return pool
}

// TestAudit_RollbackTakesTheRecordWithIt — несущее свойство журнала: запись
// живёт и умирает вместе с мутацией.
//
// # Почему отрицание идёт В ПАРЕ
//
// Проба «после отката строк ноль» сама по себе зеленеет и тогда, когда журнал
// не пишется вовсе — а это ровно тот отказ, который она должна ловить. Поэтому
// сначала утверждается, что коммит даёт РОВНО ОДНУ строку с обоими лицами, и
// только потом — что откат не оставляет ни одной.
//
// # Почему «ровно одна», а не «хотя бы одна»
//
// Две записи об одном действии — это не избыточность, а расхождение: читатель
// журнала не может знать, какая из них описывает то, что произошло.
func TestAudit_RollbackTakesTheRecordWithIt(t *testing.T) {
	pool := auditTestPool(t)
	ctx := operations.WithPrincipal(context.Background(),
		operations.Principal{Type: "user", ID: "usr-auditor"})

	countFor := func(id string) int {
		var n int
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT count(*) FROM audit_outbox WHERE resource_id = $1`, id).Scan(&n))
		return n
	}

	// (+) ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: коммит даёт ровно одну запись, и она несёт
	// актора. Без этой половины отрицание ниже ничего не доказывает.
	const committedID = "ins-audit-committed"
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	actor, onBehalf := auditPrincipals(ctx)
	require.NoError(t, emitAudit(ctx, tx, AuditEvent{
		EventType:    "instance.create",
		ResourceType: "Instance",
		ResourceID:   committedID,
		ProjectID:    "prj-audit",
		Actor:        actor,
		OnBehalfOf:   onBehalf,
	}))
	require.NoError(t, tx.Commit(ctx))

	require.Equal(t, 1, countFor(committedID), "коммит обязан оставить ровно одну запись")

	var gotActorType, gotActorID string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT actor_type, actor_id FROM audit_outbox WHERE resource_id = $1`, committedID).
		Scan(&gotActorType, &gotActorID))
	require.Equal(t, "user", gotActorType)
	require.Equal(t, "usr-auditor", gotActorID,
		"журнал без актора не отвечает на вопрос, ради которого существует")

	// (−) ОТРИЦАНИЕ: откат уносит запись с собой.
	const rolledBackID = "ins-audit-rolled-back"
	tx2, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, emitAudit(ctx, tx2, AuditEvent{
		EventType:    "instance.create",
		ResourceType: "Instance",
		ResourceID:   rolledBackID,
		Actor:        actor,
	}))
	require.NoError(t, tx2.Rollback(ctx))

	require.Zero(t, countFor(rolledBackID),
		"запись, пережившая откат мутации, утверждает о действии, которого не было")
}

// TestAudit_ActorIsRequired — пустой актор отвергается, а не записывается
// пустым.
//
// Пустое поле актора означало бы не «неизвестно», а «мы не посмотрели», и
// отличить это потом было бы нечем: запись выглядит полной, а ответственности
// в ней нет.
func TestAudit_ActorIsRequired(t *testing.T) {
	pool := auditTestPool(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()

	err = emitAudit(ctx, tx, AuditEvent{
		EventType:    "instance.create",
		ResourceType: "Instance",
		ResourceID:   "ins-no-actor",
	})
	require.Error(t, err, "запись без актора не должна попадать в журнал")
	require.Contains(t, err.Error(), "actor is required")
}

// TestAudit_SecondFaceIsAllOrNothing — половина второго лица отвергается базой.
//
// «Тип есть, идентификатора нет» — состояние, из которого нельзя понять, от
// чьего имени действовали. Свойство держится ограничением схемы, а не проверкой
// в коде: проверка в коде защищает только тот путь, который через неё проходит.
func TestAudit_SecondFaceIsAllOrNothing(t *testing.T) {
	pool := auditTestPool(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx, `INSERT INTO audit_outbox
		(id, event_type, resource_type, resource_id, actor_type, actor_id,
		 on_behalf_of_type, on_behalf_of_id)
		VALUES ('aud00000000000000000', 'instance.create', 'Instance', 'ins-x',
		        'user', 'usr-1', 'user', '')`)
	require.Error(t, err, "половина второго лица обязана отвергаться базой")
}
