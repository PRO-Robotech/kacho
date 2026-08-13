// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package securitygroup

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/listnarrow/narrowtest"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/kachomock"
)

// «Кем используется» обязано заполняться и на СПИСКЕ, а не только на карточке:
// колонка списка иначе показывала бы прочерк там, где карточка того же ресурса
// показывает потребителей, — то есть один предмет выглядел бы двумя разными.
//
// Пробы идут на подставном репозитории намеренно: их предмет — ПРОВОДКА
// (use-case зовёт обратную ссылку и кладёт её в записи, которые уедут наружу), а
// не SQL. Свойства самого запроса — граница проекта, потолок ответа, план —
// проверяются на настоящем Postgres в `repo/kacho/pg`.
//
// Подставной репозиторий воспроизводит ОБА свойства запроса (границу проекта и
// потолок), и пробы ниже это утверждают: дублёр, принимающий больше настоящего,
// делает невидимым ровно тот дефект, ради которого его подставляют.

func seedNetworkFor(t *testing.T, kr *kachomock.Repository, projectID, networkID, name string) {
	t.Helper()
	w, err := kr.Writer(context.Background())
	require.NoError(t, err)
	defer w.Abort()
	_, err = w.Networks().Insert(context.Background(), &domain.Network{
		ID:        networkID,
		ProjectID: projectID,
		Name:      domain.RcNameVPC(name),
	})
	require.NoError(t, err)
	require.NoError(t, w.Commit())
}

func seedNICHolding(t *testing.T, kr *kachomock.Repository, projectID, nicID, name string, sgIDs ...string) {
	t.Helper()
	w, err := kr.Writer(context.Background())
	require.NoError(t, err)
	defer w.Abort()
	_, err = w.NetworkInterfaces().Insert(context.Background(), &domain.NetworkInterface{
		ID:               nicID,
		ProjectID:        projectID,
		Name:             domain.RcNameVPC(name),
		SubnetID:         "sub_any",
		SecurityGroupIDs: sgIDs,
		MAC:              fmt.Sprintf("0e:00:00:00:%02x:%02x", len(nicID)%256, len(name)%256),
		Status:           domain.NIStatusAvailable,
	})
	require.NoError(t, err)
	require.NoError(t, w.Commit())
}

// TestSecurityGroupList_FillsUsedBy — список несёт потребителей, и несёт их
// ТОЛЬКО для строк, которые уедут вызывающему.
func TestSecurityGroupList_FillsUsedBy(t *testing.T) {
	kr := kachomock.NewRepository()
	seedNetworkFor(t, kr, "prj_1", "enp_net1", "net-one")
	seedSecurityGroupsLabeled(t, kr, "prj_1", "enp_net1", "sg_seen", "sg_hidden")
	seedNICHolding(t, kr, "prj_1", "nic_a", "nic-a", "sg_seen")
	seedNICHolding(t, kr, "prj_1", "nic_b", "nic-b", "sg_hidden")

	uc := NewListSecurityGroupsUseCase(kr, narrowtest.Allowing("sg_seen"))
	sgs, _, err := uc.Execute(narrowtest.Caller(), SecurityGroupFilter{ProjectID: "prj_1"}, Pagination{})
	require.NoError(t, err)
	require.Len(t, sgs, 1)
	require.Equal(t, "sg_seen", sgs[0].ID)
	require.Len(t, sgs[0].UsedBy, 1, "видимая группа обязана нести своего потребителя")
	assert.Equal(t, kacho.SecurityGroupReferrerNIC, sgs[0].UsedBy[0].Type)
	assert.Equal(t, "nic_a", sgs[0].UsedBy[0].ID)
	assert.Equal(t, "nic-a", sgs[0].UsedBy[0].Name)
}

