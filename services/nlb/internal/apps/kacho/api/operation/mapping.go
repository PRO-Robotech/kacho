// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package operation

import (
	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/apps/kacho/api/shared"
)

// operationToProto — тонкий делегатор к единому `shared.OperationToProto`
// (один источник истины для всех use-case пакетов).
func operationToProto(op *operations.Operation) *operationpb.Operation {
	return shared.OperationToProto(op)
}
