// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package clients

// openfga_check_retry.go — исполнение вопроса к хранилищу прав на ГОРЯЧЕМ пути
// авторизации: попытка с собственным сроком, ограниченный повтор перебоя и
// РАЗБОР причины отказа по закрытому набору.
//
// ПОЧЕМУ ЭТО ЗДЕСЬ, А НЕ В ОБЩЕМ ТРАНСПОРТЕ `c.do()`. Все прочие чтения
// хранилища (list-objects, list-users, read, expand, batch-check, store) идут
// через `c.do()`, который заканчивается `newRetryClient(...)` — три попытки с
// отступом на перебое и 5xx. Вопрос `check` этого повтора НЕ получал: он звал
// `fgaHTTPClient.Do` напрямую, хотя его собственный комментарий заявлял паритет
// с соседями. То есть самое частое чтение платформы было единственным, кто
// перебой не переживал, — и одиночный перебой становился терминальным отказом
// арендатору на ИДЕМПОТЕНТНОМ чтении.
//
// ПОЧЕМУ СРОК У КАЖДОЙ ПОПЫТКИ СВОЙ, А НЕ ОДИН НА ВСЕ. Наблюдавшийся перебой —
// это исчерпание бюджета (ответ не пришёл за отведённые миллисекунды), а не
// мгновенный обрыв. Повтор, делящий ОДИН общий срок с первой попыткой, такой
// перебой не переживает by construction: бюджет уже израсходован, и повторять
// нечем. Поэтому попытка получает свой срок, а срок вызывающего остаётся
// верхней границей — его мы не превышаем никогда.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync/atomic"
	"syscall"
	"time"
)

// Исход одного вопроса к хранилищу прав — ЗАКРЫТЫЙ набор взаимоисключающих
// клеток. Взаимоисключающих намеренно: сумма клеток равна числу заданных
// вопросов, поэтому «ноль отказов» отличимо от «сюда никто не приходил».
//
// Различать причины требуется не ради полноты. Сегодня «хранилище моргнуло» и
// «оборвалось соединение» выглядят для пробы ОДИНАКОВО — оба приезжают одним
// кодом недоступности, — и установить, что именно было, можно только чтением
// журнала построчно. Клетки ниже отвечают на этот вопрос числом.
const (
	// FGACheckAnswered — ответ получен с первой попытки (обычная жизнь).
	FGACheckAnswered = "answered"
	// FGACheckRecovered — ответ получен, но НЕ с первой попытки: перебой
	// поглощён повтором. Это главный новый сигнал: отличное от нуля значение
	// означает, что хранилище прав потряхивает, и видно это БЕЗ единой красной
	// пробы — то есть до того, как перебой станет отказом арендатору.
	FGACheckRecovered = "recovered"

	// FGACheckDeadline — попытка не уложилась в свой срок: хранилище приняло
	// запрос и не ответило вовремя. Лечится временем; именно эта форма
	// наблюдалась в #720.
	FGACheckDeadline = "deadline"
	// FGACheckConnect — соединение не установлено (адресата нет: свёрнут,
	// эндпоинтов у службы не осталось).
	FGACheckConnect = "connect"
	// FGACheckReset — соединение оборвано ПОСЛЕ установления, ответ обрезан.
	// Отдельно от `connect`: «не дозвонились» и «связь оборвалась на полуслове»
	// имеют разные причины и разные лекарства.
	FGACheckReset = "reset"
	// FGACheckServerError — хранилище ответило 5xx.
	FGACheckServerError = "server_error"
	// FGACheckDecode — тело ответа не разобралось, и это НЕ обрыв: по адресу
	// отвечает не то. Временем не лечится (security.md §Hardening-инвариант 8:
	// настройку не прятать под сбой), поэтому повтору не подлежит.
	FGACheckDecode = "decode"
	// FGACheckRejected — хранилище отвергло САМ запрос (401/403/404): нас туда
	// не пускают либо стора/модели по этому адресу нет. Отдельно от 400, который
	// является законным отказом В ДОСТУПЕ, и отдельно от 5xx: это настройка, а
	// повтор идентичного запроса не пройдёт никогда.
	FGACheckRejected = "rejected"
	// FGACheckCanceled — вызывающий ушёл (его срок истёк либо запрос отменён).
	// Не отказ хранилища, и держать его в одном ряду с отказами значило бы
	// приписывать хранилищу чужую нагрузку.
	FGACheckCanceled = "canceled"
	// FGACheckOther — отказ, не попавший ни в одну названную форму.
	//
	// Это НЕ корзина «прочее», в которую можно списывать: клетка видна и
	// счётна, и отличное от нуля значение — НАХОДКА, а не фон. Появилась —
	// значит набор форм отстал от действительности и его надо дополнить.
	FGACheckOther = "other"
)

