// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package shared

import (
	"errors"

	"google.golang.org/genproto/googleapis/rpc/errdetails"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// OperationToProto — единый domain `operations.Operation` → proto Operation
// маппер для всех use-case пакетов kacho-nlb (loadbalancer / listener /
// targetgroup / operation). Раньше был скопирован byte-for-byte в четырёх
// местах и успел разойтись (часть копий имела nil-guard, часть — нет).
// Здесь — один источник истины: nil → nil, principal_* заполняются,
// result-oneof (error|response) выставляется.
func OperationToProto(op *operations.Operation) *operationpb.Operation {
	if op == nil {
		return nil
	}
	p := &operationpb.Operation{
		Id:                   op.ID,
		Description:          op.Description,
		CreatedAt:            timestamppb.New(op.CreatedAt),
		CreatedBy:            op.CreatedBy,
		ModifiedAt:           timestamppb.New(op.ModifiedAt),
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

// ErrInvalidArg — InvalidArgument с указанием поля + ошибки. Единый источник
// истины (раньше идентично продублирован в loadbalancer/targetgroup/announce);
// handler'ы используют его для sync-проверок required-полей.
//
// Поле называется ДВАЖДЫ: в тексте сообщения (тон контракта, `<field>: <msg>`,
// байт-в-байт как раньше) и в `google.rpc.BadRequest` — машиночитаемо.
//
// Зачем деталь. В nlb две полосы, обе дают «INVALID_ARGUMENT с именем поля»:
// доменная (`coreerrors.InvalidArgument().AddFieldViolation`) уже эмитит
// BadRequest, а эта — только прозу. Одно и то же сообщение запроса отвечало
// вызывающему то структурой, то текстом: неверный адрес цели давал нарушения
// полей, а output-only `status` в том же теле — голую строку. Клиент, которому
// `api-conventions.md` прямо запрещает разбирать прозу, не имел на этой полосе
// ничего, на что можно ключеваться.
//
// Сообщение НЕ меняется: это добавление детали, а не переформулировка. Если
// прикрепить деталь не удалось, возвращается исходный статус — диагностика не
// может стоить вызывающему самого отказа.
func ErrInvalidArg(field, msg string) error {
	st := status.New(codes.InvalidArgument, field+": "+msg)
	if next, derr := st.WithDetails(&errdetails.BadRequest{
		FieldViolations: []*errdetails.BadRequest_FieldViolation{
			{Field: field, Description: msg},
		},
	}); derr == nil {
		st = next
	}
	return st.Err()
}

// PeerErrToStatus — peer-client error (sentinel-wrapped) → gRPC-status. Единый
// источник истины (раньше продублирован в loadbalancer/targetgroup).
// Используется при sync project/region precheck и в worker per-target
// peer-validate.
func PeerErrToStatus(err error, kind, id string) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return status.Errorf(codes.InvalidArgument, "%s %s not found", caser(kind), id)
	case errors.Is(err, domain.ErrInvalidArg):
		return status.Errorf(codes.InvalidArgument, "%s: %v", kind, err)
	case errors.Is(err, domain.ErrFailedPrecondition):
		return status.Errorf(codes.FailedPrecondition, "%s %s: %v", kind, id, err)
	case errors.Is(err, domain.ErrUnavailable):
		return status.Errorf(codes.Unavailable, "%s lookup unavailable", kind)
	}
	return status.Errorf(codes.Internal, "%s lookup failed", kind)
}

// caser — Title-case первого символа kind ("project" → "Project").
func caser(s string) string {
	if s == "" {
		return s
	}
	b := []byte(s)
	if b[0] >= 'a' && b[0] <= 'z' {
		b[0] -= 32
	}
	return string(b)
}
