// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// kaname_cpu_supply_test.go — СЛУЖБА ДОСТУПА ПОЛУЧАЕТ НА ЦЕПОЧКАХ ЗОНТА ТЕ
// ПРОЦЕССОРЫ, НА КОТОРЫХ ЕЁ ПРОВЕРЯЮЩИЙ СЕКРЕТОВ ВЕРЕН (kacho#3291, вслед
// kaname#891).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Проверяющий секретов службы (kaname, места вычисления kaname#882) пускает к
// вычислению не больше `min(GOMAXPROCS, ёмкость)` проверок; срок операции
// церемонии — ёмкость × стоимость одной проверки, откалиброванной на старте.
// Среда Go 1.26 выводит GOMAXPROCS из предела контрольной группы: меньшее из
// процессоров узла и предела, предел вверх до целого и не ниже двух
// (`runtime/cgroup_linux.go`, go1.26.9). Запрос (`requests.cpu`) среде не виден.
// Профиль самой службы (kaname deploy/values.prod.yaml, kaname#891) поставлен
// на запрос, равный пределу, два ядра; зонт переопределяет блок ресурсов
// подчарта, поэтому то же свойство судится здесь, по цепочкам зонта:
//
//	П1  предел процессора объявлен — без него GOMAXPROCS равен процессорам
//	    узла, мест вычисления столько же, сколько ёмкость, очереди нет;
//	П2  запрос равен пределу — на занятом узле процессу гарантирован запрос,
//	    а калибровка на старте видела предел;
//	П3  предел — целое число процессоров не меньше двух: дробный округляется
//	    вверх, единица поднимается до двух, и GOMAXPROCS больше данного.
//
// Мест вычисления проба не считает: проверяющий живёт во внутреннем пакете
// другого модуля, и его ответ `ProcessorSlots` судит проба kaname. Здесь
// судится предложение процессора, на котором тот ответ верен.
//
// ─────────────────────────────────────────────────────────────────────────────
// ОСВОБОЖДЕНИЕ — ВЫВОДОМ, А НЕ ПЕРЕЧНЕМ
//
// Цепочку, которую конвейер поднимает на kind в узле ранера, требование двух
// ядер может не вместить: узел 4000m, плоскость kind 950m. Такая цепочка
// освобождается, лишь пока арифметика узла это ДОКАЗЫВАЕТ: сумма её запросов,
// в которой запрос службы заменён на два ядра, плюс релиз cert-manager и
// плоскость kind больше ёмкости узла (`kanameTwoCoresOnRunner`). Ранер вырос
// либо цепочка похудела — освобождение снимается само, и цепочка судится.
// Имени цепочки в освобождении нет.
//
// Чистые функции меры — здесь (судятся без helm); рендер цепочек и слой стенда
// проб — kaname_cpu_supply_render_test.go (тег helmcharts).
package deploy_test

import (
	"fmt"
	"strings"
	"testing"
)

// kanameCPUWholeCores — наименьший предел, при котором GOMAXPROCS среды равен
// пределу: два целых ядра.
const kanameCPUWholeCores = 2000

// gomaxprocsUnderCPULimit — GOMAXPROCS, который среда Go 1.26 выведет из предела
// контрольной группы на узле с процессорами не меньше предела: предел вверх до
// целого и не ниже двух (`adjustCgroupGOMAXPROCS`, go1.26.9).
func gomaxprocsUnderCPULimit(limitMilli uint64) uint64 {
	return max((limitMilli+999)/1000, 2)
}

// kanameCPUCensus — прочитанное и выведенное; печатается всегда.
type kanameCPUCensus struct {
	RequestRaw, LimitRaw     string
	RequestMilli, LimitMilli uint64
	GOMAXPROCS               uint64
}

func (c kanameCPUCensus) String() string {
	or := func(s string) string {
		if s == "" {
			return "<не объявлен>"
		}
		return s
	}
	return fmt.Sprintf("requests.cpu %s · limits.cpu %s · GOMAXPROCS среды %d",
		or(c.RequestRaw), or(c.LimitRaw), c.GOMAXPROCS)
}

