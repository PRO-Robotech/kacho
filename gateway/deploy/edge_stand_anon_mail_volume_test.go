// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// edge_stand_anon_mail_volume_test.go — пороги ограничителя анонимной почты на
// цепочке, которой поднимается стенд, пропускают объём посева стенда без
// ступени лестницы края (приёмка NTF-2 §5 «Счётчики края на П1»; kacho#2917).
//
// # Чего не хватало
//
// Посев стенда (`tests/authz-fixtures/prodseed_matrix.py`) заводит людей
// регистрацией через край — тем путём, что консоль, — и все запросы прогона
// идут к краю с одного источника, источника раннера. Цепочка стенда брала
// пороги из базы чарта края — ориентиров поставки (без проверки 3 за 15 мин),
// и четвёртая регистрация посева получала `429 proof of work required`: посев
// падал на каждом шарде сквозных проб, суиты не запускались.
//
// Приёмка требует обратного: стенд объявляет ручки края так, что объём `V`
// анонимных почтовых запросов прогона меньше порога «без проверки» `FREE`, а
// `V + FREE` меньше жёсткого порога источника и порогов подсети; ступень
// лестницы утверждают только кейсы, которые строят её сами.
//
// # Что судится
//
//	(1) цепочка стенда — та, что называет `make -C deploy print-stand-stack`
//	    (единственная запись `STAND_STACK`, её же спрашивает конвейер);
//	(2) пороги берутся из рендера этой цепочки и разбираются САМИМ стражем
//	    старта края (`config.ResolveEdgeLimits`), а не перечитываются из values;
//	(3) `V` посева — число мест заведения человека в посеве, выведенное из
//	    дерева; ноль мест — обход ослеп, а не «объём пуст»;
//	(4) судья печатает обе величины и падает, называя нарушенное неравенство;
//	    его способность упасть держит пара «на единицу ниже — молчит, на
//	    пороге — находка»;
//	(5) близнец: послабление не выходит за стенд — каждая цепочка таблицы,
//	    кроме `STAND_STACK`, несёт пределы базы чарта края. Цепочки площадок с
//	    внешним ретранслятором наследуют средний слой стенда разработки, поэтому
//	    послабление лежит в слое стенда прогона (`values.run-stand.yaml`),
//	    который называет только цепочка стенда.
//
// # Чего эта проба НЕ судит (остаток, названный прямо)
//
// `V` здесь — только посев. Объём консольных проб и кейсов NTF2-72, NTF2-63
// приёмка поручает пробе прогона П1: она считает запросы по перечню кейсов
// прогона и печатает исход «не выполнилось», а не красный.
package deploy_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

// standSeedFile — посев стенда, заводящий людей регистрацией через край.
const standSeedFile = "tests/authz-fixtures/prodseed_matrix.py"

// seedHumanCall — место заведения человека посевом: вызов `human(` с адресом,
// но не его определение. Адрес — строковый литерал (обычный либо f-строка).
var seedHumanCall = regexp.MustCompile(`(?m)^[^#\n]*[^\w.]human\(f?["']`)

// standStackName — имя цепочки стенда у единственной записи `STAND_STACK`.
func standStackName(t *testing.T) string {
	t.Helper()
	cmd := exec.Command("make", "-s", "-C", filepath.Join(repoRootFromDeploy(t), "deploy"),
		"--no-print-directory", "print-stand-stack")
	out, err := cmd.Output()
	name := strings.TrimSpace(string(out))
	if err != nil || name == "" {
		t.Fatalf("цепочка стенда не прочитана из deploy/Makefile (STAND_STACK): err=%v, вывод=%q — "+
			"чей рендер судить, неизвестно", err, name)
	}
	return name
}

// seedVolume — число регистраций, которые посев стенда шлёт краю за прогон.
func seedVolume(t *testing.T) int {
	t.Helper()
	path := filepath.Join(repoRootFromDeploy(t), standSeedFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("посев стенда не читается (%v) — объём не измерен", err)
	}
	n := len(seedHumanCall.FindAllIndex(raw, -1))
	if n == 0 {
		t.Fatalf("в %s не найдено ни одного места заведения человека — распознаватель ослеп "+
			"либо посев сменил форму; «объём ноль» здесь не вывод", standSeedFile)
	}
	return n
}

