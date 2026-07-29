// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// list_repositories_engine_seam_test.go — перечисление репозиториев через НАСТОЯЩИЙ
// адаптер движка (не мок порта): use-case поверх zot-клиента, который ходит в
// httptest-движок. Шов между «что помнит движок» и «что знает наложение» — то место,
// где строка может потеряться незаметно для обеих сторон по отдельности: у адаптера
// нет наложения, поэтому он не может решать, жив ли ресурс без тегов, а use-case видит
// только то, что адаптер отдал.
//
// Классы строк, каждый обязан быть в выдаче ровно один раз (кроме последнего):
//  1. наложение + теги          — долговременный, обогащён полями наложения;
//  2. наложение + движок ПОМНИТ имя, тегов нет — долговременный, переживший пустоту;
//  3. наложение + движок имени НЕ знает        — долговременный, ни разу не пушенный;
//  4. теги без наложения                       — эфемерный (register-on-first-push);
//  5. движок помнит имя, тегов нет, наложения нет — не ресурс, в выдаче быть НЕ должно.
package registry_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	registry "github.com/PRO-Robotech/kacho/services/registry/internal/apps/kacho/api/registry"
	zotclient "github.com/PRO-Robotech/kacho/services/registry/internal/clients/zot"
	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
)

// engineRepo — состояние одного репозитория в движке: имя, которое движок помнит
// (GlobalSearch), и теги (ImageList). tags == nil — имя помнится, содержимого нет
// (все теги удалены, сборка мусора запись ещё не сняла).
type engineRepo struct {
	name string
	tags []string
}

// engineStub — минимальный search-ext GraphQL движка: отвечает на GlobalSearch
// (перечень имён + агрегаты) и ImageList (теги одного репозитория). Ровно те два
// запроса, из которых адаптер строит проекцию.
func engineStub(t *testing.T, repos []engineRepo) *httptest.Server {
	t.Helper()
	byName := make(map[string][]string, len(repos))
	for _, r := range repos {
		byName[r.name] = r.tags
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(body.Query, "{GlobalSearch"):
			out := make([]map[string]any, 0, len(repos))
			for _, rp := range repos {
				out = append(out, map[string]any{
					"Name": rp.name, "Size": "100",
					"LastUpdated": "2026-05-05T00:00:00Z", "DownloadCount": 7,
				})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{"GlobalSearch": map[string]any{"Repos": out}},
			})
		case strings.HasPrefix(body.Query, "{ImageList"):
			repo := body.Query[strings.Index(body.Query, `"`)+1:]
			repo = repo[:strings.Index(repo, `"`)]
			results := make([]map[string]any, 0, len(byName[repo]))
			for _, tag := range byName[repo] {
				results = append(results, map[string]any{
					"Tag": tag, "Digest": "sha256:" + tag, "MediaType": "application/vnd.oci.image.manifest.v1+json",
					"Size": "10", "DownloadCount": 1,
					"PushTimestamp": "2026-05-05T00:00:00Z", "LastPullTimestamp": "2026-05-06T00:00:00Z",
					"Manifests": []map[string]any{{"ArtifactType": "application/vnd.oci.image.config.v1+json"}},
				})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{"ImageList": map[string]any{"Results": results}},
			})
		default:
			http.Error(w, "unexpected query", http.StatusBadRequest)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// durableRow — строка наложения (сам ресурс: он существует, пока существует она).
func durableRow(name, description string) *domain.RepositoryConfig {
	return &domain.RepositoryConfig{
		RegistryID: "reg-A", Name: name, Description: description,
		Lifecycle: domain.LifecycleDurable, Visibility: domain.VisibilityPrivate,
		CreatedAt: time.Unix(1600000000, 0).UTC(),
	}
}

// Перечисление обязано покрыть ВСЕ классы строк. Класс, который терялся: строка
// наложения жива, движок ещё помнит имя, а тегов нет — она не попадала ни в полосу
// «наложение без имени в движке», ни в окно проекции (адаптер прятал репозиторий без
// тегов, не зная про наложение). Ресурс существует (его отдаёт поштучное чтение), а в
// списке его нет.
func TestListRepositories_EngineSeam_CoversEveryRowClass(t *testing.T) {
	srv := engineStub(t, []engineRepo{
		{name: "reg-A/with-tags", tags: []string{"v1"}},  // класс 1
		{name: "reg-A/kept-empty"},                       // класс 2 — терявшийся
		{name: "reg-A/ephemeral", tags: []string{"v9"}},  // класс 4
		{name: "reg-A/ghost"},                            // класс 5
		{name: "reg-B/foreign", tags: []string{"other"}}, // чужой namespace — не наш
	})
	cfg := &orderedCfg{rows: []*domain.RepositoryConfig{
		durableRow("with-tags", "первый"),
		durableRow("kept-empty", "пережил пустоту"),
		durableRow("never-pushed", "ни разу не пушен"), // класс 3
	}}
	uc := registry.New(&mockRepo{}, &mockRepo{}, cfg, zotclient.New(srv.URL), &mockIAM{}, &mockGeo{},
		&mockRepoReg{}, newMemOps(), "registry.kacho.local")

	got := map[string]*domain.Repository{}
	token := ""
	for i := 0; i < 50; i++ { // предохранитель от бесконечного обхода
		page, next, err := uc.ListRepositories(context.Background(),
			registry.RepoListQuery{RegistryID: "reg-A", PageSize: 2, PageToken: token})
		require.NoError(t, err)
		for _, r := range page {
			require.NotContainsf(t, got, r.Name, "репозиторий %s пришёл дважды", r.Name)
			got[r.Name] = r
		}
		token = next
		if token == "" {
			break
		}
	}
	require.Empty(t, token, "пагинация обязана завершиться")

	require.Containsf(t, got, "kept-empty",
		"долговременный репозиторий, чьё имя движок ещё помнит, а тегов нет, обязан быть в выдаче: "+
			"его строка наложения жива, поштучное чтение его отдаёт — а перечисление потеряло. Выдача: %v", keysOf(got))
	require.Equal(t, domain.LifecycleDurable, got["kept-empty"].Lifecycle)
	require.Equal(t, "пережил пустоту", got["kept-empty"].Description, "поля наложения обязаны доехать")
	require.Zero(t, got["kept-empty"].TagCount, "тегов нет — счётчик нулевой")

	require.Contains(t, got, "with-tags", "долговременный с тегами")
	require.Equal(t, int32(1), got["with-tags"].TagCount)
	require.Equal(t, "первый", got["with-tags"].Description)

	require.Contains(t, got, "never-pushed", "долговременный, имени которого движок не знает")
	require.Equal(t, domain.LifecycleDurable, got["never-pushed"].Lifecycle)

	require.Contains(t, got, "ephemeral", "проекция без наложения — эфемерный репозиторий")
	require.Equal(t, domain.LifecycleEphemeral, got["ephemeral"].Lifecycle)
	require.Equal(t, domain.VisibilityPrivate, got["ephemeral"].Visibility)

	require.NotContainsf(t, got, "ghost",
		"имя без тегов и без строки наложения ресурсом не является и в выдаче быть не должно; выдача: %v", keysOf(got))
	require.NotContains(t, got, "foreign", "чужой namespace не наш")

	require.Len(t, got, 4, "ровно четыре класса-ресурса, выдача: %v", keysOf(got))
}

func keysOf(m map[string]*domain.Repository) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
