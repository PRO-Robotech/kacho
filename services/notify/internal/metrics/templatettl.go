// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package metrics — ряды notify, которые не принадлежат ни одному
// исполнителю строк, а описывают сборку процесса (З27).
//
// Ряд notify_template_ttl_seconds{source, template, class} — срок каждого
// шаблона сборки в секундах. Он — вход тревоги Р18 «половина наименьшего ttl
// шаблонов security по возрасту строки» (NTF1-G12): правило чарта notify
// сравнивает возраст старейшей строки `pending` источника
// (kacho_notification_feed_oldest_pending_seconds, corelib notify/feed) с
// половиной наименьшего срока шаблонов security ТОГО ЖЕ источника. Срок
// берётся из сборки, а не литералом в правиле: правка ttl в notification.yaml
// меняет порог тревоги без правки чарта.
package metrics

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/PRO-Robotech/corelib/notify/feed"
)

// TemplateTTLName — имя ряда срока шаблонов сборки.
const TemplateTTLName = "notify_template_ttl_seconds"

const templateTTLHelp = "Срок шаблона сборки notify в секундах по источнику, шаблону и классу; " +
	"вход тревоги половины наименьшего срока шаблонов security."

// Template — шаблон сборки для ряда срока. Source — пространство шаблона,
// оно же имя модуля источника: метка `source` notify и метка `module` ленты
// источника называют модуль одним словом (deliver: `j.rt.src.Module`).
type Template struct {
	Source string
	Name   string
	Class  feed.Class
	TTL    time.Duration
}

// RegisterTemplateTTL проверяет перечень шаблонов сборки и регистрирует ряд
// срока в reg. Вход вне формы — отказ с именем предмета, и семейство не
// регистрируется: ряд с пустой меткой, классом вне перечня corelib,
// неположительным сроком или повтором шаблона сделал бы тревогу G12 ложной
// молча.
func RegisterTemplateTTL(reg prometheus.Registerer, ts []Template) error {
	if reg == nil {
		return errors.New("metrics: реестр метрик не задан")
	}
	if len(ts) == 0 {
		return errors.New("metrics: перечень шаблонов пуст — ряда срока, по которому судит тревога половины срока, нет")
	}
	type key struct{ source, name string }
	seen := make(map[key]bool, len(ts))
	for _, t := range ts {
		switch {
		case t.Source == "":
			return fmt.Errorf("metrics: шаблон %q: источник не назван", t.Name)
		case t.Name == "":
			return fmt.Errorf("metrics: шаблон источника %q: имя не названо", t.Source)
		case !slices.Contains(feed.Classes(), t.Class):
			return fmt.Errorf("metrics: шаблон %s/%s: класс %q вне перечня %v", t.Source, t.Name, t.Class, feed.Classes())
		case t.TTL <= 0:
			return fmt.Errorf("metrics: шаблон %s/%s: срок %s не положителен", t.Source, t.Name, t.TTL)
		}
		k := key{t.Source, t.Name}
		if seen[k] {
			return fmt.Errorf("metrics: шаблон %s/%s назван в перечне дважды", t.Source, t.Name)
		}
		seen[k] = true
	}

	vec := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: TemplateTTLName,
		Help: templateTTLHelp,
	}, []string{"source", "template", "class"})
	for _, t := range ts {
		vec.WithLabelValues(t.Source, t.Name, string(t.Class)).Set(t.TTL.Seconds())
	}
	if err := reg.Register(vec); err != nil {
		return fmt.Errorf("metrics: регистрация %s: %w", TemplateTTLName, err)
	}
	return nil
}
