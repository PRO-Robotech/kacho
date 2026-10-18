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
// перекат: на время переката к запросам стенда добавляются поды, которые
// развёртывания поднимают СВЕРХ реплик (maxSurge), а StatefulSet меняет поды
// без прибавки. `surge` — эта прибавка (`rolloutSurgeMilli`).
func judgeStandQuotaHoldsRollout(total, surge, quota uint64) error {
	if total+surge > quota {
		return fmt.Errorf("квота пространства стенда %dm не держит перекат: запросы %dm плюс поды сверх реплик "+
			"%dm — %dm; поды переката встанут в отказ квоты", quota, total, surge, total+surge)
	}
	return nil
}

// surgePods — сколько подов развёртывание поднимает сверх реплик при перекате:
// RollingUpdate с maxSurge числом либо процентом (вверх до целого, умолчание
// 25 %), Recreate — ни одного.
func surgePods(spec map[string]any, replicas uint64) (uint64, error) {
	st, _ := spec["strategy"].(map[string]any)
	if typ, _ := st["type"].(string); typ == "Recreate" {
		return 0, nil
	}
	ru, _ := st["rollingUpdate"].(map[string]any)
	v, ok := ru["maxSurge"]
	if !ok || v == nil {
		v = "25%"
	}
	switch x := v.(type) {
	case int:
		if x < 0 {
			return 0, fmt.Errorf("maxSurge %d отрицателен", x)
		}
		return uint64(x), nil
	case string:
		pct, isPct := strings.CutSuffix(x, "%")
		if !isPct {
			return 0, fmt.Errorf("maxSurge %q не читается", x)
		}
		n, err := parseUintStrict(pct)
		if err != nil {
			return 0, fmt.Errorf("maxSurge %q: %w", x, err)
		}
		return (replicas*n + 99) / 100, nil
	default:
		return 0, fmt.Errorf("maxSurge %v (%T) не читается", v, v)
	}
}

