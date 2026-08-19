// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package instance

import (
	"encoding/base64"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/pagetoken"
)

// Проба стережёт проверку пагинации, которая бежит ДО короткого замыкания пустого
// гранта: мусорный курсор и размер вне диапазона обязаны давать InvalidArgument
// независимо от того, что вызывающему выдано (дефект: список отдавал `200 {[]}` на
// мусорный курсор при пустом гранте, расходясь с конвенцией и с vpc).
//
// Годный токен берётся У ПРОИЗВОДИТЕЛЯ, а не выписывается литералом. Прежняя редакция
// собирала его руками — то есть была ЕЩЁ ОДНИМ объявлением формата и зеленела бы ровно
// до тех пор, пока литерал совпадал с кодеком. Заодно она закрепляла деталь снятого
// зеркала («в теле есть двоеточие»), а не свойство контракта.
func TestValidateListPagination(t *testing.T) {
	validToken := pagetoken.EncodeKeysetTime(
		pagetoken.DefaultOrder,
		time.Unix(0, 1700000000000000000).UTC(),
		"epd0000000000000000",
	)
	cases := []struct {
		name    string
		p       Pagination
		wantErr bool
	}{
		{"пустой токен + размер по умолчанию", Pagination{PageToken: "", PageSize: 0}, false},
		{"размер на верхней границе", Pagination{PageSize: 1000}, false},
		{"токен производителя", Pagination{PageToken: validToken, PageSize: 10}, false},
		{"размер выше предела", Pagination{PageSize: 1001}, true},
		{"отрицательный размер", Pagination{PageSize: -1}, true},
		{"мусор, не base64", Pagination{PageToken: "not-a-real-token!!"}, true},
		{"валидный base64 без метки формата", Pagination{PageToken: base64.RawURLEncoding.EncodeToString([]byte("nocolonhere"))}, true},
		// Токен прежней формы этого же сервиса. Он обязан быть ОТВЕРГНУТ, а не
		// истолкован: курсор опаковый и живёт один сеанс обхода, поэтому вызывающий
		// начинает обход заново — это лучше тихо неверной страницы.
		{"токен прежней формы", Pagination{PageToken: base64.RawURLEncoding.EncodeToString([]byte("1700000000000000000:epd0000000000000000"))}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateListPagination(tc.p)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ожидался отказ, получено nil")
				}
				if got := status.Code(err); got != codes.InvalidArgument {
					t.Fatalf("ожидался InvalidArgument, получено %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ожидался проход, получено %v", err)
			}
		})
	}
}

// Проверка на пути запроса и разбор на пути чтения обязаны судить ОДИН И ТОТ ЖЕ вход
// одинаково. Прежде они расходились: проверка требовала лишь наличия разделителя, а
// репозиторий разбирал поля, — и вход, прошедший проверку, падал ниже с ДРУГИМ текстом.
func TestGuardAndReadPathAgreeOnTheSameInput(t *testing.T) {
	inputs := []string{
		"",
		pagetoken.EncodeKeysetTime(pagetoken.DefaultOrder, time.Unix(0, 1).UTC(), "epd1"),
		"not-a-real-token!!",
		base64.RawURLEncoding.EncodeToString([]byte("1700000000000000000:epd1")),
		base64.RawURLEncoding.EncodeToString([]byte("nocolonhere")),
	}
	for _, in := range inputs {
		guardErr := ValidateListPagination(Pagination{PageToken: in, PageSize: 10})
		_, readErr := pagetoken.Decode(in)
		if (guardErr == nil) != (readErr == nil) {
			t.Errorf("вход %q: проверка сказала %v, путь чтения — %v", in, guardErr, readErr)
		}
	}
}
