// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// peerlanecanon_injection_test.go — доказательство, что гейт полосы geo
// СПОСОБЕН упасть, и что падает он на ТОКЕНЕ полосы, а не на факте
// использования закрытого типа.
//
// # Почему инъекция кладётся поверх ЦЕЛОГО дерева, а не одиночного файла
//
// Инъекция обязана ронять ТОЛЬКО проверяемое. Проба из одного сломанного файла
// этого не показывает: красное могло прийти от соседнего требования, и новое
// различение оказалось бы вакуумным, не выдав себя ничем. Поэтому каждая проба
// собирает КОНТРОЛЬНОЕ дерево (все действующие требования выполнены, находок
// ноль) и кладёт поверх него ровно одну поломку, после чего утверждается
// ТОЧНЫЙ набор вердиктов — не «покраснело», а «покраснело вот это и только оно».
//
// Отсюда три прогона, а не два: контроль · инъекция нового различения ·
// инъекция существующего требования. Без третьего молчание существующего
// контроля неотличимо от молчания мёртвого.
//
// # Что здесь считается «настоящим входом»
//
// Тела проб взяты дословно с форм, живущих в дереве: полоса прямого чтения
// владельца (services/geo/.../serviceerr/lanes.go), обёртка sentinel'а в его
// репозитории (services/geo/.../pg/zone.go) и эмиссия полосы у консумера
// (services/vpc/internal/clients). Отрицательные стороны получены из них сменой
// ОДНОГО решения — токена полосы либо способа сборки.
package repohygiene

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// Пути контрольного дерева. Предикат гейта ключуется на путь (сторона ребра
// определяется каталогом владельца), поэтому пробы кладутся по НАСТОЯЩИМ
// координатам, а не по выдуманным.
const (
	ownerLanePath     = "services/geo/internal/apps/kacho/shared/serviceerr/lanes.go"
	ownerRepoPath     = "services/geo/internal/repo/kacho/pg/zone.go"
	consumerLanePath  = "services/vpc/internal/clients/geo_client.go"
	ownerClientPath   = "services/geo/internal/clients/iam_client.go"
	injectionPrologue = "package fixture\n\nimport (\n\t\"fmt\"\n\n\tgeoerrors \"github.com/PRO-Robotech/kacho/services/geo/internal/errors\"\n\tkerrors \"github.com/PRO-Robotech/kacho/pkg/errors\"\n\t\"google.golang.org/grpc/codes\"\n\t\"google.golang.org/grpc/status\"\n)\n\nvar _ = fmt.Sprint\nvar _ = codes.OK\nvar _ = status.New\nvar _ = geoerrors.ErrNotFound\n\n"
)

// laneCanonBaseline — дерево, на котором действующие требования выполнены ВСЕ.
//
// Три формы разом, потому что гейт утверждает обе стороны ребра: владелец
// выражает свою полосу прямого чтения закрытым типом, он же обёртывает
// контракт-тон sentinel'ом отсутствия в репозитории, консумер эмитирует полосу
// peer-validate закрытым типом.
func laneCanonBaseline() map[string]string {
	return map[string]string{
		ownerLanePath: injectionPrologue + `func notFoundLane(resourceType, display, id string) error {
	return kerrors.ReasonResourceNotFound.Errf(
		kerrors.PeerRef{Service: "geo", ResourceType: resourceType, ResourceID: id},
		"%s %s not found", display, id)
}
`,
		ownerRepoPath: injectionPrologue + `func getZone(id string) error {
	return fmt.Errorf("%w: Zone %s not found", geoerrors.ErrNotFound, id)
}
`,
		consumerLanePath: injectionPrologue + `func zoneLane(id string) error {
	return kerrors.ReasonPeerResourceMissing.Errf(
		kerrors.PeerRef{Service: "vpc", ResourceType: "geo.zone", ResourceID: id},
		"unknown zone id %s", id)
}
`,
	}
}

// laneInjection — одна проба: чем контрольное дерево дополняется или
// подменяется и какой ТОЧНЫЙ набор вердиктов обязан получиться.
type laneInjection struct {
	name string
	// overlay — файлы поверх контрольного дерева. Совпадающий путь подменяет.
	overlay map[string]string
	// want — вердикты, ожидаемые от гейта, в порядке возрастания кода.
	want []laneVerdict
	// wantFile — координата, которую гейт обязан назвать (пусто для контроля).
	wantFile string
	// origin — откуда взята форма; без него проба через полгода нечитаема.
	origin string
}

