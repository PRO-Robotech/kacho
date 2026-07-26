// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package listener

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// Revoke-by-label on nlb.listener (T31-LBLREVOKE-NLB-LISTENER-04).
//
// `loadbalancer.listeners` is a declared label-selectable type
// (services/iam/internal/domain/feed_registry.go) and it is MIRROR-fed: an
// ARM_LABELS grant selects listeners by matching `kacho_iam.resource_mirror.labels`
// against the rule's matchLabels. The mirror is written ONLY as a side effect of
// kacho-iam's InternalIAMService.RegisterResource — the labels ride along on the
// RegisterResource call as an additive payload field.
//
// THE PART THAT MAKES THIS MORE THAN "did we emit something". kacho-iam guards
// that proxy write-path with a least-privilege relation allow-list
// (services/iam/internal/authzguard/proxy_tuple_policy.go: allowedProxyRelations =
// {project, account, parent, owner}). ValidateProxyTuple runs BEFORE the mirror
// UPSERT, so a RegisterResource carrying a relation outside that set is rejected
// with PermissionDenied and its labels/parent payload is dropped on the floor —
// the mirror is never refreshed, no `mirror.upsert` reconcile event is enqueued,
// and the stale label keeps matching the selector. Access is never revoked.
//
// nlb's own creator (`admin`) and parent-link (`load_balancer`) relations are
// deliberately NOT in that set (create.go documents this: the drainer applies
// tuples in order and short-circuits on the first rejection, which is why the
// project tuple is emitted FIRST there). So a labels-refresh intent MUST carry a
// proxy-registrable tuple, or it is a no-op that cannot revoke anything.
//
// These tests therefore assert the CROSS-SERVICE contract, not merely that an
// intent was emitted: asserting emission alone stays green on exactly the defect
// this chain caught.
//
// Regression for: `lsn-post-revoke-deny` still reporting {"allowed":true} after a
// label removal, with `lsn-post-grant-allow` green (Create emits a project tuple,
// Update emitted only the rejected parent-link).

// proxyRegistrableRelations mirrors kacho-iam's allowedProxyRelations. It is
// duplicated here rather than imported because Go's `internal/` visibility rule
// forbids services/nlb from importing services/iam/internal/authzguard — the two
// services are peers that talk over gRPC, never by build (polyrepo.md). The set is
// a stable least-privilege security boundary (ownership/parent relations only);
// kacho-iam holds the authoritative copy and its own tests over it.
var proxyRegistrableRelations = map[string]struct{}{
	"project": {},
	"account": {},
	"parent":  {},
	"owner":   {},
}

// requireCarriesProxyRegistrableTuple asserts the intent carries at least one
// tuple kacho-iam will actually accept — i.e. that the labels/parent payload
// reaches resource_mirror instead of being rejected before the UPSERT.
func requireCarriesProxyRegistrableTuple(t *testing.T, intent domain.FGARegisterIntent) {
	t.Helper()
	require.NotEmpty(t, intent.Tuples, "mirror-feed intent carries no tuples at all")
	seen := make([]string, 0, len(intent.Tuples))
	for _, tu := range intent.Tuples {
		seen = append(seen, tu.Relation)
		if _, ok := proxyRegistrableRelations[tu.Relation]; ok {
			return
		}
	}
	t.Fatalf("mirror-feed intent carries no proxy-registrable tuple: relations=%v; "+
		"kacho-iam ValidateProxyTuple rejects all of these with PermissionDenied BEFORE the "+
		"resource_mirror UPSERT, so the refreshed labels never land and an ARM_LABELS grant "+
		"is never revoked (allowed stays true)", seen)
}

// findListenerMirrorTuple returns the project-hierarchy tuple for the listener,
// which is the carrier kacho-iam accepts (parity with lbMirrorIntent/tgMirrorIntent).
func findListenerMirrorTuple(intent domain.FGARegisterIntent) (domain.FGATuple, bool) {
	for _, tu := range intent.Tuples {
		if tu.Relation == domain.FGARelationProject {
			return tu, true
		}
	}
	return domain.FGATuple{}, false
}

