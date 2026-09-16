// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

// quota_ceilings.go — ТРИ ВЕЛИЧИНЫ ПОТОЛКОВ домена балансировки, объявленных
// ПОСАДКОЙ (приёмка
// `docs/specs/sub-phase-QUOTA-FATE-1-limit-value-is-stated-by-posture-acceptance.md`,
// исход В, стадия `S1`).
//
// # Почему это посадка, а не настройка с умолчанием
//
// Авторитета величин нет ни в одном дереве, и своего домена платформа не завела:
// спросить величину не у кого — её объявляет оператор установки файлом при
// выкатке. Умолчания нет НИ У ОДНОЙ величины: то, что подставляет построение,
// предметом стража быть не может — он зелен при любом входе.
//
// # Привязка переменной ЯВНАЯ, и это решение, а не оплошность
//
// Разборщик этой службы переводит в имя переменной только точку, но не дефис
// (`load.go`, `envKeyDelimiter`). Расширить правило значило бы сделать ЧИТАЕМЫМИ
// три десятка ключей, которые сегодня переменной не читаются вовсе, — то есть
// изменить поведение установок правкой, к предмету не относящейся. Поэтому имя
// выводится общим правилом (`quotaceiling.EnvNameOf`) и привязывается ЯВНО
// (`v.BindEnv`), а поведение прочих ключей не трогается ни на байт.

import (
	"github.com/PRO-Robotech/kacho/pkg/quota/quotaceiling"
)

// quotaCeilingKeyPrefix — секция настроек внутри `quota`: предмет у неё общий с
// объявлением домена величин — откуда берётся потолок.
const quotaCeilingKeyPrefix = "quota.ceilings."

// quotaCeilingKnob собирает запись каталога, ВЫВОДЯ имя переменной из ключа.
func quotaCeilingKnob(
	name, kind, why string, value func(QuotaCeilingsConfig) *int64,
) quotaceiling.Knob[QuotaCeilingsConfig] {
	key := quotaCeilingKeyPrefix + name
	return quotaceiling.Knob[QuotaCeilingsConfig]{
		Key:   key,
		Env:   quotaceiling.EnvNameOf(envPrefix, key),
		Kind:  kind,
		Why:   why,
		Value: value,
	}
}

// QuotaCeilingCatalog — КАТАЛОГ величин домена. Порядок — порядок, в котором
// величины встречает оператор: балансировщик, затем то, что к нему крепится.
//
// Виды названы токеном КАТАЛОГА (`loadbalancer.*`), а не именем службы (`nlb`):
// каталог знает домен под первым именем, и второе здесь называло бы вид, которого
// не списывает ни один триггер.
var QuotaCeilingCatalog = quotaceiling.Catalog[QuotaCeilingsConfig]{
	quotaCeilingKnob("network-load-balancers", "loadbalancer.networkLoadBalancers",
		"сколько балансировщиков в проекте. Каждый занимает внешний адрес из "+
			"ОГРАНИЧЕННОГО пула установки, и без потолка один арендатор "+
			"исчерпывает его для всех",
		func(c QuotaCeilingsConfig) *int64 { return c.NetworkLoadBalancers }),
	quotaCeilingKnob("listeners", "loadbalancer.listeners",
		"сколько слушателей в проекте. Слушатель занимает порт на балансировщике "+
			"и держит проверки состояния",
		func(c QuotaCeilingsConfig) *int64 { return c.Listeners }),
	quotaCeilingKnob("target-groups", "loadbalancer.targetGroups",
		"сколько целевых групп в проекте. Каждая опрашивает свои цели "+
			"постоянно, поэтому её цена не разовая",
		func(c QuotaCeilingsConfig) *int64 { return c.TargetGroups }),
}

// QuotaCeilingsConfig — три величины посадки.
//
// УКАЗАТЕЛЬ, а не число: ноль — законная величина («ресурсов этого вида не
// заводить»), и на простом `int64` он неотличим от «величина не объявлена».
type QuotaCeilingsConfig struct {
	// NetworkLoadBalancers — потолок вида `loadbalancer.networkLoadBalancers`.
	NetworkLoadBalancers *int64 `mapstructure:"network-load-balancers"`
	// Listeners — потолок вида `loadbalancer.listeners`.
	Listeners *int64 `mapstructure:"listeners"`
	// TargetGroups — потолок вида `loadbalancer.targetGroups`.
	TargetGroups *int64 `mapstructure:"target-groups"`
}

// ValidateQuotaCeilings — СТРАЖ МОЩНОСТИ объявленного множества: пусто либо
// каталог целиком, промежуточного не бывает.
func (c Config) ValidateQuotaCeilings() error {
	return QuotaCeilingCatalog.Validate(c.Quota.Ceilings)
}

// StatedQuotaCeilings — объявленные величины по видам; пустая карта означает,
// что посадка потолков не объявляет.
func (c Config) StatedQuotaCeilings() map[string]int64 {
	return QuotaCeilingCatalog.Stated(c.Quota.Ceilings)
}
