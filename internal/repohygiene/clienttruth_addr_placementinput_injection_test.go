// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// clienttruth_addr_placementinput_injection_test.go — ДОКАЗАТЕЛЬСТВО того, что
// анализатор способен упасть, называет координату и молчит на законных близнецах.
//
// Инъекции вносятся ПО ОДНОЙ, и каждая снимает РОВНО ОДНО свойство у сообщения,
// чьи остальные свойства целы (`testing.md` §«Гейт на класс», п.2в).
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// placementStand — синтетическое дерево, где каждое объявление законно.
type placementStand struct{ root string }

func newPlacementStand(t *testing.T) *placementStand {
	t.Helper()
	s := &placementStand{root: t.TempDir()}

	// Канон вида «выводится из координаты».
	s.write(t, "proto/kacho/cloud/probevpc/v1/subnet_service.proto",
		"syntax = \"proto3\";\n"+
			"message CreateProbeSubnetRequest {\n"+
			"  string project_id = 1;\n"+
			"  // placement_type — server-derived: задайте zone_id либо region_id.\n"+
			"  ProbePlacementType placement_type = 13;\n"+
			"  // ID of the zone (set iff placement_type == ZONAL).\n"+
			"  string zone_id = 6;\n"+
			"}\n")

	// Канон вида «выводится из отдельного входа».
	s.write(t, "proto/kacho/cloud/probelb/v1/lb_service.proto",
		"syntax = \"proto3\";\n"+
			"message CreateProbeBalancerRequest {\n"+
			"  // placement_type — DERIVED output-only; режим задаёт `placement`.\n"+
			"  ProbePlacementType placement_type = 17;\n"+
			"}\n")

	// ЗАКОННЫЙ БЛИЗНЕЦ №1: запрос создания БЕЗ дискриминатора. Молчание о поле
	// нарушением не является — у ресурса размещение постоянное.
	s.write(t, "proto/kacho/cloud/proberegistry/v1/registry_service.proto",
		"syntax = \"proto3\";\n"+
			"message CreateProbeRegistryRequest {\n"+
			"  // region_id — якорь; placement_type всегда REGIONAL и входом не является.\n"+
			"  string region_id = 5;\n"+
			"}\n")

	// ЗАКОННЫЙ БЛИЗНЕЦ №2: поле стоит в сообщении САМОГО РЕСУРСА, а не в запросе
	// создания. На чтении дискриминатор законен и обязателен.
	s.write(t, "proto/kacho/cloud/probevpc/v1/subnet.proto",
		"syntax = \"proto3\";\n"+
			"message ProbeSubnet {\n"+
			"  ProbePlacementType placement_type = 15;\n"+
			"  string zone_id = 6;\n"+
			"}\n")

	return s
}

func (s *placementStand) write(t *testing.T, rel, body string) {
	t.Helper()
	p := filepath.Join(s.root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", rel, err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func (s *placementStand) run(
	t *testing.T, ex ...PlacementInputExemption,
) ([]PlacementInputFinding, PlacementInputCensus) {
	t.Helper()
	var log strings.Builder
	f, c, err := AuditPlacementInput(PlacementInputOptions{
		Tree: clientTruthSyntheticTree(t, s.root), ProtoRoot: "proto", Exemptions: ex,
	}, &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))
	return f, c
}

// TestPlacementInputInjection_CleanStandIsSilent — КОНТРОЛЬ.
func TestPlacementInputInjection_CleanStandIsSilent(t *testing.T) {
	s := newPlacementStand(t)
	findings, census := s.run(t)
	if len(findings) != 0 {
		t.Fatalf("на законном дереве находок %d, ожидался ноль: %v", len(findings), findings)
	}
	if census.ProtoFiles != 4 {
		t.Fatalf("файлов контракта %d, ожидалось 4 — стенд прочитан не весь", census.ProtoFiles)
	}
	// Три запроса создания; поле несут два. Сообщение РЕСУРСА в счёт запросов не
	// попало — это молчание на втором близнеце.
	if census.CreateMessages != 3 || census.WithField != 2 || census.Derived != 2 {
		t.Fatalf("запросов создания %d (ожидалось 3), с полем %d (ожидалось 2), выводимых %d (ожидалось 2)",
			census.CreateMessages, census.WithField, census.Derived)
	}
}

