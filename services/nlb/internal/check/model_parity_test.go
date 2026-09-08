// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// model_parity_test.go — drift-gate «требуемое отношение объявлено в модели прав».
//
// Класс дефекта: permission-catalog требует (object_type, required_relation),
// которого в модели прав НЕТ вовсе. Источник вердикта отвергает такой вопрос —
// плана вывода для необъявленной пары не существует, и ответом становится ОШИБКА,
// а не отказ; интерсептор фейлится fail-closed → RPC отдаёт PERMISSION_DENIED
// ВСЕГДА, независимо от любых выданных прав. Это не «нет доступа», а неработающая
// по построению модель: ни один subject не может получить доступ, и ни один
// seed/грант этого не исправит. Симметрично #68 (nlb_listener без `project`) и
// #71 (storage_* типов не было вовсе) — те же emitter/catalog↔model mismatch'и.
//
// Класс ПЕРЕЖИЛ снятие внешнего движка отношений и стал строже. Раньше вопрос
// отвергало чужое хранилище своим кодом валидации; теперь его отвергает
// `internal/authzmodel`, компилируя план вывода из той же модели, — то есть
// расхождение каталога с моделью бьёт не на границе с чужим сервисом, а внутри
// собственного решения о доступе.
//
// Гейт data-driven: пары берутся из САМОГО каталога (nlb-часть), не из
// хардкода — новый RPC с новым relation автоматически попадает под проверку.
package check_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// catalogEntry — минимальная проекция строки permission-catalog'а.
type catalogEntry struct {
	FQN              string `json:"fqn"`
	Permission       string `json:"permission"`
	RequiredRelation string `json:"required_relation"`
	ScopeExtractor   struct {
		ObjectType string `json:"object_type"`
	} `json:"scope_extractor"`
}

// repoFile — walk-up резолв файла относительно корня монорепы.
func repoFile(t *testing.T, rel string) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		cand := filepath.Join(dir, rel)
		if _, err := os.Stat(cand); err == nil {
			return cand
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("%s not found walking up from CWD — model/catalog parity gate cannot run", rel)
		}
		dir = parent
	}
}

// modelDSL — модель прав В ТОЙ КОПИИ, ПО КОТОРОЙ ПРИНИМАЕТСЯ РЕШЕНИЕ.
//
// Читалась карта настроек подчарта — блок, который загрузочное задание отправляло
// внешнему хранилищу отношений. Ни хранилища, ни подчарта, ни карты больше нет.
// Свойство гейта от переезда не потерялось, а стало точнее: сверяемся с копией,
// которую служба КОМПИЛИРУЕТ в план вывода (`internal/authzmodel`), а не с той,
// что уезжала наружу.
//
// Каноническую копию (`proto/kaname/cloud/iam/v1/fga_model.fga`) здесь подставлять
// НЕЛЬЗЯ, хотя обе байт-идентичны: их равенство держит отдельная цель сборки
// (`make -C deploy fga-model-embed`), и гейт обязан читать ту, которая
// исполняется, — иначе при расхождении он зеленел бы на неисполняемой.
func modelDSL(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(repoFile(t, "services/iam/internal/authzmodel/fga_model.fga"))
	require.NoError(t, err, "встроенная модель прав не прочитана — гейту не с чем сверять каталог")
	require.NotEmpty(t, strings.TrimSpace(string(raw)), "встроенная модель пуста")
	return string(raw)
}

// modelTypeBody — тело `type <name>` из DSL (до следующего type/condition).
func modelTypeBody(t *testing.T, dsl, typeName string) string {
	t.Helper()
	re := regexp.MustCompile(`(?ms)^type ` + regexp.QuoteMeta(typeName) + `\b.*?(?:\n(?:type |condition )|\z)`)
	body := re.FindString(dsl)
	require.NotEmptyf(t, body, "type %q is not defined in the authorization model", typeName)
	return body
}

// nlbCatalogEntries — строки каталога, принадлежащие домену loadbalancer.
func nlbCatalogEntries(t *testing.T) []catalogEntry {
	t.Helper()
	raw, err := os.ReadFile(repoFile(t, "gateway/internal/middleware/embed/permission_catalog.json"))
	require.NoError(t, err)
	var all []catalogEntry
	require.NoError(t, json.Unmarshal(raw, &all))
	var out []catalogEntry
	for _, e := range all {
		if strings.HasPrefix(e.FQN, "kacho.cloud.loadbalancer.v1.") {
			out = append(out, e)
		}
	}
	require.NotEmpty(t, out, "no loadbalancer entries in permission catalog")
	return out
}

