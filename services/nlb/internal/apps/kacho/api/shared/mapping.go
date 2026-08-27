// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package shared

import (
	"errors"

	"google.golang.org/genproto/googleapis/rpc/errdetails"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

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

// PeerErrToStatus — peer-client error → gRPC-status. Единый источник истины
// (раньше продублирован в loadbalancer/targetgroup). Используется при sync
// project/region precheck и в worker per-target peer-validate.
//
// Порядок веток несущий, а не косметический. Peer-клиент, уже собравший полосу
// резолва (код + машинный признак в details), проходит НАСКВОЗЬ: пересборка
// статуса в sentinel-ветке потеряла бы details, а несовпадение с sentinel'ами
// увело бы готовую полосу в общую ветку INTERNAL — то есть отказ переставал бы
// быть отличимым ровно на пути к клиенту. Sentinel'ы, которые классифицирует
// сам маппер, идут после и работают как прежде.
//
// codes.Unknown под pass-through НЕ попадает: это код, который status.FromError
// присваивает не-статусной ошибке, а не заявление полосы.
func PeerErrToStatus(err error, kind, id string) error {
	if err == nil {
		return nil
	}
	if st, ok := status.FromError(err); ok && st.Code() != codes.Unknown {
		return err
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
