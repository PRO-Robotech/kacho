// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package helpers

import (
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/pagetoken"
)

// InvalidPageTokenErr оборачивает отказ разбора курсора в gRPC InvalidArgument.
//
// Причина разбора наружу НЕ течёт: page_token — клиентский вход, а не состояние
// домена, и внутренняя форма токена вызывающего не касается. Прежняя редакция
// подставляла причину в текст (`%v`) — тогда проверка на пути запроса и разбор на
// пути чтения описывали один и тот же дефект двумя разными сообщениями.
func InvalidPageTokenErr(err error) error {
	_ = err
	return status.Error(codes.InvalidArgument, "page_token is invalid")
}

// InvalidFilterErr оборачивает ParseError из filter.Parse в gRPC InvalidArgument
// со стабильным message ("Bad expression at column N. ...").
func InvalidFilterErr(err error) error {
	return status.Error(codes.InvalidArgument, err.Error())
}

// EncodePageToken кодирует (created_at, id) в опаковый курсор.
//
// Форма объявлена ОДИН раз — в pkg/pagetoken; здесь её не воспроизводят.
func EncodePageToken(createdAt time.Time, id string) string {
	return pagetoken.EncodeKeysetTime(pagetoken.DefaultOrder, createdAt, id)
}

// DecodePageToken разбирает опаковый курсор обратно в (created_at, id).
func DecodePageToken(token string) (time.Time, string, error) {
	return pagetoken.DecodeKeysetTime(token, pagetoken.DefaultOrder)
}
