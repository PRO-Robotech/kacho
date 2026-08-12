// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package domain

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// PrefixDiskTypeBinding — id-префикс ревизии привязки, hyphen-канон
// (`dtb-<crockford-base32>`, ids.NewHyphenID). Как и у бэкенда, принадлежность к
// семье держит внешняя связь в БД, а не разбор строки.
const PrefixDiskTypeBinding = "dtb"

// projectPlaceholder — единственная подстановка, которую допускает шаблон единицы
// изоляции арендатора.
//
// ДВА МЕСТА ОБ ОДНОМ ПРЕДМЕТЕ, и это названо вслух: подстановку исполняет помощник
// порта (blockbackend.NamespaceOfProject), а здесь решается, какой шаблон законен.
// Домен не вправе импортировать порт (правило зависимостей), поэтому расхождение
// удерживается пробой сцепки в disk_type_binding_test.go: она падает, если домен
// начнёт принимать шаблон, который у порта не подставляется.
const projectPlaceholder = "{projectId}"

// BindingStatus — состояние ревизии привязки. Ровно два значения, и переход между
// ними ровно один: действующая ревизия вытесняется новой.
type BindingStatus string

// Состояния ревизии.
const (
	// BindingStatusActive — ревизия действует: новые ресурсы создаются под ней.
	BindingStatusActive BindingStatus = "ACTIVE"
	// BindingStatusSuperseded — ревизия вытеснена новой. Продолжает описывать то,
	// что обещали созданным под ней ресурсам, и живёт, пока на неё ссылаются.
	BindingStatusSuperseded BindingStatus = "SUPERSEDED"
)

// Valid сообщает, принадлежит ли состояние словарю. Пустое — НЕ валидно.
func (s BindingStatus) Valid() bool {
	switch s {
	case BindingStatusActive, BindingStatusSuperseded:
		return true
	default:
		return false
	}
}

// BindingLocator — где у бэкенда живут объекты этой привязки. Инфра-координата:
// наружу не выходит ни одним полем, живёт только на внутреннем листенере.
type BindingLocator struct {
	// Pool — пространство размещения у бэкенда.
	Pool string
	// NamespaceTemplate — шаблон единицы изоляции арендатора. Пусто означает
	// «изоляция по идентификатору проекта как есть» (см. Validate).
	NamespaceTemplate string
}

// Validate проверяет координату.
//
// Несущее здесь — шаблон изоляции. Непустой шаблон ОБЯЗАН нести подстановку
// проекта: шаблон без неё выглядит настроенным, а на деле сводит всех арендаторов
// класса в одно пространство имён у бэкенда. Тогда шумный сосед ничем не ограничен,
// а любая ошибка в правах на стороне хранилища сразу межарендная — то есть
// настройка, введённая ради изоляции, её же и снимает, и заметить это неоткуда.
func (l BindingLocator) Validate() error {
	if l.Pool == "" {
		return fmt.Errorf("disk_type_binding pool is required")
	}
	if l.NamespaceTemplate != "" && !strings.Contains(l.NamespaceTemplate, projectPlaceholder) {
		return fmt.Errorf(
			"disk_type_binding namespace_template %q must contain %s: without it every project shares one namespace",
			l.NamespaceTemplate, projectPlaceholder)
	}
	return nil
}

// BindingCapabilities — что бэкенд действительно умеет под этой привязкой.
// Способности живут на ревизии, а не на классе: это свойство бэкенда, а не
// продуктовой политики. Класс их ВЫВОДИТ — см. [PublicCapabilities].
//
// Состав повторяет blockbackend.Capabilities, и это не дубль по недосмотру: домен
// не импортирует порт (правило зависимостей). Предметы разные — порт объявляет, что
// умеет РЕАЛИЗАЦИЯ сейчас, ревизия фиксирует, что было обещано в момент привязки, и
// первое меняется выпуском адаптера, второе не меняется никогда.
type BindingCapabilities struct {
	Snapshots         bool
	CloneFromSnapshot bool
	CloneFromImage    bool
	// CloneKeepsParent — клон остаётся зависимым от источника. Наружу НЕ
	// проецируется: это не обещание арендатору, а свойство реализации, следствие
	// которого он видит отказом на удалении источника с живыми детьми.
	CloneKeepsParent bool
	OnlineGrow       bool
	MultiAttach      bool
	EncryptionAtRest bool
	// TrashTTLSeconds — срок корзины бэкенда. Ноль означает «корзины нет»:
	// ёмкость освобождается сразу.
	TrashTTLSeconds int64
}

// Validate проверяет числовую часть способностей.
func (c BindingCapabilities) Validate() error {
	if c.TrashTTLSeconds < 0 {
		return fmt.Errorf("disk_type_binding trash_ttl_seconds must not be negative")
	}
	return nil
}

