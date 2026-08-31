// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// clienttruth_vpc_protoprefix_injection_test.go — ДОКАЗАТЕЛЬСТВО того, что
// анализатор способен упасть, называет координату и молчит на законном близнеце.
//
// Стенд синтетический: настоящее дерево нельзя ни сломать, ни вернуть, а вердикт
// о нём (`clienttruth_vpc_protoprefix_test.go`) о способности падать не говорит
// ничего — зелёный получает и та проверка, что не смотрит никуда.
//
// Инъекции вносятся ПО ОДНОЙ, и каждая снимает РОВНО ОДНО свойство у элемента,
// чьи остальные свойства целы: инъекция вида «завести ещё один негодный элемент»
// нарушает всё, что требуется от элементов вообще, и краснота от неё ничего не
// доказывает (`testing.md` §«Гейт на класс», п.2в).
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// prefixStand — синтетическое дерево: источник словаря и контракты трёх доменов,
// где каждое утверждение о префиксе верно. Это ЗАКОННОЕ состояние, и на нём
// анализатор обязан молчать.
type prefixStand struct{ root string }

func newPrefixStand(t *testing.T) *prefixStand {
	t.Helper()
	s := &prefixStand{root: t.TempDir()}

	// Словарь. `ProbeImage` объявлен ДВАЖДЫ — голой константой и доменной: ровно
	// та форма, из-за которой одно слово значит разное в разных доменах.
	s.write(t, "pkg/ids/ids.go", `
package ids

// В комментарии стоит PrefixProbeGhost = "ghs" — и это НЕ объявление.
// Анализатор, читающий сырой текст, собрал бы словарь из прозы о нём.
const (
	PrefixProbeNetwork          = "pnt"
	PrefixProbeNetworkInterface = "pni"
	PrefixProbeImage            = "pim"
	PrefixProbestorageProbeImage = "psi"
	PrefixProbeOperationVPC     = "pop"
)
`)
	s.write(t, "proto/kacho/cloud/probevpc/v1/nic_service.proto", `
syntax = "proto3";
message AttachRequest {
  // ID of the NIC (ProbeNetworkInterface, prefix "pni") to attach.
  string nic_id = 1;
}
`)
	// Верный комментарий образа ХРАНИЛИЩА: голая константа дала бы "pim", а
	// доменная даёт "psi". Молчание здесь доказывает, что правило домена
	// работает, а не просто не сработало.
	s.write(t, "proto/kacho/cloud/probestorage/v1/image.proto", `
syntax = "proto3";
// A ProbeImage resource — boot image owned by probestorage (prefix "psi").
message ProbeImage { string id = 1; }
`)
	// Строка без известного имени: тип, которому префикс приписан, не назван.
	// Судить её значило бы краснеть наугад.
	s.write(t, "proto/kacho/cloud/probevpc/v1/ops.proto", `
syntax = "proto3";
// Operation ids carry a per-domain prefix "pop" so the edge can route them.
message ProbeOp { string id = 1; }
`)
	return s
}

func (s *prefixStand) write(t *testing.T, rel, body string) {
	t.Helper()
	p := filepath.Join(s.root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", rel, err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func (s *prefixStand) run(
	t *testing.T, ex ...ProtoPrefixClaimExemption,
) ([]ProtoPrefixClaimFinding, ProtoPrefixClaimCensus) {
	t.Helper()
	var log strings.Builder
	f, c, err := AuditProtoPrefixClaims(ProtoPrefixClaimOptions{
		Tree: clientTruthSyntheticTree(t, s.root), ProtoRoot: "proto", Exemptions: ex,
	}, &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))
	return f, c
}

