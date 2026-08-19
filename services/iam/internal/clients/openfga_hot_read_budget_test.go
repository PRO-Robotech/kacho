// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package clients_test

// openfga_hot_read_budget_test.go — вторая половина класса #720.
//
// ПРЕДМЕТ. Соседи наблюдавшегося вопроса — `CheckWithContext` (за ним стоят
// публичная авторизация, внутренний гейт каждого RPC и фильтр видимости
// страницы) и `BatchCheckWithContext` (пообъектный фильтр страницы) — повтор
// ИМЕЮТ: они идут общим транспортом. Но срок на них наложен СНАРУЖИ петли
// повтора, один на все попытки. Поэтому наблюдавшуюся форму — «ответ не пришёл
// за отведённый срок» — этот повтор не переживает by construction: к моменту
// второй попытки бюджет израсходован первой, и петля выходит по отмене.
//
// То есть повтор здесь срабатывает только на БЫСТРЫХ отказах (адресата нет,
// мгновенный 5xx) и молчит ровно на той форме, которая дала отказ арендатору.
// Отличить одно от другого прочтением кода нельзя — обе конструкции выглядят
// как «повтор есть»; отличает проба.
//
// Отрицания стоят В ПАРЕ с положительными: без «здоровый ответ стоит ровно
// одного запроса» утверждение «перебой поглощён» неотличимо от «клиент всегда
// ходит трижды».

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// ── CheckWithContext ──────────────────────────────────────────────────────

// TestCheckWithContext_SlowAnswerBlipIsAbsorbedByRetry — наблюдавшаяся форма
// #720 на соседнем вопросе. RED до фикса: общий бюджет израсходован первой
// попыткой.
func TestCheckWithContext_SlowAnswerBlipIsAbsorbedByRetry(t *testing.T) {
	endpoint, requests := scriptedStore(t, func(n int, w http.ResponseWriter) {
		if n == 1 {
			time.Sleep(4 * checkProbeTimeout)
			return
		}
		_, _ = fmt.Fprint(w, `{"allowed":true}`)
	})
	c := newProbeClient(endpoint)

	allowed, err := c.CheckWithContext(context.Background(), "user:usr01", "v_get", "account:acc01", nil)
	if err != nil {
		t.Fatalf("одиночное исчерпание бюджета обязано быть поглощено повтором: %v", err)
	}
	if !allowed {
		t.Fatalf("после повтора ожидалось allowed=true")
	}
	if got := requests(); got != 2 {
		t.Fatalf("ожидалось 2 запроса (перебой + повтор), сделано %d", got)
	}
}

// TestCheckWithContext_HealthyAnswerCostsExactlyOneRequest — положительный
// контроль: без него «перебой поглощён» неотличимо от «ходим трижды всегда».
func TestCheckWithContext_HealthyAnswerCostsExactlyOneRequest(t *testing.T) {
	endpoint, requests := scriptedStore(t, func(_ int, w http.ResponseWriter) {
		_, _ = fmt.Fprint(w, `{"allowed":true}`)
	})
	c := newProbeClient(endpoint)

	allowed, err := c.CheckWithContext(context.Background(), "user:usr01", "v_get", "account:acc01", nil)
	if err != nil || !allowed {
		t.Fatalf("здоровое хранилище: allowed=%v err=%v", allowed, err)
	}
	if got := requests(); got != 1 {
		t.Fatalf("здоровый ответ обязан стоить РОВНО одного запроса, сделано %d", got)
	}
}

// TestCheckWithContext_CleanDenyIsNotRetried — законный близнец ДРУГОЙ
// конструкции: 400 у этого вопроса означает отказ в доступе, а не сбой, и
// повтору не подлежит.
func TestCheckWithContext_CleanDenyIsNotRetried(t *testing.T) {
	endpoint, requests := scriptedStore(t, func(_ int, w http.ResponseWriter) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprint(w, `{"code":"validation_error"}`)
	})
	c := newProbeClient(endpoint)

	allowed, err := c.CheckWithContext(context.Background(), "user:usr01", "v_get", "account:acc01", nil)
	if err != nil {
		t.Fatalf("400 — чистый отказ в доступе, не сбой: err=%v", err)
	}
	if allowed {
		t.Fatalf("400 обязан читаться как отказ, получено allowed=true")
	}
	if got := requests(); got != 1 {
		t.Fatalf("отказ в доступе обязан стоить одного запроса, сделано %d", got)
	}
}

// TestCheckWithContext_CallerBudgetIsNeverExceeded — срок ВЫЗЫВАЮЩЕГО остаётся
// верхней границей: собственный срок попытки его не продлевает.
func TestCheckWithContext_CallerBudgetIsNeverExceeded(t *testing.T) {
	endpoint, _ := scriptedStore(t, func(_ int, w http.ResponseWriter) {
		time.Sleep(10 * checkProbeTimeout)
	})
	c := newProbeClient(endpoint)

	budget := 2 * checkProbeTimeout
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()

	start := time.Now()
	if _, err := c.CheckWithContext(ctx, "user:usr01", "v_get", "account:acc01", nil); err == nil {
		t.Fatalf("при мёртвом хранилище ожидался отказ")
	}
	// Запас — на планирование, а не на ещё одну попытку.
	if elapsed := time.Since(start); elapsed > budget+3*checkProbeTimeout {
		t.Fatalf("срок вызывающего (%v) превышен: ушло %v", budget, elapsed)
	}
}

// ── BatchCheckWithContext ─────────────────────────────────────────────────

