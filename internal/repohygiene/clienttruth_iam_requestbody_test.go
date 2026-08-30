// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

func clientTruthIAMRequestBodyOptions(t *testing.T) ClientTruthIAMRequestBodyOptions {
	t.Helper()
	return ClientTruthIAMRequestBodyOptions{
		Tree:         clientTruthRepoTree(t),
		ProtoPackage: "kacho.cloud.iam.v1",
		DocsDirs:     []string{"services/iam/docs/content", "services/iam/docs/engineering"},
		DocExts:      []string{".mdx", ".md"},
		UseCaseDirs:  []string{"services/iam/internal/apps/kacho/api"},
	}
}

// TestClientTruthIAMRequestBodyKeysExistInTheRequestMessage — вердикт о НАСТОЯЩЕМ
// дереве.
//
// Способность падать доказывает не этот прогон, а инъекция
// (`clienttruth_iam_requestbody_injection_test.go`): здесь только вердикт.
func TestClientTruthIAMRequestBodyKeysExistInTheRequestMessage(t *testing.T) {
	var log strings.Builder
	findings, census, err := AuditClientTruthIAMRequestBody(clientTruthIAMRequestBodyOptions(t), &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))

	// Премиса: прочитано то, что заведомо есть.
	if census.Methods < 10 {
		t.Fatalf("методов с телом выведено %d — дескрипторы не прочитаны, судить не по чему",
			census.Methods)
	}
	if census.DocFiles < 20 {
		t.Fatalf("страниц документации %d — обход пуст, вердикт беспредметен", census.DocFiles)
	}
	// Вердикт выносится ТОЛЬКО о сопоставленных телах. Ноль сопоставленных либо
	// ноль рассуженных ключей означал бы, что он не вынесен ни разу.
	if census.BodiesMatched == 0 || census.KeysJudged == 0 {
		t.Fatalf("тел сопоставлено %d, ключей рассужено %d — сверка не состоялась",
			census.BodiesMatched, census.KeysJudged)
	}
	// Второй предикат тоже обязан иметь предмет: ноль невходных полей означал бы,
	// что о них не высказались ни разу.
	if census.RejectedFields == 0 {
		t.Fatal("невходных полей выведено 0 — второй предикат беспредметен")
	}

	if len(findings) == 0 {
		return
	}
	lines := make([]string, 0, len(findings))
	for _, f := range findings {
		lines = append(lines, "  "+f.String())
	}
	t.Errorf("пример запроса в клиентской документации несёт ключ, которого нет в сообщении "+
		"(тел сопоставлено %d, ключей рассужено %d):\n%s\n\n"+
		"Край выбрасывает неизвестное поле МОЛЧА, поэтому клиент получает не отказ на ключе, "+
		"а отказ на другом поле — либо «<поле> is required» о том, что он, по его мнению, "+
		"прислал. Сообщение выводится из дескрипторов контракта; правьте пример, а не список.",
		census.BodiesMatched, census.KeysJudged, strings.Join(lines, "\n"))
}
