// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// clienttruth_addr_verbcanon_injection_test.go — ДОКАЗАТЕЛЬСТВО того, что
// анализатор способен упасть, называет координату и верную запись, и молчит на
// законном близнеце.
//
// Инъекции вносятся ПО ОДНОЙ, и каждая снимает РОВНО ОДНО свойство у адреса, чьи
// остальные свойства целы (`testing.md` §«Гейт на класс», п.2в).
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// verbStand — синтетическое дерево, где каждый адрес записан каноном. Это
// ЗАКОННОЕ состояние, и на нём анализатор обязан молчать.
type verbStand struct{ root string }

func newVerbStand(t *testing.T) *verbStand {
	t.Helper()
	s := &verbStand{root: t.TempDir()}

	// Основная привязка однословным глаголом и многословным — обе каноничны.
	s.write(t, "proto/kacho/cloud/probe/v1/thing_service.proto",
		"syntax = \"proto3\";\n"+
			"service ThingService {\n"+
			"  rpc Stop (StopRequest) returns (Op) {\n"+
			"    option (google.api.http) = {post: \"/probe/v1/things/{thing_id}:stop\"};\n"+
			"  }\n"+
			"  rpc AddBlocks (AddBlocksRequest) returns (Op) {\n"+
			"    option (google.api.http) = {\n"+
			"      post: \"/probe/v1/things/{thing_id}:addBlocks\"\n"+
			"      body: \"*\"\n"+
			"    };\n"+
			"  }\n"+
			"}\n")

	// ЗАКОННЫЙ БЛИЗНЕЦ №1: адрес БЕЗ суффикс-действия. Судить его не о чем.
	s.write(t, "proto/kacho/cloud/probe/v1/plain_service.proto",
		"syntax = \"proto3\";\n"+
			"service PlainService {\n"+
			"  rpc Get (GetRequest) returns (Thing) {\n"+
			"    option (google.api.http) = {get: \"/probe/v1/things/{thing_id}\"};\n"+
			"  }\n"+
			"}\n")

	// ЗАКОННЫЙ БЛИЗНЕЦ №2: ДЕФИС В СЕГМЕНТЕ РЕСУРСА, а не в суффиксе. Анализатор
	// судит только суффикс; покраснеть здесь значило бы судить чужой предмет.
	s.write(t, "proto/kacho/cloud/probe/v1/dashed_service.proto",
		"syntax = \"proto3\";\n"+
			"service DashedService {\n"+
			"  rpc Halt (HaltRequest) returns (Op) {\n"+
			"    option (google.api.http) = {post: \"/probe/v1/cidr-groups/{id}:halt\"};\n"+
			"  }\n"+
			"}\n")

	return s
}

func (s *verbStand) write(t *testing.T, rel, body string) {
	t.Helper()
	p := filepath.Join(s.root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", rel, err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func (s *verbStand) run(
	t *testing.T, ex ...VerbCanonExemption,
) ([]VerbCanonFinding, VerbCanonCensus) {
	t.Helper()
	var log strings.Builder
	f, c, err := AuditVerbCanon(VerbCanonOptions{
		Tree: clientTruthSyntheticTree(t, s.root), ProtoRoot: "proto", Exemptions: ex,
	}, &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))
	return f, c
}

// TestVerbCanonInjection_CleanStandIsSilent — КОНТРОЛЬ.
func TestVerbCanonInjection_CleanStandIsSilent(t *testing.T) {
	s := newVerbStand(t)
	findings, census := s.run(t)
	if len(findings) != 0 {
		t.Fatalf("на законном дереве находок %d, ожидался ноль: %v", len(findings), findings)
	}
	if census.ProtoFiles != 3 {
		t.Fatalf("файлов контракта %d, ожидалось 3 — стенд прочитан не весь", census.ProtoFiles)
	}
	// Четыре адреса, из них три с суффикс-действием: адрес без действия в счёт
	// действий не попал — это и есть молчание на первом близнеце.
	if census.Paths != 3 || census.WithVerb != 3 || census.Canonical != 3 {
		t.Fatalf("адресов %d (ожидалось 3), с действием %d (ожидалось 3), каноном %d (ожидалось 3)",
			census.Paths, census.WithVerb, census.Canonical)
	}
}