// judgeKanameCPUSupply — П1–П3 над блоком `resources` контейнера службы.
func judgeKanameCPUSupply(res map[string]any) (kanameCPUCensus, error) {
	var c kanameCPUCensus
	requests, _ := res["requests"].(map[string]any)
	limits, _ := res["limits"].(map[string]any)
	if v, ok := requests["cpu"]; ok && v != nil {
		c.RequestRaw = fmt.Sprint(v)
	}
	if v, ok := limits["cpu"]; ok && v != nil {
		c.LimitRaw = fmt.Sprint(v)
	}
	if c.LimitRaw == "" {
		return c, fmt.Errorf("П1: предел процессора не объявлен — GOMAXPROCS равен числу процессоров узла, " +
			"места вычисления проверяющего равны всей ёмкости, очереди проверок нет")
	}
	var err error
	if c.LimitMilli, err = parseCPUMillis(c.LimitRaw); err != nil {
		return c, fmt.Errorf("П1: предел процессора не читается: %w", err)
	}
	c.GOMAXPROCS = gomaxprocsUnderCPULimit(c.LimitMilli)
	// Запрос не объявлен — Kubernetes берёт его равным пределу.
	c.RequestMilli = c.LimitMilli
	if c.RequestRaw != "" {
		if c.RequestMilli, err = parseCPUMillis(c.RequestRaw); err != nil {
			return c, fmt.Errorf("П2: запрос процессора не читается: %w", err)
		}
	}
	if c.RequestMilli != c.LimitMilli {
		return c, fmt.Errorf("П2: запрос процессора %dm не равен пределу %dm — на занятом узле процессу "+
			"гарантирован запрос, а стоимость проверки калибруется на старте до предела", c.RequestMilli, c.LimitMilli)
	}
	if c.LimitMilli%1000 != 0 || c.GOMAXPROCS*1000 != c.LimitMilli {
		return c, fmt.Errorf("П3: предел %dm даёт GOMAXPROCS %d — мест вычисления больше процессоров, данных "+
			"процессу; предел обязан быть целым числом процессоров не меньше двух", c.LimitMilli, c.GOMAXPROCS)
	}
	return c, nil
}

// kanameTwoCoresOnRunner — сумма запросов узла ранера, если службе дать два
// целых ядра: запросы цепочки без нынешнего запроса службы, плюс два ядра на
// каждую реплику, плюс релиз cert-manager и плоскость kind. Второй ответ —
// вмещает ли узел эту сумму.
func kanameTwoCoresOnRunner(chainTotal, kanameNow, replicas, release, baseline, capacity uint64) (uint64, bool) {
	need := chainTotal - kanameNow + replicas*kanameCPUWholeCores + release + baseline
	return need, need <= capacity
}

// judgeStandQuotaHoldsRollout — квота процессора пространства стенда держит
// перекат: рядом со старыми подами встают новые, и на время переката запросов
// вдвое больше.
func judgeStandQuotaHoldsRollout(total, quota uint64) error {
	if 2*total > quota {
		return fmt.Errorf("квота пространства стенда %dm не держит перекат: запросы %dm, рядом со старыми подами "+
			"встают новые — %dm; поды переката встанут в отказ квоты", quota, total, 2*total)
	}
	return nil
}

func resourcesOf(req, lim map[string]any) map[string]any {
	out := map[string]any{}
	if req != nil {
		out["requests"] = req
	}
	if lim != nil {
		out["limits"] = lim
	}
	return out
}

