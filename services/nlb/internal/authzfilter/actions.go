// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package authzfilter

// kacho-nlb FGA object types (передаются в iam BatchCheck как
// AuthorizeCheckRequest.resource.type).
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

// kacho-nlb action-строки. Формат `<domain>.<resource>.<verb>` per IAM permission
// catalog. Фильтр пинит relation явно (`required_relation` = viewer, затем v_list —
// см. filter.go visibilityRelations), но action едет на каждом check'е (аудит/
// трассировка; iam отвергает пустой) и остаётся read-tier: verb=list — то, что iam
// резолвит в viewer, если override когда-нибудь снимут (read==enforce: та же
// relation, что per-RPC Check для Get; см. internal/check/permission_map.go).
const (
	ActionLoadBalancerList = "loadbalancer.networkLoadBalancers.list"
	ActionListenerList     = "loadbalancer.listeners.list"
	ActionTargetGroupList  = "loadbalancer.targetGroups.list"
)
