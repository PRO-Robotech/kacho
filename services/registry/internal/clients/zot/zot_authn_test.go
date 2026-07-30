// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package zot_test

// zot_authn_test.go — адаптер обязан предъявляться хранилищу слоёв.
//
// Хранилище слоёв больше не обслуживает анонимных: аутентификация включена в
// чарте, и это единственное, что отделяет чужой процесс в сети подов от
// перечисления, выгрузки, подмены и удаления содержимого ВСЕХ тенантов. Отсюда
// два наблюдаемых требования, и оба проверяются ниже: (1) настроенный адаптер
// предъявляет учётные данные НА КАЖДОМ запросе — по всем помощникам, а не только
// по тому, который вспомнили; (2) ненастроенный получает отказ и деградирует
// fail-closed, а не отдаёт пустую проекцию как «ничего нет».

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	zotclient "github.com/PRO-Robotech/kacho/services/registry/internal/clients/zot"
	regerrors "github.com/PRO-Robotech/kacho/services/registry/internal/errors"
)

const (
	authUser = "kacho-registry"
	authPass = "layer-store-credential"
)

// authGatedZot — хранилище слоёв, которое ОТКАЗЫВАЕТ без учётных данных (та
// посадка, которую теперь везёт чарт). Записывает, какие пути пришли без них.
type authGatedZot struct {
	mu           sync.Mutex
	unauthorized []string
	authorized   []string
}

func (z *authGatedZot) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		z.mu.Lock()
		if !ok || u != authUser || p != authPass {
			z.unauthorized = append(z.unauthorized, r.Method+" "+r.URL.Path)
			z.mu.Unlock()
			w.Header().Set("WWW-Authenticate", `Basic realm="zot"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		z.authorized = append(z.authorized, r.Method+" "+r.URL.Path)
		z.mu.Unlock()

		path := r.URL.Path
		switch {
		case path == "/v2/_zot/ext/search":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{"GlobalSearch": map[string]any{"Repos": []any{}}},
			})
		case path == "/v2/_catalog":
			_ = json.NewEncoder(w).Encode(map[string]any{"repositories": []string{"reg-A/app"}})
		case strings.HasSuffix(path, "/tags/list"):
			_ = json.NewEncoder(w).Encode(map[string]any{"name": "reg-A/app", "tags": []string{"v1"}})
		case strings.Contains(path, "/manifests/"):
			w.Header().Set("Docker-Content-Digest", "sha256:deadbeef")
			w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"config": map[string]any{"mediaType": "application/vnd.oci.image.config.v1+json"},
				"layers": []any{},
			})
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (z *authGatedZot) refusals() []string {
	z.mu.Lock()
	defer z.mu.Unlock()
	return append([]string(nil), z.unauthorized...)
}

func (z *authGatedZot) accepted() int {
	z.mu.Lock()
	defer z.mu.Unlock()
	return len(z.authorized)
}

// TestZotAdapter_PresentsCredentialsOnEveryHelper — учётные данные ставятся один
// раз на транспорт, поэтому их несут ВСЕ места сборки запроса. Тест ходит по
// разным помощникам (список тегов, каталог, GraphQL-проекция, HEAD/GET/DELETE
// манифеста) и требует НУЛЯ анонимных запросов.
func TestZotAdapter_PresentsCredentialsOnEveryHelper(t *testing.T) {
	z := &authGatedZot{}
	srv := z.server(t)
	cli := zotclient.New(srv.URL, zotclient.WithBasicAuth(authUser, authPass))

	exists, err := cli.RepoExists(t.Context(), "reg-A", "app")
	require.NoError(t, err)
	require.True(t, exists)

	names, err := cli.CatalogRepoNames(t.Context())
	require.NoError(t, err)
	require.Equal(t, []string{"reg-A/app"}, names)

	_, err = cli.ListRepositoryNames(t.Context(), "reg-A")
	require.NoError(t, err)

	require.NoError(t, cli.DeleteTag(t.Context(), "reg-A", "app", "v1"))

	require.Empty(t, z.refusals(),
		"the adapter sent anonymous requests to the layer store — every request-building helper "+
			"must carry the credential, not only the one that was remembered")
	require.Greater(t, z.accepted(), 3, "the fake store must have actually been exercised")
}

// TestZotAdapter_WithoutCredentials_FailsClosed — ненастроенный адаптер получает
// отказ хранилища и деградирует в фиксированный Unavailable: пустая проекция
// НИКОГДА не подменяет отказ (иначе «репозиториев нет» было бы неотличимо от
// «нас не пустили»).
func TestZotAdapter_WithoutCredentials_FailsClosed(t *testing.T) {
	z := &authGatedZot{}
	srv := z.server(t)
	cli := zotclient.New(srv.URL)

	_, err := cli.CatalogRepoNames(t.Context())
	require.ErrorIs(t, err, regerrors.ErrUnavailable)
	require.NotEmpty(t, z.refusals(), "the store must have seen (and refused) an anonymous request")
	require.Zero(t, z.accepted())
}
