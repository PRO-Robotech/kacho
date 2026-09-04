// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// poolcloseunbounded_injection_test.go — доказательство того, что
// `TestPoolCloseInTestsIsBounded` СПОСОБЕН упасть, и падает он на существе.
//
// Инъекция гоняет ТУ ЖЕ функцию разбора (`ScanPoolCloses`), что и гейт: проба,
// повторяющая логику гейта своей копией, доказывала бы свойство копии.
//
// Пара обязательна в ОБЕ стороны:
//
//   - (а) закрытие пула без предела → находка, называющая строку и переменную;
//   - (б) законная конструкция ТОЙ ЖЕ формы → молчание.
//
// Законных близнецов здесь три, и каждый закрывает свой способ ошибиться:
// `pgtest.ClosePoolAtEnd` (снятие, ради которого гейт написан); `defer r.Close()`
// на репозитории (та же форма, НЕ пул — по имени они неразличимы, поэтому гейт
// обязан различать их по производителю); и `defer conn.Close()` на соединении,
// взятом из пула. Без (б) гейт ловил бы форму `X.Close()`, а первый же ложный
// срабат его отключил бы.
package repohygiene

import (
	"strings"
	"testing"
)

// injectedUnbounded — обе запрещённые формы, в одной функции, рядом с законными.
const injectedUnbounded = `package x

import (
	"context"
	"testing"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
)

func TestSomething(t *testing.T) {
	ctx := context.Background()
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	defer pool.Close()

	second, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(second.Close)

	r := kachopg.New(pool, nil)
	defer r.Close()
}
`

// injectedBounded — тот же файл, где обе формы сняты, а законные соседи на месте.
const injectedBounded = `package x

import (
	"context"
	"testing"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
)

func TestSomething(t *testing.T) {
	ctx := context.Background()
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	second, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, second)

	r := kachopg.New(pool, nil)
	defer r.Close()

	conn, err := pool.Acquire(ctx)
	require.NoError(t, err)
	defer conn.Close()
}
`

// TestPoolCloseScannerFailsOnTheDefect — сторона (а).
func TestPoolCloseScannerFailsOnTheDefect(t *testing.T) {
	fs, err := ScanPoolCloses("synthetic/x_test.go", []byte(injectedUnbounded))
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if len(fs) != 2 {
		t.Fatalf("находок %d, ожидалось 2 (defer + t.Cleanup): %+v", len(fs), fs)
	}

	forms := map[string]string{}
	for _, f := range fs {
		forms[f.Form] = f.Var
		if f.Line == 0 {
			t.Errorf("находка без строки — по такому отказу нечего чинить: %+v", f)
		}
	}
	if forms["defer"] != "pool" {
		t.Errorf("форма defer не привязана к пулу: %+v", fs)
	}
	if forms["t.Cleanup"] != "second" {
		t.Errorf("форма t.Cleanup не привязана к пулу: %+v", fs)
	}

	// Ложное срабатывание на репозитории — отдельный отказ: `r.Close` в том же
	// теле стоит законно, и его попадание в находки означало бы гейт, который
	// снимут при первом же прогоне.
	for _, f := range fs {
		if f.Var == "r" {
			t.Errorf("гейт принял закрытие репозитория за закрытие пула — он ловит форму, а не предмет: %+v", f)
		}
	}
}

// TestPoolCloseScannerIsSilentOnTheLegitimateTwin — сторона (б).
func TestPoolCloseScannerIsSilentOnTheLegitimateTwin(t *testing.T) {
	fs, err := ScanPoolCloses("synthetic/x_test.go", []byte(injectedBounded))
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if len(fs) != 0 {
		var got []string
		for _, f := range fs {
			got = append(got, f.Var+" "+f.Form)
		}
		t.Fatalf("гейт нашёл %d находок там, где всё законно (%s) — "+
			"он отвергает исправный случай и будет отключён первым же ложным срабатом",
			len(fs), strings.Join(got, ", "))
	}
}

// TestPoolCloseScannerNamesItsBlindSpot — предпосылка гейта проверяется, а не
// объявляется: пул, приехавший ПАРАМЕТРОМ, гейту невидим. Это записано в его
// заголовке как ограничение радиуса; проба держит это утверждение честным —
// изменится разбор, и здесь станет видно, что заголовок пора править.
func TestPoolCloseScannerNamesItsBlindSpot(t *testing.T) {
	const viaParam = `package x

func helper(t *testing.T, pool *pgxpool.Pool) {
	defer pool.Close()
}
`
	fs, err := ScanPoolCloses("synthetic/x_test.go", []byte(viaParam))
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if len(fs) != 0 {
		t.Fatalf("гейт увидел пул-параметр (%+v) — радиус вырос, и заголовок "+
			"poolcloseunbounded_test.go, объявляющий это слепой зоной, стал ложным. "+
			"Правь заголовок вместе с разбором.", fs)
	}
}
