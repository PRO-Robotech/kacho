// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// checkviolationtone_injection_test.go — доказательство, что гейт тона умеет
// краснеть И умеет молчать.
//
// Гейт, проверенный только на «зелено», неотличим от гейта, который ничего не
// читает; проверенный только на «краснеет» — от того, который краснеет на всём.
// Обе стороны нужны потому, что дискриминатор здесь нетривиален: гейт обязан
// отличать ПРОИЗВОДСТВО фразы (строковый литерал, уезжающий вызывающему) от
// её УПОМИНАНИЯ (комментарий, объясняющий эту же защиту, — в том числе шапки
// самих отображений и этого файла).
package repohygiene

import (
	"strings"
	"testing"
)

func TestCheckViolationToneDiscriminatorSeparatesCodeFromProse(t *testing.T) {
	cases := []struct {
		name  string
		src   string
		found bool
	}{
		{
			name: "ЛОВИТСЯ: фраза уезжает вызывающему строковым литералом",
			src: `package x
import "fmt"
func f(kind string) error { return fmt.Errorf("%s violates check constraint", kind) }`,
			found: true,
		},
		{
			name: "ЛОВИТСЯ: та же фраза в другом регистре",
			src: `package x
func f() string { return "Address VIOLATES CHECK CONSTRAINT" }`,
			found: true,
		},
		{
			name: "МОЛЧИТ: фраза только в комментарии, объясняющем защиту",
			src: `package x
// Прежняя редакция отдавала «violates check constraint» дословно — формулировку
// Postgres, а не контракт продукта.
func f() string { return "Illegal argument" }`,
			found: false,
		},
		{
			name: "МОЛЧИТ: фраза в godoc отображения",
			src: `package x
// wrapCheckViolation: текст «violates check constraint» наружу не идёт.
func wrapCheckViolation() string { return "Illegal argument" }`,
			found: false,
		},
		{
			name: "МОЛЧИТ: соседние слова порознь — не фраза",
			src: `package x
func f() string { return "constraint check failed" }`,
			found: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got bool
			for _, lit := range stringLiterals(t, "synthetic.go", []byte(c.src)) {
				if strings.Contains(strings.ToLower(lit.value), dbTonePhrase) {
					got = true
				}
			}
			if got != c.found {
				t.Fatalf("дискриминатор ответил %v, ожидалось %v", got, c.found)
			}
		})
	}
}
