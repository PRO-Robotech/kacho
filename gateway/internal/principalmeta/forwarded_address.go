// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package principalmeta

// forwarded_address.go — АДРЕС ИСТОЧНИКА ЗА КРАЙ НЕ УЕЗЖАЕТ (kacho#3028, круг 5).
//
// # Предмет
//
// Адрес клиента край выводит ОДНИМ оператором (middleware.ContextExtractor):
// заголовок пересылки принимается только от звена фронта, иначе источник —
// TCP-пир. Этим адресом край пользуется сам — условие модели прав и
// ретрансляция полосы входа (там адрес пишется оператором, а не копируется).
//
// За краем адрес из метаданных не читает ни одна служба. А уезжал он туда
// СЛОВАМИ КЛИЕНТА двумя путями:
//
//   - нативный переход копирует входящие метаданные целиком
//     ([OutgoingFromIncoming]): `x-forwarded-for`, `forwarded`,
//     `grpcgateway-x-forwarded-for` клиента доезжали до службы дословно;
//   - мост REST кладёт `x-forwarded-for` = заголовок клиента + адрес пира.
//
// Служба, которая начала бы читать такой ключ, читала бы подделку, и ни один
// гейт не сказал бы этого: читатель в службе ничем не отличим от законного.
// Поэтому исходящий вызов края к службе не несёт адреса источника ВОВСЕ.
//
// # Почему перехватчиком соединения
//
// Сборок исходящих метаданных у края несколько (общий узел нативного
// перехода, библиотека моста, собственные сборки — шапка credential_strip.go),
// и снятие в каждой разошлось бы при следующей. Перехватчик стоит на
// СОЕДИНЕНИИ к службе — через него проходит всё, что уезжает по нему, как бы
// метаданные ни были собраны. Соединений два вида: общий набор нативного
// прокси (cmd/api-gateway dialBackends) и соединения моста REST
// (restmux.NewMux). Обе провязки держат пробы настоящим вызовом
// (forwarded_address_dial_test.go, forwarded_address_wiring_test.go).
//
// # Какие ключи
//
// Каждая форма адреса источника, которую пишет клиент, звено фронта или мост:
// голая и под приставкой моста `grpcgateway-`. Перечень закрыт и стоит здесь;
// ключ, которого в нём нет, но который несёт адрес, — правка перечня.

import (
	"context"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// forwardedAddressKeys — формы адреса источника (без приставки моста).
var forwardedAddressKeys = map[string]bool{
	"x-forwarded-for":          true, // звено, мост, клиент
	"x-real-ip":                true, // звено (раздача, контроллер входа), клиент
	"forwarded":                true, // RFC 7239, клиент
	"x-original-forwarded-for": true, // контроллер входа: клиентский XFF под другим именем
}

// bridgePrefix — приставка, под которой мост REST кладёт заголовки запроса.
const bridgePrefix = "grpcgateway-"

// isForwardedAddressKey — несёт ли ключ метаданных адрес источника.
func isForwardedAddressKey(key string) bool {
	k := strings.ToLower(key)
	return forwardedAddressKeys[k] || forwardedAddressKeys[strings.TrimPrefix(k, bridgePrefix)]
}

// stripForwardedAddress — исходящий контекст без адреса источника. Отображение
// вызывающего не меняется: снятие работает на копии.
func stripForwardedAddress(ctx context.Context) context.Context {
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		return ctx
	}
	var out metadata.MD
	for k := range md {
		if isForwardedAddressKey(k) {
			if out == nil {
				out = md.Copy()
			}
			delete(out, k)
		}
	}
	if out == nil {
		return ctx
	}
	return metadata.NewOutgoingContext(ctx, out)
}

// ForwardedAddressStripUnary — перехватчик соединения к службе: унарный вызов
// уезжает без адреса источника.
func ForwardedAddressStripUnary() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		return invoker(stripForwardedAddress(ctx), method, req, reply, cc, opts...)
	}
}

// ForwardedAddressStripStream — то же для потоков.
func ForwardedAddressStripStream() grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		return streamer(stripForwardedAddress(ctx), desc, cc, method, opts...)
	}
}

// ForwardedAddressStripDialOptions — оба перехватчика как параметры соединения.
// Его ставит КАЖДОЕ соединение края к службе.
func ForwardedAddressStripDialOptions() []grpc.DialOption {
	return []grpc.DialOption{
		grpc.WithChainUnaryInterceptor(ForwardedAddressStripUnary()),
		grpc.WithChainStreamInterceptor(ForwardedAddressStripStream()),
	}
}
