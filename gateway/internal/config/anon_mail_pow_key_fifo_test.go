// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build unix

package config

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Не обычный файл — вне набора. FIFO: чтение без писателя ждёт вечно, и
// страж старта висел бы вместо отказа. Проба ограничена сроком: зависание —
// красное, а не повисший прогон.
func TestAnonMailPoWKeyPathFIFOIsRefusedWithoutBlocking(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "key")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatalf("FIFO не создан — условие пробы не создано: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := ReadAnonMailPoWKey(Config{AnonMailPoWKeyFile: fifo})
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), AnonMailPoWKeyFileKnob) ||
			!strings.Contains(err.Error(), "не обычный файл") {
			t.Fatalf("FIFO: ждали отказ «не обычный файл» с именем ручки, получено %v", err)
		}
	case <-time.After(2 * time.Second):
		// Освободить зависшее чтение: открыть FIFO на запись и закрыть.
		if w, err := os.OpenFile(fifo, os.O_WRONLY, 0); err == nil {
			_ = w.Close()
		}
		<-done
		t.Fatal("FIFO: страж старта завис на чтении вместо отказа")
	}
}
