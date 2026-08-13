// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// TestApplyGuestAccessKey_MaterialIsNotOverwrittenByEcho — материал ключа,
// написанный арендатором, НЕ перезаписывается эхом края.
//
// Край вправе вернуть канонизованную форму (иной пробел, снятый комментарий в
// хвосте). Записав её в состояние, мы получили бы вечное расхождение с
// конфигурацией — а расхождение по этому полю означает ПЕРЕСОЗДАНИЕ ключа, то
// есть смену идентификатора и отвал всех машин, на него ссылающихся. Каждый
// план. Навсегда.
func TestApplyGuestAccessKey_MaterialIsNotOverwrittenByEcho(t *testing.T) {
	ctx := context.Background()

	t.Run("заданный материал переживает канонизацию края", func(t *testing.T) {
		m := guestAccessKeyModel{PublicKey: types.StringValue("ssh-ed25519 AAAA  мой-ключ")}
		applyGuestAccessKey(ctx, &m, &guestAccessKeyJSON{
			ID: "gak-1", Name: "к", PublicKey: "ssh-ed25519 AAAA", Fingerprint: "SHA256:x",
		})
		if got := m.PublicKey.ValueString(); got != "ssh-ed25519 AAAA  мой-ключ" {
			t.Errorf("материал стал %q — эхо края перезаписало написанное арендатором, "+
				"и следующий план пересоздаст ключ", got)
		}
	})

	// Положительный контроль в паре: при ВВОЗЕ материала в состоянии нет вовсе, и
	// оставить его пустым значило бы предъявить план, снимающий ключ, которого
	// арендатор не трогал. Без этой пробы первая зеленела бы на реализации, не
	// пишущей поле никогда.
	t.Run("при ввозе материал берётся у края", func(t *testing.T) {
		m := guestAccessKeyModel{PublicKey: types.StringNull()}
		applyGuestAccessKey(ctx, &m, &guestAccessKeyJSON{
			ID: "gak-1", Name: "к", PublicKey: "ssh-ed25519 AAAA", Fingerprint: "SHA256:x",
		})
		if got := m.PublicKey.ValueString(); got != "ssh-ed25519 AAAA" {
			t.Errorf("материал = %q, ожидался присланный краем: при ввозе брать его больше неоткуда", got)
		}
	})

	t.Run("отпечаток всегда от края — он и есть свидетельство", func(t *testing.T) {
		m := guestAccessKeyModel{Fingerprint: types.StringValue("SHA256:чужой")}
		applyGuestAccessKey(ctx, &m, &guestAccessKeyJSON{ID: "gak-1", Fingerprint: "SHA256:верный"})
		if got := m.Fingerprint.ValueString(); got != "SHA256:верный" {
			t.Errorf("отпечаток = %q — он вычисляется краем и не может браться из состояния", got)
		}
	})
}

// TestGuestAccessKeySchema_PrivateHalfHasNoAttribute — закрытой половины ключа в
// схеме нет НИ ОДНИМ атрибутом.
//
// Проверяется дерево атрибутов, а не наличие строки в файле: комментарий о том,
// что закрытый ключ здесь не хранится, остался бы верным по букве и ложным по
// существу, стоило бы кому-то завести поле «на удобство». Состояние Terraform
// лежит открытым текстом — атрибут с закрытым ключом означал бы, что доступ к
// файлу состояния равен доступу в машину.
func TestGuestAccessKeySchema_PrivateHalfHasNoAttribute(t *testing.T) {
	s := schemaOf(t, NewGuestAccessKeyResource())

	forbidden := []string{"private_key", "privateKey", "secret_key", "key_material", "passphrase"}
	for name := range s.Attributes {
		for _, bad := range forbidden {
			if name == bad {
				t.Errorf("в схеме есть атрибут %q — закрытая половина ключа не хранится ни в контракте, "+
					"ни в состоянии Terraform", name)
			}
		}
	}

	// Положительный контроль: публичная половина и отпечаток на месте. Без него
	// перечень запрещённых имён зеленел бы на пустой схеме.
	for _, want := range []string{"public_key", "fingerprint"} {
		if _, ok := s.Attributes[want]; !ok {
			t.Errorf("в схеме нет атрибута %q — проверка запрещённых имён осматривает не ту схему", want)
		}
	}
	t.Logf("осмотрено атрибутов: %d", len(s.Attributes))
}
