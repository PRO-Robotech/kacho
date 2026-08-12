// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package reconciler — сверщик намерения с наблюдаемым состоянием плоскости данных.
//
// # Зачем он и почему он ЕДИНСТВЕННЫЙ производитель готовности
//
// Операция фиксирует НАМЕРЕНИЕ и завершается: её предмет — «строка закоммичена», и
// только это. Провижининг у бэкенда идёт вне её функции — иначе длинное выделение
// упиралось бы в потолок исполнителя операций, а разрешитель осиротевших операций
// через своё окно признал бы строку завершённой, прочитав НАШУ базу. Клиент получил
// бы «готово» при отсутствующем объекте.
//
// Отсюда разделение: ресурс рождается создаваемым, готовым его объявляет сверщик,
// увидев объект. Публичный статус перестаёт быть утверждением о нашем намерении и
// становится утверждением о факте.
//
// # Две оси, и вторая не чинится сама
//
// Расхождение бывает в обе стороны. «Строка есть, объекта нет» — наша забота:
// создать либо признать ошибкой. «Объект есть, строки нет» — утечка ёмкости в
// хранилище, за которое отвечает другая команда, и сверщик её НЕ УДАЛЯЕТ: снос
// чужих данных по собственному выводу необратим. Он считает и называет, снимает
// оператор.
package reconciler

import (
	"github.com/PRO-Robotech/kacho/services/storage/internal/blockbackend"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
)

// Action — что сверщик намерен сделать с одной строкой.
type Action int

// Действия сверщика.
const (
	// ActionNone — расхождения нет, трогать нечего.
	ActionNone Action = iota
	// ActionProvision — создать объект у бэкенда.
	ActionProvision
	// ActionResize — довести размер объекта до желаемого.
	ActionResize
	// ActionRemove — снять объект у бэкенда.
	ActionRemove
	// ActionForget — объект снят, строку можно удалять.
	ActionForget
	// ActionConfirm — объект на месте, объявить ресурс готовым.
	ActionConfirm
	// ActionReportVanished — объект пропал у бэкенда под живой строкой.
	ActionReportVanished
	// ActionWait — состояние установить не удалось. Ничего не предпринимаем:
	// молчание бэкенда не является утверждением ни об отсутствии объекта, ни о
	// его наличии.
	ActionWait
)

// String — имя действия для журнала и счётчиков.
func (a Action) String() string {
	switch a {
	case ActionProvision:
		return "provision"
	case ActionResize:
		return "resize"
	case ActionRemove:
		return "remove"
	case ActionForget:
		return "forget"
	case ActionConfirm:
		return "confirm"
	case ActionReportVanished:
		return "vanished"
	case ActionWait:
		return "wait"
	default:
		return "none"
	}
}

// Item — одна строка, взятая сверщиком на разбор. Поля читаются из нашей базы;
// Observed приходит от бэкенда.
type Item struct {
	// Kind — какого ресурса строка. Нужен для журнала и счётчиков: расхождение по
	// томам и по снимкам — разные операционные истории.
	Kind string
	// ID — идентификатор ресурса.
	ID string
	// Desired — намерение, записанное в нашей строке.
	Desired domain.VolumeStatus
	// DesiredSizeBytes — желаемый размер объекта.
	DesiredSizeBytes int64
	// Ref — адрес объекта у бэкенда.
	Ref blockbackend.ObjectRef
	// Observed — что бэкенд ответил об этом объекте в текущем проходе.
	Observed blockbackend.Observed
}

