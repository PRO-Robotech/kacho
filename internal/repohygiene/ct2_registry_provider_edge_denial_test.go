// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/treecorpus"
)

// providerEdgeClaimCorpusDir — где живут исходники провайдера.
const providerEdgeClaimCorpusDir = "terraform/internal/provider"

// registryContractDir — контракт, относительно которого резолвятся утверждения.
const registryContractDir = "proto/kacho/cloud/registry/v1"

// rpcDeclRe — объявление RPC в контракте. Судится объявление, а не упоминание:
// имя глагола встречается и в комментариях, и гейт по подстроке считал бы
// существующим то, о чём контракт только рассуждает.
var rpcDeclRe = regexp.MustCompile(`(?m)^\s*rpc\s+([A-Za-z][A-Za-z0-9]*)\s*\(`)

// TestProviderDoesNotMisstateTheEdgeVerbs — провайдер не отрицает глагол, который
// в контракте есть, и не утверждает глагол, которого нет.
//
// Предмет, механика и границы — в шапке ct2_registry_provider_edge_denial.go;
// здесь они не пересказываются, иначе разойдутся.
func TestProviderDoesNotMisstateTheEdgeVerbs(t *testing.T) {
	root := repoRoot(t)

	verbs := registryContractVerbs(t, root)
	if len(verbs) == 0 {
		t.Fatal("глаголов контракта прочитано НОЛЬ — сверять не с чем, вердикт беспредметен")
	}
	if missing, ok := EdgeClaimPremiseHolds(verbs); !ok {
		t.Fatalf("предпосылка гейта отпала: словарь резолвит %s, а таких глаголов в контракте "+
			"больше НЕТ. Отрицания стали правдой — их надо перечитать, а не оставить под "+
			"мёртвым запретом (снять запись словаря либо перевести гейт на живой глагол)", missing)
	}

	sources := providerSourcesTouchingRegistry(t, root)
	if len(sources) == 0 {
		t.Fatal("файлов провайдера, ссылающихся на контракт registry, прочитано НОЛЬ — " +
			"обход пуст, и «ноль находок» здесь означает «ноль прочитанного»")
	}

	findings, census, err := ScanProviderEdgeClaims(sources, verbs)
	if err != nil {
		t.Fatalf("разбор корпуса: %v", err)
	}
	t.Log(census.String())
	if census.Sentences == 0 {
		t.Fatal("предложений прочитано НОЛЬ — читать было нечего")
	}

	for _, f := range findings {
		t.Error(f.String())
	}
}

// registryContractVerbs — имена RPC контракта registry, выведенные из дерева.
func registryContractVerbs(t *testing.T, root string) map[string]bool {
	t.Helper()
	files, err := treecorpus.UnderWithSuffix(filepath.Join(root, registryContractDir), ".proto")
	if err != nil {
		t.Fatalf("обход контракта: %v", err)
	}
	verbs := map[string]bool{}
	for _, f := range files {
		b, rerr := os.ReadFile(f)
		if rerr != nil {
			t.Fatalf("чтение %s: %v", f, rerr)
		}
		for _, m := range rpcDeclRe.FindAllStringSubmatch(string(b), -1) {
			verbs[m[1]] = true
		}
	}
	return verbs
}

// providerSourcesTouchingRegistry — прод-исходники провайдера, ссылающиеся на
// контракт registry.
//
// Корпус выводится по ПРИЗНАКУ ССЫЛКИ, а не по имени файла: имя — соглашение, и
// первый же файл, названный иначе, ушёл бы из наблюдения молча.
func providerSourcesTouchingRegistry(t *testing.T, root string) map[string]string {
	t.Helper()
	files, err := treecorpus.UnderWithSuffix(filepath.Join(root, providerEdgeClaimCorpusDir), ".go")
	if err != nil {
		t.Fatalf("обход провайдера: %v", err)
	}
	out := map[string]string{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, rerr := os.ReadFile(f)
		if rerr != nil {
			t.Fatalf("чтение %s: %v", f, rerr)
		}
		src := string(b)
		if !strings.Contains(src, "registryv1") && !strings.Contains(src, "/registry/v1/") {
			continue
		}
		rel, relErr := filepath.Rel(root, f)
		if relErr != nil {
			rel = f
		}
		out[rel] = src
	}
	return out
}