// Removing the matching label must produce a mirror-refresh kacho-iam actually
// applies. This is the revoke half of T31-LBLREVOKE-NLB-LISTENER-04.
func TestUpdateListener_LabelRemoval_EmitsApplicableMirrorRefresh(t *testing.T) {
	t.Parallel()
	suite := newUpdateSuite(t)

	// Given: the listener carries the label the ARM_LABELS grant selects on.
	suite.repo.mu.Lock()
	suite.repo.listeners[string(suite.listener.ID)].Labels = domain.LabelsFromMap(
		map[string]string{"lsn": "treska"})
	suite.repo.mu.Unlock()

	// When: the tenant clears the labels (newman sends updateMask="labels", labels={}).
	op, err := suite.uc.Run(context.Background(), &lbv1.UpdateListenerRequest{
		ListenerId: string(suite.listener.ID),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"labels"}},
		Labels:     map[string]string{},
	})
	require.NoError(t, err)
	done := awaitOpDone(t, suite.ops, op.ID, time.Second)
	require.Nil(t, done.Error)

	// Then: exactly one mirror-feed register intent is committed...
	fga := suite.repo.committedFGA()
	require.Len(t, fga, 1, "label-clearing Update emits one fga.register mirror intent")
	require.Equal(t, domain.FGAEventRegister, fga[0].EventType)
	intent := fga[0].Intent

	// ...and kacho-iam can actually apply it (the whole point — an intent made only
	// of proxy-rejected relations refreshes nothing).
	requireCarriesProxyRegistrableTuple(t, intent)

	tuple, ok := findListenerMirrorTuple(intent)
	require.True(t, ok, "labels-refresh feed carries the project-hierarchy tuple (parity with lbMirrorIntent/tgMirrorIntent)")
	assert.Equal(t, "project:"+string(suite.listener.ProjectID), tuple.SubjectID)
	assert.Equal(t, domain.FGAObjectRef(domain.FGAObjectTypeListener, string(suite.listener.ID)), tuple.Object)

	// The refreshed payload must actually be the CLEARED label set — a mirror that
	// keeps the old labels matches the selector just as well as no mirror update.
	assert.Empty(t, intent.Labels, "cleared labels are carried, so the selector stops matching")
	assert.Equal(t, string(suite.listener.ProjectID), intent.ParentProjectID,
		"parent-project accompanies the refresh (containment gate)")
}

// Regression (grant half): setting a matching label must still feed the mirror, so
// grant-by-label keeps working. `lsn-post-grant-allow` pins this end-to-end.
func TestUpdateListener_LabelSet_EmitsApplicableMirrorRefresh(t *testing.T) {
	t.Parallel()
	suite := newUpdateSuite(t)

	op, err := suite.uc.Run(context.Background(), &lbv1.UpdateListenerRequest{
		ListenerId: string(suite.listener.ID),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"labels"}},
		Labels:     map[string]string{"lsn": "treska"},
	})
	require.NoError(t, err)
	done := awaitOpDone(t, suite.ops, op.ID, time.Second)
	require.Nil(t, done.Error)

	fga := suite.repo.committedFGA()
	require.Len(t, fga, 1)
	intent := fga[0].Intent
	requireCarriesProxyRegistrableTuple(t, intent)
	assert.Equal(t, map[string]string{"lsn": "treska"}, intent.Labels,
		"new labels reach the mirror so the ARM_LABELS selector starts matching")
	assert.Equal(t, string(suite.listener.ProjectID), intent.ParentProjectID)
}

