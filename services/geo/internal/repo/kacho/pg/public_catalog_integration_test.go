// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

// public_catalog_integration_test.go — публичные административные глаголы
// каталога размещения против НАСТОЯЩЕГО хранилища (приёмка
// sub-phase-ADM-1-geo-placement-catalog-admin, сценарии 08–11 и 14, DoD п.2).
//
// Каждое отрицание стоит в паре с законным близнецом, отличающимся ОДНИМ полем:
// отказ без близнеца зеленел бы и на сервисе, который отвергает всё. Отказы
// хранилища (09, 10) утверждаются по строке Operation.error, а не по факту
// «вызов вернулся»; синхронные отказы входа (08, 11) — gRPC-статусом и
// отсутствием строки в таблице.

import (
	"context"
	"encoding/json"
	"sort"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	operationpb "github.com/PRO-Robotech/corelib/api/corelib/operation"
	"github.com/PRO-Robotech/corelib/operations"
	geov1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/geo/v1"

	region "github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/api/region"
	zone "github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/api/zone"
	"github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/geo/internal/handler"
	"github.com/PRO-Robotech/kacho/services/geo/internal/repo/kacho/pg"
)

// catalogSurfaces — обе поверхности каталога поверх ОДНОГО хранилища и одних
// use-case'ов: ровно так их собирает композиционный корень.
type catalogSurfaces struct {
	pool       *pgxpool.Pool
	region     *handler.RegionHandler
	zone       *handler.ZoneHandler
	intRegion  *handler.InternalRegionHandler
	intZone    *handler.InternalZoneHandler
	adminCtx   context.Context
	adminPrinc string
}

func newCatalogSurfaces(t *testing.T) catalogSurfaces {
	t.Helper()
	pool := newTestPool(t)
	ops := operations.NewRepo(pool, "kacho_geo")
	rr, zr := pg.NewRegionRepo(pool), pg.NewZoneRepo(pool)
	ruc := region.New(rr, rr, ops, serviceerr.ToStatus)
	zuc := zone.New(zr, zr, ops, serviceerr.ToStatus)
	return catalogSurfaces{
		pool:       pool,
		region:     handler.NewRegionHandler(ruc),
		zone:       handler.NewZoneHandler(zuc),
		intRegion:  handler.NewInternalRegionHandler(ruc),
		intZone:    handler.NewInternalZoneHandler(zuc),
		adminCtx:   operations.WithPrincipal(context.Background(), operations.Principal{Type: "user", ID: "usr-adm1geo"}),
		adminPrinc: "user:usr-adm1geo",
	}
}

func requireOpOK(t *testing.T, op *operationpb.Operation, err error, what string) {
	t.Helper()
	require.NoError(t, err, what)
	require.True(t, op.GetDone(), "%s: catalog mutation must be done in the response itself", what)
	require.Nil(t, op.GetError(), "%s: Operation.error = %v", what, op.GetError())
}

func requireOpFailed(t *testing.T, op *operationpb.Operation, err error, code codes.Code, msg, what string) {
	t.Helper()
	require.NoError(t, err, "%s: a storage refusal is in Operation.error, not a gRPC status", what)
	require.True(t, op.GetDone(), what)
	require.NotNil(t, op.GetError(), "%s: Operation.error expected", what)
	require.Equal(t, int32(code), op.GetError().GetCode(), "%s: Operation.error.code", what)
	require.Equal(t, msg, op.GetError().GetMessage(), "%s: Operation.error.message", what)
}

func requireSyncInvalid(t *testing.T, err error, msg, what string) {
	t.Helper()
	st, ok := status.FromError(err)
	require.True(t, ok, "%s: gRPC status expected, got %v", what, err)
	require.Equal(t, codes.InvalidArgument, st.Code(), "%s: code", what)
	require.Equal(t, msg, st.Message(), "%s: message", what)
}

func regionRows(t *testing.T, pool *pgxpool.Pool, id string) int {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT count(*) FROM regions WHERE id=$1`, id).Scan(&n))
	return n
}

func zoneRows(t *testing.T, pool *pgxpool.Pool, id string) int {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT count(*) FROM zones WHERE id=$1`, id).Scan(&n))
	return n
}

