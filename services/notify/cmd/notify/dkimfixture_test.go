// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

var (
	dkimKeyOnce sync.Once
	dkimKeyPEM  []byte
	dkimKeyErr  error
)

// dkimFiles выпускает пару DKIM пробы (RSA 2048, селектор `mail`) в каталоге
// формы тома kubelet (`..data` → поколение, файлы — ссылки через `..data`) и
// подставляет пути в окружение: KACHO_NOTIFY_DKIM_KEY_FILE,
// KACHO_NOTIFY_DKIM_SELECTOR_FILE (замысел §8, §12а).
func dkimFiles(t *testing.T, env map[string]string) {
	t.Helper()
	dkimKeyOnce.Do(func() {
		k, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			dkimKeyErr = err
			return
		}
		dkimKeyPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)})
	})
	if dkimKeyErr != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: ключ DKIM пробы не выпущен: %v", dkimKeyErr)
	}
	dir := t.TempDir()
	gen := "..2026_10_05_00_00_00.000000001"
	if err := os.Mkdir(filepath.Join(dir, gen), 0o755); err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: поколение тома DKIM: %v", err)
	}
	files := map[string][]byte{"dkim.key": dkimKeyPEM, "dkim.selector": []byte("mail")}
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
	env["KACHO_NOTIFY_DKIM_KEY_FILE"] = filepath.Join(dir, "dkim.key")
	env["KACHO_NOTIFY_DKIM_SELECTOR_FILE"] = filepath.Join(dir, "dkim.selector")
}
