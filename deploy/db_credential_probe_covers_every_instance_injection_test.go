// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// db_credential_probe_covers_every_instance_injection_test.go — инъекция в обе
// стороны для гейта парности «проба креденшелов ↔ базы зонта».
//
// Корпус — СИНТЕТИКА в t.TempDir(): фикстура, привязанная к живому перечню,
// истекла бы вместе с ним, а эти плечи переживут любую правку перечня. Судья
// зовётся ТОТ ЖЕ, что на дереве.
//
// Каждый случай меняет против контроля РОВНО ОДИН факт: лишнее имя у пробы,
// недостающее имя у пробы, форма записи. Красное от соседа сюда не приходит.
package deploy_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const injPgChart = `apiVersion: v2
name: kacho-umbrella
dependencies:
  - name: postgresql
    alias: pg-iam
    version: 13.4.4
  - name: postgresql
    alias: pg-vpc
    version: 13.4.4
`

// injPgProbe — синтетическая проба и синтетический зонт. Возвращает путь пробы
// и путь каталога зонта.
func injPgProbe(t *testing.T, probe, chart string) (string, string) {
	t.Helper()
	root := t.TempDir()
	umbrella := filepath.Join(root, "helm", "umbrella")
	if err := os.MkdirAll(umbrella, 0o755); err != nil {
		t.Fatalf("синтетика не построена: %v", err)
	}
	probePath := filepath.Join(root, "E5.sh")
	if err := os.WriteFile(probePath, []byte(probe), 0o644); err != nil {
		t.Fatalf("синтетика не построена: %v", err)
	}
	if err := os.WriteFile(filepath.Join(umbrella, "Chart.yaml"), []byte(chart), 0o644); err != nil {
		t.Fatalf("синтетика не построена: %v", err)
	}
	return probePath, umbrella
}

func injPgJudge(t *testing.T, probe, chart string) ([]dbProbeFinding, dbProbeCensus) {
	t.Helper()
	probePath, umbrella := injPgProbe(t, probe, chart)
	named, lines, err := readDBProbeAliases(probePath)
	if err != nil {
		t.Fatalf("синтетика не прочитана: %v", err)
	}
	aliases, err := umbrellaPgAliases(umbrella)
	if err != nil {
		t.Fatalf("синтетика не прочитана: %v", err)
	}
	return judgeDBCredentialProbe(named, aliases, lines)
}

const injPgProbeLawful = "#!/usr/bin/env bash\nfor svc in iam vpc; do\n  :\ndone\n"

// TestDBCredentialProbeInjection_LawfulCorpusIsSilent — КОНТРОЛЬ.
func TestDBCredentialProbeInjection_LawfulCorpusIsSilent(t *testing.T) {
	t.Parallel()
	findings, census := injPgJudge(t, injPgProbeLawful, injPgChart)
	if census.Named != 2 || census.Aliases != 2 {
		t.Fatalf("предпосылка контроля сломана: %s", census)
	}
	if len(findings) != 0 {
		t.Fatalf("контроль не молчит: %v", findings)
	}
}

// TestDBCredentialProbeInjection_PhantomInstanceIsFound — ИНЪЕКЦИЯ ВНИЗ: проба
// называет базу, которой зонт не объявляет.
func TestDBCredentialProbeInjection_PhantomInstanceIsFound(t *testing.T) {
	t.Parallel()
	findings, _ := injPgJudge(t, "for svc in iam vpc retired-thing; do\n  :\ndone\n", injPgChart)
	if len(findings) != 1 {
		t.Fatalf("инъекция не найдена: находок %d, ожидалась 1: %v", len(findings), findings)
	}
	if !findings[0].Phantom || findings[0].Alias != "pg-retired-thing" {
		t.Fatalf("находка называет не свой предмет: %+v", findings[0])
	}
	if !strings.Contains(findings[0].String(), "утверждает о несуществующем") {
		t.Fatalf("текст находки называет симптом, а не причину: %s", findings[0])
	}
}

// TestDBCredentialProbeInjection_HalfStatementIsFound — ИНЪЕКЦИЯ ВВЕРХ: зонт
// объявил базу, проба её не называет. Без этого плеча «выкинуть половину и
// оставить зелёное» прошло бы молча.
func TestDBCredentialProbeInjection_HalfStatementIsFound(t *testing.T) {
	t.Parallel()
	findings, _ := injPgJudge(t, "for svc in iam; do\n  :\ndone\n", injPgChart)
	if len(findings) != 1 {
		t.Fatalf("инъекция не найдена: находок %d, ожидалась 1: %v", len(findings), findings)
	}
	if findings[0].Phantom || findings[0].Alias != "pg-vpc" {
		t.Fatalf("находка называет не свой предмет: %+v", findings[0])
	}
	if !strings.Contains(findings[0].String(), "половинное") {
		t.Fatalf("текст находки не называет половинность: %s", findings[0])
	}
}

// TestDBCredentialProbeInjection_BothDirectionsRedSeparately — два дефекта
// разных знаков находятся ОБА и не поглощают друг друга.
func TestDBCredentialProbeInjection_BothDirectionsRedSeparately(t *testing.T) {
	t.Parallel()
	findings, _ := injPgJudge(t, "for svc in iam retired-thing; do\n  :\ndone\n", injPgChart)
	if len(findings) != 2 {
		t.Fatalf("находок %d, ожидалось 2: %v", len(findings), findings)
	}
	if !findings[0].Phantom || findings[1].Phantom {
		t.Fatalf("знаки находок перепутаны: %+v", findings)
	}
}

// TestDBCredentialProbeInjection_CommentedLoopIsNotTheList — ГРАНИЦА: перечень
// в комментарии перечнем не является. Иначе объяснение снятого имени само стало
// бы утверждением о нём.
func TestDBCredentialProbeInjection_CommentedLoopIsNotTheList(t *testing.T) {
	t.Parallel()
	probe := "# for svc in retired-thing; do\n" + injPgProbeLawful
	findings, census := injPgJudge(t, probe, injPgChart)
	if census.Named != 2 {
		t.Fatalf("комментарий прочитан как перечень: %s", census)
	}
	if len(findings) != 0 {
		t.Fatalf("гейт прочитал комментарий: %v", findings)
	}
}

// TestDBCredentialProbeInjection_EmptyListIsNotAVerdict — проба без единого
// имени даёт «не исполнялось», а не «находок ноль»: перепись обязана отличать
// одно от другого, и на ней стоит страж предпосылки на дереве.
func TestDBCredentialProbeInjection_EmptyListIsNotAVerdict(t *testing.T) {
	t.Parallel()
	findings, census := injPgJudge(t, "#!/usr/bin/env bash\necho ничего не проверяю\n", injPgChart)
	if census.Named != 0 {
		t.Fatalf("предпосылка случая сломана: %s", census)
	}
	if census.LinesRead == 0 {
		t.Fatalf("перепись не отличает «ноль имён» от «ноль прочитанного»: %s", census)
	}
	// Находки при этом ЕСТЬ — обе базы зонта не названы: именно поэтому страж
	// предпосылки стоит ДО них и объявляет прогон неисполненным.
	if len(findings) != 2 {
		t.Fatalf("находок %d, ожидалось 2: %v", len(findings), findings)
	}
}
