// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package domain

import (
	"errors"
	"time"

	corevalidate "github.com/PRO-Robotech/kacho/pkg/validate"
)

// ID-префиксы ресурсов домена Storage (3-char, ids.NewID). Тип ресурса читается
// по префиксу. op-root storage = "sop" (opsproxy маршрутизирует Operation.Get по
// первым 3 символам op-id).
const (
	PrefixVolume    = "vol"
	PrefixSnapshot  = "snp"
	PrefixImage     = "img"
	PrefixOperation = "sop"
)

// ErrIllegalName / errIllegalSize — контрактные тексты валидации (часть контракта
// Kachō, §1.7; assert'ятся behaviour-level в newman/integration). serviceerr
// срезает sentinel-префикс, клиент видит именно эту строку.
var (
	// ErrIllegalName — контрактный текст отказа по имени, ОДИН на весь сервис.
	//
	// Экспортирован затем, чтобы правка, которая судит имя через общий канон
	// (validate.NameOnUpdate), отвечала арендатору ТЕМ ЖЕ текстом, что и
	// создание. Иначе у одного поля стало бы два тона отказа, и кейсы чёрного
	// ящика, пинящие этот текст на пути правки, покраснели бы на верной правке.
	//nolint:staticcheck // ST1005: контрактный текст Kachō (§1.7, "Illegal argument <field>") — капитализация нормативна
	ErrIllegalName = errors.New("Illegal argument name")
	//nolint:staticcheck // ST1005: контрактный текст Kachō (§1.7) — капитализация нормативна
	errIllegalSize = errors.New("Illegal argument size_bytes")
	// errSourceConflict — том нельзя засеять одновременно из snapshot и image (F9).
	errSourceConflict = errors.New("a volume is seeded from either a snapshot or an image, not both")
)

// ValidateName — форма имени ресурса storage, доступная за пределами домена.
//
// Нужна тем путям, что судят имя НЕ через агрегатный Validate() — например
// правке, которая проверяет одно поле. Без неё вызывающий выбирал бы newtype
// чужого ресурса («позову VolumeName для снимка, там всё равно одна функция»),
// и связь между полем и его правилом читалась бы как совпадение.
//
// Семантика — NameOnCreate: пустая строка ЗАКОННА (см. validateDisplayName).
// Решение «пустое здесь означает снять имя» принадлежит вызывающему, а не форме.
func ValidateName(name string) error { return validateDisplayName(name) }

// VolumeName — self-validating newtype display-name тома (skill evgeniy: инвариант
// формы живёт на типе).
//
// Пустая строка — законный ВХОД создания и означает «назови сам»: до вставки её
// заменяет имя, производное от идентификатора (validate.NameOrDefault в use-case).
// Ресурса с пустым именем поэтому не существует.
//
// Здесь стояло другое объяснение: «два безымянных тома в проекте легальны, потому
// что частичный UNIQUE не действует на пустую строку». Оно пережило свой предмет
// дважды — подстановка сделала пустое имя недостижимым для вставки, а индекс,
// на который оно ссылалось, стал ПОЛНЫМ. Два безымянных тома сосуществуют
// по-прежнему, но по обратной причине: у каждого своё непустое имя, выведенное из
// глобально уникального идентификатора, — то есть уникальность соблюдена, а не
// обойдена.
type VolumeName string

// Validate проверяет форму имени: пусто → ok (см. выше); иначе единственная форма
// имени ресурса в дереве. Любое нарушение → фиксированный ErrIllegalName.
func (n VolumeName) Validate() error {
	return validateDisplayName(string(n))
}

// validateDisplayName — общий self-validating инвариант tenant display-name
// (Volume/Snapshot/Image). Форму НЕ объявляет: её единственное объявление в дереве —
// corevalidate.NameForm, и вторая копия здесь уже стоила расхождения (прежняя
// требовала букву первым символом, канон принимает и цифру).
//
// Зовётся именно NameOnCreate, а не Name: агрегатный Validate() исполняется РАНЬШЕ,
// чем чеканится идентификатор, а имя по умолчанию выводится из идентификатора. Отказ
// на пустой строке здесь отменил бы создание без имени ДО того, как подстановке будет
// из чего выводить. Пустое имя до записи не доживает — его заменяет NameOrDefault в
// use-case, в точке, где идентификатор уже есть.
//
// Возвращается контрактный ErrIllegalName, а не ошибка pkg/validate: текст «Illegal
// argument name» — часть контракта Kachō (§1.7) и утверждается кейсами чёрного ящика.
// Канону делегируется РЕШЕНИЕ о форме, а не форма ответа.
func validateDisplayName(v string) error {
	if err := corevalidate.NameOnCreate("name", v); err != nil {
		return ErrIllegalName
	}
	return nil
}

// VolumeStatus — статус жизненного цикла Volume. Ширина int32 совпадает с
// storagev1.Volume_Status, поэтому конверсии domain↔proto точны.
type VolumeStatus int32