// TestModelParity_EveryRequiredRelationIsDeclared — каждое (object_type,
// required_relation), требуемое nlb-частью каталога, ОБЯЗАНО быть объявлено в
// модели. Необъявленное отношение = вечный fail-closed отказ на этом RPC
// (плана вывода нет, вопрос не принимается), а не «least-priv по умолчанию».
func TestModelParity_EveryRequiredRelationIsDeclared(t *testing.T) {
	dsl := modelDSL(t)
	seen := map[string]bool{}
	for _, e := range nlbCatalogEntries(t) {
		rel := strings.TrimSpace(e.RequiredRelation)
		objType := strings.TrimSpace(e.ScopeExtractor.ObjectType)
		// `<exempt>`-RPC и wildcard-scope без типа проверять нечего.
		if rel == "" || objType == "" || objType == "*" {
			continue
		}
		key := objType + "#" + rel
		if seen[key] {
			continue
		}
		seen[key] = true

		body := modelTypeBody(t, dsl, objType)
		re := regexp.MustCompile(`(?m)^\s*define ` + regexp.QuoteMeta(rel) + `\s*:`)
		require.Truef(t, re.MatchString(body),
			"permission catalog requires relation %q on object type %q (%s), but the "+
				"authorization model never DECLARES it — the verdict source refuses such a "+
				"question outright (no derivation plan exists for the pair), so the RPC is "+
				"denied for EVERY subject forever and "+
				"no tuple/seed can fix it. Declare the relation on the type (and give it its "+
				"legitimate bearer) or remove the RPC together with its catalog entry.\n"+
				"type body:\n%s", rel, objType, e.FQN, body)
	}
	require.NotEmpty(t, seen, "no (object_type, relation) pairs extracted from the nlb catalog")
}

// TestModelParity_AnnounceWriterIsMachineOnlyLeastPriv — least-priv форма
// `announce_writer`: его законный носитель — МАШИННЫЙ принципал data plane
// (announce-state репортит только data plane), поэтому:
//
//   - тип subject'а — ТОЛЬКО `service_account`: ни `user`, ни `group#member`,
//     ни wildcard `user:*`; человеко-/группо-subject физически невозможно
//     записать в такой факт (модель его не допускает) → тенант не получит
//     его ни через роль, ни через группу;
//   - отношение НЕ выводится из tier'а (`or editor`/`or admin`/`or viewer`) —
//     иначе любой project-editor смог бы подделывать announce-state (BGP/route/
//     kernel-programming) чужого балансировщика: инфра-чувствительная запись,
//     не тенантская операция;
//   - и обратно: ни один tier/verb НЕ выводится из `announce_writer` — writer
//     data-plane'а не должен получать CRUD на балансировщик.
func TestModelParity_AnnounceWriterIsMachineOnlyLeastPriv(t *testing.T) {
	body := modelTypeBody(t, modelDSL(t), "nlb_network_load_balancer")

	def := regexp.MustCompile(`(?m)^\s*define announce_writer\s*:(.*)$`).FindStringSubmatch(body)
	require.Lenf(t, def, 2,
		"nlb_network_load_balancer must declare `announce_writer` (ReportAnnounceState gate). body:\n%s", body)
	rhs := strings.TrimSpace(def[1])

	require.Equalf(t, "[service_account]", rhs,
		"announce_writer must be a machine-only DIRECT relation `[service_account]`: the only "+
			"legitimate bearer is the data-plane announcer (a service account). A `user`/`group#member`/"+
			"`user:*` subject type or any `or <tier>` derivation would let a human/tenant principal "+
			"forge announce-state (BGP up/down, route/VRF id, kernel-programming) of a load balancer. got: %q", rhs)

	// Ни один tier/verb не должен выводиться ИЗ announce_writer (обратная утечка).
	leak := regexp.MustCompile(`(?m)^\s*define (?:admin|editor|viewer|v_[a-z]+)\s*:.*\bannounce_writer\b`)
	require.Falsef(t, leak.MatchString(body),
		"no tier/verb relation may derive from `announce_writer` — the data-plane writer must not "+
			"gain CRUD on the load balancer. body:\n%s", body)
}
