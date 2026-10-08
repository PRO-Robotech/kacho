// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

// notify_probe_manifest_delivery_injection_test.go — гейт «манифест пробы
// доставлен ровно цепочкам, поднимающим пробу» способен упасть, падает НА СВОЁМ
// предмете и молчит на законных близнецах.
//
// Входы — настоящие: профили стенда из дерева, настоящий производитель и
// настоящий рендер; дефект подаётся одним добавочным слоем профиля либо
// синтетическим корнем дерева. Суд — та же функция, что у гейта.
//
//	близнец       — dev и prod как есть: молчит;
//	инъекция (а)  — боевой цепочке слой объявляет условную доставку с условием,
//	                истинным в боевой установке: манифест доставлен, пробы нет;
//	инъекция (б)  — манифест пробы положен в безусловный обход
//	                (`services/notify-probe/manifest.yaml`): боевой цепочке он
//	                достаётся без всякого объявления;
//	инъекция (в)  — стенду слой снимает перечень условной доставки: проба
//	                поднята, манифест не доставлен.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// injectionLayer — добавочный профиль цепочки во временном каталоге.
func injectionLayer(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "injection.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("слой инъекции не записан: %v", err)
	}
	return p
}

// probeRowFor — исходы одного стенда на корне root и цепочке chain (пути от
// каталога deploy либо абсолютные).
func probeRowFor(t *testing.T, root, stack string, chain []string) probeDeliveryRow {
	t.Helper()
	return probeDeliveryRow{
		Stack:     stack,
		Rendered:  renderedHasProbeWorkload(t, renderChainAbs(t, chain)),
		Delivered: deliveredProbeManifest(t, root, stack, chain),
	}
}

// requireOneFinding — ровно одна находка, и она называет стенд и предмет.
func requireOneFinding(t *testing.T, label string, rows []probeDeliveryRow, wants ...string) {
	t.Helper()
	findings, census := auditProbeManifestDelivery(rows)
	if len(findings) != 1 {
		t.Fatalf("%s: находок %d, ожидалась одна (%s): %v", label, len(findings), census.Summary(), findings)
	}
	for _, w := range wants {
		if !strings.Contains(findings[0], w) {
			t.Errorf("%s: находка не называет %q — чинить придётся перебором: %s", label, w, findings[0])
		}
	}
}

func TestStandOnlyProbeManifestGateFallsOnItsSubjectAndStaysSilentOnTheTwin(t *testing.T) {
	stacks := deployStacks(t)
	dev, prod := chainPaths(stacks["dev"]), chainPaths(stacks["prod"])
	if len(dev) == 0 || len(prod) == 0 {
		t.Fatalf("предпосылка инъекции: стенды dev и prod обязаны быть в %s", stacksTable)
	}

	// ── БЛИЗНЕЦ. Дерево как есть: стенд с пробой получает манифест, боевой — нет.
	twin := []probeDeliveryRow{
		probeRowFor(t, repoRoot, "dev", dev),
		probeRowFor(t, repoRoot, "prod", prod),
	}
	if f, c := auditProbeManifestDelivery(twin); len(f) != 0 || c.Rendered != 1 || c.Delivered != 1 {
		t.Fatalf("близнец: ожидалось молчание при одной поднятой и одной доставке, %s: %v", c.Summary(), f)
	}

	// ── ИНЪЕКЦИЯ (а). Боевой цепочке объявлена условная доставка с условием,
	// истинным там (`kaname.manifests.required`), — доставлено, не поднято.
	condProd := append(append([]string{}, prod...), injectionLayer(t,
		"kaname:\n  manifests:\n    conditional:\n"+
			"      - source: "+probeManifestSource+"\n"+
			"        enabledBy: kaname.manifests.required\n"))
	requireOneFinding(t, "инъекция (а)", []probeDeliveryRow{probeRowFor(t, repoRoot, "prod", condProd)},
		"стенд prod", probeManifestKey, "нет")

	// ── ИНЪЕКЦИЯ (б). Манифест пробы в БЕЗУСЛОВНОМ обходе синтетического корня:
	// боевая цепочка не объявляет ничего, а манифест ей достаётся.
	root := t.TempDir()
	entries, err := os.ReadDir(filepath.Join(repoRoot, "services"))
	if err != nil {
		t.Fatalf("каталог служб не прочитан: %v", err)
	}
	copied := 0
	for _, e := range entries {
		src := filepath.Join(repoRoot, "services", e.Name(), "manifest.yaml")
		// #nosec G304 -- путь собран из константы repoRoot и имени каталога обхода.
		body, err := os.ReadFile(src)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatalf("%s не прочитан: %v", src, err)
		}
		writeUnder(t, root, filepath.Join("services", e.Name(), "manifest.yaml"), body)
		copied++
	}
	if copied == 0 {
		t.Fatal("предпосылка инъекции (б): безусловных манифестов в дереве ноль")
	}
	probeBody, err := os.ReadFile(filepath.Join(repoRoot, probeManifestSource))
	if err != nil {
		t.Fatalf("манифест пробы не прочитан: %v", err)
	}
	writeUnder(t, root, "services/notify-probe/manifest.yaml", probeBody)
	requireOneFinding(t, "инъекция (б)", []probeDeliveryRow{probeRowFor(t, root, "prod", prod)},
		"стенд prod", probeManifestKey)

	// ── ИНЪЕКЦИЯ (в). Стенду снят перечень условной доставки — проба поднята,
	// манифест не доставлен.
	bareDev := append(append([]string{}, dev...), injectionLayer(t,
		"kaname:\n  manifests:\n    conditional: null\n"))
	requireOneFinding(t, "инъекция (в)", []probeDeliveryRow{probeRowFor(t, repoRoot, "dev", bareDev)},
		"стенд dev", probeWorkload, "не доставлен")
}

// writeUnder — файл rel под корнем root с промежуточными каталогами.
func writeUnder(t *testing.T, root, rel string, body []byte) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatalf("каталог %s не заведён: %v", filepath.Dir(p), err)
	}
	if err := os.WriteFile(p, body, 0o600); err != nil {
		t.Fatalf("%s не записан: %v", p, err)
	}
}
