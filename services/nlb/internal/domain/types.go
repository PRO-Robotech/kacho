// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package domain — self-validating domain newtypes для kacho-nlb.
//
// Все поля с семантикой — newtypes с `Validate error`. Голый `string`
// запрещён. Domain-пакет импортирует ТОЛЬКО stdlib и собственный фундамент
// (`pkg/option`, `pkg/errors`) — никаких pgx, grpc-stubs, sqlc-types; domain не
// знает adapter'ов (workspace CLAUDE.md «Чистая архитектура»).
//
// CreatedAt сюда не входит (DB-managed) — он живёт в repo-сущности
// .
package domain

import (
	"net/netip"
	"regexp"
	"time"
	"unicode/utf8"

	coreerrors "github.com/PRO-Robotech/kacho/pkg/errors"
	"github.com/PRO-Robotech/kacho/pkg/option"
	corevalidate "github.com/PRO-Robotech/kacho/pkg/validate"
)

// ---- ID newtypes -----------------------------------------------------------

type (
	// ResourceID — 3-char prefix + 17-char crockford-base32 (corelib/ids).
	// Внутри domain хранится как непрозрачная строка; формат валидируется
	// в service-слое (corevalidate.ResourceID).
	ResourceID string

	// ProjectID — id ресурса kaname Project, "prj" + 17.
	ProjectID string

	// RegionID — семантический id региона (напр. "<region>"); владелец Geography — kacho-geo.
	RegionID string

	// ZoneID — семантический id зоны (напр. "<region>-<zone-suffix>"); владелец Geography — kacho-geo.
	ZoneID string

	// SubnetID, AddressID, NicID, InstanceID — type-aliases для ResourceID.
	// Алиасы (а не distinct newtypes) — потому что они хранят тот же
	// 20-символьный prefix-формат и cross-service refs валидируются в worker-е
	// peer-gRPC-вызовом (а не локально на типе).
	SubnetID   = ResourceID
	AddressID  = ResourceID
	NicID      = ResourceID
	InstanceID = ResourceID
)

// ---- Семантические строковые поля ------------------------------------------

type (
	// LbName — имя ресурса nlb: единая форма имени дерева
	// (corevalidate.NameForm) ПЛЮС требование имени. Требование — собственное
	// решение nlb, и оно сохранено: пустое имя отвергается своим сообщением.
	LbName string

	// LbDescription — UTF-8 длиной ≤ 256.
	LbDescription string

	// LbNameOpt — optional LbName (используется как nullable name-поле, где
	// семантически «не задано» отличается от «пустая строка»).
	LbNameOpt = option.ValueOf[LbName]
)

// ---- Labels (карта с typed key/value) --------------------------------------

type (
	// LbLabelKey — ключ label (regex `^[a-z][-_./\\@a-z0-9]{0,62}$`).
	LbLabelKey string

	// LbLabelVal — значение label (0..63 байт).
	LbLabelVal string

	// LbLabels — labels-набор; cardinality ≤ MaxLabelPairs.
	//
	// Обычная карта, а не контейнер стороннего модуля (тот не нёс лицензии и
	// снят). Из тринадцати его методов здесь читались четыре, и все четыре у
	// карты есть в языке: длина, обход, чтение по ключу, запись. Порядок обхода
	// не менялся — хеш-словарь его тоже не давал, и ни одно место на него не
	// опиралось: наружу набор уходит картой (`LabelsToMap`), а сравнивается по
	// составу (`LabelsEqual`).
	LbLabels = map[LbLabelKey]LbLabelVal
)

// ---- Сетевые/численные newtypes --------------------------------------------

type (
	// LbPort — порт TCP/UDP, 1..65535.
	LbPort int32

	// LbProto — protocol listener'а: "TCP" | "UDP" (L4 only).
	LbProto string

	// IPVersion — "IPV4" | "IPV6".
	IPVersion string

	// IPAddress — текстовое представление IP; парсится netip.ParseAddr.
	IPAddress string

	// LbWeight — вес таргета в TG, 0..MaxTargetWeight (0 = "drain без remove").
	LbWeight int32

	// LbDuration — длительность (healthcheck interval/timeout, deregistration
	// delay). Range зависит от поля; валидируется на структуре, не на типе.
	LbDuration time.Duration
)

// ---- Regex -----------------------------------------------------------------

var (
	// label-key контракт (зеркалит kacho-vpc).
	lbLabelKeyRe = regexp.MustCompile(`^[a-z][-_./\\@a-z0-9]{0,62}$`)
)

// Здесь стояла ВТОРАЯ форма имени ресурса — `^[a-z][-a-z0-9]{1,61}[a-z0-9]$`.
//
// С единственной формой дерева (corevalidate.NameForm) она расходилась по двум
// осям, и ни одна нигде не обоснована: минимальная длина 3 (имя из одного-двух
// символов было невыразимо) и запрет ведущей цифры. Расхождение накопилось
// молча — как и три остальных, сведённых тем же изменением (#715).
//
// Требование имени nlb этим НЕ ослаблено: его выражает вызов validate.Name
// (пустая строка → «name is required»), а не форма.

// ---- Validate ------------------------------------------------------------

