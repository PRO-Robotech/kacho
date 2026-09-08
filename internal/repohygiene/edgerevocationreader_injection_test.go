// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// edgerevocationreader_injection_test.go — опыт: способен ли гейт полосы отзыва
// у края упасть и способен ли он смолчать.
//
// Инъекция идёт по КАЖДОЙ оси отдельно и меняет РОВНО ОДИН факт против
// законного близнеца. Одна общая проба «сломай что-нибудь» зеленела бы на
// гейте, у которого работает лишь одна ось из трёх.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// liveEdgeRevocationFacts — законный близнец: всё на месте.
func liveEdgeRevocationFacts() edgeRevocationFacts {
	return edgeRevocationFacts{
		ClientDeclares: true,
		CallerFiles:    []string{"gateway/internal/middleware/revocation_local.go"},
		PortFiles:      []string{"gateway/internal/middleware/revocation_local.go"},
	}
}

func TestEdgeRevocationGate_SilentOnALiveLane(t *testing.T) {
	t.Parallel()
	if found := auditEdgeRevocationReader(liveEdgeRevocationFacts()); len(found) != 0 {
		t.Fatalf("законный близнец объявлен находкой: %s", strings.Join(found, "; "))
	}
}

func TestEdgeRevocationGate_FindsAMissingDeclaration(t *testing.T) {
	t.Parallel()
	f := liveEdgeRevocationFacts()
	f.ClientDeclares = false
	found := auditEdgeRevocationReader(f)
	if len(found) != 1 || !strings.Contains(found[0], "не объявляет") {
		t.Fatalf("пропавшее объявление не названо ровно одной находкой: %v", found)
	}
}

func TestEdgeRevocationGate_FindsADeadReader(t *testing.T) {
	t.Parallel()
	f := liveEdgeRevocationFacts()
	f.CallerFiles = nil
	found := auditEdgeRevocationReader(f)
	if len(found) != 1 || !strings.Contains(found[0], "вызывающего") {
		t.Fatalf("читатель без вызывающего не назван ровно одной находкой: %v", found)
	}
}

func TestEdgeRevocationGate_FindsAMissingPort(t *testing.T) {
	t.Parallel()
	f := liveEdgeRevocationFacts()
	f.PortFiles = nil
	found := auditEdgeRevocationReader(f)
	if len(found) != 1 || !strings.Contains(found[0], "портом") {
		t.Fatalf("пропавший порт не назван ровно одной находкой: %v", found)
	}
}

// TestEdgeRevocationGate_ScanIgnoresTheClientCallingItself — разбор не
// засчитывает вызывающим сам клиент.
//
// Ось несущая: без неё гейт зеленел бы на полосе, где метод объявлен и зовётся
// ТОЛЬКО из своего же файла, — то есть на мёртвом контроле.
func TestEdgeRevocationGate_ScanIgnoresTheClientCallingItself(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatalf("каталог %s: %v", rel, err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatalf("запись %s: %v", rel, err)
		}
	}
	// Клиент объявляет метод И зовёт его сам. Больше в дереве никого.
	write(edgeRevocationClientRel, `package clients

type SessionRevocationsAdapter struct{}

// Комментарий НАМЕРЕННО называет IsSessionRevoked: предикат по подстроке
// зеленел бы на этой строке, разбор по узлам — нет.
func (a *SessionRevocationsAdapter) IsSessionRevoked(jti string) bool { return false }

func (a *SessionRevocationsAdapter) other(jti string) bool { return a.IsSessionRevoked(jti) }
`)
	tt := &trackedTree{files: map[string]bool{edgeRevocationClientRel: true}, root: root}
	f, scanned, parsd := edgeRevocationScan(t, root, tt)
	if scanned != 1 || parsd != 1 {
		t.Fatalf("обход синтетики: осмотрено %d, разобрано %d — ожидался один файл", scanned, parsd)
	}
	if !f.ClientDeclares {
		t.Fatal("разбор не увидел объявления метода у клиента — предикат меряет не то")
	}
	if len(f.CallerFiles) != 0 {
		t.Fatalf("сам клиент засчитан вызывающим: %v — мёртвый контроль остался бы зелёным",
			f.CallerFiles)
	}
	found := auditEdgeRevocationReader(f)
	if !strings.Contains(strings.Join(found, "\n"), "вызывающего") {
		t.Fatalf("мёртвый контроль не назван находкой: %v", found)
	}
}

// TestEdgeRevocationGate_ScanSeesALegitimateCallerAndPort — зеркало предыдущей:
// законный вызывающий и порт находятся тем же разбором.
//
// Без него предыдущая проба зеленела бы и на разборе, который не находит НИЧЕГО.
func TestEdgeRevocationGate_ScanSeesALegitimateCallerAndPort(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatalf("каталог %s: %v", rel, err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatalf("запись %s: %v", rel, err)
		}
	}
	write(edgeRevocationClientRel, `package clients

type SessionRevocationsAdapter struct{}

func (a *SessionRevocationsAdapter) IsSessionRevoked(jti string) bool { return false }
`)
	const readerRel = "gateway/internal/middleware/revocation_local.go"
	write(readerRel, `package middleware

type localAuthority interface {
	IsSessionRevoked(jti string) bool
}

type checker struct{ local localAuthority }

func (c *checker) ask(jti string) bool { return c.local.IsSessionRevoked(jti) }
`)
	tt := &trackedTree{files: map[string]bool{edgeRevocationClientRel: true, readerRel: true}, root: root}
	f, _, _ := edgeRevocationScan(t, root, tt)
	if len(f.CallerFiles) != 1 || f.CallerFiles[0] != readerRel {
		t.Fatalf("законный вызывающий не найден: %v", f.CallerFiles)
	}
	if len(f.PortFiles) != 1 || f.PortFiles[0] != readerRel {
		t.Fatalf("законный порт не найден: %v", f.PortFiles)
	}
	if found := auditEdgeRevocationReader(f); len(found) != 0 {
		t.Fatalf("законный близнец объявлен находкой: %v", found)
	}
}
