// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package listener

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
)

// Пара «код + дословный текст» отказа на правку области владения (#1671).
//
// У слушателя СОБСТВЕННОГО глагола переноса нет и не заводится: его проект
// денормализован с балансировщика, и `NetworkLoadBalancerService.Move`
// переставляет слушателей каскадом, в той же транзакции. Поэтому отказ называет
// глагол ВЛАДЕЛЬЦА и говорит, к чему его применять, — обещать
// `ListenerService.Move` значило бы объявить возможность, которой нет
// (`api-conventions.md` §«Неисполнимая возможность»).
func TestUpdateListener_ProjectScopeRefusal_NamesTheNextStep(t *testing.T) {
	t.Parallel()
	suite := newUpdateSuite(t)

	_, err := suite.uc.Run(context.Background(), &lbv1.UpdateListenerRequest{
		ListenerId: string(suite.listener.ID),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"project_id"}},
	})

	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Equal(t,
		"project_id is immutable after Listener.Create; "+
			"use NetworkLoadBalancerService.Move on the parent load balancer",
		status.Convert(err).Message())
}

// Положительный контроль плюс контроль соседних полей: у `load_balancer_id`,
// `protocol` и `port` глагола переноса нет, и они остаются на каноническом
// тексте без следующего шага. Без этой пары отрицание выше зеленело бы на
// use-case, приписавшем хвост «use …Move» ко ВСЕМ отказам подряд.
func TestUpdateListener_NeighbourImmutablesKeepPlainRefusal(t *testing.T) {
	t.Parallel()

	for _, field := range []string{"load_balancer_id", "protocol", "port"} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			suite := newUpdateSuite(t)
			_, err := suite.uc.Run(context.Background(), &lbv1.UpdateListenerRequest{
				ListenerId: string(suite.listener.ID),
				UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{field}},
			})
			require.Equal(t, codes.InvalidArgument, status.Code(err))
			require.Equal(t, field+" is immutable after Listener.Create",
				status.Convert(err).Message())
		})
	}

	t.Run("mutable mask passes", func(t *testing.T) {
		t.Parallel()
		suite := newUpdateSuite(t)
		_, err := suite.uc.Run(context.Background(), &lbv1.UpdateListenerRequest{
			ListenerId: string(suite.listener.ID),
			Name:       "tone-control",
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"name"}},
		})
		require.NoError(t, err)
	})
}
