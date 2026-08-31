// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package serviceerr

import (
	"strings"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// InvalidArg формирует gRPC InvalidArgument с FieldViolation-деталью.
// Зеркалит kacho-vpc/internal/apps/kacho/shared/serviceerr/build.go::InvalidArg.
func InvalidArg(field, desc string) error {
	st := status.New(codes.InvalidArgument, desc)
	br := &errdetails.BadRequest{
		FieldViolations: []*errdetails.BadRequest_FieldViolation{
			{Field: field, Description: desc},
		},
	}
	if withDetails, derr := st.WithDetails(br); derr == nil {
		return withDetails.Err()
	}
	return st.Err()
}

// FieldViolation — ОДНО нарушение: путь поля и правило, относящееся именно к
// этому полю. Пара неразделима намеренно: `InvalidArg` принимает имя поля и
// текст двумя независимыми аргументами, и они расходятся молча — вызывающий
// правит текст и не правит имя. Здесь править нечего порознь.
type FieldViolation struct {
	// Field — путь поля в форме контракта (snake_case), машиночитаемый:
	// google.rpc.BadRequest.field_violations[].field.
	Field string
	// Desc — правило, относящееся К ЭТОМУ полю. Тон — как у остальных отказов:
	// `<camelCasePath> <правило>`.
	Desc string
}

// InvalidArgFields — отказ, называющий КАЖДОЕ нарушенное поле своим путём.
//
// Зачем отдельно от `InvalidArg`. У одного правила бывает несколько нарушителей
// сразу (четыре output-only подполя источника загрузки). `InvalidArg` умеет
// назвать ровно одно поле, поэтому такой отказ либо называл РОДИТЕЛЯ и
// перечислял подполя текстом — тогда машиночитаемое имя указывает на поле, к
// которому правило не относится (а родитель обычно ещё и обязателен, так что
// клиент, снявший названное, ломает запрос), — либо отдавал нарушителей по
// одному, по кругу запроса на каждого.
//
// Здесь сообщаются ВСЕ нарушители сразу, каждый своим путём: клиент снимает
// ровно то, что прислал, за один заход. Та же форма, что у
// `handler.RejectUnsupportedCreateFields`, и по той же причине.
//
// Сообщение при ОДНОМ нарушителе — дословно его правило, то есть тон совпадает
// с `InvalidArg` и текст остаётся частью контракта. При нескольких правила
// соединяются `; ` в порядке объявления полей в контракте (порядок
// детерминирован — его задаёт вызывающий перечнем, а не картой).
func InvalidArgFields(violations ...FieldViolation) error {
	if len(violations) == 0 {
		// Отказ без нарушителя назвать нечем: вызывающий обязан проверить
		// непустоту сам. Возвращаем nil, чтобы «нарушений нет» не превратилось
		// в отказ без предмета.
		return nil
	}
	msgs := make([]string, 0, len(violations))
	fvs := make([]*errdetails.BadRequest_FieldViolation, 0, len(violations))
	for _, v := range violations {
		msgs = append(msgs, v.Desc)
		fvs = append(fvs, &errdetails.BadRequest_FieldViolation{Field: v.Field, Description: v.Desc})
	}
	st := status.New(codes.InvalidArgument, strings.Join(msgs, "; "))
	if withDetails, derr := st.WithDetails(&errdetails.BadRequest{FieldViolations: fvs}); derr == nil {
		return withDetails.Err()
	}
	return st.Err()
}
