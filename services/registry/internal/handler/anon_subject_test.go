// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// anon_subject_test.go — вызывающий БЕЗ личности не обслуживается ScopeFiltered
// RPC реестра. Эти RPC per-RPC interceptor-Check не проходят вовсе (ScopeFiltered:
// interceptor отдаёт passthrough ДО извлечения субъекта), поэтому отсечь безымянный
// запрос обязан сам хендлер — и обязан делать это БЕЗУСЛОВНО: и когда порт authz
// подан, и когда его нет (breakglass). Второй линии обороны за этими RPC не стоит.
package handler

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	"github.com/PRO-Robotech/kacho/pkg/operations"

	registryv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/registry/v1"
	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
)

// subjectRecorder — Authorizer, разрешающий ровно одному субъекту. Моделирует
// реальный кластер, где субъект уровня начальной загрузки держит всё: если хендлер
// подставит его безымянному вызывающему, Check ответит «да» и запрос пройдёт.
type subjectRecorder struct {
	mu       sync.Mutex
	allowFor string   // единственный субъект, которому отвечаем allow
	subjects []string // субъекты, с которыми хендлер реально пришёл в Check
}

func (s *subjectRecorder) Check(_ context.Context, subject, _, _ string) (bool, error) {
	s.mu.Lock()
	s.subjects = append(s.subjects, subject)
	s.mu.Unlock()
	return subject == s.allowFor, nil
}

func (s *subjectRecorder) seen() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.subjects...)
}

// bootstrapSubject — субъект, в который вырождается ПУСТОЙ ctx: fallback-принципал
// {system, bootstrap} форматируется как "user:bootstrap".
const bootstrapSubject = "user:bootstrap"

// Безымянный вызывающий (ctx без принципала) НЕ должен получать каталог. До фикса
// он получал: fallback-принципал форматировался в субъект уровня начальной загрузки,
// Check отвечал «да», каталог уезжал наружу.
func TestHandler_ListRepositories_NoPrincipal_Rejected(t *testing.T) {
	zot := &fakeZotH{repos: []*domain.Repository{
		{RegistryID: validReg, Name: "app", TagCount: 2},
		{RegistryID: validReg, Name: "web", TagCount: 1},
	}}
	az := &subjectRecorder{allowFor: bootstrapSubject}
	h := newTestHandler(zot, az)

	resp, err := h.ListRepositories(context.Background(),
		&registryv1.ListRepositoriesRequest{RegistryId: validReg})
	require.Nil(t, resp, "безымянный вызывающий не должен получить ни одной строки каталога")
	require.Equal(t, codes.PermissionDenied, codeOf(t, err),
		"запрос без личности обязан быть отклонён")
	require.Empty(t, az.seen(),
		"вопрос о правах не задаётся вовсе: субъекта нет, подставлять нечего (пришли с %v)", az.seen())
}

// То же для мутации: DeleteTag — ScopeFiltered, значит безымянный вызывающий доходил
// до per-repo Check под субъектом начальной загрузки и удалял тег.
func TestHandler_DeleteTag_NoPrincipal_Rejected(t *testing.T) {
	zot := &fakeZotH{}
	az := &subjectRecorder{allowFor: bootstrapSubject}
	h := newTestHandler(zot, az)

	op, err := h.DeleteTag(context.Background(),
		&registryv1.DeleteTagRequest{RegistryId: validReg, Repository: "app", Tag: "v1"})
	require.Nil(t, op, "Operation не создаётся для запроса без личности")
	require.Equal(t, codes.PermissionDenied, codeOf(t, err))
	require.Zero(t, zot.deleteTagCalls, "движок не трогается")
}

// Отсечение безусловно: при отсутствующем порте authz (breakglass) хендлер обходит
// Check — но обход прав не есть обход личности. Безымянный вызывающий отвергается и
// здесь, иначе «отсекаем, когда фильтр подключён» = не отсекаем.
func TestHandler_ListRepositories_NoPrincipal_RejectedEvenWithoutAuthorizer(t *testing.T) {
	zot := &fakeZotH{repos: []*domain.Repository{{RegistryID: validReg, Name: "app", TagCount: 2}}}
	h := newTestHandler(zot, nil) // breakglass: порт authz не подан

	resp, err := h.ListRepositories(context.Background(),
		&registryv1.ListRepositoriesRequest{RegistryId: validReg})
	require.Nil(t, resp)
	require.Equal(t, codes.PermissionDenied, codeOf(t, err),
		"breakglass снимает проверку ПРАВ, а не требование ЛИЧНОСТИ")
}

// Именованная анонимность — то же самое, что её отсутствие: edge помечает запрос без
// credential'а зарезервированным словом, и оно не является личностью.
func TestHandler_ListRepositories_AnonymousPrincipal_Rejected(t *testing.T) {
	zot := &fakeZotH{repos: []*domain.Repository{{RegistryID: validReg, Name: "app", TagCount: 2}}}
	az := &subjectRecorder{allowFor: "user:" + operations.AnonymousPrincipalID}
	h := newTestHandler(zot, az)

	ctx := operations.WithPrincipal(context.Background(), operations.Principal{
		Type: "system", ID: operations.AnonymousPrincipalID, DisplayName: "anonymous",
	})
	_, err := h.ListRepositories(ctx, &registryv1.ListRepositoriesRequest{RegistryId: validReg})
	require.Equal(t, codes.PermissionDenied, codeOf(t, err))
	require.Empty(t, az.seen())
}

// Контроль: названный вызывающий по-прежнему обслуживается — фикс отсекает
// безымянность, а не всех подряд.
func TestHandler_ListRepositories_NamedPrincipal_StillServed(t *testing.T) {
	zot := &fakeZotH{repos: []*domain.Repository{{RegistryID: validReg, Name: "app", TagCount: 2}}}
	az := &subjectRecorder{allowFor: "user:usr-carol"}
	h := newTestHandler(zot, az)

	resp, err := h.ListRepositories(carolCtx(), &registryv1.ListRepositoriesRequest{RegistryId: validReg})
	require.NoError(t, err)
	require.Len(t, resp.GetRepositories(), 1)
	require.Contains(t, az.seen(), "user:usr-carol")
}

// CheckMany — та же дверь, выведенная из Check (см. manyFromOne).
func (s *subjectRecorder) CheckMany(
	ctx context.Context, subject, relation, objectType string, objectIDs []string,
) ([]string, error) {
	return manyFromOne{one: s.Check}.checkMany(ctx, subject, relation, objectType, objectIDs)
}
