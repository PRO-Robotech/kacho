// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// retiredblockstorageedgecatalog_injection_test.go — опыт: способен ли гейт
// каталога края упасть и способен ли он смолчать.
//
// Инъекция идёт ПО КАЖДОЙ оси отдельно. Одна общая проба «сломай что-нибудь»
// зеленела бы на гейте, у которого работает лишь одна половина: находка есть, а
// какая именно — не сказано.
//
// Вход синтетический НАМЕРЕННО: портить живой каталог ради доказательства
// нельзя, а предикат вынесен чистой функцией именно затем, чтобы его можно было
// прогнать, не трогая дерево.
package repohygiene

import (
	"strings"
	"testing"
)

// okRows — законный близнец: снятых типов нет, живые есть. На нём гейт обязан
// МОЛЧАТЬ, иначе всякое отрицание ниже зеленело бы на предикате, который
// находит нарушение всегда.
func okRows() []edgeCatalogRow {
	scope := func(t string) *struct {
		ObjectType string `json:"object_type"`
	} {
		return &struct {
			ObjectType string `json:"object_type"`
		}{ObjectType: t}
	}
	return []edgeCatalogRow{
		{FQN: "kacho.cloud.storage.v1.VolumeService/Get", Permission: "storage.volumes.get", ScopeExtractor: scope("storage_volume")},
		{FQN: "kacho.cloud.storage.v1.SnapshotService/Get", Permission: "storage.snapshots.get", ScopeExtractor: scope("storage_snapshot")},
		{FQN: "kacho.cloud.storage.v1.ImageService/Get", Permission: "storage.images.get", ScopeExtractor: scope("storage_image")},
		{FQN: "kacho.cloud.compute.v1.InstanceService/Get", Permission: "compute.instances.get", ScopeExtractor: scope("compute_instance")},
	}
}

func TestEdgeCatalogBlockStorageGate_SilentOnALegitimateCatalog(t *testing.T) {
	t.Parallel()
	found, c := auditEdgeCatalogBlockStorage(okRows())
	if len(found) != 0 {
		t.Fatalf("законный близнец объявлен находкой: %s", strings.Join(found, "; "))
	}
	if c.Rows == 0 || c.Scoped == 0 {
		t.Fatalf("перепись пуста при непустом входе: строк %d, с областью %d", c.Rows, c.Scoped)
	}
}

func TestEdgeCatalogBlockStorageGate_FindsARetiredPermission(t *testing.T) {
	t.Parallel()
	rows := append(okRows(), edgeCatalogRow{
		FQN:        "kacho.cloud.compute.v1.DiskService/Get",
		Permission: "compute.disks.get",
	})
	found, _ := auditEdgeCatalogBlockStorage(rows)
	if !containsSub(found, "compute.disks.get") {
		t.Fatalf("вернувшееся право снятого ресурса не названо находкой: %v", found)
	}
}

func TestEdgeCatalogBlockStorageGate_FindsARetiredObjectType(t *testing.T) {
	t.Parallel()
	scope := &struct {
		ObjectType string `json:"object_type"`
	}{ObjectType: "compute_image"}
	rows := append(okRows(), edgeCatalogRow{
		FQN:            "kacho.cloud.compute.v1.SomeService/Get",
		Permission:     "compute.other.get",
		ScopeExtractor: scope,
	})
	found, _ := auditEdgeCatalogBlockStorage(rows)
	if !containsSub(found, "compute_image") {
		t.Fatalf("вернувшийся снятый тип объекта не назван находкой: %v", found)
	}
}

// TestEdgeCatalogBlockStorageGate_FindsAnEmptyPositiveControl — отрицание без
// положительного контроля НЕ проходит.
//
// Ось несущая: без неё гейт зеленел бы на пустом каталоге, на переехавшем
// каталоге и на разборе, разучившемся читать поле области, — то есть ровно
// тогда, когда он ничего не проверяет.
func TestEdgeCatalogBlockStorageGate_FindsAnEmptyPositiveControl(t *testing.T) {
	t.Parallel()
	rows := []edgeCatalogRow{
		{FQN: "kacho.cloud.compute.v1.InstanceService/Get", Permission: "compute.instances.get"},
	}
	found, _ := auditEdgeCatalogBlockStorage(rows)
	for _, l := range liveEdgeBlockStorage {
		if !containsSub(found, l.ObjectType) {
			t.Fatalf("пропавший живой тип %q не назван находкой: %v", l.ObjectType, found)
		}
	}
}

// TestEdgeCatalogBlockStorageGate_OneFactAtATime — инъекция меняет РОВНО ОДИН
// факт против законного близнеца, и находка приходит только от него.
//
// Без этой проверки красное могло бы приходить от соседа, и доказательство
// способности падать было бы совпадением, а не опытом.
func TestEdgeCatalogBlockStorageGate_OneFactAtATime(t *testing.T) {
	t.Parallel()
	base, _ := auditEdgeCatalogBlockStorage(okRows())
	if len(base) != 0 {
		t.Fatalf("контроль не пуст, дельта неизмерима: %v", base)
	}
	injected, _ := auditEdgeCatalogBlockStorage(append(okRows(), edgeCatalogRow{
		FQN: "kacho.cloud.compute.v1.DiskService/Get", Permission: "compute.disks.get",
	}))
	if len(injected) != 1 {
		t.Fatalf("одно-фактная инъекция дала %d находок, а обязана одну: %v",
			len(injected), injected)
	}
}

func containsSub(hay []string, needle string) bool {
	for _, s := range hay {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}