// TestProtoPrefixInjection_CleanStandIsSilent — контроль. Без него всякая
// последующая краснота неотличима от анализатора, краснеющего на всём.
func TestProtoPrefixInjection_CleanStandIsSilent(t *testing.T) {
	s := newPrefixStand(t)
	findings, census := s.run(t)
	if len(findings) != 0 {
		t.Fatalf("на законном дереве находок %d, ожидался ноль: %v", len(findings), findings)
	}
	if census.ProtoFiles != 3 {
		t.Fatalf("файлов контракта %d, ожидалось 3 — стенд прочитан не весь", census.ProtoFiles)
	}
	// Пять объявлений; строка `PrefixProbeGhost` из комментария в словарь не
	// попала — доказательство, что читается объявление, а не текст.
	if census.KnownNames != 5 {
		t.Fatalf("имён в словаре %d, ожидалось 5 — словарь собран из прозы либо не собран",
			census.KnownNames)
	}
	if census.Claims != 3 || census.Judged != 2 || census.NoName != 1 {
		t.Fatalf("утверждений %d (ожидалось 3), рассужено %d (ожидалось 2), без имени %d (ожидалось 1)",
			census.Claims, census.Judged, census.NoName)
	}
	// Ровно один резолв выиграла доменная константа — образ хранилища.
	if census.DomainQualified != 1 {
		t.Fatalf("резолвов по домену %d, ожидался 1 — правило домена не сработало",
			census.DomainQualified)
	}
}

// TestProtoPrefixInjection_WrongPrefixIsFound — ИНЪЕКЦИЯ: у существующего
// утверждения меняется ОДНО свойство — названный префикс. Остальное цело.
func TestProtoPrefixInjection_WrongPrefixIsFound(t *testing.T) {
	s := newPrefixStand(t)
	s.write(t, "proto/kacho/cloud/probevpc/v1/nic_service.proto", `
syntax = "proto3";
message AttachRequest {
  // ID of the NIC (ProbeNetworkInterface, prefix "pop") to attach.
  string nic_id = 1;
}
`)
	findings, _ := s.run(t)
	if len(findings) != 1 {
		t.Fatalf("находок %d, ожидалась 1: %v", len(findings), findings)
	}
	f := findings[0]
	if !strings.Contains(f.File, "nic_service.proto") || f.Line != 4 {
		t.Fatalf("координата не названа: %+v", f)
	}
	if f.Name != "ProbeNetworkInterface" || f.Claimed != "pop" || f.Actual != "pni" {
		t.Fatalf("находка не называет предмет: %+v", f)
	}
}

// TestProtoPrefixInjection_DomainQualifiedTwinStaysSilent — ЗАКОННЫЙ БЛИЗНЕЦ той
// же формы: тот же префикс `psi` у образа ХРАНИЛИЩА верен, а у образа соседнего
// домена — нет. Пара доказывает, что судится предмет, а не форма строки.
func TestProtoPrefixInjection_DomainQualifiedTwinStaysSilent(t *testing.T) {
	s := newPrefixStand(t)
	// Тот же текст, тот же префикс — но домен другой, и голая константа даёт
	// "pim". Это находка.
	s.write(t, "proto/kacho/cloud/probecompute/v1/image.proto", `
syntax = "proto3";
// A ProbeImage resource — boot image owned by probecompute (prefix "psi").
message ProbeImage { string id = 1; }
`)
	findings, census := s.run(t)
	if len(findings) != 1 {
		t.Fatalf("находок %d, ожидалась 1 (образ вычислений): %v", len(findings), findings)
	}
	if !strings.Contains(findings[0].File, "probecompute") {
		t.Fatalf("находка указывает не на тот домен: %+v", findings[0])
	}
	if findings[0].Actual != "pim" {
		t.Fatalf("резолв голой константой не сработал: %+v", findings[0])
	}
	// Близнец из хранилища по-прежнему молчит — иначе краснота выше была бы
	// свойством формы, а не предмета.
	if census.DomainQualified != 1 {
		t.Fatalf("резолвов по домену %d, ожидался 1 — близнец хранилища перестал резолвиться",
			census.DomainQualified)
	}
}

// TestProtoPrefixInjection_LongestNameWins — имя берётся СЛОВОМ целиком.
// Отбор по подстроке нашёл бы в `ProbeNetworkInterface` ещё и `ProbeNetwork`,
// строка стала бы многоимённой и дефект ушёл бы в молчание — то есть защита
// выродилась бы ровно на том входе, ради которого написана.
func TestProtoPrefixInjection_LongestNameWins(t *testing.T) {
	s := newPrefixStand(t)
	s.write(t, "proto/kacho/cloud/probevpc/v1/nic_service.proto", `
syntax = "proto3";
message AttachRequest {
  // ID of ProbeNetworkInterface, prefix "pnt".
  string nic_id = 1;
}
`)
	findings, census := s.run(t)
	if census.Ambiguous != 0 {
		t.Fatalf("многоимённых %d, ожидался ноль — имя разобрано по подстроке", census.Ambiguous)
	}
	if len(findings) != 1 || findings[0].Name != "ProbeNetworkInterface" {
		t.Fatalf("находок %d, ожидалась 1 про ProbeNetworkInterface: %v", len(findings), findings)
	}
}

