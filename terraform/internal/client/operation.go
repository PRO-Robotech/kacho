// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package client

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/PRO-Robotech/kacho/pkg/backoff"
)

// Operation — ответ края об асинхронной мутации в той форме, которая нужна провайдеру.
//
// ПОЧЕМУ НЕ СГЕНЕРИРОВАННЫЙ ТИП. Метаданные операции приезжают в `google.protobuf.Any`, а
// `protojson` разрешает Any ТОЛЬКО через глобальный реестр типов — то есть разбор молча
// зависит от того, импортировал ли кто-то в этой сборке нужный пакет. Забытый импорт в
// одном ресурсе ронял бы ожидание в рантайме сообщением «unable to resolve», не имеющим
// отношения к делу. Метаданные несут одно-два поля с идентификатором, поэтому цена
// типизации здесь заведомо выше пользы.
//
// Тела ЗАПРОСОВ остаются типизированными (`protojson` над сообщением контракта): там
// типизация закрывает класс «опечатка в имени поля проходит молча», и её ценность прямо
// противоположна.
type Operation struct {
	ID   string `json:"id"`
	Done bool   `json:"done"`
	// Metadata — сырой объект: идентификатор созданного ресурса лежит здесь, но читать его
	// вправе только тот, кто уже убедился в отсутствии отказа.
	Metadata map[string]any `json:"metadata"`
	// Error — тот же конверт google.rpc.Status, что и у синхронного отказа, вместе с
	// блоком details: без него отказ асинхронной мутации не называет ни одного поля.
	Error *statusEnvelope `json:"error"`
}

// MetadataString возвращает строковое поле метаданных — например networkId.
//
// Второе значение отличает «поля нет» от «поле пусто»: пустой идентификатор в состоянии
// хуже отсутствующего, потому что выглядит как значение.
func (o *Operation) MetadataString(field string) (string, bool) {
	if o == nil || o.Metadata == nil {
		return "", false
	}
	v, ok := o.Metadata[field]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

const (
	// DefaultAwaitBudget — сколько ждать завершения мутации.
	//
	// Не меньше серверного потолка: одна операция ограничена четырьмя минутами, брошенные
	// добирает реконсайлер за пять. Бюджет короче этого превращал бы штатное завершение в
	// отказ провайдера.
	DefaultAwaitBudget = 5 * time.Minute

	// DefaultAwaitInterval — НАЧАЛЬНАЯ пауза между опросами. Реальная: плотный цикл создаёт
	// на краю ту самую нагрузку, из-за которой операция и выполняется дольше.
	//
	// Дальше пауза растёт по общему для платформы закону (pkg/backoff): короткие операции
	// завершаются на первых опросах, длинные не молотят край с постоянной частотой.
	DefaultAwaitInterval = 1 * time.Second

	// maxAwaitInterval — потолок паузы: расти бесконечно нельзя, иначе завершившаяся
	// операция ждала бы своего опроса дольше, чем выполнялась.
	maxAwaitInterval = 15 * time.Second
)

// AwaitOptions — бюджет ожидания. Нули означают умолчания выше.
type AwaitOptions struct {
	Budget   time.Duration
	Interval time.Duration
}

// AwaitOperation ждёт завершения операции и возвращает её ТОЛЬКО при успехе.
//
// Порядок буквальный и он же — весь смысл функции:
//
//	завершена → УТВЕРЖДЕНИЕ, что отказа нет → и лишь затем метаданные.
//
// При отказе операция не возвращается вовсе: идентификатор ресурса в её метаданных
// присутствует ДАЖЕ у завершившейся отказом (он чеканится при приёме, до работы), и
// вызывающий, получив объект, извлёк бы фантом. Отсюда сигнатура: либо успех с
// метаданными, либо ошибка без них.
func (c *Client) AwaitOperation(ctx context.Context, opID string, opts AwaitOptions) (*Operation, error) {
	budget := opts.Budget
	if budget <= 0 {
		budget = DefaultAwaitBudget
	}
	interval := opts.Interval
	if interval <= 0 {
		interval = DefaultAwaitInterval
	}

	// Общий для платформы закон повторов вместо самописной константы: рандомизация
	// разводит одновременные ожидания нескольких ресурсов, а множитель не даёт долгой
	// операции опрашиваться с частотой короткой.
	bo := backoff.ExponentialBackoffBuilder().
		WithInitialInterval(interval).
		WithMaxInterval(maxAwaitInterval).
		WithMultiplier(1.5).
		WithRandomizationFactor(0.2).
		Build()

	deadline := time.Now().Add(budget)
	for attempt := 1; ; attempt++ {
		resp, err := c.Do(ctx, "GET", "/operations/"+opID, nil, nil)
		if err != nil {
			return nil, fmt.Errorf("опрос операции %s: %w", opID, err)
		}

		switch out := Classify(resp); out.Kind {
		case OutcomeOK:
			// ниже
		case OutcomeNotFound:
			// Край закрывает чужие операции проверкой владения, поэтому «операции нет» и
			// «операция принадлежит другому принципалу» приходят ОДИНАКОВО. Выбрать одну
			// причину провайдер не вправе — называем обе.
			return nil, fmt.Errorf(
				"операция %s не найдена. Две возможные причины, и различить их край не даёт: "+
					"операции действительно нет, либо она создана ДРУГИМ принципалом — опрашивать "+
					"операцию обязан тот же токен, которым сделана мутация", opID)
		default:
			return nil, fmt.Errorf("опрос операции %s: %s", opID, out.Message)
		}

		op := &Operation{}
		// Разбор терпим к неизвестным полям by construction: encoding/json игнорирует
		// то, чего нет в структуре, а край добавляет поля вперёд провайдера.
		if err := json.Unmarshal(resp.Body, op); err != nil {
			return nil, fmt.Errorf("разбор операции %s: %w", opID, err)
		}

		if op.Done {
			if op.Error != nil {
				code := -1
				if op.Error.Code != nil {
					code = *op.Error.Code
				}
				return nil, fmt.Errorf("операция %s завершилась отказом (код %d): %s",
					opID, code, explain(*op.Error))
			}
			return op, nil
		}

		if time.Now().After(deadline) {
			return nil, fmt.Errorf(
				"операция %s не завершилась за %s (опросов: %d). Это НЕ означает, что ресурс не "+
					"создан: проверьте состояние операции вручную перед повторным apply",
				opID, budget, attempt)
		}

		wait := bo.NextBackOff()
		if wait == backoff.Stop {
			wait = maxAwaitInterval
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("ожидание операции %s прервано: %w", opID, ctx.Err())
		case <-time.After(wait):
		}
	}
}

// Идентификатор созданного ресурса вызывающий берёт из метаданных сам — `MetadataString`
// выше. Обёртки «создай и верни id» здесь нет намеренно: она спрятала бы место, где
// идентификатор попадает в состояние, а это ровно то место, которое обязано быть видно
// глазами при чтении ресурса.
