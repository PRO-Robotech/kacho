// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config_test

// dkim_key_test.go — загрузчик ключа и селектора DKIM (полоса N14; приёмка
// NTF-1 NTF1-P14; замысел §12а «Ключ и селектор DKIM», CX1-133, CX1-134,
// CX1-138 (б)).
//
// Семь причин P14 — по пробе на каждую. Каждая меняет ровно один факт пары
// фикстуры (исправный RSA 2048 и селектор `mail`) и требует ровно одну находку:
// по ручке ключа — для причин ключа, по ручке селектора — для причин селектора.
// Текст называет ключ объекта (имя файла тома) и причину фиксированным словом.
// Посеянная строка-маркер ни в текст отказа, ни в журнал процесса не попадает.

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// startCapturingLog — старт с перехваченным журналом процесса: всё, что
// загрузчик и страж пишут в журнал по умолчанию, попадает в буфер.
func startCapturingLog(t *testing.T) (string, error) {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)
	err := start(t)
	return buf.String(), err
}

// NTF1-P14 — ключ или селектор DKIM негодны: отказ старта без содержимого ключа.
func TestNTF1P14DKIMKeyOrSelectorUnfitRefusesStartWithoutKeyBytes(t *testing.T) {
	const marker = "DKIM-KEY-MARKER-5c1e0b"
	broken := []byte("-----BEGIN RSA PRIVATE KEY-----\n" + marker + "\n-----END RSA PRIVATE KEY-----\n")
	sel := []byte("mail")

	cases := []struct {
		name  string
		files func(t *testing.T) map[string][]byte
		env   string
		why   []string
	}{
		{"ключ — RSA 1024", func(t *testing.T) map[string][]byte {
			return map[string][]byte{dkimKeyName: rsaKeyPEM(t, 1024, false), dkimSelectorName: sel}
		}, envDKIMKeyFile, []string{dkimKeyName, "короче 2048 бит"}},
		{"ключ — не RSA", func(t *testing.T) map[string][]byte {
			return map[string][]byte{dkimKeyName: ecKeyPEM(t), dkimSelectorName: sel}
		}, envDKIMKeyFile, []string{dkimKeyName, "не RSA"}},
		{"ключ не разбирается и несёт маркер", func(t *testing.T) map[string][]byte {
			return map[string][]byte{dkimKeyName: broken, dkimSelectorName: sel}
		}, envDKIMKeyFile, []string{dkimKeyName, "не читается"}},
		{"ключа нет в объекте", func(t *testing.T) map[string][]byte {
			return map[string][]byte{dkimSelectorName: sel}
		}, envDKIMKeyFile, []string{dkimKeyName, "нет в объекте"}},
		{"селектора нет в объекте", func(t *testing.T) map[string][]byte {
			return map[string][]byte{dkimKeyName: fixtureDKIMKeyPEM(t)}
		}, envDKIMSelectorFile, []string{dkimSelectorName, "нет в объекте"}},
		{"селектор пуст", func(t *testing.T) map[string][]byte {
			return map[string][]byte{dkimKeyName: fixtureDKIMKeyPEM(t), dkimSelectorName: []byte("")}
		}, envDKIMSelectorFile, []string{dkimSelectorName, "пуст"}},
		{"селектор вне формы имени DNS (sel_1)", func(t *testing.T) map[string][]byte {
			return map[string][]byte{dkimKeyName: fixtureDKIMKeyPEM(t), dkimSelectorName: []byte("sel_1")}
		}, envDKIMSelectorFile, []string{dkimSelectorName, "вне формы имени DNS"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			useFixture(t, dkimEdits(t, c.files(t)))
			log, err := startCapturingLog(t)
			r := requireOnlyRefusalEnv(t, err, c.env, c.why...)
			if strings.Contains(r.Error(), marker) || strings.Contains(log, marker) {
				t.Fatalf("строка-маркер ключа попала в текст отказа или журнал:\nотказ: %v\nжурнал: %s", r, log)
			}
			if strings.Contains(r.Error(), "PRIVATE KEY") || strings.Contains(log, "PRIVATE KEY") {
				t.Fatalf("байты ключа попали в текст отказа или журнал:\nотказ: %v\nжурнал: %s", r, log)
			}
		})
	}

	// Ручки пары без умолчания: незаданная — отказ с её именем.
	for _, env := range []string{envDKIMKeyFile, envDKIMSelectorFile} {
		t.Run("ручка "+env+" не задана", func(t *testing.T) {
			edits := dkimEdits(t, map[string][]byte{dkimKeyName: fixtureDKIMKeyPEM(t), dkimSelectorName: sel})
			edits[env] = nil
			useFixture(t, edits)
			requireOnlyRefusalEnv(t, start(t), env, "не задана")
		})
	}

	// Близнецы: RSA 2048 в обеих формах и селекторы законной формы — приняты.
	twins := []struct {
		name string
		key  func(t *testing.T) []byte
		sel  string
	}{
		{"RSA 2048 PKCS#1, селектор mail", fixtureDKIMKeyPEM, "mail"},
		{"RSA 2048 PKCS#8, селектор mail", func(t *testing.T) []byte { return rsaKeyPEM(t, 2048, true) }, "mail"},
		{"RSA 2048, многоуровневый селектор 2026.mail", fixtureDKIMKeyPEM, "2026.mail"},
	}
	for _, c := range twins {
		t.Run("близнец: "+c.name, func(t *testing.T) {
			useFixture(t, dkimEdits(t, map[string][]byte{dkimKeyName: c.key(t), dkimSelectorName: []byte(c.sel)}))
			if err := start(t); err != nil {
				t.Fatalf("исправная пара DKIM отвергнута: %v", err)
			}
		})
	}
}

