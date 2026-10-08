// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package publicnotice — общее use-case'ов чтения извещений арендатором
// `NoticeService` (приёмка NTF-5 Р16; замысел issue-2924 З13, З14): порты
// выборки и сужения, разбор области, сужение затронутых ресурсов.
//
// Use-case на метод — подпакеты `list`, `listbyaccount`, `get`, `getbyaccount`.
// Методы освобождены у края (`<exempt>`): право судит `authzcheck.RequireScope`
// в каждом из них, после проверок формы и до выборки.
package publicnotice

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/corelib/authz"
	corevalidate "github.com/PRO-Robotech/corelib/validate"

	notifyv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/notify/v1"

	"github.com/PRO-Robotech/kacho/services/notify/internal/apps/kacho/api/notice"
	"github.com/PRO-Robotech/kacho/services/notify/internal/apps/kacho/api/notice/paging"
	"github.com/PRO-Robotech/kacho/services/notify/internal/apps/kacho/authzcheck"
)

// RelationRead — отношение права на область запроса и на каждый объект
// затронутого ресурса (Р16): `v_get` вызывающего.
const RelationRead = "v_get"

// ActionRead — действие пакетного вопроса сужения ссылок.
const ActionRead = "get"

// Reader — порт выборки с предикатом видимости Р16 в WHERE (З13 п.1, п.2).
// Видимое из проекта P — аудитория P либо «все аккаунты»; из аккаунта A —
// аудитория A, проект с записанным аккаунтом A либо «все аккаунты».
type Reader interface {
	// ListVisible — страница видимых из области извещений, keyset (created_at, id).
	ListVisible(ctx context.Context, scope notice.Scope, p notice.Page) ([]notice.Notice, error)
	// GetVisible — извещение, видимое из области; notice.ErrNotFound — нет такого
	// либо невидимо: два случая не различаются.
	GetVisible(ctx context.Context, scope notice.Scope, id string) (notice.Notice, error)
}

// Narrower — порт сужения ссылок (`*listnarrow.Narrower`, собранный
// authzwiring.NewListNarrower): видимые субъекту id одного типа.
type Narrower interface {
	Visible(ctx context.Context, subject, resourceType, action, relation string, ids []string) ([]string, error)
}

// Deps — зависимости use-case'ов чтения.
type Deps struct {
	Reader   Reader
	Checker  authz.CheckClient
	Narrower Narrower
}

// ProjectScope — область по проекту: обязательность, затем форма (Р16).
func ProjectScope(id string) (notice.Scope, error) {
	return scopeOf(notice.ScopeProject, "project_id", id)
}

// AccountScope — область по аккаунту: обязательность, затем форма (Р16).
func AccountScope(id string) (notice.Scope, error) {
	return scopeOf(notice.ScopeAccount, "account_id", id)
}

func scopeOf(typ, field, id string) (notice.Scope, error) {
	if id == "" {
		return notice.Scope{}, status.Errorf(codes.InvalidArgument, "%s: required", field)
	}
	if err := corevalidate.ResourceID(typ, "", id); err != nil {
		return notice.Scope{}, err
	}
	return notice.Scope{Type: typ, ID: id}, nil
}

// Scope — объект вопроса о праве на область.
func Scope(s notice.Scope) authzcheck.Scope { return authzcheck.Scope{Type: s.Type, ID: s.ID} }

// Page — выборка страницы ПОСЛЕ проверок формы, разбора страницы и права,
// которые use-case делает сам, в этом порядке (З13 п.2, З16 п.2).
func Page(ctx context.Context, d Deps, scope notice.Scope, pg paging.Request) (*notifyv1.ListNoticesResponse, error) {
	rows, err := d.Reader.ListVisible(ctx, scope, pg.Query())
	if err != nil {
		return nil, notice.StorageFailure()
	}
	rows, next := paging.Cut(pg, rows, notice.Key)
	out := &notifyv1.ListNoticesResponse{NextPageToken: next}
	for _, n := range rows {
		out.Notices = append(out.Notices, notice.Public(n, nil))
	}
	return out, nil
}

// One — выборка одного извещения с видимостью и сужением ссылок ПОСЛЕ проверок
// формы и права. Невидимое и несуществующее — один отказ побайтно (З13 п.1).
func One(ctx context.Context, d Deps, scope notice.Scope, id string) (*notifyv1.Notice, error) {
	n, err := d.Reader.GetVisible(ctx, scope, id)
	if errors.Is(err, notice.ErrNotFound) {
		return nil, notice.NotFound(id)
	}
	if err != nil {
		return nil, notice.StorageFailure()
	}
	refs, err := narrow(ctx, d.Narrower, n.Affected)
	if err != nil {
		return nil, err
	}
	return notice.Public(n, refs), nil
}

// narrow оставляет ссылку, если её id есть в выдаче сужателя её типа (З13 п.3):
// один пакетный вопрос на каждый различный тип, порядок оставшихся — порядок
// оператора, повтор ссылки проверяется как каждая (З13 п.4).
func narrow(ctx context.Context, n Narrower, refs []notice.Ref) ([]notice.Ref, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	subject, err := authzcheck.Subject(ctx)
	if err != nil {
		return nil, err
	}
	var types []string
	byType := map[string][]string{}
	for _, r := range refs {
		if _, seen := byType[r.Type]; !seen {
			types = append(types, r.Type)
		}
		byType[r.Type] = append(byType[r.Type], r.ID)
	}
	visible := map[notice.Ref]bool{}
	for _, t := range types {
		got, err := n.Visible(ctx, subject, t, ActionRead, RelationRead, byType[t])
		if err != nil {
			return nil, authzcheck.PeerUnavailable()
		}
		for _, id := range got {
			visible[notice.Ref{Type: t, ID: id}] = true
		}
	}
	var out []notice.Ref
	for _, r := range refs {
		if visible[r] {
			out = append(out, r)
		}
	}
	return out, nil
}
