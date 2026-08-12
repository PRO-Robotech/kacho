// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	stderrors "errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/ids"

	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	storageerr "github.com/PRO-Robotech/kacho/services/storage/internal/errors"
	"github.com/PRO-Robotech/kacho/services/storage/internal/repo/pg"
)

// sbSample — законный бэкенд целиком. Служит вторым полюсом каждой пары: без него
// отрицание зеленело бы на реализации, отвергающей вообще всё.
func sbSample(name string) *domain.StorageBackend {
	return &domain.StorageBackend{
		ID:             ids.NewHyphenID(domain.PrefixStorageBackend),
		Name:           name,
		Kind:           domain.BackendKindCephRBD,
		Description:    "кластер центрального региона",
		ZoneIDs:        []string{"ru-central1-a", "ru-central1-b"},
		Endpoint:       "cfg://ceph/central-1",
		CredentialsRef: domain.CredentialsRef("vault://kacho/storage/" + name),
		Status:         domain.BackendStatusActive,
	}
}

// iaText утверждает InvalidArgument-sentinel и возвращает текст без префикса.
func iaText(t *testing.T, err error) string {
	t.Helper()
	require.Error(t, err)
	require.True(t, stderrors.Is(err, storageerr.ErrInvalidArg), "want InvalidArgument, got %v", err)
	const prefix = "invalid argument: "
	require.True(t, strings.HasPrefix(err.Error(), prefix), "got %v", err)
	return err.Error()[len(prefix):]
}

// TestStorageBackendRegisteredAndReadBackIdentically (STOR-P-10) — бэкенд
// регистрируется и читается ОБОИМИ путями одинаково. Поле, живущее только в одном
// из двух чтений, — половина контракта, и расходится она молча.
func TestStorageBackendRegisteredAndReadBackIdentically(t *testing.T) {
	r := pg.NewStorageBackendRepo(newTestPool(t))
	ctx := context.Background()

	want := sbSample("ceph-central-1")
	created, err := r.Insert(ctx, want)
	require.NoError(t, err)
	require.Equal(t, want.ID, created.ID)
	require.True(t, strings.HasPrefix(created.ID, domain.PrefixStorageBackend+"-"),
		"id несёт hyphen-префикс семьи: %s", created.ID)
	require.Equal(t, domain.BackendStatusActive, created.Status)
	require.True(t, created.AcceptsNewBindings())
	require.False(t, created.CreatedAt.IsZero(), "момент создания проставляет БД")
	require.False(t, created.UpdatedAt.IsZero())

	got, err := r.Get(ctx, want.ID)
	require.NoError(t, err)
	require.Equal(t, created, got, "чтение по id отдаёт ровно то, что вернула запись")

	page, next, err := r.List(ctx, 50, "")
	require.NoError(t, err)
	require.Empty(t, next)
	require.Len(t, page, 1)
	require.Equal(t, created, page[0], "список несёт то же, что и чтение по id")
}

// TestStorageBackendGetNotFound — well-formed-но-нет → NotFound контрактного тона.
// В паре с положительным контролем: существующий бэкенд читается.
func TestStorageBackendGetNotFound(t *testing.T) {
	r := pg.NewStorageBackendRepo(newTestPool(t))
	ctx := context.Background()

	missing := ids.NewHyphenID(domain.PrefixStorageBackend)
	_, err := r.Get(ctx, missing)
	require.True(t, stderrors.Is(err, storageerr.ErrNotFound), "got %v", err)
	require.Equal(t, "StorageBackend "+missing+" not found", err.Error()[len("not found: "):])

	present := sbSample("ceph-present")
	_, err = r.Insert(ctx, present)
	require.NoError(t, err)
	_, err = r.Get(ctx, present.ID)
	require.NoError(t, err, "отрицание выше — про отсутствие строки, а не про нечитающий репозиторий")
}

