// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// revocation.go — чтение авторитета отзыва НА ПРЕДЪЯВЛЕНИИ токена.
//
// # Почему это отдельный контроль, а не следствие короткого срока
//
// Запись, отзывающая доступ, обязана иметь читателя на пути запроса. Читатель в
// местах ВЫДАЧИ удостоверения отзыв не исполняет: он лишь не выдаёт нового.
// Пока такого читателя нет, окно отзыва равно сроку жизни удостоверения — и оно
// не сходится само, потому что сходиться нечему.
//
// # Почему окно берётся из уже объявленной политики
//
// Число окна — параметр безопасности. Заводя своё, мы получили бы параметр,
// которого никто не выбирал: его нельзя ни обсудить, ни отозвать, ни заметить
// при смене. Источник один — `pkg/authz`.RevocationPolicy.
//
// # Что кэшируется, а что нет
//
// Кэшируется ТОЛЬКО утвердительный ответ («удостоверение действует»), и только
// на объявленное окно. Отрицательный не кэшируется вовсе: он и так закрывает
// доступ, поэтому его запоминание не защищает ничего — оно лишь откладывало бы
// восстановление доступа после того, как его вернули.
//
// # Ключ кэша — свёртка, а не сам токен
//
// Само удостоверение в памяти процесса не задерживается: ключом служит его
// SHA-256. Свёртка детерминирована, поэтому кэш работает, и необратима, поэтому
// содержимое кэша не является набором действующих удостоверений.
package jwks

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/PRO-Robotech/kacho/pkg/authz"
)

// RevocationReader — авторитет отзыва: отвечает, действует ли предъявленное
// удостоверение.
//
// Контракт узкий намеренно: проверяющему нужен один ответ «да/нет», а не форма
// документа авторитета. Ошибка означает «ответ не получен либо не опознан» и
// ВСЕГДА закрывает доступ у вызывающего — «не дозвонился» не есть «разрешено».
type RevocationReader interface {
	Active(ctx context.Context, rawToken string) (bool, error)
}

// introspectionCacheEntries — потолок числа записей кэша утвердительных
// ответов. Потолок нужен не ради памяти как таковой: без него поток
// одноразовых удостоверений превращает кэш в неограниченно растущую структуру,
// то есть в способ израсходовать процесс с поверхности, доступной каждому
// предъявителю.
const introspectionCacheEntries = 4096

// introspectionTimeout — собственный срок обращения к авторитету. Он на пути
// запроса, поэтому неотвечающий авторитет обязан давать ОТКАЗ за ограниченное
// время, а не удерживать горутину.
const introspectionTimeout = 3 * time.Second

// IntrospectionReader — читатель авторитета отзыва по форме RFC 7662:
// POST <объявленный адрес>, тело `token=<компактный JWS>`, ответ 200 c
// документом `{"active": <булево>}`.
//
// Всякий иной исход — не 200, не документ, не то содержимое, обрыв, истечение
// срока — это ОТКАЗ. Разбирать «почти ответ» нельзя: страница ошибки, разобранная
// снисходительно, даёт `active=false` либо `active` по умолчанию, и обе ветки
// читаются как утверждение авторитета, которого он не делал.
type IntrospectionReader struct {
	url    string
	client *http.Client
	now    func() time.Time
	window time.Duration

	mu       sync.Mutex
	affirmed map[[sha256.Size]byte]time.Time
}

// IntrospectionOption — настройка читателя.
type IntrospectionOption func(*IntrospectionReader)

// WithIntrospectionHTTPClient подменяет клиента обращений к авторитету.
func WithIntrospectionHTTPClient(c *http.Client) IntrospectionOption {
	return func(r *IntrospectionReader) {
		if c != nil {
			r.client = c
		}
	}
}

// WithIntrospectionClock подменяет источник времени (окно кэша — вход, а не
// системное время: иначе проба окна недетерминирована).
func WithIntrospectionClock(now func() time.Time) IntrospectionOption {
	return func(r *IntrospectionReader) {
		if now != nil {
			r.now = now
		}
	}
}

