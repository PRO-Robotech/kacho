// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// umbrella_profiles_test.go — чем пробы края читают профили чарта-зонта.
//
// Читатель жил в token_shape_test.go. Та проба судит ярус поставщика и
// переехала к чарту зонта (#2734), а читатель профилей нужен пробам края,
// которые остались здесь, и остался в этом пакете отдельным файлом: у проб
// края и у пакета зонта общего тестового кода нет.
package deploy_test

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

// umbrellaFromRoot — каталог чарта-зонта, адресованный от корня дерева.
// Единственное место пакета, где путь выписан: копия в каждом файле разошлась бы
// с деревом на той, которую забыли поправить при переезде каталога, — и такая
// проба читала бы «профилей нет» вместо того, чтобы упасть. Проверка, берущая
// корень сама (lanePrereqRoot), адресует каталог от своего корня этим же путём.
var umbrellaFromRoot = filepath.Join("deploy", "helm", "umbrella")

// umbrellaDir — тот же каталог, адресованный от этого пакета.
var umbrellaDir = filepath.Join("..", "..", umbrellaFromRoot)

// umbrellaValues loads one umbrella profile as a generic tree.
func umbrellaValues(t *testing.T, profile string) map[string]any {
	t.Helper()
	path := filepath.Join(umbrellaDir, profile)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var tree map[string]any
	if err := yaml.Unmarshal(raw, &tree); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return tree
}
