// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package domain_test

import (
	"testing"

	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
)

// Происхождение копии — НЕПОСРЕДСТВЕННЫЙ РОДИТЕЛЬ, и оно ровно одно.
//
// Оба глагола копирования не работали НИ РАЗУ с момента заведения, и причина у
// них одна: вставка копии с самого начала записывала родителя (`source_snapshot_id`
// у снимка, `source_image_id` у образа), но этот вид происхождения не признавал ни
// домен, ни контракт. Домен требовал источник СНЯТИЯ — том у снимка, снимок либо
// том у образа, — которого у копии нет, и отвергал её как ресурс без источника.
// Наружу столбец родителя не выходил вовсе: проекция чтения его не выбирала.
//
// Здесь стояла проба, закреплявшая обходной приём — наследование источника у
// оригинала. Она оставалась зелёной и после того, как приём сняли: ресурс с
// источником снятия валиден при любом правиле, поэтому она не различала «копия
// названа родителем» и «копии переписали чужое происхождение». Проба обязана
// падать на возврате обхода — эта падает.
func TestSnapshotCopyNamesItsParent(t *testing.T) {
	t.Parallel()

	src := domain.Snapshot{
		ID: "snp00000000000000001", ProjectID: "prj-1", Name: "src",
		SourceVolumeID: "vol00000000000000001", Status: domain.SnapshotStatusReady,
	}
	if err := src.Validate(); err != nil {
		t.Fatalf("положительный контроль: снимок, снятый с тома, обязан быть валиден: %v", err)
	}

	// Копия — как её строит use-case: родителем назван СНИМОК, тома у неё нет.
	cp := domain.Snapshot{
		ID: "snp00000000000000002", ProjectID: src.ProjectID, Name: "copy",
		SourceSnapshotID: src.ID, Status: domain.SnapshotStatusCreating,
	}
	if err := cp.Validate(); err != nil {
		t.Fatalf("копия обязана проходить проверку домена, назвав родителя: %v", err)
	}

	// Возврат обхода: у копии оказались оба происхождения сразу. Именно эта форма
	// получалась при наследовании, и именно она обязана отвергаться — иначе на
	// каждом пути пришлось бы решать, какое из двух происхождений настоящее.
	both := cp
	both.SourceVolumeID = src.SourceVolumeID
	if err := both.Validate(); err == nil {
		t.Fatal("два происхождения сразу обязаны отвергаться: происхождение ровно одно")
	}

	// Зеркало: без происхождения вовсе — по-прежнему отказ, иначе послабление
	// читалось бы как «домен принимает что угодно».
	orphan := cp
	orphan.SourceSnapshotID = ""
	if err := orphan.Validate(); err == nil {
		t.Fatal("снимок без происхождения обязан отвергаться")
	}
}

// У образа происхождений ТРИ вида, и ровно одно из них: снимок либо том (снятие,
// вход Create) либо образ (копирование, ставит Copy).
func TestImageCopyNamesItsParent(t *testing.T) {
	t.Parallel()

	base := domain.Image{
		ProjectID: "prj-1", RegionID: "reg-1", Name: "src",
		SourceVolume: "vol00000000000000001", Status: domain.ImageStatusReady,
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("положительный контроль: образ, снятый с тома, обязан быть валиден: %v", err)
	}

	cp := domain.Image{
		ProjectID: base.ProjectID, RegionID: "reg-2", Name: "copy",
		SourceImageID: "img00000000000000001", Status: domain.ImageStatusCreating,
	}
	if err := cp.Validate(); err != nil {
		t.Fatalf("копия образа обязана проходить проверку домена, назвав родителя: %v", err)
	}

	// Каждая пара из трёх видов — отказ. Перечислены все, а не одна: правило
	// «ровно одно» проверяется парами, и пропущенная пара — это разрешённая
	// комбинация, о которой никто не узнает.
	for name, mutate := range map[string]func(*domain.Image){
		"снимок и том":   func(i *domain.Image) { i.SourceSnapshot, i.SourceVolume = "snp-1", "vol-1" },
		"снимок и образ": func(i *domain.Image) { i.SourceSnapshot, i.SourceImageID = "snp-1", "img-1" },
		"том и образ":    func(i *domain.Image) { i.SourceVolume, i.SourceImageID = "vol-1", "img-1" },
	} {
		bad := domain.Image{
			ProjectID: "prj-1", RegionID: "reg-1", Name: "bad",
			Status: domain.ImageStatusCreating,
		}
		mutate(&bad)
		if err := bad.Validate(); err == nil {
			t.Fatalf("обязано отвергаться, происхождение ровно одно: %s", name)
		}
	}

	orphan := cp
	orphan.SourceImageID = ""
	if err := orphan.Validate(); err == nil {
		t.Fatal("образ без происхождения обязан отвергаться")
	}
}
