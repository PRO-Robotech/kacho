// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// clienttruth_vpc_tenancylevel_injection_test.go — ДОКАЗАТЕЛЬСТВО того, что
// анализатор способен упасть, называет координату и молчит на законном близнеце.
//
// Стенд синтетический: настоящее дерево нельзя ни сломать, ни вернуть, а вердикт
// о нём (`clienttruth_vpc_tenancylevel_test.go`) о способности падать не говорит
// ничего — зелёный получает и та проверка, что не смотрит никуда.
//
// Инъекции вносятся ПО ОДНОЙ, и каждая снимает РОВНО ОДНО свойство у элемента,
// чьи остальные свойства целы.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tenancyStand — синтетическое дерево контрактов, где каждое утверждение о
// контейнере имеет след. Это ЗАКОННОЕ состояние, и на нём анализатор обязан
// молчать.
type tenancyStand struct{ root string }

func newTenancyStand(t *testing.T) *tenancyStand {
	t.Helper()
	s := &tenancyStand{root: t.TempDir()}

	s.write(t, "proto/kacho/cloud/probevpc/v1/net.proto", `
syntax = "proto3";
package kacho.cloud.probevpc.v1;

message ProbeNet {
  // ID of the probe_net.
  string id = 1;

  // ID of the project that the net belongs to.
  string project_id = 2;

  // Name of the net.
  // The name is unique within the project.
  string name = 3;
}

message ProbeNic {
  // ID of the NIC to detach.
  string nic_id = 1;
}
`)
	// След ВТОРОГО вида — сообщение без одноимённого поля. Контейнер `ProbeCell`
	// назван, поля `probe_cell_id` в дереве нет, но сообщение есть.
	s.write(t, "proto/kacho/cloud/probevpc/v1/cell.proto", `
syntax = "proto3";
package kacho.cloud.probevpc.v1;

message ProbeCell { string id = 1; }

message ProbeThing {
  // The name is unique within the ProbeCell.
  string name = 1;
}
`)
	// След ТРЕТЬЕГО вида — сегмент имени пакета. «Уникально в пределах домена
	// probeiam» — верное утверждение о вещи, у которой нет и не должно быть id.
	s.write(t, "proto/kacho/cloud/probeiam/v1/account.proto", `
syntax = "proto3";
package kacho.cloud.probeiam.v1;

message ProbeAccount {
  // The name is unique within the probeiam domain.
  string name = 1;
}
`)
	// ПРОЗА об отсутствующем уровне — законное утверждение о том, что уровня НЕТ.
	// Анализатор обязан молчать: краснеть здесь значило бы требовать умолчания о
	// собственной истории.
	s.write(t, "proto/kacho/cloud/probeiam/v1/project.proto", `
syntax = "proto3";
package kacho.cloud.probeiam.v1;

// ProbeProject — заменитель ProbeFolder из прежней модели; уровня ProbeFolder в
// продукте нет, и folder-иерархия не заводится.
message ProbeProject { string id = 1; }
`)
	return s
}

func (s *tenancyStand) write(t *testing.T, rel, body string) {
	t.Helper()
	p := filepath.Join(s.root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", rel, err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func (s *tenancyStand) run(t *testing.T) ([]TenancyLevelFinding, TenancyLevelCensus) {
	t.Helper()
	var log strings.Builder
	f, c, err := AuditTenancyLevels(TenancyLevelOptions{Root: s.root, ProtoRoot: "proto"}, &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))
	return f, c
}

// TestTenancyLevelInjection_CleanStandIsSilent — контроль. Без него всякая
// последующая краснота неотличима от анализатора, краснеющего на всём.
func TestTenancyLevelInjection_CleanStandIsSilent(t *testing.T) {
	s := newTenancyStand(t)
	findings, census := s.run(t)
	if len(findings) != 0 {
		t.Fatalf("на законном дереве находок %d, ожидался ноль: %v", len(findings), findings)
	}
	if census.ProtoFiles != 4 {
		t.Fatalf("файлов контракта %d, ожидалось 4 — стенд прочитан не весь", census.ProtoFiles)
	}
	// Пять утверждений: «project that», «unique within the project», «ID of the
	// NIC to», «unique within the ProbeCell», «unique within the probeiam domain».
	// Все пять со следом — иначе молчание получено даром.
	if census.Claims != 5 || census.Traced != 5 {
		t.Fatalf("утверждений %d (ожидалось 5), со следом %d (ожидалось 5)",
			census.Claims, census.Traced)
	}
}

