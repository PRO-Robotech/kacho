// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package handler

// listener_scope.go — РУБЕЖ «какой это слушатель», удержанный конструкцией
// после переезда контура на общий носитель (`pkg/servicehost`).
//
// # Что здесь охраняется
//
// `x-kacho-admin` и `x-kacho-project-id` признаются ТОЛЬКО на внутреннем
// слушателе. Это не стиль и не паритет ради паритета: мост REST→gRPC края
// форвардит любой `Grpc-Metadata-*`, поэтому подлог приезжает в сервис как
// метадата ДОВЕРЕННОГО отправителя, и проверка доверия на этом векторе
// бесполезна by construction — доверенный отправитель ретранслирует чужой подлог.
// Разбор — в шапке [tenantFromMetadata], там же названо, что именно снимал
// поднятый таким образом признак администратора.
//
// # Почему рубеж живёт ЗДЕСЬ, а не в дескрипторе процесса
//
// Носитель контура поднимает ОБА слушателя одной парой цепочек — в этом и его
// смысл: «внутренний получает то же, что публичный» перестаёт быть свойством,
// которое автор обязан выдержать. Обратная сторона: слота «а это звено — только
// на внутреннем» у него нет, и полей интерсепторного типа в дескрипторе нет
// намеренно. Значит различие слушателей compute обязан удержать на СВОЕЙ
// поверхности — на регистрации обработчиков, которую носитель отдаёт сервису
// по одному разу на слушатель.
//
// Свойство остаётся свойством ПОСТРОЕНИЯ: [PublicRegistrar] не имеет параметра,
// которым ему можно передать «внутренний», и не вызывает [tenantFromMetadata]
// иначе как с `false`. Сколько бы край ни ретранслировал заголовок, публичная
// сторона его не прочтёт — читать его там нечему.
//
// Это ВТОРАЯ поверхность правки контура, и называть её надо прямо: пока
// носитель не выражает различие слушателей своей осью, оно держится тем, что
// сервис не перепутает два конструктора. Гейт `internal/repohygiene`
// (`TestCompositionRootsCarryNoServerConstructionOfTheirOwn`) сюда не смотрит —
// его предмет композиционный корень, а не слой обработчиков; поэтому запись об
// этом сделана здесь и в отчёте фазы, а не оставлена на память.

import (
	"context"

	"google.golang.org/grpc"
)

// PublicRegistrar оборачивает регистратор ПУБЛИЧНОГО слушателя.
//
// Параметра «слушатель» у него нет: единственное место, где решается, читать ли
// authz-несущие заголовки, — выбор конструктора, и на публичной стороне выбора
// не существует.
func PublicRegistrar(reg grpc.ServiceRegistrar, productionMode bool) grpc.ServiceRegistrar {
	return listenerScoped{
		inner:  reg,
		unary:  TenantUnaryInterceptor(false, productionMode),
		stream: TenantStreamInterceptor(false, productionMode),
	}
}

// InternalRegistrar оборачивает регистратор ВНУТРЕННЕГО слушателя (:9091):
// заголовки администратора и проектной области читаются, а не-администратор
// отвергается ([assertAdminAccess]).
func InternalRegistrar(reg grpc.ServiceRegistrar, productionMode bool) grpc.ServiceRegistrar {
	return listenerScoped{
		inner:  reg,
		unary:  TenantUnaryInterceptor(true, productionMode),
		stream: TenantStreamInterceptor(true, productionMode),
	}
}

// listenerScoped — регистратор, надевающий рубеж слушателя на КАЖДЫЙ метод
// регистрируемой службы.
//
// Обёртывается ОПИСАНИЕ службы, а не сервер: сервера сервис не получает ни в
// каком виде (носитель отдаёт `grpc.ServiceRegistrar` — интерфейс с
// единственным методом), и приделать своё звено к цепочке отсюда нельзя. Рубеж
// исполняется ПОСЛЕ всей цепочки носителя — в том числе после извлечения
// личности пира и переданной личности, которые он читает.
type listenerScoped struct {
	inner  grpc.ServiceRegistrar
	unary  grpc.UnaryServerInterceptor
	stream grpc.StreamServerInterceptor
}

// RegisterService копирует описание службы и подменяет обработчики.
//
// Копия, а не правка на месте: описание службы — пакетная переменная
// сгенерированного кода, общая для всех, кто её регистрирует. Правка на месте
// надела бы рубеж ОДНОГО слушателя на оба — то есть ровно сняла бы различие,
// ради которого этот файл существует.
func (l listenerScoped) RegisterService(sd *grpc.ServiceDesc, impl any) {
	cp := *sd

	cp.Methods = make([]grpc.MethodDesc, len(sd.Methods))
	for i, m := range sd.Methods {
		inner := m.Handler
		cp.Methods[i] = grpc.MethodDesc{
			MethodName: m.MethodName,
			Handler: func(srv any, ctx context.Context, dec func(any) error, ic grpc.UnaryServerInterceptor) (any, error) {
				return inner(srv, ctx, dec, chainUnary(ic, l.unary))
			},
		}
	}

	cp.Streams = make([]grpc.StreamDesc, len(sd.Streams))
	for i, s := range sd.Streams {
		inner := s.Handler
		// Полное имя и вид потока собираются из ОПИСАНИЯ службы, а не выводятся
		// из имени метода: сервер строит своё `StreamServerInfo` снаружи нашего
		// обработчика, поэтому здесь нужно собрать своё — и собрать его из того же
		// источника, из которого его собирает сервер.
		info := &grpc.StreamServerInfo{
			FullMethod:     "/" + sd.ServiceName + "/" + s.StreamName,
			IsClientStream: s.ClientStreams,
			IsServerStream: s.ServerStreams,
		}
		cp.Streams[i] = grpc.StreamDesc{
			StreamName:    s.StreamName,
			ServerStreams: s.ServerStreams,
			ClientStreams: s.ClientStreams,
			Handler: func(srv any, ss grpc.ServerStream) error {
				return l.stream(srv, ss, info, inner)
			},
		}
	}

	l.inner.RegisterService(&cp, impl)
}

// chainUnary ставит рубеж слушателя ПОСЛЕ цепочки носителя.
//
// Порядок именно такой, а не обратный: рубеж читает переданную личность, а
// извлекает её носитель. Решение о доверии по ещё не извлечённой личности
// решением не является.
//
// `outer == nil` означает сервер без единого звена — такого у носителя не
// бывает, но ветка остаётся: она даёт рубежу исполниться и в этом случае, а не
// исчезнуть вместе с цепочкой.
func chainUnary(outer, inner grpc.UnaryServerInterceptor) grpc.UnaryServerInterceptor {
	if outer == nil {
		return inner
	}
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
		return outer(ctx, req, info, func(c context.Context, r any) (any, error) {
			return inner(c, r, info, h)
		})
	}
}
