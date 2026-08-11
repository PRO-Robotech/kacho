// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// knobDebtEntry — ключ, объявленный без читателя ДО заведения этой проверки.
//
// # Почему перечень, а не «починить все и не заводить перечень»
//
// Перепись нашла класс сразу в нескольких чартах. Решать за чужой чарт, «мёртв
// ключ или его забыли провязать», — значит либо снести настройку, которую
// собирались подключить, либо оставить дефект под видом решения. Такое решение
// принимает владелец чарта, по одному, со своим предикатом.
//
// Поэтому долг не прячется, а НАЗЫВАЕТСЯ и СЧИТАЕТСЯ: число печатается каждым
// прогоном, новая запись роняет прогон немедленно, а запись, которой больше
// нечего исключать, — тоже находка (проба самоистечения ниже). Так перечень не
// может пережить свой предмет: он истекает сам, а не по чьей-то памяти.
//
// Пустой перечень — законное будущее состояние: он не является предпосылкой
// проверки, только её долговой ведомостью.
type knobDebtEntry struct {
	File string
	Key  string
}

// knobDebt — закрытая ведомость. Отсортирована по файлу и ключу.
//
// Ни одна запись НЕ относится к `kacho-nlb`: ключи этого чарта сняты тем же
// изменением, которым заведена проверка, — иначе она объявляла бы долгом ровно
// тот дефект, ради которого написана.
var knobDebt = []knobDebtEntry{
	// ─── kacho-iam ───────────────────────────────────────────────────────
	{"deploy/helm/umbrella/values.dev.yaml", "kacho-iam.compliance.s3.accessKey"},
	{"deploy/helm/umbrella/values.dev.yaml", "kacho-iam.compliance.s3.bucket"},
	{"deploy/helm/umbrella/values.dev.yaml", "kacho-iam.compliance.s3.endpoint"},
	{"deploy/helm/umbrella/values.dev.yaml", "kacho-iam.compliance.s3.pathStyle"},
	{"deploy/helm/umbrella/values.dev.yaml", "kacho-iam.compliance.s3.region"},
	{"deploy/helm/umbrella/values.dev.yaml", "kacho-iam.compliance.s3.secretKey"},
	{"deploy/helm/umbrella/values.dev.yaml", "kacho-iam.config.extapi.openfga.enabled"},
	{"deploy/helm/umbrella/values.dev.yaml", "kacho-iam.config.extapi.openfga.endpoint"},
	{"deploy/helm/umbrella/values.dev.yaml", "kacho-iam.config.healthcheck.enable"},
	{"deploy/helm/umbrella/values.dev.yaml", "kacho-iam.config.metrics.enable"},
	{"deploy/helm/umbrella/values.dev.yaml", "kacho-iam.opaSidecar.bundle.buildShaEnvVar"},
	{"deploy/helm/umbrella/values.dev.yaml", "kacho-iam.opaSidecar.bundle.cacheStaleAfterSeconds"},
	{"deploy/helm/umbrella/values.dev.yaml", "kacho-iam.opaSidecar.bundle.keyRotationGraceSeconds"},
	{"deploy/helm/umbrella/values.dev.yaml", "kacho-iam.opaSidecar.bundle.signingAlgorithm"},
	{"deploy/helm/umbrella/values.dev.yaml", "kacho-iam.opaSidecar.bundle.signingKeyKid"},
	{"deploy/helm/umbrella/values.dev.yaml", "kacho-iam.opaSidecar.bundle.ttlSeconds"},
	{"deploy/helm/umbrella/values.dev.yaml", "kacho-iam.opaSidecar.iamInternalHost"},
	{"deploy/helm/umbrella/values.prod.yaml", "kacho-iam.config.extapi.openfga.enabled"},
	{"deploy/helm/umbrella/values.prod.yaml", "kacho-iam.config.extapi.openfga.endpoint"},
	{"deploy/helm/umbrella/values.yaml", "kacho-iam.kacho.iam.dpopReplay.cacheSize"},
	{"deploy/helm/umbrella/values.yaml", "kacho-iam.kacho.iam.dpopReplay.cacheTTLSeconds"},
	{"deploy/helm/umbrella/values.yaml", "kacho-iam.kacho.iam.sessionRevocations.cacheTTLSeconds"},
	{"deploy/helm/umbrella/values.yaml", "kacho-iam.kacho.subdomains.api"},

	// ─── openfga-bootstrap ───────────────────────────────────────────────
	{"deploy/helm/umbrella/charts/openfga-bootstrap/values.yaml", "openfgaBootstrap.cliImage"},
	{"deploy/helm/umbrella/values.dev.yaml", "openfga-bootstrap.openfgaBootstrap.cliImage"},
	{"deploy/helm/umbrella/values.yaml", "openfga-bootstrap.openfgaBootstrap.cliImage"},

	// ─── uif (федеративная консоль): у четырёх удалённых модулей объявлены
	// границы масштабирования, а HorizontalPodAutoscaler для них не рендерится ─
	{"ui-future/deploy/values.yaml", "compute.autoscaling.maxReplicas"},
	{"ui-future/deploy/values.yaml", "compute.autoscaling.minReplicas"},
	{"ui-future/deploy/values.yaml", "compute.autoscaling.targetCPUUtilizationPercentage"},
	{"ui-future/deploy/values.yaml", "nlb.autoscaling.maxReplicas"},
	{"ui-future/deploy/values.yaml", "nlb.autoscaling.minReplicas"},
	{"ui-future/deploy/values.yaml", "nlb.autoscaling.targetCPUUtilizationPercentage"},
	{"ui-future/deploy/values.yaml", "registry.autoscaling.maxReplicas"},
	{"ui-future/deploy/values.yaml", "registry.autoscaling.minReplicas"},
	{"ui-future/deploy/values.yaml", "registry.autoscaling.targetCPUUtilizationPercentage"},
	{"ui-future/deploy/values.yaml", "storage.autoscaling.maxReplicas"},
	{"ui-future/deploy/values.yaml", "storage.autoscaling.minReplicas"},
	{"ui-future/deploy/values.yaml", "storage.autoscaling.targetCPUUtilizationPercentage"},

	// ─── зонтичный чарт: адресация чарта `ui`, которого в дереве нет
	// (переименован в `uif`), и собственный блок `kacho:`, из которого шаблоны
	// родителя читают только домен ───────────────────────────────────────
	{"deploy/helm/umbrella/values.digests.example.yaml", "ui.image.tag"},
	{"deploy/helm/umbrella/values.prorobotech.yaml", "ui.image"},
	{"deploy/helm/umbrella/values.prorobotech.yaml", "ui.imagePullPolicy"},
	{"deploy/helm/umbrella/values.yaml", "kacho.hooks.tokenHookSecretKey"},
	{"deploy/helm/umbrella/values.yaml", "kacho.hooks.tokenHookSecretName"},
	{"deploy/helm/umbrella/values.yaml", "kacho.iam.dpopReplay.cacheSize"},
	{"deploy/helm/umbrella/values.yaml", "kacho.iam.dpopReplay.cacheTTLSeconds"},
	{"deploy/helm/umbrella/values.yaml", "kacho.iam.jwks.encKeySecretKey"},
	{"deploy/helm/umbrella/values.yaml", "kacho.iam.jwks.encKeySecretName"},
	{"deploy/helm/umbrella/values.yaml", "kacho.iam.sessionRevocations.cacheTTLSeconds"},
	{"deploy/helm/umbrella/values.yaml", "kacho.subdomains.api"},
	{"deploy/helm/umbrella/values.yaml", "kacho.subdomains.app"},
	{"deploy/helm/umbrella/values.yaml", "kacho.subdomains.hydra"},
	{"deploy/helm/umbrella/values.yaml", "kacho.subdomains.kratos"},
}

