// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// list_repositories_page_test.go — ListRepositories обязан отдавать СТРАНИЦУ:
// размером не больше запрошенного, за число обращений к хранилищу образов, не
// растущее с числом репозиториев реестра, и так, чтобы обход всех страниц дал
// каждый видимый репозиторий РОВНО один раз (объединение долговременных строк
// наложения с проекцией движка — не повод показать строку дважды).
package registry_test

import (
	"context"
	"encoding/base64"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	registry "github.com/PRO-Robotech/kacho/services/registry/internal/apps/kacho/api/registry"
	"github.com/PRO-Robotech/kacho/services/registry/internal/apps/kacho/shared/namepage"
	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
	regerrors "github.com/PRO-Robotech/kacho/services/registry/internal/errors"
)

// ---- фикстура: проекция движка + наложение, обе с детерминированным порядком ----

// pageZot — ZotClient c проекцией из фиксированного набора имён (ASC) и счётчиками
// обращений. Окно режется у источника — как настоящий адаптер (WindowByOffset).
type pageZot struct {
	mu sync.Mutex

	projected []string // имена, реально присутствующие в движке (ASC)
	tagCount  int32

	listCalls int // обращений ListRepositories (GlobalSearch)
	projCalls int // обращений RepositoryProjection (по одному repo)
}

func (z *pageZot) repoAt(registryID, name string) *domain.Repository {
	return &domain.Repository{
		RegistryID: registryID,
		Name:       name,
		TagCount:   z.tagCount,
		UpdatedAt:  time.Unix(1700000000, 0).UTC(),
	}
}

func (z *pageZot) ListRepositories(_ context.Context, q registry.RepoListQuery) ([]*domain.Repository, string, error) {
	z.mu.Lock()
	z.listCalls++
	names := append([]string(nil), z.projected...)
	z.mu.Unlock()

	window, next, err := namepage.WindowByOffset(names, q.PageSize, q.PageToken)
	if err != nil {
		return nil, "", err
	}
	out := make([]*domain.Repository, 0, len(window))
	for _, n := range window {
		out = append(out, z.repoAt(q.RegistryID, n))
	}
	return out, next, nil
}

func (z *pageZot) ListRepositoryNames(_ context.Context, _ string) ([]string, error) {
	z.mu.Lock()
	z.listCalls++
	names := append([]string(nil), z.projected...)
	z.mu.Unlock()
	return names, nil
}

func (z *pageZot) RepositoryProjection(_ context.Context, registryID, repository string) (*domain.Repository, error) {
	z.mu.Lock()
	z.projCalls++
	present := false
	for _, n := range z.projected {
		if n == repository {
			present = true
			break
		}
	}
	z.mu.Unlock()
	if !present {
		return nil, nil // долговременная строка без единого тега — проекции нет
	}
	return z.repoAt(registryID, repository), nil
}

func (z *pageZot) counts() (list, proj int) {
	z.mu.Lock()
	defer z.mu.Unlock()
	return z.listCalls, z.projCalls
}

func (z *pageZot) ListTags(context.Context, registry.TagListQuery) ([]*domain.Tag, string, error) {
	return nil, "", nil
}
func (z *pageZot) DeleteTag(context.Context, string, string, string) error { return nil }
func (z *pageZot) NamespaceEmpty(context.Context, string) (bool, error)    { return true, nil }
func (z *pageZot) RemoveNamespace(context.Context, string) error           { return nil }
func (z *pageZot) TriggerGC(context.Context, string) error                 { return nil }
func (z *pageZot) RepositoryEmpty(context.Context, string, string) (bool, error) {
	return true, nil
}
func (z *pageZot) CopyRepositoryTags(context.Context, string, string, string) error { return nil }
func (z *pageZot) PurgeRepositoryTags(context.Context, string, string) error        { return nil }
func (z *pageZot) Stats(context.Context, string) (*domain.RegistryStats, error) {
	return &domain.RegistryStats{}, nil
}
func (z *pageZot) ListReferrers(context.Context, string, string, string, string) ([]*domain.Referrer, error) {
	return nil, nil
}

