// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// assertionadmissioncalls_injection_test.go — доказательство того, что
// TestAssertionAdmissionIsASingleDatabaseCall СПОСОБЕН упасть, и падает он на
// существе, а не на форме.
//
// Инъекция гоняет ТУ ЖЕ функцию разбора (ScanDatabaseCallsByFunction), что и
// гейт: иначе проверялась бы копия правил, а не они сами.
//
// Пара обязательна в обе стороны, и вторая сторона здесь тяжелее первой.
// Сборщику два вызова ЗАКОННЫ — он не решает, принять ли предъявление, поэтому
// неделимости от него не требуется. Гейт, считающий вызовы у ВСЕХ функций
// хранилища, запретил бы сборщику существовать в той форме, в какой он полезен.
package repohygiene

import "testing"

// admissionInjectedCheckThenAct — возвращённая пара «посмотреть — записать».
//
// Ровно та реализация, которую запрещает ban #10: два одновременных
// предъявления одного утверждения промахиваются ОБА мимо чужой ещё не
// записанной строки и проходят ОБА. Последовательные пробы на ней зелены все до
// единой — окна между чтением и записью при последовательном прогоне нет.
const admissionInjectedCheckThenAct = `package pg

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type ClientAssertionReplayRepo struct {
	pool    *pgxpool.Pool
	metrics *counter
}

func (r *ClientAssertionReplayRepo) Redeem(ctx context.Context, clientID, assertionID string, expiresAt time.Time) error {
	var seen bool
	if err := r.pool.QueryRow(ctx, ` + "`SELECT true FROM t WHERE id=$1`" + `, assertionID).Scan(&seen); err != nil {
		return err
	}
	if seen {
		return errReplayed
	}
	_, err := r.pool.Exec(ctx, ` + "`INSERT INTO t VALUES ($1)`" + `, assertionID)
	return err
}

func (r *ClientAssertionReplayRepo) Reap(ctx context.Context, now time.Time) (int64, error) {
	tag, err := r.pool.Exec(ctx, ` + "`DELETE FROM t WHERE expires_at <= $1`" + `, now)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
`

// admissionInjectedLawfulPair — допуск одним оператором И сборщик, которому два
// вызова законны.
//
// Сборщик здесь намеренно сделан ДВУХВЫЗОВНЫМ (пересчёт остатка после уборки):
// в дереве он сегодня однооператорный, поэтому молчание гейта на нём ничего бы
// не различало — оно было бы верно и для гейта, требующего одного вызова ОТ
// ВСЕХ. Различие проверяется только там, где законный близнец сам нарушает
// число.
//
// Рядом стоят два соседа, каждый со своим способом обмануть разбор:
//
//   - `r.metrics.Exec(...)` — одноимённый метод ЧУЖОГО поля: к базе отношения
//     не имеет, и вызов к базе из него не делается;
//   - `strings.QueryRow` — одноимённая функция чужого пакета.
const admissionInjectedLawfulPair = `package pg

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type ClientAssertionReplayRepo struct {
	pool    *pgxpool.Pool
	metrics *counter
}

func (r *ClientAssertionReplayRepo) Redeem(ctx context.Context, clientID, assertionID string, expiresAt time.Time) error {
	if clientID == "" {
		return errNoClient
	}
	tag, err := r.pool.Exec(ctx, ` + "`INSERT INTO t VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`" + `, clientID, assertionID, expiresAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errReplayed
	}
	return nil
}

func (r *ClientAssertionReplayRepo) Reap(ctx context.Context, now time.Time) (int64, error) {
	tag, err := r.pool.Exec(ctx, ` + "`DELETE FROM t WHERE expires_at <= $1`" + `, now)
	if err != nil {
		return 0, err
	}
	var left int64
	if err := r.pool.QueryRow(ctx, ` + "`SELECT count(*) FROM t`" + `).Scan(&left); err != nil {
		return 0, err
	}
	_ = left
	return tag.RowsAffected(), nil
}

func (r *ClientAssertionReplayRepo) Observe(ctx context.Context) {
	r.metrics.Exec(ctx)
	_ = strings.QueryRow("noop")
}
`