// Public проецирует способности ревизии на публичный набор класса. ОДНО место, где
// принято решение «что арендатор видит»: булевы без имён пулов, без сроков корзины
// и без зависимости клона от родителя.
//
// Направление сужения у отброшенных флагов ОБРАТНОЕ, и это вторая причина, по
// которой они здесь не появляются: способность консервативна при пересечении
// («умеют все зоны»), а зависимость клона от родителя консервативна при
// объединении («хоть одна зона так делает»). Одно правило пересечения не может
// обслуживать оба направления сразу.
func (c BindingCapabilities) Public() Capabilities {
	return Capabilities{
		Snapshots:         c.Snapshots,
		CloneFromSnapshot: c.CloneFromSnapshot,
		CloneFromImage:    c.CloneFromImage,
		OnlineGrow:        c.OnlineGrow,
		MultiAttach:       c.MultiAttach,
		EncryptionAtRest:  c.EncryptionAtRest,
	}
}

// BindingQoS — числа производительности, объявленные ревизией. Формула, а не
// константа: потолок считается от размера тома.
//
// Ноль означает «не объявлено», а не «ограничение равно нулю» — то же различение,
// что у границ размера класса (disk_type.go).
type BindingQoS struct {
	BaselineIOPS int64
	IOPSPerGiB   int64
	MaxIOPS      int64

	BaselineThroughputMiBps int64
	// ThroughputPerGiBMiBps — единственное дробное число набора, и намеренно:
	// наклон полосы задаётся долями мебибайта в секунду на гибибайт, округление до
	// целого обнулило бы его на классах ёмкости.
	ThroughputPerGiBMiBps float64
	MaxThroughputMiBps    int64
}

// Validate проверяет внутреннюю согласованность чисел.
//
// Форму (объект, а не массив) держит ограничение БД в 0015; числовая вменяемость
// на уровне БД не выражается — колонка хранит документ, — поэтому она здесь.
func (q BindingQoS) Validate() error {
	if q.BaselineIOPS < 0 || q.IOPSPerGiB < 0 || q.MaxIOPS < 0 ||
		q.BaselineThroughputMiBps < 0 || q.MaxThroughputMiBps < 0 {
		return fmt.Errorf("disk_type_binding qos values must not be negative")
	}
	// Не-число и бесконечность отвергаются отдельно: они не отрицательны, но и
	// сравнению не поддаются, поэтому проверка выше их пропускает молча.
	if math.IsNaN(q.ThroughputPerGiBMiBps) || math.IsInf(q.ThroughputPerGiBMiBps, 0) {
		return fmt.Errorf("disk_type_binding qos throughput_per_gib_mibps must be a finite number")
	}
	if q.ThroughputPerGiBMiBps < 0 {
		return fmt.Errorf("disk_type_binding qos values must not be negative")
	}
	if q.MaxIOPS > 0 && q.BaselineIOPS > q.MaxIOPS {
		return fmt.Errorf("disk_type_binding qos baseline_iops %d exceeds max_iops %d",
			q.BaselineIOPS, q.MaxIOPS)
	}
	if q.MaxThroughputMiBps > 0 && q.BaselineThroughputMiBps > q.MaxThroughputMiBps {
		return fmt.Errorf("disk_type_binding qos baseline_throughput_mibps %d exceeds max_throughput_mibps %d",
			q.BaselineThroughputMiBps, q.MaxThroughputMiBps)
	}
	return nil
}

// DiskTypeBinding — НЕИЗМЕНЯЕМАЯ ревизия привязки класса диска к бэкенду в одной
// зоне. Ресурс только внутреннего листенера (:9091).
//
// # Неизменяемость — это механизм, а не дисциплина
//
// Ресурс ссылается на ту ревизию, под которой создан. Пока ревизия не меняется,
// правка класса физически не может задним числом изменить свойства уже созданного
// тома: джойн к ревизии не может уехать, потому что цель неизменяема. Прежняя форма
// — том хранит только идентификатор класса и читает справочник В МОМЕНТ
// ИСПОЛЬЗОВАНИЯ — давала обратное: смена координаты у класса молча переадресовывала
// данные существующих томов, и заметить это было неоткуда.
//
// Отсюда форма ТИПА: у него нет ни одного метода, способного изменить поля.
// Единственный переход — вытеснение новой ревизией — производит НОВОЕ значение
// ([DiskTypeBinding.Supersede]), а не правит это. Отсутствие обновления и есть
// механизм, поэтому оно закреплено гейтом (TestDiskTypeBinding_HasNoMutatingMethods),
// а не комментарием: комментарий переживёт первый же метод с указательным
// приёмником.
//
// В БД та же дисциплина держится с двух сторон: строки ревизий пишутся вставкой,
// единственное существующее обновление — перевод статуса, а ограничительная внешняя
// связь (0015, 0017) не даёт удалить ревизию, на которую кто-то ссылается. Поля
// updated_at у ревизии нет и не должно быть.
type DiskTypeBinding struct {
	ID         string
	DiskTypeID string
	// ZoneID — зона, в которой действует эта ревизия. Класс предлагается в
	// нескольких зонах, и в каждой у него своя привязка: зоны могут обслуживаться
	// разными бэкендами.
	ZoneID    string
	BackendID string
	// Revision — номер ревизии в пределах пары (класс, зона), начиная с единицы.
	// Уникальность номера и «ровно одна действующая на пару» держат частичные
	// уникальные индексы в 0015, а не проверка в коде: две конкурентные
	// регистрации иначе обе прошли бы чтение и обе записали.
	Revision     int32
	Locator      BindingLocator
	Capabilities BindingCapabilities
	QoS          BindingQoS
	Status       BindingStatus
	CreatedAt    time.Time
}

