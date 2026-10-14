// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Рецепты применения стенда взводят страж слоя площадки (kacho#3040); сам страж
// и его отказы — site_layer_guard_render_test.go (тег helmcharts).
package deploy_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const siteLayerEnforcedSet = "global.kacho.siteLayer.enforced=true"

// Рецепты применения взводят ручку и кладут слой площадки последним. Без
// взведённой ручки страж выше — слово без исполнителя.
func TestApplyRecipesEnforceTheSiteLayer(t *testing.T) {
	raw, err := os.ReadFile("Makefile")
	if err != nil {
		t.Fatalf("deploy/Makefile не прочитан: %v", err)
	}
	targets := parseRecipeTargets(string(raw))
	up, ok := targets["stack-up"]
	if !ok {
		t.Fatalf("цели stack-up в Makefile нет")
	}
	recipe := strings.Join(up.recipe, "\n")
	if !strings.Contains(recipe, siteLayerEnforcedSet) {
		t.Errorf("stack-up применяет стенд, не взводя %s", siteLayerEnforcedSet)
	}
	if !strings.Contains(recipe, "-secrets.yaml") {
		t.Errorf("stack-up не кладёт слой площадки (values.<стенд>-secrets.yaml)")
	}
	cut, err := os.ReadFile(filepath.Join(umbrellaDir, "cutover-fe3455.sh"))
	if err != nil {
		t.Fatalf("cutover-fe3455.sh не прочитан: %v", err)
	}
	if !strings.Contains(string(cut), siteLayerEnforcedSet) {
		t.Errorf("cutover-fe3455.sh применяет стенд, не взводя %s", siteLayerEnforcedSet)
	}
}