// judgeStandVolume — находки: пороги цепочки не пропускают объём v без ступени.
func judgeStandVolume(label string, v int, l config.AnonMailLimits) []string {
	var out []string
	if v >= l.Source.Free {
		out = append(out, fmt.Sprintf("%s: V=%d не меньше порога «без проверки» FREE=%d — "+
			"регистрация посева номер %d получит вызов proof-of-work", label, v, l.Source.Free, l.Source.Free+1))
	}
	if v+l.Source.Free >= l.Source.Hard {
		out = append(out, fmt.Sprintf("%s: V+FREE=%d не меньше жёсткого порога источника HARD=%d",
			label, v+l.Source.Free, l.Source.Hard))
	}
	for _, s := range []struct {
		name string
		lim  config.AnonMailSubnetLimits
	}{{"/24", l.SubnetV4Len24}, {"/56", l.SubnetV6Len56}, {"/48", l.SubnetV6Len48}} {
		if v+l.Source.Free >= s.lim.PoW {
			out = append(out, fmt.Sprintf("%s: V+FREE=%d не меньше порога PoW подсети %s = %d",
				label, v+l.Source.Free, s.name, s.lim.PoW))
		}
	}
	return out
}

// chainAnonMailLimits — пределы ограничителя, которые страж старта края
// разбирает из окружения пода, отрендеренного цепочкой c (chain == nil — чарт
// края как есть, его база).
func chainAnonMailLimits(t *testing.T, c edgeAlertChain) config.AnonMailLimits {
	t.Helper()
	out, err := renderEdgeChain(t, c)
	if err != nil {
		t.Fatalf("цепочка %s: рендер не выполнен (%v) — условие не создано, вердикта нет:\n%s", c.name, err, out)
	}
	got, found := readEdgeContainer(t, out)
	if !found {
		t.Fatalf("цепочка %s: в рендере нет пода края — смотреть было не на что", c.name)
	}
	limits, err := config.ResolveEdgeLimits(configFromEnv(got.env))
	if err != nil {
		t.Fatalf("цепочка %s: страж старта края отверг окружение пода: %v", c.name, err)
	}
	return limits.AnonMail
}

// judgeOffStand — находка: цепочка, которой поднимается не стенд прогона,
// несёт пределы ограничителя, отличные от базы чарта края. Послабление
// приёмка разрешает только стенду П1; на площадке, доступной снаружи, оно
// ослабило бы рубеж против рассылки с одного источника молча.
func judgeOffStand(label string, base, got config.AnonMailLimits) []string {
	if got == base {
		return nil
	}
	return []string{fmt.Sprintf("%s: пределы ограничителя анонимной почты %+v, а база чарта края %+v — "+
		"послабление стенда прогона протекло на цепочку, которой стенд прогона не поднимается",
		label, got, base)}
}

func TestEdgeStandChain_AnonMailThresholdsAdmitTheSeedVolume(t *testing.T) {
	name := standStackName(t)
	chain, ok := deployableStacks(t)[name]
	if !ok {
		t.Fatalf("цепочки стенда %q нет в deploy/stacks.txt — STAND_STACK называет несуществующее", name)
	}
	v := seedVolume(t)

	a := chainAnonMailLimits(t, edgeAlertChain{name: name, chain: chain})
	t.Logf("цепочка стенда %s (%s): V посева=%d (%s); FREE=%d/%s · POW=%d/%s · HARD=%d/%s; "+
		"PoW подсети /24=%d /56=%d /48=%d за %s",
		name, strings.Join(chain, ","), v, standSeedFile,
		a.Source.Free, a.Source.FreeWindow, a.Source.PoW, a.Source.PoWWindow, a.Source.Hard, a.Source.HardWindow,
		a.SubnetV4Len24.PoW, a.SubnetV6Len56.PoW, a.SubnetV6Len48.PoW, a.SubnetPoWWindow)
	for _, f := range judgeStandVolume("цепочка "+name, v, a) {
		t.Error(f)
	}
}

