// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// modulemanifestjoins_injection_test.go — доказательство, что гейт вступлений
// СПОСОБЕН упасть и способен смолчать, по каждой оси отдельно
// (kacho#2687; `testing.md` §«Гейт на класс», п. 2).
//
// На синтетическом дереве, а не на дереве продукта: там инъекция меняла бы
// живой манифест, и её след пережил бы прогон. Здесь предмет существует by
// construction, и каждая ось меняет РОВНО ОДИН факт против законного близнеца.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// syntheticJoinsTree — дерево из двух манифестов с разделом `seed` и одного
// без него (у него вступлений нет законно, и гейт его не судит).
func syntheticJoinsTree(t *testing.T, vpcJoins string) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("каталог %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("файл %s: %v", rel, err)
		}
	}
	write("services/vpc/manifest.yaml", "module: vpc\nseed:\n  joins:\n"+vpcJoins)
	write("services/compute/manifest.yaml", "module: compute\nseed:\n  joins:\n"+lawfulJoin("kacho-compute"))
	write("services/geo/manifest.yaml", "module: geo\nresources: []\n")
	return root
}

// lawfulJoin — вступление в группу, которую служба объявляет: законный близнец
// каждой оси ниже.
func lawfulJoin(sa string) string {
	return "    - serviceAccount: {account: kacho-system, name: " + sa + "}\n" +
		"      group: {account: kacho-system, name: " + serviceDeclaredJoinGroup + "}\n"
}

func joinFaultsOn(t *testing.T, root string) []manifestJoinFault {
	t.Helper()
	files := readManifestJoins(t, newSyntheticTree(t, root))
	if len(files) != 3 {
		t.Fatalf("синтетика прочитана не целиком: манифестов %d, ждали 3", len(files))
	}
	return findManifestJoinFaults(files)
}

// Контроль: оба манифеста вступают ровно в объявленную группу — гейт молчит.
func TestManifestJoinsGateControlIsSilent(t *testing.T) {
	t.Parallel()
	if faults := joinFaultsOn(t, syntheticJoinsTree(t, lawfulJoin("kacho-vpc"))); len(faults) != 0 {
		t.Fatalf("гейт покраснел на законном дереве: %+v", faults)
	}
}

// Ось 1: вступление в группу, которую служба не объявляет, — находка с
// координатой файла и именем группы; законное вступление рядом ЦЕЛО, поэтому
// вторая половина (ровно одно объявленное) не срабатывает — краснеет одна ось.
func TestManifestJoinsGateFindsAJoinIntoAGroupTheServiceDoesNotDeclare(t *testing.T) {
	t.Parallel()
	faults := joinFaultsOn(t, syntheticJoinsTree(t,
		lawfulJoin("kacho-vpc")+
			"    - serviceAccount: {account: kacho-system, name: kacho-vpc}\n"+
			"      group: {account: kacho-system, name: module-quota-readers}\n"))
	if len(faults) != 1 {
		t.Fatalf("ждали одну находку, получили %d: %+v", len(faults), faults)
	}
	if faults[0].Rel != "services/vpc/manifest.yaml" || !strings.Contains(faults[0].Detail, "module-quota-readers") {
		t.Fatalf("находка не называет файл и группу: %+v", faults[0])
	}
}

// Ось 2: объявленного вступления НЕТ (а других нет тоже) — гейт краснеет:
// членство, ради которого группа существует, у модуля отсутствует.
func TestManifestJoinsGateFindsAManifestWithoutTheDeclaredJoin(t *testing.T) {
	t.Parallel()
	faults := joinFaultsOn(t, syntheticJoinsTree(t, "    []\n"))
	if len(faults) != 1 || faults[0].Rel != "services/vpc/manifest.yaml" ||
		!strings.Contains(faults[0].Detail, "0 раз") {
		t.Fatalf("ждали одну находку об отсутствующем вступлении в vpc: %+v", faults)
	}
}

// Ось 3: объявленное вступление продублировано — не «набор», а второе
// утверждение об одном членстве; гейт краснеет, называя число.
func TestManifestJoinsGateFindsADuplicatedDeclaredJoin(t *testing.T) {
	t.Parallel()
	faults := joinFaultsOn(t, syntheticJoinsTree(t, lawfulJoin("kacho-vpc")+lawfulJoin("kacho-vpc")))
	if len(faults) != 1 || !strings.Contains(faults[0].Detail, "2 раз") {
		t.Fatalf("ждали одну находку о двух вступлениях: %+v", faults)
	}
}

// Ось 4: манифест без раздела `seed` вступлений не несёт ЗАКОННО и находкой не
// является — иначе гейт требовал бы посев от модуля, у которого его нет.
func TestManifestJoinsGateIgnoresAManifestWithoutSeed(t *testing.T) {
	t.Parallel()
	files := readManifestJoins(t, newSyntheticTree(t, syntheticJoinsTree(t, lawfulJoin("kacho-vpc"))))
	for _, f := range files {
		if f.Module == "geo" && f.HasSeed {
			t.Fatalf("манифест без seed прочитан как несущий seed: %+v", f)
		}
	}
	if faults := findManifestJoinFaults(files); len(faults) != 0 {
		t.Fatalf("гейт судит манифест без раздела seed: %+v", faults)
	}
}
