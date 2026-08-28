// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

import (
	"fmt"
	"strings"

	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
	"github.com/PRO-Robotech/kacho/pkg/servicecontract"
)

// Validate — страж старта geo.
//
// ПОЧЕМУ ЕГО ЗДЕСЬ НЕ БЫЛО И ЧЕМ ЭТО ГРОЗИЛО. geo объявлял четыре посадочные
// ручки — режим, `sslmode` (умолчание `disable`), обе ручки круга отправителей —
// каждую с комментарием про боевой режим, и не читал НИ ОДНУ. Самоотчёт о
// посадке (`cmd/kacho-geo/bootposture.go`) существовал и создавал видимость
// контроля, но самоотчёт СООБЩАЕТ, а не отказывает: процесс с `sslmode=disable`
// в боевом режиме стартовал и честно об этом рапортовал. Ban #16 требует
// обратного — `refuse-to-start при insecure config`.
//
// ПОЧЕМУ РЯДОМ С КОНФИГУРАЦИЕЙ, А НЕ В КОМПОЗИЦИОННОМ КОРНЕ. Проверка,
// привязанная к значению, едет с ним всюду; стража в `cmd/` — часть одного
// бинаря, и второй процесс из того же конфига (мигратор, проба) получил бы
// конфигурацию без единой проверки. Довод и форма взяты у compute, а не
// придуманы: там она стоит здесь же и по той же причине.
//
// ЧТО ЭТОТ СТРАЖ СУДИТ, И ПОЧЕМУ НЕ БОЛЬШЕ. Две оси, обе — из объявленных geo
// ручек, и обе выражены ОБЩИМ механизмом:
//
//   - круг отправителей чужой личности — `grpcsrv.ForwarderGate`, тот же вызов,
//     что у compute, iam, nlb, registry и vpc. Пустой круг означает «не сужаем»,
//     то есть принимать переданную личность будет любой проверенный пир;
//   - `sslmode` в боевом режиме — прямой пример из ban #16.
//
// Наборы осей у шести соседних стражей разные (48…967 строк), и сведение их к
// одному — предмет ОТДЕЛЬНЫЙ, со своей приёмкой: смешивать перенос конструкции
// с изменением поведения нельзя. Здесь geo доводится до того минимума, ниже
// которого запрет не исполняется вовсе.
func (c Config) Validate() error {
	mode, err := servicecontract.ParseMode(c.AuthMode)
	if err != nil {
		return fmt.Errorf("KACHO_GEO_AUTH_MODE: %w", err)
	}

	var problems []string

	// sslmode до своей БД. Умолчание ручки — `disable`, поэтому в боевом режиме
	// молчание конфигурации означает незашифрованное соединение, а не «оставили
	// как было».
	if mode.IsProduction() && !productionSSLMode(c.DBSSLMode) {
		problems = append(problems, fmt.Sprintf(
			"KACHO_GEO_DB_SSLMODE=%q (нужен require, verify-ca или verify-full)", c.DBSSLMode))
	}

	if len(problems) > 0 {
		return fmt.Errorf("%s mode refuses insecure config: %s", mode, strings.Join(problems, "; "))
	}

	// Круг отправителей судится ОБЩИМ предикатом — тем же, которым его читает
	// транспорт и самоотчёт. Три разных предиката об одном значении разошлись бы
	// там, где расхождение и опасно: на вырожденном входе вроде одинокой запятой
	// длина строки настроек не ноль, а записей ноль.
	return c.TrustedForwarders().Require(grpcsrv.ForwarderGate{
		Production:   mode.IsProduction(),
		DevTrustAny:  c.AuthZTrustAnyForwarder,
		SANsKnob:     "KACHO_GEO_AUTHZ_TRUSTED_FORWARDER_SANS",
		TrustAnyKnob: "KACHO_GEO_AUTHZ_TRUST_ANY_FORWARDER",
	})
}

// productionSSLMode — режимы соединения с БД, допустимые в боевом.
//
// Перечень закрыт и повторяет ban #16 дословно: `require`, `verify-ca`,
// `verify-full`. Пустая строка сюда не входит намеренно — `baseDSN` деривит её
// в `disable`, поэтому «не задано» и «выключено» здесь одно и то же.
func productionSSLMode(v string) bool {
	switch v {
	case "require", "verify-ca", "verify-full":
		return true
	default:
		return false
	}
}
