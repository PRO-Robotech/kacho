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
//	    пороге — находка».
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

func TestEdgeStandChain_AnonMailThresholdsAdmitTheSeedVolume(t *testing.T) {
	name := standStackName(t)
	chain, ok := deployableStacks(t)[name]
	if !ok {
		t.Fatalf("цепочки стенда %q нет в deploy/stacks.txt — STAND_STACK называет несуществующее", name)
	}
	v := seedVolume(t)

	out, err := renderEdgeChain(t, edgeAlertChain{name: name, chain: chain})
	if err != nil {
		t.Fatalf("цепочка %s: рендер не выполнен (%v) — условие не создано, вердикта нет:\n%s", name, err, out)
	}
	got, found := readEdgeContainer(t, out)
	if !found {
		t.Fatalf("цепочка %s: в рендере нет пода края — смотреть было не на что", name)
	}
	limits, err := config.ResolveEdgeLimits(configFromEnv(got.env))
	if err != nil {
		t.Fatalf("цепочка %s: страж старта края отверг окружение пода: %v", name, err)
	}
	a := limits.AnonMail
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