// Мера молчит на поставляемой величине и на законном близнеце (4 и 4), иначе
// она требовала бы ровно число, а не свойство; каждая инъекция меняет один факт
// и называет своё условие.
func TestKanameCPUSupplyJudgeFindsEachBrokenCondition(t *testing.T) {
	silent := []struct {
		name string
		res  map[string]any
		want uint64
	}{
		{"поставляемая величина 2 и 2", resourcesOf(map[string]any{"cpu": "2"}, map[string]any{"cpu": "2"}), 2},
		{"законный близнец 4 и 4", resourcesOf(map[string]any{"cpu": "4"}, map[string]any{"cpu": "4"}), 4},
		{"только предел 2000m — запрос равен пределу", resourcesOf(nil, map[string]any{"cpu": "2000m"}), 2},
	}
	for _, c := range silent {
		census, err := judgeKanameCPUSupply(c.res)
		if err != nil {
			t.Errorf("%s: мера отвергла законную величину: %v (%s)", c.name, err, census)
			continue
		}
		if census.GOMAXPROCS != c.want {
			t.Errorf("%s: GOMAXPROCS %d, ожидалось %d (%s)", c.name, census.GOMAXPROCS, c.want, census)
		}
	}
	fire := []struct {
		name string
		res  map[string]any
		want string
	}{
		{"предел снят (как у kaname до #891)", resourcesOf(map[string]any{"cpu": "100m"}, nil), "П1:"},
		{"запрос ниже предела (умолчание подчарта)", resourcesOf(map[string]any{"cpu": "100m"}, map[string]any{"cpu": "1000m"}), "П2:"},
		{"запрос 10m (прежний a8f60d)", resourcesOf(map[string]any{"cpu": "10m"}, map[string]any{"cpu": "2"}), "П2:"},
		{"дробный предел 1500m", resourcesOf(map[string]any{"cpu": "1500m"}, map[string]any{"cpu": "1500m"}), "П3:"},
		{"предел один процессор", resourcesOf(map[string]any{"cpu": "1"}, map[string]any{"cpu": "1"}), "П3:"},
		{"предел не читается", resourcesOf(nil, map[string]any{"cpu": "1.5"}), "П1:"},
	}
	for _, c := range fire {
		census, err := judgeKanameCPUSupply(c.res)
		if err == nil {
			t.Errorf("%s: мера смолчала (%s)", c.name, census)
			continue
		}
		if !strings.HasPrefix(err.Error(), c.want) {
			t.Errorf("%s: отказ называет не то условие — %v, ожидалось %s", c.name, err, c.want)
		}
	}
}

// Освобождение даётся ровно тогда, когда узел двух ядер не вмещает: цифры
// цепочки own на узле 4000m (рендер 2026-10-18) — освобождена; тот же стенд на
// узле 8000m — судится.
func TestKanameTwoCoresOnRunnerIsDecidedByTheNodeArithmetic(t *testing.T) {
	need, fits := kanameTwoCoresOnRunner(1970, 100, 1, 0, 950, 4000)
	if fits || need != 4820 {
		t.Errorf("узел 4000m: сумма %dm, вмещает %v — ожидалось 4820m и «не вмещает»", need, fits)
	}
	need, fits = kanameTwoCoresOnRunner(1970, 100, 1, 0, 950, 8000)
	if !fits || need != 4820 {
		t.Errorf("узел 8000m: сумма %dm, вмещает %v — ожидалось 4820m и «вмещает»: освобождение пережило бы свою причину", need, fits)
	}
	need, fits = kanameTwoCoresOnRunner(1970, 100, 2, 0, 950, 6000)
	if fits || need != 6820 {
		t.Errorf("две реплики: сумма %dm, вмещает %v — ожидалось 6820m и «не вмещает»", need, fits)
	}
}

// Квота 4 ядра (до kacho#3291) при запросах стенда 3770m перекат не держит;
// 8 ядер — держит; близнец на границе (ровно вдвое) молчит.
func TestStandQuotaJudgeFiresOnTheOldQuotaAndIsSilentOnTheTwin(t *testing.T) {
	if err := judgeStandQuotaHoldsRollout(3770, 4000); err == nil {
		t.Error("квота 4000m при запросах 3770m: мера смолчала, а перекат службы доступа в квоту не встаёт")
	}
	if err := judgeStandQuotaHoldsRollout(3770, 8000); err != nil {
		t.Errorf("квота 8000m при запросах 3770m отвергнута: %v", err)
	}
	if err := judgeStandQuotaHoldsRollout(4000, 8000); err != nil {
		t.Errorf("граница (ровно вдвое) отвергнута: %v", err)
	}
}
