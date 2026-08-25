// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// expired_credential_reclaimer.go — долговечная половина снятия истёкших
// удостоверений (задача #1264).
//
// ЗАГЛУШКА ПЕРВОГО ЗАХОДА: находит ноль, снимает ноль. Существует затем, чтобы
// проба предмета могла УПАСТЬ НА ПОВЕДЕНИИ, а не на отсутствии символа, — то
// есть чтобы «красное» доказывало, что проба различает наличие свойства и его
// отсутствие. Заменяется настоящим отбором в том же изменении.
package pg

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ExpiredCredentialReclaimSpec — границы одного прогона. Длительности, а не
// моменты: момент вычисляет база своими часами.
type ExpiredCredentialReclaimSpec struct {
	MinDelay  time.Duration
	Grace     time.Duration
	BatchSize int
	DryRun    bool
}

// ExpiredCredentialReclaimResult — перепись прогона.
type ExpiredCredentialReclaimResult struct {
	Found     int
	Reclaimed int
	ByKind    map[string]int
}

// ExpiredCredentialReclaimer снимает удостоверения обеих таблиц.
type ExpiredCredentialReclaimer struct {
	pool   *pgxpool.Pool
	schema string
}

// NewExpiredCredentialReclaimer — конструктор композиционного корня.
func NewExpiredCredentialReclaimer(pool *pgxpool.Pool, schema string) *ExpiredCredentialReclaimer {
	if schema == "" {
		schema = "kacho_iam"
	}
	return &ExpiredCredentialReclaimer{pool: pool, schema: schema}
}

// ReclaimExpiredCredentials — заглушка.
func (r *ExpiredCredentialReclaimer) ReclaimExpiredCredentials(
	_ context.Context, _ ExpiredCredentialReclaimSpec,
) (ExpiredCredentialReclaimResult, error) {
	return ExpiredCredentialReclaimResult{ByKind: map[string]int{}}, nil
}
