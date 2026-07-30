// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A pool-size setting that nothing reads is worse than no setting: it is declared,
// documented, validated and shipped in the chart, so an operator sizing the pool
// against a saturated service changes a number that goes nowhere. The width of the
// pool decides how many concurrent Lists it takes before Get and every mutation
// start waiting, so this particular knob has to reach the pool.
//
// DSN is the only place that may carry pool_* parameters: they are understood by
// pgxpool.ParseConfig and are FATAL for database/sql (migrator) and for a single
// pgx.Conn (the lifecycle LISTEN feed) — both of which keep taking the plain URL.

func poolCfg(url string) Config {
	var c Config
	c.Repository.Postgres.URL = url
	return c
}

func TestDSN_CarriesMaxConnsToThePool(t *testing.T) {
	t.Parallel()
	c := poolCfg("postgres://u:p@h:5432/kacho_nlb?sslmode=require")
	c.Repository.Postgres.MaxConns = 25

	dsn := c.DSN()
	require.Contains(t, dsn, "pool_max_conns=25",
		"the configured pool size must reach the pool — otherwise the knob is decoration")
	require.True(t, strings.HasPrefix(dsn, c.Repository.Postgres.URL),
		"the DSN must extend the configured URL, not rewrite it: got %q", dsn)
}

func TestDSN_CarriesConnLifetimeToThePool(t *testing.T) {
	t.Parallel()
	c := poolCfg("postgres://u:p@h:5432/kacho_nlb?sslmode=require")
	c.Repository.Postgres.ConnLifetime = 30 * time.Minute

	require.Contains(t, c.DSN(), "pool_max_conn_lifetime=30m0s")
}

// Unset knobs must add nothing: the pgxpool defaults stay in force and the DSN is
// byte-identical to the configured URL.
func TestDSN_UnsetKnobsLeaveTheURLUntouched(t *testing.T) {
	t.Parallel()
	c := poolCfg("postgres://u:p@h:5432/kacho_nlb?sslmode=require")

	require.Equal(t, c.Repository.Postgres.URL, c.DSN())
}

// A URL without a query string must still get a well-formed one.
func TestDSN_AppendsFirstParameterWithQuestionMark(t *testing.T) {
	t.Parallel()
	c := poolCfg("postgres://u:p@h:5432/kacho_nlb")
	c.Repository.Postgres.MaxConns = 4

	require.Equal(t, "postgres://u:p@h:5432/kacho_nlb?pool_max_conns=4", c.DSN())
}

// Empty URL stays empty — Validate is what reports a missing URL, and DSN must not
// turn "" into a string that looks like a connection.
func TestDSN_EmptyURLStaysEmpty(t *testing.T) {
	t.Parallel()
	c := poolCfg("")
	c.Repository.Postgres.MaxConns = 10

	require.Equal(t, "", c.DSN())
}
