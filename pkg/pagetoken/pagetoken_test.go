// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pagetoken

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// Круг: что положили, то и достали. Положительный контроль для всех отрицаний ниже —
// без него кодек, отвергающий вообще всё, прошёл бы каждую отрицательную пробу.
func TestCursorSurvivesTheRoundTrip(t *testing.T) {
	cases := []Cursor{
		{Order: "", Keys: []string{"1700000000000000000", "net-abc"}},
		{Order: "name asc", Keys: []string{"my-network", "net-abc"}},
		{Order: "created_at desc", Keys: []string{"1700000000000000000", "net-abc"}},
		{Order: "id asc", Keys: []string{"ru-central1-a"}},
		// Значение, содержащее КАЖДЫЙ разделитель, применявшийся прежними формами:
		// двоеточие, вертикальную черту и нулевой байт. Длина перед полем снимает
		// вопрос экранирования — формы с одним разделительным байтом здесь ломались.
		{Order: "name asc", Keys: []string{"имя:с|всеми\x00байтами", "id-1"}},
		{Order: "name asc", Keys: []string{"", "id-1"}},
	}
	for _, want := range cases {
		tok := Encode(want)
		got, err := Decode(tok)
		if err != nil {
			t.Fatalf("Decode(%q) вернул ошибку: %v (курсор %+v)", tok, err, want)
		}
		if got.Order != want.Order || len(got.Keys) != len(want.Keys) {
			t.Fatalf("курсор не пережил круг: got=%+v want=%+v", got, want)
		}
		for i := range want.Keys {
			if got.Keys[i] != want.Keys[i] {
				t.Fatalf("ключ %d не пережил круг: got=%q want=%q", i, got.Keys[i], want.Keys[i])
			}
		}
	}
}

// Отсутствие курсора представимо ОТДЕЛЬНО от курсора: пустая строка — первая страница,
// а не позиция ноль и не ошибка.
func TestAbsentCursorIsNotAnError(t *testing.T) {
	c, err := Decode("")
	if err != nil {
		t.Fatalf("пустой токен должен означать первую страницу, а не ошибку: %v", err)
	}
	if c.Order != "" || len(c.Keys) != 0 {
		t.Fatalf("пустой токен дал непустой курсор: %+v", c)
	}
	if tok := Encode(Cursor{Order: "name asc"}); tok != "" {
		t.Fatalf("курсор без ключей обязан кодироваться в пустую строку, получено %q", tok)
	}
}

// Мусор ОТВЕРГАЕТСЯ, а не истолковывается. Это то свойство, которого не было у форм без
// метки: там любой валидный base64 был валидным курсором, и страница молча съезжала.
func TestGarbageIsRejectedNeverInterpreted(t *testing.T) {
	garbage := []struct{ tok, why string }{
		{"!!!не base64!!!", "не та кодировка"},
		{"YWJjZGVm", "валидный base64 без метки формата"},
		{"AAAA", "валидный base64 без метки формата"},
		// Метка есть, тела нет.
		{enc("kct1"), "метка без полей"},
		// Метка и порядок есть, ключей нет: позиция не названа.
		{enc("kct10:"), "нет ни одного ключа"},
		// Длина больше остатка.
		{enc("kct19:abc"), "длина поля больше остатка"},
		// Длина не число.
		{enc("kct1x:abc"), "длина поля не число"},
		// Хвост после последнего поля: приписанный мусор не может читаться как законный.
		{enc("kct10:3:abcХВОСТ"), "непотреблённый хвост"},
		// Прежние формы того же контракта — их токены обязаны отвергаться, а не
		// разбираться наугад.
		{enc("1700000000000000000:net-abc"), "прежняя форма с двоеточием"},
		{enc("2026-08-18T00:00:00Z|usr-1"), "прежняя форма с чертой"},
		{enc(`{"created_at":"2026-08-18T00:00:00Z","id":"reg-1"}`), "прежняя форма JSON"},
	}
	for _, g := range garbage {
		got, err := Decode(g.tok)
		if err == nil {
			t.Errorf("токен %q (%s) принят как курсор %+v — должен быть отвергнут", g.tok, g.why, got)
			continue
		}
		if !errors.Is(err, ErrInvalid) {
			t.Errorf("токен %q (%s) отвергнут не тем sentinel: %v", g.tok, g.why, err)
		}
	}
}