// TestPlacementInputInjection_RequiredInputIsFound — снято ОДНО свойство: поле
// объявлено обязательным входом вместо выводимого.
func TestPlacementInputInjection_RequiredInputIsFound(t *testing.T) {
	s := newPlacementStand(t)
	s.write(t, "proto/kacho/cloud/probecompute/v1/group_service.proto",
		"syntax = \"proto3\";\n"+
			"message CreateProbeGroupRequest {\n"+
			"  // Якорь размещения. Обязателен и взаимоисключающ: ровно одна координата.\n"+
			"  ProbePlacementType placement_type = 6;\n"+
			"  string zone_id = 7;\n"+
			"}\n")
	findings, census := s.run(t)
	if len(findings) != 1 {
		t.Fatalf("находок %d, ожидалась одна: %v", len(findings), findings)
	}
	f := findings[0]
	if f.File != "proto/kacho/cloud/probecompute/v1/group_service.proto" || f.Line != 4 {
		t.Fatalf("координата %s:%d — не та, что у внесённого объявления", f.File, f.Line)
	}
	if f.Message != "CreateProbeGroupRequest" {
		t.Fatalf("сообщение названо как %q — не то", f.Message)
	}
	// Соседи не задеты.
	if census.Derived != 2 {
		t.Fatalf("выводимых %d, ожидалось 2 — задеты соседние объявления", census.Derived)
	}
}

// TestPlacementInputInjection_CommentOfANeighbourDoesNotCount — УПОМИНАНИЕ поля в
// комментарии СОСЕДНЕГО объявления полем не является.
//
// Проба существует потому, что форма «set iff placement_type == ZONAL» стоит в
// дереве рядом с каждой координатой: распознаватель по подстроке нашёл бы здесь
// поле и вынес вердикт о том, чего нет.
func TestPlacementInputInjection_CommentOfANeighbourDoesNotCount(t *testing.T) {
	s := newPlacementStand(t)
	s.write(t, "proto/kacho/cloud/probeother/v1/thing_service.proto",
		"syntax = \"proto3\";\n"+
			"message CreateProbeThingRequest {\n"+
			"  // ID зоны (set iff placement_type == ZONAL). Дискриминатора здесь нет.\n"+
			"  string zone_id = 1;\n"+
			"}\n")
	findings, census := s.run(t)
	if len(findings) != 0 {
		t.Fatalf("упоминание в комментарии соседа принято за объявление: %v", findings)
	}
	if census.WithField != 2 {
		t.Fatalf("с полем %d, ожидалось 2 — распознаватель читает текст, а не объявление",
			census.WithField)
	}
}

// TestPlacementInputInjection_DerivedClaimOfANeighbourDoesNotCarry — комментарий
// «выводимо», относящийся к ПРЕДЫДУЩЕМУ полю, не оправдывает следующее.
//
// Без обнуления накопленного комментария объявление, стоящее сразу за законным
// соседом, наследовало бы его оправдание — и требование поля прошло бы молча.
func TestPlacementInputInjection_DerivedClaimOfANeighbourDoesNotCarry(t *testing.T) {
	s := newPlacementStand(t)
	s.write(t, "proto/kacho/cloud/probeother/v1/thing_service.proto",
		"syntax = \"proto3\";\n"+
			"message CreateProbeThingRequest {\n"+
			"  // region_id — server-derived зеркало, output-only.\n"+
			"  string region_id = 1;\n"+
			"  ProbePlacementType placement_type = 2;\n"+
			"}\n")
	findings, _ := s.run(t)
	if len(findings) != 1 {
		t.Fatalf("оправдание соседнего поля перенеслось на дискриминатор: %v", findings)
	}
}