func knobDebtKey(file, key string) string { return file + "\x00" + key }

// TestDeclaredKnobHasAReader — ключ, объявленный в профиле развёртывания,
// обязан иметь читателя в шаблоне чарта либо в шаблоне его родителя.
//
// Прогон печатает объём осмотренного всегда: «ноль новых находок» обязано быть
// отличимо от «ноль прочитанного».
func TestDeclaredKnobHasAReader(t *testing.T) {
	findings, census, err := auditProfileKnobReaders(repoRoot(t))
	if err != nil {
		t.Fatalf("перепись профилей не выполнена: %v", err)
	}
	t.Log(census.String())
	t.Logf("ведомость долга: %d записей", len(knobDebt))

	if census.KeysEnforced == 0 {
		t.Fatal("под требованием читателя ноль ключей — проверка ничего не осмотрела, " +
			"и её молчание не является утверждением о дереве")
	}
	if census.TemplateFiles == 0 {
		t.Fatal("прочитано ноль файлов шаблонов — читателя искать было негде, " +
			"поэтому «читателя нет» сказано обо всём дереве сразу")
	}

	seen := map[string]bool{}
	var fresh []string
	for _, f := range findings {
		seen[knobDebtKey(f.File, f.Key)] = true
	}
	debt := map[string]bool{}
	for _, d := range knobDebt {
		debt[knobDebtKey(d.File, d.Key)] = true
	}
	for _, f := range findings {
		if !debt[knobDebtKey(f.File, f.Key)] {
			fresh = append(fresh, f.String())
		}
	}
	sort.Strings(fresh)

	if len(fresh) > 0 {
		t.Fatalf("объявлено без читателя — %d НОВЫХ ключей (всего находок %d, из них в ведомости %d):\n%s\n\n"+
			"Ключ профиля, которого не читает ни шаблон чарта, ни его родитель, до процесса не "+
			"доедет НИКОГДА: значение остаётся в файле. Для поверхности безопасности это хуже "+
			"отсутствия ключа — оператор читает строку как заявление о посадке и распоряжается "+
			"тем, чего нет. Исходов три: провязать читателя, снять ключ из профиля, либо (если "+
			"ключ читает сам helm) объявить его `condition:` зависимости.\n%s",
			len(fresh), len(findings), len(findings)-len(fresh),
			strings.Join(fresh, "\n"), census.String())
	}
}

