// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// publishedkeyprivatehalf_injection_test.go — доказательство того, что
// TestPublishedKeyFormCarriesNoPrivateHalf СПОСОБЕН упасть, и падает он на
// существе, а не на форме.
//
// Инъекция гоняет ТУ ЖЕ функцию разбора (ScanKeyProjections), что и гейт: проба,
// повторяющая логику гейта своей копией, доказывала бы свойство копии.
//
// Пара обязательна в ОБЕ стороны:
//
//   - (а) приватная половина возвращена в публикуемую форму → находка, и она
//     называет координату: файл, строку, имя поля;
//   - (б) законная форма того же вида → молчание, но пара при этом всё равно
//     НАЙДЕНА. Второе не украшение: гейт, переставший видеть пару, зеленеет
//     на чём угодно, и без этой половины инъекция не отличила бы «дефекта нет»
//     от «разбор ослеп».
package repohygiene

import (
	"strings"
	"testing"
)

// keyInjectedDefect — приватная половина ВЕРНУЛАСЬ в публикуемую форму.
//
// Рядом с ней намеренно стоят законные соседи, на которых разбор обязан
// промолчать: публичная половина, отпечаток, срок.
const keyInjectedDefect = `package domain

import "time"

type SigningKeyRecord struct {
	KID               string
	PublicKeyPEM      string
	PrivateKeyWrapped []byte
	NotAfter          time.Time
}

func (r SigningKeyRecord) Published() PublishedKey {
	return PublishedKey{KID: r.KID, PublicKeyPEM: r.PublicKeyPEM, PrivateKeyWrapped: r.PrivateKeyWrapped}
}

type PublishedKey struct {
	KID               string
	PublicKeyPEM      string
	PrivateKeyWrapped []byte
	NotAfter          time.Time
}
`

// keyInjectedLegitimate — та же конструкция БЕЗ дефекта.
//
// Законных соседей здесь пять, и каждый закрывает свой способ ошибиться:
// PublicKeyPEM (содержит «Key», но не приватен), Thumbprint (нейтральное имя
// приватно выглядящего предмета), SeedlingCount (слово «seedling» не есть
// «seed» — разбор по подстроке спутал бы их), Deprecated (подстрока «Deprecat…»
// на границе слова) и SecretRef у ХРАНИМОЙ формы (приватное у хранимой формы
// законно — оно там и должно быть).
const keyInjectedLegitimate = `package domain

import "time"

type SigningKeyRecord struct {
	KID               string
	PublicKeyPEM      string
	PrivateKeyWrapped []byte
	SecretRef         string
	NotAfter          time.Time
}

func (r SigningKeyRecord) Published() PublishedKey {
	return PublishedKey{KID: r.KID, PublicKeyPEM: r.PublicKeyPEM}
}

type PublishedKey struct {
	KID           string
	PublicKeyPEM  string
	Thumbprint    string
	SeedlingCount int
	Deprecated    bool
	NotAfter      time.Time
}
`

// TestKeyProjectionScannerFailsOnTheDefect — сторона (а).
func TestKeyProjectionScannerFailsOnTheDefect(t *testing.T) {
	found, census, err := ScanKeyProjections("synthetic/signing_key.go", []byte(keyInjectedDefect))
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if census.TypesInspected != 2 {
		t.Fatalf("осмотрено типов %d, ожидалось 2 — разбирается не то дерево", census.TypesInspected)
	}
	if len(found) != 1 {
		t.Fatalf("пар найдено %d, ожидалась ровно 1: %+v", len(found), found)
	}
	p := found[0]
	if len(p.PrivateInPublished) != 1 {
		t.Fatalf("приватных полей у публикуемой формы найдено %d, ожидалось 1 — "+
			"разбор не падает на внесённом дефекте: %+v", len(p.PrivateInPublished), p)
	}
	got := p.PrivateInPublished[0]
	if got.Name != "PrivateKeyWrapped" {
		t.Errorf("находка называет поле %q вместо PrivateKeyWrapped", got.Name)
	}
	if got.Line == 0 || p.File == "" {
		t.Errorf("находка без координаты — по такому отказу нечего чинить: %+v", p)
	}
	if p.StoredType != "SigningKeyRecord" || p.PublishedType != "PublishedKey" {
		t.Errorf("пара опознана неверно: %s → %s", p.StoredType, p.PublishedType)
	}
}

// TestKeyProjectionScannerIsSilentOnTheLegitimateTwin — сторона (б).
//
// Пара обязана быть НАЙДЕНА и при этом не дать ни одной находки. Проверяются оба
// утверждения: гейт, потерявший пару, зеленеет на любом дереве.
func TestKeyProjectionScannerIsSilentOnTheLegitimateTwin(t *testing.T) {
	found, _, err := ScanKeyProjections("synthetic/signing_key.go", []byte(keyInjectedLegitimate))
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("пар найдено %d, ожидалась ровно 1 — гейт потерял положительного "+
			"близнеца и с этого момента молчит о чём угодно: %+v", len(found), found)
	}
	p := found[0]
	if len(p.PrivateInPublished) != 0 {
		var names []string
		for _, f := range p.PrivateInPublished {
			names = append(names, f.Name+" "+f.Type)
		}
		t.Fatalf("разбор нашёл приватное там, где всё законно (%s) — он ловит форму, "+
			"а не предмет, и будет отключён первым же ложным срабатыванием",
			strings.Join(names, ", "))
	}
	if p.SameType {
		t.Errorf("две РАЗНЫЕ формы объявлены одной — разбор перепутал приёмник с результатом")
	}
	if len(p.StoredPrivate) != 2 {
		t.Errorf("приватных полей у ХРАНИМОЙ формы найдено %d, ожидалось 2 "+
			"(PrivateKeyWrapped и SecretRef): именно они делают пару парой", len(p.StoredPrivate))
	}
}

