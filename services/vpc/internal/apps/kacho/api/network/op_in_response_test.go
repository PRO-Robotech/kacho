// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package network

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/kachomock"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/repomock"
)

// TestNetwork_VPC_1_14_OpInResponse — F4: Network.Create — statusless op-in-response.
// В ТОМ ЖЕ ответе Operation.done == true, metadata → CreateNetworkMetadata{networkId},
// result — response (не error) с полным телом Network (id, projectId, name,
// ipv4CidrBlocks, defaultSecurityGroupId°, createdAt°). Follow-up GET не нужен.
func TestNetwork_VPC_1_14_OpInResponse(t *testing.T) {
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	uc := NewCreateNetworkUseCase(kr, &repomock.ProjectClient{OK: true}, or, true)

	op, err := uc.Execute(context.Background(), domain.Network{
		ProjectID:      "prj-b3n7k1x9q2m5t8",
		Name:           domain.RcNameVPC("core-prod"),
		IPv4CidrBlocks: []string{"10.20.0.0/16"},
	})
	require.NoError(t, err)

	// op-in-response: возвращённая операция УЖЕ завершена (не done:false).
	require.True(t, op.Done, "Create must return an already-completed Operation (op-in-response)")
	require.Nil(t, op.Error, "successful Create → result.response, not result.error")
	require.NotNil(t, op.Response, "result.response must carry the created Network")

	// metadata доступна сразу.
	require.NotNil(t, op.Metadata)
	var meta vpcv1.CreateNetworkMetadata
	require.NoError(t, op.Metadata.UnmarshalTo(&meta))
	require.NotEmpty(t, meta.NetworkId)

	// response анмаршалится в полный public Network.
	var got vpcv1.Network
	require.NoError(t, op.Response.UnmarshalTo(&got))
	assert.Equal(t, meta.NetworkId, got.Id)
	assert.Equal(t, "prj-b3n7k1x9q2m5t8", got.ProjectId)
	assert.Equal(t, "core-prod", got.Name)
	assert.Equal(t, []string{"10.20.0.0/16"}, got.Ipv4CidrBlocks)
	assert.NotEmpty(t, got.DefaultSecurityGroupId, "default-SG id echoed in op-in-response")
}

// TestNetwork_VPC_1_15_Update_OpInResponse — F4/VPC-1-15: Update тоже op-in-response.
func TestNetwork_VPC_1_15_Update_OpInResponse(t *testing.T) {
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	create := NewCreateNetworkUseCase(kr, &repomock.ProjectClient{OK: true}, or, false)
	update := NewUpdateNetworkUseCase(kr, or)

	cOp, err := create.Execute(context.Background(), domain.Network{ProjectID: "f1", Name: domain.RcNameVPC("core-prod")})
	require.NoError(t, err)
	require.True(t, cOp.Done)
	var created vpcv1.Network
	require.NoError(t, cOp.Response.UnmarshalTo(&created))

	uOp, err := update.Execute(context.Background(), UpdateInput{
		NetworkID: created.Id,
		Network: domain.Network{
			Description: domain.RcDescription("Primary prod VPC"),
			Labels:      domain.LabelsFromMap(map[string]string{"env": "prod"}),
		},
		UpdateMask: []string{"description", "labels"},
	})
	require.NoError(t, err)
	require.True(t, uOp.Done, "Update must return an already-completed Operation")
	require.Nil(t, uOp.Error)
	var upd vpcv1.Network
	require.NoError(t, uOp.Response.UnmarshalTo(&upd))
	assert.Equal(t, "Primary prod VPC", upd.Description)
	assert.Equal(t, map[string]string{"env": "prod"}, upd.Labels)
}

// TestNetwork_VPC_1_19_Delete_OpInResponse — F4/VPC-1-19: Delete op-in-response,
// result.response — google.protobuf.Empty.
func TestNetwork_VPC_1_19_Delete_OpInResponse(t *testing.T) {
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	create := NewCreateNetworkUseCase(kr, &repomock.ProjectClient{OK: true}, or, false)
	del := NewDeleteNetworkUseCase(kr, repomock.NewSubnetRepo(), repomock.NewRouteTableRepo(), nil, or)

	cOp, err := create.Execute(context.Background(), domain.Network{ProjectID: "f1", Name: domain.RcNameVPC("to-del")})
	require.NoError(t, err)
	var created vpcv1.Network
	require.NoError(t, cOp.Response.UnmarshalTo(&created))

	dOp, err := del.Execute(context.Background(), created.Id)
	require.NoError(t, err)
	require.True(t, dOp.Done, "Delete must return an already-completed Operation")
	require.Nil(t, dOp.Error)
	require.NotNil(t, dOp.Response)
	var empty emptypb.Empty
	require.NoError(t, dOp.Response.UnmarshalTo(&empty))
}
