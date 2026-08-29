import { labelFilterActive, parseLabelQuery, rowMatchesLabels } from "./list-label-filter";

/**
 * Отбор строк списка ПО МЕТКАМ — на клиенте, и это решение, а не упущение.
 *
 * Серверного отбора по меткам нет намеренно: метка мутабельна, ресурс входит в
 * выборку и выходит из неё, а событие потока без предыдущего состояния не даёт
 * отличить «вышел из выборки» от «удалён» — подписчик принял бы правку метки за
 * снос. Поэтому метки судит тот, у кого есть ОБА состояния: страница, которая
 * перечитала список.
 *
 * Отсюда требование к этому предикату: он обязан быть ЧИСТОЙ функцией от строки
 * и запроса. Держи он своё состояние — строка, потерявшая метку, осталась бы в
 * списке до перезагрузки, то есть ровно тот дефект, ради которого отбор и пишут.
 */
describe("отбор списка по меткам", () => {
  const row = (labels?: Record<string, string>) => ({ id: "x", labels });

  // verifies #1021
  it("пустой запрос НЕ сужает — иначе список пуст до первого ввода", () => {
    expect(parseLabelQuery("")).toEqual([]);
    expect(parseLabelQuery("   ")).toEqual([]);
    expect(labelFilterActive("")).toBe(false);
    expect(rowMatchesLabels(row({ env: "prod" }), parseLabelQuery(""))).toBe(true);
    expect(rowMatchesLabels(row(undefined), parseLabelQuery(""))).toBe(true);
  });

  it("одно имя метки требует НАЛИЧИЯ ключа, значение любое", () => {
    const terms = parseLabelQuery("env");
    expect(labelFilterActive("env")).toBe(true);
    expect(rowMatchesLabels(row({ env: "prod" }), terms)).toBe(true);
    expect(rowMatchesLabels(row({ env: "" }), terms)).toBe(true);
    expect(rowMatchesLabels(row({ tier: "web" }), terms)).toBe(false);
    expect(rowMatchesLabels(row(undefined), terms)).toBe(false);
  });

  it("пара требует ТОЧНОГО значения, а не подстроки", () => {
    // Подстрока здесь была бы ловушкой: `env=pro` показал бы `prod`, и человек
    // прочитал бы сужение как выполненное. Метка — точное значение.
    const terms = parseLabelQuery("env=prod");
    expect(rowMatchesLabels(row({ env: "prod" }), terms)).toBe(true);
    expect(rowMatchesLabels(row({ env: "production" }), terms)).toBe(false);
    expect(rowMatchesLabels(row({ env: "pro" }), terms)).toBe(false);
  });

  it("несколько условий соединяются конъюнкцией — как оси подписки", () => {
    const terms = parseLabelQuery("env=prod tier");
    expect(terms).toHaveLength(2);
    expect(rowMatchesLabels(row({ env: "prod", tier: "web" }), terms)).toBe(true);
    expect(rowMatchesLabels(row({ env: "prod" }), terms)).toBe(false);
    expect(rowMatchesLabels(row({ tier: "web" }), terms)).toBe(false);
  });

  it("значение со знаком равенства внутри разрезается по ПЕРВОМУ знаку", () => {
    expect(parseLabelQuery("url=a=b")).toEqual([{ key: "url", value: "a=b" }]);
  });

  it("ключ без значения после знака равенства требует ПУСТОГО значения", () => {
    // `env=` — не то же, что `env`: первое спрашивает метку с пустым значением,
    // второе — метку с любым. Свести их значило бы сделать один из двух
    // вопросов незадаваемым.
    const empty = parseLabelQuery("env=");
    expect(empty).toEqual([{ key: "env", value: "" }]);
    expect(rowMatchesLabels(row({ env: "" }), empty)).toBe(true);
    expect(rowMatchesLabels(row({ env: "prod" }), empty)).toBe(false);
  });

  it("строка не из объекта меток отбрасывается, а не роняет страницу", () => {
    // `labels` приезжает из `JSON.parse` — компилятор его формы не знает.
    const terms = parseLabelQuery("env");
    expect(rowMatchesLabels({ id: "x", labels: "не-объект" }, terms)).toBe(false);
    expect(rowMatchesLabels({ id: "x", labels: null }, terms)).toBe(false);
    expect(rowMatchesLabels({ id: "x" }, terms)).toBe(false);
  });

  it("числовое и логическое значение метки сравниваются как строка", () => {
    // Заголовок прежде обещал ещё и «а не через toString объекта», чего тело не
    // проверяло: объектный случай стоит отдельной пробой ниже. Заголовок,
    // объявляющий больше своего тела, зеленеет на коде, которого у него нет.
    expect(rowMatchesLabels({ id: "x", labels: { n: 1 } }, parseLabelQuery("n=1"))).toBe(true);
    expect(rowMatchesLabels({ id: "x", labels: { n: true } }, parseLabelQuery("n=true"))).toBe(true);
  });

  it("значение метки без осмысленной строки не совпадает НИ С ЧЕМ", () => {
    // ДОСТИЖИМОЕ приводится списком, а не объектом, и это надо сказать точно.
    // `String([1, 2])` даёт «1,2» — без пробела, значит такой запрос человек
    // НАБРАТЬ МОЖЕТ, и список сравнивался бы равным строке «1,2».
    expect(rowMatchesLabels({ id: "x", labels: { n: [1, 2] } }, parseLabelQuery("n=1,2"))).toBe(false);
    // Пустой список даёт пустую строку — то есть попадал в запрос «n=», который
    // означает «метка с ПУСТЫМ значением». Список пустым значением не является.
    expect(rowMatchesLabels({ id: "x", labels: { n: [] } }, parseLabelQuery("n="))).toBe(false);

    // ПОЛОЖИТЕЛЬНЫЕ КОНТРОЛИ: отрицания выше зеленели бы на отборе, который не
    // совпадает вообще ни с чем.
    expect(rowMatchesLabels({ id: "x", labels: { n: "1,2" } }, parseLabelQuery("n=1,2"))).toBe(true);
    expect(rowMatchesLabels({ id: "x", labels: { n: "" } }, parseLabelQuery("n="))).toBe(true);

    // Наличие ключа объект по-прежнему подтверждает: спрошено про метку, а не
    // про её значение.
    expect(rowMatchesLabels({ id: "x", labels: { n: {} } }, parseLabelQuery("n"))).toBe(true);

    // ОБЪЕКТ ЖЕ НЕДОСТИЖИМ, и выдавать его за дефект нельзя: `String({})` даёт
    // «[object Object]» — с пробелом, а условия режутся по пробелу, поэтому
    // такого условия в запросе не бывает. Строка ниже закрепляет поведение, а
    // не воспроизводит дефект.
    expect(rowMatchesLabels({ id: "x", labels: { n: {} } }, parseLabelQuery("n=object"))).toBe(false);
  });
});
