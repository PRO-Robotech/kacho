// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config_test

// dkimfixture_test.go — фикстура пары DKIM для проб загрузчика (полоса N14,
// §12а «Ключ и селектор DKIM», NTF1-P14).
//
// Каталог пары — форма тома kubelet без `subPath`: `..<поколение>/` с файлами,
// `..data` → `..<поколение>`, `<имя>` → `..data/<имя>`. Загрузчик читает пару
// из одного поколения (CX1-134), поэтому каталог другой формы фикстурой быть
// не может: проба, кладущая файлы плоско, зеленила бы снисходительность,
// которой у тома нет.

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

const (
	envDKIMKeyFile      = "KACHO_NOTIFY_DKIM_KEY_FILE"
	envDKIMSelectorFile = "KACHO_NOTIFY_DKIM_SELECTOR_FILE"

	// Имена ключей объекта (`privateKeyKey`, `selectorKey`) — имена файлов тома.
	dkimKeyName      = "dkim.key"
	dkimSelectorName = "dkim.selector"
)

var (
	fixtureKeyOnce sync.Once
	fixtureKeyPEM  []byte
	fixtureKeyErr  error
)

// fixtureDKIMKeyPEM — исправный ключ фикстуры: RSA 2048, PKCS#1. Выпускается
// один раз на прогон пакета — выпуск RSA дорог, а ключ в пробах не меняется.
func fixtureDKIMKeyPEM(t *testing.T) []byte {
	t.Helper()
	fixtureKeyOnce.Do(func() {
		k, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			fixtureKeyErr = err
			return
		}
		fixtureKeyPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)})
	})
	if fixtureKeyErr != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: ключ DKIM фикстуры не выпущен: %v", fixtureKeyErr)
	}
	return fixtureKeyPEM
}

// rsaKeyPEM — ключ RSA заданной длины; pkcs8 — форма PKCS#8 вместо PKCS#1.
func rsaKeyPEM(t *testing.T, bits int, pkcs8 bool) []byte {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: ключ RSA %d не выпущен: %v", bits, err)
	}
	if !pkcs8 {
		return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)})
	}
	der, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: PKCS#8: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

// ecKeyPEM — ключ не RSA (ECDSA P-256, PKCS#8).
func ecKeyPEM(t *testing.T) []byte {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: ключ ECDSA не выпущен: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: PKCS#8: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

// kubeletDir раскладывает файлы в каталог формы тома kubelet и возвращает его.
// Файл, которого нет в files, в томе отсутствует вовсе — ни в поколении, ни
// ссылкой: так выглядит ключ, которого нет в объекте.
func kubeletDir(t *testing.T, files map[string][]byte) string {
	t.Helper()
	dir := t.TempDir()
	gen := "..2026_10_05_00_00_00.000000001"
	if err := os.Mkdir(filepath.Join(dir, gen), 0o755); err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: каталог поколения: %v", err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, gen, name), body, 0o600); err != nil {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: файл %s: %v", name, err)
		}
		if err := os.Symlink(filepath.Join("..data", name), filepath.Join(dir, name)); err != nil {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: ссылка %s: %v", name, err)
		}
	}
	if err := os.Symlink(gen, filepath.Join(dir, "..data")); err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: ссылка ..data: %v", err)
	}
	return dir
}

// dkimEdits — правки фикстуры, ставящие пару из каталога формы kubelet.
// Отсутствующий в files ключ получает путь в том же каталоге — файла по нему нет.
func dkimEdits(t *testing.T, files map[string][]byte) map[string]*string {
	t.Helper()
	dir := kubeletDir(t, files)
	return map[string]*string{
		envDKIMKeyFile:      str(filepath.Join(dir, dkimKeyName)),
		envDKIMSelectorFile: str(filepath.Join(dir, dkimSelectorName)),
	}
}
