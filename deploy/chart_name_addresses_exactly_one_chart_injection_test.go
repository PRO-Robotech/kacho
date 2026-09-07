// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

// chart_name_addresses_exactly_one_chart_injection_test.go — доказательство того,
// что соседний держатель СПОСОБЕН упасть, и падает ровно на своём предмете.
//
// ПОЧЕМУ ОТДЕЛЬНАЯ ПРОБА. Зелёное на целом дереве не говорит о проверке ничего:
// проверка, потерявшая способность краснеть, на целом дереве выглядит точно так
// же. Различает их только внесённый дефект.
//
// ФОРМА. Мир берётся ЦЕЛЫЙ — объявления настоящего дерева, — и каждый случай
// меняет против него РОВНО ОДИН факт. Меняющий два не доказывает ничего:
// неизвестно, который из них дал красное.
//
// КОНТРОЛЬ В ОБРАТНУЮ СТОРОНУ ОБЯЗАТЕЛЕН, и его здесь ДВА: целый мир и чарт без
// тёзки, не объявляющий различия, — законный близнец случая о самоистечении,
// отличающийся от него ровно наличием объявления.

import (
	"strings"
	"testing"
)

// chartNameWorld — целый мир объявлений: два чарта-тёзки и один одиночка.
// Одиночка нужен обоим контролям: он и есть законный близнец самоистечения.
func chartNameWorld() []chartNameDecl {
	return []chartNameDecl{
		{
			Path:         "services/iam/deploy/Chart.yaml",
			Name:         "kaname",
			Distribution: chartDistributionStandalone,
			NameTwin:     "deploy/helm/umbrella/charts/kaname/Chart.yaml",
		},
		{
			Path:         "deploy/helm/umbrella/charts/kaname/Chart.yaml",
			Name:         "kaname",
			Distribution: chartDistributionPlatform,
			NameTwin:     "services/iam/deploy/Chart.yaml",
		},
		{
			Path: "services/vpc/deploy/Chart.yaml",
			Name: "vpc",
		},
	}
}

// chartNameExists — резолвер путей целого мира.
func chartNameExists(world []chartNameDecl) func(string) bool {
	known := map[string]bool{}
	for _, d := range world {
		known[d.Path] = true
	}
	return func(p string) bool { return known[p] }
}

// atPath — указатель на объявление мира по пути. Отказывает, если пути нет:
// молчаливая правка нуля объявлений дала бы случай, который ничего не внёс, — и
// его зелёное читалось бы как доказательство.
func atPath(t *testing.T, world []chartNameDecl, path string) *chartNameDecl {
	t.Helper()
	for i := range world {
		if world[i].Path == path {
			return &world[i]
		}
	}
	t.Fatalf("инъекция беспредметна: в мире нет объявления %q", path)
	return nil
}

