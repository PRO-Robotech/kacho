// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: AGPL-3.0-or-later

package access_binding_test

import (
	stderrors "errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

type pgxpoolT = pgxpool.Pool

func asPg(err error, target any) bool { return stderrors.As(err, target) }
