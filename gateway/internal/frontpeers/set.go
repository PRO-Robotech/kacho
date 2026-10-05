// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package frontpeers — ЗВЕНЬЯ ФРОНТА ПОИМЁННО: адреса подов, которые край
// признаёт доверенными звеньями адреса клиента (kacho#3028, круг 3).
//
// Сеть круга (KACHO_API_GATEWAY_AUTHZ_TRUSTED_PROXY_CIDRS) — всё, что край
// знает о пире по самой сети, а сеть подов общая для кластера: она выделяет
// «под кластера», а не «раздачу консоли». Звено фронта узнаётся иначе —
// именем безголовой службы, выбирающей поды фронта МЕТКАМИ (раздача консоли,
// контроллер входа). Имя такой службы разрешается в адреса её подов; эти
// адреса и есть звенья. Под в той же сети, службой не выбранный, звеном не
// становится, сколько бы заголовков он ни прислал.
//
// Перечень обновляется периодически и по промаху (под фронта сменил адрес —
// его первый запрос будит обновление; сам запрос судится прежним перечнем,
// доверие авансом не выдаётся). Частота обновлений по промаху ограничена:
// промахом служит любой пир сети круга, и без предела под кластера заставлял
// бы край разрешать имена на каждый свой запрос.
//
// Исходы разрешения:
//   - адреса — перечень заменяется ими;
//   - «нет такого имени» — у службы нет подов: звеньев нет, это не сбой;
//   - сбой — прежний перечень действует, но не дольше предела давности
//     (три периода обновления); старше — звеньев нет никому. Адрес ушедшего
//     пода переиспользуется другим подом, и давний ответ звеном сделал бы его.
package frontpeers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"
)

// Resolver разрешает имя в адреса; (*net.Resolver).LookupNetIP с сетью "ip"
// подходит без обёртки.
type Resolver func(ctx context.Context, name string) ([]netip.Addr, error)

// Options — настройки перечня.
type Options struct {
	// Names — имена безголовых служб фронта (pkg/proxycircle.ParsePeers).
	Names []string
	// Refresh — период обновления; предел давности — три периода.
	Refresh time.Duration
	// Resolve — разрешатель имён.
	Resolve Resolver
	// Now — часы; nil — time.Now.
	Now func() time.Time
	// MinGap — наименьший промежуток между обновлениями по промаху; ноль —
	// одна секунда.
	MinGap time.Duration
	// Logger — журнал сбоев разрешения; nil — slog.Default().
	Logger *slog.Logger
}

// staleAfter — во сколько периодов обновления ответ разрешателя ещё действует
// при сбое последующих.
const staleAfter = 3

// lookupBudget — предел одного разрешения имени.
const lookupBudget = 2 * time.Second

type snapshot struct {
	addrs map[netip.Addr]struct{}
	at    time.Time
}

// Set — перечень адресов звеньев фронта. Безопасен для конкурентного чтения.
type Set struct {
	names   []string
	refresh time.Duration
	minGap  time.Duration
	resolve Resolver
	now     func() time.Time
	log     *slog.Logger

	current atomic.Pointer[snapshot]
	nudge   chan struct{}
	mu      sync.Mutex // сериализует Refresh: ответы не перезаписывают друг друга вне порядка
}

// New собирает перечень. Отказ — то, с чем перечень не заработал бы: нет имён,
// нет разрешателя, неположительный период.
func New(o Options) (*Set, error) {
	switch {
	case len(o.Names) == 0:
		return nil, errors.New("frontpeers: имён служб фронта нет — звеньев не было бы ни одного")
	case o.Resolve == nil:
		return nil, errors.New("frontpeers: разрешатель не задан")
	case o.Refresh <= 0:
		return nil, fmt.Errorf("frontpeers: период обновления %v не положителен — перечень не обновлялся бы", o.Refresh)
	}
	s := &Set{
		names:   append([]string(nil), o.Names...),
		refresh: o.Refresh,
		minGap:  o.MinGap,
		resolve: o.Resolve,
		now:     o.Now,
		log:     o.Logger,
		nudge:   make(chan struct{}, 1),
	}
	if s.now == nil {
		s.now = time.Now
	}
	if s.minGap <= 0 {
		s.minGap = time.Second
	}
	if s.log == nil {
		s.log = slog.Default()
	}
	return s, nil
}

// Trusts — звено ли адрес. Промах будит обновление (не дожидаясь его).
func (s *Set) Trusts(a netip.Addr) bool {
	a = a.Unmap()
	if snap := s.current.Load(); snap != nil && s.now().Sub(snap.at) <= staleAfter*s.refresh {
		if _, ok := snap.addrs[a]; ok {
			return true
		}
	}
	select {
	case s.nudge <- struct{}{}:
	default:
	}
	return false
}

// Refresh разрешает все имена и заменяет перечень. Сбой хотя бы одного имени
// (кроме «нет такого имени») оставляет прежний перечень и возвращается.
func (s *Set) Refresh(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	addrs := map[netip.Addr]struct{}{}
	for _, name := range s.names {
		lctx, cancel := context.WithTimeout(ctx, lookupBudget)
		got, err := s.resolve(lctx, name)
		cancel()
		if err != nil {
			var dnsErr *net.DNSError
			if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
				continue
			}
			return fmt.Errorf("frontpeers: имя %q не разрешено: %w", name, err)
		}
		for _, a := range got {
			addrs[a.Unmap()] = struct{}{}
		}
	}
	s.current.Store(&snapshot{addrs: addrs, at: s.now()})
	return nil
}

// РЕПЛИКИ: на-реплику — перечень звеньев живёт в памяти КАЖДОЙ реплики края и
// судит пиров, пришедших именно к ней; разрешение имени только читает записи
// службы имён, и дубль запроса между репликами ничего не меняет.
//
// Run держит перечень свежим до отмены контекста: разрешает при запуске,
// затем по периоду и по промаху — не чаще MinGap (промах раньше откладывается). Возвращается, когда ctx
// отменён.
func (s *Set) Run(ctx context.Context) {
	ticker := time.NewTicker(s.refresh)
	defer ticker.Stop()
	var last time.Time
	attempt := func() {
		last = time.Now()
		if err := s.Refresh(ctx); err != nil && ctx.Err() == nil {
			s.log.Warn("client address: front links not refreshed, previous list kept within its staleness",
				"error", err, "stale_after", staleAfter*s.refresh)
		}
	}
	attempt()
	// Промах раньше MinGap не теряется, а откладывается до MinGap: новый под
	// фронта не ждёт полного периода, а частота разрешений остаётся под
	// пределом. Отложенное обновление одно на все промахи до него.
	var pending <-chan time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			attempt()
		case <-pending:
			pending = nil
			attempt()
		case <-s.nudge:
			if pending != nil {
				continue
			}
			if wait := s.minGap - time.Since(last); wait > 0 {
				pending = time.After(wait)
				continue
			}
			attempt()
		}
	}
}
