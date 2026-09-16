// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"errors"
	"strings"
	"testing"
)

// Инъекция в обе стороны: КАЖДАЯ форма утверждения даёт находку с координатой,
// её надгробный близнец молчит, нейтральное упоминание молчит, пустой корпус —
// отказ, а не «находок нет».
func TestRetiredIssuerClaim_InjectionBothWays(t *testing.T) {
	t.Parallel()

	defects := map[string]string{
		"остаётся (stays/remains)":       "# fetches verification keys here (Hydra stays signer/issuer).",
		"является издателем/подписантом": "// Hydra is the token issuer; the edge only verifies.",
		"ключи проверки — его":           "# served kids are Hydra's, ONE-WAY server-TLS to the mirror.",
		"служба не чеканит":              "// it does not mint tokens (Hydra does) and does not decrypt any key.",
		"остаётся (по-русски)":           "# Подписант на стенде один: издатель — Hydra, служба лишь проксирует.",
	}
	for form, line := range defects {
		t.Run("дефект: "+form, func(t *testing.T) {
			t.Parallel()
			corpus := map[string]string{"deploy/helm/umbrella/values.x.yaml": "a: 1\n" + line + "\nb: 2\n"}
			f, census, err := judgeRetiredIssuerClaims(corpus)
			if err != nil {
				t.Fatalf("фикстура обязана судиться: %v", err)
			}
			if len(f) != 1 {
				t.Fatalf("ожидалась ровно одна находка, получено %d: %+v", len(f), f)
			}
			if f[0].File != "deploy/helm/umbrella/values.x.yaml" || f[0].Line != 2 {
				t.Fatalf("находка обязана называть файл и строку: %s:%d", f[0].File, f[0].Line)
			}
			if f[0].Form != form {
				t.Fatalf("форма распознана не та: хотели %q, получили %q", form, f[0].Form)
			}
			if census.Findings != 1 || census.Tombstones != 0 {
				t.Fatalf("перепись расходится с находкой: %s", census)
			}
			if !strings.Contains(f[0].String(), "values.x.yaml:2") {
				t.Fatalf("текст находки обязан нести координату: %s", f[0])
			}
		})
	}

	twins := map[string]string{
		"кавычки-ёлочки":     "# ЗДЕСЬ СТОЯЛО «Hydra stays the SIGNER/ISSUER — only the key-DISTRIBUTION URL",
		"прямые кавычки":     `// It said "Hydra remains the issuer / signer; iam only brokers" — false now.`,
		"маркер без кавычек": "# Здесь стояло Hydra stays issuer/signer, и это противоречило профилю",
		"no longer":          "// Hydra is the token issuer no longer: the key set is ours since F1.",
		"нейтральное имя":    "hydraPublic: kacho-umbrella-hydra-public.kacho.svc.cluster.local:4444",
		"описание полосы":    "// TokenVerifier — port для JWKS-валидации Hydra-issued RS256 access JWT.",
		"проброс консоли":    `location ^~ /.ory/hydra/public/ { proxy_pass http://$hydra_public; }`,
		"чеканит о другом":   "# Operation здесь не чеканится: отказ синхронный.",
	}
	for name, line := range twins {
		t.Run("близнец: "+name, func(t *testing.T) {
			t.Parallel()
			f, census, err := judgeRetiredIssuerClaims(map[string]string{"x.go": line + "\n"})
			if err != nil {
				t.Fatalf("фикстура обязана судиться: %v", err)
			}
			if len(f) != 0 {
				t.Fatalf("законный близнец обязан молчать, получено: %+v", f)
			}
			if census.Findings != 0 {
				t.Fatalf("перепись не сошлась с молчанием: %s", census)
			}
		})
	}

	t.Run("надгробие считается, а не теряется", func(t *testing.T) {
		t.Parallel()
		_, census, err := judgeRetiredIssuerClaims(map[string]string{
			"a.md": "Здесь стояло «Hydra remains the signer» — и это было верно до F1.\n",
		})
		if err != nil {
			t.Fatal(err)
		}
		if census.Claims != 1 || census.Tombstones != 1 || census.Findings != 0 {
			t.Fatalf("надгробие обязано попасть в перепись утверждений и надгробий: %s", census)
		}
	})

	t.Run("пустой корпус — отказ, не «находок нет»", func(t *testing.T) {
		t.Parallel()
		_, _, err := judgeRetiredIssuerClaims(map[string]string{})
		if !errors.Is(err, errRetiredIssuerEmptyCorpus) {
			t.Fatalf("пустой корпус обязан давать отказ обхода, получено: %v", err)
		}
	})

	t.Run("отбор корпуса: пробы, стабы и e2e вне; профили и документация внутри", func(t *testing.T) {
		t.Parallel()
		cases := map[string]bool{
			"gateway/internal/middleware/auth.go":          true,
			"gateway/internal/middleware/auth_test.go":     false,
			"gateway/internal/e2e/dpop_e2e_test.go":        false,
			"pkg/api/kaname/cloud/iam/v1/user.pb.go":       false,
			"tests/authz-fixtures/prodseed_all.py":         false,
			"internal/repohygiene/testdata/x.sh.before":    false,
			"gateway/docs/content/architecture/authn.mdx":  true,
			"deploy/helm/umbrella/values.fe3455-prod.yaml": true,
			"ui-future/package-lock.json":                  false,
			"docs/plans/xc-3/baseline/run.log":             false,
		}
		for rel, want := range cases {
			if got := retiredIssuerProseFile(rel); got != want {
				t.Errorf("%s: хотели %v, получили %v", rel, want, got)
			}
		}
	})
}