// TestSecurityGroupList_UsedByVanishesWhenReferenceIsReleased — вторая половина
// на том же наборе: интерфейс отпускает группу, потребитель уходит. Без неё
// проба выше зеленела бы на коде, который возвращает потребителя всегда.
func TestSecurityGroupList_UsedByVanishesWhenReferenceIsReleased(t *testing.T) {
	kr := kachomock.NewRepository()
	seedNetworkFor(t, kr, "prj_1", "enp_net1", "net-one")
	seedSecurityGroupsLabeled(t, kr, "prj_1", "enp_net1", "sg_seen")
	seedNICHolding(t, kr, "prj_1", "nic_a", "nic-a", "sg_seen")

	uc := NewListSecurityGroupsUseCase(kr, narrowtest.AllowingAll())
	sgs, _, err := uc.Execute(narrowtest.Caller(), SecurityGroupFilter{ProjectID: "prj_1"}, Pagination{})
	require.NoError(t, err)
	require.Len(t, sgs, 1)
	require.Len(t, sgs[0].UsedBy, 1)

	// Интерфейс остаётся, ссылку отпускает.
	seedNICHolding(t, kr, "prj_1", "nic_a", "nic-a")

	sgs, _, err = uc.Execute(narrowtest.Caller(), SecurityGroupFilter{ProjectID: "prj_1"}, Pagination{})
	require.NoError(t, err)
	require.Len(t, sgs, 1)
	assert.Empty(t, sgs[0].UsedBy, "снятая ссылка обязана убрать потребителя и на списке")
}

// TestSecurityGroupUsedBy_MockHonoursProjectBoundaryAndCap — дублёр обязан быть
// не снисходительнее настоящего.
//
// Проба стоит здесь, а не в интеграции, именно потому, что её предмет — САМ
// ДУБЛЁР: если он покажет чужого потребителя или отдаст весь набор, то каждая
// unit-проба, опирающаяся на него, будет зелёной на поведении, которого
// настоящий репозиторий не производит.
func TestSecurityGroupUsedBy_MockHonoursProjectBoundaryAndCap(t *testing.T) {
	kr := kachomock.NewRepository()
	seedNetworkFor(t, kr, "prj_1", "enp_net1", "net-one")
	seedSecurityGroupsLabeled(t, kr, "prj_1", "enp_net1", "sg_x")

	// Чужой проект держит ту же группу — и не показывается.
	seedNICHolding(t, kr, "prj_other", "nic_foreign", "nic-foreign", "sg_x")

	rd, err := kr.Reader(context.Background())
	require.NoError(t, err)
	defer func() { _ = rd.Close() }()

	refs, err := rd.SecurityGroups().ReferrersFor(context.Background(), []string{"sg_x"})
	require.NoError(t, err)
	assert.Empty(t, refs["sg_x"], "потребитель чужого проекта не показывается и в дублёре")

	// Положительный контроль на том же дублёре: свой — показывается.
	seedNICHolding(t, kr, "prj_1", "nic_own", "nic-own", "sg_x")
	rd2, err := kr.Reader(context.Background())
	require.NoError(t, err)
	defer func() { _ = rd2.Close() }()
	refs, err = rd2.SecurityGroups().ReferrersFor(context.Background(), []string{"sg_x"})
	require.NoError(t, err)
	require.Len(t, refs["sg_x"], 1)
	assert.Equal(t, "nic_own", refs["sg_x"][0].ID)

	// Потолок: сверх предела дублёр обязан обрезать так же, как запрос.
	for i := 0; i < kacho.SecurityGroupUsedByFetch+5; i++ {
		seedNICHolding(t, kr, "prj_1", fmt.Sprintf("nic_%03d", i), fmt.Sprintf("nic-%03d", i), "sg_x")
	}
	rd3, err := kr.Reader(context.Background())
	require.NoError(t, err)
	defer func() { _ = rd3.Close() }()
	refs, err = rd3.SecurityGroups().ReferrersFor(context.Background(), []string{"sg_x"})
	require.NoError(t, err)
	assert.Len(t, refs["sg_x"], kacho.SecurityGroupUsedByFetch,
		"дублёр обязан ограничивать ответ тем же потолком, что и запрос")
}
