// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1
//
// internal_check.js — нагрузка на InternalIAMService.Check: точечную проверку
// доступа, которую per-RPC интерсепторы vpc / compute / nlb зовут на КАЖДОМ
// запросе к платформе.
//
// ПОЧЕМУ ИМЕННО ЭТОТ ГЛАГОЛ, А НЕ ПУБЛИЧНЫЙ AuthorizeService.Check.
// Публичный Check стоит за краем, и путь до него включает разбор RS256-токена
// шлюзом, его кэш положительных вердиктов и внутреннюю калитку
// authorizeCaller — до четырёх лишних обращений к хранилищу прав на отказе.
// Замер через край мерил бы край. Внутренний Check объявлен `<exempt>`, его
// пускает только пол проверенного сертификата, и он ровно то, что тратится на
// каждом запросе платформы: один поход в хранилище прав плюс теневой запрос
// в свою базу.
//
// ЧТО ИМЕННО ОПЛАЧИВАЕТСЯ ОДНИМ ВЫЗОВОМ (важно для истолкования чисел):
//   · 1 HTTP-вызов к хранилищу прав (бюджет 200 мс, до 3 попыток внутри него);
//   · теневой запрос в свою базу — СБОКУ ОТ ПУТИ ОТВЕТА: у него свой срок 10 мс,
//     свой контекст и потолок восьми одновременных сравнений с ОТБРАСЫВАНИЕМ, а не
//     очередью. Ответ вызывающему он не задерживает ни значением, ни задержкой;
//     отброшенный вопрос попадает в исход «не выполнилось» со своей причиной.
// Прогон всё равно снимается вместе с показателями пула — соединение теневой
// запрос берёт, — но «ждём свободное соединение» больше не сидит на пути ответа.
//
// Здесь стояло: теневой запрос «блокирует ответ до 50 мс», а потолок равен
// «100 / среднее время теневого запроса» при `pool_max_conns=100`. Ни срок, ни
// предел пула, ни сам механизм давно не таковы; логика прибора при этом была верна
// — устарел текст. Снято по находке перемера 2026-08-20.
//
// Транспорт: gRPC + mTLS клиентским сертификатом модуля (SPIFFE-SAN), схема
// берётся серверным отражением — грузить .proto с их импортами не требуется.
//
// Запуск (в кластере, см. deploy/load-tests/k6-iam-internal-check.yaml):
//   k6 run -e TARGET_RPS=800 -e DURATION=60s /scripts/internal_check.js
import grpc from 'k6/net/grpc';
import { check } from 'k6';
import { Trend, Rate, Counter } from 'k6/metrics';

const ADDR        = __ENV.IAM_ADDR    || 'kacho-iam-internal.kacho.svc:9091';
const TARGET_RPS  = parseInt(__ENV.TARGET_RPS || '200', 10);
const DURATION    = __ENV.DURATION    || '60s';
const ALLOW_RATIO = parseFloat(__ENV.ALLOW_RATIO || '0.9');
const MAX_VUS     = parseInt(__ENV.MAX_VUS || String(Math.max(64, TARGET_RPS)), 10);
const PRE_VUS     = parseInt(__ENV.PRE_VUS || String(Math.min(MAX_VUS, Math.max(32, Math.ceil(TARGET_RPS / 4)))), 10);

const CERT_PATH = __ENV.CERT_PATH || '/certs/tls.crt';
const KEY_PATH  = __ENV.KEY_PATH  || '/certs/tls.key';
const CA_PATH   = __ENV.CA_PATH   || '/certs/ca.crt';

// Фикстура: реальные тройки (субъект, отношение, объект) из хранилища прав
// стенда. Разрешающие взяты как есть; отказ строится ПЕРЕСТАНОВКОЙ — субъект
// одной тройки против объекта другой. Так отказ остаётся well-formed (объект
// существует, тип верен) и оплачивает полный обход, а не короткий отлуп на
// разборе идентификатора.
//
// ПЕРЕСТАНОВКА ПРОВЕРЕНА И ИСТОЧНИКОМ РАСХОЖДЕНИЙ НЕ ЯВЛЯЕТСЯ (замер #775,
// 2026-08-20). Подозрение было обратным: что теневая сверка расходится с движком
// именно на синтетических парах. Развели двумя прогонами на одном стенде —
// `ALLOW_RATIO=1.0` (ни одной перестановки) дал 61.0 % расхождений, `ALLOW_RATIO=0.0`
// (одни перестановки) дал 0.13 %. Расхождение приносят НАСТОЯЩИЕ выдачи, а не
// перестановка; она, наоборот, почти всегда даёт согласное «нет». Повторяется
// пробой `deploy/load-tests/iam-shadow-divergence-probe.sh`.
//
// Но сам прибор этого не знал и знать не мог: до этой правки он НЕ ПРОВЕРЯЛ, что
// его отказной вход действительно отказывает. Утверждение «вердикт достоверно
// нет пути» стояло комментарием и не держалось ничем — положительный контроль был
// односторонним (`wrongVerdict` ловит только «разрешающая тройка не разрешила»).
// Обратная сторона — «перестановка оказалась разрешающей» — теперь считается
// (`iam_check_deny_input_allowed`), и доля отказного входа перестала быть
// объявлением: её видно числом в сводке прогона.
const TUPLES = JSON.parse(open(__ENV.FIXTURE_PATH || '/fixtures/allow_tuples.json'));

