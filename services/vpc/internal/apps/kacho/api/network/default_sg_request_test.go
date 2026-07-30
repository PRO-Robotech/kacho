// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package network

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/kachomock"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/repomock"
)

// `CreateNetworkRequest.create_default_security_group` объявляет ТРЁХЗНАЧНУЮ
// процедуру решения, и до этой правки исполнялась только одна её ветка.
//
// Комментарий поля в контракте: «не задано → fallback на env
// KACHO_VPC_DEFAULT_SG_INLINE (back-compat); true → создать default-SG для сети;
// false → сеть без default-SG». Читал же решение ТОЛЬКО конфиг: поле запроса не
// доезжало из хендлера в use-case вовсе. Значит вызывающий, попросивший `false`,
// получал сеть С default-SG, если стенд настроен inline, а попросивший `true` не
// получал её, если не настроен, — и в обоих случаях успех.
//
// Почему это не «поле на будущее», а дефект: решение касается ГРУППЫ БЕЗОПАСНОСТИ,
// то есть того, что определяет допустимый трафик. Тенант, сознательно отказавшийся
// от автосоздания (свои правила, свой SG), молча получал системный — и наоборот.
//
// Проверяется на уровне ХЕНДЛЕРА, потому что дефект сидел именно в отображении
// proto → domain: use-case решение принимал корректно для того входа, который до
// него доезжал. Handler собирается с одним нужным use-case'ом (`Create` не
// касается остальных) — так тест проходит по настоящему пути, а не по его модели.
func newSGRequestHandler(t *testing.T, inlineFromConfig bool) (*Handler, *kachomock.Repository) {
	t.Helper()
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	create := NewCreateNetworkUseCase(kr, &repomock.ProjectClient{OK: true}, or, inlineFromConfig)
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

// TestNetwork_CreateDefaultSG_RequestFalseWins — явный `false` запрещает default-SG
// даже когда конфиг стенда велит создавать её inline.
func TestNetwork_CreateDefaultSG_RequestFalseWins(t *testing.T) {
	h, _ := newSGRequestHandler(t, true) // стенд настроен inline
	n := createdNetwork(t, h, &vpcv1.CreateNetworkRequest{
		ProjectId:                  "prj-b3n7k1x9q2m5t8",
		Name:                       "sg-off",
		Ipv4CidrBlocks:             []string{"10.31.0.0/16"},
		CreateDefaultSecurityGroup: proto.Bool(false),
	})
	assert.Empty(t, n.DefaultSecurityGroupId,
		"вызывающий попросил сеть БЕЗ default-SG — контракт поля обещает именно это; "+
			"непустой id значит, что решение принял конфиг стенда, а выбор тенанта выброшен")
}

// TestNetwork_CreateDefaultSG_RequestTrueWins — явный `true` создаёт default-SG
// даже когда конфиг стенда inline-провижн выключил.
func TestNetwork_CreateDefaultSG_RequestTrueWins(t *testing.T) {
	h, kr := newSGRequestHandler(t, false) // стенд inline НЕ настроен
	n := createdNetwork(t, h, &vpcv1.CreateNetworkRequest{
		ProjectId:                  "prj-b3n7k1x9q2m5t8",
		Name:                       "sg-on",
		Ipv4CidrBlocks:             []string{"10.32.0.0/16"},
		CreateDefaultSecurityGroup: proto.Bool(true),
	})
	require.NotEmpty(t, n.DefaultSecurityGroupId,
		"вызывающий попросил default-SG — она обязана быть создана и её id проставлен")

	// Ресурс, а не висячий id: SG лежит в той же writer-TX, что и сеть.
	rd, err := kr.Reader(context.Background())
	require.NoError(t, err)
	defer func() { _ = rd.Close() }()
	sg, sgErr := rd.SecurityGroups().Get(context.Background(), n.DefaultSecurityGroupId)
	require.NoError(t, sgErr, "default-SG обязана существовать как ресурс")
	assert.Equal(t, n.Id, sg.NetworkID)
}

// TestNetwork_CreateDefaultSG_UnsetFollowsConfig — не задано → решает конфиг
// (back-compat, ровно как обещает комментарий поля). Обе ветки, чтобы «молчание»
// не оказалось замаскированным «всегда да» или «всегда нет».
func TestNetwork_CreateDefaultSG_UnsetFollowsConfig(t *testing.T) {
	hOn, _ := newSGRequestHandler(t, true)
	on := createdNetwork(t, hOn, &vpcv1.CreateNetworkRequest{
		ProjectId:      "prj-b3n7k1x9q2m5t8",
		Name:           "sg-unset-on",
		Ipv4CidrBlocks: []string{"10.33.0.0/16"},
	})
	assert.NotEmpty(t, on.DefaultSecurityGroupId,
		"поле не задано, стенд inline → default-SG создаётся (back-compat)")

	hOff, _ := newSGRequestHandler(t, false)
	off := createdNetwork(t, hOff, &vpcv1.CreateNetworkRequest{
		ProjectId:      "prj-b3n7k1x9q2m5t8",
		Name:           "sg-unset-off",
		Ipv4CidrBlocks: []string{"10.34.0.0/16"},
	})
	assert.Empty(t, off.DefaultSecurityGroupId,
		"поле не задано, стенд inline выключен → default-SG нет (back-compat)")
}
