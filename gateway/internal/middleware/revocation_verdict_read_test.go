// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware

// revocation_verdict_read_test.go — ВЕРДИКТ, КОТОРЫЙ ВЫНЕСЛИ, ОБЯЗАН БЫТЬ
// ПРОЧИТАН.
//
// ПРЕДМЕТ (#2728). Исходов полосы отзыва было пять, а каждая из двух развилок
// края читала два и не имела `default`. Третий исход — молчание источника —
// проваливался сквозь развилку к обработчику МОЛЧА: читателей у него по дереву
// было НОЛЬ при трёх вхождениях самого идентификатора (объявление, комментарий
// и возврат). Вердикт выносился, логировался и ни на что не влиял.
//
// ЗАМЕР ЭТОГО СВОЙСТВА ТЕКСТОМ НЕ ДЕЛАЕТСЯ. Исход читается законно и одиночным
// `case`, и `case` на несколько значений, и таблицей — поиск по одной из форм
// недобирает МОЛЧА, а именно так и выглядит зелёное у слепого предиката.
// Поэтому свойство держится ЭТОЙ пробой, а не выражением над текстом: она
// спрашивает словарь напрямую и про каждый объявленный исход, и про
// необъявленный.
//
// Поэтому здесь проверяется не «наш новый исход отказывает», а СВОЙСТВО
// словаря: каждый объявленный исход у него назван, а исход, которого он не
// знает, ОТКАЗЫВАЕТ. Вторая половина несущая — она и есть fail-closed: следующий
// исход, заведённый без читателя, упрётся в отказ, а не пройдёт к обработчику.

import "testing"

// TestEveryRevocationVerdictIsRead — перепись и обе стороны разом.
func TestEveryRevocationVerdictIsRead(t *testing.T) {
	declared := map[revocationVerdict]struct {
		name string
		want revocationDisposition
	}{
		revocationNotAsked:            {"revocationNotAsked", revocationProceed},
		revocationLive:                {"revocationLive", revocationProceed},
		revocationRevoked:             {"revocationRevoked", revocationDenyCredential},
		revocationUnanswerable:        {"revocationUnanswerable", revocationDenyService},
		revocationUnanswered:          {"revocationUnanswered", revocationProceed},
		revocationOwnSourceUnanswered: {"revocationOwnSourceUnanswered", revocationDenyService},
	}

	// ПЕРЕПИСЬ отдельно от находок: «исходов 0» и «находок 0» — разные
	// утверждения, и зелёное на пустом обходе не читается.
	if len(declared) == 0 {
		t.Fatal("перепись исходов пуста — проверять нечего, и это не зелёное")
	}
	t.Logf("исходов осмотрено: %d", len(declared))

	for v, want := range declared {
		if got := v.disposition(); got != want.want {
			t.Errorf("%s: действие %d, ждали %d", want.name, got, want.want)
		}
	}

	// ПРЕДПОСЫЛКА переписи: объявленных исходов ровно столько, сколько здесь
	// перечислено. Если кто-то заведёт ещё один, значение сразу за последним
	// перестанет быть неизвестным — и эта проверка скажет об этом словами,
	// вместо того чтобы молча сузить предмет пробы ниже.
	beyond := revocationOwnSourceUnanswered + 1
	if _, known := declared[beyond]; known {
		t.Fatalf("перепись отстала от объявления: %d уже объявлен, но в ней его нет", beyond)
	}

	// НЕСУЩАЯ ПОЛОВИНА: исход, которого словарь не знает, ОТКАЗЫВАЕТ. Без неё
	// «каждый объявленный назван» зеленело бы ровно до того дня, когда заведут
	// шестой, — то есть в точности до повторения #2728.
	if got := beyond.disposition(); got != revocationDenyService {
		t.Fatalf("исход, которого словарь не знает (%d), даёт действие %d вместо отказа — "+
			"следующий заведённый без читателя пройдёт к обработчику молча, ровно как #2728",
			beyond, got)
	}
}