// TestVerbCanonInjection_DashedVerbIsFound — снято ОДНО свойство: суффикс записан
// дефисом. Остальное у адреса цело.
func TestVerbCanonInjection_DashedVerbIsFound(t *testing.T) {
	s := newVerbStand(t)
	s.write(t, "proto/kacho/cloud/probe/v1/thing_service.proto",
		"syntax = \"proto3\";\n"+
			"service ThingService {\n"+
			"  rpc AddBlocks (AddBlocksRequest) returns (Op) {\n"+
			"    option (google.api.http) = {post: \"/probe/v1/things/{thing_id}:add-cidr-blocks\"};\n"+
			"  }\n"+
			"}\n")
	findings, census := s.run(t)
	if len(findings) != 1 {
		t.Fatalf("находок %d, ожидалась одна: %v", len(findings), findings)
	}
	f := findings[0]
	if f.File != "proto/kacho/cloud/probe/v1/thing_service.proto" || f.Line != 4 {
		t.Fatalf("координата %s:%d — не та, что у внесённого адреса", f.File, f.Line)
	}
	// Диагностика обязана называть ВЕРНУЮ запись: находка «не канон» заставляет
	// читателя выдумывать её самому.
	if f.Canonical != "addCidrBlocks" {
		t.Fatalf("верная запись названа как %q, ожидалось %q", f.Canonical, "addCidrBlocks")
	}
	if !strings.Contains(f.String(), "addCidrBlocks") {
		t.Fatalf("находка не называет верную запись: %s", f.String())
	}
	// Соседний файл не задет: каноничный `:halt` из dashed_service по-прежнему
	// учтён. Инъекция ЗАМЕЩАЕТ файл стенда, поэтому два его прежних каноничных
	// адреса в счёт не входят — краснеть обязан ровно внесённый.
	if census.Canonical != 1 || census.WithVerb != 2 {
		t.Fatalf("каноничных %d (ожидался 1), с действием %d (ожидалось 2) — задеты соседние адреса",
			census.Canonical, census.WithVerb)
	}
}

// TestVerbCanonInjection_UnderscoreVerbIsFound — вторая незаконная запись того же
// предмета. Без неё анализатор мог бы ловить ОДИН разделитель, а не форму.
func TestVerbCanonInjection_UnderscoreVerbIsFound(t *testing.T) {
	s := newVerbStand(t)
	s.write(t, "proto/kacho/cloud/probe/v1/snake_service.proto",
		"syntax = \"proto3\";\n"+
			"service SnakeService {\n"+
			"  rpc AddBlocks (AddBlocksRequest) returns (Op) {\n"+
			"    option (google.api.http) = {post: \"/probe/v1/things/{id}:add_blocks\"};\n"+
			"  }\n"+
			"}\n")
	findings, _ := s.run(t)
	if len(findings) != 1 || findings[0].Canonical != "addBlocks" {
		t.Fatalf("подчёркивание в суффиксе не поймано либо верная запись не названа: %v", findings)
	}
}

// TestVerbCanonInjection_UppercaseFirstIsFound — заглавная первым знаком:
// `:AddBlocks` каноном не является, и это третий вид отступления.
func TestVerbCanonInjection_UppercaseFirstIsFound(t *testing.T) {
	s := newVerbStand(t)
	s.write(t, "proto/kacho/cloud/probe/v1/upper_service.proto",
		"syntax = \"proto3\";\n"+
			"service UpperService {\n"+
			"  rpc AddBlocks (AddBlocksRequest) returns (Op) {\n"+
			"    option (google.api.http) = {post: \"/probe/v1/things/{id}:AddBlocks\"};\n"+
			"  }\n"+
			"}\n")
	findings, _ := s.run(t)
	if len(findings) != 1 {
		t.Fatalf("заглавная первым знаком не поймана: %v", findings)
	}
}

