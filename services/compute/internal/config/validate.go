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
// Прочие боевые инварианты compute (транспорт листенеров и рёбер, sslmode,
// фильтр видимости) остаются в `cmd/compute` и переезжают сюда отдельным шагом:
// смешивать перенос конструкции с изменением поведения нельзя, это разные
// предметы обзора.
func (c Config) Validate() error {
	// Круг отправителей обязан быть сужен на ЛЮБОМ non-breakglass старте, а не
	// только в боевом режиме: контроль, чья ветка на локальном стенде не
	// исполняется ни разу, находит «забыл выставить круг» только на боевом
	// профиле, где цена ошибки максимальна. Вне боевого режима пустой круг
	// остаётся возможным, но как ЯВНЫЙ опт-ин.
	//
	// Аварийный режим освобождает: он и так снимает per-RPC проверку целиком, а в
	// боевом режиме отвергается отдельно (cmd/compute validateAuthMode).
	//
	// Стража общая на все семь сервисов: grpcsrv.TrustedForwarders.Require — один
	// исход, один текст отказа, различаются только имена ручек.
	if c.AuthZBreakglass {
		return nil
	}
	return c.TrustedForwarders().Require(grpcsrv.ForwarderGate{
		Production:   c.AuthMode == "production" || c.AuthMode == "production-strict",
		DevTrustAny:  c.AuthZTrustAnyForwarder,
		SANsKnob:     "KACHO_COMPUTE_AUTHZ_TRUSTED_FORWARDER_SANS",
		TrustAnyKnob: "KACHO_COMPUTE_AUTHZ_TRUST_ANY_FORWARDER",
	})
}
