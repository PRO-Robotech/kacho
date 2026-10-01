// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package idempotencypg_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/PRO-Robotech/corelib/pgtest"
	"github.com/PRO-Robotech/kacho/gateway/internal/config"
	"github.com/PRO-Robotech/kacho/gateway/internal/idempotencypg"
)

// TestPurgeAnonMail_RemovesOnlyWhatOutlivedItsTerm — уборщик хранилища
// однократности уносит моменты пропуска старше срока З26 и пометки вызовов
// после срока, а всё живое и строку ведра оставляет (З26, CX2-35).
func TestPurgeAnonMail_RemovesOnlyWhatOutlivedItsTerm(t *testing.T) {
	dsn := pgtest.NewDB(t)
	s, err := idempotencypg.New(context.Background(), idempotencypg.Config{DSN: dsn})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	c, err := pgx.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close(context.Background()) }()
	ret := config.AnonMailPassRetention()
	id := make([]byte, 16)
	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO kacho_gateway.anon_mail_passes (key, at, decision_id) VALUES ('old', now() - $1::interval - interval '1 minute', $2)`, []any{ret, id}},
		{`INSERT INTO kacho_gateway.anon_mail_passes (key, at, decision_id) VALUES ('live', now() - $1::interval + interval '1 minute', $2)`, []any{ret, id}},
		{`INSERT INTO kacho_gateway.pow_spent (id, expires_at) VALUES ('\x00000000000000000000000000000001', now() - interval '1 second')`, nil},
		{`INSERT INTO kacho_gateway.pow_spent (id, expires_at) VALUES ('\x00000000000000000000000000000002', now() + interval '5 minutes')`, nil},
	} {
		if _, err := c.Exec(context.Background(), stmt.sql, stmt.args...); err != nil {
			t.Fatalf("%s: %v", stmt.sql, err)
		}
	}
	sw, err := s.PurgeAnonMail(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var passes, spent, bucket int
	_ = c.QueryRow(context.Background(), `SELECT count(*) FROM kacho_gateway.anon_mail_passes`).Scan(&passes)
	_ = c.QueryRow(context.Background(), `SELECT count(*) FROM kacho_gateway.pow_spent`).Scan(&spent)
	_ = c.QueryRow(context.Background(), `SELECT count(*) FROM kacho_gateway.anon_mail_bucket`).Scan(&bucket)
	t.Logf("унесено %d (догнала %v) · осталось моментов %d, пометок %d, строк ведра %d · срок хранения %s",
		sw.Removed, sw.Drained, passes, spent, bucket, ret)
	if sw.Removed != 2 || passes != 1 || spent != 1 || bucket != 1 || !sw.Drained {
		t.Fatalf("ожидалось унесено 2, осталось 1 · 1 · 1")
	}
}