// TestStorageBackendIdentityHeldByDB — уникальность id и имени держат индексы БД, а
// не проверка в коде. Каждое отрицание — с законным близнецом той же формы.
func TestStorageBackendIdentityHeldByDB(t *testing.T) {
	r := pg.NewStorageBackendRepo(newTestPool(t))
	ctx := context.Background()

	first := sbSample("ceph-uniq")
	_, err := r.Insert(ctx, first)
	require.NoError(t, err)

	// Имя — единственный человекочитаемый ключ бэкенда и уникально по установке.
	sameName := sbSample("ceph-uniq")
	_, err = r.Insert(ctx, sameName)
	require.True(t, stderrors.Is(err, storageerr.ErrAlreadyExists), "дубль имени, got %v", err)
	require.Equal(t, "storage backend with name ceph-uniq already exists",
		err.Error()[len("already exists: "):])

	_, err = r.Insert(ctx, sbSample("ceph-uniq-2"))
	require.NoError(t, err, "другое имя проходит — отказ выше про дубль, а не про вторую запись вообще")

	sameID := sbSample("ceph-uniq-3")
	sameID.ID = first.ID
	_, err = r.Insert(ctx, sameID)
	require.True(t, stderrors.Is(err, storageerr.ErrAlreadyExists), "дубль id, got %v", err)
	require.Equal(t, "StorageBackend "+first.ID+" already exists", err.Error()[len("already exists: "):])
}

// TestStorageBackendDictionariesHeldByDB — вид, состояние обращения и обязательность
// координаты энфорсятся ОГРАНИЧЕНИЯМИ БД (0015), а не разбором в репозитории. Пары
// обязательны: иначе проба зеленела бы на реализации, отвергающей всё подряд.
func TestStorageBackendDictionariesHeldByDB(t *testing.T) {
	r := pg.NewStorageBackendRepo(newTestPool(t))
	ctx := context.Background()

	badKind := sbSample("kind-bad")
	badKind.Kind = "S3_COMPAT"
	_, err := r.Insert(ctx, badKind)
	require.True(t, stderrors.Is(err, storageerr.ErrInvalidArg), "вид вне словаря, got %v", err)
	_, err = r.Insert(ctx, sbSample("kind-ok"))
	require.NoError(t, err)

	badStatus := sbSample("status-bad")
	badStatus.Status = "PAUSED"
	_, err = r.Insert(ctx, badStatus)
	require.True(t, stderrors.Is(err, storageerr.ErrInvalidArg), "состояние вне словаря, got %v", err)
	draining := sbSample("status-ok")
	draining.Status = domain.BackendStatusDraining
	created, err := r.Insert(ctx, draining)
	require.NoError(t, err)
	require.False(t, created.AcceptsNewBindings(), "выведенный из обращения новых привязок не принимает")

	noEndpoint := sbSample("endpoint-bad")
	noEndpoint.Endpoint = ""
	_, err = r.Insert(ctx, noEndpoint)
	require.True(t, stderrors.Is(err, storageerr.ErrInvalidArg), "пустая координата, got %v", err)

	noCreds := sbSample("creds-bad")
	noCreds.CredentialsRef = ""
	_, err = r.Insert(ctx, noCreds)
	require.True(t, stderrors.Is(err, storageerr.ErrInvalidArg), "пустая ссылка на учётные данные, got %v", err)

	_, err = r.Insert(ctx, sbSample("endpoint-creds-ok"))
	require.NoError(t, err, "заполненные координата и ссылка проходят")
}

// TestStorageBackendUpdateAppliesOnlyNamedFields — правка применяет ТОЛЬКО названные
// поля: nil-указатель означает «не менять», а не «обнулить». Правка, не назвавшая ни
// одного поля, не трогает и момент изменения: строка, которая не менялась, не вправе
// выглядеть изменённой.
func TestStorageBackendUpdateAppliesOnlyNamedFields(t *testing.T) {
	r := pg.NewStorageBackendRepo(newTestPool(t))
	ctx := context.Background()

	before, err := r.Insert(ctx, sbSample("ceph-upd"))
	require.NoError(t, err)

	name := "ceph-renamed"
	got, err := r.Update(ctx, before.ID, pg.StorageBackendUpdate{Name: &name})
	require.NoError(t, err)
	want := *before
	want.Name = name
	want.UpdatedAt = got.UpdatedAt
	require.Equal(t, &want, got, "названо имя — изменилось только имя")
	require.True(t, got.UpdatedAt.After(before.UpdatedAt), "названная правка двигает момент изменения")

	status := domain.BackendStatusDraining
	got, err = r.Update(ctx, before.ID, pg.StorageBackendUpdate{Status: &status})
	require.NoError(t, err)
	want.Status = status
	want.UpdatedAt = got.UpdatedAt
	require.Equal(t, &want, got, "названо состояние — имя предыдущей правки на месте")
	require.False(t, got.AcceptsNewBindings())

	zones := []string{"ru-central1-c"}
	ref := domain.CredentialsRef("vault://kacho/storage/rotated")
	got, err = r.Update(ctx, before.ID, pg.StorageBackendUpdate{ZoneIDs: &zones, CredentialsRef: &ref})
	require.NoError(t, err)
	want.ZoneIDs = zones
	want.CredentialsRef = ref
	want.UpdatedAt = got.UpdatedAt
	require.Equal(t, &want, got, "названы зоны и ссылка — координата и вид не тронуты")

	// Снять описание — тоже правка: пустая строка есть ЗНАЧЕНИЕ, а не отсутствие.
	empty := ""
	got, err = r.Update(ctx, before.ID, pg.StorageBackendUpdate{Description: &empty})
	require.NoError(t, err)
	want.Description = ""
	want.UpdatedAt = got.UpdatedAt
	require.Equal(t, &want, got, "названное пустым описание снимается, остальное на месте")

	touched := *got
	got, err = r.Update(ctx, before.ID, pg.StorageBackendUpdate{})
	require.NoError(t, err)
	require.Equal(t, &touched, got, "правка без названных полей не меняет ничего, включая момент изменения")

	_, err = r.Update(ctx, ids.NewHyphenID(domain.PrefixStorageBackend), pg.StorageBackendUpdate{Name: &name})
	require.True(t, stderrors.Is(err, storageerr.ErrNotFound), "правка отсутствующего → NotFound, got %v", err)
}

