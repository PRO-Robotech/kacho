// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package authzfilter

// Compute-domain FGA object types (consumed by
// iam.BatchCheck.checks[].resource.type). Must stay in sync with
// internal/check/permission_map.go.
const (
	ResourceTypeInstance = "compute_instance"
	ResourceTypeDisk     = "compute_disk"
	ResourceTypeImage    = "compute_image"
	ResourceTypeSnapshot = "compute_snapshot"
)

// Compute-domain action strings. Format: `<domain>.<resource>.<verb>`.
//
// The action is carried on every AuthorizeCheckRequest for audit/trace; the
// DECISION is taken on the explicit `required_relation` the filter pins per batch
// (`viewer`, then `v_list` — see authzfilter.visibilityRelations), not on a
// server-side verb→relation derivation. The verb still MUST be one kacho-iam
// maps (`resolveActionToRelation` maps only the canonical RPC verbs get/list;
// "read" is UNMAPPED → "Illegal argument action"), because a request that fails
// action-validation never reaches the relation check.
//
// "viewer" is also the SAME relation the per-RPC Check gate uses for Get/List
// (internal/check/permission_map.go) — so List visibility == Check-allow
// (read==enforce), and "viewer" cascades from an account-tier scope_grant
// (g_viewer_compute_<type>) so a rules-role list-grant stays visible per-object.
const (
	ActionInstanceRead = "compute.instances.list"
	ActionDiskRead     = "compute.disks.list"
	ActionImageRead    = "compute.images.list"
	ActionSnapshotRead = "compute.snapshots.list"
)
