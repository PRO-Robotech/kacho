// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package blockbackend — порт плоскости данных блочного хранения.
//
// # Что здесь есть и чего здесь нет
//
// Здесь объявлено, что control plane умеет ПОПРОСИТЬ у хранилища. Реализация живёт
// в internal/clients рядом с клиентами соседних сервисов — тем же порядком, каким
// уже сделаны клиенты географии и прав: порт в слое use-case, адаптер в clients,
// провязка в композиционном корне. Ни реестра адаптеров, ни протокола объявления
// способностей, ни конфигурируемой загрузки здесь нет и не должно быть, пока
// адаптер один: механизм, заведённый под второй бэкенд до его появления, — это
// абстракция, за которую платят сейчас, а получают потом.
//
// # Чего в порту нет НАМЕРЕННО
//
// Отображения тома на узле (map/unmap). Это работа узлового агента плоскости
// данных: у него своя граница доверия и свои учётные данные к хранилищу. Втащив её
// сюда, control plane отрастил бы ногу в плоскость данных.
//
// # Идемпотентность — свойство ИМЕНИ, а не протокола
//
// Имя объекта у бэкенда выводится детерминированно из неизменяемого идентификатора
// ресурса (см. naming.go). Поэтому повтор любого глагола попадает в тот же объект, и
// повтор безопасен by construction: исполнитель операций переисполняет функцию не
// по ошибке, а by construction — при переполнении очереди, перекате пода и
// исчерпании бюджета терминальной записи.
//
// # Способности объявляются, а не предполагаются
//
// [Backend.Capabilities] — константы реализации. Операция, чьей способности бэкенд
// не объявил, до него не доходит: её отвергает use-case, называя класс и способность.
package blockbackend

import "context"

// Locator — где у бэкенда живёт группа объектов. Инфра-координата: наружу не
// выходит ни одним полем, живёт в ревизии привязки класса и в internal-проекции.
type Locator struct {
	// Pool — пространство размещения у бэкенда.
	Pool string
	// Namespace — единица изоляции арендатора внутри пространства. Выводится из
	// идентификатора проекта: без неё все арендаторы класса делят одно
	// пространство имён, и любая ошибка в правах на стороне бэкенда становится
	// межарендной.
	Namespace string
}

// ObjectRef — адрес одного объекта у бэкенда.
type ObjectRef struct {
	Locator
	// Name — детерминированное имя, выведенное из идентификатора ресурса.
	// Адаптер имя ВЫВОДИТ и никогда не принимает из запроса арендатора.
	Name string
}

// ObservedState — что бэкенд отвечает о существовании объекта.
type ObservedState int

// Наблюдённые состояния.
const (
	// ObservedAbsent — объекта нет.
	ObservedAbsent ObservedState = iota
	// ObservedReady — объект есть и пригоден.
	ObservedReady
	// ObservedError — объект есть, но бэкенд считает его неисправным.
	ObservedError
	// ObservedUnknown — установить не удалось. НЕ то же, что «нет»: недоступность
	// бэкенда не является утверждением об отсутствии объекта.
	ObservedUnknown
)

// String возвращает имя наблюдённого состояния (для колонки и журнала).
func (s ObservedState) String() string {
	switch s {
	case ObservedAbsent:
		return "ABSENT"
	case ObservedReady:
		return "READY"
	case ObservedError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// Observed — что бэкенд рассказал об объекте.
type Observed struct {
	State     ObservedState
	SizeBytes int64

	// UsedBytes и HasUsedBytes раздельны намеренно: отсутствие сведений о
	// потреблении отличается от потребления, равного нулю. Ноль — утверждение о
	// пустом томе, и выдавать за него «бэкенд не сказал» значит врать.
	UsedBytes    int64
	HasUsedBytes bool

	// Parent — объект-родитель, если этот создан клоном и родитель ещё держится.
	// Пусто, когда родителя нет либо связь уже разорвана.
	Parent string
}

// VolumeSpec — что именно создаётся.
type VolumeSpec struct {
	Ref       ObjectRef
	SizeBytes int64
	// QoS применяется к объекту после создания, если бэкенд это умеет. Пустая
	// карта означает «класс не объявил чисел», а не «нулевые ограничения».
	QoS map[string]int64
}

// CloneSpec — засев объекта из источника.
type CloneSpec struct {
	Source ObjectRef
	Target ObjectRef
	// SizeBytes — размер цели. Не меньше размера источника, проверено вызывающим.
	SizeBytes int64
	// Detached требует, чтобы цель НЕ зависела от источника по завершении.
	// Вызывающий ставит его, когда бэкенд объявил зависимость клона от родителя,
	// а семантика контракта требует независимости.
	Detached bool
	QoS      map[string]int64
}

// Capabilities — что реализация умеет. Константы, а не результат опроса: пока
// адаптер один, опрос был бы протоколом без второй стороны.
type Capabilities struct {
	Snapshots         bool
	CloneFromSnapshot bool
	CloneFromImage    bool
	CloneKeepsParent  bool
	OnlineGrow        bool
	MultiAttach       bool
	EncryptionAtRest  bool
	TrashTTLSeconds   int64
}

// Backend — порт плоскости данных. Десять глаголов, ни одного лишнего.
//
// Каждый глагол ИДЕМПОТЕНТЕН по имени объекта. Формулировка «идемпотентен» здесь
// точная и проверяемая контрактной суитой: повтор с теми же аргументами обязан
// вернуть успех, а повтор с РАСХОДЯЩИМИСЯ аргументами — [OutcomeConflict], а не
// молча принять новое значение.
type Backend interface {
	// Kind возвращает вид бэкенда (для журналов и счётчиков).
	Kind() string

	// Capabilities — что эта реализация умеет.
	Capabilities() Capabilities

	// CreateVolume создаёт объект тома. Существует с тем же размером → успех;
	// существует с другим → конфликт.
	CreateVolume(ctx context.Context, spec VolumeSpec) error

	// DeleteVolume снимает объект. Отсутствует → успех.
	DeleteVolume(ctx context.Context, ref ObjectRef) error

	// ResizeVolume увеличивает объект. Уже не меньше запрошенного → успех.
	// Уменьшение вызывающим не запрашивается и реализацией не поддерживается.
	ResizeVolume(ctx context.Context, ref ObjectRef, sizeBytes int64) error

	// CreateSnapshot снимает снимок с тома. Снимок существует → успех.
	CreateSnapshot(ctx context.Context, volume, snapshot ObjectRef) error

	// DeleteSnapshot снимает снимок. Отсутствует → успех. Если у снимка есть
	// зависимые клоны и бэкенд это отслеживает — конфликт, а не тихое удаление.
	DeleteSnapshot(ctx context.Context, ref ObjectRef) error

	// CloneVolume засевает цель из источника (снимка либо образа).
	CloneVolume(ctx context.Context, spec CloneSpec) error

	// CopySnapshot переносит снимок в другой локатор. Цель существует → успех.
	CopySnapshot(ctx context.Context, source ObjectRef, target ObjectRef) error

	// MigrateVolume переносит объект в другой локатор, сохраняя данные.
	MigrateVolume(ctx context.Context, ref ObjectRef, target Locator) error

	// Observe рассказывает об объекте. Недоступность бэкенда — ОШИБКА, а не
	// [ObservedAbsent]: молчание не является утверждением об отсутствии.
	Observe(ctx context.Context, ref ObjectRef) (Observed, error)

	// ListObjects перечисляет имена объектов локатора для сверки. Курсор
	// непрозрачен; пустой next означает конец.
	ListObjects(ctx context.Context, loc Locator, cursor string, limit int) (names []string, next string, err error)
}
