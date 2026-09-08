// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// modulemanifestreadable_injection_test.go — опыт: способен ли гейт
// разбираемости манифестов упасть и способен ли он смолчать.
//
// Инъекция идёт по КАЖДОЙ оси отдельно и меняет РОВНО ОДИН факт против
// законного близнеца: дельта вычисляется сравнением с контролем, а не
// объявляется.
package repohygiene

import (
	"strings"
	"testing"
)

// livePlatformManifests — законный близнец: два модуля, разные имена, разные
// типы. Взят по форме живого дерева, где каталог `nlb` объявляет модуль
// `loadbalancer`: несовпадение каталога и имени законно и находкой быть не должно.
func livePlatformManifests() []readableManifest {
	mk := func(rel, module string, types ...string) readableManifest {
		d := moduleManifestDoc{APIVersion: "iam/v1", Module: module}
		for _, t := range types {
			d.Resources = append(d.Resources, struct {
				ObjectType string `yaml:"objectType"`
			}{ObjectType: t})
		}
		return readableManifest{Rel: rel, Doc: d}
	}
	return []readableManifest{
		mk("services/vpc/manifest.yaml", "vpc", "vpc_network", "vpc_subnet"),
		mk("services/nlb/manifest.yaml", "loadbalancer", "nlb_listener"),
	}
}

func TestReadableManifestsGate_SilentOnALegitimateTree(t *testing.T) {
	t.Parallel()
	if found := auditReadableManifests(livePlatformManifests()); len(found) != 0 {
		t.Fatalf("законный близнец объявлен находкой: %s", strings.Join(found, "; "))
	}
}

func TestReadableManifestsGate_FindsAnEmptyAPIVersion(t *testing.T) {
	t.Parallel()
	docs := livePlatformManifests()
	docs[0].Doc.APIVersion = ""
	found := auditReadableManifests(docs)
	if len(found) != 1 || !strings.Contains(found[0], "apiVersion") {
		t.Fatalf("пустой адресат не назван ровно одной находкой: %v", found)
	}
}

func TestReadableManifestsGate_FindsAnEmptyModule(t *testing.T) {
	t.Parallel()
	docs := livePlatformManifests()
	docs[1].Doc.Module = ""
	found := auditReadableManifests(docs)
	if len(found) != 1 || !strings.Contains(found[0], "module") {
		t.Fatalf("пустое имя модуля не названо ровно одной находкой: %v", found)
	}
}

func TestReadableManifestsGate_FindsACollidingModule(t *testing.T) {
	t.Parallel()
	docs := livePlatformManifests()
	docs[1].Doc.Module = docs[0].Doc.Module
	found := auditReadableManifests(docs)
	if len(found) != 1 || !strings.Contains(found[0], "объявлен 2 документами") {
		t.Fatalf("столкновение имён модулей не названо ровно одной находкой: %v", found)
	}
}

func TestReadableManifestsGate_FindsACollidingObjectType(t *testing.T) {
	t.Parallel()
	docs := livePlatformManifests()
	docs[1].Doc.Resources[0].ObjectType = "vpc_network"
	found := auditReadableManifests(docs)
	if len(found) != 1 || !strings.Contains(found[0], `"vpc_network"`) {
		t.Fatalf("столкновение типов объектов не названо ровно одной находкой: %v", found)
	}
}

func TestReadableManifestsGate_FindsAResourceWithoutAnObjectType(t *testing.T) {
	t.Parallel()
	docs := livePlatformManifests()
	docs[0].Doc.Resources[1].ObjectType = "   "
	found := auditReadableManifests(docs)
	if len(found) != 1 || !strings.Contains(found[0], "objectType") {
		t.Fatalf("ресурс без типа объекта не назван ровно одной находкой: %v", found)
	}
}

// TestReadableManifestsGate_DirectoryNeedNotMatchTheModuleName — законный
// близнец в отдельной пробе: несовпадение каталога и имени модуля НЕ находка.
//
// Ось названа отдельно, потому что соблазн потребовать совпадения велик, а
// требование объявило бы находкой действующее решение дерева.
func TestReadableManifestsGate_DirectoryNeedNotMatchTheModuleName(t *testing.T) {
	t.Parallel()
	docs := []readableManifest{{
		Rel: "services/nlb/manifest.yaml",
		Doc: moduleManifestDoc{APIVersion: "iam/v1", Module: "loadbalancer"},
	}}
	if found := auditReadableManifests(docs); len(found) != 0 {
		t.Fatalf("несовпадение каталога и имени модуля объявлено находкой: %v", found)
	}
}
