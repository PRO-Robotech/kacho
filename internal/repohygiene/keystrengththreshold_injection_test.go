// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// keystrengththreshold_injection_test.go — доказательство того, что
// TestKeyStrengthFloorIsDeclaredExactlyOnce СПОСОБЕН упасть, и падает он на
// существе.
//
// Инъекция гоняет ТУ ЖЕ функцию разбора (ScanKeyStrengthThresholds), что и гейт.
//
// Пара обязательна в ОБЕ стороны, и вторая сторона здесь тяжелее первой: числа
// вокруг ключей встречаются постоянно, и почти все они законны. Гейт, который
// ловит число, был бы снят первым же прогоном, поэтому законных близнецов тут
// шесть, и каждый закрывает свой способ ошибиться.
package repohygiene

import (
	"fmt"
	"strings"
	"testing"
)

// keyStrengthInjectedSecondFloor — ВТОРОЕ объявление порога, в обеих формах:
// стойкость слева и стойкость справа.
const keyStrengthInjectedSecondFloor = `package keystore

func admit(k *rsa.PublicKey, keyBits int) error {
	if k.N.BitLen() < 2048 {
		return errWeak
	}
	if 256 > keyBits {
		return errWeak
	}
	return nil
}
`

// keyStrengthInjectedLegitimate — те же места БЕЗ второго объявления, вместе с
// законными соседями.
//
// Соседи, каждый со своим способом обмануть разбор по числу:
//
//   - `bits < min` — ЧИТАТЕЛЬ объявленного порога, а не второе объявление;
//   - `return 256` — сообщение ИЗМЕРЕННОЙ стойкости, а не решение о приёме;
//   - `KeySize = 32` — размер ключа обёртки: число рядом с ключами, порогом не
//     являющееся;
//   - `len(key) != 32` — сравнение с числом, где сравнивают не стойкость;
//   - `bodyLen > 4096` — число ИЗ словаря стойкости, но предмет другой;
//   - `submitsCount < 2048` — слово, содержащее «bits» ПОДСТРОКОЙ. Разбор по
//     подстроке объявил бы это порогом; разбор по словам — нет.
const keyStrengthInjectedLegitimate = `package keystore

const KeySize = 32

func admit(alg Algorithm, bits int, key []byte, bodyLen, submitsCount int) error {
	if min := alg.MinBits(); bits < min {
		return errWeak
	}
	if len(key) != 32 {
		return errKeySize
	}
	if bodyLen > 4096 {
		return errTooLarge
	}
	if submitsCount < 2048 {
		return errQuota
	}
	return nil
}

func strengthOf(pub any) int {
	return 256
}
`

// TestKeyStrengthScannerFailsOnASecondFloor — сторона (а).
func TestKeyStrengthScannerFailsOnASecondFloor(t *testing.T) {
	_, compares, census, err := ScanKeyStrengthThresholds(
		"synthetic/keystore.go", []byte(keyStrengthInjectedSecondFloor), keyStrengthDeclName)
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if census.Comparisons < 2 {
		t.Fatalf("осмотрено сравнений %d — разбирается не то дерево", census.Comparisons)
	}
	if len(compares) != 2 {
		t.Fatalf("вторых объявлений найдено %d, ожидалось 2 (стойкость слева и справа): %+v",
			len(compares), compares)
	}
	for _, c := range compares {
		if c.Line == 0 || c.File == "" || c.Expr == "" {
			t.Errorf("находка без координаты — по такому отказу нечего чинить: %+v", c)
		}
	}
	if compares[0].Literal != 2048 || !strings.Contains(compares[0].Expr, "BitLen") {
		t.Errorf("первая находка описана неверно: %+v", compares[0])
	}
	if compares[1].Literal != 256 || compares[1].Expr != "keyBits" {
		t.Errorf("форма «литерал слева, стойкость справа» опознана неверно: %+v", compares[1])
	}
}

// TestKeyStrengthScannerIsSilentOnLegitimateNumbersNearKeys — сторона (б).
func TestKeyStrengthScannerIsSilentOnLegitimateNumbersNearKeys(t *testing.T) {
	_, compares, census, err := ScanKeyStrengthThresholds(
		"synthetic/keystore.go", []byte(keyStrengthInjectedLegitimate), keyStrengthDeclName)
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	// Сравнений здесь ТРИ, а не четыре: `len(key) != 32` разбором не
	// рассматривается вовсе — неравенство порогом не бывает.
	if census.Comparisons < 3 {
		t.Fatalf("осмотрено сравнений %d — разбирается не то дерево", census.Comparisons)
	}
	if len(compares) != 0 {
		var got []string
		for _, c := range compares {
			got = append(got, fmt.Sprintf("%s %s %d", c.Expr, c.Op, c.Literal))
		}
		t.Fatalf("разбор объявил порогом законное число рядом с ключами (%s) — он ловит "+
			"число, а не роль выражения, и будет отключён первым же прогоном",
			strings.Join(got, ", "))
	}
}

// TestKeyStrengthScannerReadsTheDeclaredNumbers — объявление порога обязано
// возвращать ЧИСЛА, и разбор обязан их видеть.
//
// Без этой стороны утверждение «порог объявлен числом» проверялось бы разбором,
// который чисел не читает вовсе, и было бы зелёным при любом объявлении.
func TestKeyStrengthScannerReadsTheDeclaredNumbers(t *testing.T) {
	const src = `package domain

func (a SigningAlgorithm) MinBits() int {
	switch a {
	case SigningAlgRS256:
		return 2048
	case SigningAlgES256:
		return 256
	default:
		return 0
	}
}
`
	decls, compares, _, err := ScanKeyStrengthThresholds(
		"synthetic/signing_key.go", []byte(src), keyStrengthDeclName)
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if len(decls) != 1 {
		t.Fatalf("объявлений порога найдено %d, ожидалось 1: %+v", len(decls), decls)
	}
	if len(decls[0].Literals) != 3 {
		t.Fatalf("чисел объявления прочитано %d, ожидалось 3 — разбор не видит, чем "+
			"объявлен порог, и утверждение «объявлен числом» зеленело бы всегда: %v",
			len(decls[0].Literals), decls[0].Literals)
	}
	if len(compares) != 0 {
		t.Errorf("возврат числа опознан сравнением (%+v) — сообщение стойкости не есть "+
			"решение о приёме", compares)
	}
}

// TestKeyStrengthScannerNamesItsBlindSpot — предпосылка разбора проверяется, а не
// объявляется.
//
// Порог, вынесенный в именованную константу пакета, разбору неотличим от
// законного чтения объявленного порога: обе формы сравнивают с идентификатором.
// Это записано слепой зоной в заголовке keystrengththreshold.go; проба держит то
// утверждение честным — вырастет радиус, и здесь станет видно, что заголовок
// пора править.
func TestKeyStrengthScannerNamesItsBlindSpot(t *testing.T) {
	const src = `package keystore

const rsaFloor = 2048

func admit(bits int) error {
	if bits < rsaFloor {
		return errWeak
	}
	return nil
}
`
	_, compares, _, err := ScanKeyStrengthThresholds(
		"synthetic/keystore.go", []byte(src), keyStrengthDeclName)
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if len(compares) != 0 {
		t.Fatalf("разбор увидел порог за именованной константой (%+v) — радиус вырос, и "+
			"заголовок keystrengththreshold.go, объявляющий это слепой зоной, стал ложным. "+
			"Правь заголовок вместе с разбором.", compares)
	}
}
