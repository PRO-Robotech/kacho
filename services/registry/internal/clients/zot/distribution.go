// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package zot

// distribution.go — Distribution-API операции zot-адаптера: catalog/tags-чтение,
// удаление тега/манифеста, GC-триггер, infra-Stats namespace, precondition пустоты
// namespace. Всё поверх низкоуровневых HTTP-помощников (httpclient.go).

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"golang.org/x/sync/errgroup"

	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
	regerrors "github.com/PRO-Robotech/kacho/services/registry/internal/errors"
)

// catalogResponse — тело GET /v2/_catalog.
type catalogResponse struct {
	Repositories []string `json:"repositories"`
}

// tagsResponse — тело GET /v2/<repo>/tags/list.
type tagsResponse struct {
	Name string   `json:"name"`
	Tags []string `json:"tags"`
}

// namespaceRepos читает GET /v2/_catalog и возвращает full-repo-имена (с namespace-
// префиксом) реестра registryID. Любой HTTP-сбой → ErrUnavailable (fail-closed).
func (c *Client) namespaceRepos(ctx context.Context, registryID string) ([]string, error) {
	var cat catalogResponse
	if err := c.getJSON(ctx, "/v2/_catalog", &cat); err != nil {
		return nil, err
	}
	prefix := registryID + "/"
	var out []string
	for _, name := range cat.Repositories {
		if strings.HasPrefix(name, prefix) {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out, nil
}

// repoTags читает GET /v2/<full-repo>/tags/list. 404 (repo нет / GC-нут) → пустой
// список (грациозный dangling-ref, не ошибка); прочий HTTP-сбой → ErrUnavailable.
func (c *Client) repoTags(ctx context.Context, fullRepo string) ([]string, error) {
	var tr tagsResponse
	err := c.getJSON(ctx, "/v2/"+repoPath(fullRepo)+"/tags/list", &tr)
	if err != nil {
		if errors.Is(err, errNotFound) {
			return nil, nil
		}
		return nil, err
	}
	sort.Strings(tr.Tags)
	return tr.Tags, nil
}

// DeleteTag удаляет тег/манифест: сначала резолвит digest тега (HEAD), затем
// DELETE /manifests/<digest>. Отсутствующий тег/манифест → идемпотентный success
// (async-retry worker'а не залипает). zot недоступен → ErrUnavailable.
func (c *Client) DeleteTag(ctx context.Context, registryID, repository, tag string) error {
	if err := c.ready(); err != nil {
		return err
	}
	fullRepo := registryID + "/" + repository
	digest, err := c.headManifest(ctx, fullRepo, tag)
	if err != nil {
		if errors.Is(err, errNotFound) {
			return nil // тег уже отсутствует — идемпотентно
		}
		return err
	}
	delErr := c.do(ctx, http.MethodDelete, "/v2/"+repoPath(fullRepo)+"/manifests/"+digest, nil, nil)
	if errors.Is(delErr, errNotFound) {
		return nil // манифест уже снят — идемпотентно
	}
	return delErr
}

// NamespaceEmpty сообщает, пуст ли namespace реестра: ни у одного repo с префиксом
// <registryID>/ нет ни одного тега. Судится СОДЕРЖИМОЕ, а не каталог: каталог движка
// (zot v2.1.20) помнит имя repo после снятия последнего тега и имя с одной открытой
// загрузкой, а снять такое имя через API движка нельзя — маршрута удаления repo у него
// нет. Проверка по каталогу поэтому отказывала в удалении опустошённого реестра
// навсегда (kacho#3327). Предикат тот же, что у RepositoryEmpty (DeleteRepository):
// реестр пуст ровно тогда, когда пуст каждый его repo.
//
// zot недоступен либо теги хоть одного repo не прочитаны → ErrUnavailable (fail-closed:
// Delete-precondition НЕ трактует ошибку как «пусто»). Найденный тег — ответ «непусто»
// независимо от сбоев соседних чтений: содержимое доказано.
func (c *Client) NamespaceEmpty(ctx context.Context, registryID string) (bool, error) {
	if err := c.ready(); err != nil {
		return false, err
	}
	repos, err := c.namespaceRepos(ctx, registryID)
	if err != nil {
		return false, err
	}
	return c.noneTagged(ctx, repos)
}

// noneTagged читает теги каждого repo ограниченным fan-out'ом (zotFanout) и сообщает,
// что ни у одного тегов нет. После первого найденного тега новые чтения не начинаются;
// начатые доходят до конца сами — без отмены контекста, чтобы отменённые соседи не
// писали в журнал ложных отказов движка.
func (c *Client) noneTagged(ctx context.Context, fullRepos []string) (bool, error) {
	var tagged atomic.Bool
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(zotFanout)
	for _, full := range fullRepos {
		if tagged.Load() {
			break
		}
		g.Go(func() error {
			if tagged.Load() {
				return nil
			}
			tags, terr := c.repoTags(gctx, full)
			if terr != nil {
				return terr
			}
			if len(tags) > 0 {
				tagged.Store(true)
			}
			return nil
		})
	}
	werr := g.Wait()
	if tagged.Load() {
		return false, nil
	}
	if werr != nil {
		return false, werr
	}
	return true, nil
}

// RemoveNamespace — шаг снятия storage-namespace в async-Delete. Отдельного объекта
// namespace в zot нет (repo адресуются полным путём), маршрута удаления repo — тоже:
// у движка есть лишь DELETE манифеста, блоба и загрузки, а перечислить блобы и
// загрузки repo через API нельзя. Поэтому шаг подтверждает пустоту по содержимому
// (NamespaceEmpty) и завершается; имена без тегов остаются движку — его сборщик мусора
// снимает неиспользуемые блобы и устаревшие загрузки после gcDelay, а repo, чьи блобы
// сняты все, — вместе с ними (zot v2.1.20, pkg/storage/gc/gc.go, removeUnreferencedBlobs).
// Непусто → ErrFailedPrecondition; zot недоступен → ErrUnavailable.
func (c *Client) RemoveNamespace(ctx context.Context, registryID string) error {
	if err := c.ready(); err != nil {
		return err
	}
	empty, err := c.NamespaceEmpty(ctx, registryID)
	if err != nil {
		return err
	}
	if !empty {
		return regerrors.ErrFailedPrecondition
	}
	return nil
}

// TriggerGC форсирует garbage collection namespace. Реальная рекламация unreferenced-
// блобов исполняется native-scheduler'ом zot по расписанию; ad-hoc HTTP-триггера у zot
// нет, поэтому trigger проверяет достижимость zot (/v2/ handshake) и подтверждает
// (идемпотентно). zot недоступен → ErrUnavailable (fail-closed).
func (c *Client) TriggerGC(ctx context.Context, registryID string) error {
	if err := c.ready(); err != nil {
		return err
	}
	return c.do(ctx, http.MethodGet, "/v2/", nil, nil)
}

// Stats возвращает инфра-статистику namespace (repo/tag count, суммарный размер,
// число уникальных блобов) — только для Internal-API (:9091). Размер/блобы считаются
// из манифестов (config.size + layers[].size, уникальные digest'ы). Манифест, который
// не удалось прочитать, пропускается (best-effort). zot недоступен → ErrUnavailable.
func (c *Client) Stats(ctx context.Context, registryID string) (*domain.RegistryStats, error) {
	if err := c.ready(); err != nil {
		return nil, err
	}
	fullNames, err := c.namespaceRepos(ctx, registryID)
	if err != nil {
		return nil, err
	}
	stats := &domain.RegistryStats{RegistryID: registryID, RepositoryCount: int32(len(fullNames))} // #nosec G115 -- repo count of one registry, bounded well below int32 max

	// Собираем (full-repo, tag) пары; И per-repo tags, И манифесты читаем bounded-
	// concurrency fan-out'ом (cap blobScopeConcurrency) — namespace с тысячами repo/тегов
	// иначе последовательно прочёл бы тысячи round-trip'ов на один Stats-вызов, упираясь
	// в inbound-дедлайн Internal-RPC. blobs/tagCount/pairs под mutex'ом. Любой tags-сбой →
	// ErrUnavailable (fail-closed: не отдаём частичную статистику).
	type repoTagPair struct{ full, tag string }
	var (
		mu    sync.Mutex
		pairs []repoTagPair
	)
	tg, tgctx := errgroup.WithContext(ctx)
	tg.SetLimit(blobScopeConcurrency)
	for _, full := range fullNames {

		tg.Go(func() error {
			tags, terr := c.repoTags(tgctx, full)
			if terr != nil {
				return terr
			}
			mu.Lock()
			stats.TagCount += int32(len(tags)) // #nosec G115 -- per-repo tag count, bounded well below int32 max
			for _, tag := range tags {
				pairs = append(pairs, repoTagPair{full: full, tag: tag})
			}
			mu.Unlock()
			return nil
		})
	}
	if err := tg.Wait(); err != nil {
		return nil, err
	}

	blobs := map[string]int64{}
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(blobScopeConcurrency)
	for _, p := range pairs {

		g.Go(func() error {
			if gctx.Err() != nil {
				return nil
			}
			mb, merr := c.getManifest(gctx, p.full, p.tag)
			if merr != nil {
				return nil // best-effort: не валим Stats из-за одного манифеста
			}
			mu.Lock()
			if mb.Config.Digest != "" {
				blobs[mb.Config.Digest] = mb.Config.Size
			}
			for _, l := range mb.Layers {
				if l.Digest != "" {
					blobs[l.Digest] = l.Size
				}
			}
			mu.Unlock()
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	for _, sz := range blobs {
		stats.TotalSizeBytes += sz
	}
	stats.BlobCount = int64(len(blobs))
	return stats, nil
}
