// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// peerlaneclassifier_injection_test.go — доказательство, что гейт полосы СПОСОБЕН
// упасть, и что падает он на существе, а не на форме.
//
// Инъекция ведётся В ОБЕ СТОРОНЫ и настоящим входом:
//
//   - отрицательная — код, ДОСЛОВНО взятый из дерева ДО этого перехода (провенанс
//     назван у каждой пробы). Гейт обязан покраснеть и назвать координату;
//   - положительная — законный близнец той же формы. Гейт обязан смолчать.
//     Без него гейт ловил бы слово `status`, а не разбор ответа соседа, и первое
//     же ложное срабатывание сняли бы вместе с гейтом.
//
// Отдельно проверяется предпосылка объёма: положительная сторона читает НАСТОЯЩЕЕ
// дерево, а не копию, — иначе «молчит» означало бы лишь «в фикстуре ничего нет».
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// injectionCase — одна проба. `body` — тело файла, `path` — координата, по которой
// он кладётся во временное дерево (предикат гейта ключуется на путь).
type injectionCase struct {
	name     string
	path     string
	body     string
	wantFind bool
	origin   string
}

const injectionHeader = "package clients\n\nimport (\n\t\"google.golang.org/grpc/codes\"\n\t\"google.golang.org/grpc/status\"\n\n\t\"github.com/PRO-Robotech/kacho/pkg/peer\"\n)\n\nvar _ = codes.OK\nvar _ = peer.OutcomeOK\n\n"

func TestPeerLaneGateFiresOnTheStateThisChangeFixed(t *testing.T) {
	cases := []injectionCase{
		{
			name: "инъекция: разбор через status.FromError + st.Code() (форма vpc/compute/nlb)",
			path: "services/vpc/internal/clients/geo_client.go",
			origin: "дословно из services/vpc/internal/clients/geo_client.go до перехода: " +
				"распознавался ОДИН код, отказ в правах уходил сырым ответом соседа",
			body: injectionHeader + `func mapZone(rerr error) error {
	if st, ok := status.FromError(rerr); ok && st.Code() == codes.NotFound {
		return errNotFound
	}
	return rerr
}

var errNotFound = status.Error(codes.NotFound, "x")
`,
			wantFind: true,
		},
		{
			name: "инъекция: разбор через status.Code(err) (форма storage)",
			path: "services/storage/internal/clients/geo_client.go",
			origin: "дословно из services/storage/internal/clients/geo_client.go до перехода: " +
				"ветка default объявляла отказ в правах НЕДОСТУПНОСТЬЮ, то есть временной полосой",
			body: injectionHeader + `func geoLane(err error, id string) error {
	switch status.Code(err) {
	case codes.NotFound, codes.InvalidArgument:
		return status.Error(codes.FailedPrecondition, "unknown zone id '"+id+"'")
	default:
		return status.Error(codes.Unavailable, "geo zone validation unavailable")
	}
}
`,
			wantFind: true,
		},
		{
			name: "инъекция: третья дверь — status.Convert(err).Code()",
			path: "services/compute/internal/clients/vpc_subnet_client.go",
			origin: "формы в дереве нет; она запрещена ВПЕРЁД — гейт обязан видеть и её, " +
				"иначе следующий разбор просто сменит вызов",
			body: injectionHeader + `func mapSubnet(err error) error {
	if status.Convert(err).Code() == codes.NotFound {
		return errAbsent
	}
	return err
}

var errAbsent = status.Error(codes.NotFound, "x")
`,
			wantFind: true,
		},
		{
			name:   "законный близнец: решение принимает носитель",
			path:   "services/vpc/internal/clients/geo_client.go",
			origin: "текущая форма после перехода",
			body: injectionHeader + `func mapZone(rerr error) error {
	if peer.Classify(rerr).RefusedReference() {
		return errNotFound
	}
	return rerr
}

var errNotFound = status.Error(codes.NotFound, "x")
`,
			wantFind: false,
		},
		{
			name:   "законный близнец: диагностика через дверь носителя",
			path:   "services/registry/internal/clients/iam/iam_client.go",
			origin: "текущая форма: код соседа берётся у носителя и уходит В ЖУРНАЛ, не в решение",
			body: injectionHeader + `func logUnexpected(err error) string {
	return peer.PeerCode(err).String() + ": " + peer.PeerMessage(err)
}
`,
			wantFind: false,
		},
		{
			name:   "законный близнец: тот же разбор ВНЕ клиента к соседу",
			path:   "services/vpc/internal/handler/network_handler.go",
			origin: "разбор собственного ответа в транспортном слое — не полоса соседа",
			body: injectionHeader + `func ownCode(err error) bool {
	return status.Code(err) == codes.NotFound
}
`,
			wantFind: false,
		},
		{
			name:   "законный близнец: адаптер к ВНЕШНЕЙ системе",
			path:   "services/iam/internal/clients/openfga_write.go",
			origin: "чужой протокол и чужие коды — словарь из пяти полос к ним неприменим",
			body: injectionHeader + `func mapFGA(err error) error {
	if st, ok := status.FromError(err); ok && st.Code() == codes.Aborted {
		return err
	}
	return err
}
`,
			wantFind: false,
		},
	}

	var injections, twins int
	for _, tc := range cases {
		if tc.wantFind {
			injections++
		} else {
			twins++
		}
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			abs := filepath.Join(root, filepath.FromSlash(tc.path))
			if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
				t.Fatalf("подготовка пробы: %v", err)
			}
			if err := os.WriteFile(abs, []byte(tc.body), 0o600); err != nil {
				t.Fatalf("подготовка пробы: %v", err)
			}

			sites, filesRead, _ := collectPeerCodeReads(t, root)
			if filesRead == 0 {
				t.Fatalf("предпосылка пробы не выполнена: обход не прочитал ни одного файла")
			}

			if !tc.wantFind {
				if len(sites) != 0 {
					t.Fatalf("гейт покраснел на ЗАКОННОЙ форме (%s): %+v.\n"+
						"    Он ловит форму, а не существо, и первый же ложный срабат снимет его вместе\n"+
						"    с запретом.", tc.origin, sites)
				}
				return
			}
			if len(sites) == 0 {
				t.Fatalf("гейт смолчал на возвращённом дефекте (%s) — он не способен упасть", tc.origin)
			}
			for _, s := range sites {
				if s.file != tc.path {
					t.Fatalf("гейт назвал не ту координату: %s вместо %s", s.file, tc.path)
				}
				if s.line == 0 {
					t.Fatalf("гейт назвал находку без строки — по такой находке дефект не найти")
				}
			}
		})
	}

	// Объём осмотренного: «ноль находок» обязано быть отличимо от «ноль прочитанного».
	t.Logf("перепись: проб %d (инъекций %d, законных близнецов %d)", len(cases), injections, twins)
}

