// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo_test

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"

	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
	"github.com/PRO-Robotech/kacho/services/compute/internal/ports"
	"github.com/PRO-Robotech/kacho/services/compute/internal/repo"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
)

// Обновление каталога размеров — единственное место в compute и storage, где
// ПРОЧИТАННЫЙ РАНЕЕ снимок строки уезжал в БД целиком.
//
// Мутация асинхронная, поэтому «две одновременные правки» не требуют двух
// администраторов: клиент получает только Operation, и два последовательных
// вызова одного скрипта (сначала вывести размер из эксплуатации, потом поправить
// метки) исполняются разными исполнителями без гарантии порядка. Каждый читал
// строку, сливал СВОЮ маску в памяти и писал ВСЕ изменяемые колонки из своего
// снимка. Кто писал вторым, тот возвращал чужое поле к прочитанному значению — и
// обе операции завершались успехом. Потеря молчаливая.
//
// Особенно неприятно на поле состояния: возврат размера в доступные снова
// пропускает его через гейт создания машины, а устаревшие ресурсы запекаются в
// новую машину неизменяемым эхом.
//
// Проверяется НАБЛЮДАЕМОЕ: после двух правок с НЕПЕРЕСЕКАЮЩИМИСЯ масками в строке
// обязаны быть ОБА изменения. Вреду не нужна конкуренция за одно поле — достаточно
// того, что писался весь снимок.

// mtUpdateFixture — размер в исходном состоянии: доступен, метка «gp».
func mtUpdateFixture(t *testing.T, ctx context.Context, r *repo.MachineTypeRepo, name string) *domain.MachineType {
	t.Helper()
	mt := newMachineType(name, domain.MachineTypeFamilyStandard, 0)
	created, err := r.Insert(ctx, mt)
	require.NoError(t, err)
	require.Equal(t, domain.MachineTypeStatusAvailable, created.Status)
	return created
}

// TestIntegration_MachineTypeUpdate_DisjointMasksDoNotClobberEachOther —
// последовательные правки с непересекающимися масками.
func TestIntegration_MachineTypeUpdate_DisjointMasksDoNotClobberEachOther(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	r := repo.NewMachineTypeRepo(pool)
	mt := mtUpdateFixture(t, ctx, r, "std-disjoint-1")

	// Правка A: вывести размер из эксплуатации.
	retired := domain.MachineTypeStatusRetired
	_, err = r.Update(ctx, mt.ID, ports.MachineTypeUpdate{Status: &retired})
	require.NoError(t, err)

	// Правка B: поменять метки. Маска НЕ содержит состояния.
	_, err = r.Update(ctx, mt.ID, ports.MachineTypeUpdate{
		Labels: map[string]string{"tier": "memory"}, LabelsSet: true,
	})
	require.NoError(t, err)

	got, err := r.Get(ctx, mt.ID)
	require.NoError(t, err)
	require.Equal(t, domain.MachineTypeStatusRetired, got.Status,
		"правка меток вернула размер в доступные: писался весь прочитанный снимок, "+
			"а не названные маской колонки — снятый с эксплуатации размер снова проходит гейт создания машины")
	require.Equal(t, "memory", got.Labels["tier"], "правка меток не применилась")
}

// TestIntegration_MachineTypeUpdate_ConcurrentDisjointMasksBothSurvive — та же
// потеря под настоящей конкуренцией: два писателя стартуют одновременно.
//
// Здесь порядок не определён, поэтому утверждение симметрично: чем бы ни
// закончилась гонка, ОБА непересекающихся изменения обязаны быть в строке.
func TestIntegration_MachineTypeUpdate_ConcurrentDisjointMasksBothSurvive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	r := repo.NewMachineTypeRepo(pool)
	mt := mtUpdateFixture(t, ctx, r, "std-disjoint-2")

	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, 2)

	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		retired := domain.MachineTypeStatusRetired
		_, errs[0] = r.Update(ctx, mt.ID, ports.MachineTypeUpdate{Status: &retired})
	}()
	go func() {
		defer wg.Done()
		<-start
		_, errs[1] = r.Update(ctx, mt.ID, ports.MachineTypeUpdate{
			Labels: map[string]string{"tier": "memory"}, LabelsSet: true,
		})
	}()
	close(start)
	wg.Wait()

	require.NoError(t, errs[0])
	require.NoError(t, errs[1])

	got, err := r.Get(ctx, mt.ID)
	require.NoError(t, err)
	require.Equal(t, domain.MachineTypeStatusRetired, got.Status,
		"состояние потеряно под конкуренцией: правка меток переписала колонку, которой не касалась")
	require.Equal(t, "memory", got.Labels["tier"],
		"метки потеряны под конкуренцией: правка состояния переписала колонку, которой не касалась")
}

// TestIntegration_MachineTypeUpdate_UntouchedColumnsKeepTheirValues — обратная
// сторона: правка одной колонки не трогает остальные (иначе «не терять чужое»
// можно было бы выполнить, не записав ничего).
func TestIntegration_MachineTypeUpdate_UntouchedColumnsKeepTheirValues(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	r := repo.NewMachineTypeRepo(pool)
	mt := mtUpdateFixture(t, ctx, r, "std-disjoint-3")

	desc := "only the description moves"
	updated, err := r.Update(ctx, mt.ID, ports.MachineTypeUpdate{Description: &desc})
	require.NoError(t, err)
	require.Equal(t, desc, updated.Description)
	require.Equal(t, mt.EffectiveResources, updated.EffectiveResources, "ресурсы не назывались маской")
	require.Equal(t, mt.AvailableZones, updated.AvailableZones, "зоны не назывались маской")
	require.Equal(t, mt.Status, updated.Status, "состояние не называлось маской")
	require.Equal(t, mt.Labels, updated.Labels, "метки не назывались маской")
	require.Equal(t, mt.Name, updated.Name, "имя неизменяемо")
}

// TestIntegration_MachineTypeUpdate_MissingRowIsNotFound — исчезнувшая строка
// отвечает отсутствием, а не тихим нулём затронутых строк.
func TestIntegration_MachineTypeUpdate_MissingRowIsNotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	r := repo.NewMachineTypeRepo(pool)
	desc := "x"
	_, err = r.Update(ctx, "mt-doesnotexist00000", ports.MachineTypeUpdate{Description: &desc})
	require.ErrorIs(t, err, ports.ErrNotFound)
}
