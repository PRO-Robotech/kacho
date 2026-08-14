// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package network

// Группа правил по умолчанию создаётся БЕЗУСЛОВНО: ни поля запроса, ни настройки
// оператора, которые бы это отменяли, больше нет.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЗДЕСЬ БЫЛО И ПОЧЕМУ ЭТО СНЯТО
//
// Прежняя редакция проверяла ТРИ ветки решения: явный `false` запрещает группу,
// явный `true` создаёт её, не задано — решает настройка стенда. Ветки были верны
// для того контракта, который тогда существовал, и проба была честной.
//
// Снят сам контракт. Условность создания группы по умолчанию — непоставленный пункт
// утверждённой приёмки при поставленных соседних, и она давала состояние, в котором
// сеть жива, а группы у неё нет. По решению владельца модель закрыта в обе стороны и
// интерфейс НАСЛЕДУЕТ группу своей сети — значит сеть без группы означает интерфейс
// без единого правила, то есть «не разрешено ничего». Посадка безопасности не может
// зависеть от того, что арендатор прислал в теле и что оператор написал в файле
// значений.
//
// Поле снято с контракта с резервированием номера И имени; разрыв объявлен в
// перечне (`proto/declared-breaks.yaml`). Настройка оператора снята вместе с ним:
// оставить её значило бы держать второй способ получить то же запрещённое
// состояние.
//
// ПОЧЕМУ ПРОБА ОСТАЛАСЬ НА УРОВНЕ ХЕНДЛЕРА. Прежний дефект сидел именно в
// отображении контракта в домен — поле не доезжало из хендлера в use-case вовсе.
// Проверять безусловность на уровне use-case значило бы проверять её там, где
// прежний дефект и не жил.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/kachomock"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/repomock"
)

func newSGRequestHandler(t *testing.T) (*Handler, *kachomock.Repository) {
	t.Helper()
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	// Конструктор БОЛЬШЕ НЕ ПРИНИМАЕТ признак условности: если он появится снова,
	// этот вызов перестанет компилироваться. Для смены формы это строже
	// утверждения — «не собралось» нельзя обойти толерантностью.
	create := NewCreateNetworkUseCase(kr, &repomock.ProjectClient{OK: true}, or)
	return NewHandler(create, nil, nil, NewGetNetworkUseCase(kr), nil, nil, nil, nil, nil, nil, nil), kr
}

func createdNetwork(t *testing.T, h *Handler, req *vpcv1.CreateNetworkRequest) *vpcv1.Network {
	t.Helper()
	op, err := h.Create(context.Background(), req)
	require.NoError(t, err)
	require.Nil(t, op.GetError(), "op must not fail: %v", op.GetError())
	require.NotNil(t, op.GetResponse(), "op must carry the created Network")
	var n vpcv1.Network
	require.NoError(t, op.GetResponse().UnmarshalTo(&n))
	return &n
}

// TestNetwork_CreateDefaultSG_Unconditional — группа по умолчанию есть у КАЖДОЙ
// созданной сети, и она существует как РЕСУРС, а не как непустой идентификатор.
func TestNetwork_CreateDefaultSG_Unconditional(t *testing.T) {
	h, kr := newSGRequestHandler(t)
	n := createdNetwork(t, h, &vpcv1.CreateNetworkRequest{
		ProjectId:      "prj-b3n7k1x9q2m5t8",
		Name:           "sg-always",
		Ipv4CidrBlocks: []string{"10.31.0.0/16"},
	})
	require.NotEmpty(t, n.DefaultSecurityGroupId,
		"у каждой сети есть группа по умолчанию: без неё интерфейс, наследующий её, "+
			"получил бы пустой набор, то есть по закрытой модели — «не разрешено ничего»")

	rd, err := kr.Reader(context.Background())
	require.NoError(t, err)
	defer func() { _ = rd.Close() }()
	sg, sgErr := rd.SecurityGroups().Get(context.Background(), n.DefaultSecurityGroupId)
	require.NoError(t, sgErr,
		"идентификатор непуст, а ресурса нет — это висячая ссылка, а не группа")
	assert.Equal(t, n.Id, sg.NetworkID)
	assert.True(t, sg.DefaultForNetwork, "группа помечена как системная для своей сети")

	// Посадка: послабление объявлено ресурсом и только на выход.
	require.Len(t, sg.Rules, 2, "исходящее разрешение в двух семействах адресов")
	for i, r := range sg.Rules {
		assert.Equal(t, domain.SecurityGroupRuleDirectionEgress, r.Direction,
			"правило %d: вход у группы по умолчанию закрыт", i)
	}
}

// TestNetwork_CreateDefaultSG_UnknownRequestFieldIsRejectedByTheEdge — снятое поле
// не принимается молча.
//
// Это НЕ дублирование предыдущей пробы: та утверждает, что группа создаётся, а эта
// — что прежний способ ОТМЕНИТЬ её больше не существует как вход. Без второй
// половины снятие поля было бы неотличимо от «поле принимается и игнорируется» —
// ровно того исхода, который конвенция запрещает.
//
// Разбор тела делает край (grpc-gateway со строгим маршалером), поэтому проверяется
// он на уровне края, а не здесь: здесь достаточно того, что в сгенерированном типе
// запроса поля НЕТ — иначе оно снова доехало бы до сервиса.
func TestNetwork_CreateDefaultSG_UnknownRequestFieldIsRejectedByTheEdge(t *testing.T) {
	md := (&vpcv1.CreateNetworkRequest{}).ProtoReflect().Descriptor()
	for i := 0; i < md.Fields().Len(); i++ {
		f := md.Fields().Get(i)
		assert.NotEqual(t, "create_default_security_group", string(f.Name()),
			"поле снято с контракта: его присутствие в дескрипторе означает, что край "+
				"его примет, а сервис — проигнорирует")
	}
	// Номер и имя зарезервированы — повторное использование запрещено.
	assert.True(t, md.ReservedNames().Has("create_default_security_group"),
		"имя снятого поля обязано стоять в reserved: иначе его переиспользуют, и "+
			"старый клиент получит поле с другим смыслом")
}