// orderedCfg — наложение с ДЕТЕРМИНИРОВАННЫМ порядком (как настоящий репозиторий:
// ORDER BY created_at, name), в отличие от map-обхода общего мока.
type orderedCfg struct {
	rows []*domain.RepositoryConfig
}

func (c *orderedCfg) ListConfigs(context.Context, string) ([]*domain.RepositoryConfig, error) {
	return append([]*domain.RepositoryConfig(nil), c.rows...), nil
}

func (c *orderedCfg) GetConfig(_ context.Context, _, name string) (*domain.RepositoryConfig, error) {
	for _, r := range c.rows {
		if r.Name == name {
			return r, nil
		}
	}
	return nil, regerrors.ErrNotFound
}

func (c *orderedCfg) InsertConfig(context.Context, *domain.RepositoryConfig, ...registry.OutboxIntent) (*domain.RepositoryConfig, error) {
	return nil, regerrors.ErrUnavailable
}

func (c *orderedCfg) UpdateConfig(context.Context, registry.RepositoryConfigUpdate, ...registry.OutboxIntent) (*domain.RepositoryConfig, error) {
	return nil, regerrors.ErrUnavailable
}

func (c *orderedCfg) RekeyConfig(context.Context, string, string, string, ...registry.OutboxIntent) (*domain.RepositoryConfig, error) {
	return nil, regerrors.ErrUnavailable
}

func (c *orderedCfg) DeleteConfig(context.Context, string, string, ...registry.OutboxIntent) error {
	return regerrors.ErrUnavailable
}

// listFixture — реестр с pushedN репозиториями, у которых есть теги (все несут
// долговременную строку наложения), и emptyN долговременно-пустыми (строка есть,
// тегов нет — их в проекции движка НЕТ вовсе).
func listFixture(t *testing.T, pushedN, emptyN int) (*registry.UseCase, *pageZot, []string) {
	t.Helper()
	zot := &pageZot{tagCount: 3}
	cfg := &orderedCfg{}
	var want []string
	for i := 0; i < pushedN; i++ {
		name := fmt.Sprintf("pushed-%03d", i)
		zot.projected = append(zot.projected, name)
		cfg.rows = append(cfg.rows, &domain.RepositoryConfig{
			RegistryID: "reg-A", Name: name, Lifecycle: domain.LifecycleDurable,
			Visibility: domain.VisibilityPrivate, CreatedAt: time.Unix(1600000000, 0).UTC(),
		})
		want = append(want, name)
	}
	for i := 0; i < emptyN; i++ {
		name := fmt.Sprintf("empty-%03d", i)
		cfg.rows = append(cfg.rows, &domain.RepositoryConfig{
			RegistryID: "reg-A", Name: name, Lifecycle: domain.LifecycleDurable,
			Visibility: domain.VisibilityPrivate, CreatedAt: time.Unix(1600000000, 0).UTC(),
		})
		want = append(want, name)
	}
	uc := registry.New(&mockRepo{}, &mockRepo{}, cfg, zot, &mockIAM{}, &mockGeo{},
		&mockRepoReg{}, newMemOps(), "registry.kacho.local")
	return uc, zot, want
}

// Страница обязана быть не больше запрошенного размера. До фикса первая страница
// несла ВСЁ наложение реестра поверх окна.
func TestListRepositories_FirstPage_RespectsPageSize(t *testing.T) {
	uc, _, _ := listFixture(t, 40, 2)

	page, next, err := uc.ListRepositories(context.Background(),
		registry.RepoListQuery{RegistryID: "reg-A", PageSize: 10})
	require.NoError(t, err)
	require.LessOrEqual(t, len(page), 10,
		"страница обязана уложиться в page_size, а не вырасти до размера всего реестра")
	require.NotEmpty(t, next, "42 репозитория при page_size=10 → есть следующая страница")
}