// TestProtoPrefixInjection_AmbiguousLineIsNotJudged — строка с ДВУМЯ известными
// именами не судится: о котором из них сказано, неизвестно. Без этой ветви
// анализатор краснел бы наугад.
func TestProtoPrefixInjection_AmbiguousLineIsNotJudged(t *testing.T) {
	s := newPrefixStand(t)
	s.write(t, "proto/kacho/cloud/probevpc/v1/nic_service.proto", `
syntax = "proto3";
message AttachRequest {
  // ProbeNetwork owns the ProbeNetworkInterface (prefix "pnt").
  string nic_id = 1;
}
`)
	findings, census := s.run(t)
	if census.Ambiguous != 1 {
		t.Fatalf("многоимённых %d, ожидалась 1", census.Ambiguous)
	}
	if len(findings) != 0 {
		t.Fatalf("находок %d, ожидался ноль — многоимённая строка рассужена наугад: %v",
			len(findings), findings)
	}
}

// TestProtoPrefixInjection_ExemptionSilencesItsOwnSubject — послабление снимает
// РОВНО свою находку.
func TestProtoPrefixInjection_ExemptionSilencesItsOwnSubject(t *testing.T) {
	s := newPrefixStand(t)
	s.write(t, "proto/kacho/cloud/probevpc/v1/nic_service.proto", `
syntax = "proto3";
message AttachRequest {
  // ID of the NIC (ProbeNetworkInterface, prefix "pop") to attach.
  string nic_id = 1;
}
`)
	ex := ProtoPrefixClaimExemption{
		File:   "proto/kacho/cloud/probevpc/v1/nic_service.proto",
		Name:   "ProbeNetworkInterface",
		Prefix: "pop",
		Reason: "правится соседней полосой",
	}
	findings, census := s.run(t, ex)
	if len(findings) != 0 {
		t.Fatalf("находок %d, ожидался ноль — послабление не сработало: %v", len(findings), findings)
	}
	if census.Exempted != 1 {
		t.Fatalf("снято послаблением %d, ожидалась 1", census.Exempted)
	}
}

// TestProtoPrefixInjection_StaleExemptionIsAFinding — послабление, которому
// БОЛЬШЕ НЕЧЕГО исключать, обязано краснеть. Без этой ветви слепая зона пережила
// бы свой предмет и досталась следующему как «тут так принято».
func TestProtoPrefixInjection_StaleExemptionIsAFinding(t *testing.T) {
	s := newPrefixStand(t) // стенд ЗАКОННЫЙ — исключать нечего
	ex := ProtoPrefixClaimExemption{
		File:   "proto/kacho/cloud/probevpc/v1/nic_service.proto",
		Name:   "ProbeNetworkInterface",
		Prefix: "pop",
		Reason: "правится соседней полосой",
	}
	findings, _ := s.run(t, ex)
	if len(findings) != 1 || !findings[0].StaleExemption {
		t.Fatalf("устаревшее послабление не найдено: %v", findings)
	}
	if !strings.Contains(findings[0].String(), "потеряло предмет") {
		t.Fatalf("находка не объясняет, что делать: %q", findings[0].String())
	}
}

// TestProtoPrefixInjection_EmptyWalkIsAFinding — «ноль находок» обязано быть
// отличимо от «ноль прочитанного»: на дереве без контрактов перепись обязана
// показать ноль, и вердикт по ней выносить нельзя.
func TestProtoPrefixInjection_EmptyWalkIsAFinding(t *testing.T) {
	s := &prefixStand{root: t.TempDir()}
	s.write(t, "pkg/ids/ids.go", "package ids\n")
	s.write(t, "proto/.keep", "")
	findings, census := s.run(t)
	if len(findings) != 0 {
		t.Fatalf("находок %d на пустом дереве, ожидался ноль", len(findings))
	}
	if census.ProtoFiles != 0 || census.KnownNames != 0 || census.Claims != 0 {
		t.Fatalf("перепись не показала пустоту: %+v", census)
	}
}