// Значения VolumeStatus (parity с proto-enum storage.v1:
// UNSPECIFIED=0, CREATING=1, AVAILABLE=2, IN_USE=3, DELETING=4, ERROR=5).
const (
	VolumeStatusUnspecified VolumeStatus = iota
	VolumeStatusCreating
	VolumeStatusAvailable
	VolumeStatusInUse
	VolumeStatusDeleting
	VolumeStatusError
	// VolumeStatusMigrating — том переезжает в другой класс.
	//
	// Состояние ВЫВОДИТСЯ из расхождения ревизий привязки (желаемая назначена,
	// действующая ещё прежняя), а не хранится колонкой — той же линией, которой
	// привязанность выводится из наличия строки привязки. Хранить его значило бы
	// завести второй источник истины об одном факте.
	VolumeStatusMigrating
)

// Validate проверяет, что статус — известное значение.
func (s VolumeStatus) Validate() error {
	switch s {
	case VolumeStatusUnspecified, VolumeStatusCreating, VolumeStatusAvailable,
		VolumeStatusInUse, VolumeStatusDeleting, VolumeStatusError, VolumeStatusMigrating:
		return nil
	default:
		return errors.New("volume status is out of range")
	}
}

// DeriveMigrating — статус тома, у которого назначена другая ревизия привязки.
// Переезд перекрывает готовность и привязанность: пока данные едут, «доступен» было
// бы утверждением о том, чего сейчас нет.
func DeriveMigrating(state string, migrating bool) (VolumeStatus, bool) {
	if migrating && (state == "READY") {
		return VolumeStatusMigrating, true
	}
	return VolumeStatusUnspecified, false
}

// DeriveStatus вычисляет wire-Status из persisted state + наличия attachment
// (§1.3, фикс дрейфа B3): READY+attachment → IN_USE, READY без attach → AVAILABLE,
// остальные state отображаются 1:1. Единственный источник derive — не хранится
// отдельной колонкой.
func DeriveStatus(state string, attached bool) VolumeStatus {
	switch state {
	case "CREATING":
		return VolumeStatusCreating
	case "DELETING":
		return VolumeStatusDeleting
	case "ERROR":
		return VolumeStatusError
	case "READY":
		if attached {
			return VolumeStatusInUse
		}
		return VolumeStatusAvailable
	default:
		return VolumeStatusUnspecified
	}
}

// Volume — блочный том (zonal-ресурс, привязан к zone_id и disk_type_id).
// Владелец — kacho-storage. Плоский ресурс (без K8s-envelope). Публичная проекция
// lean (INV-7): только tenant-facing поля, инфра-полей (backend-LUN/pool/node) на
// публичном Volume нет — они живут в internal-проекции :9091 (будущий data-plane).
//
// Attachments — output-only derive-on-read из volume_attachments (source of truth
// для attach-state); Status — derived (см. DeriveStatus). Оба заполняются repo на
// чтении, на вход Create/Update не принимаются.
type Volume struct {
	ID             string
	ProjectID      string
	Name           string
	Description    string
	Labels         map[string]string
	ZoneID         string
	DiskTypeID     string
	SizeBytes      int64
	SourceSnapshot string
	// SourceImage — id образа (Image), из которого материализован boot-Volume (F9).
	// Immutable; same-DB FK → images ON DELETE SET NULL (provenance, не live-dependency).
	// Взаимоисключение с SourceSnapshot: том засевается из ОДНОГО источника.
	SourceImage string
	Status      VolumeStatus
	Attachments []VolumeAttachment // output-only, выводится из строк привязки
	CreatedAt   time.Time
	UpdatedAt   time.Time

	// StatusReason — почему том оказался в своём состоянии. Публичное поле,
	// закрытый словарь: свободная строка на этом месте была бы прямым каналом
	// утечки текста бэкенда наружу.
	StatusReason StatusReason

	// Placement и Observation — ИНФРА-проекция: ни одно их поле не выходит на
	// публичную поверхность, они живут только на внутреннем листенере.
	Backend     Placement
	Observation Observation
}

// Validate проверяет domain-инварианты Volume перед созданием. Порядок выдаёт
// контрактные тексты для input-негативов S1-11: name-формат → ErrIllegalName;
// size_bytes<=0 → errIllegalSize (DB-backstop CHECK size_bytes>0). Cross-service
// ссылки (zone_id→geo, project_id→iam) валидируются peer-API на request-path, а не
// здесь (existence — не domain-инвариант формы).
func (v Volume) Validate() error {
	if v.ProjectID == "" {
		return errors.New("volume project_id is required")
	}
	if v.ZoneID == "" {
		return errors.New("volume zone_id is required")
	}
	if v.DiskTypeID == "" {
		return errors.New("volume disk_type_id is required")
	}
	if err := VolumeName(v.Name).Validate(); err != nil {
		return err
	}
	if v.SizeBytes <= 0 {
		return errIllegalSize
	}
	// Взаимоисключение источников (F9, STOR-1-19): том засевается ЛИБО из snapshot,
	// ЛИБО из image, не из обоих (spoken-exclusion). Backstop — нет (нельзя выразить
	// одним DB-CHECK, оба поля независимо nullable), поэтому энфорсим в домене.
	if v.SourceSnapshot != "" && v.SourceImage != "" {
		return errSourceConflict
	}
	return v.Status.Validate()
}
