// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package authzfilter

// kacho-nlb FGA object types (передаются в iam.ListObjects.resource_type).
//
// префикс `nlb_` (совпадает с service-именем kacho-nlb, NLB-1a hard-rename) —
// совпадает с FGA-моделью (`type nlb_network_load_balancer / nlb_listener /
// nlb_target_group` в kacho-proto fga_model.fga) и api-gateway permission_catalog.
// Зеркало internal/domain/fga_intent.go FGAObjectType* + internal/check/permission_map.go.
const (
	ResourceTypeLoadBalancer = "nlb_network_load_balancer"
	ResourceTypeListener     = "nlb_listener"
	ResourceTypeTargetGroup  = "nlb_target_group"
)

// kacho-nlb action-строки — iam-сервер мапит на FGA relation (viewer для read/list).
// Формат `<domain>.<resource>.<verb>` per IAM permission catalog. verb=list →
// iam мапит на relation viewer (read==enforce: та же relation, что per-RPC Check
// для Get; см. internal/check/permission_map.go relationViewer).
const (
	ActionLoadBalancerList = "loadbalancer.networkLoadBalancers.list"
	ActionListenerList     = "loadbalancer.listeners.list"
	ActionTargetGroupList  = "loadbalancer.targetGroups.list"
)
