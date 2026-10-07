// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config_test

// required_knobs_injection_test.go — способность держателя Р16 упасть
// (DoD S1 п.5): семь инъекций — умолчание у одной ручки, ручка вне перечня
// Р16, чтение окружения в обход загрузчика, снятая граница у одной ручки,
// снятая зависимая граница `…_REPUTATION_WINDOW ≤ …_SENT_LOG_RETENTION`,
// снятая нижняя граница длины ключа отпечатка, материал ключа отпечатка в
// тексте отказа.
//
// Испытуемый инъекций — синтетика: эталон границ s1Model, синтетическая
// перепись групп, синтетические исходники. Контроль — та же синтетика без
// дефекта: держатель на ней молчит. Каждая инъекция роняет ровно свой предмет
// и требует, чтобы находка НАЗВАЛА его, а не только покраснела.

import (
	"strings"
	"testing"
)

func rowOf(t *testing.T, env string) knobRow {
	t.Helper()
	for _, r := range s1Rows() {
		if r.env == env {
			return r
		}
	}
	t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: строки %s в таблице нет", env)
	return knobRow{}
}

// syntheticCensus — перепись загрузчика, у которого группы объявлены верно:
// прочие ручки — NTF-1, ручки S1 — NTF-4.
func syntheticCensus() []censusKnob {
	out := []censusKnob{
		{env: "KACHO_NOTIFY_DB_HOST", group: groupNTF1},
		{env: "KACHO_NOTIFY_DNS_BOOT_DEADLINE", group: groupNTF1},
	}
	for _, env := range s1Envs {
		out = append(out, censusKnob{env: env, group: groupNTF4})
	}
	return out
}

func syntheticSources() map[string]string {
	return map[string]string{
		"internal/config/config.go":                  "package config\nimport \"os\"\nfunc f() { _ = os.Getenv(\"X\") }\n",
		"cmd/notify-probe/internal/config/config.go": "package config\nimport \"os\"\nfunc f() { _, _ = os.LookupEnv(\"X\") }\n",
		"internal/deliver/deliver.go":                "package deliver\nimport \"os\"\nfunc g() { _, _ = os.ReadFile(\"x\") }\n",
	}
}

func requireFindingNames(t *testing.T, found []string, subject ...string) {
	t.Helper()
	if len(found) == 0 {
		t.Fatalf("инъекция не стала находкой: держатель промолчал (ожидалось упоминание %q)", subject)
	}
	all := strings.Join(found, "\n")
	for _, s := range subject {
		if !strings.Contains(all, s) {
			t.Fatalf("находка не называет %q:\n%s", s, all)
		}
	}
}

// Контроль: на синтетике без дефекта держатель молчит.
func TestRequiredKnobsInjection_ControlIsSilent(t *testing.T) {
	model := s1Model(defectNone)
	for _, r := range s1Rows() {
		if f := judgeKnob(t, r, model); len(f) > 0 {
			t.Fatalf("контроль: держатель краснеет на эталоне без дефекта:\n%s", strings.Join(f, "\n"))
		}
	}
	if f := judgeWindowWithinRetention(t, model); len(f) > 0 {
		t.Fatalf("контроль: зависимая граница краснеет на эталоне без дефекта: %v", f)
	}
	if f := judgeGroups(syntheticCensus(), s1Envs); len(f) > 0 {
		t.Fatalf("контроль: группы краснеют на верной переписи: %v", f)
	}
	f, err := envReadFindings(syntheticSources())
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: %v", err)
	}
	if len(f) > 0 {
		t.Fatalf("контроль: чтение окружения находит лишнее: %v", f)
	}
}

// Инъекция 1: умолчание у одной ручки.
func TestRequiredKnobsInjection_DefaultOnOneKnobIsFound(t *testing.T) {
	found := judgeKnob(t, rowOf(t, envHardTTL), s1Model(defectDefaultHardTTL))
	requireFindingNames(t, found, envHardTTL+" снята", "старт принят")
}

