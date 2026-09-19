// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package restmux

import (
	"context"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// restBridgeCallTimeout — предел края на КАЖДЫЙ backend-вызов REST-моста (#2713).
// Без него неотвечающий сосед (сборка мусора, перегрузка, полуоткрытый TCP)
// держит горутину края, пока клиент сам не разорвёт соединение: сервер края
// ограничивает чтение запроса (ReadTimeout), но не ожидание соседа. Величина та
// же, что у соседних клиентов внутреннего слушателя (iam_subject: 5s; opsproxy:
// 5s) и у адаптера сессии #2713 — одна на все грани моста.
const restBridgeCallTimeout = 5 * time.Second

// restBridgeDialOpts собирает dial-опции, с которыми REST-мост (grpc-gateway)
// дозванивается до одного backend'а по его ключу: per-edge транспорт (mTLS
// client-cert + ServerName, когда edge включён; insecure — когда нет), общий
// round-robin service-config и перехватчики предела на каждый вызов (#2713).
// Когда в dialOpts нет записи под ключ, dial уходит insecure — dev
// backward-compat.
//
// Перехватчики предела (unary + stream) — на СОЕДИНЕНИИ, а не на call-site:
// grpc-gateway генерирует call-site'ы сам, у моста нет своей точки, где можно
// поставить per-call context.WithTimeout, как это делают соседние клиенты
// (iam_subject/opsproxy) в собственном коде. Перехватчик закрывает КАЖДЫЙ
// backend-вызов моста единообразно, включая те, у которых входящий контекст
// дедлайна не несёт.
func restBridgeDialOpts(backendKey string, dialOpts map[string]grpc.DialOption) []grpc.DialOption {
	transport, ok := dialOpts[backendKey]
	if !ok {
		transport = grpc.WithTransportCredentials(insecure.NewCredentials())
	}
	return []grpc.DialOption{
		transport,
		// Client-side round-robin; pair with `dns:///<headless-svc>:<port>` dial target.
		grpc.WithDefaultServiceConfig(`{"loadBalancingConfig":[{"round_robin":{}}]}`),
		grpc.WithChainUnaryInterceptor(backendCallDeadlineUnaryInterceptor(restBridgeCallTimeout)),
		grpc.WithChainStreamInterceptor(backendCallDeadlineStreamInterceptor(restBridgeCallTimeout)),
	}
}

// backendCallDeadlineUnaryInterceptor ограничивает каждый unary backend-вызов
// пределом d. context.WithTimeout берёт МИНИМУМ из уже стоящего дедлайна и
// now+d, поэтому предел никогда не РАСШИРЯЕТ более ранний дедлайн входящего
// запроса — только вносит свой, когда его нет.
func backendCallDeadlineUnaryInterceptor(d time.Duration) grpc.UnaryClientInterceptor {
	return func(
		ctx context.Context,
		method string,
		req, reply any,
		cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		opts ...grpc.CallOption,
	) error {
		ctx, cancel := context.WithTimeout(ctx, d)
		defer cancel()
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

// backendCallDeadlineStreamInterceptor ограничивает каждый stream backend-вызов
// пределом d. Отменять контекст сразу по возврату нельзя — поток переживает
// вызов перехватчика; поэтому cancel зовётся, когда контекст потока завершится
// (успех, ошибка или сам предел), а на ошибке создания — немедленно, чтобы не
// потёк контекст. Стриминговых Internal-служб мост сегодня не проксирует (они
// gRPC-direct), поэтому перехватчик — единообразное покрытие грани, а не горячий
// путь.
func backendCallDeadlineStreamInterceptor(d time.Duration) grpc.StreamClientInterceptor {
	return func(
		ctx context.Context,
		desc *grpc.StreamDesc,
		cc *grpc.ClientConn,
		method string,
		streamer grpc.Streamer,
		opts ...grpc.CallOption,
	) (grpc.ClientStream, error) {
		ctx, cancel := context.WithTimeout(ctx, d)
		stream, err := streamer(ctx, desc, cc, method, opts...)
		if err != nil {
			cancel()
			return nil, err
		}
		go func() {
			<-stream.Context().Done()
			cancel()
		}()
		return stream, nil
	}
}