// Decide отвечает, что делать с одной строкой. Функция ЧИСТАЯ намеренно: решение
// сверщика — самая опасная часть (оно приводит к созданию и удалению данных), и
// проверять его через живую базу значило бы проверять его редко и дорого.
//
// Порядок ветвей несущий и объяснён по месту.
func Decide(it Item) Action {
	// Неустановленное состояние разбирается ПЕРВЫМ и всегда даёт «подождать».
	// Иначе первая же сетевая неполадка была бы прочитана как исчезновение
	// объекта — и сверщик, «починив» расхождение, создал бы второй объект либо
	// объявил живой ресурс ошибочным.
	if it.Observed.State == blockbackend.ObservedUnknown {
		return ActionWait
	}

	switch it.Desired {
	case domain.VolumeStatusDeleting:
		// Снятие: пока объект виден — снимаем, как только его нет — забываем
		// строку. Порядок именно такой: строка живёт ДОЛЬШЕ объекта, потому что
		// крах между снятием строки и снятием объекта оставил бы ёмкость, о
		// которой в системе не осталось записи.
		if it.Observed.State == blockbackend.ObservedAbsent {
			return ActionForget
		}
		return ActionRemove

	case domain.VolumeStatusCreating:
		if it.Observed.State == blockbackend.ObservedAbsent {
			return ActionProvision
		}
		if it.Observed.State == blockbackend.ObservedError {
			// Объект есть, но бэкенд считает его неисправным. Пересоздавать
			// нельзя: под именем уже лежат чьи-то данные, и «починка» стёрла бы
			// их. Это предмет оператора.
			return ActionReportVanished
		}
		// Объект на месте — намерение исполнено, объявляем готовность.
		return ActionConfirm

	case domain.VolumeStatusAvailable, domain.VolumeStatusInUse:
		switch it.Observed.State {
		case blockbackend.ObservedAbsent:
			// Объект пропал под живой строкой. Пересоздание НЕ является починкой:
			// данные не вернутся, а пустой объект того же имени выглядел бы
			// здоровым ресурсом. Ресурс объявляется ошибочным, разбирается оператор.
			return ActionReportVanished
		case blockbackend.ObservedError:
			return ActionReportVanished
		}
		if it.DesiredSizeBytes > 0 && it.Observed.SizeBytes > 0 &&
			it.Observed.SizeBytes < it.DesiredSizeBytes {
			// Размер не доехал: увеличение принято нашей строкой, но у бэкенда не
			// применено. Уменьшение сюда не попадает by construction — контракт
			// его не принимает.
			return ActionResize
		}
		return ActionNone

	case domain.VolumeStatusError:
		// Ошибочный ресурс, чей объект оказался на месте и здоров, возвращается в
		// строй: ошибка была наблюдением, а не приговором, и держать её после
		// исчезновения причины значило бы врать о ресурсе.
		if it.Observed.State == blockbackend.ObservedReady {
			return ActionConfirm
		}
		return ActionNone
	}
	return ActionNone
}

// ReasonFor переводит исход обращения к бэкенду в причину состояния ресурса.
//
// Словарь причин — НАШИ полосы, не таксономия бэкенда: значения, называющие физику,
// в нём невозможны by construction, а текст бэкенда наружу не выходит вовсе.
func ReasonFor(outcome blockbackend.Outcome) domain.StatusReason {
	switch outcome {
	case blockbackend.OutcomeUnavailable:
		return domain.ReasonBackendUnavailable
	case blockbackend.OutcomeCapacityExhausted:
		return domain.ReasonBackendCapacityExhausted
	case blockbackend.OutcomeRejected, blockbackend.OutcomeConflict,
		blockbackend.OutcomeDenied, blockbackend.OutcomeMisconfigured:
		return domain.ReasonBackendRejected
	case blockbackend.OutcomeNotFound:
		return domain.ReasonPreconditionFailed
	default:
		// Неклассифицированный исход не превращается в догадку о причине.
		return domain.ReasonInternalError
	}
}

// Terminal сообщает, обязан ли сверщик остановиться на этой строке до вмешательства.
//
// Повторяем ровно недоступность. Всё остальное на повторе даст тот же ответ, а
// бесконечный повтор терминального отказа — это очередь, в которой голова никогда не
// проходит: класс, который в продукте уже случался.
func Terminal(outcome blockbackend.Outcome) bool {
	return !outcome.Retryable()
}
