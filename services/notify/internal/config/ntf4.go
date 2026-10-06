// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

import (
	"encoding/hex"
	iofs "io/fs"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Границы ручек NTF-4 стадии S1 — таблица Р16 приёмки NTF-4 (Д11). Границы
// включительные, кроме двух строгих верхних: срок аренды одиночки строго
// меньше «застывший опрос − опрос», срок ожидания контрольного письма строго
// меньше его интервала. Зависимые границы — в validateNTF4.
const (
	FeedbackMailboxLocalPartMaxOctets = 15 // 64 − 1 − 48 (Р2)

	FeedbackPollIntervalMin = 5 * time.Second
	FeedbackPollIntervalMax = 10 * time.Minute

	// Нижняя граница застывшего опроса — большее из PollStaleMinFactor ×
	// опрос и опрос + PollStaleMinGap.
	FeedbackPollStaleMinFactor = 4
	FeedbackPollStaleMinGap    = time.Minute
	FeedbackPollStaleMaxMax    = 24 * time.Hour

	FeedbackMaxMessageBytesMin = 64 << 10
	FeedbackMaxMessageBytesMax = 25 << 20

	FeedbackMaxExpansionRatioMin = 1
	FeedbackMaxExpansionRatioMax = 100

	FeedbackCanaryIntervalMin = time.Minute
	FeedbackCanaryIntervalMax = 24 * time.Hour
	FeedbackCanaryDeadlineMin = 30 * time.Second

	// Нижняя граница аренды одиночки — большее из LeaseTTLMin и
	// LeaseTTLMinFactor × опрос.
	FeedbackLeaseTTLMin       = 30 * time.Second
	FeedbackLeaseTTLMinFactor = 2

	SentLogRetentionMin = 7 * 24 * time.Hour
	SentLogRetentionMax = 90 * 24 * time.Hour

	SuppressionHardTTLMin = time.Hour
	SuppressionHardTTLMax = 72 * time.Hour // Д11

	SuppressionSoftThresholdMin = 2
	SuppressionSoftThresholdMax = 20

	SuppressionSoftWindowMin = time.Hour
	SuppressionSoftWindowMax = 7 * 24 * time.Hour
	SuppressionSoftTTLMin    = time.Hour
	SuppressionSoftTTLMax    = 7 * 24 * time.Hour

	SuppressionSweepIntervalMin = time.Minute
	SuppressionSweepIntervalMax = 24 * time.Hour

	SecretReloadIntervalMin = 5 * time.Second
	SecretReloadIntervalMax = 10 * time.Minute

	ReputationHardBounceRateMaxMin = 0.001
	ReputationHardBounceRateMaxMax = 0.2
	ReputationComplaintRateMaxMin  = 0.0001
	ReputationComplaintRateMaxMax  = 0.05

	ReputationWindowMin = time.Hour
	ReputationWindowMax = 30 * 24 * time.Hour
)

// Ключ отпечатка адреса в каталоге [Config.AddressKeyDir] (Р15, Д23): файл
// AddressKeyFileName — материал HMAC-SHA256, записанный шестнадцатеричными
// символами, не короче [AddressKeyMinBytes] октетов — той же формы и той же
// нижней границы, что ключ сетки (Д89).
const (
	AddressKeyFileName = "addressKey"
	AddressKeyMinBytes = RecipientKeyMinBytes
)

// validateNTF4 — страж ручек NTF-4 стадии S1 (Р16): незаданная названа общим
// перебором; пустая, не той формы и вне границы — отказ с именем ручки и
// границей. Зависимая граница судится только на годных ручках, от которых
// зависит: иначе отказ одной ручки назывался бы дважды.
func (c *Config) validateNTF4(fs *findings) {
	c.validateMailboxAddr(fs)
	c.validateMailboxLocalPart(fs)
	c.validateReturnDomain(fs)

	pollOK := c.checkDuration(fs, "FeedbackPollInterval", c.FeedbackPollInterval, FeedbackPollIntervalMin, FeedbackPollIntervalMax)
	staleOK := c.validatePollStaleMax(fs, pollOK)
	c.validateLeaseTTL(fs, pollOK, staleOK)

	c.checkInt(fs, "FeedbackMaxMessageBytes", c.FeedbackMaxMessageBytes, FeedbackMaxMessageBytesMin, FeedbackMaxMessageBytesMax)
	c.checkInt(fs, "FeedbackMaxExpansionRatio", c.FeedbackMaxExpansionRatio, FeedbackMaxExpansionRatioMin, FeedbackMaxExpansionRatioMax)

	canaryOK := c.checkDuration(fs, "FeedbackCanaryInterval", c.FeedbackCanaryInterval, FeedbackCanaryIntervalMin, FeedbackCanaryIntervalMax)
	c.validateCanaryDeadline(fs, canaryOK)

	retentionOK := c.checkDuration(fs, "SentLogRetention", c.SentLogRetention, SentLogRetentionMin, SentLogRetentionMax)
	c.checkDuration(fs, "SuppressionHardTTL", c.SuppressionHardTTL, SuppressionHardTTLMin, SuppressionHardTTLMax)
	c.checkInt(fs, "SuppressionSoftThreshold", c.SuppressionSoftThreshold, SuppressionSoftThresholdMin, SuppressionSoftThresholdMax)
	c.checkDuration(fs, "SuppressionSoftWindow", c.SuppressionSoftWindow, SuppressionSoftWindowMin, SuppressionSoftWindowMax)
	c.checkDuration(fs, "SuppressionSoftTTL", c.SuppressionSoftTTL, SuppressionSoftTTLMin, SuppressionSoftTTLMax)
	c.checkDuration(fs, "SuppressionSweepInterval", c.SuppressionSweepInterval, SuppressionSweepIntervalMin, SuppressionSweepIntervalMax)
	c.checkDuration(fs, "SecretReloadInterval", c.SecretReloadInterval, SecretReloadIntervalMin, SecretReloadIntervalMax)
	c.validateAddressKeyDir(fs)

	c.checkRate(fs, "ReputationHardBounceRateMax", c.ReputationHardBounceRateMax, ReputationHardBounceRateMaxMin, ReputationHardBounceRateMaxMax)
	c.checkRate(fs, "ReputationComplaintRateMax", c.ReputationComplaintRateMax, ReputationComplaintRateMaxMin, ReputationComplaintRateMaxMax)
	windowOK := c.checkDuration(fs, "ReputationWindow", c.ReputationWindow, ReputationWindowMin, ReputationWindowMax)
	if windowOK && retentionOK && c.ReputationWindow > c.SentLogRetention {
		w, r := KnobOfField("ReputationWindow"), KnobOfField("SentLogRetention")
		fs.add(w, "окно долей %s больше срока журнала отправленного %s (%s): граница %s ≤ %s (Р12)",
			c.ReputationWindow, c.SentLogRetention, r, w.Env, r.Env)
	}
}

// checkRate судит долю по включительной границе. NaN вне любой границы.
func (c *Config) checkRate(fs *findings, field string, v, lo, hi float64) {
	k := KnobOfField(field)
	if c.unset[k.Env] {
		return
	}
	if !(v >= lo && v <= hi) {
		fs.add(k, "значение %s вне границы [%s..%s]", formatRate(v), formatRate(lo), formatRate(hi))
	}
}

func formatRate(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// validateMailboxAddr — адрес POP3 ящика: `узел:порт`, узел — имя DNS либо
// IP-литерал, порт в [1..65535].
func (c *Config) validateMailboxAddr(fs *findings) {
	k := KnobOfField("FeedbackMailboxAddr")
	if c.unset[k.Env] {
		return
	}
	if c.FeedbackMailboxAddr == "" {
		fs.add(k, "значение пусто: ожидается узел:порт ящика POP3")
		return
	}
	host, port, err := net.SplitHostPort(c.FeedbackMailboxAddr)
	if err != nil {
		fs.add(k, "адрес %q не в форме узел:порт: %v", c.FeedbackMailboxAddr, err)
		return
	}
	if !relayHostForm(host) {
		fs.add(k, "адрес %q: узел не имя DNS (RFC 1123) и не IP-литерал", c.FeedbackMailboxAddr)
		return
	}
	if _, err := parsePort(port); err != nil {
		fs.add(k, "%v", err)
	}
}

// dotAtomText — символы atext RFC 5322 без `+`: `+` отделяет в адресе возврата
// метку письма (Р2), в локальной части ящика его быть не может.
const dotAtomText = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!#$%&'*/=?^_`{|}~-"

// validateMailboxLocalPart — локальная часть адреса возврата: dot-atom без
// `+`, 1..15 октетов (Р2).
func (c *Config) validateMailboxLocalPart(fs *findings) {
	k := KnobOfField("FeedbackMailboxLocalPart")
	if c.unset[k.Env] {
		return
	}
	v := c.FeedbackMailboxLocalPart
	if v == "" {
		fs.add(k, "значение пусто")
		return
	}
	if len(v) > FeedbackMailboxLocalPartMaxOctets {
		fs.add(k, "длина %d октетов вне границы [1..%d] (%d = 64 − 1 − 48, Р2)",
			len(v), FeedbackMailboxLocalPartMaxOctets, FeedbackMailboxLocalPartMaxOctets)
		return
	}
	for _, atom := range strings.Split(v, ".") {
		if atom == "" || strings.Trim(atom, dotAtomText) != "" {
			fs.add(k, "значение %q не dot-atom без «+» (RFC 5322, Р2)", v)
			return
		}
	}
}

// validateReturnDomain — домен возврата: имя DNS по RFC 1123 из двух меток и
// больше; IP-литерал доменом не является.
func (c *Config) validateReturnDomain(fs *findings) {
	k := KnobOfField("ReturnDomain")
	if c.unset[k.Env] {
		return
	}
	v := c.ReturnDomain
	switch {
	case v == "":
		fs.add(k, "значение пусто")
	case net.ParseIP(v) != nil || !dnsNameForm(v):
		fs.add(k, "значение %q не имя DNS по RFC 1123", v)
	case strings.Count(v, ".") < 1:
		fs.add(k, "значение %q: меток меньше двух", v)
	}
}

// validatePollStaleMax — застывший опрос: от большего из 4 × опрос и опрос +
// 60s до 24h. Нижняя граница зависит от опроса и судится только на годном.
func (c *Config) validatePollStaleMax(fs *findings, pollOK bool) bool {
	k := KnobOfField("FeedbackPollStaleMax")
	if c.unset[k.Env] {
		return false
	}
	v := c.FeedbackPollStaleMax
	if !pollOK {
		if v > FeedbackPollStaleMaxMax {
			fs.add(k, "значение %s выше границы %s", v, FeedbackPollStaleMaxMax)
		}
		return false
	}
	p := c.FeedbackPollInterval
	lo := max(FeedbackPollStaleMinFactor*p, p+FeedbackPollStaleMinGap)
	if v < lo || v > FeedbackPollStaleMaxMax {
		fs.add(k, "значение %s вне границы [%s..%s]: нижняя — большее из %d × %s и %s + %s",
			v, lo, FeedbackPollStaleMaxMax, FeedbackPollStaleMinFactor, KnobOfField("FeedbackPollInterval").Env,
			KnobOfField("FeedbackPollInterval").Env, FeedbackPollStaleMinGap)
		return false
	}
	return true
}

// validateLeaseTTL — аренда одиночки опроса (Р21): от большего из 30s и 2 ×
// опрос, строго меньше «застывший опрос − опрос». Зависимые части судятся
// только на годных ручках опроса.
func (c *Config) validateLeaseTTL(fs *findings, pollOK, staleOK bool) {
	k := KnobOfField("FeedbackLeaseTTL")
	if c.unset[k.Env] {
		return
	}
	v := c.FeedbackLeaseTTL
	lo := FeedbackLeaseTTLMin
	if pollOK {
		lo = max(lo, FeedbackLeaseTTLMinFactor*c.FeedbackPollInterval)
	}
	if !pollOK || !staleOK {
		if v < lo {
			fs.add(k, "значение %s ниже границы %s", v, lo)
		}
		return
	}
	hi := c.FeedbackPollStaleMax - c.FeedbackPollInterval
	if v < lo || v >= hi {
		fs.add(k, "значение %s вне границы [%s..%s): верхняя строго меньше %s − %s",
			v, lo, hi, KnobOfField("FeedbackPollStaleMax").Env, KnobOfField("FeedbackPollInterval").Env)
	}
}

// validateCanaryDeadline — срок ожидания контрольного письма: от 30s, строго
// меньше интервала контрольного письма (судится только на годном интервале).
func (c *Config) validateCanaryDeadline(fs *findings, intervalOK bool) {
	k := KnobOfField("FeedbackCanaryDeadline")
	if c.unset[k.Env] {
		return
	}
	v := c.FeedbackCanaryDeadline
	if !intervalOK {
		if v < FeedbackCanaryDeadlineMin {
			fs.add(k, "значение %s ниже границы %s", v, FeedbackCanaryDeadlineMin)
		}
		return
	}
	if v < FeedbackCanaryDeadlineMin || v >= c.FeedbackCanaryInterval {
		fs.add(k, "значение %s вне границы [%s..%s): верхняя строго меньше %s",
			v, FeedbackCanaryDeadlineMin, c.FeedbackCanaryInterval, KnobOfField("FeedbackCanaryInterval").Env)
	}
}

// validateAddressKeyDir — каталог ключа отпечатка (Р15, Д23): абсолютный путь;
// файл [AddressKeyFileName] в нём читается при старте и разбирается. Ни
// значение ключа, ни его длина в текст отказа не попадают.
func (c *Config) validateAddressKeyDir(fs *findings) {
	k := KnobOfField("AddressKeyDir")
	if c.unset[k.Env] {
		return
	}
	dir := c.AddressKeyDir
	switch {
	case dir == "":
		fs.add(k, "значение пусто: ожидается абсолютный путь каталога ключа отпечатка")
		return
	case !filepath.IsAbs(dir):
		fs.add(k, "путь %q не абсолютный", dir)
		return
	}
	raw, err := iofs.ReadFile(os.DirFS(dir), AddressKeyFileName)
	if err != nil {
		fs.add(k, "файл ключа отпечатка %s в каталоге %q не читается", AddressKeyFileName, dir)
		return
	}
	key, err := hex.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil || len(key) < AddressKeyMinBytes {
		fs.add(k, "ключ отпечатка %s в каталоге %q не разбирается: ожидается материал HMAC-SHA256 "+
			"шестнадцатеричными символами не короче %d октетов", AddressKeyFileName, dir, AddressKeyMinBytes)
	}
}
