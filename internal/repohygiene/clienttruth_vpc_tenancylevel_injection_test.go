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
	// СОСТАВНОЕ ИМЯ С УТОЧНЕНИЕМ ВПЕРЕДИ — форма, на которой прежний захват дал
	// ЛОЖНОЕ КРАСНОЕ: он брал первое слово (`PARENT`) и следа ему не находил.
	// След даёт головное существительное — `probe_lb_id`.
	//
	// Рядом, В ТОЙ ЖЕ СТРОКЕ, стоит «NOT within the ProbeProject» — так написан и
	// настоящий текст. Формой оно НЕ является (голое «within the» под перечень не
	// подпадает), и захват первой группы обязан на нём остановиться, а не утянуть
	// его в имя.
	s.write(t, "proto/kacho/cloud/probelb/v1/listener.proto", `
syntax = "proto3";
package kacho.cloud.probelb.v1;

message ProbeListener {
  // ID of the parent ProbeLb.
  string probe_lb_id = 1;

  // Name of the listener. Is unique within the PARENT PROBE LB — NOT within the
  // ProbeProject.
  string name = 2;

  // ID of the ProbeListener membership row to block.
  string probe_membership_row_id = 3;
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
	f, c, err := AuditTenancyLevels(TenancyLevelOptions{
		Tree: clientTruthSyntheticTree(t, s.root), ProtoRoot: "proto",
	}, &log)
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
	if census.ProtoFiles != 5 {
		t.Fatalf("файлов контракта %d, ожидалось 5 — стенд прочитан не весь", census.ProtoFiles)
	}
	// Семь утверждений: «project that», «unique within the project», «ID of the
	// NIC to», «unique within the ProbeCell», «unique within the probeiam domain»,
	// «unique within the PARENT PROBE LB» и «ID of the ProbeListener membership
	// row to». Все семь со следом — иначе молчание получено даром.
	//
	// «NOT within the ProbeProject» из той же строки утверждением НЕ считается, и
	// это правильно: голое «within the» формой не является — под него попало бы
	// любое предложение, где рядом стоят предлог и существительное. Восьмого
	// утверждения здесь нет by construction, и проверка это фиксирует числом.
	if census.Claims != 7 || census.Traced != 7 {
		t.Fatalf("утверждений %d (ожидалось 7), со следом %d (ожидалось 7)",
			census.Claims, census.Traced)
	}
	// Составных имён ровно три: «PARENT PROBE LB», «ProbeListener membership row»
	// и «probeiam domain». Ноль означал бы, что захват именной группы выродился в
	// одно слово, — и тогда молчание выше получено не различением, а тем, что
	// анализатор перестал видеть предмет.
	if census.MultiWord != 3 {
		t.Fatalf("составных имён %d, ожидалось 3 — захват именной группы не работает",
			census.MultiWord)
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

// TestTenancyLevelInjection_QualifiedNameTracesByItsHead — ФОРМА «уточнение +
// головное существительное». Инъекция снимает РОВНО ОДНО свойство: поле
// `probe_lb_id`, дающее след головному существительному. Само утверждение и его
// форма целы.
//
// Это тот самый вход, на котором прежний захват дал ложное красное: он брал
// первое слово (`PARENT`) и следа ему не находил. Пара «поле есть — молчит /
// поля нет — находка» доказывает, что судится ПРЕДМЕТ, а не длина фразы.
func TestTenancyLevelInjection_QualifiedNameTracesByItsHead(t *testing.T) {
	s := newTenancyStand(t)
	s.write(t, "proto/kacho/cloud/probelb/v1/listener.proto", `
syntax = "proto3";
package kacho.cloud.probelb.v1;

message ProbeListener {
  // Поля probe_lb_id больше нет — следа головному существительному взяться
  // неоткуда.
  string other_id = 1;

  // Name of the listener. Is unique within the PARENT PROBE LB — NOT within the
  // ProbeProject.
  string name = 2;
}
`)
	findings, census := s.run(t)
	if len(findings) != 1 {
		t.Fatalf("находок %d, ожидалась 1: %v", len(findings), findings)
	}
	f := findings[0]
	if f.Level != "PARENT PROBE LB" {
		t.Fatalf("имя контейнера захвачено не целиком: %q", f.Level)
	}
	// Находка обязана назвать написания, которыми искался след, — иначе читатель
	// не поймёт, ЧТО не нашлось, и пойдёт править не то.
	if !strings.Contains(f.String(), "probe_lb_id") {
		t.Fatalf("находка не называет искомое головное написание: %q", f.String())
	}
	if census.MultiWord == 0 {
		t.Fatalf("составных имён 0 — захват именной группы выродился в одно слово")
	}
}

// TestTenancyLevelInjection_ThreeWordNameIsSeenAtAll — ФОРМА из трёх слов.
// Прежний захват не видел её ВОВСЕ: ни красного, ни зелёного — невидимость.
// Инъекция снимает след (`probe_membership_row_id`), оставив всё прочее целым;
// утверждение обязано и распознаться, и покраснеть.
func TestTenancyLevelInjection_ThreeWordNameIsSeenAtAll(t *testing.T) {
	s := newTenancyStand(t)
	s.write(t, "proto/kacho/cloud/probelb/v1/listener.proto", `
syntax = "proto3";
package kacho.cloud.probelb.v1;

message ProbeListener {
  // ID of the ProbeGhost membership row to block.
  string other_id = 1;
}
`)
	findings, census := s.run(t)
	if len(findings) != 1 || findings[0].Level != "ProbeGhost membership row" {
		t.Fatalf("трёхсловное имя не распознано либо не найдено: %v", findings)
	}
	// Пять утверждений базового стенда плюс это, шестое. Без трёхсловной формы
	// оно не распозналось бы вовсе, и перепись показала бы пять — то есть
	// «находок ноль» было бы получено невидимостью, а не различением.
	if census.Claims != 6 {
		t.Fatalf("утверждений %d, ожидалось 6 — трёхсловная форма не попала в перепись",
			census.Claims)
	}
}

// TestTenancyLevelInjection_StopWordKeepsTheGateHonest — РЕВЕРСНЫЙ КОНТРОЛЬ, и
// самый важный: расширение захвата обязано не ослабить обнаружение.
//
// «in the specified drawer and net» — контейнера `drawer` не существует, а слово
// за союзом (`net`) существует. Без обрезки по не-существительному кандидат `net`
// нашёл бы след, и находка ИСЧЕЗЛА БЫ — то есть расширение захвата сузило бы
// наблюдение молча. Здесь утверждается обратное: находка на месте, и её имя —
// `drawer`, а не `drawer and net`.
func TestTenancyLevelInjection_StopWordKeepsTheGateHonest(t *testing.T) {
	s := newTenancyStand(t)
	s.write(t, "proto/kacho/cloud/probevpc/v1/net.proto", `
syntax = "proto3";
package kacho.cloud.probevpc.v1;

message ProbeNet {
  // Creates a thing in the specified drawer and net.
  string project_id = 1;

  // ID of the probe_net.
  string id = 2;
}
`)
	findings, _ := s.run(t)
	if len(findings) != 1 {
		t.Fatalf("находок %d, ожидалась 1 — слово за союзом помиловало находку: %v",
			len(findings), findings)
	}
	if findings[0].Level != "drawer" {
		t.Fatalf("имя контейнера %q — захват ушёл за пределы именной группы",
			findings[0].Level)
	}
}

// TestTenancyLevelInjection_InventedContainerStillFound — РЕВЕРСНЫЙ КОНТРОЛЬ к
// расширению: выдуманный контейнер обязан находиться по-прежнему, в том числе
// составной. Без этой ветви «ноль находок» после расширения было бы неотличимо
// от анализатора, который перестал судить.
func TestTenancyLevelInjection_InventedContainerStillFound(t *testing.T) {
	s := newTenancyStand(t)
	s.write(t, "proto/kacho/cloud/probevpc/v1/net.proto", `
syntax = "proto3";
package kacho.cloud.probevpc.v1;

message ProbeNet {
  // The name is unique within the FOLDER.
  string name = 1;

  // The name is unique within the PARENT FOLDER.
  string other = 2;
}
`)
	findings, _ := s.run(t)
	if len(findings) != 2 {
		t.Fatalf("находок %d, ожидалось 2 (одно- и двусловный выдуманный контейнер): %v",
			len(findings), findings)
	}
	got := []string{findings[0].Level, findings[1].Level}
	if got[0] != "FOLDER" || got[1] != "PARENT FOLDER" {
		t.Fatalf("имена контейнеров %v — захват именной группы неверен", got)
	}
}
