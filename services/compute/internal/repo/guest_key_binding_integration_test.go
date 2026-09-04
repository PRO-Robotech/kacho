// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/ids"

	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
	"github.com/PRO-Robotech/kacho/services/compute/internal/repo"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
)

// seedGuestKey заводит ключ в названном проекте и возвращает его идентификатор.
func seedGuestKey(t *testing.T, ctx context.Context, r *repo.GuestAccessKeyRepo, projectID, name string) string {
	t.Helper()
	k := &domain.GuestAccessKey{
		ID:          ids.NewHyphenID("gak"),
		ProjectID:   projectID,
		Name:        name,
		PublicKey:   "ssh-ed25519 AAAA" + name,
		Fingerprint: "SHA256:" + name,
	}
	created, _, err := r.Insert(ctx, k)
	require.NoError(t, err)
	return created.ID
}

func seedInstanceWithKeys(t *testing.T, ctx context.Context, r *repo.InstanceRepo, projectID string, keyIDs []string) (*domain.Instance, error) {
	t.Helper()
	inID := ids.NewID(ids.PrefixInstance)
	in, _, err := r.Insert(ctx, &domain.Instance{
		ID: inID, Name: inID, ProjectID: projectID, CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
		ZoneID: "ru-central1-a", Status: domain.InstanceStatusRunning, FQDN: inID + ".auto.internal",
		InstanceKind: domain.InstanceKindVM, MachineTypeID: "mt-std2",
		EffectiveResources: domain.EffectiveResources{VCPU: 2, MemoryMiB: 8192},
		BootSource:         domain.BootSource{Type: "storage.image", ID: "img-x:22.04", ImageKind: domain.ImageKindStorageImage},
		GuestAccessKeyIDs:  keyIDs,
	})
	return in, err
}

// TestGuestKeyBinding_ForeignKeyNeverAttaches — ключ ЧУЖОГО проекта не
// привязывается, и отказ не отличает его от несуществующего.
//
// Положительный контроль обязателен и стоит первым: без него «не привязался»
// зеленело бы и на связи, которая не работает вовсе.
func TestGuestKeyBinding_ForeignKeyNeverAttaches(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	instRepo := repo.NewInstanceRepo(pool)
	keyRepo := repo.NewGuestAccessKeyRepo(pool)

	own := seedGuestKey(t, ctx, keyRepo, "proj-own", "key-own")
	foreign := seedGuestKey(t, ctx, keyRepo, "proj-other", "key-foreign")

	t.Run("свой ключ привязывается и виден в ответе", func(t *testing.T) {
		in, ierr := seedInstanceWithKeys(t, ctx, instRepo, "proj-own", []string{own})
		require.NoError(t, ierr)
		require.Equal(t, []string{own}, in.GuestAccessKeyIDs)

		// И читается тем же составом при повторном чтении — набор приезжает из
		// связи, а не из входа запроса.
		got, gerr := instRepo.Get(ctx, in.ID)
		require.NoError(t, gerr)
		require.Equal(t, []string{own}, got.GuestAccessKeyIDs)
	})

	t.Run("чужой ключ не привязывается, и машина не создаётся", func(t *testing.T) {
		_, ierr := seedInstanceWithKeys(t, ctx, instRepo, "proj-own", []string{foreign})
		require.ErrorIs(t, ierr, repo.ErrGuestKeyNotInProject)
	})

	t.Run("несуществующий ключ отвечает тем же отказом", func(t *testing.T) {
		_, ierr := seedInstanceWithKeys(t, ctx, instRepo, "proj-own", []string{ids.NewHyphenID("gak")})
		require.ErrorIs(t, ierr, repo.ErrGuestKeyNotInProject,
			"ответ «есть, но чужой» отличался бы от «нет» и сообщал бы про чужой проект")
	})

	t.Run("смесь своего и чужого не привязывает НИЧЕГО", func(t *testing.T) {
		_, ierr := seedInstanceWithKeys(t, ctx, instRepo, "proj-own", []string{own, foreign})
		require.ErrorIs(t, ierr, repo.ErrGuestKeyNotInProject)

		// Частичная привязка была бы хуже отказа: машина существовала бы с
		// набором, отличным от запрошенного, и вызывающий об этом не узнал бы.
		var n int64
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT count(*) FROM instance_guest_access_keys g
			   JOIN instances i ON i.id = g.instance_id
			  WHERE i.project_id = 'proj-own' AND g.guest_access_key_id = $1`, foreign,
		).Scan(&n))
		require.Zero(t, n, "чужой ключ не имеет права оказаться привязанным ни к одной машине проекта")
	})
}

// TestGuestKeyDelete_RefusesWithTheInstancesNamed — снятие ключа, который держат
// машины, отвергается ПЕРЕЧНЕМ этих машин.
func TestGuestKeyDelete_RefusesWithTheInstancesNamed(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	instRepo := repo.NewInstanceRepo(pool)
	keyRepo := repo.NewGuestAccessKeyRepo(pool)

	held := seedGuestKey(t, ctx, keyRepo, "proj-del", "key-held")
	free := seedGuestKey(t, ctx, keyRepo, "proj-del", "key-free")

	in, ierr := seedInstanceWithKeys(t, ctx, instRepo, "proj-del", []string{held})
	require.NoError(t, ierr)

	t.Run("занятый ключ не снимается и называет машину", func(t *testing.T) {
		derr := keyRepo.Delete(ctx, held)
		require.Error(t, derr)
		var inUse *repo.ErrGuestKeyInUse
		require.ErrorAs(t, derr, &inUse)
		require.Equal(t, []string{in.ID}, inUse.InstanceIDs,
			"отказ обязан назвать машины: без перечня арендатор выясняет радиус перебором")
		require.False(t, inUse.Truncated)
		require.True(t, strings.Contains(derr.Error(), in.ID))
	})

	// Положительный контроль: свободный ключ того же проекта снимается. Без него
	// «не снимается» зеленело бы на удалении, сломанном для всех ключей сразу.
	t.Run("свободный ключ снимается", func(t *testing.T) {
		require.NoError(t, keyRepo.Delete(ctx, free))
	})

	t.Run("после снятия ключа с машины он снимается", func(t *testing.T) {
		_, _, uerr := instRepo.Update(ctx, &domain.Instance{ID: in.ID, Name: in.ID, ProjectID: "proj-del"},
			false, []string{"guest_access_key_ids"})
		require.NoError(t, uerr, "пустой набор при названной маске — законное «снять все»")
		require.NoError(t, keyRepo.Delete(ctx, held))
	})
}
