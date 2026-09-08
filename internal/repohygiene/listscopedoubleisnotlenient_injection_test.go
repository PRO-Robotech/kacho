// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLenientScopeAudit_Injection — доказательство того, что проверка выше
// СПОСОБНА упасть и способна смолчать. Обе стороны обязательны: отрицание без
// положительного контроля зеленеет на всём сломанном, а положительный без
// отрицания — на всём исправном.
//
// Инъекция зовёт ТУ ЖЕ функцию, что и проверка дерева, а не свою копию: иначе
// доказывала бы свойство копии.
func TestLenientScopeAudit_Injection(t *testing.T) {
	const lenientDouble = `package p

import "testing"

type v struct{}

func (v) ScopeOf() visibility.Scope {
	return visibility.Scope{Unrestricted: true}
}

func TestX(t *testing.T) {}
`
	const restrictedDouble = `package p

import "testing"

func TestY(t *testing.T) {
	_ = visibility.Scope{ScopedAccounts: []string{"acc"}}
}
`
	const holderRefComment = `package p

// Отбор проверяется на настоящей базе:
// services/iam/internal/apps/kaname/api/holder — там снисходительного дублёра нет.

import "testing"

func TestZ(t *testing.T) {}
`

	cases := []struct {
		name    string
		pkgs    map[string]map[string]string // пакет → файл → содержимое
		wantErr bool                         // ждём отказ предпосылки
		wantHit string                       // подстрока находки; пусто = молчит
	}{
		{
			name: "снисходительный называет живого держателя — молчит",
			pkgs: map[string]map[string]string{
				"usecase": {"a_test.go": lenientDouble, "b_test.go": holderRefComment},
				"holder":  {"h_test.go": restrictedDouble},
			},
		},
		{
			name: "держатель назван, но проб не несёт — краснеет ЕГО именем",
			pkgs: map[string]map[string]string{
				"usecase": {"a_test.go": lenientDouble, "b_test.go": holderRefComment},
				"holder":  {"readme.txt": "не проба"},
			},
			wantHit: "holder",
		},
		{
			name: "снисходительный не называет держателя — краснеет своим именем",
			pkgs: map[string]map[string]string{
				"usecase": {"a_test.go": lenientDouble},
				"holder":  {"h_test.go": restrictedDouble},
			},
			wantHit: "usecase",
		},
		{
			name: "снисходительных нет вовсе — предпосылка отказывает, а не зеленеет",
			pkgs: map[string]map[string]string{
				"usecase": {"a_test.go": restrictedDouble},
			},
			wantErr: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			for pkg, files := range c.pkgs {
				dir := filepath.Join(root, pkg)
				if err := os.MkdirAll(dir, 0o750); err != nil {
					t.Fatalf("подготовка %s: %v", pkg, err)
				}
				for name, body := range files {
					if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
						t.Fatalf("запись %s/%s: %v", pkg, name, err)
					}
				}
			}

			census, findings, err := auditLenientScopes(root)
			if c.wantErr {
				if err == nil {
					t.Fatalf("предпосылка обязана была отказать, а проверка вышла зелёной; перепись: %s", census)
				}
				return
			}
			if err != nil {
				t.Fatalf("неожиданный отказ предпосылки: %v", err)
			}
			if c.wantHit == "" {
				if len(findings) != 0 {
					t.Fatalf("законный случай признан находкой: %v", findings)
				}
				return
			}
			if len(findings) == 0 {
				t.Fatalf("дефект не найден — проверка неспособна упасть; перепись: %s", census)
			}
			var named bool
			for _, f := range findings {
				if strings.Contains(f, c.wantHit) {
					named = true
				}
			}
			if !named {
				t.Fatalf("находка есть, но виновник не назван (%q ожидалось): %v", c.wantHit, findings)
			}
		})
	}
}
