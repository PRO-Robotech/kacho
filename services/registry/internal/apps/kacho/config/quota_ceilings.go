// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

// quota_ceilings.go — ВЕЛИЧИНЫ ПОТОЛКОВ домена, объявленные ПОСАДКОЙ (приёмка
// судьбы авторитета величин, исход «величину объявляет посадка домена»,
// стадия S1).
//
// # Почему это посадка, а не настройка с умолчанием
//
// Авторитета величин нет ни в одном дереве, и своего домена платформа не завела:
// спросить величину не у кого — её объявляет оператор установки при выкатке.
// Умолчания нет НИ У ОДНОЙ величины: то, что подставляет построение, предметом
// стража быть не может — он зелен при любом входе, потому что незаданной такая
// величина не бывает.
//
// # Почему указатель, а не число
//
// Ноль — законная величина: «ресурсов этого вида не заводить». На простом
// `int64` он неотличим от «величина не объявлена», и оператор, желающий
// запретить вид, не смог бы этого выразить ни при каком вводе.
//
// # Имя переменной стоит ДВАЖДЫ — в таблице и в теге, и это держит проба
//
// Тег разбора обязан быть литералом, поэтому имя переменной повторяется. Второе
// написание разошлось бы с первым молча: отказ называл бы переменную, которая до
// поля не доезжает. Держит это проба «переменная каталога доезжает до своего
// поля» — она выводит имена ИЗ ТАБЛИЦЫ и проверяет теги.

import (
	"github.com/PRO-Robotech/kacho/pkg/quota/quotaceiling"
)

// quotaCeilingEnvPrefix — корневой сегмент имён величин потолков. Выводится из
// префикса службы, а не пишется третьим литералом.
const quotaCeilingEnvPrefix = envPrefix + "_QUOTA_CEILING_"

// quotaCeilingKnob собирает запись каталога. Координата ручки здесь и есть имя
// переменной: файла настроек у этой службы нет, и второй координаты у оператора
// не бывает.
func quotaCeilingKnob(
	name, kind, why string, value func(QuotaCeilingsConfig) *int64,
) quotaceiling.Knob[QuotaCeilingsConfig] {
	env := quotaCeilingEnvPrefix + name
	return quotaceiling.Knob[QuotaCeilingsConfig]{
		Key:   env,
		Env:   env,
		Kind:  kind,
		Why:   why,
		Value: value,
	}
}

// QuotaCeilingCatalog — КАТАЛОГ величин домена. Порядок — порядок, в котором
// величины встречает оператор.
//
// Таблица — единственное место, где ручка связана с ВИДОМ. Второе разошлось бы с
// первым молча: ручку правят коммитом сюда, вид — в миграцию учёта.
var QuotaCeilingCatalog = quotaceiling.Catalog[QuotaCeilingsConfig]{
	quotaCeilingKnob("REGISTRY", "registry.registries",
		"сколько реестров в проекте. Реестр — внешне адресуемая координата (`$domain/$registryId/...`), и каждый новый занимает её навсегда",
		func(c QuotaCeilingsConfig) *int64 { return c.Registry }),
	quotaCeilingKnob("REPOSITORY", "registry.repositories",
		"сколько репозиториев в проекте. Репозиторий копит слои, и место под ними освобождается только уборкой",
		func(c QuotaCeilingsConfig) *int64 { return c.Repository }),
}

// QuotaCeilingsConfig — величины посадки.
type QuotaCeilingsConfig struct {
	// Registry — потолок вида `registry.registries`.
	// ENV `KACHO_REGISTRY_QUOTA_CEILING_REGISTRY`.
	Registry *int64 `envconfig:"KACHO_REGISTRY_QUOTA_CEILING_REGISTRY"`
	// Repository — потолок вида `registry.repositories`.
	// ENV `KACHO_REGISTRY_QUOTA_CEILING_REPOSITORY`.
	Repository *int64 `envconfig:"KACHO_REGISTRY_QUOTA_CEILING_REPOSITORY"`
}

// ValidateQuotaCeilings — СТРАЖ МОЩНОСТИ объявленного множества: пусто либо
// каталог целиком, промежуточного не бывает.
//
// Предикат ОДИН на весь каталог, а не по величине за величиной: две отдельные
// проверки дают вызывающему возможность позвать первую и забыть вторую.
func (c Config) ValidateQuotaCeilings() error {
	return QuotaCeilingCatalog.Validate(c.QuotaCeilings)
}

// StatedQuotaCeilings — объявленные величины по видам; пустая карта означает,
// что посадка потолков не объявляет.
func (c Config) StatedQuotaCeilings() map[string]int64 {
	return QuotaCeilingCatalog.Stated(c.QuotaCeilings)
}
