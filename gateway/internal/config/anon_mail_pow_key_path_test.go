// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Пробы пути к файлу ключа подписи вызовов (З9): страж старта читает ключ
// только по АБСОЛЮТНОМУ пути в КАНОНИЧЕСКОЙ форме и только из ОБЫЧНОГО файла.
// Каждый отрицательный кейс меняет ровно один факт против законного близнеца —
// абсолютного канонического пути к тому же 32-байтовому ключу, который принят.

// writePoWKey кладёт законный 32-байтовый ключ в dir/name и возвращает путь.
func writePoWKey(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, bytes.Repeat([]byte{'k'}, anonMailPoWKeyMinBytes), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// refusedNamingKnob — отказ старта, называющий ручку и признак отказа.
func refusedNamingKnob(t *testing.T, name, path, sign string) {
	t.Helper()
	key, err := ReadAnonMailPoWKey(Config{AnonMailPoWKeyFile: path})
	if err == nil {
		t.Fatalf("%s: путь %q принят, ключ %d байт; ждали отказ старта", name, path, len(key.Bytes()))
	}
	if !strings.Contains(err.Error(), AnonMailPoWKeyFileKnob) || !strings.Contains(err.Error(), sign) {
		t.Fatalf("%s: отказ не называет ручку и признак %q: %v", name, sign, err)
	}
}

// Законный близнец: абсолютный канонический путь к обычному файлу принят.
func TestAnonMailPoWKeyPathTwinIsAccepted(t *testing.T) {
	dir := t.TempDir()
	p := writePoWKey(t, dir, "key")
	key, err := ReadAnonMailPoWKey(Config{AnonMailPoWKeyFile: p})
	if err != nil {
		t.Fatalf("законный путь %q отвергнут: %v", p, err)
	}
	if len(key.Bytes()) != anonMailPoWKeyMinBytes {
		t.Fatalf("ключ прочитан длиной %d", len(key.Bytes()))
	}
}

// Относительный путь — вне набора: разрешается от рабочего каталога процесса,
// то есть читает то, что окажется рядом при запуске. Ключ по нему ЛЕЖИТ и
// законен — меняется только форма пути.
func TestAnonMailPoWKeyPathRelativeIsRefused(t *testing.T) {
	dir := t.TempDir()
	writePoWKey(t, dir, "key")
	t.Chdir(dir)
	refusedNamingKnob(t, "относительный путь", "key", "не абсолютный")
}

// Неканоническая форма (`..`, двойной разделитель, хвостовой разделитель) —
// вне набора: путь, который назвал оператор, и путь, который прочитан, обязаны
// совпадать буква в букву. Файл по каждому из путей существует и законен.
func TestAnonMailPoWKeyPathNonCanonicalIsRefused(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	writePoWKey(t, dir, "key")
	for name, path := range map[string]string{
		"через ..":            dir + "/sub/../key",
		"двойной разделитель": dir + "//key",
		"через .":             dir + "/./key",
	} {
		refusedNamingKnob(t, name, path, "не в канонической форме")
	}
}

// Каталог — вне набора, и отказ называет признак, а не текст системного вызова.
func TestAnonMailPoWKeyPathDirectoryIsRefused(t *testing.T) {
	refusedNamingKnob(t, "каталог", t.TempDir(), "не обычный файл")
}