// Число обращений к движку не должно расти с числом репозиториев реестра: иначе
// дешёвый ListRepositories(page_size=1) разворачивается в обход всего каталога.
func TestListRepositories_EngineCalls_BoundedByPage_NotByRegistrySize(t *testing.T) {
	const pageSize = 10
	uc, zot, _ := listFixture(t, 40, 2) // 42 репозитория в реестре

	_, _, err := uc.ListRepositories(context.Background(),
		registry.RepoListQuery{RegistryID: "reg-A", PageSize: pageSize})
	require.NoError(t, err)

	list, proj := zot.counts()
	total := list + proj
	t.Logf("обращений к движку на первую страницу: %d (список %d + поштучно %d) при 42 репозиториях и page_size=%d",
		total, list, proj, pageSize)
	require.LessOrEqualf(t, total, pageSize,
		"обращений к движку на одну страницу: %d (список %d + по одному репозиторию %d); "+
			"при 42 репозиториях и page_size=%d это обход каталога, а не страница",
		total, list, proj, pageSize)
}

// Обход всех страниц отдаёт каждый репозиторий РОВНО один раз: и те, что есть в
// движке, и долговременно-пустые. Дубль между страницами — такой же дефект, как
// пропажа: клиент, собирающий каталог постранично, получает неверный набор.
func TestListRepositories_PaginationCoversEachRepositoryExactlyOnce(t *testing.T) {
	uc, _, want := listFixture(t, 40, 2)

	seen := map[string]int{}
	token := ""
	for i := 0; i < 200; i++ { // предохранитель от бесконечного цикла
		page, next, err := uc.ListRepositories(context.Background(),
			registry.RepoListQuery{RegistryID: "reg-A", PageSize: 10, PageToken: token})
		require.NoError(t, err)
		require.LessOrEqual(t, len(page), 10, "каждая страница не больше page_size")
		for _, r := range page {
			seen[r.Name]++
		}
		token = next
		if token == "" {
			break
		}
	}
	require.Empty(t, token, "пагинация обязана завершиться")

	for _, name := range want {
		require.Equalf(t, 1, seen[name], "репозиторий %s встретился %d раз(а), ожидалось ровно 1", name, seen[name])
	}
	require.Len(t, seen, len(want), "лишних имён в выдаче быть не должно")
}

// Битый курсор — терминальный InvalidArgument, а не «начнём сначала»: молчаливый
// сброс на первую страницу превратил бы опечатку клиента в бесконечный обход.
func TestListRepositories_GarbagePageToken_Rejected(t *testing.T) {
	uc, _, _ := listFixture(t, 3, 1)

	for _, token := range []string{"!!!not-base64!!!", base64.StdEncoding.EncodeToString([]byte("x:0")),
		base64.StdEncoding.EncodeToString([]byte("no-lane-separator"))} {
		_, _, err := uc.ListRepositories(context.Background(),
			registry.RepoListQuery{RegistryID: "reg-A", PageSize: 10, PageToken: token})
		require.ErrorIs(t, err, regerrors.ErrInvalidArg, "курсор %q обязан быть отвергнут", token)
	}
}

// Долговременно-пустой репозиторий (строка наложения без единого тега) присутствует в
// выдаче и несёт нули проекции — A20. Опрашивать по нему движок поштучно не за чем:
// его имени в движке нет вовсе.
func TestListRepositories_DurableEmpty_PresentWithoutPerRepoLookup(t *testing.T) {
	uc, zot, _ := listFixture(t, 2, 1)

	page, _, err := uc.ListRepositories(context.Background(),
		registry.RepoListQuery{RegistryID: "reg-A", PageSize: 50})
	require.NoError(t, err)

	var empty *domain.Repository
	for _, r := range page {
		if r.Name == "empty-000" {
			empty = r
		}
	}
	require.NotNil(t, empty, "долговременно-пустой репозиторий обязан присутствовать в выдаче")
	require.Zero(t, empty.TagCount)
	require.Equal(t, domain.LifecycleDurable, empty.Lifecycle)

	_, proj := zot.counts()
	require.Zerof(t, proj, "поштучных обращений к проекции быть не должно, было %d", proj)
}
