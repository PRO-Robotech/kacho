// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package domain

import "fmt"

// PerformanceTier — ярус класса диска. ЗАКРЫТЫЙ словарь, а не свободная строка.
//
// Почему закрытый. Публичная поверхность не несёт инфра-лексики, и гейт проекции
// это держит — но он перечисляет ИМЕНА полей, а не значения. Свободный ярус
// проходит сквозь него со значением вида "nvme-fast" или "pool-b-replicated", то
// есть открывает по значениям ровно тот канал, который закрыт по именам.
type PerformanceTier string

// Значения яруса. Собственные названия Kachō, не заимствованные продуктовые линейки.
const (
	TierCapacity PerformanceTier = "CAPACITY"
	TierBalanced PerformanceTier = "BALANCED"
	TierFast     PerformanceTier = "FAST"
	TierSingle   PerformanceTier = "SINGLE"
	TierIOMax    PerformanceTier = "IO_MAX"
)

// Valid сообщает, принадлежит ли ярус словарю. Пустой ярус законен: класс вправе
// его не объявлять, и «не объявлен» отличается от «объявлен неизвестным».
func (t PerformanceTier) Valid() bool {
	switch t {
	case "", TierCapacity, TierBalanced, TierFast, TierSingle, TierIOMax:
		return true
	default:
		return false
	}
}

// DiskTypeLifecycle — состояние обращения класса. Отвечает на единственный вопрос:
// принимает ли класс НОВЫЕ тома. Существующие живут при любом состоянии — их права
// не отзываются правкой справочника.
type DiskTypeLifecycle string

// Значения жизненного цикла класса.
const (
	// LifecycleActive — класс принимает новые тома.
	LifecycleActive DiskTypeLifecycle = "ACTIVE"
	// LifecycleDeprecated — существующие тома живут, новые не создаются.
	LifecycleDeprecated DiskTypeLifecycle = "DEPRECATED"
	// LifecycleRetired — класс выведен: новые не создаются, удаление разрешено
	// при нуле ссылающихся томов.
	LifecycleRetired DiskTypeLifecycle = "RETIRED"
)

// Valid сообщает, принадлежит ли состояние словарю. Пустое — НЕ валидно: умолчание
// проставляет вызывающий край, домен же не должен угадывать намерение админа.
func (l DiskTypeLifecycle) Valid() bool {
	switch l {
	case LifecycleActive, LifecycleDeprecated, LifecycleRetired:
		return true
	default:
		return false
	}
}

// Capability — имя способности класса. Строковый тип, а не булево поле, чтобы отказ
// мог НАЗВАТЬ отсутствующую способность вызывающему.
type Capability string

// Способности класса.
const (
	CapSnapshots         Capability = "snapshots"
	CapCloneFromSnapshot Capability = "cloneFromSnapshot"
	CapCloneFromImage    Capability = "cloneFromImage"
	CapOnlineGrow        Capability = "onlineGrow"
	CapMultiAttach       Capability = "multiAttach"
	CapEncryptionAtRest  Capability = "encryptionAtRest"
)

// Capabilities — что класс умеет. Публичны намеренно: способность, которую нельзя
// прочитать заранее, превращает полноту API в непредсказуемость — арендатор узнаёт
// об отсутствии снимков отказом, уже написав код.
type Capabilities struct {
	Snapshots         bool
	CloneFromSnapshot bool
	CloneFromImage    bool
	OnlineGrow        bool
	MultiAttach       bool
	EncryptionAtRest  bool
}

// Has отвечает про НАЗВАННУЮ способность. Неизвестное имя — «нет», а не «разрешено
// по умолчанию»: корзины «прочее» здесь нет by construction.
func (c Capabilities) Has(name Capability) bool {
	switch name {
	case CapSnapshots:
		return c.Snapshots
	case CapCloneFromSnapshot:
		return c.CloneFromSnapshot
	case CapCloneFromImage:
		return c.CloneFromImage
	case CapOnlineGrow:
		return c.OnlineGrow
	case CapMultiAttach:
		return c.MultiAttach
	case CapEncryptionAtRest:
		return c.EncryptionAtRest
	default:
		return false
	}
}

// SizeLimits — границы размера тома, объявленные классом. Нули означают «класс не
// сужает»: отсутствие границы отличается от границы, равной нулю.
type SizeLimits struct {
	MinSizeBytes  int64
	MaxSizeBytes  int64
	SizeStepBytes int64
}

