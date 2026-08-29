// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package address

import (
	"errors"
	"fmt"

	"google.golang.org/protobuf/types/known/anypb"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/dto"
	kachorepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"

	// Blank-import регистрирует трансферы Address/time через init().
	_ "github.com/PRO-Robotech/kacho/services/vpc/internal/dto/toproto"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo"
)

// niReferrerType — ReferrerType в address_references для адресов, привязанных
// к NetworkInterface. Зеркальная копия константы из
// `internal/apps/kacho/api/networkinterface/create.go::niReferrerType`.
const niReferrerType = "network_interface"

// lbReferrerType — ReferrerType в address_references для VIP-адресов, привязанных
// к network load balancer'у (owner-сервис хранит referrer через SetReference).
const lbReferrerType = "network_load_balancer"

// referrerTypeLabel переводит машинный ReferrerType в человекочитаемую форму для
// Delete-guard-сообщения. Неизвестный тип отдается как есть.
func referrerTypeLabel(referrerType string) string {
	switch referrerType {
	case niReferrerType:
		return "network interface"
	case lbReferrerType:
		return "network_load_balancer"
	default:
		return referrerType
	}
}

// isUniqueViolation распознаёт конфликт уникальности для петли повторов в
// аллокаторе: «этот адрес уже занят, возьми следующий».
//
// # Решение принимает слой repo, а не этот
//
// Род отказа хранилища разбирается по КОДУ и ровно в одном доме
// (`pkg/db/pgfault`); слой repo переводит разобранный род в сигнальную ошибку
// (`helpers.WrapPgErr`: 23505 → `ErrAlreadyExists`), и она — единственный
// контракт между repo и use-case. Читать здесь что-либо кроме сигнала нечем и
// незачем: дом опирается на `*pgconn.PgError`, а use-case драйвер не импортирует
// (`architecture.md`, dependency rule).
//
// # Здесь стоял запасной разбор ПО СЛОВАМ сервера — снят (#1455)
//
// Он сверял текст ошибки с «duplicate key value» и «SQLSTATE 23505», объявляя
// себя оборонительным — на случай, если repo вернёт неразобранный отказ. Такого
// пути в дереве нет: оба вызывающих (`SetInternalIPv4`, `SetInternalIPv6`)
// возвращают либо `subnetPairRefusal` (ErrNotFound), либо `helpers.WrapPgErr`.
// То есть ветка не имела ПРОИЗВОДИТЕЛЯ, а цену несла: текст сервера зависит от
// `lc_messages` и от выпуска сервера, поэтому на русской локали подстрока не
// совпадала бы ВОВСЕ — и конфликт уникальности уезжал бы в ветку
// неклассифицированного отказа. Предикат по подстроке при этом не краснеет: он
// молча перестаёт совпадать.
func isUniqueViolation(err error) bool {
	return errors.Is(err, repo.ErrAlreadyExists)
}

// marshalAddressRecord конвертирует repo-entity Address в *anypb.Any через
// DTO-реестр. Используется worker'ами Create/Update/Move для упаковки
// результата в Operation.response.
func marshalAddressRecord(rec *kachorepo.AddressRecord) (*anypb.Any, error) {
	var dst *vpcv1.Address
	if err := dto.Transfer(dto.FromTo(*rec, &dst)); err != nil {
		return nil, fmt.Errorf("dto.Transfer Address: %w", err)
	}
	return anypb.New(dst)
}
