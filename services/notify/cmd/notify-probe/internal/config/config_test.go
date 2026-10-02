// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

// config_test.go — страж согласия флага доставки, кольца ключей и таблицы
// звена идентичности (NTF1-N08 на уровне разбора ручек).
//
// Базовое окружение одно (baseEnv); каждое отрицание меняет РОВНО ОДИН факт
// против положительного близнеца и утверждает имя своей ручки в тексте отказа.

import (
	"encoding/base64"
	"errors"
	"io/fs"
	"strings"
	"testing"
)

const (
	keyringPath = "/run/secrets/notifyprobe/keyring.json"
	notifySAN   = "spiffe://kacho.cloud/ns/kacho/sa/kacho-notify"
)

// keyringFile — файл кольца формы фундамента: активный ключ, 32 байта.
func keyringFile() []byte {
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	return []byte(`{"active":{"id":1,"key":"` + key + `"}}`)
}

// baseEnv — окружение, с которым проба стартует с включённой доставкой.
func baseEnv() map[string]string {
	return map[string]string{
		"KACHO_NOTIFYPROBE_DB_PASSWORD": "pw",
		FlagKnob:                        "true",
		KeyringKnob:                     keyringPath,
		NotifySANKnob:                   notifySAN,
	}
}

// loadWith — load над поданным окружением: теги читает envconfig из окружения
// процесса, поэтому оно выставляется t.Setenv; флаг, кольцо и SAN читает
// та же функция поданным lookup — ровно как в Load.
func loadWith(t *testing.T, env map[string]string) (Config, error) {
	t.Helper()
	for _, k := range []string{"KACHO_NOTIFYPROBE_DB_PASSWORD", FlagKnob, KeyringKnob, NotifySANKnob} {
		t.Setenv(k, "")
	}
	for k, v := range env {
		t.Setenv(k, v)
	}
	lookup := func(k string) (string, bool) {
		v, ok := env[k]
		return v, ok
	}
	readFile := func(p string) ([]byte, error) {
		if p == keyringPath {
			return keyringFile(), nil
		}
		return nil, fs.ErrNotExist
	}
	return load(lookup, readFile)
}

func without(env map[string]string, key string) map[string]string {
	out := map[string]string{}
	for k, v := range env {
		if k != key {
			out[k] = v
		}
	}
	return out
}

func with(env map[string]string, key, value string) map[string]string {
	out := map[string]string{}
	for k, v := range env {
		out[k] = v
	}
	out[key] = value
	return out
}

func requireRefusalNames(t *testing.T, err error, knob string) {
	t.Helper()
	if err == nil {
		t.Fatalf("старт принят, а обязан быть отказом с именем %s", knob)
	}
	if !strings.Contains(err.Error(), knob) {
		t.Fatalf("отказ не называет ручку %s:\n%v", knob, err)
	}
}

// NTF1-N08 — переменная флага не задана: отказ с её именем.
func TestFlagUnsetRefusesNamingTheVariable(t *testing.T) {
	_, err := loadWith(t, without(baseEnv(), FlagKnob))
	requireRefusalNames(t, err, FlagKnob)
}

// Иное написание флага — тоже отказ: false из «0» или «TRUE» неотличим от
// выключенного модуля, и разбор принимает ровно два слова.
func TestFlagForeignSpellingRefusesNamingTheVariable(t *testing.T) {
	for _, v := range []string{"", "1", "TRUE", "yes", " true"} {
		t.Run(v, func(t *testing.T) {
			_, err := loadWith(t, with(baseEnv(), FlagKnob, v))
			requireRefusalNames(t, err, FlagKnob)
		})
	}
}

// Близнец NTF1-N08: «true» и «false» принимаются, и значение доезжает.
func TestFlagTrueAndFalseAreAccepted(t *testing.T) {
	on, err := loadWith(t, baseEnv())
	if err != nil {
		t.Fatalf("true отвергнут: %v", err)
	}
	if !on.Notifications.Set() || !on.Notifications.On() {
		t.Fatalf("true разобран как %+v", on.Notifications)
	}
	if on.Keyring == nil {
		t.Fatal("при включённой доставке кольцо не собрано")
	}

	// Выключенная доставка ни кольца, ни SAN не требует: их нет в окружении.
	off, err := loadWith(t, without(without(with(baseEnv(), FlagKnob, "false"), KeyringKnob), NotifySANKnob))
	if err != nil {
		t.Fatalf("false отвергнут: %v", err)
	}
	if !off.Notifications.Set() || off.Notifications.On() {
		t.Fatalf("false разобран как %+v", off.Notifications)
	}
	if off.Keyring != nil {
		t.Fatal("при выключенной доставке кольцо прочитано — ручка, которой нет, читается")
	}
}

// Включённая доставка без кольца — отказ с именем ручки кольца.
func TestEnabledWithoutKeyringRefusesNamingTheKnob(t *testing.T) {
	_, err := loadWith(t, without(baseEnv(), KeyringKnob))
	requireRefusalNames(t, err, KeyringKnob)
}

// Включённая доставка с нечитаемым кольцом — отказ с именем ручки, без
// содержимого файла.
func TestEnabledWithUnreadableKeyringRefusesNamingTheKnob(t *testing.T) {
	_, err := loadWith(t, with(baseEnv(), KeyringKnob, "/nowhere/keyring.json"))
	requireRefusalNames(t, err, KeyringKnob)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("причина отказа потеряна: %v", err)
	}
}

// Включённая доставка без SAN notify — отказ с именем ручки SAN.
func TestEnabledWithoutNotifySANRefusesNamingTheKnob(t *testing.T) {
	_, err := loadWith(t, without(baseEnv(), NotifySANKnob))
	requireRefusalNames(t, err, NotifySANKnob)
}