// TestKeyProjectionScannerCatchesPrivateHiddenBehindANeutralName — второй способ
// внести тот же дефект: поле названо нейтрально, а тип приватен.
//
// Без этой стороны разбор проверял бы имена, а не предмет: достаточно было бы
// назвать поле Handle, и приватный ключ уехал бы в ответ под законным именем.
func TestKeyProjectionScannerCatchesPrivateHiddenBehindANeutralName(t *testing.T) {
	const src = `package domain

import "crypto/ed25519"

type SigningKeyRecord struct {
	PrivateKeyWrapped []byte
	PublicKeyPEM      string
}

func (r SigningKeyRecord) Published() PublishedKey { return PublishedKey{} }

type PublishedKey struct {
	PublicKeyPEM string
	Handle       ed25519.PrivateKey
}
`
	found, _, err := ScanKeyProjections("synthetic/signing_key.go", []byte(src))
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if len(found) != 1 || len(found[0].PrivateInPublished) != 1 {
		t.Fatalf("нейтрально названное поле приватного ТИПА не опознано (%+v) — "+
			"разбор проверяет имена, а не предмет", found)
	}
	if got := found[0].PrivateInPublished[0]; got.Name != "Handle" {
		t.Errorf("находка называет %q вместо Handle", got.Name)
	}
}

// TestKeyProjectionScannerCatchesTheUnseparatedForm — третий способ: публикуемая
// форма вообще не отделена от хранимой.
//
// F1-05 требует ОТДЕЛЬНОГО типа, а не только отсутствия поля: проекция в самоё
// себя обходит требование, ничего формально не нарушая.
func TestKeyProjectionScannerCatchesTheUnseparatedForm(t *testing.T) {
	const src = `package domain

type SigningKeyRecord struct {
	PrivateKeyWrapped []byte
	PublicKeyPEM      string
}

func (r SigningKeyRecord) Published() SigningKeyRecord { return r }
`
	found, _, err := ScanKeyProjections("synthetic/signing_key.go", []byte(src))
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("пар найдено %d, ожидалась 1: %+v", len(found), found)
	}
	if !found[0].SameType {
		t.Fatalf("проекция типа в самоё себя не опознана — публикуемая форма может "+
			"оказаться хранимой, и гейт этого не заметит: %+v", found[0])
	}
}

// TestKeyProjectionScannerNamesItsBlindSpot — предпосылка разбора проверяется, а
// не объявляется.
//
// Радиус ограничен: приватная половина, спрятанная за интерфейсным полем, разбору
// не видна — это записано в заголовке publishedkeyprivatehalf.go как слепая зона.
// Проба держит то утверждение честным: вырастет радиус — здесь станет видно, что
// заголовок пора править.
func TestKeyProjectionScannerNamesItsBlindSpot(t *testing.T) {
	const src = `package domain

type SigningKeyRecord struct {
	PrivateKeyWrapped []byte
	PublicKeyPEM      string
}

func (r SigningKeyRecord) Published() PublishedKey { return PublishedKey{} }

type PublishedKey struct {
	PublicKeyPEM string
	Extra        interface{}
}
`
	found, _, err := ScanKeyProjections("synthetic/signing_key.go", []byte(src))
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("пар найдено %d, ожидалась 1: %+v", len(found), found)
	}
	if len(found[0].PrivateInPublished) != 0 {
		t.Fatalf("разбор увидел приватное за интерфейсным полем (%+v) — радиус вырос, "+
			"и заголовок publishedkeyprivatehalf.go, объявляющий это слепой зоной, стал "+
			"ложным. Правь заголовок вместе с разбором.", found[0].PrivateInPublished)
	}
}

// TestKeyProjectionScannerIgnoresAKeylessProjection — четвёртая сторона: обычное
// приведение DTO парой форм ключа НЕ является.
//
// Без этого условия под гейт попало бы всякое приведение в дереве, и первый же
// ложный срабат его отключил бы.
func TestKeyProjectionScannerIgnoresAKeylessProjection(t *testing.T) {
	const src = `package api

type Row struct {
	ID   string
	Name string
}

func (r Row) DTO() Out { return Out{ID: r.ID} }

type Out struct {
	ID string
}
`
	found, census, err := ScanKeyProjections("synthetic/dto.go", []byte(src))
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if census.TypesInspected != 2 {
		t.Fatalf("осмотрено типов %d, ожидалось 2", census.TypesInspected)
	}
	if len(found) != 0 {
		t.Fatalf("приведение DTO опознано парой форм ключа (%+v) — гейт ловит форму "+
			"метода, а не предмет", found)
	}
}
