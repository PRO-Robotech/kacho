// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package handler

import (
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/PRO-Robotech/kacho/pkg/operations"
	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
)

// operationToProto конвертирует corelib operations.Operation в proto-форму
// (OperationService.Get/мутации возвращают её клиенту). oneof result —
// error|response (заполнен только при done).
func operationToProto(op *operations.Operation) *operationpb.Operation {
	if op == nil {
		return nil
	}
	p := &operationpb.Operation{
		Id:          op.ID,
		Description: op.Description,
		// Truncate до секунд — единый apiconv-формат timestamp'ов на wire
		// (микросекунды с БД не текут наружу, как и в protoconv.ts для Region/Zone).
		CreatedAt:            timestamppb.New(op.CreatedAt.Truncate(time.Second)),
		CreatedBy:            op.CreatedBy,
		ModifiedAt:           timestamppb.New(op.ModifiedAt.Truncate(time.Second)),
		Done:                 op.Done,
		Metadata:             op.Metadata,
		PrincipalType:        op.Principal.Type,
		PrincipalId:          op.Principal.ID,
		PrincipalDisplayName: op.Principal.DisplayName,
	}
	if op.Error != nil {
		p.Result = &operationpb.Operation_Error{Error: op.Error}
	} else if op.Response != nil {
		p.Result = &operationpb.Operation_Response{Response: op.Response}
	}
	return p
}
