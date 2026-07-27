// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package operations

import "context"

// Owner — ключ владельца операции для ownership-scoped доступа (Get/Cancel).
// Operative-ключ — пара (PrincipalType, PrincipalID) создателя операции
// (не project_id): видимость операции creator-principal-scoped. AccountID —
// дополнительный IAM-only ключ: gate ДОПОЛНИТЕЛЬНО матчит по account там, где
// колонка account_id NOT NULL; для сервисов без account-metadata (vpc/compute/
// nlb) AccountID == "" и эта ветка инертна.
type Owner struct {
	PrincipalType string
	PrincipalID   string
	AccountID     string
}

// OwnerFromPrincipal строит Owner из УЖЕ УСТАНОВЛЕННОГО Principal'а. AccountID
// не заполняется (principal-ключ); IAM-сервис при необходимости выставляет его
// сам.
//
// ВНИМАНИЕ: не скармливай сюда PrincipalFromContext(ctx). Тот на пустом (и на
// явно очищенном) ctx отдаёт SystemPrincipal() — backward-compat fallback для
// ЗАПИСИ операций фоновыми путями, а не удостоверение личности. Как owner-ключ
// для ЧТЕНИЯ он совпадает с ownership-предикатом на каждой операции, записанной
// системным принципалом, то есть анонимный вызывающий получает чужие операции.
// Для вывода owner-ключа из ctx есть OwnerFromContext.
func OwnerFromPrincipal(p Principal) Owner {
	return Owner{PrincipalType: p.Type, PrincipalID: p.ID}
}

// OwnerFromContext выводит owner-ключ из ctx и отдельно сообщает, есть ли он.
//
// ok=false означает «принципала не было» (анонимный запрос, ctx без
// auth-интерсептора, либо принципал явно снят transport-слоем на недоверенном
// форвардере) ЛИБО «принципал без id». В этом случае возвращается НУЛЕВОЙ Owner,
// а не SystemPrincipal-fallback: личность по умолчанию не может быть владельцем
// чего-либо. Вызывающий обязан на ok=false отказать (для tenant-facing чтения —
// тем же no-leak NotFound, что и «чужая операция»), а не продолжать с
// полученным ключом.
//
// ЯВНО установленный системный принципал (WithPrincipal(SystemPrincipal()) на
// доверенном internal-пути) даёт ok=true и остаётся владельцем своих операций —
// сужается именно анонимность, а не bootstrap-личность как таковая. Ровно то же
// различение делает authz-слой (pkg/authz defaultSubjectExtractor).
func OwnerFromContext(ctx context.Context) (Owner, bool) {
	p, ok := PrincipalFromContextOK(ctx)
	if !ok || p.ID == "" || p.Type == "" {
		return Owner{}, false
	}
	return OwnerFromPrincipal(p), true
}

// IsAnonymous — principal-ключ отсутствует. Такой ключ НИКОГДА не должен
// доезжать до ownership-предиката: пустые компоненты матчатся со строками, у
// которых колонки принципала пусты.
//
// Про AccountID: у предиката есть вторая, account-ветка, но её ключ сейчас НИКТО
// не заполняет — во всём дереве Owner строится исключительно из пары
// (PrincipalType, PrincipalID) через OwnerFromPrincipal/OwnerFromContext, а
// AccountID остаётся "" и ветка инертна. Поэтому проверка намеренно смотрит
// только на principal-пару. Если появится account-only владелец (Owner с пустым
// принципалом и непустым AccountID), этот предикат обязан быть расширен ВМЕСТЕ
// с ним — иначе такой владелец будет молча отвергнут; правку делать осознанно,
// а не «ослабить проверку, чтобы прошло».
func (o Owner) IsAnonymous() bool {
	return o.PrincipalType == "" || o.PrincipalID == ""
}

// OwnedOperationRepo — узкий ownership-scoped порт чтения/отмены операции.
// Ownership-предикат — внутри SQL WHERE / атомарного CAS (within-service
// инвариант на DB-уровне; software-side Get→check исключен). Чужой owner →
// ErrNotFound (no-leak, неотличимо от «нет такой»).
//
// Это ОТДЕЛЬНЫЙ от Repo интерфейс: новые методы не добавляются в общий
// operations.Repo, чтобы не ломать его mock-реализации в других сервисах
// (iam/compute/nlb/geo). Реализуется конкретным pgRepo; consumer получает его
// через AsOwned(repo).
type OwnedOperationRepo interface {
	// GetOwned возвращает операцию по id ТОЛЬКО если она принадлежит owner.
	// 0 строк (нет такой ИЛИ не владелец) → ErrNotFound.
	GetOwned(ctx context.Context, id string, owner Owner) (*Operation, error)
	// CancelOwned атомарно отменяет операцию owner'а (CAS WHERE done=false AND
	// ownership-предикат, RETURNING терминальное состояние без reload-Get).
	// Идемпотентно на уже-CANCELLED (→ OK с тем же Operation); на терминале
	// SUCCESS/ERROR → ErrAlreadyDone; чужая/нет → ErrNotFound.
	CancelOwned(ctx context.Context, id string, owner Owner) (*Operation, error)
	// ListOwned возвращает страницу операций owner'а: ownership-предикат внутри
	// SQL WHERE (симметрично GetOwned) комбинируется AND-ом с фильтрами
	// ListFilter (ResourceID/AccountID/keyset-пагинация). Чужие операции не
	// попадают в выдачу — tenant-facing OperationService.List обязан идти этим
	// путём, а не unscoped Repo.List. Возвращает (страница, next_page_token, err).
	ListOwned(ctx context.Context, filter ListFilter, owner Owner) ([]Operation, string, error)
}

// AsOwned проверяет, что Repo поддерживает ownership-scoped доступ, и возвращает
// узкий порт. Для pgRepo (NewRepo) — true. Composition root vpc-handler'а:
//
//	owned, ok := operations.AsOwned(opsRepo)
//	if !ok { /* fail-fast при wiring'е */ }
func AsOwned(r Repo) (OwnedOperationRepo, bool) {
	o, ok := r.(OwnedOperationRepo)
	return o, ok
}
