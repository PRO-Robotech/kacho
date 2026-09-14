// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package handler

import (
	operationpb "github.com/PRO-Robotech/corelib/api/kacho/cloud/operation"
	"github.com/PRO-Robotech/corelib/operations"
	"github.com/PRO-Robotech/corelib/operations/operationspb"
)

// operationToProto — прослойка к общему слою: перевод строки операции в
// контракт объявлен в дереве ОДИН раз (`corelib/operations/operationspb`, #1369).
func operationToProto(op *operations.Operation) *operationpb.Operation {
	return operationspb.ToProto(op)
}
