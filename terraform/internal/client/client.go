// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package client — единственная дверь провайдера к краю Kachō.
//
// Здесь ровно один производитель HTTP-клиента и ровно один — настроек TLS. Это не стиль:
// вторая точка создания транспорта означает вторую политику доверия и второй срок вызова,
// причём вторая обычно оказывается умолчательной. Отключения проверки сертификата в этом
// пакете нет и быть не может — доверие расширяется только явным набором корней.
package client

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-log/tflog"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// DefaultTimeout — срок ОДНОГО вызова к краю.
//
// Он не про длительность операции: асинхронная мутация возвращает Operation сразу, а
// ожидание её завершения — отдельный бюджет с отдельным умолчанием. Смешивать их нельзя,
// иначе один таймаут либо рвёт долгие ожидания, либо позволяет висеть каждому запросу.
const DefaultTimeout = 30 * time.Second

// Config — то, что провайдер знает о крае.
type Config struct {
	// Endpoint — базовый адрес края, например https://api.kacho.example.
	// Обязателен: умолчание, выведенное из чужого значения, всегда непусто, поэтому
	// провайдер выглядел бы настроенным и ходил бы в никуда.
	Endpoint string

	// Token — Bearer, выпущенный iam. Никогда не попадает в текст ошибок этого пакета.
	Token string

	// TLSRoots — корни доверия. nil означает системные корни, а НЕ отсутствие проверки.
	TLSRoots *x509.CertPool

	// Timeout — срок одного вызова; ноль означает DefaultTimeout.
	Timeout time.Duration
}

// Headers — заголовки, значение которых решает вызывающий.
type Headers struct {
	// IdempotencyKey — ключ повторной подачи. Пустой означает «не слать заголовок»:
	// край отбрасывает пустое значение, и слать его было бы формой без содержания.
	IdempotencyKey string
}

// Client — клиент края. Создавать только через New.
type Client struct {
	endpoint   string
	token      string
	httpClient *http.Client
}

// Response — ответ края в форме, пригодной для классификации.
type Response struct {
	StatusCode  int
	Body        []byte
	ContentType string
}

// New строит клиента. Единственный конструктор в пакете.
func New(cfg Config) (*Client, error) {
	endpoint := strings.TrimRight(strings.TrimSpace(cfg.Endpoint), "/")
	if endpoint == "" {
		return nil, errors.New(
			"адрес края не задан: укажите атрибут endpoint в блоке provider \"kacho\" " +
				"либо переменную окружения KACHO_ENDPOINT. " +
				"Умолчания у этого значения нет намеренно — адрес, выведенный из чужого, " +
				"всегда непуст, и провайдер выглядел бы настроенным, обращаясь в никуда")
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	// Единственное место, где рождаются настройки TLS. RootCAs = nil — системные корни;
	// InsecureSkipVerify отсутствует как идентификатор, а не выставлен в false: поля,
	// которого нет, нельзя включить правкой одной строки в спешке.
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    cfg.TLSRoots,
	}

	return &Client{
		endpoint:   endpoint,
		token:      cfg.Token,
		httpClient: &http.Client{Timeout: timeout, Transport: transport},
	}, nil
}

// Do выполняет один вызов края.
//
// Срок применяется КАЖДЫМ вызовом — он живёт в самом клиенте, а не расставляется по местам
// вызова, где его однажды забудут. Тело сериализуется protojson из типа контракта: карт
// строк здесь нет, потому что край молча отбрасывает неизвестные ключи, и опечатка в имени
// поля не дала бы ни отказа, ни предупреждения.
func (c *Client) Do(ctx context.Context, method, path string, body proto.Message, hdr *Headers) (*Response, error) {
	var reader io.Reader
	if body != nil {
		raw, err := protojson.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("сериализация тела запроса: %w", err)
		}
		reader = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.endpoint+path, reader)
	if err != nil {
		return nil, fmt.Errorf("сборка запроса %s %s: %w", method, path, err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if hdr != nil && hdr.IdempotencyKey != "" {
		req.Header.Set("Idempotency-Key", hdr.IdempotencyKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// Ошибку транспорта заворачиваем сами: текст от net/http несёт полный URL, но
		// никогда не заголовки, поэтому токен сюда не попадает. Проверено тестом.
		return nil, fmt.Errorf("вызов %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("чтение ответа %s %s: %w", method, path, err)
	}

	return &Response{
		StatusCode:  resp.StatusCode,
		Body:        raw,
		ContentType: resp.Header.Get("Content-Type"),
	}, nil
}

// IdempotencyKey — детерминированный ключ повторной подачи.
//
// Выводится из ТОГО ЖЕ, что определяет намерение: тип ресурса, его адрес в конфигурации и
// содержание запроса. Ни времени, ни случайности: повтор после потерянного ответа обязан
// принести тот же ключ, иначе заголовок не значит ничего.
//
// Заголовок снижает вероятность дубля, но НЕ является гарантией: его хранилище у края
// живёт в памяти каждого пода. Настоящая защита — усыновление по имени перед повторным
// созданием; см. read.go.
func IdempotencyKey(resourceType, address string, body []byte) string {
	h := sha256.New()
	h.Write([]byte(resourceType))
	h.Write([]byte{0})
	h.Write([]byte(address))
	h.Write([]byte{0})
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

// DoRaw — вызов с уже готовым телом.
//
// Нужен там, где тела у контракта нет отдельным сообщением: суффикс-действия края
// принимают маленький объект, для которого генерировать тип не из чего. Общий путь
// остаётся типизированным (Do), и это исключение видно по имени.
// previewJSON — начало тела для журнала. Обрезается, потому что список на тысячу строк в
// журнале не читают, а ищут в нём первую сотню знаков.
func previewJSON(b []byte) string {
	const limit = 512
	if len(b) == 0 {
		return ""
	}
	if len(b) > limit {
		return string(b[:limit]) + "…"
	}
	return string(b)
}

func (c *Client) DoRaw(ctx context.Context, method, path string, body []byte, hdr *Headers) (*Response, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.endpoint+path, reader)
	if err != nil {
		return nil, fmt.Errorf("сборка запроса %s %s: %w", method, path, err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if hdr != nil && hdr.IdempotencyKey != "" {
		req.Header.Set("Idempotency-Key", hdr.IdempotencyKey)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("вызов %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("чтение ответа %s %s: %w", method, path, err)
	}
	// Журнал уровня DEBUG (`TF_LOG=DEBUG`) — единственный способ увидеть, ЧТО провайдер
	// на самом деле послал: без него отладка идёт по догадкам о собственном коде.
	// Заголовок с учётными данными сюда не попадает никогда.
	tflog.Debug(ctx, "kacho: запрос к краю", map[string]any{
		"method": method, "path": path, "status": resp.StatusCode,
		"request":  previewJSON(body),
		"response": previewJSON(raw),
	})
	return &Response{StatusCode: resp.StatusCode, Body: raw, ContentType: resp.Header.Get("Content-Type")}, nil
}
