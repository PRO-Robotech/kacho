// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

import (
	"errors"
	"time"

	"github.com/PRO-Robotech/kacho/services/notify/internal/dkimkey"
	"github.com/PRO-Robotech/kacho/services/notify/internal/dnscheck"
)

// Границы ручек DNS установки — §8, §12а «Ручки и признак». Нижняя граница
// срока старта согласована с формой попытки стража: в 10 с входят одна полная
// попытка (≤ 5 с), пауза 1 с и вторая, обрезанная не больше чем на 1 с.
const (
	DNSBootDeadlineMin    = 10 * time.Second
	DNSBootDeadlineMax    = 10 * time.Minute
	DNSRecheckIntervalMin = time.Minute
	DNSRecheckIntervalMax = 24 * time.Hour
)

// validateDNS — две ручки DNS установки (NTF1-P13): незаданная названа общим
// перебором, вне границы — отказ с именем ручки и границей.
func (c *Config) validateDNS(fs *findings) {
	c.checkDuration(fs, "DNSBootDeadline", c.DNSBootDeadline, DNSBootDeadlineMin, DNSBootDeadlineMax)
	c.checkDuration(fs, "DNSRecheckInterval", c.DNSRecheckInterval, DNSRecheckIntervalMin, DNSRecheckIntervalMax)
}

// validateDKIM — пара ключа и селектора DKIM (NTF1-P14, CX1-133, CX1-134):
// читается той же функцией, что у перепроверки стража ([dnscheck.LoadPair] на
// [dkimkey.OS]), из одного поколения тома. Отказ — фиксированный текст пакета
// чтения с именем ключа объекта; ручка находки — ручка той части пары, которую
// отказ называет. Содержимого ключа ни текст, ни журнал не несут.
func (c *Config) validateDKIM(fs *findings) {
	keyKnob, selKnob := KnobOfField("DKIMKeyFile"), KnobOfField("DKIMSelectorFile")
	if c.unset[keyKnob.Env] || c.unset[selKnob.Env] {
		return
	}
	empty := false
	if c.DKIMKeyFile == "" {
		fs.add(keyKnob, "значение пусто: путь файла ключа DKIM в томе объекта")
		empty = true
	}
	if c.DKIMSelectorFile == "" {
		fs.add(selKnob, "значение пусто: путь файла селектора DKIM в томе объекта")
		empty = true
	}
	if empty {
		return
	}
	if _, err := dnscheck.LoadPair(dkimkey.OS, c.DKIMKeyFile, c.DKIMSelectorFile); err != nil {
		var kerr *dkimkey.Error
		if !errors.As(err, &kerr) {
			fs.add(keyKnob, "пара DKIM не читается")
			return
		}
		k := keyKnob
		if kerr.Part == dkimkey.PartSelector {
			k = selKnob
		}
		fs.add(k, "%s", kerr.Error())
	}
}