func TestChartNameAddressingInjection(t *testing.T) {
	const (
		productChart  = "services/iam/deploy/Chart.yaml"
		platformChart = "deploy/helm/umbrella/charts/kaname/Chart.yaml"
		loneChart     = "services/vpc/deploy/Chart.yaml"
	)

	cases := []struct {
		name string
		// mutate меняет РОВНО ОДИН факт против целого мира.
		mutate func(t *testing.T, world []chartNameDecl)
		// want — по какому признаку узнаём находку. Пусто — ждём молчания.
		want string
		// wantCoordinate — находка обязана называть место.
		wantCoordinate string
	}{
		{
			// ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ. Без него все отрицания ниже зеленели бы на
			// мире, который разбор вообще не читает.
			name:   "целый мир — молчание",
			mutate: func(*testing.T, []chartNameDecl) {},
		},
		{
			// ВТОРОЙ КОНТРОЛЬ и законный близнец самоистечения: чарт без тёзки,
			// не объявляющий различия. Отличается от случая ниже ровно наличием
			// объявления.
			name: "одиночка без объявления — молчание",
			mutate: func(t *testing.T, w []chartNameDecl) {
				atPath(t, w, loneChart).Name = "vpc"
			},
		},
		{
			name: "тёзка не объявляет назначения — находка",
			mutate: func(t *testing.T, w []chartNameDecl) {
				atPath(t, w, platformChart).Distribution = ""
			},
			want:           "НЕ объявляет назначения поставки",
			wantCoordinate: platformChart,
		},
		{
			name: "назначение вне закрытого словаря — находка",
			mutate: func(t *testing.T, w []chartNameDecl) {
				atPath(t, w, platformChart).Distribution = "umbrella"
			},
			want:           "не из закрытого словаря",
			wantCoordinate: platformChart,
		},
		{
			name: "пара имя-назначение не уникальна — находка",
			mutate: func(t *testing.T, w []chartNameDecl) {
				atPath(t, w, platformChart).Distribution = chartDistributionStandalone
			},
			want:           "не уникальна",
			wantCoordinate: platformChart,
		},
		{
			name: "тёзка не назван — находка",
			mutate: func(t *testing.T, w []chartNameDecl) {
				atPath(t, w, productChart).NameTwin = ""
			},
			want:           "чарт-тёзка не назван",
			wantCoordinate: productChart,
		},
		{
			name: "координата тёзки не резолвится — находка",
			mutate: func(t *testing.T, w []chartNameDecl) {
				atPath(t, w, productChart).NameTwin = "deploy/helm/umbrella/charts/iam/Chart.yaml"
			},
			want:           "не резолвится",
			wantCoordinate: productChart,
		},
		{
			name: "тёзкой назван не тёзка — находка",
			mutate: func(t *testing.T, w []chartNameDecl) {
				atPath(t, w, productChart).NameTwin = loneChart
			},
			want:           "назван не тёзка",
			wantCoordinate: productChart,
		},
		{
			name: "взаимность нарушена — находка",
			mutate: func(t *testing.T, w []chartNameDecl) {
				atPath(t, w, platformChart).NameTwin = productChart + ".bak"
			},
			want:           "не резолвится",
			wantCoordinate: platformChart,
		},
		{
			// Найдено ЭТОЙ ЖЕ инъекцией: ссылка на себя проходила взаимность
			// тождественно, и держатель молчал. Случай оставлен как регрессия.
			name: "тёзкой назван сам чарт — находка",
			mutate: func(t *testing.T, w []chartNameDecl) {
				atPath(t, w, platformChart).NameTwin = platformChart
			},
			want:           "называет ЭТОТ ЖЕ чарт",
			wantCoordinate: platformChart,
		},
		{
			// САМОИСТЕЧЕНИЕ: имя стало уникальным, а объявление осталось.
			name: "объявление у чарта без тёзки — находка",
			mutate: func(t *testing.T, w []chartNameDecl) {
				atPath(t, w, loneChart).Distribution = chartDistributionPlatform
			},
			want:           "Объявлению нечего различать",
			wantCoordinate: loneChart,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			world := chartNameWorld()
			tc.mutate(t, world)

			findings, census, err := auditChartNameAddressing(world, chartNameExists(world))
			if err != nil {
				t.Fatalf("обход не состоялся: %v", err)
			}
			all := strings.Join(findings, "\n")

			if tc.want == "" {
				if len(findings) != 0 {
					t.Fatalf("ожидалось молчание, получено находок %d:\n%s\nперепись: %s",
						len(findings), all, census)
				}
				t.Logf("молчание подтверждено · перепись: %s", census)
				return
			}
			if len(findings) == 0 {
				t.Fatalf("ожидалась находка по признаку %q — держатель смолчал на внесённом "+
					"дефекте\nперепись: %s", tc.want, census)
			}
			if !strings.Contains(all, tc.want) {
				t.Fatalf("находка есть, но не та: ждали %q, получили:\n%s", tc.want, all)
			}
			if !strings.Contains(all, tc.wantCoordinate) {
				t.Fatalf("находка не называет координату (%s):\n%s", tc.wantCoordinate, all)
			}
			t.Logf("находка подтверждена: %s", all)
		})
	}
}

// TestChartNameAddressingRefusesOnEmptyTraversal — «ноль находок» обязано быть
// отличимо от «ноль прочитанного». Пустой обход есть ОТКАЗ, а не чистое дерево.
func TestChartNameAddressingRefusesOnEmptyTraversal(t *testing.T) {
	if _, _, err := auditChartNameAddressing(nil, func(string) bool { return true }); err == nil {
		t.Fatalf("пустой обход прошёл молча — значит вердикт держателя не отличает " +
			"чистое дерево от непрочитанного")
	}

	// Второй отказ предпосылки: чарт без имени. Группировать по имени нечем, и
	// молчание означало бы, что предмет группировки исчез незамеченным.
	nameless := []chartNameDecl{{Path: "services/x/deploy/Chart.yaml"}}
	if _, _, err := auditChartNameAddressing(nameless, func(string) bool { return true }); err == nil {
		t.Fatalf("чарт без имени прошёл молча — предпосылка группировки не проверяется")
	}
}
