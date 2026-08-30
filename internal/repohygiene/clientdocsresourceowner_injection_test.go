// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// clientdocsresourceowner_injection_test.go — ДОКАЗАТЕЛЬСТВО того, что анализатор
// способен упасть, называет координату и молчит на законном близнеце.
//
// Стенд синтетический: настоящее дерево нельзя ни сломать, ни вернуть, а вердикт
// о нём (`clientdocsresourceowner_test.go`) о способности падать не говорит
// ничего — зелёный получает и та проверка, что не смотрит никуда.
//
// Инъекции вносятся ПО ОДНОЙ. Инъекция, нарушающая два свойства разом,
// доказывает лишь то, что покраснело хоть что-то. К каждой приложен законный
// близнец той же формы, обязанный молчать.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ownerStand — синтетическое дерево: контракты двух доменов и клиентская
// страница, называющая владение верно. Это ЗАКОННОЕ состояние, и на нём
// анализатор обязан молчать.
type ownerStand struct{ root string }

func newOwnerStand(t *testing.T) *ownerStand {
	t.Helper()
	s := &ownerStand{root: t.TempDir()}

	s.write(t, "proto/kacho/cloud/probecompute/v1/machine_service.proto", `
syntax = "proto3";
package kacho.cloud.probecompute.v1;

// В комментарии стоит "service ProbeVolumeService" — и это НЕ объявление.
// Анализатор, считающий сырой текст, приписал бы том вычислениям.
service ProbeMachineService {
  rpc Get(GetRequest) returns (ProbeMachine);
}
service InternalProbeMachineService {
  rpc Get(GetRequest) returns (ProbeMachine);
}
`)
	s.write(t, "proto/kacho/cloud/probestorage/v1/volume_service.proto", `
syntax = "proto3";
package kacho.cloud.probestorage.v1;

service ProbeVolumeService {
  rpc Get(GetRequest) returns (ProbeVolume);
}
service ProbeSnapshotService {
  rpc Get(GetRequest) returns (ProbeSnapshot);
}
`)
	// `ProbeQuota` служит КАЖДЫЙ домен — имя с несколькими владельцами. Оно
	// обязано быть выведено из словаря: судить его значило бы краснеть на
	// верном тексте.
	s.write(t, "proto/kacho/cloud/probecompute/v1/quota_service.proto", `
syntax = "proto3";
package kacho.cloud.probecompute.v1;
service ProbeQuotaService { rpc List(R) returns (S); }
`)
	s.write(t, "proto/kacho/cloud/probestorage/v1/quota_service.proto", `
syntax = "proto3";
package kacho.cloud.probestorage.v1;
service ProbeQuotaService { rpc List(R) returns (S); }
`)

	s.write(t, "gateway/docs/content/intro.mdx", `
<table>
  <tbody>
    <tr><td><strong>Compute</strong></td><td><code>kacho-probecompute</code></td><td>ProbeMachine / ProbeQuota</td></tr>
    <tr><td><strong>Storage</strong></td><td><code>kacho-probestorage</code></td><td>ProbeVolume / ProbeSnapshot</td></tr>
  </tbody>
</table>
`)
	return s
}

func (s *ownerStand) write(t *testing.T, rel, body string) {
	t.Helper()
	p := filepath.Join(s.root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", rel, err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func (s *ownerStand) run(t *testing.T) ([]ClientDocsResourceOwnerFinding, ClientDocsResourceOwnerCensus) {
	t.Helper()
	var log strings.Builder
	f, c, err := AuditClientDocsResourceOwner(ClientDocsResourceOwnerOptions{
		Root:          s.root,
		ProtoRoot:     "proto",
		DomainAliases: map[string]string{"probenlb": "probeloadbalancer"},
	}, &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))
	return f, c
}

