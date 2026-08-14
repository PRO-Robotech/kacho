// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package kacho

import (
	"context"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
)

// SecurityGroupFilter — фильтр для списка SG. Живет в leaf-пакете `kacho`
// (вместе с Pagination/NetworkFilter), чтобы избежать import-cycle
// `repo → repo/kacho → repo`; в `internal/repo/iface.go` — тонкий type-alias
// `SecurityGroupFilter = kacho.SecurityGroupFilter`.
type SecurityGroupFilter struct {
	ProjectID string
	NetworkID string
	Name      string
	Filter    string
}

// SecurityGroupReaderIface — read-операции над SecurityGroup в TX-области.
type SecurityGroupReaderIface interface {
	Get(ctx context.Context, id string) (*SecurityGroupRecord, error)
	// GetMany — резолв набора id ОДНИМ запросом (`WHERE id = ANY(...)`).
	// Отсутствующие id в карте отсутствуют — вызывающий сам решает, что значит
	// «не нашёл». Нужен там, где ссылки приходят массивом от вызывающего
	// (группы на интерфейсе, цели правил группы): резолв в цикле означал бы
	// обращение к БД на элемент массива, размер которого задаёт вызывающий.
	GetMany(ctx context.Context, ids []string) (map[string]*SecurityGroupRecord, error)
	List(ctx context.Context, f SecurityGroupFilter, p Pagination) ([]*SecurityGroupRecord, string, error)
	// ReferrersFor — потребители групп («кем используется») ПАКЕТНО, по набору
	// id: страница групп стоит одного запроса, а не одного на строку.
	//
	// Ответ по каждой группе ОГРАНИЧЕН `SecurityGroupUsedByFetch` записями —
	// пределом плюс одной. Лишняя запись и есть признак «есть ещё» (см.
	// `SecurityGroupUsedByLimit`); отдавать весь набор нельзя, число
	// интерфейсов, держащих группу, ничем не ограничено.
	//
	// Границу проекта держит САМ ЗАПРОС: потребитель из другого проекта в ответ
	// не попадает, поэтому группа с чужим потребителем неотличима от группы без
	// потребителей вовсе. Группы без потребителей в карте отсутствуют.
	ReferrersFor(ctx context.Context, sgIDs []string) (map[string][]SecurityGroupReferrer, error)
}

// SecurityGroupWriterIface — write-операции плюс read (writer видит свои writes).
//
// DML-методы НЕ открывают свою TX и НЕ emit'ят outbox — это делает caller
// (use-case) через `RepositoryWriter.Outbox().Emit(...)` после успешного DML.
// Атомарность DML + outbox держится на том, что обе операции идут через одну
// pgx.Tx (writer-instance), как у NetworkWriterIface.
//
// SG разнесен на CQRS, чтобы Network.Create мог inline создать default-SG в
// одной writer-TX вместо трех отдельных (окно orphan-SG закрыто).
type SecurityGroupWriterIface interface {
	SecurityGroupReaderIface
	Insert(ctx context.Context, sg *domain.SecurityGroup) (*SecurityGroupRecord, error)
	Update(ctx context.Context, sg *domain.SecurityGroup) (*SecurityGroupRecord, error)
	Delete(ctx context.Context, id string) error
	// UpdateRules атомарно заменяет набор правил SG (xmin-OCC).
	// Concurrent-modification → ErrFailedPrecondition.
	UpdateRules(ctx context.Context, sgID string, deleteIDs []string, add []domain.SecurityGroupRule) (*SecurityGroupRecord, error)
	// UpdateRule обновляет description/labels единичного правила в SG (xmin-OCC).
	UpdateRule(ctx context.Context, sgID, ruleID, description string, labels map[string]string, mask []string) (*SecurityGroupRecord, error)
	// GetForUpdate — Get с `SELECT ... FOR UPDATE` (row-lock) внутри writer-TX.
	// Сериализует конкурентный read-modify-write в Update (Get → applyMask →
	// UPDATE всех mutable-колонок, включая rule_specs): без него две Update с
	// disjoint update_mask читали бы один snapshot и второй commit затирал бы
	// un-masked поле первого (lost-update).
	GetForUpdate(ctx context.Context, id string) (*SecurityGroupRecord, error)
}