// TestEdgeStandChain_VolumeJudgeFiresAndStaysSilent — судья различает исходы:
// близнецы отличаются одним фактом — FREE на единицу выше V либо равен ему.
func TestEdgeStandChain_VolumeJudgeFiresAndStaysSilent(t *testing.T) {
	const v = 10
	admit := config.AnonMailLimits{
		Source:        config.AnonMailSourceLimits{Free: v + 1, PoW: v + 1, Hard: 2*v + 2},
		SubnetV4Len24: config.AnonMailSubnetLimits{PoW: 2*v + 2, Hard: 2*v + 2},
		SubnetV6Len56: config.AnonMailSubnetLimits{PoW: 2*v + 2, Hard: 2*v + 2},
		SubnetV6Len48: config.AnonMailSubnetLimits{PoW: 2*v + 2, Hard: 2*v + 2},
	}
	if f := judgeStandVolume("близнец", v, admit); len(f) != 0 {
		t.Fatalf("FREE=V+1, V+FREE ниже прочих порогов — судья обязан молчать, а сказал: %v", f)
	}
	atFree := admit
	atFree.Source.Free = v
	if f := judgeStandVolume("положительный", v, atFree); len(f) != 1 || !strings.Contains(f[0], "FREE=10") {
		t.Fatalf("FREE=V — ровно одна находка о пороге «без проверки», получено: %v", f)
	}
	atSubnet := admit
	atSubnet.SubnetV6Len48.PoW = v + admit.Source.Free
	if f := judgeStandVolume("подсеть", v, atSubnet); len(f) != 1 || !strings.Contains(f[0], "/48") {
		t.Fatalf("V+FREE на пороге PoW подсети /48 — ровно одна находка о /48, получено: %v", f)
	}
}

// TestEdgeStandChain_AnonMailRelaxationStaysOnTheStand — близнец пробы объёма:
// каждая цепочка таблицы, кроме цепочки стенда прогона (`STAND_STACK`), несёт
// пределы базы чарта края. Цепочки площадок с внешним ретранслятором
// (`a8f60d`, `prorobotech`) наследуют средний слой стенда разработки, и
// послабление, объявленное в нём, дошло бы до них — это и судится.
func TestEdgeStandChain_AnonMailRelaxationStaysOnTheStand(t *testing.T) {
	stand := standStackName(t)
	stacks := deployableStacks(t)
	if _, ok := stacks[stand]; !ok {
		t.Fatalf("цепочки стенда %q нет в deploy/stacks.txt — STAND_STACK называет несуществующее", stand)
	}
	base := chainAnonMailLimits(t, edgeAlertChain{name: "chart"})
	judged, inheriting := 0, 0
	for _, name := range sortedStackNames(stacks) {
		if name == stand {
			continue
		}
		chain := stacks[name]
		for _, p := range chain {
			if p == "values.dev-prod.yaml" {
				inheriting++
				break
			}
		}
		judged++
		got := chainAnonMailLimits(t, edgeAlertChain{name: name, chain: chain})
		for _, f := range judgeOffStand("цепочка "+name+" ("+strings.Join(chain, ",")+")", base, got) {
			t.Error(f)
		}
	}
	if judged == 0 {
		t.Fatalf("вне цепочки стенда %s в таблице ни одной цепочки — судить нечего, и это не «послабление не протекло»", stand)
	}
	t.Logf("перепись: цепочка стенда %s вне суда; судимо цепочек %d, из них наследуют средний слой стенда "+
		"разработки (values.dev-prod.yaml) — %d", stand, judged, inheriting)
}

// TestEdgeStandChain_OffStandJudgeFiresAndStaysSilent — судья близнеца
// различает исходы: близнецы отличаются одним фактом — FREE источника.
func TestEdgeStandChain_OffStandJudgeFiresAndStaysSilent(t *testing.T) {
	base := config.AnonMailLimits{
		Source:        config.AnonMailSourceLimits{Free: 3, PoW: 10, Hard: 100},
		SubnetV4Len24: config.AnonMailSubnetLimits{PoW: 50, Hard: 500},
	}
	if f := judgeOffStand("близнец", base, base); len(f) != 0 {
		t.Fatalf("пределы равны базе — судья обязан молчать, а сказал: %v", f)
	}
	relaxed := base
	relaxed.Source.Free = 400
	if f := judgeOffStand("положительный", base, relaxed); len(f) != 1 || !strings.Contains(f[0], "Free:400") {
		t.Fatalf("FREE источника отличен от базы — ровно одна находка с величиной, получено: %v", f)
	}
}