// NewIntrospectionReader строит читателя по ОБЪЯВЛЕННОМУ адресу авторитета.
//
// Адрес задаётся явно и НИКОГДА не выводится из адреса соседней службы:
// выведенный адрес всегда непуст, поэтому контроль выглядит включённым, ведя в
// никуда, и ни один профиль развёртывания не обязан ничего задавать, чтобы это
// заметить.
func NewIntrospectionReader(authorityURL string, transport RevocationTransport, opts ...IntrospectionOption) (*IntrospectionReader, error) {
	trimmed := strings.TrimSpace(authorityURL)
	if trimmed == "" {
		return nil, errors.New("jwks: revocation authority URL is required (it is never derived from a neighbour's address)")
	}
	u, err := url.Parse(trimmed)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("jwks: revocation authority URL %q is not an absolute URL", trimmed)
	}
	// Учётные данные ребра строятся ЗДЕСЬ: якорь, объявленный и непригодный,
	// обязан ронять старт, а не молча откатываться на системные корни.
	client, cerr := transport.httpClientFor(trimmed)
	if cerr != nil {
		return nil, fmt.Errorf("jwks: %w", cerr)
	}

	window := authz.RevocationPolicy.Default
	if window > authz.RevocationPolicy.Ceiling {
		window = authz.RevocationPolicy.Ceiling
	}
	r := &IntrospectionReader{
		url:      trimmed,
		client:   client,
		now:      time.Now,
		window:   window,
		affirmed: map[[sha256.Size]byte]time.Time{},
	}
	for _, o := range opts {
		o(r)
	}
	return r, nil
}

// Window возвращает окно, на которое удерживается утвердительный ответ. Оно
// объявлено политикой, а не выбрано здесь; метод существует ради пробы, которая
// утверждает именно это.
func (r *IntrospectionReader) Window() time.Duration { return r.window }

// Active спрашивает авторитет, действует ли удостоверение.
func (r *IntrospectionReader) Active(ctx context.Context, rawToken string) (bool, error) {
	if strings.TrimSpace(rawToken) == "" {
		return false, errors.New("empty token")
	}
	key := sha256.Sum256([]byte(rawToken))
	if r.affirmedFresh(key) {
		return true, nil
	}

	active, err := r.ask(ctx, rawToken)
	if err != nil {
		return false, err
	}
	if active {
		r.remember(key)
	}
	return active, nil
}

func (r *IntrospectionReader) affirmedFresh(key [sha256.Size]byte) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	until, ok := r.affirmed[key]
	if !ok {
		return false
	}
	if !r.now().Before(until) {
		delete(r.affirmed, key)
		return false
	}
	return true
}

func (r *IntrospectionReader) remember(key [sha256.Size]byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.affirmed) >= introspectionCacheEntries {
		now := r.now()
		for k, until := range r.affirmed {
			if !now.Before(until) {
				delete(r.affirmed, k)
			}
		}
		// Истёкших не нашлось — сбрасываем целиком. Потеря кэша стоит лишнего
		// обращения к авторитету; рост без границы стоит процесса.
		if len(r.affirmed) >= introspectionCacheEntries {
			r.affirmed = map[[sha256.Size]byte]time.Time{}
		}
	}
	r.affirmed[key] = r.now().Add(r.window)
}

// ask выполняет одно обращение к авторитету. Всякий неопознанный исход — ошибка.
func (r *IntrospectionReader) ask(ctx context.Context, rawToken string) (bool, error) {
	// Собственный срок на каждый внешний вызов: контекст вызывающего может быть
	// сколь угодно долгим, а этот вызов стоит на пути запроса.
	callCtx, cancel := context.WithTimeout(ctx, introspectionTimeout)
	defer cancel()

	form := url.Values{"token": []string{rawToken}}
	req, err := http.NewRequestWithContext(callCtx, http.MethodPost, r.url, strings.NewReader(form.Encode()))
	if err != nil {
		return false, errors.New("revocation request could not be built")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return false, errors.New("revocation authority did not answer")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("revocation authority answered %d", resp.StatusCode)
	}
	if err := introspectionContentTypeOK(resp.Header.Get("Content-Type")); err != nil {
		return false, err
	}

	// Потолок тела: ответ авторитета — один булев признак, и мегабайты в нём
	// означают, что по адресу не тот эндпоинт.
	body, err := io.ReadAll(io.LimitReader(resp.Body, introspectionBodyCeiling+1))
	if err != nil {
		return false, errors.New("revocation answer could not be read")
	}
	if len(body) > introspectionBodyCeiling {
		return false, errors.New("revocation answer exceeds the declared ceiling")
	}

	// Признак разбирается УКАЗАТЕЛЕМ: документ без поля `active` — это не
	// «неактивен», это неопознанный ответ, и он обязан отличаться от отказа
	// авторитета.
	var doc struct {
		Active *bool `json:"active"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return false, errors.New("revocation answer is not an introspection document")
	}
	if doc.Active == nil {
		return false, errors.New("revocation answer carries no active flag")
	}
	return *doc.Active, nil
}

// introspectionBodyCeiling — потолок тела ответа авторитета.
const introspectionBodyCeiling = 64 << 10

func introspectionContentTypeOK(header string) error {
	if strings.TrimSpace(header) == "" {
		return errors.New("revocation answer carries no content type")
	}
	mt, _, err := mime.ParseMediaType(header)
	if err != nil {
		return errors.New("revocation answer carries an unparsable content type")
	}
	if strings.ToLower(mt) != "application/json" {
		return errors.New("revocation answer is not a JSON document")
	}
	return nil
}
