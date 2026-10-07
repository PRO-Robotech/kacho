// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package paging — разбор и нарезка страниц трёх списков извещений (внутренний
// `List`, публичные `List` и `ListByAccount`): keyset `(created_at, id)`, курсор —
// общий кодек страниц фундамента (`corelib/pagetoken`), третьей формы курсора нет
// (замысел issue-2924 З13 п.2).
package paging

import (
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/corelib/pagetoken"
	corevalidate "github.com/PRO-Robotech/corelib/validate"

	"github.com/PRO-Robotech/kacho/services/notify/internal/apps/notify/api/notice"
)

// Request — разобранные page_size и page_token.
type Request struct {
	size  int
	after *pagetoken.Cursor
}

// Parse разбирает page_size (`0` — 50, наибольшее 1000; вне `[0..1000]` —
// отказ, без подрезки) и page_token (мусорный — отказ, а не первая страница).
// Текст отказа называет поле (Р16, `api-pagesize`).
func Parse(size int64, token string) (Request, error) {
	n, err := corevalidate.PageSize("page_size", size)
	if err != nil {
		return Request{}, status.Errorf(codes.InvalidArgument,
			"page_size: must be in [0..%d] (0 means default)", corevalidate.MaxPageSize)
	}
	after, ok := pagetoken.Decode(token)
	if !ok {
		return Request{}, status.Error(codes.InvalidArgument, "page_token: malformed")
	}
	return Request{size: int(n), after: after}, nil
}

// Query — страница хранилища: строки строго после курсора, на одну больше
// размера — лишняя строка признаёт следующую страницу.
func (r Request) Query() notice.Page {
	p := notice.Page{Limit: r.size + 1}
	if r.after != nil {
		p.AfterCreatedAt, p.AfterID = r.after.CreatedAt, r.after.ID
	}
	return p
}

// Cut отрезает лишнюю строку и кодирует курсор следующей страницы; пустой
// курсор — на странице с последней строкой.
func Cut[T any](r Request, rows []T, key func(T) (time.Time, string)) ([]T, string) {
	if len(rows) <= r.size {
		return rows, ""
	}
	rows = rows[:r.size]
	at, id := key(rows[len(rows)-1])
	return rows, pagetoken.Encode(pagetoken.Cursor{CreatedAt: at, ID: id})
}