// FGACheckOutcomes — закрытый набор клеток семейства, в порядке вывода.
var FGACheckOutcomes = []string{
	FGACheckAnswered, FGACheckRecovered,
	FGACheckDeadline, FGACheckConnect, FGACheckReset,
	FGACheckServerError, FGACheckDecode, FGACheckRejected, FGACheckCanceled, FGACheckOther,
}

const (
	// checkMaxAttempts — сколько раз спрашиваем. Три — паритет с
	// `newRetryClient(fgaHTTPClient, 3, …)`, который уже получает КАЖДОЕ
	// остальное чтение хранилища через `c.do()`. Отдельное число здесь завело
	// бы второй закон для одного предмета.
	checkMaxAttempts = 3
	// checkRetryBackoff — основание отступа (20ms, 40ms). Тот же паритет.
	checkRetryBackoff = 20 * time.Millisecond
)

// fgaCheckStats — счётчики исходов. Живут значением в клиенте и читаются
// снаружи через CheckOutcomeCounts; клиент всегда используется по указателю
// (см. `var _ RelationStore = (*OpenFGAHTTPClient)(nil)`), поэтому копирования
// атомиков не происходит — а если кто-то его заведёт, это поймает `go vet`.
type fgaCheckStats struct {
	answered    atomic.Uint64
	recovered   atomic.Uint64
	deadline    atomic.Uint64
	connect     atomic.Uint64
	reset       atomic.Uint64
	serverError atomic.Uint64
	decode      atomic.Uint64
	rejected    atomic.Uint64
	canceled    atomic.Uint64
	other       atomic.Uint64
}

func (s *fgaCheckStats) record(outcome string) {
	switch outcome {
	case FGACheckAnswered:
		s.answered.Add(1)
	case FGACheckRecovered:
		s.recovered.Add(1)
	case FGACheckDeadline:
		s.deadline.Add(1)
	case FGACheckConnect:
		s.connect.Add(1)
	case FGACheckReset:
		s.reset.Add(1)
	case FGACheckServerError:
		s.serverError.Add(1)
	case FGACheckDecode:
		s.decode.Add(1)
	case FGACheckRejected:
		s.rejected.Add(1)
	case FGACheckCanceled:
		s.canceled.Add(1)
	default:
		s.other.Add(1)
	}
}

// FGACheckCounts — снимок счётчиков исходов вопроса к хранилищу прав.
type FGACheckCounts struct {
	Answered    uint64
	Recovered   uint64
	Deadline    uint64
	Connect     uint64
	Reset       uint64
	ServerError uint64
	Decode      uint64
	Rejected    uint64
	Canceled    uint64
	Other       uint64
}

// CheckOutcomeCounts — читатель счётчиков для коллектора наблюдаемости.
func (c *OpenFGAHTTPClient) CheckOutcomeCounts() FGACheckCounts {
	return FGACheckCounts{
		Answered:    c.checkStats.answered.Load(),
		Recovered:   c.checkStats.recovered.Load(),
		Deadline:    c.checkStats.deadline.Load(),
		Connect:     c.checkStats.connect.Load(),
		Reset:       c.checkStats.reset.Load(),
		ServerError: c.checkStats.serverError.Load(),
		Decode:      c.checkStats.decode.Load(),
		Rejected:    c.checkStats.rejected.Load(),
		Canceled:    c.checkStats.canceled.Load(),
		Other:       c.checkStats.other.Load(),
	}
}

// retryableCheckOutcome — переживает ли эта форма отказа повтор.
//
// `decode` и `canceled` — нет, и по разным причинам: первое означает, что по
// адресу отвечает не то (повтор даст тот же разбор), второе — что спрашивать
// уже не для кого.
func retryableCheckOutcome(outcome string) bool {
	switch outcome {
	case FGACheckDeadline, FGACheckConnect, FGACheckReset, FGACheckServerError, FGACheckOther:
		return true
	default:
		return false
	}
}