// TestPlacementInputInjection_CanonQuotedBelowTheOpeningDoesNotCount — снято
// ОДНО свойство: объявление говорит «обязателен», а канон лишь ПРОЦИТИРОВАН ниже.
//
// Проба заведена НАСТОЯЩИМ дефектом, а не выдуманным. Написав честный
// комментарий, который объясняет отступление и цитирует при этом текст
// канонического отказа, я удовлетворил бы предикат по всему блоку — то есть
// ссылка на канон засчиталась бы за его соблюдение, и расхождение стало бы
// невидимым ровно у того ресурса, ради которого гейт написан. Поймало это
// самоистечение послабления, а не чтение.
func TestPlacementInputInjection_CanonQuotedBelowTheOpeningDoesNotCount(t *testing.T) {
	s := newPlacementStand(t)
	s.write(t, "proto/kacho/cloud/probecompute/v1/group_service.proto",
		"syntax = \"proto3\";\n"+
			"message CreateProbeGroupRequest {\n"+
			"  // Якорь размещения. Обязателен и взаимоисключающ.\n"+
			"  //\n"+
			"  // Это отступление от канона: у подсети поле server-derived и\n"+
			"  // отвергается («placement_type is server-derived; set zone_id…»),\n"+
			"  // у балансировщика — DERIVED output-only.\n"+
			"  ProbePlacementType placement_type = 6;\n"+
			"}\n")
	findings, census := s.run(t)
	if len(findings) != 1 {
		t.Fatalf("цитата канона ниже объявления засчиталась за его соблюдение: %v", findings)
	}
	if findings[0].Message != "CreateProbeGroupRequest" {
		t.Fatalf("сообщение названо как %q — не то", findings[0].Message)
	}
	// Соседи, объявляющие выводимость ПЕРВОЙ строкой, по-прежнему молчат: сужение
	// до открывающей строки не сломало положительную сторону.
	if census.Derived != 2 {
		t.Fatalf("выводимых %d, ожидалось 2 — сужение задело законные объявления",
			census.Derived)
	}
}

// TestPlacementInputInjection_LiveExemptionSuppresses — послабление с ЖИВЫМ
// предметом снимает находку и считается переписью.
func TestPlacementInputInjection_LiveExemptionSuppresses(t *testing.T) {
	s := newPlacementStand(t)
	s.write(t, "proto/kacho/cloud/probecompute/v1/group_service.proto",
		"syntax = \"proto3\";\n"+
			"message CreateProbeGroupRequest {\n"+
			"  // Якорь размещения. Обязателен.\n"+
			"  ProbePlacementType placement_type = 6;\n"+
			"}\n")
	findings, census := s.run(t, PlacementInputExemption{
		File:    "proto/kacho/cloud/probecompute/v1/group_service.proto",
		Message: "CreateProbeGroupRequest",
		Reason:  "снятие ломающее, переводится своим изменением",
	})
	if len(findings) != 0 {
		t.Fatalf("живое послабление не сняло находку: %v", findings)
	}
	if census.Exempted != 1 {
		t.Fatalf("снято послаблением %d, ожидалось 1", census.Exempted)
	}
}

// TestPlacementInputInjection_StaleExemptionIsAFinding — послабление, которому
// нечего исключать, обязано быть находкой.
func TestPlacementInputInjection_StaleExemptionIsAFinding(t *testing.T) {
	s := newPlacementStand(t)
	findings, _ := s.run(t, PlacementInputExemption{
		File:    "proto/kacho/cloud/probecompute/v1/group_service.proto",
		Message: "CreateProbeGroupRequest",
		Reason:  "требование давно снято",
	})
	if len(findings) != 1 || !findings[0].StaleExemption {
		t.Fatalf("устаревшее послабление не стало находкой: %v", findings)
	}
	if !strings.Contains(findings[0].String(), "требование давно снято") {
		t.Fatalf("находка не называет причину послабления: %s", findings[0].String())
	}
}

// TestPlacementInputInjection_EmptyWalkIsNotSilentSuccess — «ноль находок»
// обязано быть отличимо от «ноль прочитанного».
func TestPlacementInputInjection_EmptyWalkIsNotSilentSuccess(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "proto"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	var log strings.Builder
	findings, census, err := AuditPlacementInput(
		PlacementInputOptions{Tree: clientTruthSyntheticTree(t, root), ProtoRoot: "proto"}, &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	if len(findings) != 0 || census.ProtoFiles != 0 || census.WithField != 0 {
		t.Fatalf("перепись непуста на пустом обходе: %+v", census)
	}
}