func TestPeerLaneCanonJudgesTheTokenNotTheForm(t *testing.T) {
	cases := []laneInjection{
		{
			name:   "контроль: все действующие требования выполнены",
			origin: "три формы, живущие в дереве после задачи #1419",
			want:   nil,
		},
		{
			name: "НОВОЕ различение: владелец отвечает полосой промаха peer-validate",
			origin: "форма получена из полосы владельца сменой ОДНОГО решения — токена: " +
				"о своей строке сказано «предусловие на ЧУЖОЙ ресурс не выполнено»",
			overlay: map[string]string{
				ownerLanePath: injectionPrologue + `func notFoundLane(resourceType, display, id string) error {
	return kerrors.ReasonPeerResourceMissing.Errf(
		kerrors.PeerRef{Service: "geo", ResourceType: resourceType, ResourceID: id},
		"%s %s not found", display, id)
}
`,
			},
			want:     []laneVerdict{verdictOwnerPeerLane},
			wantFile: ownerLanePath,
		},
		{
			name:   "НОВОЕ различение: владелец отвечает полосой состояния peer-validate",
			origin: "вторая полоса того же набора — гейт обязан видеть её наравне с первой",
			overlay: map[string]string{
				ownerLanePath: injectionPrologue + `func stateLane(id string) error {
	return kerrors.ReasonPeerResourceState.Errf(
		kerrors.PeerRef{Service: "geo", ResourceType: "geo.zone", ResourceID: id},
		"Zone %s not found", id)
}
`,
			},
			want:     []laneVerdict{verdictOwnerPeerLane},
			wantFile: ownerLanePath,
		},
		{
			name:   "НОВОЕ различение: владелец отвечает полосой недоступности peer-validate",
			origin: "третья полоса того же набора; недоступность СВОЕЙ БД полосой резолва ссылки не выражается",
			overlay: map[string]string{
				ownerLanePath: injectionPrologue + `func unavailableLane(id string) error {
	return kerrors.ReasonPeerUnavailable.Errf(
		kerrors.PeerRef{Service: "geo", ResourceType: "geo.zone", ResourceID: id},
		"Zone %s not found", id)
}
`,
			},
			want:     []laneVerdict{verdictOwnerPeerLane},
			wantFile: ownerLanePath,
		},
		{
			name: "НОВОЕ различение: у владельца полоса вне словаря — судить сторону нечем",
			origin: "формы в дереве нет; она запрещена ВПЕРЁД: молчание на неопознанной полосе " +
				"было бы fail-open — она может оказаться и peer-validate",
			overlay: map[string]string{
				ownerLanePath: injectionPrologue + `func sixthLane(id string) error {
	return kerrors.ReasonSixthLane.Errf(
		kerrors.PeerRef{Service: "geo", ResourceType: "geo.zone", ResourceID: id},
		"Zone %s not found", id)
}
`,
			},
			want:     []laneVerdict{verdictOwnerUnknownLane},
			wantFile: ownerLanePath,
		},
		{
			name: "законный близнец: владелец отвечает own-полосой формата",
			origin: "вторая own-полоса словаря; гейт обязан судить СТОРОНУ, а не один вшитый токен — " +
				"иначе он пропускал бы ровно одну законную форму из двух",
			overlay: map[string]string{
				ownerLanePath: injectionPrologue + `func malformedLane(id string) error {
	return kerrors.ReasonInvalidResourceID.Errf(
		kerrors.PeerRef{Service: "geo", ResourceType: "geo.zone", ResourceID: id},
		"invalid zone id '%s'", id)
}
`,
			},
			want: nil,
		},
		{
			name: "СУЩЕСТВУЮЩЕЕ требование: у владельца полоса не выражена ничем",
			origin: "форма получена из репозитория владельца снятием обёртки sentinel'а — " +
				"ответ о своей строке стал зависеть от того, куда ошибка попадёт дальше",
			overlay: map[string]string{
				ownerRepoPath: injectionPrologue + `func getZone(id string) error {
	return fmt.Errorf("Zone %s not found", id)
}
`,
			},
			want:     []laneVerdict{verdictOwnerLaneUnexpressed},
			wantFile: ownerRepoPath,
		},
		{
			name: "СУЩЕСТВУЮЩЕЕ требование: консумер собрал статус в обход закрытого типа",
			origin: "дословно форма vpc/storage до перехода на закрытый тип: код выписан руками " +
				"и с машинным признаком разъезжается молча",
			overlay: map[string]string{
				consumerLanePath: injectionPrologue + `func zoneLane(id string) error {
	return status.Errorf(codes.FailedPrecondition, "unknown zone id '%s'", id)
}
`,
			},
			want:     []laneVerdict{verdictConsumerHandRolled},
			wantFile: consumerLanePath,
		},
	}

	var controls, injections int
	for _, tc := range cases {
		if len(tc.want) == 0 {
			controls++
		} else {
			injections++
		}
		t.Run(tc.name, func(t *testing.T) {
			root := laneFixtureTree(t, tc.overlay)
			sites, filesRead := collectLaneSites(t, root, prodRoots)
			findings, tally := judgeLaneSites(sites)

			// Предпосылка пробы: обход что-то прочитал и обе стороны ребра
			// представлены. Иначе «находок ноль» означало бы «осмотрено ноль».
			if filesRead == 0 {
				t.Fatalf("предпосылка пробы не выполнена: обход не прочитал ни одного файла")
			}
			if tally.ownerTotal == 0 || tally.consumerViaType+tally.consumerHandRolled == 0 {
				t.Fatalf("предпосылка пробы не выполнена: сторона владельца %d, сторона консумера %d —\n"+
					"    проба судила бы дерево, в котором предмета нет",
					tally.ownerTotal, tally.consumerViaType+tally.consumerHandRolled)
			}

			got := make([]laneVerdict, 0, len(findings))
			for _, f := range findings {
				got = append(got, f.verdict)
			}
			sort.Slice(got, func(i, j int) bool { return got[i] < got[j] })

			if len(got) != len(tc.want) {
				t.Fatalf("гейт вынес %d вердиктов, ожидалось %d (%s).\n"+
					"    Получено: %+v", len(got), len(tc.want), tc.origin, findings)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("вердикт %d: гейт сказал %d, ожидалось %d (%s)",
						i, got[i], tc.want[i], tc.origin)
				}
			}
			if tc.wantFile == "" {
				return
			}
			for _, f := range findings {
				if filepath.ToSlash(f.file) != tc.wantFile {
					t.Fatalf("гейт назвал не ту координату: %s вместо %s", f.file, tc.wantFile)
				}
				if f.line == 0 {
					t.Fatalf("гейт назвал находку без строки — по такой находке дефект не найти")
				}
				if !laneMessageNamesTheSite(f) {
					t.Fatalf("текст находки не называет координату (%s:%d):\n%s",
						f.file, f.line, f.message)
				}
			}
		})
	}

	t.Logf("перепись: проб %d (контролей %d, инъекций %d)", len(cases), controls, injections)
}