// Regression (re-grant): a label re-added after a revoke must not be swallowed —
// each labels-Update emits its own applicable refresh carrying the current state.
func TestUpdateListener_RegrantAfterRevoke_EachRefreshApplicable(t *testing.T) {
	t.Parallel()
	suite := newUpdateSuite(t)
	id := string(suite.listener.ID)

	grant, err := suite.uc.Run(context.Background(), &lbv1.UpdateListenerRequest{
		ListenerId: id,
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"labels"}},
		Labels:     map[string]string{"lsn": "treska"},
	})
	require.NoError(t, err)
	require.Nil(t, awaitOpDone(t, suite.ops, grant.ID, time.Second).Error)

	revoke, err := suite.uc.Run(context.Background(), &lbv1.UpdateListenerRequest{
		ListenerId: id,
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"labels"}},
		Labels:     map[string]string{},
	})
	require.NoError(t, err)
	require.Nil(t, awaitOpDone(t, suite.ops, revoke.ID, time.Second).Error)

	regrant, err := suite.uc.Run(context.Background(), &lbv1.UpdateListenerRequest{
		ListenerId: id,
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"labels"}},
		Labels:     map[string]string{"lsn": "treska"},
	})
	require.NoError(t, err)
	require.Nil(t, awaitOpDone(t, suite.ops, regrant.ID, time.Second).Error)

	fga := suite.repo.committedFGA()
	require.Len(t, fga, 3, "grant → revoke → re-grant each emit their own mirror refresh")
	for i, ev := range fga {
		requireCarriesProxyRegistrableTuple(t, ev.Intent)
		assert.Equal(t, domain.FGAEventRegister, ev.EventType, "intent %d", i)
	}
	assert.Equal(t, map[string]string{"lsn": "treska"}, fga[0].Intent.Labels, "grant carries the label")
	assert.Empty(t, fga[1].Intent.Labels, "revoke carries the cleared set")
	assert.Equal(t, map[string]string{"lsn": "treska"}, fga[2].Intent.Labels, "re-grant carries the label again")
}

// A full-object PATCH (empty mask) reapplies labels, so it must feed the mirror
// too — clearing labels through an empty mask is the same revocation.
func TestUpdateListener_EmptyMask_EmitsApplicableMirrorRefresh(t *testing.T) {
	t.Parallel()
	suite := newUpdateSuite(t)

	suite.repo.mu.Lock()
	suite.repo.listeners[string(suite.listener.ID)].Labels = domain.LabelsFromMap(
		map[string]string{"lsn": "treska"})
	suite.repo.mu.Unlock()

	op, err := suite.uc.Run(context.Background(), &lbv1.UpdateListenerRequest{
		ListenerId: string(suite.listener.ID),
		Name:       "initial",
		Labels:     map[string]string{},
	})
	require.NoError(t, err)
	require.Nil(t, awaitOpDone(t, suite.ops, op.ID, time.Second).Error)

	fga := suite.repo.committedFGA()
	require.Len(t, fga, 1, "full-object PATCH reapplies labels → one mirror refresh")
	requireCarriesProxyRegistrableTuple(t, fga[0].Intent)
	assert.Empty(t, fga[0].Intent.Labels)
}

// requireEveryTupleProxyRegistrable is the stronger form required of a DURABLE
// outbox intent: not "at least one tuple lands" but "no tuple can fail", because
// the register-drainer is fail-closed and short-circuits on the first rejection.
func requireEveryTupleProxyRegistrable(t *testing.T, intent domain.FGARegisterIntent) {
	t.Helper()
	require.NotEmpty(t, intent.Tuples, "durable intent carries no tuples at all")
	for _, tu := range intent.Tuples {
		if _, ok := proxyRegistrableRelations[tu.Relation]; !ok {
			t.Fatalf("durable intent carries a tuple kacho-iam always rejects: relation=%q object=%q; "+
				"the register-drainer classifies PermissionDenied as TRANSIENT and short-circuits, so the "+
				"row is never marked sent — and with PartitionColumn=resource_id the partition-head claim "+
				"then blocks every LATER intent for this same object (notably the labels-refresh that "+
				"revokes access) until the row poisons after MaxAttempts", tu.Relation, tu.Object)
		}
	}
}

