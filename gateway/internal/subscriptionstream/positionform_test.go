// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionstream_test

import (
	"strings"
	"testing"

	subscriptionv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"
	"github.com/PRO-Robotech/kacho/pkg/pagetoken"
)

// TestFramerIsCorrectAtAnyPositionForm — форму позиции выбирает её ВЛАДЕЛЕЦ, и
// кадрировщик обязан быть верен при любой.
//
// Проба заведена под утверждение, которое кадрировщик делает о себе
// комментарием: она даёт ему держателя, а не оставляет обещанием. Обе стороны
// стоят вместе намеренно — одностороннее «мусор отвергнут» зеленело бы на
// кадрировщике, отвергающем ВСЁ, а одностороннее «значение проехало» — на
// кадрировщике, не проверяющем НИЧЕГО.
func TestFramerIsCorrectAtAnyPositionForm(t *testing.T) {
	t.Run("законная позиция проезжает дословно", func(t *testing.T) {
		// Позиция, выпущенная владельцем формы: `pkg/pagetoken` кодирует её
		// стандартным base64 С ДОПОЛНЕНИЕМ, поэтому её алфавит несёт `+`, `/`
		// и `=`. Кадрировщик, написанный по вере в крокфордов алфавит, отверг
		// бы законное значение — и потеря была бы не в мусоре, а в
		// возобновлении: без `id:` клиенту не с чего продолжить.
		real := pagetoken.EncodeSubscriptionPosition(pagetoken.SubscriptionPosition{Settled: 42})
		if real == "" {
			t.Fatal("кодек позиции выпустил пустую строку — пустое значение означает «позиция не задана»")
		}
		// Синтетическое значение добирает те знаки стандартного алфавита,
		// которых короткое тело не производит: предмет пробы — что кадрировщик
		// НЕ судит алфавит, а не то, какие байты выпало сегодня.
		for _, position := range []string{real, "aB+/cd=="} {
			owner := &ownerStub{script: []*subscriptionv1.SubscriptionMessage{
				openedMessage(position, true),
			}}
			rec := serve(t, newHandler(t, owner), request("owner=probe"))

			got := frames(t, rec.Body.String())
			if len(got) != 1 {
				t.Fatalf("кадров %d, ожидался 1 при позиции %q: %q", len(got), position, rec.Body.String())
			}
			if got[0].id != position {
				t.Errorf("позиция доехала как %q, владелец выпустил %q: позиция непрозрачна "+
					"и едет полем `id:` ДОСЛОВНО", got[0].id, position)
			}
		}
	})

	t.Run("перевод строки отвергнут", func(t *testing.T) {
		// Перевод строки внутри позиции закрыл бы кадр досрочно и склеил бы два
		// события в одно — клиент прочитал бы одно событие вместо двух и не
		// узнал бы об этом ничем. Кадрировщик закрывает поток, не отдав кадра.
		owner := &ownerStub{script: []*subscriptionv1.SubscriptionMessage{
			openedMessage("pos\n0", true),
		}}
		rec := serve(t, newHandler(t, owner), request("owner=probe"))

		if got := frames(t, rec.Body.String()); len(got) != 0 {
			t.Fatalf("кадров %d, ожидался 0: позиция с переводом строки обязана закрыть поток, "+
				"а не разрезать кадр — тело %q", len(got), rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "id: pos") {
			t.Error("позиция с переводом строки всё же попала в кадр — граница кадра порвана")
		}
	})
}
