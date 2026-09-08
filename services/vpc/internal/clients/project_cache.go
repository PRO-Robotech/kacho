// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package clients

import (
	"container/list"
	"context"
	"sync"
	"time"

	"github.com/PRO-Robotech/kacho/pkg/peer"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo"
)

// CachedProjectClient — TTL+LRU декоратор поверх любого repo.ProjectClient
// (port-интерфейс). Убирает gRPC RTT к kaname из hot-path
// Network.Create/Subnet.Create/... при burst-нагрузке: без кеша каждый
// запрос делает hop в kaname и упирает throughput в потолок RTT.
//
// Семантика кеширования Exists:
//   - Положительный результат (Exists=true) кешируется на полный TTL
//     (default 30s). Существование project — стабильное свойство (project
//     редко удаляется), но все-таки кешируем не вечно.
//   - Negative-результат (Exists=false / underlying NotFound) кешируется
//     на короткий negative-TTL (default 5s) — чтобы свеже-созданный
//     project быстро стал виден, но повторные «project не найден» не
//     хаммерили kaname.
//   - Любая другая ошибка (Unavailable, Internal, DeadlineExceeded) —
//     НЕ кешируется, fail-open: следующий запрос попробует снова. Это
//     корректное поведение для transient ошибок kaname.
//
// LRU bounded — защита от unbounded memory growth: при достижении
// MaxSize самый старый (по recency) entry вытесняется. Без bound на
// случайном workload (миллионы уникальных project-id за сессию) кеш мог
// бы дорасти до сотен МБ.
//
// Concurrency: один Mutex защищает map + LRU-list, все операции O(1)
// среднеамортизированно. Goroutine-safe (проверено unit-тестом с -race).
type CachedProjectClient struct {
	upstream projectDescriber
	posTTL   time.Duration
	negTTL   time.Duration
	maxSize  int
	clock    func() time.Time // для тестов; в проде = time.Now

	mu     sync.Mutex
	cache  map[string]*list.Element
	lruLst *list.List
}

// projectDescriber — то, что декоратор ожидает от upstream: существование
// проекта И его аккаунт, оба из ОДНОГО вызова `ProjectService.Get`.
//
// Порт шире `repo.ProjectClient` намеренно. Аккаунт нужен материализации учёта
// (приёмка квот, V2-4), и он обязан приезжать тем же обращением: заведи ему
// отдельный вызов — «нового ребра работа не заводит» осталось бы верным на
// бумаге и ложным в нагрузке, а кэш хранил бы два ответа об одном проекте.
type projectDescriber interface {
	repo.ProjectClient
	// Describe отдаёт ОБА факта разом. Именно оба, а не аккаунт: выводить
	// существование из непустоты аккаунта значило бы утверждать, что у
	// существующего проекта аккаунт непуст ВСЕГДА, — обещания такого владелец
	// проектов не давал, а цена ошибки в том, что существующий проект стал бы
	// «несуществующим».
	Describe(ctx context.Context, projectID string) (bool, string, error)
}

// projectCacheEntry — одна запись кеша.
//
// Запись ОДНА на проект и несёт оба факта: они получены одним ответом соседа и
// устаревают вместе. Две записи разошлись бы по времени жизни, и «проект есть»
// могло бы жить дольше, чем «его аккаунт такой-то», — состояние, в котором
// материализация заводит строку с чужим зеркалом.
type projectCacheEntry struct {
	projectID string
	exists    bool
	accountID string
	exp       time.Time
}

// Compile-time проверка: CachedProjectClient реализует port-интерфейс.
var _ repo.ProjectClient = (*CachedProjectClient)(nil)

// ProjectCacheConfig — параметры кеша. Все поля опциональны; нулевые
// значения заменяются на дефолты (positiveTTL=30s, negativeTTL=5s,
// maxSize=10000).
type ProjectCacheConfig struct {
	PositiveTTL time.Duration
	NegativeTTL time.Duration
	MaxSize     int
	// Clock — опциональный override таймера (для unit-тестов). Если nil,
	// используется time.Now.
	Clock func() time.Time
}

// NewCachedProjectClient оборачивает upstream ProjectClient TTL+LRU кешем
// для метода Exists.
//
// Применять как drop-in замену projectClient в composition root
// (`cmd/vpc/main.go`):
//
//	rawProjectClient := clients.NewProjectClient(iamConn)
//	projectClient := clients.NewCachedProjectClient(rawProjectClient, clients.ProjectCacheConfig{
//	    PositiveTTL: cfg.ProjectCacheTTL,
//	    NegativeTTL: cfg.ProjectCacheNegativeTTL,
//	    MaxSize:     cfg.ProjectCacheSize,
//	})
func NewCachedProjectClient(upstream projectDescriber, cfg ProjectCacheConfig) *CachedProjectClient {
	if cfg.PositiveTTL <= 0 {
		cfg.PositiveTTL = 30 * time.Second
	}
	if cfg.NegativeTTL <= 0 {
		cfg.NegativeTTL = 5 * time.Second
	}
	if cfg.MaxSize <= 0 {
		cfg.MaxSize = 10000
	}
	if cfg.Clock == nil {
		cfg.Clock = time.Now
	}
	return &CachedProjectClient{
		upstream: upstream,
		posTTL:   cfg.PositiveTTL,
		negTTL:   cfg.NegativeTTL,
		maxSize:  cfg.MaxSize,
		clock:    cfg.Clock,
		cache:    make(map[string]*list.Element, cfg.MaxSize),
		lruLst:   list.New(),
	}
}