// The durable Create intent must be drainable end-to-end.
//
// This is the second half of why revoke-by-label never took effect, and it is a
// head-of-line wedge rather than a lag. nlb's creator (`admin`) and parent-link
// (`load_balancer`) relations are refused by kacho-iam's least-privilege proxy
// policy — privilege relations are writable only by the AccessBinding flow, never
// by a module proxy. The register-drainer treats PermissionDenied as transient and
// short-circuits the tuple loop, so a Create intent containing them can NEVER be
// marked sent: it retries to MaxAttempts (10, backoff 1s→30s ≈ 150s) and only then
// stops blocking. Because the drainer claims partition-head-only on resource_id,
// that unsent Create row blocks the listener's own later labels-refresh intent for
// the whole of that window — which is exactly why `lsn-post-revoke-deny` stayed
// {"allowed":true} well past its 15s budget with no consistency race involved.
//
// The rejected tuples were never a loss to begin with: they are refused on every
// delivery, so nothing that depended on them ever worked. Access to a new listener
// comes from the project tuple plus IAM's own per-object materialisation
// (flat Contract-A), not from a module-written `admin` tuple.
func TestCreateListener_DurableRegisterIntentIsDrainable(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	ops := newFakeOpsRepo()
	lb := seedParentLB(t, repo)
	uc := newCreateUC(repo, ops)

	op, err := uc.Run(contextWithSubject("user:usr01TESTUSER000001"), &lbv1.CreateListenerRequest{
		LoadBalancerId: string(lb.ID),
		Name:           "drainable",
		Protocol:       lbv1.Listener_TCP,
		Port:           443,
		TargetPort:     8443,
		Labels:         map[string]string{"lsn": "treska"},
	})
	require.NoError(t, err)
	require.Nil(t, awaitOpDone(t, ops, op.ID, 2*time.Second).Error)

	fga := repo.committedFGA()
	require.Len(t, fga, 1, "Create emits one durable fga.register intent")
	requireEveryTupleProxyRegistrable(t, fga[0].Intent)
	assert.Equal(t, map[string]string{"lsn": "treska"}, fga[0].Intent.Labels,
		"the surviving tuple still carries the labels that feed the mirror")
}

// Delete must retract the mirror row, and it retracts it through the SAME
// least-privilege proxy gate: UnregisterResource runs the identical
// ValidateProxyTuple check (internal_iam/handler.go). An unregister-intent whose
// only tuple carries a rejected relation never reaches the mirror DELETE, so the
// row for a deleted listener survives and the reconciler keeps re-materialising
// access from it — a deleted resource keeping its grant.
//
// This is the same over-grant class closed for registry/vpc/storage (their
// tombstones lost a version comparison); the listener lost it on the relation
// allow-list instead. Its LB and TargetGroup siblings already unregister via the
// project tuple — the listener was the only outlier, on both of its mirror paths.
func TestDeleteListener_EmitsApplicableMirrorRetraction(t *testing.T) {
	t.Parallel()
	suite := newDeleteSuite(t)
	suite.repo.seedListener(suite.listener)

	op, err := suite.uc.Run(context.Background(), &lbv1.DeleteListenerRequest{
		ListenerId: string(suite.listener.ID),
	})
	require.NoError(t, err)
	require.Nil(t, awaitOpDone(t, suite.ops, op.ID, time.Second).Error)

	fga := suite.repo.committedFGA()
	require.Len(t, fga, 1, "Delete emits one fga.unregister mirror retraction")
	require.Equal(t, domain.FGAEventUnregister, fga[0].EventType)

	requireCarriesProxyRegistrableTuple(t, fga[0].Intent)

	tuple, ok := findListenerMirrorTuple(fga[0].Intent)
	require.True(t, ok, "retraction carries the project-hierarchy tuple (parity with lbUnregisterIntent/tgUnregisterIntent)")
	assert.Equal(t, "project:"+string(suite.listener.ProjectID), tuple.SubjectID)
	assert.Equal(t, domain.FGAObjectRef(domain.FGAObjectTypeListener, string(suite.listener.ID)), tuple.Object)
}

// Parity guard (do not weaken): a non-labels Update stays a mirror no-op, so the
// fix does not turn every Update into a RegisterResource round-trip.
// Mirrors targetgroup.TestUpdate_NonLabelsMask_NoMirrorIntent.
func TestUpdateListener_NonLabelsMask_NoMirrorIntent(t *testing.T) {
	t.Parallel()
	suite := newUpdateSuite(t)

	op, err := suite.uc.Run(context.Background(), &lbv1.UpdateListenerRequest{
		ListenerId:      string(suite.listener.ID),
		UpdateMask:      &fieldmaskpb.FieldMask{Paths: []string{"proxy_protocol_v2"}},
		ProxyProtocolV2: true,
	})
	require.NoError(t, err)
	require.Nil(t, awaitOpDone(t, suite.ops, op.ID, time.Second).Error)

	require.Empty(t, suite.repo.committedFGA(), "non-labels Update emits no mirror intent")
}
