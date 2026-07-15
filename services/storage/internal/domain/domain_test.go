// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package domain

import "testing"

// TestVolumeNameValidate — self-validating newtype: пусто ok; валидные lowercase-
// имена ok; too-long(>63)/uppercase/невалидный-символ → фикс. "Illegal argument
// name" (контрактный текст S1-11, важное #2).
func TestVolumeNameValidate(t *testing.T) {
	ok := []string{"", "vol-data-1", "a", "v0", "my-vol-name-9"}
	for _, n := range ok {
		if err := VolumeName(n).Validate(); err != nil {
			t.Errorf("VolumeName(%q) rejected: %v", n, err)
		}
	}
	bad := []string{"Vol-Data", "vol_data!", "1vol", "-vol", "vol-", "оём",
		"way-too-long-name-that-clearly-exceeds-the-sixty-three-char-limit-x"}
	for _, n := range bad {
		err := VolumeName(n).Validate()
		if err == nil {
			t.Errorf("VolumeName(%q): expected error, got nil", n)
			continue
		}
		if err.Error() != "Illegal argument name" {
			t.Errorf("VolumeName(%q) message = %q, want %q", n, err.Error(), "Illegal argument name")
		}
	}
}

// TestVolumeValidateSizeMessage — size_bytes<=0 → фикс. "Illegal argument
// size_bytes" (контрактный текст S1-11, важное #1).
func TestVolumeValidateSizeMessage(t *testing.T) {
	v := Volume{ProjectID: "prj-1", ZoneID: "region-1-a", DiskTypeID: "block-balanced", SizeBytes: 0}
	err := v.Validate()
	if err == nil || err.Error() != "Illegal argument size_bytes" {
		t.Fatalf("Validate size=0 = %v, want %q", err, "Illegal argument size_bytes")
	}
}

// TestSnapshotNameValidate — CS1-S3-03: Snapshot.name — тот же self-validating
// lowercase-ASCII инвариант, что VolumeName. uppercase / non-ASCII (кириллица
// "снимок") / >63 / invalid-char → фикс. "Illegal argument name" (не length-only).
func TestSnapshotNameValidate(t *testing.T) {
	okNames := []string{"", "snap-01", "s", "my-snap-9"}
	for _, n := range okNames {
		s := Snapshot{ProjectID: "prj-1", SourceVolumeID: "vol-1", Name: n, Status: SnapshotStatusReady}
		if err := s.Validate(); err != nil {
			t.Errorf("Snapshot name %q rejected: %v", n, err)
		}
	}
	badNames := []string{"Bad_Name", "снимок", "Snap!", "1snap", "-snap",
		"way-too-long-name-that-clearly-exceeds-the-sixty-three-char-limit-x"}
	for _, n := range badNames {
		s := Snapshot{ProjectID: "prj-1", SourceVolumeID: "vol-1", Name: n, Status: SnapshotStatusReady}
		err := s.Validate()
		if err == nil {
			t.Errorf("Snapshot name %q: expected error, got nil", n)
			continue
		}
		if err.Error() != "Illegal argument name" {
			t.Errorf("Snapshot name %q message = %q, want %q", n, err.Error(), "Illegal argument name")
		}
	}
}

// TestDeriveStatus — status derived из state + attach (§1.3): READY+attach→IN_USE,
// READY без attach→AVAILABLE, прочие state 1:1.
func TestDeriveStatus(t *testing.T) {
	cases := []struct {
		state    string
		attached bool
		want     VolumeStatus
	}{
		{"READY", false, VolumeStatusAvailable},
		{"READY", true, VolumeStatusInUse},
		{"CREATING", false, VolumeStatusCreating},
		{"DELETING", false, VolumeStatusDeleting},
		{"ERROR", false, VolumeStatusError},
	}
	for _, c := range cases {
		if got := DeriveStatus(c.state, c.attached); got != c.want {
			t.Errorf("DeriveStatus(%q,%v)=%d, want %d", c.state, c.attached, got, c.want)
		}
	}
}

func TestVolumeValidate(t *testing.T) {
	valid := Volume{ProjectID: "prj-1", ZoneID: "region-1-a", DiskTypeID: "network-ssd", SizeBytes: 1, Status: VolumeStatusAvailable}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid volume rejected: %v", err)
	}
	cases := map[string]Volume{
		"missing project": {ZoneID: "region-1-a", DiskTypeID: "network-ssd", SizeBytes: 1},
		"missing zone":    {ProjectID: "prj-1", DiskTypeID: "network-ssd", SizeBytes: 1},
		"missing type":    {ProjectID: "prj-1", ZoneID: "region-1-a", SizeBytes: 1},
		"zero size":       {ProjectID: "prj-1", ZoneID: "region-1-a", DiskTypeID: "network-ssd", SizeBytes: 0},
		"bad status":      {ProjectID: "prj-1", ZoneID: "region-1-a", DiskTypeID: "network-ssd", SizeBytes: 1, Status: VolumeStatus(99)},
	}
	for name, v := range cases {
		if err := v.Validate(); err == nil {
			t.Errorf("%s: expected validation error, got nil", name)
		}
	}
}

func TestSnapshotValidate(t *testing.T) {
	if err := (Snapshot{ProjectID: "prj-1", SourceVolumeID: "vol-1", Status: SnapshotStatusReady}).Validate(); err != nil {
		t.Fatalf("valid snapshot rejected: %v", err)
	}
	if err := (Snapshot{ProjectID: "prj-1"}).Validate(); err == nil {
		t.Error("snapshot without source_volume_id should be rejected")
	}
}

func TestDiskTypeValidate(t *testing.T) {
	if err := (DiskType{ID: "network-ssd", Name: "SSD"}).Validate(); err != nil {
		t.Fatalf("valid disk_type rejected: %v", err)
	}
	if err := (DiskType{Name: "SSD"}).Validate(); err == nil {
		t.Error("disk_type without id should be rejected")
	}
}
