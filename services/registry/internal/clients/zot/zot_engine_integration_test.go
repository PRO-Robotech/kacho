// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build integration

// zot_engine_integration_test.go — адаптер против НАСТОЯЩЕГО движка той же версии,
// что развёртывает чарт (services/registry/deploy/values.yaml, zot.image.tag).
//
// Зачем настоящий движок. Подставной zot пакетных проб (zot_client_test.go) воспроизводит
// то, что о движке ЗАПИСАНО, а дефект kacho#3327 жил ровно в незаписанном: каталог
// движка помнит имя репозитория после снятия последнего тега и имя, в которое лишь
// открыли загрузку. Проверка пустоты реестра читала каталог, поэтому опустошённый
// реестр не удалялся никогда. Здесь это свойство движка ИЗМЕРЯЕТСЯ (предпосылка), и
// на том же движке судится предикат: имена без тегов содержимым не являются, тег —
// является.
//
// Контейнер один на пакет: тестовая функция одна, шаги — подтестами по порядку
// (internal/repohygiene/containerperpackage_test.go).
package zot_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	zotclient "github.com/PRO-Robotech/kacho/services/registry/internal/clients/zot"
	regerrors "github.com/PRO-Robotech/kacho/services/registry/internal/errors"
)

// zotEngineImage — образ движка той же версии, что объявляет чарт службы.
const zotEngineImage = "ghcr.io/project-zot/zot-linux-amd64:v2.1.20"

// zotEngineConfig — конфигурация движка в той же форме, что собирает чарт
// (statefulset-zot.yaml): локальное хранилище, расширение search включено.
const zotEngineConfig = `{
  "distSpecVersion": "1.1.0",
  "storage": {"rootDirectory": "/var/lib/registry", "dedupe": false},
  "http": {"address": "0.0.0.0", "port": "5000"},
  "log": {"level": "info"},
  "extensions": {"search": {"enable": true}}
}`

func TestZotEngine_NamespaceEmptyJudgesContentNotCatalog(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()

	ctr, err := testcontainers.Run(ctx, zotEngineImage,
		testcontainers.WithExposedPorts("5000/tcp"),
		testcontainers.WithFiles(testcontainers.ContainerFile{
			Reader:            strings.NewReader(zotEngineConfig),
			ContainerFilePath: "/etc/zot/config.json",
			FileMode:          0o644,
		}),
		testcontainers.WithWaitStrategyAndDeadline(3*time.Minute,
			wait.ForHTTP("/v2/").WithPort("5000/tcp").WithStatusCodeMatcher(func(s int) bool { return s == http.StatusOK })),
	)
	testcontainers.CleanupContainer(t, ctr)
	require.NoError(t, err, "старт движка %s", zotEngineImage)

	endpoint, err := ctr.PortEndpoint(ctx, "5000/tcp", "http")
	require.NoError(t, err)
	eng := &engine{t: t, base: endpoint, http: &http.Client{Timeout: 30 * time.Second}}
	cli := zotclient.New(endpoint)

	// Соседи с содержимым: чужой реестр и реестр, чей id начинается с id предмета.
	// Ни тот, ни другой не делают предмет непустым.
	eng.pushImage(ctx, "regb/other", "v1")
	eng.pushImage(ctx, "regab/x", "v1")

	t.Run("реестр с тегом — не пуст", func(t *testing.T) {
		eng.pushImage(ctx, "rega/app", "latest")
		empty, err := cli.NamespaceEmpty(ctx, "rega")
		require.NoError(t, err)
		require.False(t, empty, "тег у репозитория — содержимое: удаление реестра обязано отказать")
		repoEmpty, err := cli.RepositoryEmpty(ctx, "rega", "app")
		require.NoError(t, err)
		require.False(t, repoEmpty)
	})

	t.Run("предпосылка: каталог помнит имена без содержимого", func(t *testing.T) {
		require.NoError(t, cli.DeleteTag(ctx, "rega", "app", "latest"))
		eng.openUpload(ctx, "rega/up")
		names := eng.catalog(ctx)
		require.Contains(t, names, "rega/app",
			"движок %s: после снятия последнего тега имя репозитория остаётся в каталоге — "+
				"на этом свойстве проверка по каталогу и отказывала", zotEngineImage)
		require.Contains(t, names, "rega/up",
			"движок %s: имя с одной открытой загрузкой есть в каталоге", zotEngineImage)
		require.Empty(t, eng.tags(ctx, "rega/app"), "теги rega/app сняты")
		require.Empty(t, eng.tags(ctx, "rega/up"), "у rega/up тегов не было")
	})

	t.Run("снятые теги и открытая загрузка — реестр пуст", func(t *testing.T) {
		empty, err := cli.NamespaceEmpty(ctx, "rega")
		require.NoError(t, err)
		require.True(t, empty, "в каталоге остались лишь имена без тегов — содержимого нет, удаление реестра завершается")
		require.NoError(t, cli.RemoveNamespace(ctx, "rega"), "шаг снятия пространства подтверждает ту же пустоту")
		for _, repo := range []string{"app", "up"} {
			repoEmpty, err := cli.RepositoryEmpty(ctx, "rega", repo)
			require.NoError(t, err)
			require.True(t, repoEmpty, "repo %s без тегов пуст для DeleteRepository", repo)
		}
	})

	t.Run("соседи с содержимым остаются непустыми", func(t *testing.T) {
		for _, reg := range []string{"regb", "regab"} {
			empty, err := cli.NamespaceEmpty(ctx, reg)
			require.NoError(t, err)
			require.False(t, empty, "реестр %s несёт тег", reg)
		}
	})

	t.Run("тег, залитый в опустошённый реестр, снова делает его непустым", func(t *testing.T) {
		eng.pushImage(ctx, "rega/up", "late")
		empty, err := cli.NamespaceEmpty(ctx, "rega")
		require.NoError(t, err)
		require.False(t, empty, "повторная проверка удаления обязана увидеть дозалитый тег")
		require.ErrorIs(t, cli.RemoveNamespace(ctx, "rega"), regerrors.ErrFailedPrecondition)
	})
}