// TestVerbCanonInjection_AdditionalBindingIsJudged — суффикс ДОПОЛНИТЕЛЬНОЙ
// привязки судится наравне с основной.
//
// Проба существует потому, что переводом дефисных путей и будет заведена первая
// в дереве `additional_bindings`: распознаватель, читающий только основную
// привязку, оставил бы прежнюю запись вне наблюдения ровно в тот день, когда она
// станет предметом (`testing.md` §«Гейт на класс», п.7).
func TestVerbCanonInjection_AdditionalBindingIsJudged(t *testing.T) {
	s := newVerbStand(t)
	s.write(t, "proto/kacho/cloud/probe/v1/thing_service.proto",
		"syntax = \"proto3\";\n"+
			"service ThingService {\n"+
			"  rpc AddBlocks (AddBlocksRequest) returns (Op) {\n"+
			"    option (google.api.http) = {\n"+
			"      post: \"/probe/v1/things/{thing_id}:addBlocks\"\n"+
			"      body: \"*\"\n"+
			"      additional_bindings {\n"+
			"        post: \"/probe/v1/things/{thing_id}:add-blocks\"\n"+
			"        body: \"*\"\n"+
			"      }\n"+
			"    };\n"+
			"  }\n"+
			"}\n")
	findings, census := s.run(t)
	if len(findings) != 1 {
		t.Fatalf("суффикс дополнительной привязки не судится: %v", findings)
	}
	if findings[0].Line != 8 {
		t.Fatalf("координата — строка %d, ожидалась 8 (дополнительная привязка)", findings[0].Line)
	}
	// Основная привязка ТОГО ЖЕ метода осталась каноничной и учтена — то есть
	// анализатор судит привязки по отдельности, а не метод целиком. Три адреса:
	// две привязки этого метода и `:halt` соседнего файла.
	if census.WithVerb != 3 || census.Canonical != 2 {
		t.Fatalf("с действием %d (ожидалось 3), каноном %d (ожидалось 2)",
			census.WithVerb, census.Canonical)
	}
}

// TestVerbCanonInjection_LiveExemptionSuppresses — послабление с ЖИВЫМ предметом
// снимает находку и считается переписью.
func TestVerbCanonInjection_LiveExemptionSuppresses(t *testing.T) {
	s := newVerbStand(t)
	s.write(t, "proto/kacho/cloud/probe/v1/legacy_service.proto",
		"syntax = \"proto3\";\n"+
			"service LegacyService {\n"+
			"  rpc AddBlocks (AddBlocksRequest) returns (Op) {\n"+
			"    option (google.api.http) = {post: \"/probe/v1/things/{id}:add-blocks\"};\n"+
			"  }\n"+
			"}\n")
	findings, census := s.run(t, VerbCanonExemption{
		File:   "proto/kacho/cloud/probe/v1/legacy_service.proto",
		Path:   "/probe/v1/things/{id}:add-blocks",
		Reason: "landed-путь, переводится своим изменением",
	})
	if len(findings) != 0 {
		t.Fatalf("живое послабление не сняло находку: %v", findings)
	}
	if census.Exempted != 1 {
		t.Fatalf("снято послаблением %d, ожидалось 1", census.Exempted)
	}
}

// TestVerbCanonInjection_StaleExemptionIsAFinding — послабление, которому нечего
// исключать, обязано быть находкой: иначе слепая зона переживёт свой предмет.
func TestVerbCanonInjection_StaleExemptionIsAFinding(t *testing.T) {
	s := newVerbStand(t)
	findings, _ := s.run(t, VerbCanonExemption{
		File:   "proto/kacho/cloud/probe/v1/legacy_service.proto",
		Path:   "/probe/v1/things/{id}:add-blocks",
		Reason: "путь давно переведён",
	})
	if len(findings) != 1 || !findings[0].StaleExemption {
		t.Fatalf("устаревшее послабление не стало находкой: %v", findings)
	}
	if !strings.Contains(findings[0].String(), "путь давно переведён") {
		t.Fatalf("находка не называет причину послабления: %s", findings[0].String())
	}
}

// TestVerbCanonInjection_EmptyWalkIsNotSilentSuccess — «ноль находок» обязано
// быть отличимо от «ноль прочитанного».
func TestVerbCanonInjection_EmptyWalkIsNotSilentSuccess(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "proto"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	var log strings.Builder
	findings, census, err := AuditVerbCanon(
		VerbCanonOptions{Tree: clientTruthSyntheticTree(t, root), ProtoRoot: "proto"}, &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	if len(findings) != 0 || census.ProtoFiles != 0 || census.WithVerb != 0 {
		t.Fatalf("перепись непуста на пустом обходе: %+v", census)
	}
}
