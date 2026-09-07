// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: Apache-2.0

package authz

import (
	"context"

	"google.golang.org/grpc"
)

// CheckClient — port-интерфейс (DIP). Реализация — клиентский adapter в
// сервисе (например `kacho-vpc/internal/clients/iam_authz_client.go`),
// который импортирует kacho-proto stubs и кальзывает
// `iamv1.InternalIAMServiceClient.Check(...)`.
//
// Decoupling: corelib НЕ зависит от kacho-proto stubs (см. comment в doc.go).
type CheckClient interface {
	// Check возвращает (allowed, err).
	//
	//   - subjectID: "user:usr_xxx" | "service_account:sva_xxx" | "group:grp_xxx#member"
	//   - relation:  "viewer" | "editor" | "admin" | "use" | ...
	//   - object:    "project:prj_xxx" | "vpc_network:enp_xxx" | ...
	//
	// Error semantics:
	//   - returned err = nil + allowed=true  → пропустить RPC
	//   - returned err = nil + allowed=false → DENY (PermissionDenied)
	//   - returned err != nil                → considered Unavailable
	//     → fail-closed PermissionDenied (если не выставлен break-glass)
	Check(ctx context.Context, subjectID, relation, object string) (allowed bool, err error)
}

// CheckClientFunc — adapter, который позволяет использовать функцию как CheckClient.
//
// Использование в тестах:
//
//	stub := authz.CheckClientFunc(func(ctx context.Context, s, r, o string) (bool, error) {
//	    return s == "user:usr_alice" && r == "viewer", nil
//	})
type CheckClientFunc func(ctx context.Context, subjectID, relation, object string) (bool, error)

// Check satisfies CheckClient.
func (f CheckClientFunc) Check(ctx context.Context, subjectID, relation, object string) (bool, error) {
	return f(ctx, subjectID, relation, object)
}

// CheckClientFrom — СБОРЩИК решателя из соединения с владельцем модели.
//
// Именованный тип живёт здесь, у порта, а не у дескриптора носителя, и это не
// стиль. Дескриптор (`pkg/servicecontract`) обязан оставаться пакетом, которому
// звено цепочки выразить НЕЧЕМ: его гейт (`internal/repohygiene`
// `TestDescriptorCarriesNoChainLink`) держит это тем, что из grpc там доступны
// только креденшелы. Поле типа `func(grpc.ClientConnInterface) CheckClient`
// втащило бы туда grpc целиком и обезвредило предпосылку гейта — он это и
// сказал, когда поле объявили там.
//
// Соединение по-прежнему набирает НОСИТЕЛЬ по объявленному ребру; наружу
// вынесен ровно перевод вопроса в контракт владельца, потому что контракт
// принадлежит службе доступа, а не фундаменту (приёмка K3-1 §7.2, задача
// #2131). Боевая реализация — `pkg/authz/authziam`.
type CheckClientFrom func(conn grpc.ClientConnInterface) CheckClient