// engine — прямой OCI-клиент к движку: заливка образа, открытая загрузка и чтение
// каталога. Адаптер этого не умеет (заливку ведёт плоскость данных), а предпосылку
// пробы нужно измерять мимо проверяемого кода.
type engine struct {
	t    *testing.T
	base string
	http *http.Client
}

// pushImage заливает однослойный образ repo:tag (слой уникален — блобы соседей не делятся).
func (e *engine) pushImage(ctx context.Context, repo, tag string) {
	e.t.Helper()
	layer := make([]byte, 64)
	_, err := rand.Read(layer)
	require.NoError(e.t, err)
	layerDigest := e.uploadBlob(ctx, repo, layer)
	cfg := []byte(fmt.Sprintf(
		`{"architecture":"amd64","os":"linux","rootfs":{"type":"layers","diff_ids":[%q]},"config":{}}`, layerDigest))
	cfgDigest := e.uploadBlob(ctx, repo, cfg)
	manifest, err := json.Marshal(map[string]any{
		"schemaVersion": 2,
		"mediaType":     "application/vnd.oci.image.manifest.v1+json",
		"config": map[string]any{
			"mediaType": "application/vnd.oci.image.config.v1+json", "digest": cfgDigest, "size": len(cfg),
		},
		"layers": []any{map[string]any{
			"mediaType": "application/vnd.oci.image.layer.v1.tar+gzip", "digest": layerDigest, "size": len(layer),
		}},
	})
	require.NoError(e.t, err)
	resp := e.do(ctx, http.MethodPut, e.base+"/v2/"+repo+"/manifests/"+tag,
		"application/vnd.oci.image.manifest.v1+json", manifest)
	require.Equal(e.t, http.StatusCreated, resp.code, "PUT манифеста %s:%s: %s", repo, tag, resp.body)
}

// uploadBlob — монолитная заливка блоба: POST открывает загрузку, PUT ?digest= её закрывает.
func (e *engine) uploadBlob(ctx context.Context, repo string, blob []byte) string {
	e.t.Helper()
	sum := sha256.Sum256(blob)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	loc := e.openUpload(ctx, repo)
	u, err := url.Parse(loc)
	require.NoError(e.t, err)
	if !u.IsAbs() {
		base, berr := url.Parse(e.base)
		require.NoError(e.t, berr)
		u = base.ResolveReference(u)
	}
	q := u.Query()
	q.Set("digest", digest)
	u.RawQuery = q.Encode()
	resp := e.do(ctx, http.MethodPut, u.String(), "application/octet-stream", blob)
	require.Equal(e.t, http.StatusCreated, resp.code, "PUT блоба %s: %s", repo, resp.body)
	return digest
}

// openUpload открывает загрузку блоба и возвращает её Location, не закрывая её.
func (e *engine) openUpload(ctx context.Context, repo string) string {
	e.t.Helper()
	resp := e.do(ctx, http.MethodPost, e.base+"/v2/"+repo+"/blobs/uploads/", "", nil)
	require.Equal(e.t, http.StatusAccepted, resp.code, "POST загрузки %s: %s", repo, resp.body)
	require.NotEmpty(e.t, resp.location, "у открытой загрузки нет Location")
	return resp.location
}

func (e *engine) catalog(ctx context.Context) []string {
	e.t.Helper()
	resp := e.do(ctx, http.MethodGet, e.base+"/v2/_catalog", "", nil)
	require.Equal(e.t, http.StatusOK, resp.code, "каталог: %s", resp.body)
	var cat struct {
		Repositories []string `json:"repositories"`
	}
	require.NoError(e.t, json.Unmarshal(resp.body, &cat))
	return cat.Repositories
}

// tags читает теги repo; 404 — тегов нет.
func (e *engine) tags(ctx context.Context, repo string) []string {
	e.t.Helper()
	resp := e.do(ctx, http.MethodGet, e.base+"/v2/"+repo+"/tags/list", "", nil)
	if resp.code == http.StatusNotFound {
		return nil
	}
	require.Equal(e.t, http.StatusOK, resp.code, "теги %s: %s", repo, resp.body)
	var tl struct {
		Tags []string `json:"tags"`
	}
	require.NoError(e.t, json.Unmarshal(resp.body, &tl))
	return tl.Tags
}

type engineResp struct {
	code     int
	body     []byte
	location string
}

func (e *engine) do(ctx context.Context, method, target, contentType string, body []byte) engineResp {
	e.t.Helper()
	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	require.NoError(e.t, err)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := e.http.Do(req)
	require.NoError(e.t, err, "%s %s", method, target)
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	require.NoError(e.t, err)
	return engineResp{code: resp.StatusCode, body: raw, location: resp.Header.Get("Location")}
}
