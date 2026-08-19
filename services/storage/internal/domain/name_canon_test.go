// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package domain

import "testing"

// ── Одна форма имени на дерево (#715) ────────────────────────────────────────
//
// Здесь стояла ВТОРАЯ форма — `^[a-z](...)`, — расходившаяся с канонической по
// одной оси: ведущая цифра. Проба закрепляет ту сторону, ради которой сведение
// делалось, и обе стороны сразу: канон принимает то, что прежняя форма
// отвергала, и по-прежнему отвергает то, что именем не является.

// TestDisplayNameAcceptsLeadingDigit — ведущая цифра законна: канон — DNS label
// по RFC 1123, а он её принимает. Прежняя форма storage её отвергала, и это
// расхождение было единственным между двумя правилами одного поля.
func TestDisplayNameAcceptsLeadingDigit(t *testing.T) {
	for _, n := range []string{"1vol", "9", "0-snap"} {
		if err := VolumeName(n).Validate(); err != nil {
			t.Errorf("VolumeName(%q) отвергнуто: %v", n, err)
		}
		if err := SnapshotName(n).Validate(); err != nil {
			t.Errorf("SnapshotName(%q) отвергнуто: %v", n, err)
		}
		if err := ImageName(n).Validate(); err != nil {
			t.Errorf("ImageName(%q) отвергнуто: %v", n, err)
		}
	}
}

// TestDisplayNameStillRejectsNonNames — отрицание в паре с положительным выше:
// расширение формы на цифру не открыло её ни для регистра, ни для подчёркивания,
// ни для дефиса по краям, ни для длины сверх 63.
func TestDisplayNameStillRejectsNonNames(t *testing.T) {
	bad := []string{"Vol-Data", "vol_data", "-vol", "vol-", "оём", "vol name",
		"way-too-long-name-that-clearly-exceeds-the-sixty-three-char-limit-x"}
	for _, n := range bad {
		if err := VolumeName(n).Validate(); err == nil {
			t.Errorf("VolumeName(%q) принято, want отказ", n)
		} else if err.Error() != "Illegal argument name" {
			t.Errorf("VolumeName(%q) message = %q, want контрактный текст", n, err.Error())
		}
	}
}

// TestDiskTypeNameFollowsTheSameForm — класс диска судится ТОЙ ЖЕ формой.
// Прежде его имя проверялось только длиной (≤253) и без набора символов, то
// есть на одном дереве жили два правила об одном предмете.
func TestDiskTypeNameFollowsTheSameForm(t *testing.T) {
	// Положительный контроль — посевные имена каталога (миграция 0004).
	for _, n := range []string{"block-standard", "block-balanced", "block-fast",
		"block-single", "block-io-max"} {
		d := DiskType{ID: n, Name: n, PerformanceTier: TierBalanced, Lifecycle: LifecycleActive}
		if err := d.Validate(); err != nil {
			t.Errorf("DiskType(name=%q) отвергнут: %v", n, err)
		}
	}
	// Отрицание.
	for _, n := range []string{"Block Standard", "block_standard", "-block"} {
		d := DiskType{ID: "block-x", Name: n, PerformanceTier: TierBalanced, Lifecycle: LifecycleActive}
		if err := d.Validate(); err == nil {
			t.Errorf("DiskType(name=%q) принят, want отказ", n)
		}
	}
}

// TestDiskTypeNameMayStayEmpty — пустое имя класса остаётся законным входом:
// подстановки умолчания у него нет (идентификатор — слаг, назначаемый
// администратором, и канону он ничем не обязан), поэтому требовать имя здесь
// значило бы отвергать правку описания у класса, заведённого без имени.
func TestDiskTypeNameMayStayEmpty(t *testing.T) {
	d := DiskType{ID: "block-x", PerformanceTier: TierBalanced, Lifecycle: LifecycleActive}
	if err := d.Validate(); err != nil {
		t.Fatalf("DiskType без имени отвергнут: %v", err)
	}
}