// Инъекция 2: ручка вне перечня Р16 — в группе NTF-4; и её разновидности:
// ручка стадии S3 в группе `notify-sender`, ручка S1 вне группы NTF-4,
// ручка Р16 в группе NTF-1, пустая перепись.
func TestRequiredKnobsInjection_KnobOutsideTheListIsFound(t *testing.T) {
	extra := append(syntheticCensus(), censusKnob{env: "KACHO_NOTIFY_FEEDBACK_EXTRA", group: groupNTF4})
	requireFindingNames(t, judgeGroups(extra, s1Envs), "KACHO_NOTIFY_FEEDBACK_EXTRA", "вне перечня")

	s3 := append(syntheticCensus(), censusKnob{env: s3Envs[0], group: groupNTF4})
	requireFindingNames(t, judgeGroups(s3, s1Envs), s3Envs[0], "стадии S3")

	var dropped []censusKnob
	for _, k := range syntheticCensus() {
		if k.env != envSecretReload {
			dropped = append(dropped, k)
		}
	}
	requireFindingNames(t, judgeGroups(dropped, s1Envs), envSecretReload, "нет в группе NTF-4")

	moved := syntheticCensus()
	for i := range moved {
		if moved[i].env == envLeaseTTL {
			moved[i].group = groupNTF1
		}
	}
	requireFindingNames(t, judgeGroups(moved, s1Envs), envLeaseTTL, "в группе NTF-1")

	requireFindingNames(t, judgeGroups(nil, s1Envs), "пуста")
}

// Инъекция 3: чтение окружения в обход загрузчика — вызовом, ссылкой без
// вызова, под другим именем импорта; исключение загрузчика без предмета.
func TestRequiredKnobsInjection_EnvReadBypassIsFound(t *testing.T) {
	for name, src := range map[string]string{
		"вызов":           "package deliver\nimport \"os\"\nfunc g() string { return os.Getenv(\"KACHO_NOTIFY_X\") }\n",
		"ссылка":          "package deliver\nimport \"os\"\nvar look = os.LookupEnv\n",
		"другое имя":      "package deliver\nimport sys \"os\"\nfunc g() []string { return sys.Environ() }\n",
		"загрузчик чужой": "package deliver\nimport \"github.com/kelseyhightower/envconfig\"\nfunc g(v any) error { return envconfig.Process(\"\", v) }\n",
	} {
		t.Run(name, func(t *testing.T) {
			srcs := syntheticSources()
			srcs["internal/deliver/bypass.go"] = src
			f, err := envReadFindings(srcs)
			if err != nil {
				t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: %v", err)
			}
			requireFindingNames(t, f, "internal/deliver/bypass.go", "вне загрузчика")
		})
	}
	t.Run("исключение без предмета", func(t *testing.T) {
		srcs := syntheticSources()
		srcs["cmd/notify-probe/internal/config/config.go"] = "package config\nfunc f() {}\n"
		f, err := envReadFindings(srcs)
		if err != nil {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: %v", err)
		}
		requireFindingNames(t, f, "cmd/notify-probe/internal/config", "без предмета")
	})
}

// Инъекция 4: снятая граница у одной ручки (верхняя у срока HARD_BOUNCE).
func TestRequiredKnobsInjection_RemovedBoundIsFound(t *testing.T) {
	found := judgeKnob(t, rowOf(t, envHardTTL), s1Model(defectNoUpperHardTTL))
	requireFindingNames(t, found, envHardTTL+" за границей 72h0m1s", "старт принят")
}

// Инъекция 5: снятая зависимая граница окна долей (Д26·6): при сроке журнала
// 7 сут и окне 30 сут процесс стартует.
func TestRequiredKnobsInjection_RemovedWindowRetentionBoundIsFound(t *testing.T) {
	found := judgeWindowWithinRetention(t, s1Model(defectNoWindowRetention))
	requireFindingNames(t, found, "окно 30 сут при сроке журнала 7 сут", "старт принят")
}

// Инъекция 6: нижняя граница длины ключа отпечатка снята до 1 октета — ключ на
// октет короче границы принят. Находку даёт строка «на один октет короче», а не
// соседняя «не разбирается».
func TestRequiredKnobsInjection_RemovedAddressKeyLowerBoundIsFound(t *testing.T) {
	found := judgeKnob(t, rowOf(t, envAddressKeyDir), s1Model(defectNoAddressKeyLowerBound))
	requireFindingNames(t, found, envAddressKeyDir+" за границей: ключ на один октет короче", "старт принят")
	for _, f := range found {
		if strings.Contains(f, "не разбирается") {
			t.Fatalf("инъекция роняет не только свой предмет: %s", f)
		}
	}
}

// Инъекция 7: текст отказа ключа отпечатка несёт содержимое файла — обе
// строки отказа, где проба подала материал, называют утечку.
func TestRequiredKnobsInjection_AddressKeyMaterialInRefusalIsFound(t *testing.T) {
	found := judgeKnob(t, rowOf(t, envAddressKeyDir), s1Model(defectAddressKeyLeaked))
	requireFindingNames(t, found,
		envAddressKeyDir+" за границей: ключ на один октет короче",
		envAddressKeyDir+" за границей: ключ не разбирается",
		"несёт материал секрета")
}