// rolloutSurgeMilli — прибавка запросов процессора на время переката: под
// каждого развёртывания × его поды сверх реплик.
func rolloutSurgeMilli(docs []map[string]any) (uint64, error) {
	var sum uint64
	for _, d := range docs {
		if k, _ := d["kind"].(string); k != "Deployment" {
			continue
		}
		md, _ := d["metadata"].(map[string]any)
		spec, _ := d["spec"].(map[string]any)
		replicas, err := declaredReplicas(spec)
		if err != nil {
			return 0, fmt.Errorf("Deployment/%v: %w", md["name"], err)
		}
		n, err := surgePods(spec, replicas)
		if err != nil {
			return 0, fmt.Errorf("Deployment/%v: %w", md["name"], err)
		}
		tpl, _ := spec["template"].(map[string]any)
		ps, _ := tpl["spec"].(map[string]any)
		per, err := podCPUMilli(ps)
		if err != nil {
			return 0, fmt.Errorf("Deployment/%v: %w", md["name"], err)
		}
		sum += n * per
	}
	return sum, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// П2′ — ПОРОГ ЗАПРОСА СЛОЯ СТЕНДА ПРОБ (решение диспетчера по kacho#3291)
//
// Стенд проб в своём пространстве общего кластера получает запрос 500m при
// пределе 2: GOMAXPROCS 2 и мест вычисления 2 даёт предел, а запрос — лишь
// гарантию на занятом узле. Два ядра запроса на каждое пространство кластер не
// вмещает (с ними параллельных стендов 2 вместо 6). Поэтому П2 («запрос равен
// пределу») на слое стенда заменено порогом: на гарантированной доле очередь
// всей ёмкости проверяющего укладывается в срок операции церемонии.
//
//	срок D  = 2 × предел вызова соседа + C × стоимость проверки по часам
//	          (`ceremonyOperationDeadline`, kaname cmd/kaname/ceremony.go;
//	          предел вызова соседа — 3 с, cmd/kaname/client_token.go:56,
//	          kaname @1c1870e70);
//	очередь = C × процессорное время проверки / запрос;
//	П2′     : очередь ≤ D, то есть запрос ≥ C × процессорное время / D.
//
// Стоимости — замер kaname#891 (машина разработки, медиана из 7) для класса
// argon2id m=65536 КиБ t=3 p=4: 113 мс процессорного времени, 61 мс по часам на
// двух ядрах. Калибровка на занятом узле даёт стоимость по часам не меньше,
// срок от этого только растёт — 61 мс здесь нижняя оценка срока. Класс в
// рендере другой — порог не выводится, и это отказ, а не молчание.
//
// Ёмкость C — большее из ёмкости цепочки и ёмкости боевой цепочки prod: стенд
// проб не снисходительнее продукта, и подъём ёмкости стенда до боевой порог не
// обходит.
const (
	ceremonyPeerTimeoutMilli = 3000
	argon2CheckCPUMilli      = 113
	argon2CheckWallMilli     = 61
)

// argon2MeasuredClass — класс, для которого стоимости измерены.
var argon2MeasuredClass = map[string]string{
	"hasher-format": "argon2id", "hasher-memory": "65536", "hasher-iterations": "3", "hasher-parallelism": "4",
}

// standCPUFloor — срок, очередь на данном запросе и порог запроса для ёмкости.
type standCPUFloor struct {
	Capacity                  uint64
	DeadlineMilli, QueueMilli uint64
	FloorMilli, RequestMilli  uint64
}

func (f standCPUFloor) String() string {
	return fmt.Sprintf("срок D = 2 × %d мс + %d × %d мс = %d мс · очередь = %d × %d мс / %dm = %d мс · "+
		"порог запроса = ⌈%d × %d мс / %d мс⌉ = %dm",
		ceremonyPeerTimeoutMilli, f.Capacity, argon2CheckWallMilli, f.DeadlineMilli,
		f.Capacity, argon2CheckCPUMilli, f.RequestMilli, f.QueueMilli,
		f.Capacity, argon2CheckCPUMilli, f.DeadlineMilli, f.FloorMilli)
}

// judgeStandKanameCPU — П1, П3 и П2′ над блоком ресурсов контейнера службы под
// слоем стенда; class — величины класса проверки из карты настроек рендера.
func judgeStandKanameCPU(res map[string]any, capacity uint64, class map[string]string) (kanameCPUCensus, standCPUFloor, error) {
	var c kanameCPUCensus
	f := standCPUFloor{Capacity: capacity}
	requests, _ := res["requests"].(map[string]any)
	limits, _ := res["limits"].(map[string]any)
	if v, ok := requests["cpu"]; ok && v != nil {
		c.RequestRaw = fmt.Sprint(v)
	}
	if v, ok := limits["cpu"]; ok && v != nil {
		c.LimitRaw = fmt.Sprint(v)
	}
	if c.LimitRaw == "" {
		return c, f, fmt.Errorf("П1: предел процессора не объявлен — GOMAXPROCS равен числу процессоров узла, " +
			"места вычисления проверяющего равны всей ёмкости, очереди проверок нет")
	}
	var err error
	if c.LimitMilli, err = parseCPUMillis(c.LimitRaw); err != nil {
		return c, f, fmt.Errorf("П1: предел процессора не читается: %w", err)
	}
	c.GOMAXPROCS = gomaxprocsUnderCPULimit(c.LimitMilli)
	if c.LimitMilli%1000 != 0 || c.GOMAXPROCS*1000 != c.LimitMilli {
		return c, f, fmt.Errorf("П3: предел %dm даёт GOMAXPROCS %d — мест вычисления больше процессоров, данных "+
			"процессу; предел обязан быть целым числом процессоров не меньше двух", c.LimitMilli, c.GOMAXPROCS)
	}
	c.RequestMilli = c.LimitMilli
	if c.RequestRaw != "" {
		if c.RequestMilli, err = parseCPUMillis(c.RequestRaw); err != nil {
			return c, f, fmt.Errorf("П2′: запрос процессора не читается: %w", err)
		}
	}
	for k, want := range argon2MeasuredClass {
		if class[k] != want {
			return c, f, fmt.Errorf("П2′: класс проверки в рендере %s=%q, а стоимости измерены для %s=%q — "+
				"порог запроса не выводится; перемерьте стоимость класса", k, class[k], k, want)
		}
	}
	if capacity == 0 || c.RequestMilli == 0 {
		return c, f, fmt.Errorf("П2′: ёмкость %d либо запрос %dm нулевые — очередь не считается", capacity, c.RequestMilli)
	}
	f.RequestMilli = c.RequestMilli
	f.DeadlineMilli = 2*ceremonyPeerTimeoutMilli + capacity*argon2CheckWallMilli
	f.QueueMilli = (capacity*argon2CheckCPUMilli*1000 + c.RequestMilli - 1) / c.RequestMilli
	f.FloorMilli = (capacity*argon2CheckCPUMilli*1000 + f.DeadlineMilli - 1) / f.DeadlineMilli
	if f.QueueMilli > f.DeadlineMilli {
		return c, f, fmt.Errorf("П2′: на гарантированной доле %dm очередь всей ёмкости %d мс длиннее срока операции %d мс "+
			"(%s)", c.RequestMilli, f.QueueMilli, f.DeadlineMilli, f)
	}
	return c, f, nil
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

// Квота держит перекат по модели «запросы + поды сверх реплик»: 2270m + 1470m
// в 4000m молчит, а с двумя ядрами службы (3770m + 2970m) — краснеет; граница
// молчит.
func TestStandQuotaJudgeFiresOnTheOverflowAndIsSilentOnTheTwin(t *testing.T) {
	if err := judgeStandQuotaHoldsRollout(3770, 2970, 4000); err == nil {
		t.Error("квота 4000m при запросах 3770m и прибавке 2970m: мера смолчала")
	}
	if err := judgeStandQuotaHoldsRollout(2270, 1470, 4000); err != nil {
		t.Errorf("квота 4000m при 2270m + 1470m отвергнута: %v", err)
	}
	if err := judgeStandQuotaHoldsRollout(2000, 2000, 4000); err != nil {
		t.Errorf("граница отвергнута: %v", err)
	}
}

func TestSurgePodsReadsEveryLawfulForm(t *testing.T) {
	cases := []struct {
		spec     map[string]any
		replicas uint64
		want     uint64
	}{
		{map[string]any{}, 1, 1},
		{map[string]any{}, 4, 1},
		{map[string]any{}, 5, 2},
		{map[string]any{"strategy": map[string]any{"type": "Recreate"}}, 3, 0},
		{map[string]any{"strategy": map[string]any{"rollingUpdate": map[string]any{"maxSurge": 0}}}, 3, 0},
		{map[string]any{"strategy": map[string]any{"rollingUpdate": map[string]any{"maxSurge": "100%"}}}, 3, 3},
	}
	for _, c := range cases {
		got, err := surgePods(c.spec, c.replicas)
		if err != nil || got != c.want {
			t.Errorf("%v × %d: %d (%v), ожидалось %d", c.spec, c.replicas, got, err, c.want)
		}
	}
	if _, err := surgePods(map[string]any{"strategy": map[string]any{"rollingUpdate": map[string]any{"maxSurge": "one"}}}, 1); err == nil {
		t.Error("нечитаемый maxSurge принят")
	}
}

// П2′: решение диспетчера — 100m краснеет, 500m молчит (ёмкость 8 боевой
// цепочки); П1 и П3 на слое стенда судятся, как на боевых цепочках; класс
// проверки, для которого стоимость не измерена, — отказ.
func TestStandKanameCPUFloorFiresAt100mAndIsSilentAt500m(t *testing.T) {
	lim := map[string]any{"cpu": "2"}
	_, f, err := judgeStandKanameCPU(resourcesOf(map[string]any{"cpu": "500m"}, lim), 8, argon2MeasuredClass)
	if err != nil {
		t.Fatalf("500m при ёмкости 8 отвергнут: %v", err)
	}
	if f.DeadlineMilli != 6488 || f.QueueMilli != 1808 || f.FloorMilli != 140 {
		t.Errorf("числа порога разошлись с расчётом (D 6488, очередь 1808, порог 140): %s", f)
	}
	t.Logf("500m: %s", f)
	_, f, err = judgeStandKanameCPU(resourcesOf(map[string]any{"cpu": "100m"}, lim), 8, argon2MeasuredClass)
	if err == nil || !strings.HasPrefix(err.Error(), "П2′:") {
		t.Errorf("100m при ёмкости 8: мера смолчала либо назвала не то условие: %v (%s)", err, f)
	}
	t.Logf("100m: %v", err)
	for _, c := range []struct {
		name string
		res  map[string]any
		want string
	}{
		{"предел снят", resourcesOf(map[string]any{"cpu": "500m"}, nil), "П1:"},
		{"предел 1500m", resourcesOf(map[string]any{"cpu": "500m"}, map[string]any{"cpu": "1500m"}), "П3:"},
		{"предел 1", resourcesOf(map[string]any{"cpu": "500m"}, map[string]any{"cpu": "1"}), "П3:"},
	} {
		if _, _, err := judgeStandKanameCPU(c.res, 8, argon2MeasuredClass); err == nil || !strings.HasPrefix(err.Error(), c.want) {
			t.Errorf("%s: %v, ожидалось %s", c.name, err, c.want)
		}
	}
	other := map[string]string{}
	for k, v := range argon2MeasuredClass {
		other[k] = v
	}
	other["hasher-iterations"] = "4"
	if _, _, err := judgeStandKanameCPU(resourcesOf(map[string]any{"cpu": "500m"}, lim), 8, other); err == nil {
		t.Error("класс t=4 без замера стоимости принят — порог выведен из чужих чисел")
	}
}