// TestPublicCatalog_ADM1GEO08_InputFormRefusedSynchronously — сценарий 08: три
// отказа формы входа синхронным INVALID_ARGUMENT, у каждого — близнец,
// отличающийся одним полем и проходящий.
func TestPublicCatalog_ADM1GEO08_InputFormRefusedSynchronously(t *testing.T) {
	s := newCatalogSurfaces(t)
	ctx := s.adminCtx
	for _, id := range []string{"adm1g-cp", "adm1g-other"} {
		op, err := s.region.Create(ctx, &geov1.CreatePublicRegionRequest{Id: id, Status: geov1.GeoStatus_UP})
		requireOpOK(t, op, err, "seed region "+id)
	}

	_, err := s.region.Create(ctx, &geov1.CreatePublicRegionRequest{Id: "Bad_Id"})
	requireSyncInvalid(t, err, "invalid region id 'Bad_Id'", "malformed region id")
	op, err := s.region.Create(ctx, &geov1.CreatePublicRegionRequest{Id: "adm1g-ok"})
	requireOpOK(t, op, err, "twin: well-formed region id")

	_, err = s.zone.Create(ctx, &geov1.CreatePublicZoneRequest{Id: "adm1g-cp-a", RegionId: "adm1g-other"})
	requireSyncInvalid(t, err, "zone id 'adm1g-cp-a' must be prefixed by its regionId 'adm1g-other'", "zone coupling")
	require.Zero(t, zoneRows(t, s.pool, "adm1g-cp-a"), "a refused zone must not be stored")
	op, err = s.zone.Create(ctx, &geov1.CreatePublicZoneRequest{Id: "adm1g-cp-a", RegionId: "adm1g-cp"})
	requireOpOK(t, op, err, "twin: zone id prefixed by its region")

	_, err = s.region.Create(ctx, &geov1.CreatePublicRegionRequest{Id: "adm1g-cc", CountryCode: "rus"})
	requireSyncInvalid(t, err, "countryCode must be an ISO-3166 alpha-2 code", "country code")
	require.Zero(t, regionRows(t, s.pool, "adm1g-cc"), "a refused region must not be stored")
	op, err = s.region.Create(ctx, &geov1.CreatePublicRegionRequest{Id: "adm1g-cc", CountryCode: "RU"})
	requireOpOK(t, op, err, "twin: alpha-2 country code")
}

// TestPublicCatalog_ADM1GEO09_ZoneInMissingRegion — сценарий 09: зона в
// несуществующем регионе отвергается хранилищем (FK) в Operation.error; после
// заведения региона тот же запрос проходит.
func TestPublicCatalog_ADM1GEO09_ZoneInMissingRegion(t *testing.T) {
	s := newCatalogSurfaces(t)
	ctx := s.adminCtx
	req := &geov1.CreatePublicZoneRequest{Id: "adm1g-ghost-a", RegionId: "adm1g-ghost", Status: geov1.GeoStatus_UP}

	op, err := s.zone.Create(ctx, req)
	requireOpFailed(t, op, err, codes.FailedPrecondition, "Zone adm1g-ghost-a violates a reference constraint", "zone in a missing region")
	require.Zero(t, zoneRows(t, s.pool, "adm1g-ghost-a"))

	op, err = s.region.Create(ctx, &geov1.CreatePublicRegionRequest{Id: "adm1g-ghost", Status: geov1.GeoStatus_UP})
	requireOpOK(t, op, err, "create the region")
	op, err = s.zone.Create(ctx, req)
	requireOpOK(t, op, err, "twin: the same zone once its region exists")
}