// Положительная сторона на НАСТОЯЩЕМ дереве: файлы, переведённые этим переходом,
// существуют и разбора по коду не несут.
//
// Проба нужна потому, что предыдущая работает на копиях: «молчит» там могло бы
// означать «в фикстуре ничего нет». Здесь молчание относится к дереву, и перепись
// названа числом.
func TestConvertedClientsCarryNoHandRolledLaneRead(t *testing.T) {
	root := repoRoot(t)
	converted := []string{
		"services/storage/internal/clients/geo_client.go",
		"services/storage/internal/clients/iam_client.go",
		"services/vpc/internal/clients/geo_client.go",
		"services/vpc/internal/clients/geo_region_client.go",
		"services/vpc/internal/clients/iam_client.go",
		"services/vpc/internal/clients/project_cache.go",
		"services/compute/internal/clients/geo_client.go",
		"services/compute/internal/clients/iam_client.go",
		"services/compute/internal/clients/vpc_subnet_client.go",
		"services/registry/internal/clients/geo/region_client.go",
		"services/registry/internal/clients/iam/iam_client.go",
		"services/nlb/internal/clients/geo/region_client.go",
		"services/nlb/internal/clients/geo/zone_client.go",
		"services/nlb/internal/clients/geo/zone_region_client.go",
		"services/nlb/internal/clients/iam/project_client.go",
		"services/nlb/internal/clients/iam/check_client.go",
		"services/nlb/internal/clients/compute/instance_client.go",
		"services/nlb/internal/clients/vpc/subnet_client.go",
		"services/nlb/internal/clients/vpc/address_client.go",
		"services/nlb/internal/clients/vpc/security_group_client.go",
		"services/nlb/internal/clients/vpc/network_interface_client.go",
	}

	sites, _, _ := collectPeerCodeReads(t, root)
	dirty := map[string]int{}
	for _, s := range sites {
		dirty[s.file]++
	}

	var carriers int
	for _, f := range converted {
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(f)))
		if err != nil {
			t.Errorf("%s: файл перечислен как переведённый, но не читается: %v —\n"+
				"    перечень пережил своё дерево", f, err)
			continue
		}
		if n := dirty[f]; n != 0 {
			t.Errorf("%s: переведённый клиент снова разбирает ответ по коду (%d мест)", f, n)
		}
		if !strings.Contains(string(body), "pkg/peer") {
			t.Errorf("%s: переведённый клиент не импортирует носитель — либо перевод откачен,\n"+
				"    либо запись перечня пережила свой предмет", f)
			continue
		}
		carriers++
	}
	var amongConverted int
	for _, f := range converted {
		amongConverted += dirty[f]
	}
	t.Logf("перепись: переведённых клиентов %d, из них ходят через носитель %d, "+
		"мест разбора по коду среди них %d (по всему дереву клиентов — %d)",
		len(converted), carriers, amongConverted, len(sites))
}