// Доказательство, что ПРЕДПОСЫЛКА гейта проверяется, а не объявлена.
//
// Предпосылка — «сторону ребра называет путь» — верна ровно пока владелец не
// консумер. Проба ставит обе стороны: раскладка владельца, какая она есть
// (адаптера нет — молчание), и та же раскладка с заведённым адаптером к соседу
// (находка с координатой). Без второй половины проверка предпосылки была бы
// объявлением: на сегодняшнем дереве она молчит, и молчание мёртвой неотличимо
// от молчания исправной.
func TestOwnerSidePremiseGateFiresWhenTheOwnerBecomesAConsumer(t *testing.T) {
	cases := []struct {
		name         string
		overlay      map[string]string
		wantAdapters []string
		origin       string
	}{
		{
			name:   "законный близнец: у владельца адаптера к соседу нет",
			origin: "раскладка geo, какая она есть: use-case и репозиторий, каталога адаптеров нет",
		},
		{
			name: "инъекция: владелец завёл адаптер к соседу — путь больше не различает сторону",
			origin: "форма адаптера нормативна (`architecture.md`): клиент к соседу лежит " +
				"в internal/clients/, и другого дома у него нет",
			overlay: map[string]string{
				ownerClientPath: injectionPrologue + `func getProject(id string) error {
	return fmt.Errorf("project %s not found", id)
}
`,
			},
			wantAdapters: []string{ownerClientPath},
		},
	}

	var controls, injections int
	for _, tc := range cases {
		if len(tc.wantAdapters) == 0 {
			controls++
		} else {
			injections++
		}
		t.Run(tc.name, func(t *testing.T) {
			root := laneFixtureTree(t, tc.overlay)
			adapters, filesRead := collectGeoOwnerAdapters(t, root)

			if filesRead == 0 {
				t.Fatalf("предпосылка пробы не выполнена: в каталоге владельца прочитано ноль\n" +
					"    файлов — проба судила бы дерево, в котором предмета нет")
			}
			if len(adapters) != len(tc.wantAdapters) {
				t.Fatalf("гейт назвал %d адаптеров, ожидалось %d (%s).\n    Получено: %v",
					len(adapters), len(tc.wantAdapters), tc.origin, adapters)
			}
			for i := range adapters {
				if adapters[i] != tc.wantAdapters[i] {
					t.Fatalf("гейт назвал координату %q, ожидалась %q (%s)",
						adapters[i], tc.wantAdapters[i], tc.origin)
				}
			}
		})
	}

	t.Logf("перепись: проб %d (контролей %d, инъекций %d)", len(cases), controls, injections)
}

// laneMessageNamesTheSite — текст находки обязан НАЗЫВАТЬ место, а не только
// описывать беду. Находка, называющая симптом вместо координаты, посылает
// читателя искать не там: на неё тратят прогон, а потом снимают гейт как
// непонятный.
func laneMessageNamesTheSite(f laneFinding) bool {
	want := f.file + ":"
	return len(f.message) >= len(want) && f.message[:len(want)] == want
}

// laneFixtureTree раскладывает контрольное дерево с наложенным overlay.
//
// Каталоги ВСЕХ корней обхода создаются, даже пустые: обход отказывает на
// несуществующем каталоге, и без них проба падала бы на своей раскладке, а не
// на предмете.
func laneFixtureTree(t *testing.T, overlay map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for _, sub := range prodRoots {
		if err := os.MkdirAll(filepath.Join(root, sub), 0o750); err != nil {
			t.Fatalf("подготовка пробы: %v", err)
		}
	}
	files := laneCanonBaseline()
	for path, body := range overlay {
		files[path] = body
	}
	for path, body := range files {
		abs := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
			t.Fatalf("подготовка пробы: %v", err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o600); err != nil {
			t.Fatalf("подготовка пробы: %v", err)
		}
	}
	return root
}
