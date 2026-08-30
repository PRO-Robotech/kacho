// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package handler

import (
	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	"github.com/PRO-Robotech/kacho/pkg/operations/operationspb"
)

// operationToProto — прослойка к общему слою: перевод строки операции в
// контракт объявлен в дереве ОДИН раз (`pkg/operations/operationspb`, #1369).
func operationToProto(op *operations.Operation) *operationpb.Operation {
	return operationspb.ToProto(op)
}
