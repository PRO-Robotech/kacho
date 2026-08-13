// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package kacho

import (
	"context"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
)

// CidrGroupFilter — фильтр списка именованных наборов. Живёт в leaf-пакете
// `kacho` вместе с Pagination (см. doc-комментарий на Pagination).
type CidrGroupFilter struct {
	ProjectID string
	Name      string
	// Filter — сырое выражение фильтра; разбирается в repo с белым списком полей.
	Filter string
}

// CidrGroupReaderIface — read-операции над CidrGroup в read-only TX-области.
type CidrGroupReaderIface interface {
	Get(ctx context.Context, id string) (*CidrGroupRecord, error)
	List(ctx context.Context, f CidrGroupFilter, p Pagination) ([]*CidrGroupRecord, string, error)
	// ReferrersFor — потребители набора (кто на него ссылается) ПАКЕТНО, по
	// набору идентификаторов. Пакетно, а не по одному: страница списка отдаёт до
	// тысячи наборов, и обращение на каждый превратило бы одно чтение в тысячу.
	ReferrersFor(ctx context.Context, groupIDs []string) (map[string][]CidrGroupReferrer, error)
}

// CidrGroupWriterIface — write-операции плюс read (writer видит свои writes).
//
// DML-методы НЕ открывают свою TX и НЕ emit'ят outbox — это делает caller
// (use-case) через `RepositoryWriter.Outbox().Emit(...)`.
type CidrGroupWriterIface interface {
	CidrGroupReaderIface
	Insert(ctx context.Context, g *domain.CidrGroup) (*CidrGroupRecord, error)
	// Update мутирует ТОЛЬКО косметические поля (name/description/labels).
	// Состава он не касается by construction: полная замена набора дала бы
	// «победил последний» между двумя редакторами, каждый из которых прислал свой
	// полный список.
	Update(ctx context.Context, g *domain.CidrGroup) (*CidrGroupRecord, error)
	Delete(ctx context.Context, id string) error
	// GetForUpdate — Get с `SELECT … FOR UPDATE` внутри writer-TX.
	//
	// `FOR UPDATE`, а не `FOR NO KEY UPDATE` (который взял бы обычный UPDATE), —
	// это несущее различие, а не строгость ради строгости: только `FOR UPDATE`
	// конфликтует с `FOR KEY SHARE`, которую берёт проверка внешнего ключа при
	// вставке ссылки правила. Без него правило, создаваемое ОДНОВРЕМЕННО с
	// опустошением набора, проходило бы мимо: обе стороны читали бы состояние до
	// чужой записи и обе бы разрешили себе продолжить.
	GetForUpdate(ctx context.Context, id string) (*CidrGroupRecord, error)
	// AddBlocks атомарно добавляет блоки: условный инкремент счётчиков (потолок
	// проверяет предикат самого UPDATE, а row-lock сериализует писателей), затем
	// вставка блоков без падения на уже присутствующих, затем приведение
	// счётчиков к фактическому составу — всё в одной writer-TX. Идемпотентно:
	// повтор не «съедает» потолок.
	//
	// Потолок исчерпан → ErrFailedPrecondition с текстом, называющим текущий
	// размер, запрошенное и предел.
	AddBlocks(ctx context.Context, id string, v4, v6 []string) (*CidrGroupRecord, error)
	// RemoveBlocks атомарно снимает блоки и приводит счётчики к фактическому
	// составу. Идемпотентно: снятие отсутствующего блока — успех без изменения.
	// Потолком НЕ гейтится — сужение обязано проходить всегда.
	RemoveBlocks(ctx context.Context, id string, v4, v6 []string) (*CidrGroupRecord, error)
}