// Validate проверяет имя: единая форма дерева ПЛЮС требование имени.
//
// Зовётся именно validate.Name, а не NameOnCreate: nlb требует имя на создании,
// и сведение к канону этого НЕ ослабляет. Пустая строка отвергается ОТДЕЛЬНЫМ
// сообщением («name is required»), а не общим отказом по форме — вызывающий,
// забывший поле, и вызывающий, приславший `My_Name`, ошиблись по-разному, и
// отказ обязан это различать. Оба сообщения производит канон, поэтому текст
// отказа не может разойтись с самой формой.
func (n LbName) Validate() error {
	return corevalidate.Name("name", string(n))
}

// Validate проверяет длину description (UTF-8 rune count ≤ MaxDescriptionLen).
func (d LbDescription) Validate() error {
	if utf8.RuneCountInString(string(d)) > MaxDescriptionLen {
		return coreerrors.InvalidArgument().
			AddFieldViolation("description", "description length exceeds 256 chars").
			Err()
	}
	return nil
}

// Validate проверяет LabelKey-регекс (1..63 bytes).
func (k LbLabelKey) Validate() error {
	s := string(k)
	if len(s) == 0 || len(s) > MaxLabelKeyLen || !lbLabelKeyRe.MatchString(s) {
		return coreerrors.InvalidArgument().
			AddFieldViolation("labels."+s,
				"invalid label key (1..63 chars, lowercase letters, digits, _-./\\@; must start with letter)").
			Err()
	}
	return nil
}

// Validate проверяет LbLabelVal (0..63 bytes; пустая строка OK).
func (v LbLabelVal) Validate() error {
	if len(string(v)) > MaxLabelValueLen {
		return coreerrors.InvalidArgument().
			AddFieldViolation("labels", "label value exceeds 63 chars").
			Err()
	}
	return nil
}

// ValidateLabels — cardinality ≤ MaxLabelPairs + per-key/value validate.
// Свободная функция, а не метод: LbLabels — псевдоним карты, метод на нём не
// объявить.
func ValidateLabels(labels LbLabels) error {
	if len(labels) > MaxLabelPairs {
		return coreerrors.InvalidArgument().
			AddFieldViolation("labels", "too many labels (max 64)").
			Err()
	}
	for k, v := range labels {
		if err := k.Validate(); err != nil {
			return err
		}
		if err := v.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// Validate проверяет port-range [PortMin, PortMax].
func (p LbPort) Validate() error {
	if p < PortMin || p > PortMax {
		return coreerrors.InvalidArgument().
			AddFieldViolation("port", "port must be in range [1, 65535]").
			Err()
	}
	return nil
}

// Validate проверяет, что proto ∈ {TCP, UDP} (L4 only —).
func (p LbProto) Validate() error {
	switch p {
	case ProtoTCP, ProtoUDP:
		return nil
	}
	return coreerrors.InvalidArgument().
		AddFieldViolation("protocol", "protocol must be one of: TCP, UDP").
		Err()
}

// Validate проверяет, что ip_version ∈ {IPV4, IPV6}.
func (v IPVersion) Validate() error {
	switch v {
	case IPVersionV4, IPVersionV6:
		return nil
	}
	return coreerrors.InvalidArgument().
		AddFieldViolation("ip_version", "ip_version must be one of: IPV4, IPV6").
		Err()
}

// Validate проверяет, что address парсится netip.ParseAddr.
// Bogon-/public-only policy для target.external_ip — отдельно в Target.Validate
// (там это не «формат IP», а «политика target'а»).
func (a IPAddress) Validate() error {
	if a == "" {
		return coreerrors.InvalidArgument().
			AddFieldViolation("address", "address is required").
			Err()
	}
	if _, err := netip.ParseAddr(string(a)); err != nil {
		return coreerrors.InvalidArgument().
			AddFieldViolation("address", "invalid IP address").
			Err()
	}
	return nil
}

// Validate проверяет weight ∈ [0, MaxTargetWeight].
func (w LbWeight) Validate() error {
	if w < 0 || w > MaxTargetWeight {
		return coreerrors.InvalidArgument().
			AddFieldViolation("weight", "weight must be in range [0, 1000]").
			Err()
	}
	return nil
}

// ---- Helpers для конверсии LbLabels ↔ map[string]string --------------------

// LabelsFromMap конвертирует map[string]string → LbLabels (handler-layer).
// nil-map → пустой LbLabels.
func LabelsFromMap(m map[string]string) LbLabels {
	d := make(LbLabels, len(m))
	for k, v := range m {
		d[LbLabelKey(k)] = LbLabelVal(v)
	}
	return d
}

// LabelsToMap — обратное преобразование для DTO. nil если LbLabels пуст
// (паритет с proto-семантикой: отсутствие labels = поле не задано).
func LabelsToMap(d LbLabels) map[string]string {
	if len(d) == 0 {
		return nil
	}
	m := make(map[string]string, len(d))
	for k, v := range d {
		m[string(k)] = string(v)
	}
	return m
}

// stringsEqualOrdered — порядко-чувствительное сравнение двух наборов строк
// (used in Update no-op detection для disabled_announce_zones). Набор хранится
// в стабильном порядке (нормализован на входе), поэтому ordered-compare корректен.
func stringsEqualOrdered(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// LabelsEqual — set-equality для LbLabels (used in Update no-op detection).
func LabelsEqual(a, b LbLabels) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			return false
		}
	}
	return true
}