// classifyCheckTransportErr раскладывает отказ транспорта по закрытому набору.
//
// Порядок ветвей значим: срок ВЫЗЫВАЮЩЕГО проверяется раньше срока попытки,
// иначе чужая отмена засчиталась бы хранилищу как его отказ.
func classifyCheckTransportErr(callerCtx context.Context, err error) string {
	if callerCtx.Err() != nil {
		return FGACheckCanceled
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return FGACheckDeadline
	case errors.Is(err, context.Canceled):
		return FGACheckCanceled
	case errors.Is(err, syscall.ECONNREFUSED):
		return FGACheckConnect
	case errors.Is(err, syscall.ECONNRESET), errors.Is(err, syscall.EPIPE),
		errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return FGACheckReset
	}
	var netErr *net.OpError
	if errors.As(err, &netErr) {
		if netErr.Op == "dial" {
			return FGACheckConnect
		}
		if netErr.Timeout() {
			return FGACheckDeadline
		}
		return FGACheckReset
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return FGACheckConnect
	}
	return FGACheckOther
}

// checkAttempt — ОДНА попытка со СВОИМ сроком.
//
// Возвращает (ответ, исход, ошибка). Исход непуст только когда ошибка непуста.
// 400 — чистый ОТКАЗ В ДОСТУПЕ, а не сбой: такой вопрос не разрешится никогда
// (отношения нет у типа объекта, объект-подстановка), поэтому он терминален и
// считается отвеченным.
func (c *OpenFGAHTTPClient) checkAttempt(ctx context.Context, body []byte) (bool, string, error) {
	cctx, cancel := context.WithTimeout(ctx, c.checkTimeout())
	defer cancel()

	req, err := http.NewRequestWithContext(cctx, http.MethodPost,
		fmt.Sprintf("http://%s/stores/%s/check", c.Endpoint, c.StoreID),
		bytes.NewReader(body))
	if err != nil {
		return false, FGACheckOther, fmt.Errorf("openfga check: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := fgaHTTPClient.Do(req)
	if err != nil {
		return false, classifyCheckTransportErr(ctx, err), fmt.Errorf("openfga check: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusBadRequest {
		// Слив тела (с потолком) до Close — чтобы соединение вернулось в пул, а
		// не пересоздавалось. Деградировавшее хранилище, сыплющее 400, не должно
		// вдобавок жечь соединения. Локировано openfga_check_drain_test.go.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrBodyBytes))
		return false, "", nil
	}
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrBodyBytes))
		if resp.StatusCode >= http.StatusInternalServerError {
			return false, FGACheckServerError, fmt.Errorf("openfga check: status %d", resp.StatusCode)
		}
		// Прочие 4xx — не перебой: повтор идентичного запроса не пройдёт.
		return false, FGACheckRejected, fmt.Errorf("openfga check: status %d", resp.StatusCode)
	}

	var r openfgaCheckResponse
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		// Обрыв на полуслове и «отвечает не то» различаются здесь, а не
		// сваливаются в одну форму: первое лечится повтором, второе — нет.
		if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
			return false, FGACheckReset, fmt.Errorf("openfga check decode: %w", err)
		}
		return false, FGACheckDecode, fmt.Errorf("openfga check decode: %w", err)
	}
	return r.Allowed, "", nil
}

// check — общий транспорт вопроса к хранилищу прав: ограниченный повтор
// перебоя на ИДЕМПОТЕНТНОМ чтении. `consistency` — проволочное значение
// OpenFGA ("" ⇒ поле опущено ⇒ умолчание MINIMIZE_LATENCY).
func (c *OpenFGAHTTPClient) check(ctx context.Context, subject, relation, object, consistency string) (bool, error) {
	if c.Endpoint == "" || c.StoreID == "" {
		return false, ErrNotConfigured
	}
	body, _ := json.Marshal(openfgaCheckRequest{
		AuthorizationModelID: c.AuthorizationModel,
		TupleKey:             openfgaTupleKey{User: subject, Relation: relation, Object: object},
		Consistency:          consistency,
	})

	var lastErr error
	lastOutcome := FGACheckOther
	for attempt := 0; attempt < checkMaxAttempts; attempt++ {
		if attempt > 0 {
			sleep := time.Duration(1<<uint(attempt-1)) * checkRetryBackoff // #nosec G115 -- attempt>0 гарантирован ветвью.
			select {
			case <-time.After(sleep):
			case <-ctx.Done():
				c.checkStats.record(FGACheckCanceled)
				return false, fmt.Errorf("openfga check: %w", ctx.Err())
			}
		}
		allowed, outcome, err := c.checkAttempt(ctx, body)
		if err == nil {
			if attempt == 0 {
				c.checkStats.record(FGACheckAnswered)
			} else {
				c.checkStats.record(FGACheckRecovered)
			}
			return allowed, nil
		}
		lastErr, lastOutcome = err, outcome
		// Срок вызывающего — верхняя граница: исчерпан, повторять не на что.
		if !retryableCheckOutcome(outcome) || ctx.Err() != nil {
			break
		}
	}
	c.checkStats.record(lastOutcome)
	return false, lastErr
}
