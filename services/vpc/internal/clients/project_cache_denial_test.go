// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package clients

// project_cache_denial_test.go — отказ, ВЫНЕСЕННЫЙ ОДНОМУ, не отдаётся другому.
//
// # Предмет
//
// Ключ кэша — идентификатор проекта; личности вызывающего в нём нет. Значит в
// кэш вправе попасть только тот исход, который одинаков для любого
// спрашивающего: ресурса нет, идентификатор негоден. Отказ в правах вычислен ДЛЯ
// КОНКРЕТНОГО вызывающего, и, сохранённый под таким ключом, он будет отдан
// следующему — то есть один арендатор получит решение, вынесенное не ему.
//
// Дефект был живым: клиент стал принимать любой «отказ ссылки» за «проекта нет»,
// а кэш клал это на окно TTL. Штатное окно 403/404 на СВОЁМ свежесозданном
// ресурсе (материализация прав идёт eventually-consistent — объявлено нормой в
// testing.md и api-conventions.md) фиксировалось как «проекта нет» для всех.
//
// Проба держит обе половины: отказ в правах не кэшируется, промах кэшируется.
// Без второй половины «не кэшируем» зеленело бы и на кэше, выключенном целиком.

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// countingProjects — источник, считающий обращения и всегда отвечающий одним и
// тем же отказом.
type countingProjects struct {
	calls atomic.Int64
	err   error
}

func (c *countingProjects) Exists(context.Context, string) (bool, error) {
	c.calls.Add(1)
	return false, c.err
}

// AccountOf — тот же вызов, что и Exists: счётчик обращений обязан считать
// ОДНО обращение, иначе проба про нагрузку на соседа мерила бы не то.
func (c *countingProjects) Describe(ctx context.Context, projectID string) (bool, string, error) {
	exists, err := c.Exists(ctx, projectID)
	if err != nil || !exists {
		return exists, "", err
	}
	return true, "acc-" + projectID, nil
}

func denialCache(up *countingProjects) *CachedProjectClient {
	return NewCachedProjectClient(up, ProjectCacheConfig{
		PositiveTTL: time.Minute,
		NegativeTTL: time.Minute,
		MaxSize:     16,
	})
}

// TestPermissionDenialIsNotCachedForOtherCallers — отказ в правах не оседает в
// кэше: второй вызывающий обязан дойти до владельца сам.
func TestPermissionDenialIsNotCachedForOtherCallers(t *testing.T) {
	up := &countingProjects{err: status.Error(codes.PermissionDenied, "no path")}
	c := denialCache(up)

	if _, err := c.Exists(context.Background(), "prj-0000000000000001"); err != nil {
		t.Fatalf("первый вызывающий получил ошибку вместо отказа ссылки: %v", err)
	}
	if _, err := c.Exists(context.Background(), "prj-0000000000000001"); err != nil {
		t.Fatalf("второй вызывающий получил ошибку вместо отказа ссылки: %v", err)
	}

	if got := up.calls.Load(); got != 2 {
		t.Errorf("обращений к владельцу %d, ожидалось 2: отказ в правах, вынесенный ПЕРВОМУ, "+
			"отдан второму из кэша — ключ личности не несёт, поэтому решение авторизации "+
			"кэшировать нельзя", got)
	}
}

// TestAbsenceIsCached — положительный близнец: промах кэшируется, иначе
// утверждение выше зеленело бы на кэше, выключенном целиком.
func TestAbsenceIsCached(t *testing.T) {
	up := &countingProjects{err: status.Error(codes.NotFound, "Project prj-x not found")}
	c := denialCache(up)

	for i := 0; i < 3; i++ {
		if _, err := c.Exists(context.Background(), "prj-0000000000000002"); err != nil {
			t.Fatalf("промах пришёл ошибкой: %v", err)
		}
	}
	if got := up.calls.Load(); got != 1 {
		t.Errorf("обращений к владельцу %d, ожидалось 1: отсутствие ресурса одинаково для "+
			"любого спрашивающего и обязано кэшироваться — иначе проба выше ничего не "+
			"утверждает", got)
	}
}
