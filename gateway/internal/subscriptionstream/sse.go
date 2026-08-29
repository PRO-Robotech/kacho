// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionstream

import (
	"fmt"
	"net/http"
	"strings"
)

// Имена событий потока. Их два — ровно по числу ветвей носителя контракта, и
// названы они теми же словами: подписчик выбирает обработчик по имени события,
// а не разбирает тело, чтобы понять, что ему пришло.
const (
	eventOpened = "opened"
	eventEvent  = "event"
)

// sseWriter — кадрирование Server-Sent Events.
//
// Отдельный тип, а не строки на месте: у кадра три обязательства (позиция без
// переводов строки, тело построчно, сброс после каждого кадра), и держать их в
// одном месте дешевле, чем сверять три копии.
type sseWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

// newSSEWriter отдаёт кадрировщик либо отказывает, если ответ не умеет
// сбрасываться.
//
// Отказ здесь — отказ ПОДЪЁМА ручки, а не запроса: поток без сброса накопится в
// буфере и приедет одним куском в конце, то есть перестанет быть потоком, ничем
// себя не выдав.
func newSSEWriter(w http.ResponseWriter) (*sseWriter, bool) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, false
	}
	return &sseWriter{w: w, flusher: flusher}, true
}

// writeHead отдаёт заголовки потока.
//
// `X-Accel-Buffering: no` — не украшение: посредник по умолчанию копит ответ и
// отдаёт целиком, превращая поток в документ, который приходит один раз в конце.
// `Cache-Control: no-store` — тем же порядком: кэшированный поток есть запись
// прошлого, выданная за настоящее.
func (s *sseWriter) writeHead() {
	h := s.w.Header()
	h.Set("Content-Type", "text/event-stream; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	s.w.WriteHeader(http.StatusOK)
	s.flusher.Flush()
}

// frame отдаёт один кадр: имя события, позицию и тело.
//
// Позиция едет полем `id:` — тем самым, которое браузер запоминает и возвращает
// заголовком при переподключении. Это и есть возобновление через край: клиенту
// для него не нужно ни строки кода.
func (s *sseWriter) frame(name, position, data string) error {
	var b strings.Builder
	if position != "" {
		// Перевод строки внутри позиции разорвал бы кадр и склеил бы два
		// события в одно. Позиция крокфордова by construction, но проверка
		// стоит здесь, а не в вере в это: кадрировщик обязан быть верен и
		// тогда, когда владелец сменит форму позиции.
		if strings.ContainsAny(position, "\r\n") {
			return fmt.Errorf("subscriptionstream: позиция содержит перевод строки и разорвала бы кадр")
		}
		b.WriteString("id: ")
		b.WriteString(position)
		b.WriteByte('\n')
	}
	b.WriteString("event: ")
	b.WriteString(name)
	b.WriteByte('\n')
	// Тело — построчно: перевод строки внутри `data:` завершает кадр, поэтому
	// многострочное тело обязано ехать несколькими полями. Сегодня тело
	// однострочно (JSON без отступов), и правило стоит именно затем, чтобы смена
	// маршалинга не разрезала кадры молча.
	for _, line := range strings.Split(strings.ReplaceAll(data, "\r\n", "\n"), "\n") {
		b.WriteString("data: ")
		b.WriteString(line)
		b.WriteByte('\n')
	}
	b.WriteByte('\n')
	if _, err := s.w.Write([]byte(b.String())); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}

// heartbeat отдаёт служебный кадр поддержания связи.
//
// Он обязателен, а не «на всякий случай»: посредник закрывает соединение, по
// которому ничего не шло дольше своего предела чтения, и молчащая подписка —
// самый обычный её режим. Двоеточие первым знаком означает комментарий: клиент
// его не видит, соединение живёт, а промежуточный буфер не копит.
func (s *sseWriter) heartbeat() error {
	if _, err := s.w.Write([]byte(": keep-alive\n\n")); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}