// Exists проверяет существование project через кеш + upstream.
func (c *CachedProjectClient) Exists(ctx context.Context, projectID string) (bool, error) {
	exists, _, err := c.describe(ctx, projectID)
	return exists, err
}

// AccountOf возвращает аккаунт проекта из ТОЙ ЖЕ записи кеша, что и Exists.
//
// Промах здесь стоит ровно одного вызова к соседу — того самого, который путь
// создания сделал бы и без учёта.
func (c *CachedProjectClient) AccountOf(ctx context.Context, projectID string) (string, error) {
	_, accountID, err := c.describe(ctx, projectID)
	return accountID, err
}

// describe — единственная точка обращения к upstream и к кешу.
func (c *CachedProjectClient) describe(ctx context.Context, projectID string) (bool, string, error) {
	// Cache hit?
	if exists, accountID, ok := c.lookup(projectID); ok {
		return exists, accountID, nil
	}

	// Miss → upstream call. Оба факта приходят одним обращением.
	exists, accountID, err := c.upstream.Describe(ctx, projectID)
	if err != nil {
		// Различаем семантически:
		//   - codes.NotFound внутри err: наш ProjectClient уже маппит
		//     NotFound → (false, nil), поэтому сюда NotFound обычно не
		//     доходит. На всякий случай обработаем — кешируем negative.
		//   - Unavailable / Internal / DeadlineExceeded / любая другая
		//     ошибка — НЕ кешируем (fail-open). Возвращаем err как есть.
		// Отрицательный результат кешируется ТОЛЬКО тогда, когда владелец его
		// установил (носитель: pkg/peer). Недоступность и непонятый ответ
		// установленным отказом не являются — их кеширование зафиксировало бы
		// перебой у соседа как «проекта нет» на всё окно TTL.
		lane := peer.Classify(err)
		if !lane.RefusedReference() {
			return false, "", err
		}
		// Наружу — отказ ссылки (анти-оракул: промах, отказ в правах и негодный
		// идентификатор для арендатора неразличимы).
		//
		// В КЭШ — только исход, не зависящий от того, кто спросил. Ключ здесь
		// не несёт личности, поэтому отказ в правах, сохранённый под ним, был бы
		// отдан ДРУГОМУ вызывающему — решение, вынесенное не ему. Окно отказа на
		// своём свежем ресурсе объявлено штатным (материализация прав идёт
		// eventually-consistent), и фиксировать его как «проекта нет» на всё окно
		// TTL для всех — значит превратить транзиент в общий отказ.
		if lane.CallerIndependent() {
			c.store(projectID, false, "", c.negTTL)
		}
		return false, "", nil
	}

	ttl := c.posTTL
	if !exists {
		ttl = c.negTTL
	}
	c.store(projectID, exists, accountID, ttl)
	return exists, accountID, nil
}

// lookup возвращает (exists, true) если кеш hit и не expired, иначе
// (_, false). Также промотирует entry в head LRU.
func (c *CachedProjectClient) lookup(projectID string) (bool, string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	el, ok := c.cache[projectID]
	if !ok {
		return false, "", false
	}
	e := el.Value.(*projectCacheEntry)
	if c.clock().After(e.exp) {
		// Expired → evict.
		c.lruLst.Remove(el)
		delete(c.cache, projectID)
		return false, "", false
	}
	// LRU touch.
	c.lruLst.MoveToFront(el)
	return e.exists, e.accountID, true
}

// store записывает entry в кеш с указанным TTL; вытесняет LRU-tail
// если перешагнули maxSize.
func (c *CachedProjectClient) store(projectID string, exists bool, accountID string, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	exp := c.clock().Add(ttl)
	if el, ok := c.cache[projectID]; ok {
		// Обновляем существующую запись.
		e := el.Value.(*projectCacheEntry)
		e.exists = exists
		e.accountID = accountID
		e.exp = exp
		c.lruLst.MoveToFront(el)
		return
	}

	// Insert new.
	e := &projectCacheEntry{projectID: projectID, exists: exists, accountID: accountID, exp: exp}
	el := c.lruLst.PushFront(e)
	c.cache[projectID] = el

	// Evict LRU-tail если перешагнули bound.
	for c.lruLst.Len() > c.maxSize {
		tail := c.lruLst.Back()
		if tail == nil {
			break
		}
		te := tail.Value.(*projectCacheEntry)
		c.lruLst.Remove(tail)
		delete(c.cache, te.projectID)
	}
}

// Len возвращает текущее число entries (для тестов / observability).
func (c *CachedProjectClient) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lruLst.Len()
}