// TestKnobDebtExpiresOnItsOwn — запись ведомости, которой больше нечего
// исключать, роняет прогон.
//
// Без этой половины ведомость пережила бы свой предмет: починенный ключ остался
// бы в списке, следующий читатель принял бы его за действующий долг, а
// освободившееся место унаследовал бы новый дефект с тем же путём.
func TestKnobDebtExpiresOnItsOwn(t *testing.T) {
	findings, census, err := auditProfileKnobReaders(repoRoot(t))
	if err != nil {
		t.Fatalf("перепись профилей не выполнена: %v", err)
	}
	t.Log(census.String())

	live := map[string]bool{}
	for _, f := range findings {
		live[knobDebtKey(f.File, f.Key)] = true
	}
	var stale []string
	for _, d := range knobDebt {
		if !live[knobDebtKey(d.File, d.Key)] {
			stale = append(stale, fmt.Sprintf("%s: %s", d.File, d.Key))
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Fatalf("в ведомости %d записей, которым больше нечего исключать:\n%s\n\n"+
			"Ключ провязан или снят — запись обязана уйти из ведомости тем же изменением. "+
			"Оставленная, она объявляет живым закрытый долг и освобождает место новому "+
			"дефекту с тем же путём.", len(stale), strings.Join(stale, "\n"))
	}
}

// TestKnobDebtIsWellFormed — ведомость не содержит дублей и отсортирована по
// файлу: перечень, который читают глазами при каждой находке, обязан быть
// читаемым, а дубль скрывает, что записей на одну меньше, чем кажется.
func TestKnobDebtIsWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, d := range knobDebt {
		k := knobDebtKey(d.File, d.Key)
		if seen[k] {
			t.Errorf("дубль в ведомости: %s / %s", d.File, d.Key)
		}
		seen[k] = true
		if d.File == "" || d.Key == "" {
			t.Errorf("запись ведомости без координаты: %+v", d)
		}
	}
	t.Logf("ведомость: %d записей", len(knobDebt))
}