// Validate проверяет внутреннюю согласованность границ.
func (l SizeLimits) Validate() error {
	if l.MinSizeBytes < 0 || l.MaxSizeBytes < 0 || l.SizeStepBytes < 0 {
		return fmt.Errorf("disk_type size limits must not be negative")
	}
	if l.MinSizeBytes > 0 && l.MaxSizeBytes > 0 && l.MinSizeBytes > l.MaxSizeBytes {
		return fmt.Errorf("disk_type min_size_bytes %d exceeds max_size_bytes %d",
			l.MinSizeBytes, l.MaxSizeBytes)
	}
	if l.SizeStepBytes > 0 {
		if l.MinSizeBytes%l.SizeStepBytes != 0 {
			return fmt.Errorf("disk_type min_size_bytes %d is not a multiple of size_step_bytes %d",
				l.MinSizeBytes, l.SizeStepBytes)
		}
		if l.MaxSizeBytes%l.SizeStepBytes != 0 {
			return fmt.Errorf("disk_type max_size_bytes %d is not a multiple of size_step_bytes %d",
				l.MaxSizeBytes, l.SizeStepBytes)
		}
	}
	return nil
}

// DiskType — класс диска: admin-справочник, несущий ПОЛИТИКУ (ярус, состояние
// обращения, границы размера, способности), а не только косметику. Публичный
// read-only Get/List, admin CRUD через Internal* на :9091. id — admin-assigned slug.
// zone_ids — cross-service ссылки на geo.Zone по id, без FK.
//
// Числа производительности и координата бэкенда на классе НЕ живут: они —
// свойство ревизии привязки (:9091), потому что меняются вместе с бэкендом, а класс
// обязан переживать его смену.
type DiskType struct {
	ID              string
	Name            string
	Description     string
	ZoneIDs         []string
	PerformanceTier PerformanceTier
	Lifecycle       DiskTypeLifecycle
	Limits          SizeLimits
	Capabilities    Capabilities
}

// Validate проверяет domain-инварианты DiskType перед сохранением.
func (d DiskType) Validate() error {
	if d.ID == "" {
		return fmt.Errorf("disk_type id is required")
	}
	if err := validateName("disk_type name", d.Name); err != nil {
		return err
	}
	if !d.PerformanceTier.Valid() {
		return fmt.Errorf("disk_type performance_tier %q is not a known tier", d.PerformanceTier)
	}
	if !d.Lifecycle.Valid() {
		return fmt.Errorf("disk_type lifecycle %q is not a known lifecycle", d.Lifecycle)
	}
	return d.Limits.Validate()
}

// AcceptsNewVolumes сообщает, принимает ли класс новые тома. Единственный вопрос,
// ради которого заведено состояние обращения.
func (d DiskType) AcceptsNewVolumes() bool { return d.Lifecycle == LifecycleActive }

// ValidateVolumeSize судит размер тома по границам, объявленным ЭТИМ классом.
// Предикат живёт здесь, а не в use-case тома: границы объявляет класс, значит и
// решать обязан он — иначе появится второе место, где они интерпретируются.
//
// Тексты — часть контракта. Ноль и отрицательный размер отвергаются независимо от
// границ: это инвариант тома, а не сужение класса.
func (d DiskType) ValidateVolumeSize(sizeBytes int64) error {
	if sizeBytes <= 0 {
		return fmt.Errorf("Illegal argument size_bytes") //nolint:staticcheck // контрактный текст Kachō (§1.7)
	}
	if d.Limits.MinSizeBytes > 0 && sizeBytes < d.Limits.MinSizeBytes {
		return fmt.Errorf("Volume size %d is less than DiskType %s minimum %d", //nolint:staticcheck // контрактный текст
			sizeBytes, d.ID, d.Limits.MinSizeBytes)
	}
	if d.Limits.MaxSizeBytes > 0 && sizeBytes > d.Limits.MaxSizeBytes {
		return fmt.Errorf("Volume size %d exceeds DiskType %s maximum %d", //nolint:staticcheck // контрактный текст
			sizeBytes, d.ID, d.Limits.MaxSizeBytes)
	}
	if d.Limits.SizeStepBytes > 0 && sizeBytes%d.Limits.SizeStepBytes != 0 {
		return fmt.Errorf("Volume size %d is not a multiple of DiskType %s step %d", //nolint:staticcheck // контрактный текст
			sizeBytes, d.ID, d.Limits.SizeStepBytes)
	}
	return nil
}

// RequireCapability отказывает, если класс не объявил названную способность.
// Отказ НАЗЫВАЕТ и класс, и способность: иначе арендатор видит «нельзя» и не знает,
// что именно поменять — а способности для того и публичны, чтобы это было читаемо.
func (d DiskType) RequireCapability(name Capability) error {
	if d.Capabilities.Has(name) {
		return nil
	}
	return fmt.Errorf("DiskType %s does not support %s", d.ID, name) //nolint:staticcheck // контрактный текст
}
