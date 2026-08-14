// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package instance

import (
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
)

const goodKeyID = "gak-abcdefghjkmnpqrst"

// TestValidateGuestAccessKeyIDs_FormatAndCardinality — форма ссылок проверяется
// СВОИМ отказом, а не превращается в отказ о принадлежности проекту.
//
// Непригодная строка, доехавшая до связи, вернулась бы «ключ не этого проекта» —
// то есть утверждением о принадлежности строки, которая ключом быть не может.
func TestValidateGuestAccessKeyIDs_FormatAndCardinality(t *testing.T) {
	t.Run("пустой набор законен", func(t *testing.T) {
		if err := validateGuestAccessKeyIDs(nil); err != nil {
			t.Fatalf("машина без ключей — обычная машина, получено %v", err)
		}
	})

	// Положительный контроль: пригодные ссылки проходят. Без него отрицания ниже
	// зеленели бы на проверке, отвергающей всё подряд.
	t.Run("пригодные ссылки проходят", func(t *testing.T) {
		if err := validateGuestAccessKeyIDs([]string{goodKeyID}); err != nil {
			t.Fatalf("пригодная ссылка обязана пройти, получено %v", err)
		}
	})

	t.Run("непригодная ссылка отвергается разбором", func(t *testing.T) {
		err := validateGuestAccessKeyIDs([]string{"не идентификатор"})
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("код = %v, ожидался InvalidArgument", status.Code(err))
		}
	})

	t.Run("набор сверх предела отвергается по имени поля", func(t *testing.T) {
		ids := make([]string, domain.MaxGuestAccessKeysPerInstance+1)
		for i := range ids {
			ids[i] = goodKeyID
		}
		err := validateGuestAccessKeyIDs(ids)
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("код = %v, ожидался InvalidArgument", status.Code(err))
		}
		if !strings.Contains(status.Convert(err).Message(), "guestAccessKeyIds") {
			t.Errorf("сообщение %q не называет поле", status.Convert(err).Message())
		}
	})

	// Граница принимается: предел объявлен как «не более», и ровно предел законен.
	t.Run("ровно предел проходит", func(t *testing.T) {
		ids := make([]string, domain.MaxGuestAccessKeysPerInstance)
		for i := range ids {
			ids[i] = goodKeyID
		}
		if err := validateGuestAccessKeyIDs(ids); err != nil {
			t.Fatalf("ровно предел обязан проходить, получено %v", err)
		}
	})
}

// TestInstanceUpdate_GuestKeysNotInFullPatchSet — пустая маска НЕ трогает набор
// ключей.
//
// Иначе правка описания молча снимала бы с машины весь доступ, и это выглядело
// бы как успешное переименование. Свойство утверждается о СОСТАВЕ набора полей
// полной правки, потому что именно он решает, что применяется при пустой маске.
func TestInstanceUpdate_GuestKeysNotInFullPatchSet(t *testing.T) {
	for _, f := range instanceFullPatchFields {
		if f == "guest_access_key_ids" {
			t.Fatal("набор ключей входа не имеет права применяться при ПУСТОЙ маске: " +
				"правка описания молча сняла бы весь доступ к машине")
		}
	}

	// Положительный контроль: названная маской правка набор ЗНАЕТ — иначе
	// утверждение выше выполнялось бы и на поле, которого нет вовсе.
	if _, ok := instanceUpdateKnown["guest_access_key_ids"]; !ok {
		t.Fatal("набор ключей входа обязан быть известен маске: иначе его правка " +
			"отвечала бы «неизвестное поле», а не применялась")
	}

	// И он не входит в набор, требующий остановленной машины: ключи меняются на
	// живой машине — иначе смена доступа требовала бы простоя.
	if _, ok := instanceStoppedGatedMask["guest_access_key_ids"]; ok {
		t.Fatal("смена ключей не имеет права требовать остановки машины")
	}
}
