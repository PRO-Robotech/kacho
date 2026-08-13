// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package domain_test

import (
	"testing"

	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
)

// TestSnapshotCopyKeepsProvenance — копия снимка обязана проходить проверку домена.
//
// Домен требует у снимка ровно один источник — том. У копии источник иной по
// смыслу (снимок), и без наследования происхождения она отвергалась как «снимок
// без источника»: глагол копирования не работал НИ РАЗУ с момента заведения, и
// это не было видно, потому что сквозная проба на него появилась позже кода.
//
// Проба закрепляет ПРАВИЛО, а не реализацию: копия несёт то же происхождение,
// что и оригинал, поэтому проверка домена её принимает.
func TestSnapshotCopyKeepsProvenance(t *testing.T) {
	t.Parallel()
	src := domain.Snapshot{
		ID: "snp00000000000000001", ProjectID: "prj-1", Name: "src",
		SourceVolumeID: "vol00000000000000001", Status: domain.SnapshotStatusReady,
	}
	if err := src.Validate(); err != nil {
		t.Fatalf("положительный контроль: исходный снимок обязан быть валиден: %v", err)
	}

	// Копия — как её строит use-case: своё имя и id, происхождение от источника.
	cp := domain.Snapshot{
		ID: "snp00000000000000002", ProjectID: src.ProjectID, Name: "copy",
		SourceVolumeID: src.SourceVolumeID, Status: domain.SnapshotStatusCreating,
	}
	if err := cp.Validate(); err != nil {
		t.Fatalf("копия обязана проходить проверку домена: %v", err)
	}

	// Зеркало: снимок БЕЗ происхождения по-прежнему отвергается — иначе проба
	// выше означала бы «домен принимает что угодно».
	orphan := cp
	orphan.SourceVolumeID = ""
	if err := orphan.Validate(); err == nil {
		t.Fatal("снимок без источника обязан отвергаться: иначе правило не проверено")
	}
}