// Индекс объектов ПО ТИПУ: отказ строится подстановкой объекта ТОГО ЖЕ типа.
// Кросс-типовая подстановка тоже дала бы «нет пути», но сменила бы стоимость:
// у типов, принадлежащих iam (account/project), путь отказа доплачивает
// структурным чтением в свою базу, у vpc-типов — нет. Смешав типы, мы мерили бы
// не ту работу, которую платит настоящий отказ по этому объекту.
const BY_TYPE = {};
for (const t of TUPLES) {
  const ty = t.object.split(':')[0];
  (BY_TYPE[ty] = BY_TYPE[ty] || []).push(t.object);
}

// open() доступен ТОЛЬКО в init-контексте — читаем ключевой материал здесь,
// иначе connect() внутри итерации падает на каждом VU.
const CERT = open(CERT_PATH);
const KEY  = open(KEY_PATH);
const CA   = open(CA_PATH);

const checkLatency = new Trend('iam_check_latency_ms', true);
const allowLatency = new Trend('iam_check_allow_latency_ms', true);
const denyLatency  = new Trend('iam_check_deny_latency_ms', true);
const errRate      = new Rate('iam_check_errors');
const okCount      = new Counter('iam_check_ok');
const wrongVerdict = new Counter('iam_check_wrong_verdict');
// Три счётчика ниже делают ОТКАЗНОЙ вход наблюдаемым. Без них доля отказа была
// намерением (`ALLOW_RATIO`), а не измеренной величиной, и разойтись с ней могла
// молча — см. `denyInputUnavailable`.
const denyInput            = new Counter('iam_check_deny_input');
const denyInputAllowed     = new Counter('iam_check_deny_input_allowed');
const denyInputUnavailable = new Counter('iam_check_permutation_unavailable');

export const options = {
  scenarios: {
    steady: {
      executor: 'constant-arrival-rate',
      rate: TARGET_RPS,
      timeUnit: '1s',
      duration: DURATION,
      preAllocatedVUs: PRE_VUS,
      maxVUs: MAX_VUS,
      gracefulStop: '10s',
    },
  },
  // Порог — БЮДЖЕТ ЧТЕНИЯ, а не украшение: прогон, вышедший за него, обязан
  // пометиться отказом, иначе точку насыщения пришлось бы искать глазами.
  thresholds: {
    'iam_check_latency_ms': ['p(99)<30'],
    'iam_check_errors':     ['rate<0.001'],
  },
  summaryTrendStats: ['min', 'avg', 'med', 'p(90)', 'p(95)', 'p(99)', 'max'],
  noConnectionReuse: false,
};

const client = new grpc.Client();
let connected = false;

export default function () {
  if (!connected) {
    client.connect(ADDR, {
      reflect: true,
      tls: { cert: CERT, key: KEY, cacerts: [CA] },
    });
    connected = true;
  }

  const i = Math.floor(Math.random() * TUPLES.length);
  const t = TUPLES[i];
  const wantAllow = Math.random() < ALLOW_RATIO;

  // Отказ: объект берётся у ДРУГОЙ тройки того же типа, поэтому вердикт
  // достоверно «нет пути», а не «неверный идентификатор».
  let object = t.object;
  let isDeny = false;
  if (!wantAllow) {
    const ty = t.object.split(':')[0];
    const pool = BY_TYPE[ty];
    if (pool && pool.length > 1) {
      let cand = t.object;
      for (let k = 0; k < 4 && cand === t.object; k++) {
        cand = pool[Math.floor(Math.random() * pool.length)];
      }
      if (cand !== t.object) { object = cand; isDeny = true; }
    }
  }
  // Перестановка НЕ СОСТОЯЛАСЬ (в пуле типа один объект, либо четыре броска
  // выпали в тот же). Запрос уходит исходной РАЗРЕШАЮЩЕЙ тройкой, то есть
  // фактическая доля отказа ниже объявленной `ALLOW_RATIO`. Прежде этот снос был
  // невидим: вызов просто считался разрешающим, и объявленная доля читалась как
  // измеренная. Теперь он называет себя сам.
  if (!wantAllow && !isDeny) denyInputUnavailable.add(1);
  if (isDeny) denyInput.add(1);

  const started = Date.now();
  const res = client.invoke('kacho.cloud.iam.v1.InternalIAMService/Check', {
    subject_id: t.subject,
    relation: t.relation,
    object: object,
  });
  const ms = Date.now() - started;

  checkLatency.add(ms);
  const ok = res && res.status === grpc.StatusOK;
  errRate.add(!ok);
  if (!ok) return;
  okCount.add(1);

  const allowed = res.message && res.message.allowed === true;
  if (allowed) { allowLatency.add(ms); } else { denyLatency.add(ms); }
  // Положительный контроль: разрешающая тройка ОБЯЗАНА разрешать. Без него
  // прогон остался бы зелёным на стенде, где посев не доехал, и мерил бы
  // сплошной отказ, называя это пропускной способностью.
  if (!isDeny && !allowed) wrongVerdict.add(1);
  // Симметричная половина того же контроля: перестановка, которую движок РАЗРЕШИЛ,
  // отказом не является, и мерить на ней стоимость отказа нельзя. Это не обязано
  // быть нулём — у субъекта законно бывают права на несколько объектов своего
  // типа, — поэтому здесь счётчик, а не порог: порог краснел бы на исправном
  // стенде. Но величина обязана быть ВИДНА, иначе «10 % отказа» остаётся
  // объявлением о входе, которое никто не сверял с исходом.
  if (isDeny && allowed) denyInputAllowed.add(1);
  check(res, { 'grpc OK': (r) => r.status === grpc.StatusOK });
}

export function teardown() {
  client.close();
}
