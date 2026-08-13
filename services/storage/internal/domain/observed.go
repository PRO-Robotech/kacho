// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package domain

import (
	"fmt"
	"time"
)

// ObservedState — что бэкенд ответил о существовании объекта ресурса.
//
// # Почему это ОТДЕЛЬНАЯ величина, а не часть состояния ресурса
//
// Колонка состояния отвечала на два разных вопроса сразу: «что мы намерены иметь»
// и «что там на самом деле». Пока плоскости данных нет, разницы между ними нет
// тоже — каждая вставка объявляла ресурс готовым немедленно. С появлением бэкенда
// разница возникает by construction: появляется «строка закоммичена, объекта нет» и
// обратное «объект есть, строки нет». Одна величина делает такое расхождение
// НЕНАХОДИМЫМ: она отвечает про намерение и выдаёт ответ за факт.
type ObservedState string

// Наблюдённые состояния. Значения совпадают с тем, что принимает колонка.
const (
	// ObservedAbsent — объекта у бэкенда нет.
	ObservedAbsent ObservedState = "ABSENT"
	// ObservedReady — объект есть и пригоден.
	ObservedReady ObservedState = "READY"
	// ObservedError — объект есть, но бэкенд считает его неисправным.
	ObservedError ObservedState = "ERROR"
	// ObservedUnknown — установить не удалось.
	//
	// НЕ то же, что ObservedAbsent, и путать их нельзя: недоступность бэкенда не
	// является утверждением об отсутствии объекта. Слив их в одно значение,
	// сверщик объявлял бы живой том пропавшим на первой же сетевой неполадке — и
	// это была бы не диагностика, а порча состояния.
	ObservedUnknown ObservedState = "UNKNOWN"
)

// Valid сообщает, принадлежит ли значение словарю.
func (s ObservedState) Valid() bool {
	switch s {
	case ObservedAbsent, ObservedReady, ObservedError, ObservedUnknown:
		return true
	default:
		return false
	}
}

// StatusReason — почему ресурс оказался в своём состоянии. Закрытый словарь НАШИХ
// полос, а не таксономия бэкенда.
//
// Значения, называющие физику (заполненность пула, состояние реплики, узел), в
// этом словаре невозможны by construction: арендатору адресовано то, что он может
// изменить своим действием либо обязан переждать, а не устройство плоскости
// данных, которой он не управляет. Свободная строка на этом месте была бы самым
// прямым каналом утечки — в неё естественно попадает текст бэкенда целиком, а
// проба двухпроекционности перечисляет ИМЕНА полей и такого значения не увидит.
type StatusReason string

// Причины. Пустая означает «причины нет»: ресурс в штатном состоянии.
const (
	ReasonNone                     StatusReason = ""
	ReasonBackendUnavailable       StatusReason = "BACKEND_UNAVAILABLE"
	ReasonBackendRejected          StatusReason = "BACKEND_REJECTED"
	ReasonBackendCapacityExhausted StatusReason = "BACKEND_CAPACITY_EXHAUSTED"
	ReasonSourceNotReady           StatusReason = "SOURCE_NOT_READY"
	ReasonPreconditionFailed       StatusReason = "PRECONDITION_FAILED"
	ReasonInternalError            StatusReason = "INTERNAL_ERROR"
)

// Valid сообщает, принадлежит ли причина словарю.
func (r StatusReason) Valid() bool {
	switch r {
	case ReasonNone, ReasonBackendUnavailable, ReasonBackendRejected,
		ReasonBackendCapacityExhausted, ReasonSourceNotReady,
		ReasonPreconditionFailed, ReasonInternalError:
		return true
	default:
		return false
	}
}

// Placement — где ресурс материализован у бэкенда. Инфра-проекция: НИ ОДНО из этих
// полей не выходит на публичную поверхность, они живут только на :9091.
type Placement struct {
	// BindingID — ревизия привязки, под которой ресурс создан. Ссылка на
	// НЕИЗМЕНЯЕМУЮ строку: именно поэтому правка класса не может задним числом
	// изменить свойства уже созданного ресурса.
	BindingID string
	// DesiredBindingID — ревизия, в которую ресурс переезжает. Непусто только на
	// время смены класса.
	DesiredBindingID string
	// BackendObject — имя объекта у бэкенда, выведенное из неизменяемого
	// идентификатора ресурса с префиксом установки.
	BackendObject string
	// BackendNamespace — единица изоляции арендатора у бэкенда.
	BackendNamespace string
}

// Observation — что сверщик увидел у бэкенда в последний раз.
type Observation struct {
	State ObservedState
	// At — когда наблюдение сделано. Нулевое значение означает «не наблюдали ни
	// разу», и это отличается от «наблюдали и не нашли».
	At time.Time
	// SizeBytes — размер объекта у бэкенда. Отличается от желаемого, пока ресайз
	// не доведён.
	SizeBytes int64
	// UsedBytes и HasUsedBytes раздельны намеренно: отсутствие сведений о
	// потреблении отличается от потребления, равного нулю. Ноль — утверждение о
	// пустом томе, и выдавать за него «бэкенд не сказал» значит врать.
	UsedBytes    int64
	HasUsedBytes bool
}

// Validate проверяет согласованность наблюдения.
func (o Observation) Validate() error {
	if !o.State.Valid() {
		return fmt.Errorf("observed state %q is not a known state", o.State)
	}
	if o.SizeBytes < 0 || o.UsedBytes < 0 {
		return fmt.Errorf("observed sizes must not be negative")
	}
	if !o.HasUsedBytes && o.UsedBytes != 0 {
		return fmt.Errorf("used bytes reported without being marked as present")
	}
	return nil
}
