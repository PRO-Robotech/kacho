// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package restmux

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
)

// bridgeConns — соединения моста к службам: ОДНО на ключ адреса.
//
// Прежде каждая регистрация шла через `Register<X>ServiceHandlerFromEndpoint`, а
// он заводит своё соединение на вызов: по одному на службу контракта и на
// каждый мультиплексор (к службе доступа — 51). После разрыва каждое из них
// восстанавливается по своему экспоненциальному таймеру, а вызов на соединении
// в отказе отвечает сразу. `/readyz` края опрашивает ДРУГОЕ соединение — из
// карты бэкендов корня, — и после переката службы «готов» не говорил ничего о
// соединении, которым идёт глагол: перечень ключей получал 503 «connection
// error» после «готов» (kacho#3284, проба Ф12-35 (в)).
//
// Поэтому соединение ключа берётся из карты, которую передал корень, — той же,
// что опрашивает `/readyz`: «готов» значит «соединение моста восстановлено».
// Ключа в карте нет (сборка моста вне корня) — мост заводит одно своё на ключ, а
// не на регистрацию.
type bridgeConns struct {
	ctx    context.Context
	addrs  map[string]string
	shared map[string]*grpc.ClientConn
	dial   func(key string) []grpc.DialOption
	budget time.Duration

	byKey map[string]grpc.ClientConnInterface
	err   error
}

// newBridgeConns сверяет переданные соединения с картой адресов: соединение,
// ведущее не туда, куда велит адрес ключа, — отказ сборки. Иначе мост молча
// ходил бы одним адресом, а готовность спрашивала бы о другом.
func newBridgeConns(ctx context.Context, addrs map[string]string, shared map[string]*grpc.ClientConn,
	dial func(key string) []grpc.DialOption, budget time.Duration) (*bridgeConns, error) {
	for key, conn := range shared {
		addr, routed := addrs[key]
		if !routed || conn == nil {
			continue
		}
		if conn.Target() != addr {
			return nil, fmt.Errorf("REST bridge: connection for backend %q targets %q, address map says %q", key, conn.Target(), addr)
		}
	}
	return &bridgeConns{
		ctx: ctx, addrs: addrs, shared: shared, dial: dial, budget: budget,
		byKey: make(map[string]grpc.ClientConnInterface),
	}, nil
}

// to — соединение моста к ключу адреса; одно на ключ на весь процесс. Ошибка
// заведения своего соединения запоминается и возвращается сборкой (failed):
// регистрация с таким соединением не обслуживает ни одного запроса.
func (b *bridgeConns) to(key string) grpc.ClientConnInterface {
	if c, ok := b.byKey[key]; ok {
		return c
	}
	conn := b.shared[key]
	if conn == nil {
		own, err := grpc.NewClient(b.addrs[key], b.dial(key)...)
		if err != nil {
			if b.err == nil {
				b.err = fmt.Errorf("REST bridge: dial %s (%s): %w", key, b.addrs[key], err)
			}
			return nil
		}
		// Своё соединение живёт, пока жив мост: закрывается с контекстом сборки,
		// как прежде закрывал его FromEndpoint.
		go func() {
			<-b.ctx.Done()
			_ = own.Close()
		}()
		conn = own
	}
	c := budgetedConn{cc: conn, unary: backendBudgetInterceptor(b.budget)}
	b.byKey[key] = c
	return c
}

// failed — первая ошибка заведения своего соединения, nil — все заведены.
func (b *bridgeConns) failed() error { return b.err }

// budgetedConn ограничивает бюджетом УНАРНЫЙ вызов моста; поток живёт по своему
// сроку (приёмка KA1, KA1-24). Прежде бюджет ставился перехватчиком на каждом
// своём соединении моста; соединение корня перехватчика бюджета не несёт —
// его же читают нативный переход и прочие звенья, — поэтому бюджет стоит здесь,
// на стороне моста, и действует одинаково на переданном и на своём соединении.
// Учёт причины отказа (kacho#3221) остаётся перехватчиком соединения и видит
// истечение срока своей причиной: срок ставится до входа в соединение.
type budgetedConn struct {
	cc    *grpc.ClientConn
	unary grpc.UnaryClientInterceptor
}

func (c budgetedConn) Invoke(ctx context.Context, method string, args, reply any, opts ...grpc.CallOption) error {
	invoke := func(ctx context.Context, method string, args, reply any, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		return cc.Invoke(ctx, method, args, reply, opts...)
	}
	return c.unary(ctx, method, args, reply, c.cc, invoke, opts...)
}

func (c budgetedConn) NewStream(ctx context.Context, desc *grpc.StreamDesc, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	return c.cc.NewStream(ctx, desc, method, opts...)
}
