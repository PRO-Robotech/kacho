// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package domain_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
)

// Класс диска несёт ПОЛИТИКУ, а не только косметику: ярус из закрытого словаря,
// состояние жизненного цикла и границы размера. Приёмка STOR-P-05, STOR-P-07..09.
//
// Почему ярус обязан быть закрытым словарём. Публичная поверхность не несёт
// инфра-лексики, и гейт проекции это держит — но он читает ИМЕНА полей, а не
// значения. Свободная строка яруса проходит сквозь него со значением вида
// "nvme-fast" или "pool-b-replicated", то есть открывает ровно тот канал, который
// гейт закрывает по именам.

func TestDiskTypeTier_ClosedDictionary(t *testing.T) {
	t.Parallel()

	valid := []domain.PerformanceTier{
		domain.TierCapacity, domain.TierBalanced, domain.TierFast,
		domain.TierSingle, domain.TierIOMax,
	}
	for _, tier := range valid {
		d := newValidDiskType()
		d.PerformanceTier = tier
		require.NoErrorf(t, d.Validate(), "ярус %q обязан приниматься", tier)
	}

	// Пустой ярус допустим: класс вправе его не объявлять.
	d := newValidDiskType()
	d.PerformanceTier = ""
	require.NoError(t, d.Validate(), "пустой ярус — законное состояние")

	// Отрицание в паре с положительным контролем выше: иначе проба зеленела бы
	// на реализации, отвергающей вообще всё.
	for _, bad := range []domain.PerformanceTier{"nvme-fast", "pool-b-replicated", "FAST ", "fast"} {
		d := newValidDiskType()
		d.PerformanceTier = bad
		require.Errorf(t, d.Validate(), "ярус вне словаря обязан отвергаться: %q", bad)
	}
}

func TestDiskTypeLifecycle_ClosedDictionary(t *testing.T) {
	t.Parallel()

	for _, lc := range []domain.DiskTypeLifecycle{
		domain.LifecycleActive, domain.LifecycleDeprecated, domain.LifecycleRetired,
	} {
		d := newValidDiskType()
		d.Lifecycle = lc
		require.NoErrorf(t, d.Validate(), "состояние %q обязано приниматься", lc)
	}

	for _, bad := range []domain.DiskTypeLifecycle{"", "active", "DISABLED", "DELETED"} {
		d := newValidDiskType()
		d.Lifecycle = bad
		require.Errorf(t, d.Validate(), "состояние вне словаря обязано отвергаться: %q", bad)
	}
}

// AcceptsNewVolumes отвечает на единственный вопрос, ради которого заведено
// состояние: можно ли создать на этом классе НОВЫЙ том. Существующие живут при
// любом состоянии — их не отзывают правкой справочника.
func TestDiskTypeLifecycle_OnlyActiveAcceptsNewVolumes(t *testing.T) {
	t.Parallel()

	cases := map[domain.DiskTypeLifecycle]bool{
		domain.LifecycleActive:     true,
		domain.LifecycleDeprecated: false,
		domain.LifecycleRetired:    false,
	}
	for lc, want := range cases {
		d := newValidDiskType()
		d.Lifecycle = lc
		require.Equalf(t, want, d.AcceptsNewVolumes(), "состояние %q", lc)
	}
}

func TestDiskTypeLimits_Validated(t *testing.T) {
	t.Parallel()

	const giB = int64(1) << 30

	// Положительный контроль: законные границы проходят.
	d := newValidDiskType()
	d.Limits = domain.SizeLimits{MinSizeBytes: giB, MaxSizeBytes: 16 * giB, SizeStepBytes: giB}
	require.NoError(t, d.Validate())

	// Границы не объявлены вовсе — законно: класс не обязан их сужать.
	d = newValidDiskType()
	d.Limits = domain.SizeLimits{}
	require.NoError(t, d.Validate())

	for name, lim := range map[string]domain.SizeLimits{
		"минимум больше максимума": {MinSizeBytes: 16 * giB, MaxSizeBytes: giB},
		"отрицательный минимум":    {MinSizeBytes: -1},
		"отрицательный максимум":   {MaxSizeBytes: -1},
		"отрицательный шаг":        {SizeStepBytes: -1},
		"минимум не кратен шагу":   {MinSizeBytes: giB + 1, MaxSizeBytes: 16 * giB, SizeStepBytes: giB},
		"максимум не кратен шагу":  {MinSizeBytes: giB, MaxSizeBytes: 16*giB + 1, SizeStepBytes: giB},
	} {
		d := newValidDiskType()
		d.Limits = lim
		require.Errorf(t, d.Validate(), "границы обязаны отвергаться: %s", name)
	}
}

// ValidateVolumeSize — предикат, которым класс судит размер тома. Он живёт на классе,
// а не в use-case тома: границы объявляет класс, и решать обязан он же.
func TestDiskTypeLimits_ValidateVolumeSize(t *testing.T) {
	t.Parallel()

	const giB = int64(1) << 30
	d := newValidDiskType()
	d.Limits = domain.SizeLimits{MinSizeBytes: giB, MaxSizeBytes: 16 * giB, SizeStepBytes: giB}

	require.NoError(t, d.ValidateVolumeSize(giB), "нижняя граница включающая")
	require.NoError(t, d.ValidateVolumeSize(16*giB), "верхняя граница включающая")
	require.NoError(t, d.ValidateVolumeSize(4*giB))

	require.Error(t, d.ValidateVolumeSize(giB/2), "меньше минимума")
	require.Error(t, d.ValidateVolumeSize(32*giB), "больше максимума")
	require.Error(t, d.ValidateVolumeSize(giB+1), "не кратен шагу")

	// Класс без объявленных границ не сужает ничего, но нулевой размер остаётся
	// незаконным — это инвариант тома, а не класса.
	free := newValidDiskType()
	require.NoError(t, free.ValidateVolumeSize(1))
	require.Error(t, free.ValidateVolumeSize(0))
}

// Способности класса — публичные булевы. Они публичны намеренно: иначе арендатор
// узнаёт об отсутствии снимков ОТКАЗОМ, уже написав код, и «полнота API без
// обнаружимости» оказывается хуже честного сужения.
func TestDiskTypeCapabilities_QueriedByName(t *testing.T) {
	t.Parallel()

	d := newValidDiskType()
	d.Capabilities = domain.Capabilities{Snapshots: true, CloneFromSnapshot: false}

	require.NoError(t, d.RequireCapability(domain.CapSnapshots))

	err := d.RequireCapability(domain.CapCloneFromSnapshot)
	require.Error(t, err)
	require.Contains(t, err.Error(), d.ID, "отказ обязан называть класс")
	require.Contains(t, err.Error(), string(domain.CapCloneFromSnapshot),
		"отказ обязан называть способность, которой нет")

	// Неизвестная способность — не «разрешено по умолчанию».
	require.Error(t, d.RequireCapability("teleportation"))
}

// newValidDiskType — класс, законный по ВСЕМ осям сразу.
//
// Имя здесь `block-balanced`, а не `Balanced`: имя класса судится единой формой
// имени ресурса (corevalidate.NameForm, #715), и заглавная буква ей не отвечает.
// Прежнее значение было законно, пока имя проверялось только длиной, — фикстура
// с заглавной уронила бы пробы ЯРУСА и ГРАНИЦ по причине, к их предмету
// отношения не имеющей.
func newValidDiskType() domain.DiskType {
	return domain.DiskType{
		ID:              "block-balanced",
		Name:            "block-balanced",
		Lifecycle:       domain.LifecycleActive,
		PerformanceTier: domain.TierBalanced,
	}
}
