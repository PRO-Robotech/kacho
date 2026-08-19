// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo

import (
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/pagetoken"
)

// invalidPageTokenErr оборачивает отказ разбора курсора в контрактный InvalidArgument.
//
// Причина разбора НАРУЖУ НЕ ТЕЧЁТ. Прежняя редакция подставляла её в текст (`%v`), и
// клиент получал внутреннюю форму токена — а проверка того же входа на пути запроса
// давала ДРУГОЙ текст. Один и тот же дефект описывался двумя сообщениями, и то, какое
// из них увидит вызывающий, зависело от того, дошёл ли запрос до базы.
func invalidPageTokenErr(err error) error {
	_ = err
	return status.Error(codes.InvalidArgument, "page_token is invalid")
}

// invalidFilterErr оборачивает ParseError из filter.Parse в gRPC InvalidArgument.
func invalidFilterErr(err error) error {
	return status.Error(codes.InvalidArgument, err.Error())
}

// encodePageToken кодирует (created_at, id) в опаковый курсор.
//
// Форма объявлена ОДИН раз — в pkg/pagetoken. Здесь её не воспроизводят: копия
// разошлась бы с проверкой формата молча, потому что обе возвращают «валидно» на
// валидном входе.
func encodePageToken(createdAt time.Time, id string) string {
	return pagetoken.EncodeKeysetTime(pagetoken.DefaultOrder, createdAt, id)
}

// decodePageToken разбирает опаковый курсор обратно в (created_at, id).
func decodePageToken(token string) (time.Time, string, error) {
	return pagetoken.DecodeKeysetTime(token, pagetoken.DefaultOrder)
}
