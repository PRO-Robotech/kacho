// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

// Two halves of one rule about update_mask, asserted on the row that ends up in
// the database rather than on the arguments a function received.
//
// (1) A field NAMED in the mask is applied verbatim — including the value that
// clears it. Clearing the host-class list used to be a silent no-op: an empty Go
// slice reaches the driver as NULL, COALESCE folds NULL back onto the previous
// value, and the caller is handed a successful mutation that changed nothing. The
// insert path already normalised nil→{} at the adapter boundary; update did not.
//
// (2) A field NOT carried by the caller is not wiped just because the mask was
// empty. An empty mask means "apply what you were given", not "assume every field
// I did not mention is now its zero value" — the infra columns were being zeroed by
// a PATCH that only meant to rename, while `name` was protected from exactly that.
// Both branches cannot be right at once; the one that survives is the one the
// domain already documents ("on Update an empty name means: do not change it").

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	region "github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/api/region"
	zone "github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/api/zone"
	"github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/geo/internal/domain"
	"github.com/PRO-Robotech/kacho/services/geo/internal/repo/kacho/pg"
	"github.com/PRO-Robotech/kacho/services/geo/internal/repo/kacho/repomock"
)

// zoneInfraRow reads the infra columns straight out of the table — the observable
// that separates "the mutation was accepted" from "the mutation happened".
func zoneInfraRow(t *testing.T, pool *pgxpool.Pool, id string) domain.ZoneInfra {
	t.Helper()
	var got domain.ZoneInfra
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT host_classes, failure_domain_count, underlay_anchor, capacity_hint
		   FROM zones WHERE id = $1`, id).
		Scan(&got.HostClasses, &got.FailureDomainCount, &got.UnderlayAnchor, &got.CapacityHint))
	return got
}

// seedZoneWithInfra creates region+zone carrying a full set of infra values.
func seedZoneWithInfra(t *testing.T, pool *pgxpool.Pool) (*pg.ZoneRepo, string) {
	t.Helper()
	ctx := context.Background()
	rr := pg.NewRegionRepo(pool)
	zr := pg.NewZoneRepo(pool)
	_, err := rr.Insert(ctx, &domain.Region{ID: "region-1", Name: "Region 1"})
	require.NoError(t, err)
	_, err = zr.Insert(ctx, &domain.Zone{
		ID: "region-1-a", RegionID: "region-1", Name: "Zone A", Status: domain.GeoStatusUp,
		Infra: domain.ZoneInfra{
			NumericInfraID:     7,
			HostClasses:        []string{"std-v3", "mem-v2"},
			FailureDomainCount: 3,
			UnderlayAnchor:     "anchor-a",
			CapacityHint:       "ROOMY",
		},
	})
	require.NoError(t, err)
	return zr, "region-1-a"
}

// TestZoneUpdateClearsHostClasses — clearing the list must actually clear the row.
// Asserted on the stored value, not on the returned status.
func TestZoneUpdateClearsHostClasses(t *testing.T) {
	pool := newTestPool(t)
	zr, id := seedZoneWithInfra(t, pool)

	empty := []string{}
	updated, err := zr.Update(context.Background(), id, zone.UpdateParams{HostClasses: &empty})
	require.NoError(t, err)
	require.Empty(t, updated.Infra.HostClasses, "the returned zone must show the cleared list")
	require.Empty(t, zoneInfraRow(t, pool, id).HostClasses,
		"a mutation reported as successful must be visible in the row — an empty list reaching the driver as NULL is folded back by COALESCE and changes nothing")
}

// TestZoneUpdateOmittedHostClassesUntouched — the other side of the same pointer
// contract: nil means "not provided", and COALESCE must keep the stored value.
func TestZoneUpdateOmittedHostClassesUntouched(t *testing.T) {
	pool := newTestPool(t)
	zr, id := seedZoneWithInfra(t, pool)

	hint := "CONSTRAINED"
	_, err := zr.Update(context.Background(), id, zone.UpdateParams{CapacityHint: &hint})
	require.NoError(t, err)
	require.Equal(t, []string{"std-v3", "mem-v2"}, zoneInfraRow(t, pool, id).HostClasses,
		"a parameter that was not provided must not be rewritten")
}

// TestZoneEmptyMaskDoesNotWipeUnsentInfra — a PATCH with an empty mask carrying only
// a new name must leave every field the caller did not send exactly as it was. The
// name was already protected from this; the infra columns were not, so a rename
// silently erased the underlay anchor, the capacity hint and the failure-domain
// count.
func TestZoneEmptyMaskDoesNotWipeUnsentInfra(t *testing.T) {
	pool := newTestPool(t)
	zr, id := seedZoneWithInfra(t, pool)
	uc := zone.New(zr, zr, repomock.NewOpsRepo(), serviceerr.ToStatus)

	op, err := uc.Update(context.Background(), zone.UpdateInput{ID: id, Name: "Zone A renamed"})
	require.NoError(t, err)
	require.Nil(t, op.Error, "the rename itself must succeed")

	got := zoneInfraRow(t, pool, id)
	require.Equal(t, []string{"std-v3", "mem-v2"}, got.HostClasses, "host classes were not sent")
	require.Equal(t, int32(3), got.FailureDomainCount, "failure-domain count was not sent")
	require.Equal(t, "anchor-a", got.UnderlayAnchor, "underlay anchor was not sent")
	require.Equal(t, "ROOMY", got.CapacityHint, "capacity hint was not sent")
}

// TestZoneEmptyMaskAppliesWhatWasSent — the rule stays useful: an empty mask still
// applies every field the caller did carry.
func TestZoneEmptyMaskAppliesWhatWasSent(t *testing.T) {
	pool := newTestPool(t)
	zr, id := seedZoneWithInfra(t, pool)
	uc := zone.New(zr, zr, repomock.NewOpsRepo(), serviceerr.ToStatus)

	_, err := uc.Update(context.Background(), zone.UpdateInput{
		ID: id,
		Infra: domain.ZoneInfra{
			HostClasses:        []string{"gpu-v1"},
			FailureDomainCount: 5,
			UnderlayAnchor:     "anchor-b",
			CapacityHint:       "CONSTRAINED",
		},
	})
	require.NoError(t, err)

	got := zoneInfraRow(t, pool, id)
	require.Equal(t, []string{"gpu-v1"}, got.HostClasses)
	require.Equal(t, int32(5), got.FailureDomainCount)
	require.Equal(t, "anchor-b", got.UnderlayAnchor)
	require.Equal(t, "CONSTRAINED", got.CapacityHint)
}

// TestZoneMaskedClearReachesTheRow — naming a field in the mask is how a caller
// clears it, and that has to work through the use-case too, all the way to the row.
func TestZoneMaskedClearReachesTheRow(t *testing.T) {
	pool := newTestPool(t)
	zr, id := seedZoneWithInfra(t, pool)
	uc := zone.New(zr, zr, repomock.NewOpsRepo(), serviceerr.ToStatus)

	op, err := uc.Update(context.Background(), zone.UpdateInput{
		ID:   id,
		Mask: []string{"infra.hostClasses", "infra.underlayAnchor", "infra.failureDomainCount"},
	})
	require.NoError(t, err)
	require.Nil(t, op.Error)

	got := zoneInfraRow(t, pool, id)
	require.Empty(t, got.HostClasses, "a masked empty list clears the list")
	require.Equal(t, "", got.UnderlayAnchor, "a masked empty anchor clears the anchor")
	require.Equal(t, int32(0), got.FailureDomainCount, "a masked zero count clears the count")
	require.Equal(t, "ROOMY", got.CapacityHint, "a field outside the mask is untouched")
}

// TestZoneCannotBeReparented — the registry of by-design divergences claimed that
// re-pointing a zone at another region is a supported admin operation and that the
// id↔region relationship is enforced nowhere. Both halves are false, and this locks
// which way. Naming the field in the mask is refused synchronously in either spelling,
// an empty-mask full PATCH leaves the parent region exactly as it was, and the id is
// required to be prefixed by its region in the first place.
func TestZoneCannotBeReparented(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	rr := pg.NewRegionRepo(pool)
	zr, id := seedZoneWithInfra(t, pool)
	_, err := rr.Insert(ctx, &domain.Region{ID: "region-2", Name: "Region 2"})
	require.NoError(t, err)

	uc := zone.New(zr, zr, repomock.NewOpsRepo(), serviceerr.ToStatus)

	for _, spelling := range []string{"regionId", "region_id"} {
		_, uerr := uc.Update(ctx, zone.UpdateInput{ID: id, Mask: []string{spelling}})
		require.Error(t, uerr, "mask path %q must be refused", spelling)
		require.Equal(t, codes.InvalidArgument, status.Code(serviceerr.ToStatus(uerr)))
		require.Contains(t, serviceerr.ToStatus(uerr).Error(), "regionId is immutable after Zone.Create")
	}

	// Full PATCH with no mask at all: nothing in the request can carry a new parent,
	// and the stored parent must be untouched.
	_, err = uc.Update(ctx, zone.UpdateInput{ID: id, Name: "Zone A renamed"})
	require.NoError(t, err)
	var regionID string
	require.NoError(t, pool.QueryRow(ctx, `SELECT region_id FROM zones WHERE id = $1`, id).Scan(&regionID))
	require.Equal(t, "region-1", regionID, "a zone must not change its parent region")

	// The id is not a naming convention either — creating a zone whose id is not
	// prefixed by its region is refused before any FK is consulted.
	_, err = uc.Create(ctx, zone.CreateInput{ID: "region-1-x", RegionID: "region-2", Name: "Mismatched"})
	require.Error(t, err)
	require.Contains(t, serviceerr.ToStatus(err).Error(),
		"zone id 'region-1-x' must be prefixed by its regionId 'region-2'")
}

// TestRegionEmptyMaskDoesNotWipeCountryCode — the same class on the sibling
// resource: an empty-mask rename must not erase the country code the caller did not
// send, while naming it in the mask still clears it.
func TestRegionEmptyMaskDoesNotWipeCountryCode(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	rr := pg.NewRegionRepo(pool)
	_, err := rr.Insert(ctx, &domain.Region{ID: "region-1", Name: "Region 1", CountryCode: "RU"})
	require.NoError(t, err)

	uc := region.New(rr, rr, repomock.NewOpsRepo(), serviceerr.ToStatus)
	_, err = uc.Update(ctx, region.UpdateInput{ID: "region-1", Name: "Region One"})
	require.NoError(t, err)

	var cc string
	require.NoError(t, pool.QueryRow(ctx, `SELECT country_code FROM regions WHERE id = $1`, "region-1").Scan(&cc))
	require.Equal(t, "RU", cc, "a country code that was not sent must not be erased by a rename")

	_, err = uc.Update(ctx, region.UpdateInput{ID: "region-1", Mask: []string{"countryCode"}})
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, `SELECT country_code FROM regions WHERE id = $1`, "region-1").Scan(&cc))
	require.Equal(t, "", cc, "naming the field in the mask is how it is cleared")
}
