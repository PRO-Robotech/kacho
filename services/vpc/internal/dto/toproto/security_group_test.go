// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package toproto_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/dto"

	// blank-import регистрирует трансферы (включая SecurityGroup).
	_ "github.com/PRO-Robotech/kacho/services/vpc/internal/dto/toproto"
	kachorepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// TestDTO_SecurityGroupRule_AllTargetOneofBranches — regression: toPb обязан
// сериализовать ВСЕ три взаимоисключающие ветки target-oneof
// {cidr_blocks | security_group_id | predefined_target}. Раньше мапилась только
// cidr_blocks → rule с SG-target/predefined приходил Target=nil (в Get/List
// `securityGroupId`/`predefinedTarget` были undefined).
func TestDTO_SecurityGroupRule_AllTargetOneofBranches(t *testing.T) {
	at := time.Date(2026, 5, 15, 12, 34, 56, 0, time.UTC)
	rec := kachorepo.SecurityGroupRecord{
		SecurityGroup: domain.SecurityGroup{
			ID:        "enpsg",
			ProjectID: "project-x",
			NetworkID: "e9bnet",
			Name:      "my-sg",
			Rules: []domain.SecurityGroupRule{
				{ID: "rule-cidr", Direction: domain.SecurityGroupRuleDirectionIngress, V4CidrBlocks: []string{"10.0.0.0/8"}},
				{ID: "rule-sg", Direction: domain.SecurityGroupRuleDirectionIngress, SecurityGroupID: "enpsgpeer"},
				{ID: "rule-pre", Direction: domain.SecurityGroupRuleDirectionEgress, PredefinedTarget: "self_security_group"},
			},
		},
		CreatedAt: at,
	}
	var dst *vpcv1.SecurityGroup
	require.NoError(t, dto.Transfer(dto.FromTo(rec, &dst)))
	require.NotNil(t, dst)
	require.Len(t, dst.Rules, 3)

	cidr, ok := dst.Rules[0].Target.(*vpcv1.SecurityGroupRule_CidrBlocks)
	require.True(t, ok, "rule-cidr → CidrBlocks target")
	assert.Equal(t, []string{"10.0.0.0/8"}, cidr.CidrBlocks.V4CidrBlocks)

	sg, ok := dst.Rules[1].Target.(*vpcv1.SecurityGroupRule_SecurityGroupId)
	require.True(t, ok, "rule-sg → SecurityGroupId target (было Target=nil)")
	assert.Equal(t, "enpsgpeer", sg.SecurityGroupId)

	pre, ok := dst.Rules[2].Target.(*vpcv1.SecurityGroupRule_PredefinedTarget)
	require.True(t, ok, "rule-pre → PredefinedTarget target (было Target=nil)")
	assert.Equal(t, "self_security_group", pre.PredefinedTarget)
}
