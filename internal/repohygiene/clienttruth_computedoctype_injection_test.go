// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// clienttruth_computedoctype_injection_test.go — ДОКАЗАТЕЛЬСТВО того, что
// анализатор способен упасть, называет координату и молчит на законном близнеце.
//
// Стенд синтетический: настоящее дерево нельзя ни сломать, ни вернуть, а вердикт
// о нём (`clienttruth_computedoctype_test.go`) о способности падать не говорит
// ничего — зелёный получает и та проверка, что не смотрит никуда.
//
// Инъекции вносятся ПО ОДНОЙ, каждая — на своей странице. К каждой приложен
// законный близнец той же формы, обязанный молчать.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// anyTypeStand — синтетическое дерево: контракты двух доменов и клиентская
// страница, называющая типы верно. Это ЗАКОННОЕ состояние, и на нём анализатор
// обязан молчать.
type anyTypeStand struct{ root string }

func newAnyTypeStand(t *testing.T) *anyTypeStand {
	t.Helper()
	s := &anyTypeStand{root: t.TempDir()}

	s.write(t, "proto/kacho/cloud/probestorage/v1/volume_service.proto", `
syntax = "proto3";
package kacho.cloud.probestorage.v1;

/* Блочный комментарий, внутри которого стоит
   message ProbeGhostFromBlockComment {
   — и это НЕ объявление. Анализатор, считающий сырой текст, положил бы его
   в словарь и промолчал бы на документе, называющем несуществующий тип. */

service ProbeVolumeService {
  // Путь-шаблон в строковом литерале несёт фигурные скобки:
  // "/probestorage/v1/volumes/{volume_id}". Если их посчитать за код,
  // глубина вложенности поедет и вложенные сообщения потеряются.
  rpc Get(GetProbeVolumeRequest) returns (ProbeVolume) {
    option (google.api.http) = {get: "/probestorage/v1/volumes/{volume_id}"};
  }
}

message ProbeCreateVolumeMetadata {
  string volume_id = 1;
}

message ProbeVolume {
  message ProbeNestedAttachment {
    string instance_id = 1;
  }
  ProbeNestedAttachment attachment = 1;
}

// message ProbeGhostFromLineComment { — тоже НЕ объявление.
`)
	s.write(t, "proto/kacho/cloud/probecompute/v1/instance_service.proto", `
syntax = "proto3";
package kacho.cloud.probecompute.v1;

message ProbeCreateInstanceMetadata {
  string instance_id = 1;
}
`)

	// Законное состояние: верное полное имя, вложенное имя и общеизвестный тип.
	s.write(t, "services/probecompute/docs/content/getting-started.mdx", `
        "@type": "type.googleapis.com/kacho.cloud.probecompute.v1.ProbeCreateInstanceMetadata",
        "@type": "type.googleapis.com/kacho.cloud.probestorage.v1.ProbeCreateVolumeMetadata",
        "@type": "type.googleapis.com/kacho.cloud.probestorage.v1.ProbeVolume.ProbeNestedAttachment",
        "@type": "type.googleapis.com/google.protobuf.Empty"
`)
	return s
}

func (s *anyTypeStand) write(t *testing.T, rel, body string) {
	t.Helper()
	p := filepath.Join(s.root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", rel, err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func (s *anyTypeStand) audit(t *testing.T) ([]ClientDocsAnyTypeFinding, ClientDocsAnyTypeCensus) {
	t.Helper()
	var log strings.Builder
	f, c, err := AuditClientDocsAnyType(
		ClientDocsAnyTypeOptions{Root: s.root, ProtoRoot: "proto"}, &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))
	return f, c
}

// Законный близнец: верные полные имена — анализатор молчит, но ЧИТАЕТ их.
// Молчание без переписи было бы неотличимо от пустого обхода.
func TestAnyType_LawfulNamesAreSilentAndCounted(t *testing.T) {
	s := newAnyTypeStand(t)
	f, c := s.audit(t)
	if len(f) != 0 {
		t.Fatalf("анализатор покраснел на законном дереве: %v", f)
	}
	if c.Judged != 3 {
		t.Fatalf("рассужено %d вместо трёх — молчание получено пустым обходом", c.Judged)
	}
	if c.OutsideCount != 1 || len(c.OutsideNames) != 1 {
		t.Fatalf("общеизвестный тип не отнесён к границе анализатора: вне словаря %d %v",
			c.OutsideCount, c.OutsideNames)
	}
	if c.NestedTypes != 1 {
		t.Fatalf("вложенных типов в словаре %d — разбор не читает вложенность, "+
			"и вложенное имя объявлялось бы несуществующим", c.NestedTypes)
	}
}

