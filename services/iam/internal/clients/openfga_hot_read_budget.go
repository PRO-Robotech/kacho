// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package clients

// openfga_hot_read_budget.go — общий транспорт горячего чтения хранилища прав,
// у которого бюджет принадлежит ПОПЫТКЕ, а не всей петле повтора.
//
// ПОЧЕМУ ЭТО ПОНАДОБИЛОСЬ (#720). Соседи наблюдавшегося вопроса — вопрос с
// контекстом (за ним публичная авторизация, внутренний гейт каждого RPC и
// фильтр видимости страницы) и пачечный вопрос (пообъектный фильтр страницы) —
// повтор ИМЕЮТ: они идут общим `c.do()`. Но срок на них накладывался СНАРУЖИ
// петли, один на все попытки, поэтому наблюдавшуюся форму — «ответ не пришёл за
// отведённый срок» — этот повтор не переживал by construction: к моменту второй
// попытки бюджет израсходован первой, и петля выходит по отмене. Повтор
// срабатывал только на БЫСТРЫХ отказах и молчал ровно на той форме, которая
// дала арендатору отказ посреди здорового прогона.
//
// Прочтением кода одно от другого не отличить: обе конструкции выглядят как
// «повтор есть». Отличает проба — `openfga_hot_read_budget_test.go`.
//
// ИСХОДЫ СЧИТАЮТСЯ ТЕМИ ЖЕ КЛЕТКАМИ, что у одиночного вопроса
// (`openfga_check_retry.go`). Иначе наблюдаемость покрывала бы меньшую часть
// поверхности: через эти два вопроса проходит и публичная авторизация, и
// внутренний гейт каждого RPC, и фильтр видимости страницы, — и перебой на них
// оставался бы невидимым при нулевых счётчиках, то есть «отказов не было» было
// бы неотличимо от «сюда никто не приходил».
//
// Единица счёта — ВОПРОС к хранилищу, а не объект: пачечный вопрос о странице
// считается одним, сколько бы объектов он ни нёс.
//
// ПОЧЕМУ ОТМЕНУ ВОЗВРАЩАЕМ ВЫЗЫВАЮЩЕМУ, А НЕ ЗАКРЫВАЕМ ЗДЕСЬ. Срок ответившей
// попытки обязан дожить до конца чтения ТЕЛА ответа — иначе таймер сработает
// посреди разбора и превратит полученный ответ в отказ. Поэтому попытки,
// которые не ответили, отменяются здесь, а отмену ответившей забирает
// вызывающий и откладывает до закрытия тела. Ровно та же граница, что была у
// прежнего внешнего `context.WithTimeout`, — сдвинуто только НАЧАЛО отсчёта: с
// первой попытки на ту, что ответила.

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	// hotReadMaxAttempts / hotReadBaseBackoff — паритет с `newRetryClient(…, 3,
	// 20ms)`, через который идёт каждое остальное чтение хранилища. Отдельные
	// числа завели бы второй закон для одного предмета.
	hotReadMaxAttempts = 3
	hotReadBaseBackoff = 20 * time.Millisecond
)

// doHotRead — POST к хранилищу прав с повтором перебоя, где КАЖДАЯ попытка
// получает свой бюджет `perAttempt`.
//
// Возвращает (ответ, отмена, ошибка). Отмену вызывающий обязан отложить до
// закрытия тела; при ошибке она nil. Срок вызывающего остаётся верхней
// границей: `context.WithTimeout` от него не продлевает, а только сужает.
//
// Повторяем перебой транспорта и 5xx — ровно то же, что повторяет общий
// `retryClient`; 4xx возвращаются немедленно (отказ, а не сбой). Запрос на
// каждую попытку строится ЗАНОВО из тех же байт, поэтому переиграть тело
// нечем — оно не расходуется между попытками.
func (c *OpenFGAHTTPClient) doHotRead(
	ctx context.Context, url string, body []byte, perAttempt time.Duration,
) (*http.Response, context.CancelFunc, error) {
	var lastErr error
	var lastResp *http.Response
	lastOutcome := FGACheckOther
	for attempt := 0; attempt < hotReadMaxAttempts; attempt++ {
		if attempt > 0 {
			sleep := time.Duration(1<<uint(attempt-1)) * hotReadBaseBackoff // #nosec G115 -- attempt>0 гарантирован ветвью.
			select {
			case <-time.After(sleep):
			case <-ctx.Done():
				c.checkStats.record(FGACheckCanceled)
				if lastErr != nil {
					return nil, nil, lastErr
				}
				return nil, nil, ctx.Err()
			}
		}

		actx, cancel := context.WithTimeout(ctx, perAttempt)
		req, err := http.NewRequestWithContext(actx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			cancel()
			return nil, nil, err
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := fgaHTTPClient.Do(req)
		if err != nil {
			cancel()
			lastErr, lastOutcome = err, classifyCheckTransportErr(ctx, err)
			// Срок вызывающего исчерпан — повторять не на что.
			if ctx.Err() != nil {
				c.checkStats.record(lastOutcome)
				return nil, nil, lastErr
			}
			continue
		}
		if resp.StatusCode < http.StatusInternalServerError {
			if attempt == 0 {
				c.checkStats.record(FGACheckAnswered)
			} else {
				c.checkStats.record(FGACheckRecovered)
			}
			return resp, cancel, nil
		}
		// 5xx — перебой. Тело сливаем (с потолком) до закрытия, чтобы
		// соединение вернулось в пул, а не пересоздавалось: деградировавшее
		// хранилище не должно вдобавок жечь соединения.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrBodyBytes))
		_ = resp.Body.Close()
		cancel()
		lastResp, lastErr, lastOutcome = resp, nil, FGACheckServerError
	}
	c.checkStats.record(lastOutcome)
	if lastErr != nil {
		return nil, nil, lastErr
	}
	if lastResp != nil {
		// Последний 5xx отдаём вызывающему, чтобы тон отказа остался прежним
		// («status 503»), а не сменился на текст транспорта. Тело уже слито и
		// закрыто — как и у общего `retryClient`.
		return lastResp, func() {}, nil
	}
	return nil, nil, fmt.Errorf("openfga: retry exhausted")
}
