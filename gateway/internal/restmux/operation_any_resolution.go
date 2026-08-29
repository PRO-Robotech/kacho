// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package restmux

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

// operation_any_resolution.go — край обязан уметь РАЗРЕШИТЬ каждый тип, который
// владельцы кладут в `Operation.response`.
//
// ПРЕДМЕТ. `Operation.response` — `Any`, то есть пара «адрес типа + байты».
// Упаковка у владельца реестра не спрашивает: она сериализует сообщение, тип
// которого у неё под рукой. Спрашивает РАСПАКОВКА, и происходит она в ДРУГОМ
// процессе — на крае, когда protojson отображает ответ в JSON. Резолвер
// protojson по умолчанию — реестр типов процесса (`protoregistry.GlobalTypes`),
// а туда тип попадает единственным способом: пакет с ним ВЛИНКОВАН в бинарь.
// Тип, которого в реестре края нет, роняет маршаллинг; grpc-gateway пишет
// `Marshal error` и уходит в `HTTPError`, поэтому вызывающий получает 500 на
// совершенно штатном пути — например на удалении, чей ответ пуст by design.
//
// ПОЧЕМУ ЭТО ОБЪЯВЛЕНО ЗДЕСЬ, А НЕ ВЫХОДИТ САМО. Раньше `google.protobuf.Empty`
// линковался в край ПОБОЧНО — его импортировал файл соседнего пакета, к
// отображению ответов отношения не имевший. Пока файл существовал, всё
// работало; когда его сняли вместе с его собственным предметом, разрешение
// ответов уехало вместе с ним. Сборка не сломалась, ни одна проба не покраснела:
// связь была ненаблюдаемой, потому что нигде не объявлена. Здесь она объявлена.
//
// ДВЕ ПОЛОВИНЫ НАЗВАНЫ ПОРОЗНЬ НАМЕРЕННО:
//   - `requiredOperationResponseTypeURLs` — НАМЕРЕНИЕ: что край обязан отдать;
//   - `operationResponseAnchors` — МЕХАНИЗМ: чем это обеспечено (импорт, чей
//     `init()` кладёт тип в реестр процесса).
//
// Слить их в одно значение нельзя: тогда перечень выводился бы из якорей, и
// проверка «намерение обеспечено» стала бы тождественно истинной. Порознь она
// ловит обе ошибки — якорь без намерения (мёртвый импорт) и намерение без
// якоря (обещание, которого край не исполнит).

// operationResponseAnchors — МЕХАНИЗМ разрешения. Значение существует ради
// побочного эффекта своего пакета: импорт `emptypb` регистрирует
// `google.protobuf.Empty` в реестре типов процесса. Это НЕ мёртвый код —
// снятие ссылки уносит регистрацию, и край начинает отвечать 500 на каждом
// завершённом удалении.
var operationResponseAnchors = []proto.Message{
	(*emptypb.Empty)(nil), // ответ всякого Delete: `Operation.response`
}

// requiredOperationResponseTypeURLs — НАМЕРЕНИЕ: адреса, которые край обязан
// разрешать. Записаны строками, а не выведены из якорей, — иначе проверка
// сверяла бы якоря сами с собой.
func requiredOperationResponseTypeURLs() []string {
	return []string{
		"type.googleapis.com/google.protobuf.Empty",
	}
}

// anchoredTypeURLs — адреса, которые якоря фактически вносят в реестр.
func anchoredTypeURLs() []string {
	urls := make([]string, 0, len(operationResponseAnchors))
	for _, m := range operationResponseAnchors {
		urls = append(urls, "type.googleapis.com/"+string(m.ProtoReflect().Descriptor().FullName()))
	}
	return urls
}