// TestPublicCatalog_ADM1GEO10_UpdateOfMissingRegion — сценарий 10: правка
// несуществующего региона — NOT_FOUND в Operation.error по полосе прямого
// чтения; тот же запрос к существующему — проходит.
func TestPublicCatalog_ADM1GEO10_UpdateOfMissingRegion(t *testing.T) {
	s := newCatalogSurfaces(t)
	ctx := s.adminCtx
	upd := func(id string) (*operationpb.Operation, error) {
		return s.region.Update(ctx, &geov1.UpdatePublicRegionRequest{
			RegionId: id, Status: geov1.GeoStatus_UP,
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"status"}},
		})
	}
	op, err := upd("adm1g-none")
	requireOpFailed(t, op, err, codes.NotFound, "Region adm1g-none not found", "update of a missing region")

	op, err = s.region.Create(ctx, &geov1.CreatePublicRegionRequest{Id: "adm1g-some"})
	requireOpOK(t, op, err, "create the twin region")
	op, err = upd("adm1g-some")
	requireOpOK(t, op, err, "twin: update of an existing region")
}

// TestPublicCatalog_ADM1GEO11_MaskOnTheStoredRow — сценарий 11 на строке
// хранилища: отвергнутая маска не меняет ничего, близнец по полю status — меняет.
func TestPublicCatalog_ADM1GEO11_MaskOnTheStoredRow(t *testing.T) {
	s := newCatalogSurfaces(t)
	ctx := s.adminCtx
	op, err := s.region.Create(ctx, &geov1.CreatePublicRegionRequest{Id: "adm1g-m", Status: geov1.GeoStatus_UP})
	requireOpOK(t, op, err, "region")
	op, err = s.zone.Create(ctx, &geov1.CreatePublicZoneRequest{Id: "adm1g-m-a", RegionId: "adm1g-m", Status: geov1.GeoStatus_UP})
	requireOpOK(t, op, err, "zone")

	_, err = s.zone.Update(ctx, &geov1.UpdatePublicZoneRequest{
		ZoneId: "adm1g-m-a", Status: geov1.GeoStatus_DOWN,
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"infra.hostClasses"}},
	})
	st, _ := status.FromError(err)
	require.Equal(t, codes.InvalidArgument, st.Code())
	require.Equal(t, "invalid argument", st.Message())
	var desc string
	for _, d := range st.Details() {
		if br, ok := d.(*errdetails.BadRequest); ok {
			for _, v := range br.GetFieldViolations() {
				if v.GetField() == "update_mask" {
					desc = v.GetDescription()
				}
			}
		}
	}
	require.Equal(t, "unknown field in update_mask: infra.hostClasses", desc)

	_, err = s.zone.Update(ctx, &geov1.UpdatePublicZoneRequest{
		ZoneId: "adm1g-m-a", UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"regionId"}},
	})
	requireSyncInvalid(t, err, "regionId is immutable after Zone.Create", "zone regionId")

	_, err = s.region.Update(ctx, &geov1.UpdatePublicRegionRequest{
		RegionId: "adm1g-m", UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"infra.numericInfraId"}},
	})
	requireSyncInvalid(t, err, "numericInfraId is immutable after Region.Create", "region numericInfraId")

	got, err := s.intZone.GetInternal(ctx, &geov1.GetInternalZoneRequest{ZoneId: "adm1g-m-a"})
	require.NoError(t, err)
	require.Equal(t, geov1.GeoStatus_UP, got.GetStatus(), "a refused mask must not have changed the row")

	op, err = s.zone.Update(ctx, &geov1.UpdatePublicZoneRequest{
		ZoneId: "adm1g-m-a", Status: geov1.GeoStatus_DOWN,
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"status"}},
	})
	requireOpOK(t, op, err, "twin: mask status")
	got, err = s.intZone.GetInternal(ctx, &geov1.GetInternalZoneRequest{ZoneId: "adm1g-m-a"})
	require.NoError(t, err)
	require.Equal(t, geov1.GeoStatus_DOWN, got.GetStatus(), "the twin must have changed the row")
}

