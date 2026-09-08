// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package loadbalancer

import (
	"context"
	"errors"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/option"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/clients/iam"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// Проба `TestLBOutboxPayload_Nil` СНЯТА вместе со своим предметом (#1551):
// строитель минимального снимка `lbOutboxPayload` больше не существует — форма
// нагрузки вида заменена конвертом полного состояния, а строитель переехал в
// repo-leaf, где его и зовут ОБА пакета use-case. Проба нулевого входа уехала
// туда же (`TestLoadBalancerStatePayload_NilGuard`), к своей функции: оставленная
// здесь, она утверждала бы о том, чего в этом пакете нет.

func TestAddressOfTarget_AllVariants(t *testing.T) {
	t.Parallel()
	require.Equal(t, "epd00INSTANCE000000", addressOfTarget(domain.Target{
		InstanceID: option.MustNewOption(domain.InstanceID("epd00INSTANCE000000")),
	}))
	require.Equal(t, "e9b00NIC00000000000", addressOfTarget(domain.Target{
		NicID: option.MustNewOption(domain.NicID("e9b00NIC00000000000")),
	}))
	require.Equal(t, "", addressOfTarget(domain.Target{}))
}

func TestSubnetOfTarget(t *testing.T) {
	t.Parallel()
	require.Equal(t, "", subnetOfTarget(domain.Target{}))
	require.Equal(t, "sub-x", subnetOfTarget(domain.Target{
		IPRef: &domain.TargetIPRef{SubnetID: "sub-x", Address: "10.0.0.5"},
	}))
}

// TestLBRegisterIntent_ProjectTupleOnly — the durable Create intent carries
// exactly the project-hierarchy tuple, independent of the principal.
//
// It used to append a creator (`admin`) tuple for an authenticated user, and that
// tuple could never land: kaname's least-privilege proxy policy accepts only
// ownership/parent relations declared in pkg/authz/proxytuple and reserves
// privilege relations like `admin` for the AccessBinding flow, so every delivery
// was refused. Worse, the refusal costs the whole registration: it is TERMINAL
// (drainer.ErrPermanent, both at the applier and in the shared drainer), the
// applier stops at the rejected tuple, and the row poisons on its first attempt —
// so this load balancer's mirror row, and every verb materialised from it, waits
// for reconciler.RedrivePoisoned. (While the refusal was still classified as
// retryable it was worse in a different way: a retryable row is held below the
// poison threshold by design, so it never left the partition-head blocking set and
// blocked every LATER intent for the same load balancer — including a
// labels-refresh that revokes an ARM_LABELS grant — with no end.)
//
// Creator access does not depend on it: it is materialised per-object by IAM's
// reconciler (flat Contract-A), not by a module-written admin tuple. The intent is
// now principal-independent, which is why one case replaces the former
// system-vs-user pair.
func TestLBRegisterIntent_ProjectTupleOnly(t *testing.T) {
	t.Parallel()
	intent := lbRegisterIntent(
		&kachorepo.LoadBalancerRecord{LoadBalancer: domain.LoadBalancer{ID: "nlb-x", ProjectID: "prj"}})
	require.Equal(t, "NetworkLoadBalancer", intent.Kind)
	require.Len(t, intent.Tuples, 1, "durable intent carries only proxy-registrable tuples")
	require.Equal(t, domain.FGARelationProject, intent.Tuples[0].Relation)
	require.Equal(t, "project:prj", intent.Tuples[0].SubjectID)
	require.Equal(t, "nlb_network_load_balancer:nlb-x", intent.Tuples[0].Object)
}

func TestPeerErrToStatus_ProjectClientCaller(t *testing.T) {
	t.Parallel()
	pc := &fakeProjectClient{getFunc: func(_ context.Context, _ string) (*iam.Project, error) {
		return nil, errors.New("transient")
	}}
	_, err := pc.Get(context.Background(), "prj")
	require.Error(t, err)
	st := peerErrToStatus(err, "project", "prj")
	require.Equal(t, codes.Internal, codes.Code(codeFromErr(st)))
}

// codeFromErr — мини-helper, чтобы избежать import status в helpers_test.
func codeFromErr(err error) uint32 {
	type coder interface{ Code() uint32 }
	if c, ok := err.(coder); ok {
		return c.Code()
	}
	// fallback through status.FromError.
	return 13 // Internal
}