// TestClientDocsOwnerInjection_CleanStandIsSilent — контроль. Без него всякая
// последующая краснота неотличима от анализатора, краснеющего на всём.
func TestClientDocsOwnerInjection_CleanStandIsSilent(t *testing.T) {
	s := newOwnerStand(t)
	findings, census := s.run(t)
	if len(findings) != 0 {
		t.Fatalf("на законном дереве находок %d, ожидался ноль: %v", len(findings), findings)
	}
	if census.OwnershipRow != 2 {
		t.Fatalf("строк владения распознано %d, ожидалось 2 — стенд прочитан не весь", census.OwnershipRow)
	}
	// Три имени с единственным владельцем (ProbeMachine, ProbeVolume,
	// ProbeSnapshot); ProbeQuota выведен как многовладельческий.
	if census.OwnedNames != 3 || census.AmbiguousOut != 1 {
		t.Fatalf("имён с одним владельцем %d (ожидалось 3), многовладельческих %d (ожидалось 1)",
			census.OwnedNames, census.AmbiguousOut)
	}
	// Судятся три имени: ProbeMachine, ProbeVolume, ProbeSnapshot. ProbeQuota
	// стоит в ячейке и НЕ судится — вот доказательство, что вывод из словаря
	// работает, а не просто не сработал.
	if census.NamesJudged != 3 || census.NamesOutside != 1 {
		t.Fatalf("имён рассужено %d (ожидалось 3), вне словаря %d (ожидалось 1: ProbeQuota)",
			census.NamesJudged, census.NamesOutside)
	}
}

// TestClientDocsOwnerInjection_ForeignResourceIsFound — НАСТОЯЩИЙ дефект:
// ресурс хранения приписан вычислениям. Ровно тот текст, что жил в дереве до
// kacho#1392.
func TestClientDocsOwnerInjection_ForeignResourceIsFound(t *testing.T) {
	s := newOwnerStand(t)
	s.write(t, "gateway/docs/content/intro.mdx", `
<table>
  <tbody>
    <tr><td><strong>Compute</strong></td><td><code>kacho-probecompute</code></td><td>ProbeMachine / ProbeVolume / ProbeSnapshot</td></tr>
  </tbody>
</table>
`)
	findings, _ := s.run(t)
	if len(findings) != 2 {
		t.Fatalf("находок %d, ожидалось 2 (ProbeVolume и ProbeSnapshot у чужого домена): %v",
			len(findings), findings)
	}
	joined := findings[0].String() + "\n" + findings[1].String()
	// Находка обязана называть КООРДИНАТУ и обе стороны спора — иначе на неё
	// потратят прогон, а потом снимут гейт как непонятный.
	for _, want := range []string{
		"gateway/docs/content/intro.mdx:4",
		"ProbeVolume", "ProbeSnapshot",
		"probecompute", "probestorage",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("находка не называет %q:\n%s", want, joined)
		}
	}
}

// TestClientDocsOwnerInjection_RetiredNameIsNotJudged — граница, объявленная
// анализатором, ДЕЙСТВИТЕЛЬНО действует: снятое имя (`ProbeDisk` — службы такой
// нет ни у кого) не судится и находкой не становится. Проба стоит здесь, чтобы
// слепая зона была доказана, а не заявлена.
func TestClientDocsOwnerInjection_RetiredNameIsNotJudged(t *testing.T) {
	s := newOwnerStand(t)
	s.write(t, "gateway/docs/content/intro.mdx", `
<table>
  <tbody>
    <tr><td><strong>Compute</strong></td><td><code>kacho-probecompute</code></td><td>ProbeMachine / ProbeDisk</td></tr>
  </tbody>
</table>
`)
	findings, census := s.run(t)
	if len(findings) != 0 {
		t.Fatalf("снятое имя не судится по объявленной границе, а находок %d: %v", len(findings), findings)
	}
	if census.NamesOutside != 1 {
		t.Fatalf("имён вне словаря %d, ожидалось 1 — слепая зона обязана печататься числом", census.NamesOutside)
	}
}