// Validate проверяет domain-инварианты ревизии перед вставкой.
func (b DiskTypeBinding) Validate() error {
	if b.ID == "" {
		return fmt.Errorf("disk_type_binding id is required")
	}
	if b.DiskTypeID == "" {
		return fmt.Errorf("disk_type_binding disk_type_id is required")
	}
	if b.ZoneID == "" {
		return fmt.Errorf("disk_type_binding zone_id is required")
	}
	if b.BackendID == "" {
		return fmt.Errorf("disk_type_binding backend_id is required")
	}
	if b.Revision <= 0 {
		return fmt.Errorf("disk_type_binding revision must be positive, got %d", b.Revision)
	}
	if !b.Status.Valid() {
		return fmt.Errorf("disk_type_binding status %q is not a known status", b.Status)
	}
	if err := b.Locator.Validate(); err != nil {
		return err
	}
	if err := b.Capabilities.Validate(); err != nil {
		return err
	}
	return b.QoS.Validate()
}

// IsActive сообщает, действует ли ревизия.
func (b DiskTypeBinding) IsActive() bool { return b.Status == BindingStatusActive }

// Supersede возвращает ревизию, вытесненную новой. Единственный переход состояния,
// который у ревизии есть.
//
// Приёмник — ЗНАЧЕНИЕ, и это не стиль: метод обязан произвести новое значение, а не
// изменить существующее, иначе неизменяемость держалась бы только тем, что никто не
// вызвал метод не вовремя. Повтор безопасен — вытеснение уже вытесненной ревизии
// случается при повторной доставке и отказом не является.
func (b DiskTypeBinding) Supersede() DiskTypeBinding {
	b.Status = BindingStatusSuperseded
	return b
}

// PublicCapabilities выводит публичные способности КЛАССА пересечением по его
// ДЕЙСТВУЮЩИМ ревизиям.
//
// Консервативно намеренно: класс предлагается в нескольких зонах, зоны могут
// обслуживаться разными бэкендами, и объявить способность, которой нет в одной из
// зон, значит пообещать ОТКАЗ — арендатор напишет код по каталогу и получит его в
// проде. Ноль действующих ревизий → класс не объявляет ничего: пока привязки нет,
// обещать нечего, и это законное состояние, а не поломка.
//
// Вытесненные ревизии в выводе не участвуют: они описывают то, что обещали прежним
// ресурсам, а не то, что предлагается сейчас. Ревизии чужих классов отсеиваются
// здесь же — выборка, отданная страницей на несколько классов, иначе молча смешала
// бы обещания разных классов.
func PublicCapabilities(diskTypeID string, bindings []DiskTypeBinding) Capabilities {
	var out Capabilities
	first := true
	for _, b := range bindings {
		if b.DiskTypeID != diskTypeID || !b.IsActive() {
			continue
		}
		caps := b.Capabilities.Public()
		if first {
			out, first = caps, false
			continue
		}
		out = intersectCapabilities(out, caps)
	}
	if first {
		return Capabilities{}
	}
	return out
}

// intersectCapabilities — пересечение двух наборов: способность остаётся, только
// если её несут ОБА.
func intersectCapabilities(a, b Capabilities) Capabilities {
	return Capabilities{
		Snapshots:         a.Snapshots && b.Snapshots,
		CloneFromSnapshot: a.CloneFromSnapshot && b.CloneFromSnapshot,
		CloneFromImage:    a.CloneFromImage && b.CloneFromImage,
		OnlineGrow:        a.OnlineGrow && b.OnlineGrow,
		MultiAttach:       a.MultiAttach && b.MultiAttach,
		EncryptionAtRest:  a.EncryptionAtRest && b.EncryptionAtRest,
	}
}