// Ось 1: типа нет вовсе — находка с координатой и без ложного «живёт в другом
// пакете» (у выдуманного имени соседа не бывает).
func TestAnyType_TypeThatExistsNowhereIsAFinding(t *testing.T) {
	s := newAnyTypeStand(t)
	s.write(t, "services/probecompute/docs/content/api/operations.mdx", `
        "@type": "type.googleapis.com/kacho.cloud.probecompute.v1.ProbeCreateDiskMetadata"
`)
	f, _ := s.audit(t)
	if len(f) != 1 {
		t.Fatalf("находок %d вместо одной: %v", len(f), f)
	}
	if f[0].Line != 2 || !strings.Contains(f[0].File, "operations.mdx") {
		t.Fatalf("координата не названа: %s", f[0])
	}
	if len(f[0].Elsewhere) != 0 {
		t.Fatalf("у выдуманного имени назван сосед, которого нет: %v", f[0].Elsewhere)
	}
	if !strings.Contains(f[0].String(), "ProbeCreateDiskMetadata") {
		t.Fatalf("находка не называет тип: %s", f[0])
	}
}

// Ось 2: сообщение существует, но в ДРУГОМ пакете. Это отдельная находка, и она
// обязана называть, где тип живёт: починка тут другая — сменить пакет, а не
// завести сообщение.
func TestAnyType_RightNameInTheWrongPackageIsAFinding(t *testing.T) {
	s := newAnyTypeStand(t)
	s.write(t, "services/probecompute/docs/content/api/operations.mdx", `
        "@type": "type.googleapis.com/kacho.cloud.probecompute.v1.ProbeCreateVolumeMetadata"
`)
	f, _ := s.audit(t)
	if len(f) != 1 {
		t.Fatalf("находок %d вместо одной: %v", len(f), f)
	}
	if len(f[0].Elsewhere) != 1 ||
		f[0].Elsewhere[0] != "kacho.cloud.probestorage.v1.ProbeCreateVolumeMetadata" {
		t.Fatalf("находка не называет настоящего владельца типа: %v", f[0].Elsewhere)
	}
}

// Ось 3: имя, стоящее в контракте только ПРОЗОЙ (в комментарии — строчном или
// блочном), в словарь не попадает. Иначе анализатор молчал бы на документе,
// называющем тип, которого нет, — доказывая предпосылку её же пересказом.
func TestAnyType_MessageNamedOnlyInACommentIsNotInTheDictionary(t *testing.T) {
	for _, ghost := range []string{"ProbeGhostFromBlockComment", "ProbeGhostFromLineComment"} {
		t.Run(ghost, func(t *testing.T) {
			s := newAnyTypeStand(t)
			s.write(t, "services/probecompute/docs/content/api/operations.mdx",
				"\n\"@type\": \"type.googleapis.com/kacho.cloud.probestorage.v1."+ghost+"\"\n")
			f, _ := s.audit(t)
			if len(f) != 1 {
				t.Fatalf("имя из комментария принято за объявление: находок %d", len(f))
			}
		})
	}
}

// Ось 4: пустой обход не выдаётся за «находок ноль». Премиса вердикта о дереве
// (`clienttruth_computedoctype_test.go`) читает именно эти величины, поэтому
// нули обязаны БЫТЬ нулями, а не растворяться в зелёном.
func TestAnyType_EmptyWalkIsVisibleInTheCensus(t *testing.T) {
	s := &anyTypeStand{root: t.TempDir()}
	s.write(t, "proto/keep.txt", "контрактов нет\n")
	f, c := s.audit(t)
	if len(f) != 0 {
		t.Fatalf("находки на пустом дереве: %v", f)
	}
	if c.ProtoFiles != 0 || c.ContractTypes != 0 || c.DocFiles != 0 || c.Judged != 0 {
		t.Fatalf("перепись не показывает пустоту обхода: контрактов %d, типов %d, страниц %d, рассужено %d",
			c.ProtoFiles, c.ContractTypes, c.DocFiles, c.Judged)
	}
}