// TestClientDocsOwnerInjection_TwoDomainsInOneRowAreNotJudged — законный близнец
// той же формы: строка, называющая ДВА домена, говорит о связи, а не о владении.
func TestClientDocsOwnerInjection_TwoDomainsInOneRowAreNotJudged(t *testing.T) {
	s := newOwnerStand(t)
	s.write(t, "gateway/docs/content/intro.mdx", `
<table>
  <tbody>
    <tr><td><code>kacho-probecompute</code> зовёт <code>kacho-probestorage</code></td><td>ProbeVolume / ProbeSnapshot</td></tr>
  </tbody>
</table>
`)
	findings, census := s.run(t)
	if len(findings) != 0 {
		t.Fatalf("строка о связи двух доменов о владении не высказывается, а находок %d: %v",
			len(findings), findings)
	}
	if census.OwnershipRow != 0 {
		t.Fatalf("строк владения распознано %d, ожидался ноль", census.OwnershipRow)
	}
}

// TestClientDocsOwnerInjection_AliasIsHonoured — короткое имя домена в
// документации (`nlb` при каталоге контракта `loadbalancer`) не делает верную
// строку находкой.
func TestClientDocsOwnerInjection_AliasIsHonoured(t *testing.T) {
	s := newOwnerStand(t)
	s.write(t, "proto/kacho/cloud/probeloadbalancer/v1/lb_service.proto", `
syntax = "proto3";
package kacho.cloud.probeloadbalancer.v1;
service ProbeListenerService { rpc Get(R) returns (S); }
service ProbeTargetGroupService { rpc Get(R) returns (S); }
`)
	s.write(t, "gateway/docs/content/intro.mdx", `
<table>
  <tbody>
    <tr><td><strong>Load Balancer</strong></td><td><code>kacho-probenlb</code></td><td>ProbeListener / ProbeTargetGroup</td></tr>
  </tbody>
</table>
`)
	findings, census := s.run(t)
	if len(findings) != 0 {
		t.Fatalf("псевдоним домена не почтён, находок %d: %v", len(findings), findings)
	}
	if census.NamesJudged != 2 {
		t.Fatalf("имён рассужено %d, ожидалось 2 — строка под псевдонимом не прочитана", census.NamesJudged)
	}
}

// TestClientDocsOwnerInjection_ServiceNamedOnlyInCommentIsNotOwnership — гейт
// читает ОБЪЯВЛЕНИЕ службы, а не упоминание. Стенд несёт строку
// "service ProbeVolumeService" в комментарии контракта вычислений; если бы
// анализатор считал её объявлением, том принадлежал бы двум доменам, ушёл бы в
// многовладельческие и перестал судиться ВООБЩЕ — то есть настоящий дефект стал
// бы невидим.
func TestClientDocsOwnerInjection_ServiceNamedOnlyInCommentIsNotOwnership(t *testing.T) {
	s := newOwnerStand(t)
	_, census := s.run(t)
	if census.AmbiguousOut != 1 {
		t.Fatalf("многовладельческих имён %d, ожидалось ровно 1 (ProbeQuota): "+
			"упоминание службы в комментарии зачтено за объявление", census.AmbiguousOut)
	}
}

// TestClientDocsOwnerInjection_EmptyTreeIsRefusal — пустой обход обязан быть
// ОТКАЗОМ, а не успехом: «находок ноль» иначе неотличимо от «прочитано ноль».
func TestClientDocsOwnerInjection_EmptyTreeIsRefusal(t *testing.T) {
	root := t.TempDir()
	var log strings.Builder
	_, _, err := AuditClientDocsResourceOwner(ClientDocsResourceOwnerOptions{
		Root: root, ProtoRoot: "proto",
	}, &log)
	if err == nil {
		t.Fatal("на дереве без контрактов анализатор обязан отказать, а не выйти успехом")
	}
}