// outboxKeys — набор ключей payload и исполнитель последней строки аудита.
func outboxKeys(t *testing.T, pool *pgxpool.Pool, kind, id, event string) ([]string, string) {
	t.Helper()
	var raw []byte
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT payload FROM geo_outbox WHERE resource_kind=$1 AND resource_id=$2 AND event_type=$3
		  ORDER BY sequence_no DESC LIMIT 1`, kind, id, event).Scan(&raw))
	var m map[string]any
	require.NoError(t, json.Unmarshal(raw, &m))
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	actor, _ := m["actor"].(string)
	return keys, actor
}

// withoutIdentity обнуляет id и createdAt — единственные поля, которыми два
// ресурса, записанные одинаковым входом, вправе различаться.
func withoutIdentity[M proto.Message](m M, clear func(M)) M {
	c := proto.Clone(m).(M)
	clear(c)
	return c
}

// TestPublicCatalog_ADM1GEO14_TwoVerbsWriteTheSame — сценарий 14: публичный и
// внутренний глаголы при одинаковом входе пишут одно и то же — в обеих
// проекциях и в строке аудита (тот же набор ключей, непустой исполнитель).
// Регион, зона и правка.
func TestPublicCatalog_ADM1GEO14_TwoVerbsWriteTheSame(t *testing.T) {
	s := newCatalogSurfaces(t)
	ctx := s.adminCtx

	op, err := s.region.Create(ctx, &geov1.CreatePublicRegionRequest{Id: "adm1g-pa", CountryCode: "RU", Status: geov1.GeoStatus_UP})
	requireOpOK(t, op, err, "public region")
	op, err = s.intRegion.Create(ctx, &geov1.CreateRegionRequest{Id: "adm1g-pb", CountryCode: "RU", Status: geov1.GeoStatus_UP})
	requireOpOK(t, op, err, "internal region")

	clearRegion := func(r *geov1.InternalRegion) { r.Id = ""; r.CreatedAt = nil }
	clearPubRegion := func(r *geov1.Region) { r.Id = ""; r.CreatedAt = nil }
	ia, err := s.intRegion.GetInternal(ctx, &geov1.GetInternalRegionRequest{RegionId: "adm1g-pa"})
	require.NoError(t, err)
	ib, err := s.intRegion.GetInternal(ctx, &geov1.GetInternalRegionRequest{RegionId: "adm1g-pb"})
	require.NoError(t, err)
	require.True(t, proto.Equal(withoutIdentity(ia, clearRegion), withoutIdentity(ib, clearRegion)),
		"GetInternal differs between the two write paths:\npublic   %v\ninternal %v", ia, ib)
	pa, err := s.region.Get(ctx, &geov1.GetRegionRequest{RegionId: "adm1g-pa"})
	require.NoError(t, err)
	pb, err := s.region.Get(ctx, &geov1.GetRegionRequest{RegionId: "adm1g-pb"})
	require.NoError(t, err)
	require.True(t, proto.Equal(withoutIdentity(pa, clearPubRegion), withoutIdentity(pb, clearPubRegion)),
		"public Get differs between the two write paths:\npublic   %v\ninternal %v", pa, pb)
	ka, aa := outboxKeys(t, s.pool, "Region", "adm1g-pa", "CREATED")
	kb, ab := outboxKeys(t, s.pool, "Region", "adm1g-pb", "CREATED")
	require.Equal(t, kb, ka, "audit row keys differ between the two write paths")
	require.Equal(t, s.adminPrinc, aa, "public path audit actor")
	require.Equal(t, s.adminPrinc, ab, "internal path audit actor")

	op, err = s.zone.Create(ctx, &geov1.CreatePublicZoneRequest{Id: "adm1g-pa-a", RegionId: "adm1g-pa", Status: geov1.GeoStatus_DOWN})
	requireOpOK(t, op, err, "public zone")
	op, err = s.intZone.Create(ctx, &geov1.CreateZoneRequest{Id: "adm1g-pb-a", RegionId: "adm1g-pb", Status: geov1.GeoStatus_DOWN})
	requireOpOK(t, op, err, "internal zone")
	clearZone := func(z *geov1.InternalZone) { z.Id = ""; z.RegionId = ""; z.CreatedAt = nil }
	clearPubZone := func(z *geov1.Zone) { z.Id = ""; z.RegionId = ""; z.CreatedAt = nil }
	za, err := s.intZone.GetInternal(ctx, &geov1.GetInternalZoneRequest{ZoneId: "adm1g-pa-a"})
	require.NoError(t, err)
	zb, err := s.intZone.GetInternal(ctx, &geov1.GetInternalZoneRequest{ZoneId: "adm1g-pb-a"})
	require.NoError(t, err)
	require.True(t, proto.Equal(withoutIdentity(za, clearZone), withoutIdentity(zb, clearZone)),
		"zone GetInternal differs:\npublic   %v\ninternal %v", za, zb)
	pza, err := s.zone.Get(ctx, &geov1.GetZoneRequest{ZoneId: "adm1g-pa-a"})
	require.NoError(t, err)
	pzb, err := s.zone.Get(ctx, &geov1.GetZoneRequest{ZoneId: "adm1g-pb-a"})
	require.NoError(t, err)
	require.True(t, proto.Equal(withoutIdentity(pza, clearPubZone), withoutIdentity(pzb, clearPubZone)),
		"zone public Get differs:\npublic   %v\ninternal %v", pza, pzb)
	zka, zaa := outboxKeys(t, s.pool, "Zone", "adm1g-pa-a", "CREATED")
	zkb, zab := outboxKeys(t, s.pool, "Zone", "adm1g-pb-a", "CREATED")
	require.Equal(t, zkb, zka, "zone audit row keys differ")
	require.Equal(t, s.adminPrinc, zaa)
	require.Equal(t, s.adminPrinc, zab)

	// Правка: одинаковая маска обоими глаголами.
	op, err = s.zone.Update(ctx, &geov1.UpdatePublicZoneRequest{ZoneId: "adm1g-pa-a", Status: geov1.GeoStatus_UP,
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"status"}}})
	requireOpOK(t, op, err, "public zone update")
	op, err = s.intZone.Update(ctx, &geov1.UpdateZoneRequest{ZoneId: "adm1g-pb-a", Status: geov1.GeoStatus_UP,
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"status"}}})
	requireOpOK(t, op, err, "internal zone update")
	za, err = s.intZone.GetInternal(ctx, &geov1.GetInternalZoneRequest{ZoneId: "adm1g-pa-a"})
	require.NoError(t, err)
	zb, err = s.intZone.GetInternal(ctx, &geov1.GetInternalZoneRequest{ZoneId: "adm1g-pb-a"})
	require.NoError(t, err)
	require.True(t, proto.Equal(withoutIdentity(za, clearZone), withoutIdentity(zb, clearZone)),
		"zone GetInternal after update differs:\npublic   %v\ninternal %v", za, zb)
	uka, uaa := outboxKeys(t, s.pool, "Zone", "adm1g-pa-a", "UPDATED")
	ukb, uab := outboxKeys(t, s.pool, "Zone", "adm1g-pb-a", "UPDATED")
	require.Equal(t, ukb, uka, "zone update audit row keys differ")
	require.Equal(t, s.adminPrinc, uaa)
	require.Equal(t, s.adminPrinc, uab)

	op, err = s.region.Update(ctx, &geov1.UpdatePublicRegionRequest{RegionId: "adm1g-pa", CountryCode: "KZ",
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"countryCode"}}})
	requireOpOK(t, op, err, "public region update")
	op, err = s.intRegion.Update(ctx, &geov1.UpdateRegionRequest{RegionId: "adm1g-pb", CountryCode: "KZ",
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"countryCode"}}})
	requireOpOK(t, op, err, "internal region update")
	ia, err = s.intRegion.GetInternal(ctx, &geov1.GetInternalRegionRequest{RegionId: "adm1g-pa"})
	require.NoError(t, err)
	ib, err = s.intRegion.GetInternal(ctx, &geov1.GetInternalRegionRequest{RegionId: "adm1g-pb"})
	require.NoError(t, err)
	require.Equal(t, "KZ", ia.GetCountryCode())
	require.True(t, proto.Equal(withoutIdentity(ia, clearRegion), withoutIdentity(ib, clearRegion)),
		"region GetInternal after update differs:\npublic   %v\ninternal %v", ia, ib)
}
