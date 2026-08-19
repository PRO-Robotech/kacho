// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package namepage

import "testing"

// Два курсора одного пакета — по ИМЕНИ и по ПОЗИЦИИ — кодировали голую строку одной
// и той же кодировкой, поэтому тег с цифровым именем давал токен, БАЙТ-В-БАЙТ равный
// offset-курсору. Оба окна принимали чужой токен без ошибки и отдавали не ту страницу:
// молча неверный ответ, а не отказ. Цифровые имена тегов — обычный случай (`5`, `12`).
//
// Проба утверждает НАБЛЮДАЕМОЕ: токен, выданный одним производителем, не может быть
// разобран другим. Она не утверждает конкретную схему — та может смениться.
func TestCursorOfOneKindIsNotReadableAsTheOther(t *testing.T) {
	for _, name := range []string{"5", "12", "0", "42"} {
		nameTok := Encode(name)

		if got, err := decodeOffset(nameTok); err == nil {
			t.Errorf("курсор имени %q (токен %q) разобран как позиция %d — должен быть отвергнут",
				name, nameTok, got)
		}
	}

	for _, off := range []int{0, 5, 12, 42} {
		offTok := EncodeOffset(off)

		got, err := Decode(offTok)
		if err == nil {
			t.Errorf("курсор позиции %d (токен %q) разобран как имя %q — должен быть отвергнут",
				off, offTok, got)
		}
	}
}

// Разбор курсора имени принимал ЛЮБОЙ валидный base64 и возвращал его содержимое как
// имя — то есть мусорный токен давал 200 со съехавшей страницей вместо 400. Это
// единственный путь в дереве, где мусор на публичном списке не давал InvalidArgument.
func TestGarbageNameCursorIsRejectedRatherThanRead(t *testing.T) {
	// Валидный base64, но не курсор этого производителя.
	for _, tok := range []string{"YWJjZGVm", "ISFAIw==", "AAAA"} {
		if got, err := Decode(tok); err == nil {
			t.Errorf("мусорный токен %q принят как имя %q — должен быть отвергнут", tok, got)
		}
	}
}

// Положительный контроль: законный токен своего производителя проходит и возвращает
// то, что в него положили. Без него отрицания выше зеленели бы на кодеке, который
// отвергает вообще всё.
func TestOwnCursorRoundTrips(t *testing.T) {
	for _, name := range []string{"5", "latest", "v1.2.3", ""} {
		if got, err := Decode(Encode(name)); err != nil || got != name {
			t.Errorf("имя %q не пережило круг: got=%q err=%v", name, got, err)
		}
	}
	for _, off := range []int{0, 1, 99, 100000} {
		if got, err := DecodeOffset(EncodeOffset(off)); err != nil || got != off {
			t.Errorf("позиция %d не пережила круг: got=%d err=%v", off, got, err)
		}
	}
}