// CX1-134 (г) — пути ключа и селектора в разных каталогах тома: пару нельзя
// прочитать из одного поколения — отказ с именем ручки селектора.
func TestDKIMPairPathsInDifferentDirectoriesRefuseStart(t *testing.T) {
	keyDir := kubeletDir(t, map[string][]byte{dkimKeyName: fixtureDKIMKeyPEM(t)})
	selDir := kubeletDir(t, map[string][]byte{dkimSelectorName: []byte("mail")})
	useFixture(t, map[string]*string{
		envDKIMKeyFile:      str(filepath.Join(keyDir, dkimKeyName)),
		envDKIMSelectorFile: str(filepath.Join(selDir, dkimSelectorName)),
	})
	requireOnlyRefusalEnv(t, start(t), envDKIMSelectorFile, envDKIMKeyFile)

	t.Run("близнец: тот же каталог", func(t *testing.T) {
		useFixture(t, dkimEdits(t, map[string][]byte{dkimKeyName: fixtureDKIMKeyPEM(t), dkimSelectorName: []byte("mail")}))
		if err := start(t); err != nil {
			t.Fatalf("пара в одном каталоге тома отвергнута: %v", err)
		}
	})
}

// CX1-134 (г) — файл вне каталога формы kubelet (нет `..data`) — отказ: обойти
// чтение из одного поколения фикстура не может.
func TestDKIMPairOutsideAKubeletVolumeRefusesStart(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string][]byte{dkimKeyName: fixtureDKIMKeyPEM(t), dkimSelectorName: []byte("mail")} {
		if err := os.WriteFile(filepath.Join(dir, name), body, 0o600); err != nil {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: файл %s: %v", name, err)
		}
	}
	useFixture(t, map[string]*string{
		envDKIMKeyFile:      str(filepath.Join(dir, dkimKeyName)),
		envDKIMSelectorFile: str(filepath.Join(dir, dkimSelectorName)),
	})
	requireOnlyRefusalEnv(t, start(t), envDKIMKeyFile, dkimKeyName, "не читается")
}