// TestStorageBackendDeleteRestrictedByBinding — бэкенд, на который ссылается ревизия
// привязки, не удаляется: держит ОГРАНИЧИТЕЛЬНАЯ внешняя связь (0015), а не счётчик
// в коде. Пара обязательна: бэкенд без привязок удаляется тем же вызовом — значит
// отказ выше про ссылку, а не про запрет удаления вообще.
func TestStorageBackendDeleteRestrictedByBinding(t *testing.T) {
	pool := newTestPool(t)
	r := pg.NewStorageBackendRepo(pool)
	br := pg.NewDiskTypeBindingRepo(pool)
	ctx := context.Background()

	referenced, err := r.Insert(ctx, sbSample("ceph-referenced"))
	require.NoError(t, err)
	bindSeedDiskType(t, pool, "block-del")
	_, err = br.Register(ctx, bindingSample("block-del", "ru-central1-a", referenced.ID))
	require.NoError(t, err)

	err = r.Delete(ctx, referenced.ID)
	require.Equal(t, "StorageBackend "+referenced.ID+" is in use", fpText(t, err))
	_, err = r.Get(ctx, referenced.ID)
	require.NoError(t, err, "бэкенд цел, пока на него ссылаются")

	free, err := r.Insert(ctx, sbSample("ceph-free"))
	require.NoError(t, err)
	require.NoError(t, r.Delete(ctx, free.ID), "бэкенд без привязок удаляется")
	_, err = r.Get(ctx, free.ID)
	require.True(t, stderrors.Is(err, storageerr.ErrNotFound))

	err = r.Delete(ctx, ids.NewHyphenID(domain.PrefixStorageBackend))
	require.True(t, stderrors.Is(err, storageerr.ErrNotFound), "удаление отсутствующего → NotFound, got %v", err)
}

// TestStorageBackendListCursor — курсорная пагинация по (created_at, id) ASC: строка
// не пропускается и не повторяется. Мусорный курсор → InvalidArgument, и это в паре
// с законным курсором, иначе отрицание зеленело бы на списке, отвергающем любой ввод.
func TestStorageBackendListCursor(t *testing.T) {
	r := pg.NewStorageBackendRepo(newTestPool(t))
	ctx := context.Background()

	const total = 5
	for i := 0; i < total; i++ {
		_, err := r.Insert(ctx, sbSample(fmt.Sprintf("ceph-page-%d", i)))
		require.NoError(t, err)
	}

	page, next, err := r.List(ctx, 2, "")
	require.NoError(t, err)
	require.Len(t, page, 2)
	require.NotEmpty(t, next)

	seen := map[string]struct{}{}
	for _, b := range page {
		seen[b.ID] = struct{}{}
	}
	token := next
	for token != "" {
		var chunk []*domain.StorageBackend
		chunk, token, err = r.List(ctx, 2, token)
		require.NoError(t, err)
		for _, b := range chunk {
			_, dup := seen[b.ID]
			require.False(t, dup, "курсор не повторяет строку: %s", b.ID)
			seen[b.ID] = struct{}{}
		}
	}
	require.Len(t, seen, total, "курсор прошёл все строки")

	_, _, err = r.List(ctx, 2, "не-курсор")
	require.Equal(t, "invalid page_token", iaText(t, err))
}
