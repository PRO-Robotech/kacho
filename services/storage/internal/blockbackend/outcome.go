// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package blockbackend

import (
	"errors"
	"fmt"
)

// Outcome — исход обращения к бэкенду блочного хранения. ЗАКРЫТЫЙ набор.
//
// # Почему без корзины «прочее»
//
// Нулевое значение — [OutcomeUnclassified], и это СОСТОЯНИЕ («классифицировать не
// удалось»), а не молчаливый выбор политики повтора. Корзина «прочее» в такой
// таблице всегда получает семантику того, кто её первым прочитал: один вызывающий
// решит, что это временно, и заведёт вечный повтор, другой — что терминально, и
// потеряет доставку. Поэтому неизвестный исход терминален и наружу выходит
// фиксированной внутренней ошибкой, а его появление — повод дописать таблицу, а не
// расширить трактовку.
//
// # Почему отказ в правах не временный
//
// Повтор идентичного запроса не может пройти. Трактовать его как временный значит
// вечно держать голову очереди на строке, которая никогда не применится, — этот
// класс в продукте уже случался и стоил очереди, где за всю её жизнь не доехало ни
// одной строки.
type Outcome int

// Исходы обращения к бэкенду.
const (
	// OutcomeUnclassified — исход не опознан. Терминален. Наружу — фиксированная
	// внутренняя ошибка, без текста бэкенда.
	OutcomeUnclassified Outcome = iota

	// OutcomeUnavailable — бэкенд не ответил в срок либо недоступен. ПОВТОРЯЕМО.
	OutcomeUnavailable

	// OutcomeRejected — бэкенд отказал по существу запроса. Не повторяемо.
	OutcomeRejected

	// OutcomeCapacityExhausted — у бэкенда нет места. Не повторяемо без
	// вмешательства оператора либо высвобождения ёмкости.
	OutcomeCapacityExhausted

	// OutcomeNotFound — объекта нет. Для удаления это УСПЕХ (идемпотентность),
	// для чтения — факт отсутствия.
	OutcomeNotFound

	// OutcomeConflict — объект существует, но его параметры расходятся с
	// запрошенными. Не повторяемо: повтор даст тот же конфликт.
	OutcomeConflict

	// OutcomeDenied — бэкенд отказал в правах. НЕ временно.
	OutcomeDenied

	// OutcomeMisconfigured — ответ доказывает, что мы обращаемся не туда: не тот
	// эндпоинт, неожиданный формат, неизвестный протокол. Это НАСТРОЙКА, а не сбой,
	// и потому громко: мягкий проход по такому ответу превращает постоянную
	// неправильную конфигурацию в штатный режим, при котором контроль присутствует
	// и не отказывает ни разу за всю свою жизнь.
	OutcomeMisconfigured
)

// String возвращает имя исхода. Используется в журналах и счётчиках; наружу
// арендатору не выходит.
func (o Outcome) String() string {
	switch o {
	case OutcomeUnclassified:
		return "unclassified"
	case OutcomeUnavailable:
		return "unavailable"
	case OutcomeRejected:
		return "rejected"
	case OutcomeCapacityExhausted:
		return "capacity_exhausted"
	case OutcomeNotFound:
		return "not_found"
	case OutcomeConflict:
		return "conflict"
	case OutcomeDenied:
		return "denied"
	case OutcomeMisconfigured:
		return "misconfigured"
	default:
		return "unclassified"
	}
}

// Retryable сообщает, имеет ли смысл повторить обращение. Отвечает ровно один
// исход: недоступность. Всё остальное на повторе даст тот же ответ.
func (o Outcome) Retryable() bool { return o == OutcomeUnavailable }

// Known сообщает, принадлежит ли значение объявленному набору. Нужен гейтам и
// пробам: значение вне набора — находка, а не «что-то новое».
func (o Outcome) Known() bool {
	return o >= OutcomeUnclassified && o <= OutcomeMisconfigured
}

// Error — отказ бэкенда, несущий свою полосу. Текст бэкенда СОХРАНЯЕТСЯ внутри для
// журнала оператора, но наружу арендатору не выходит: его выдаёт край по полосе, из
// фиксированной таблицы.
type Error struct {
	Outcome Outcome
	Op      string // какой глагол порта вызывался
	Object  string // имя объекта у бэкенда, если применимо
	Err     error  // исходная ошибка — для журнала, не для арендатора
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("blockbackend %s %s: %s: %v", e.Op, e.Object, e.Outcome, e.Err)
	}
	return fmt.Sprintf("blockbackend %s %s: %s", e.Op, e.Object, e.Outcome)
}

func (e *Error) Unwrap() error { return e.Err }

// Errorf собирает отказ названной полосы.
func Errorf(outcome Outcome, op, object string, err error) *Error {
	return &Error{Outcome: outcome, Op: op, Object: object, Err: err}
}

// OutcomeOf извлекает полосу из ошибки. Ошибка, не несущая полосы, —
// [OutcomeUnclassified]: отсутствие классификации не превращается в допущение о ней.
func OutcomeOf(err error) Outcome {
	if err == nil {
		return OutcomeUnclassified
	}
	var be *Error
	if errors.As(err, &be) {
		return be.Outcome
	}
	return OutcomeUnclassified
}
