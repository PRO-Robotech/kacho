// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

import (
	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
)

// Validate — стража старта, живущая РЯДОМ С КОНФИГУРАЦИЕЙ, а не в
// композиционном корне.
//
// Почему место значимо. Пока стража стояла в `cmd/`, она была частью одного
// конкретного бинаря: тот, кто соберёт из этого конфига второй процесс (мигратор,
// проба, обслуживающая утилита), получил бы конфигурацию без единой проверки и не
// узнал бы об этом. Проверка, привязанная к значению, едет с ним всюду.
//
// Сегодня Validate гейтит одно измерение — круг отправителей чужой личности.
// Прочие боевые инварианты geo (транспорт листенеров и ребра к iam, sslmode,
// адрес проверки прав) остаются в `cmd/kacho-geo` и переезжают сюда отдельным
// шагом: смешивать перенос конструкции с изменением поведения нельзя, это разные
// предметы обзора.
func (c Config) Validate() error {
	// Круг отправителей обязан быть сужен на ЛЮБОМ non-breakglass старте, а не
	// только в боевом режиме. Эта форма пришла из geo: он единственный из семи
	// применял её изначально, и канон распространил её на остальных, а не понизил
	// до общего знаменателя.
	//
	// Аварийный режим освобождает: он и так снимает per-RPC проверку и mTLS
	// целиком, а в боевом режиме отвергается отдельно (validateSecurityConfig).
	//
	// Стража общая на все семь сервисов: grpcsrv.TrustedForwarders.Require — один
	// исход, один текст отказа, различаются только имена ручек.
	if c.AuthZBreakglass {
		return nil
	}
	return c.TrustedForwarders().Require(grpcsrv.ForwarderGate{
		Production:   c.AuthMode == "production" || c.AuthMode == "production-strict",
		DevTrustAny:  c.AuthZTrustAnyForwarder,
		SANsKnob:     "KACHO_GEO_AUTHZ_TRUSTED_FORWARDER_SANS",
		TrustAnyKnob: "KACHO_GEO_AUTHZ_TRUST_ANY_FORWARDER",
	})
}
