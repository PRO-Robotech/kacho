// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// render_geo_test.go — deploy render-guard for the geo backend cutover.
//
// The api-gateway dials the kacho-geo gRPC backend by host:port. The geo k8s
// Service is "kacho-geo" (public) / "kacho-geo-internal" (internal); the bare
// "geo.kacho.svc" host does NOT resolve (NXDOMAIN) → the grpc
// balancer reports "no children to pick from" → every authenticated /geo/v1/*
// request returns 503. This guard renders the helm chart and asserts the chart
// emits the correct dial host (and the geo mTLS edge env when the edge is on),
// so a future stale-host regression fails the build, not production.
package deploy_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// helmTemplate renders deploy/ with the given --set overrides. On a dev machine
// without helm the render-guard is skipped (keeps `go test ./...` green), but in
// CI (env CI set — GitHub Actions sets it) a missing helm binary is a hard
// FAILURE, not a skip: the render guards must never silently become inert on the
// job that gates merge. The CI job installs helm (azure/setup-helm), so this
// path only fires if that step is dropped.
func helmTemplate(t *testing.T, sets ...string) string {
	t.Helper()
	if _, err := exec.LookPath("helm"); err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("helm binary not on PATH in CI — render-guard must run, not skip (add azure/setup-helm to the job)")
		}
		t.Skip("helm binary not on PATH — skipping deploy render-guard")
	}
	args := []string{"template", "."}
	for _, s := range sets {
		args = append(args, "--set", s)
	}
	cmd := exec.Command("helm", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helm template failed: %v\n%s", err, out)
	}
	return string(out)
}

// TestRender_GeoBackendHost — the rendered Deployment must set the geo gRPC
// backend env to the real Service host (kacho-geo / kacho-geo-internal), never
// the NXDOMAIN "geo.kacho.svc".
func TestRender_GeoBackendHost(t *testing.T) {
	out := helmTemplate(t)

	mustContain(t, out, "KACHO_API_GATEWAY_GEO_GRPC")
	mustContain(t, out, "kacho-geo.kacho.svc:9090")
	mustContain(t, out, "KACHO_API_GATEWAY_GEO_INTERNAL_GRPC")
	mustContain(t, out, "kacho-geo-internal.kacho.svc:9091")

	if strings.Contains(out, "geo.kacho.svc") &&
		!strings.Contains(out, "kacho-geo.kacho.svc") {
		t.Fatalf("stale NXDOMAIN geo host rendered; want kacho-geo.*")
	}
	// Belt-and-suspenders: the bare "geo." host (not prefixed by kacho-) must be
	// absent from the geo env value lines.
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "value:") && strings.Contains(line, " geo.kacho.svc") {
			t.Fatalf("stale geo host in rendered env line: %q", strings.TrimSpace(line))
		}
	}
}

// TestRender_GeoMTLSEdgeEnabled — when the geo mTLS edge is enabled the chart
// must emit KACHO_API_GATEWAY_MTLS_GEO_ENABLE=true (so dialOpts["geo"] /
// dialOpts["geoInternal"] carry the gateway client cert). Mirrors the compute
// edge wiring. SERVER_NAME stays unset (derived per dial-host like iam/nlb).
func TestRender_GeoMTLSEdgeEnabled(t *testing.T) {
	out := helmTemplate(t, "mtls.enable=true", "mtls.edges.geo=true")
	mustContain(t, out, "KACHO_API_GATEWAY_MTLS_GEO_ENABLE")
	// the geo backend host must still be correct under the mTLS profile.
	mustContain(t, out, "kacho-geo.kacho.svc:9090")
}

// Здесь стояли три стража ручки `internalListener` края — «переменные mTLS
// рендерятся», «по умолчанию выключено» и «рендер падает без mtls.enable».
// Предмета у всех трёх нет с задачи #1024: внутреннего gRPC-листенера у края нет
// вовсе, ручки нет, и падать рендеру не на чем.
//
// Стражи СНЯТЫ ВМЕСТЕ С ПРЕДМЕТОМ, а не ослаблены: проба, чей предмет снят, либо
// утверждает несуществующее, либо не может упасть. Их место занял более широкий
// страж, который у прежних был бы недостижим, — «ни одной переменной
// KACHO_API_GATEWAY_INTERNAL_GRPC_* ни на одной посадке, даже когда оверлей
// выставляет снятые значения» (render_internal_listener_retired_test.go), плюс
// парная к нему сторона: адреса ВНУТРЕННИХ листенеров СОСЕДЕЙ обязаны уцелеть,
// иначе запрет стал бы маской, снявшей связь края с сервисами.

// TestRender_AppEnv — the first-class appEnv knob drives the domain-LESS
// KACHO_APP_ENV deployment label that keys the composition-root production guards
// (validateProductionAuthzConfig; страж внутреннего листенера снят вместе с самим
// листенером, #1024). Default ""
// → not emitted (Go default "" is already production-class / fail-closed); an
// explicit label renders the env so the dev overlay can opt into the dev-class
// tolerance and prod overlays can be explicit.
func TestRender_AppEnv(t *testing.T) {
	if strings.Contains(helmTemplate(t), "KACHO_APP_ENV") {
		t.Fatalf("KACHO_APP_ENV must not render when appEnv is unset (Go default '' = production-class)")
	}
	out := helmTemplate(t, "appEnv=prod-sentinel-42")
	mustContain(t, out, "KACHO_APP_ENV")
	mustContain(t, out, "prod-sentinel-42")
}

func mustContain(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Fatalf("rendered manifest missing %q", needle)
	}
}
