// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package clients_test

import (
	"os"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/pgtest"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/migrations"
)

// TestMain hands this package one Postgres instead of one per test.
//
// setupRegisterOutboxDB used to start a container AND replay the whole migration
// chain on every call; the container now starts once, the chain runs once into a
// template, and each caller gets its own database cloned from it. See
// internal/pgtest for why a clone is the same isolation a separate container gave.
//
// The container starts lazily on first use, so `-short` — under which every
// integration test here skips itself — still pays nothing.
func TestMain(m *testing.M) {
	os.Exit(pgtest.Run(m, pgtest.Config{
		// Приведение схемы — ОДИН раз на пакет, у выдающего базу.
		// Прежде его приписывал каждый вызывающий своей копией; забывший
		// получал `relation … does not exist` — отказ, читающийся как дефект
		// продукта. Довод целиком — `internal/pgtest` §WithSearchPath.
		SearchPath: "kacho_vpc,public",
		Name:       "vpc",
		User:       "vpc",
		Password:   "vpc",
		Migrate:    pgtest.Goose(migrations.FS),
	}))
}