// TestAdmissionScannerFindsTheReturnedCheckThenAct — сторона (а): внесённый
// дефект становится находкой, находка несёт координату, и она указывает на
// ДОПУСК, а не на сборщика.
func TestAdmissionScannerFindsTheReturnedCheckThenAct(t *testing.T) {
	byFunc, census, err := ScanDatabaseCallsByFunction(
		"synthetic/pg/replay.go", []byte(admissionInjectedCheckThenAct))
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if census.Functions == 0 {
		t.Fatalf("осмотрено ноль функций — разбирается не то дерево")
	}

	redeem, ok := byFunc["ClientAssertionReplayRepo.Redeem"]
	if !ok {
		t.Fatalf("допуск не найден разбором; найдено: %v", sortedFuncNames(byFunc))
	}
	if len(redeem.Calls) != 2 {
		t.Fatalf("вызовов к базе у допуска насчитано %d, ожидалось 2 (чтение и запись): %+v",
			len(redeem.Calls), redeem.Calls)
	}
	for _, c := range redeem.Calls {
		if c.File == "" || c.Line == 0 {
			t.Errorf("находка без координаты — по такому отказу нечего чинить: %+v", c)
		}
	}
	if redeem.Calls[0].Line == redeem.Calls[1].Line {
		t.Errorf("оба вызова на одной строке (%d) — разбор считает строки, а не вызовы: %+v",
			redeem.Calls[0].Line, redeem.Calls)
	}

	// И то же самое глазами гейта: число вызовов допуска отличается от одного.
	if len(redeem.Calls) == assertionAdmissionCallBudget {
		t.Fatalf("гейт на этом дефекте остался бы зелёным: у допуска %d вызов(а) при бюджете %d",
			len(redeem.Calls), assertionAdmissionCallBudget)
	}
}

// TestAdmissionScannerIsSilentOnTheReaper — сторона (б): законный близнец.
//
// Сборщик с ДВУМЯ вызовами находкой не становится, а допуск с одним — тем более.
// Без этой стороны гейт ловил бы число, а не предмет.
func TestAdmissionScannerIsSilentOnTheReaper(t *testing.T) {
	byFunc, census, err := ScanDatabaseCallsByFunction(
		"synthetic/pg/replay.go", []byte(admissionInjectedLawfulPair))
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if census.Functions < 3 {
		t.Fatalf("осмотрено функций %d — разбирается не то дерево", census.Functions)
	}

	redeem, ok := byFunc["ClientAssertionReplayRepo.Redeem"]
	if !ok {
		t.Fatalf("допуск не найден разбором; найдено: %v", sortedFuncNames(byFunc))
	}
	if len(redeem.Calls) != assertionAdmissionCallBudget {
		t.Fatalf("у законного допуска насчитано %d вызовов при бюджете %d — гейт краснел бы "+
			"на исправной реализации: %+v", len(redeem.Calls), assertionAdmissionCallBudget, redeem.Calls)
	}

	reap, ok := byFunc["ClientAssertionReplayRepo.Reap"]
	if !ok {
		t.Fatalf("сборщик не найден разбором; найдено: %v", sortedFuncNames(byFunc))
	}
	if len(reap.Calls) != 2 {
		t.Fatalf("у двухвызовного сборщика насчитано %d вызовов, ожидалось 2: %+v",
			len(reap.Calls), reap.Calls)
	}

	// Соседи: одноимённый метод чужого поля и одноимённая функция чужого пакета
	// вызовом к базе не являются.
	observe, ok := byFunc["ClientAssertionReplayRepo.Observe"]
	if !ok {
		t.Fatalf("соседняя функция не найдена разбором; найдено: %v", sortedFuncNames(byFunc))
	}
	if len(observe.Calls) != 0 {
		t.Fatalf("разбор объявил вызовом к базе чужой одноимённый метод или функцию (%+v) — "+
			"тогда гейт судит по ИМЕНИ метода, а не по владельцу соединения", observe.Calls)
	}
}
