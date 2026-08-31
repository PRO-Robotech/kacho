// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package config — конфигурация kacho-vpc (YAML + viper).
//
// Default'ы — в defaults.go (не в struct-tags). ENV-binding — в load.go через
// `viper.SetEnvPrefix("KACHO_VPC")` + delimiter `__` для иерархии
// (`KACHO_VPC_REPOSITORY__POSTGRES__URL` → `repository.postgres.url`).
//
// Mode — общий режим работы сервиса (anonymous-allowed / fail-closed /
// fail-closed+strict-TLS), ENUM Mode{ModeDev, ModeProduction,
// ModeProductionStrict}. Это не «auth-mode» (TLS/none — отдельная подсекция
// authn.*).
package config

import (
	"encoding/json"
	"fmt"

	"github.com/PRO-Robotech/kacho/pkg/servicecontract"
)

// Mode — общий режим работы сервиса.
//
//	ModeDev              — СТРОГО локальный fixture-режим (unit/integration-тесты и
//	                       локальная разработка), НИКОГДА не для развернутого стенда:
//	                       AuthN-guard пропускает вызовы без предъявленного принципала,
//	                       insecure-defaults (TLS off, sslmode=disable) только
//	                       логируются. В проде — ModeProduction (fail-closed).
//	ModeProduction       — fail-closed: каждый запрос обязан нести forwarded
//	                       принципала (`x-kacho-principal-*`). Личность, которую
//	                       вызывающий объявил о себе сам, аутентификацией не
//	                       является; anonymous → PermissionDenied.
//	ModeProductionStrict — production + дополнительно валидирует extapi.*.tls.*
//	                       и repository.postgres.ssl-mode (require|verify-ca|verify-full).
type Mode int

// Значения ENUM. iota порядок стабилен; не менять без миграции values.yaml.
const (
	ModeDev Mode = iota
	ModeProduction
	ModeProductionStrict
)

// String — каноническое имя для журнала и текстов отказа. Берётся у ДОМА
// словаря, а не пишется здесь: имя и разбор — две стороны одного соответствия, и
// объявленные порознь они разошлись бы молча.
func (m Mode) String() string {
	host, known := m.host()
	if !known {
		// Значение вне перечня сюда не приходит: разбор его отвергает. Ветка
		// остаётся ради ЧИТАЕМОСТИ отказа, если оно всё же появится — иначе
		// невозможное значение напечаталось бы именем законной посадки, и
		// разбирающий пошёл бы искать не там.
		return fmt.Sprintf("mode(%d)", int(m))
	}
	return host.String()
}

// host переводит режим сервиса в общий словарь посадки. Второе значение — знал
// ли перевод, что переводит: «не знаю» и «dev» обязаны быть различимы, иначе
// невозможное значение молча читалось бы как самая слабая посадка.
func (m Mode) host() (servicecontract.Mode, bool) {
	switch m {
	case ModeDev:
		return servicecontract.ModeDev, true
	case ModeProduction:
		return servicecontract.ModeProduction, true
	case ModeProductionStrict:
		return servicecontract.ModeProductionStrict, true
	default:
		return 0, false
	}
}

// IsProduction возвращает true для любого production-варианта.
func (m Mode) IsProduction() bool {
	return m == ModeProduction || m == ModeProductionStrict
}

// parseMode — точечная инверсия String(); используется кастомным
// mapstructure-хуком и YAML-/ENV-loader'ом.
//
// Словарь допустимых написаний — НЕ свой: он объявлен в дереве один раз
// (`servicecontract.Modes`), и отказ перечисляет ТОТ ЖЕ набор, что у остальных
// шести стражей старта. Свой словарь здесь был, и он был одним из пяти; копии не
// собираются вместе и друг друга не читают, поэтому расхождение приходило молча —
// один из пяти расходился с остальными В ОБЕ СТОРОНЫ (задача продукта #1656).
//
// Неизвестное значение спарено с БОЕВЫМ режимом, а не с dev: оба вызывающих
// (хук mapstructure и UnmarshalJSON) на ошибке прерываются, но вызывающий,
// игнорирующий ошибку, обязан получить fail-closed, а не анонимный полный доступ.
func parseMode(s string) (Mode, error) {
	switch mode, err := servicecontract.ParseMode(s); {
	case err != nil:
		return ModeProduction, err
	case mode == servicecontract.ModeDev:
		return ModeDev, nil
	case mode == servicecontract.ModeProductionStrict:
		return ModeProductionStrict, nil
	default:
		return ModeProduction, nil
	}
}

// MarshalJSON / UnmarshalJSON — для удобной сериализации (mapstructure
// сам через DecodeHook парсит string, но JSON-output логов и тестов
// удобнее иметь строкой).
func (m Mode) MarshalJSON() ([]byte, error) { return json.Marshal(m.String()) }

func (m *Mode) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	parsed, err := parseMode(s)
	if err != nil {
		return err
	}
	*m = parsed
	return nil
}