// Курсор, выданный в одном порядке, не может быть предъявлен вместе с другим: он
// описывал бы позицию в порядке, которого больше нет.
func TestCursorIssuedForAnotherOrderIsRejected(t *testing.T) {
	tok := Encode(Cursor{Order: "name asc", Keys: []string{"a", "id-1"}})

	if _, err := DecodeInOrder(tok, "name asc"); err != nil {
		t.Fatalf("положительный контроль: курсор своего порядка обязан проходить, получено %v", err)
	}
	for _, other := range []string{"name desc", "created_at asc", ""} {
		if _, err := DecodeInOrder(tok, other); err == nil {
			t.Errorf("курсор порядка %q принят как курсор порядка %q", "name asc", other)
		} else if !errors.Is(err, ErrInvalid) {
			t.Errorf("отказ по порядку пришёл не тем sentinel: %v", err)
		}
	}
}

// Токен опаковый: в нём не должно быть читаемого глазом значения ключа. Проба стережёт
// не секретность (её тут нет), а обещание контракта — вызывающий не вправе разбирать
// токен и строить на его частях свою логику.
func TestTokenIsOpaqueToTheEye(t *testing.T) {
	tok := Encode(Cursor{Order: "name asc", Keys: []string{"совершенно-узнаваемое-имя", "id-1"}})
	if strings.Contains(tok, "совершенно-узнаваемое-имя") {
		t.Fatalf("значение ключа читается в токене как есть: %q", tok)
	}
}

func enc(s string) string {
	return encodeRawForTest(s)
}

// Самая частая форма ключа платформы переживает круг, включая границы разрядности.
func TestKeysetTimeSurvivesTheRoundTrip(t *testing.T) {
	moments := []time.Time{
		time.Unix(0, 1700000000123456789).UTC(),
		time.Unix(0, 0).UTC(),
		// Момент с ненулевым смещением: круг обязан вернуть ТОТ ЖЕ миг, а не ту же
		// настенную запись. Прежние формы возили время текстом и на этом теряли.
		time.Date(2026, 8, 18, 12, 0, 0, 42, time.FixedZone("MSK", 3*3600)),
	}
	for _, want := range moments {
		tok := EncodeKeysetTime(DefaultOrder, want, "net-abc")
		gotAt, gotID, err := DecodeKeysetTime(tok, DefaultOrder)
		if err != nil {
			t.Fatalf("DecodeKeysetTime(%q): %v", tok, err)
		}
		if !gotAt.Equal(want) {
			t.Errorf("момент не пережил круг: got=%v want=%v", gotAt, want)
		}
		if gotID != "net-abc" {
			t.Errorf("идентификатор не пережил круг: %q", gotID)
		}
	}
}

// Курсор формы (created_at, id) обязан ОТВЕРГАТЬ курсор другой формы того же кодека,
// а не читать из него первое попавшееся поле.
func TestKeysetTimeRejectsACursorOfAnotherShape(t *testing.T) {
	oneKey := Encode(Cursor{Order: DefaultOrder, Keys: []string{"ru-central1-a"}})
	if _, _, err := DecodeKeysetTime(oneKey, DefaultOrder); err == nil {
		t.Error("курсор из одного ключа принят как (created_at, id)")
	}
	notAMoment := Encode(Cursor{Order: DefaultOrder, Keys: []string{"вчера", "id-1"}})
	if _, _, err := DecodeKeysetTime(notAMoment, DefaultOrder); err == nil {
		t.Error("курсор с нечисловой отметкой принят как момент")
	}
	noRow := Encode(Cursor{Order: DefaultOrder, Keys: []string{"1700000000000000000", ""}})
	if _, _, err := DecodeKeysetTime(noRow, DefaultOrder); err == nil {
		t.Error("курсор без идентификатора строки принят")
	}
}
