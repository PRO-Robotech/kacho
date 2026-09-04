// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	"fmt"
	"sort"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/option"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// targetIdentity — сравнимый ключ цели: то, чем цель отличается от соседней по
// объявленным полям контракта. Порядок целей в ответе краем НЕ сохраняется
// (см. комментарий поля `TargetGroup.targets` в контракте — набор, не
// последовательность), поэтому сверка идёт по СОСТАВУ, а не по индексу.
func targetIdentity(t kacho.TargetRecord) string {
	if v, ok := t.InstanceID.Maybe(); ok {
		return fmt.Sprintf("instance=%s w=%d st=%s", string(v), t.Weight, t.Status)
	}
	if v, ok := t.NicID.Maybe(); ok {
		return fmt.Sprintf("nic=%s w=%d st=%s", string(v), t.Weight, t.Status)
	}
	switch {
	case t.IPRef != nil:
		return fmt.Sprintf("ipref=%s/%s w=%d st=%s",
			string(t.IPRef.SubnetID), string(t.IPRef.Address), t.Weight, t.Status)
	case t.ExternalIP != nil:
		return fmt.Sprintf("ext=%s w=%d st=%s", string(t.ExternalIP.Address), t.Weight, t.Status)
	}
	return "«ни одной формы идентичности»"
}

func targetComposition(states []kacho.TargetRecord) []string {
	out := make([]string, 0, len(states))
	for _, s := range states {
		out = append(out, targetIdentity(s))
	}
	sort.Strings(out)
	return out
}

// TestTG_ListProjectionMatchesGet — один и тот же ресурс, прочитанный ОБОИМИ
// путями, обязан совпадать по объявленным полям.
//
// Предмет: пустой массив в ответе читается вызывающим как ФАКТ о ресурсе
// («целей нет»), а не как «это чтение поле не заполняет». Клиент, ведущий своё
// состояние из списка — дешёвый и потому частый путь, — увидит «все цели
// удалены» и предложит создать их заново. Различить два смысла на проводе
// нечем: message у обоих чтений один.
//
// Расхождение проекций законно РОВНО там, где контракт назвал проекцию другой
// (отдельный message элемента списка). Здесь message общий, поэтому список
// обязан заполнять поле.
func TestTG_ListProjectionMatchesGet(t *testing.T) {
	repo, cleanup := newRepo(t, setupTestDB(t))
	defer cleanup()
	ctx := context.Background()

	const projectID = "prj01TGLP1234567890ll"

	// Две группы на одной странице: вторая существует, чтобы отказ «подгрузил
	// цели, но приписал их не той группе» отличался от отказа «не подгрузил
	// вовсе». Без неё оба выглядят одинаково.
	withTargets := newTG(projectID, "list-proj-with-targets")
	empty := newTG(projectID, "list-proj-empty")
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		_, err := w.TargetGroups().Insert(ctx, withTargets)
		require.NoError(t, err)
		_, err = w.TargetGroups().Insert(ctx, empty)
		require.NoError(t, err)
	})

	targets := []domain.Target{
		{InstanceID: option.MustNewOption(domain.InstanceID("epd0LPINST1")), Weight: 100},
		{NicID: option.MustNewOption(domain.NicID("e9b0LPNIC1")), Weight: 50},
		{
			IPRef:  &domain.TargetIPRef{SubnetID: "sub0LPSUB1", Address: "10.20.30.40"},
			Weight: 25,
		},
	}
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		n, err := w.TargetGroups().AddTargets(ctx, string(withTargets.ID), targets)
		require.NoError(t, err)
		require.Equal(t, len(targets), n)
	})

	rd, err := repo.Reader(ctx)
	require.NoError(t, err)
	defer func() { _ = rd.Close() }()

	got, err := rd.TargetGroups().Get(ctx, string(withTargets.ID))
	require.NoError(t, err)
	require.Len(t, got.TargetStates, len(targets), "предусловие: Get заполняет цели")

	page, _, err := rd.TargetGroups().List(ctx,
		kacho.TargetGroupFilter{ProjectID: projectID}, kacho.Pagination{})
	require.NoError(t, err)
	require.Len(t, page, 2, "предусловие: обе группы на странице")

	byID := map[string]*kacho.TargetGroupRecord{}
	for _, rec := range page {
		byID[string(rec.ID)] = rec
	}

	listed, ok := byID[string(withTargets.ID)]
	require.True(t, ok, "группа, прочитанная Get, обязана быть в списке")
	assert.Equal(t, targetComposition(got.TargetStates), targetComposition(listed.TargetStates),
		"состав целей обязан совпадать у обоих чтений одного ресурса: "+
			"пустой массив в списке утверждает «целей нет», а не «это чтение поле не заполняет»")
	assert.Len(t, listed.Targets, len(targets),
		"доменный взгляд заполняется вместе с lifecycle-взглядом (контракт fillTargets)")

	other, ok := byID[string(empty.ID)]
	require.True(t, ok)
	assert.Empty(t, other.TargetStates,
		"цели соседней группы не приписываются группе без целей — иначе подгрузка "+
			"на страницу сшивает строки не по своему ключу")
}

// TestTG_ListTargetsPerGroupCapped — потолок целей на группу остаётся потолком
// НА ГРУППУ и после того, как список стал подгружать цели одной выборкой на
// страницу.
//
// Предмет: одиночное чтение защищено `LIMIT MaxTargetsPerGroup` (CWE-770,
// безусловная материализация распухшей группы). Общая на страницу выборка
// применить тот же LIMIT «как есть» не может — он усечёт страницу целиком, и
// первая же большая группа съест квоту остальных. Проба требует, чтобы потолок
// считался ПО ГРУППЕ у обоих чтений.
func TestTG_ListTargetsPerGroupCapped(t *testing.T) {
	repo, cleanup := newRepo(t, setupTestDB(t))
	defer cleanup()
	ctx := context.Background()

	const projectID = "prj01TGCP1234567890ll"

	// Три группы, у каждой цели. Потолок домена (100) на порядок больше, поэтому
	// проверяется не сам предел, а то, что квота НЕ общая: 3 группы × 4 цели.
	const perGroup = 4
	groups := make([]*domain.TargetGroup, 0, 3)
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		for i := 0; i < 3; i++ {
			tg := newTG(projectID, fmt.Sprintf("cap-tg-%d", i))
			_, err := w.TargetGroups().Insert(ctx, tg)
			require.NoError(t, err)
			groups = append(groups, tg)
		}
	})
	for i, tg := range groups {
		batch := make([]domain.Target, 0, perGroup)
		for j := 0; j < perGroup; j++ {
			batch = append(batch, domain.Target{
				ExternalIP: &domain.TargetExternalIP{
					Address: domain.IPAddress(fmt.Sprintf("203.0.113.%d", i*10+j+1)),
				},
				Weight: 10,
			})
		}
		commitWriter(t, repo, func(w kacho.RepositoryWriter) {
			n, err := w.TargetGroups().AddTargets(ctx, string(tg.ID), batch)
			require.NoError(t, err)
			require.Equal(t, perGroup, n)
		})
	}

	rd, err := repo.Reader(ctx)
	require.NoError(t, err)
	defer func() { _ = rd.Close() }()

	page, _, err := rd.TargetGroups().List(ctx,
		kacho.TargetGroupFilter{ProjectID: projectID}, kacho.Pagination{})
	require.NoError(t, err)
	require.Len(t, page, 3)
	for _, rec := range page {
		assert.Len(t, rec.TargetStates, perGroup,
			"каждая группа страницы несёт СВОИ цели целиком: id=%s", rec.ID)
	}
}
