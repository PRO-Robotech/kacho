// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

// quota_ceilings.go — ВОСЕМЬ ВЕЛИЧИН ПОТОЛКОВ домена vpc, объявленных ПОСАДКОЙ
// (приёмка `docs/specs/sub-phase-QUOTA-FATE-1-limit-value-is-stated-by-posture-acceptance.md`,
// исход В, стадия `S1`).
//
// # Почему это посадка, а не настройка с умолчанием
//
// Авторитета величин нет ни в одном дереве: контракт снят вместе с модулем
// квотирования службы доступа, и своего домена платформа не завела. Спросить
// величину не у кого — её объявляет оператор установки файлом при выкатке.
//
// Умолчания здесь нет НИ У ОДНОЙ величины, и это не строгость ради строгости:
// величина, которую подставляет построение, предметом стража быть не может —
// он зелен при любом входе, потому что незаданной она не бывает.
//
// # Что эти восемь ручек НЕ отменяют
//
// Ручку `quota.authority`: появись у контракта авторитета производитель, он
// заполнит области `ACCOUNT` и `PROJECT`, которые СТАРШЕ `DEFAULT`, и перекроет
// посадку по построению. Посадка занимает низшую полосу — ту, что сегодня пуста.

import (
	"github.com/PRO-Robotech/kacho/pkg/quota/quotaceiling"
)

// quotaCeilingKeyPrefix — секция настроек внутри `quota`. Рядом с объявлением
// домена величин намеренно: предмет у них один — откуда берётся потолок.
const quotaCeilingKeyPrefix = "quota.ceilings."

// quotaCeilingKnob собирает запись каталога, ВЫВОДЯ имя переменной из ключа.
//
// Второе написание имени разошлось бы с первым молча, и переменная, названную
// текстом отказа, перестала бы доезжать до поля.
func quotaCeilingKnob(
	name, kind, why string, value func(QuotaCeilingsConfig) *int64,
) quotaceiling.Knob[QuotaCeilingsConfig] {
	key := quotaCeilingKeyPrefix + name
	return quotaceiling.Knob[QuotaCeilingsConfig]{
		Key:   key,
		Env:   quotaceiling.EnvNameOf(EnvPrefix, key),
		Kind:  kind,
		Why:   why,
		Value: value,
	}
}

// QuotaCeilingCatalog — КАТАЛОГ величин домена. Порядок — порядок, в котором
// величины встречает оператор: сперва сеть (первое, что он заводит), затем то,
// что внутри неё.
//
// Таблица — единственное место, где ключ настройки связан с ВИДОМ. Второе
// разошлось бы с первым молча: ключ правят коммитом сюда, вид — в миграцию
// учёта, и расхождение видно только тому, кто в этот день ставит службу.
var QuotaCeilingCatalog = quotaceiling.Catalog[QuotaCeilingsConfig]{
	quotaCeilingKnob("network", "vpc.network",
		"сколько сетей в проекте. Сеть — корень всего остального в домене: без "+
			"этого потолка каждая новая сеть покупает полный набор всех прочих",
		func(c QuotaCeilingsConfig) *int64 { return c.Network }),
	quotaCeilingKnob("subnet", "vpc.subnet",
		"сколько подсетей в проекте. Каждая занимает диапазон адресов, и "+
			"исчерпание плана адресации видно не сразу",
		func(c QuotaCeilingsConfig) *int64 { return c.Subnet }),
	quotaCeilingKnob("security-group", "vpc.securityGroup",
		"сколько групп безопасности в проекте. Каждая несёт правила, которые "+
			"вычисляются на каждом изменении состава",
		func(c QuotaCeilingsConfig) *int64 { return c.SecurityGroup }),
	quotaCeilingKnob("route-table", "vpc.routeTable",
		"сколько таблиц маршрутизации в проекте",
		func(c QuotaCeilingsConfig) *int64 { return c.RouteTable }),
	quotaCeilingKnob("address", "vpc.address",
		"сколько адресов в проекте. Адрес берётся из ОГРАНИЧЕННОГО пула "+
			"установки, и без потолка один арендатор исчерпывает его для всех",
		func(c QuotaCeilingsConfig) *int64 { return c.Address }),
	quotaCeilingKnob("gateway", "vpc.gateway",
		"сколько шлюзов в проекте",
		func(c QuotaCeilingsConfig) *int64 { return c.Gateway }),
	quotaCeilingKnob("network-interface", "vpc.networkInterface",
		"сколько сетевых интерфейсов в проекте. Интерфейс держит адрес, поэтому "+
			"его потолок ограничивает потребление пула вторым путём",
		func(c QuotaCeilingsConfig) *int64 { return c.NetworkInterface }),
	quotaCeilingKnob("cidr-group", "vpc.cidrGroup",
		"сколько групп префиксов в проекте",
		func(c QuotaCeilingsConfig) *int64 { return c.CidrGroup }),
}

// QuotaCeilingsConfig — восемь величин посадки.
//
// УКАЗАТЕЛЬ, а не число: ноль — законная и осмысленная величина («ресурсов
// этого вида не заводить»), и на простом `int64` он неотличим от «величина не
// объявлена». Оператор, желающий запретить вид, не смог бы этого выразить ни
// при каком вводе.
type QuotaCeilingsConfig struct {
	// Network — потолок вида `vpc.network`. ENV `KACHO_VPC_QUOTA__CEILINGS__NETWORK`.
	Network *int64 `mapstructure:"network"`
	// Subnet — потолок вида `vpc.subnet`.
	Subnet *int64 `mapstructure:"subnet"`
	// SecurityGroup — потолок вида `vpc.securityGroup`.
	SecurityGroup *int64 `mapstructure:"security-group"`
	// RouteTable — потолок вида `vpc.routeTable`.
	RouteTable *int64 `mapstructure:"route-table"`
	// Address — потолок вида `vpc.address`.
	Address *int64 `mapstructure:"address"`
	// Gateway — потолок вида `vpc.gateway`.
	Gateway *int64 `mapstructure:"gateway"`
	// NetworkInterface — потолок вида `vpc.networkInterface`.
	NetworkInterface *int64 `mapstructure:"network-interface"`
	// CidrGroup — потолок вида `vpc.cidrGroup`.
	CidrGroup *int64 `mapstructure:"cidr-group"`
}

// ValidateQuotaCeilings — СТРАЖ МОЩНОСТИ объявленного множества: пусто либо
// каталог целиком, промежуточного не бывает.
//
// Предикат ОДИН на весь каталог, а не по величине за величиной: две отдельные
// проверки дают вызывающему возможность позвать первую и забыть вторую.
func (c Config) ValidateQuotaCeilings() error {
	return QuotaCeilingCatalog.Validate(c.Quota.Ceilings)
}

// StatedQuotaCeilings — объявленные величины по видам; пустая карта означает,
// что посадка потолков не объявляет.
func (c Config) StatedQuotaCeilings() map[string]int64 {
	return QuotaCeilingCatalog.Stated(c.Quota.Ceilings)
}