// TestBatchCheck_SlowAnswerBlipIsAbsorbedByRetry — та же форма на пообъектном
// фильтре страницы. Здесь цена отказа выше: не один ресурс, а вся страница.
func TestBatchCheck_SlowAnswerBlipIsAbsorbedByRetry(t *testing.T) {
	// Бюджет пачки выводится из бюджета одиночного вопроса (вчетверо), поэтому
	// перебой обязан быть длиннее ИМЕННО его, а не одиночного.
	endpoint, requests := scriptedStore(t, func(n int, w http.ResponseWriter) {
		if n == 1 {
			time.Sleep(8 * checkProbeTimeout)
			return
		}
		_, _ = fmt.Fprint(w, `{"result":{"0":{"allowed":true},"1":{"allowed":false}}}`)
	})
	c := newProbeClient(endpoint)

	got, err := c.BatchCheckWithContext(context.Background(), "user:usr01", "v_get",
		[]string{"account:acc01", "account:acc02"}, nil)
	if err != nil {
		t.Fatalf("одиночное исчерпание бюджета обязано быть поглощено повтором: %v", err)
	}
	if len(got) != 2 || !got[0] || got[1] {
		t.Fatalf("вердикты страницы после повтора: %v (ожидалось [true false])", got)
	}
	if n := requests(); n != 2 {
		t.Fatalf("ожидалось 2 запроса (перебой + повтор), сделано %d", n)
	}
}

// TestBatchCheck_HealthyAnswerCostsExactlyOneRequest — положительный контроль.
func TestBatchCheck_HealthyAnswerCostsExactlyOneRequest(t *testing.T) {
	endpoint, requests := scriptedStore(t, func(_ int, w http.ResponseWriter) {
		_, _ = fmt.Fprint(w, `{"result":{"0":{"allowed":true},"1":{"allowed":false}}}`)
	})
	c := newProbeClient(endpoint)

	if _, err := c.BatchCheckWithContext(context.Background(), "user:usr01", "v_get",
		[]string{"account:acc01", "account:acc02"}, nil); err != nil {
		t.Fatalf("здоровое хранилище: %v", err)
	}
	if n := requests(); n != 1 {
		t.Fatalf("здоровый ответ обязан стоить РОВНО одного запроса, сделано %d", n)
	}
}

// ── наблюдаемость покрывает ВСЮ горячую поверхность ───────────────────────

// TestHotReads_OutcomesAreCountedForTheWholeSurface — предикат снятия #720
// требует различать источники перебоя ЧИСЛОМ, а не чтением журнала построчно.
// Если бы клетки считали только одиночный вопрос, перебой на соседях — за
// которыми стоят публичная авторизация, внутренний гейт каждого RPC и фильтр
// видимости страницы — остался бы при нулевых счётчиках. То есть «отказов не
// было» было бы неотличимо от «сюда никто не приходил» — ровно та слепота, от
// которой заведено семейство.
func TestHotReads_OutcomesAreCountedForTheWholeSurface(t *testing.T) {
	t.Run("вопрос с контекстом: поглощённый перебой виден клеткой recovered", func(t *testing.T) {
		endpoint, _ := scriptedStore(t, func(n int, w http.ResponseWriter) {
			if n == 1 {
				time.Sleep(4 * checkProbeTimeout)
				return
			}
			_, _ = fmt.Fprint(w, `{"allowed":true}`)
		})
		c := newProbeClient(endpoint)
		if _, err := c.CheckWithContext(context.Background(), "user:usr01", "v_get", "account:acc01", nil); err != nil {
			t.Fatalf("перебой обязан быть поглощён: %v", err)
		}
		cnt := c.CheckOutcomeCounts()
		if cnt.Recovered != 1 || cnt.Answered != 0 {
			t.Fatalf("recovered=%d answered=%d (ожидалось 1 и 0): перебой на этой "+
				"поверхности обязан быть виден числом", cnt.Recovered, cnt.Answered)
		}
	})

	t.Run("пачечный вопрос: здоровый ответ — один вопрос, не по числу объектов", func(t *testing.T) {
		endpoint, _ := scriptedStore(t, func(_ int, w http.ResponseWriter) {
			_, _ = fmt.Fprint(w, `{"result":{"0":{"allowed":true},"1":{"allowed":false}}}`)
		})
		c := newProbeClient(endpoint)
		if _, err := c.BatchCheckWithContext(context.Background(), "user:usr01", "v_get",
			[]string{"account:acc01", "account:acc02"}, nil); err != nil {
			t.Fatalf("здоровое хранилище: %v", err)
		}
		cnt := c.CheckOutcomeCounts()
		if cnt.Answered != 1 || cnt.Recovered != 0 {
			t.Fatalf("answered=%d recovered=%d (ожидалось 1 и 0): единица счёта — "+
				"ВОПРОС к хранилищу, а не объект в нём", cnt.Answered, cnt.Recovered)
		}
	})

	t.Run("положительный контроль: без вопросов все клетки нулевые", func(t *testing.T) {
		endpoint, _ := scriptedStore(t, func(_ int, w http.ResponseWriter) {
			_, _ = fmt.Fprint(w, `{"allowed":true}`)
		})
		c := newProbeClient(endpoint)
		cnt := c.CheckOutcomeCounts()
		if cnt.Answered+cnt.Recovered+cnt.Deadline+cnt.Connect+cnt.Reset+
			cnt.ServerError+cnt.Decode+cnt.Rejected+cnt.Canceled+cnt.Other != 0 {
			t.Fatalf("клетки обязаны быть нулевыми до первого вопроса, получено %+v", cnt)
		}
	})
}