// TestTenancyLevelInjection_MissingContainerIsFound — ИНЪЕКЦИЯ: у существующего
// утверждения меняется ОДНО свойство — названный контейнер. Остальное цело.
func TestTenancyLevelInjection_MissingContainerIsFound(t *testing.T) {
	s := newTenancyStand(t)
	s.write(t, "proto/kacho/cloud/probevpc/v1/net.proto", `
syntax = "proto3";
package kacho.cloud.probevpc.v1;

message ProbeNet {
  // ID of the probe_net.
  string id = 1;

  // ID of the drawer that the net belongs to.
  string project_id = 2;

  // Name of the net.
  // The name is unique within the drawer.
  string name = 3;
}

message ProbeNic {
  // ID of the NIC to detach.
  string nic_id = 1;
}
`)
	findings, _ := s.run(t)
	if len(findings) != 2 {
		t.Fatalf("находок %d, ожидалось 2: %v", len(findings), findings)
	}
	for _, f := range findings {
		if !strings.Contains(f.File, "net.proto") || f.Line == 0 {
			t.Fatalf("координата не названа: %+v", f)
		}
		if f.Level != "drawer" {
			t.Fatalf("находка не называет предмет: %+v", f)
		}
		if !strings.Contains(f.String(), "drawer_id") {
			t.Fatalf("находка не говорит, ЧТО искалось: %q", f.String())
		}
	}
}

// TestTenancyLevelInjection_RealLevelSilencesTheGate — самоистечение: заведи
// названный уровень ПО-НАСТОЯЩЕМУ, и анализатор замолчит сам. Без этой ветви
// гейт требовал бы вечного умолчания об уровне, который однажды появится.
func TestTenancyLevelInjection_RealLevelSilencesTheGate(t *testing.T) {
	s := newTenancyStand(t)
	s.write(t, "proto/kacho/cloud/probevpc/v1/net.proto", `
syntax = "proto3";
package kacho.cloud.probevpc.v1;

message ProbeNet {
  // ID of the drawer that the net belongs to.
  string drawer_id = 1;

  // The name is unique within the drawer.
  string name = 2;
}
`)
	findings, census := s.run(t)
	if len(findings) != 0 {
		t.Fatalf("находок %d, ожидался ноль — самоистечение не работает: %v", len(findings), findings)
	}
	if census.Traced != census.Claims || census.Claims == 0 {
		t.Fatalf("утверждений %d, со следом %d — след уровня не найден", census.Claims, census.Traced)
	}
}

// TestTenancyLevelInjection_AcronymTraceIsFound — аббревиатура резолвится целиком.
// Наивный разбор «перед каждой заглавной — подчёркивание» дал бы `n_i_c`, след не
// нашёлся бы ни разу, и анализатор краснел бы на ВЕРНЫХ комментариях. Инъекция
// снимает поле `nic_id`, оставив утверждение на месте.
func TestTenancyLevelInjection_AcronymTraceIsFound(t *testing.T) {
	s := newTenancyStand(t)
	s.write(t, "proto/kacho/cloud/probevpc/v1/net.proto", `
syntax = "proto3";
package kacho.cloud.probevpc.v1;

message ProbeNic {
  // ID of the NIC to detach.
  string other_id = 1;
}
`)
	findings, _ := s.run(t)
	if len(findings) != 1 || findings[0].Level != "NIC" {
		t.Fatalf("находок %d, ожидалась 1 про NIC: %v", len(findings), findings)
	}
	if !strings.Contains(findings[0].String(), "nic_id") {
		t.Fatalf("аббревиатура разобрана неверно: %q", findings[0].String())
	}
}

// TestTenancyLevelInjection_DeclarationNotProse — след берётся из ОБЪЯВЛЕНИЯ, а не
// из текста. Поле, упомянутое только комментарием, следом не является: иначе
// анализатор замолчал бы ровно на том входе, ради которого написан.
func TestTenancyLevelInjection_DeclarationNotProse(t *testing.T) {
	s := newTenancyStand(t)
	s.write(t, "proto/kacho/cloud/probevpc/v1/net.proto", `
syntax = "proto3";
package kacho.cloud.probevpc.v1;

message ProbeNet {
  // Когда-нибудь здесь появится string drawer_id = 9; но пока его нет.
  // The name is unique within the drawer.
  string name = 1;
}
`)
	findings, _ := s.run(t)
	if len(findings) != 1 || findings[0].Level != "drawer" {
		t.Fatalf("находок %d, ожидалась 1 — след собран из прозы: %v", len(findings), findings)
	}
}

// TestTenancyLevelInjection_EmptyWalkIsVisible — «ноль находок» обязано быть
// отличимо от «ноль прочитанного».
func TestTenancyLevelInjection_EmptyWalkIsVisible(t *testing.T) {
	s := &tenancyStand{root: t.TempDir()}
	s.write(t, "proto/.keep", "")
	findings, census := s.run(t)
	if len(findings) != 0 {
		t.Fatalf("находок %d на пустом дереве, ожидался ноль", len(findings))
	}
	if census.ProtoFiles != 0 || census.Claims != 0 || census.Fields != 0 {
		t.Fatalf("перепись не показала пустоту: %+v", census)
	}
}
