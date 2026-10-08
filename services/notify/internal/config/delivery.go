// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

import (
	"github.com/PRO-Robotech/corelib/notify/feed"
	"github.com/PRO-Robotech/corelib/notify/form"
)

// validateDelivery — ручки цикла доставки (§8 замысла, З21, З23, З25, З26):
// число исполнителей, отсрочка DEFER, имя отправителя, внутренний слушатель
// kaname с точным SAN и якорь ретранслятора. Незаданные уже названы общим
// перебором и второй раз не называются.
func (c *Config) validateDelivery(fs *findings) {
	c.checkInt(fs, "Workers", c.Workers, WorkersMin, WorkersMax)
	c.checkDuration(fs, "DeferFor", c.DeferFor, feed.MinDefer, feed.MaxDefer)

	if k := KnobOfField("SMTPFromName"); !c.unset[k.Env] {
		if c.SMTPFromName == "" {
			fs.add(k, "значение пусто: имя отправителя — часть заголовка From каждого письма")
		} else if h, err := form.ParseHeaderText(c.SMTPFromName); err != nil {
			fs.add(k, "значение вне формы заголовка письма: %v", err)
		} else {
			c.fromName = h
		}
	}
	if k := KnobOfField("KanameAddr"); !c.unset[k.Env] {
		if err := checkHostPort(c.KanameAddr); err != nil {
			fs.add(k, "%v", err)
		}
	}
	if k := KnobOfField("KanameSAN"); !c.unset[k.Env] {
		if err := checkSPIFFE(c.KanameSAN); err != nil {
			fs.add(k, "%v", err)
		}
	}
	if c.trustAnchorSet && c.trustAnchor == "" {
		fs.add(trustAnchorKnob, "переменная задана, но путь пуст; отсутствие якоря выражается "+
			"отсутствием переменной, а не пустой строкой")
	}
}

// TrustAnchorKnob — ручка якоря ретранслятора для текста отказа у читателя
// файла (корень): поля [Config] у неё нет, [KnobOfField] её не называет.
func TrustAnchorKnob() Knob { return trustAnchorKnob }
