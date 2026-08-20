// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1
//
// resource_ops.js — нагрузка на ПРОДУКТОВЫЕ операции (чтение и создание
// ресурсов) через КРАЙ, по REST. Отвечает на вопрос «сколько стоит операция
// целиком и какая доля этого — проверка доступа».
//
// ЧТО ИМЕННО ОПЛАЧИВАЕТСЯ ОДНИМ ВЫЗОВОМ (важно для истолкования чисел) —
// замер идёт ЧЕРЕЗ КРАЙ, поэтому в задержку входит и разбор токена:
//   · разбор и верификация RS256-Bearer'а краем (подпись, aud, acr);
//   · per-RPC FGA-Check края (каталог прав + scope_extractor);
//   · gRPC-хоп край → сервис по mTLS;
//   · СОБСТВЕННЫЙ authz-интерсептор сервиса → InternalIAMService.Check;
//   · собственно работа ресурса (SQL, peer-вызовы, материализация прав).
// Полоса `geo_get` НАМЕРЕННО обходит проверку прав на уровне проекта
// (project-scope EXEMPT, см. security.md) и служит контролем: разница с
// `net_get` — цена проверки доступа при прочих равных.
import http from 'k6/http';
import { check } from 'k6';
import { Trend, Rate, Counter } from 'k6/metrics';

const BASE       = __ENV.BASE_URL   || 'http://api-gateway.kacho.svc:8080';
// Ключевой материал и фикстура читаются в init-контексте: open() вне его
// недоступен, и попытка прочитать файл в итерации падает на каждом VU.
const FIXTURES   = JSON.parse(open(__ENV.FIXTURE_PATH || '/fixtures/fixtures.json'));
const TOKEN      = __ENV.TOKEN || FIXTURES[__ENV.TOKEN_KEY || 'jwtProjectEditorA'];
// Пул объектов для ХОЛОДНОЙ полосы. Кэш положительных вердиктов живёт 5 с и
// на КАЖДОМ из двух слоёв (край и сервис), поэтому повторный вопрос про тот же
// объект проверку НЕ оплачивает. Полоса, гоняющая один объект, меряет кэш;
// чтобы мерить проверку, объектов нужно больше, чем подача успевает пройти за
// время жизни записи (пул > 5 × подача). Пустой пул ⇒ полоса горячая.
const NET_POOL   = __ENV.NET_POOL_PATH ? JSON.parse(open(__ENV.NET_POOL_PATH)) : [];
const OP         = __ENV.OP         || 'net_get';
const PROJECT    = __ENV.PROJECT_ID  || FIXTURES.existingProjectId;
const NETWORK    = __ENV.NETWORK_ID  || FIXTURES.seedNetworkA1Id;
const ZONE       = __ENV.ZONE_ID     || FIXTURES.existingZoneId || 'ru-central1-a';
const REGION     = __ENV.REGION_ID   || FIXTURES.existingRegionId || 'ru-central1';
const RUN_ID     = __ENV.RUN_ID     || `r${Date.now()}`;
const PAGE_SIZE  = parseInt(__ENV.PAGE_SIZE || '50', 10);

const TARGET_RPS = parseInt(__ENV.TARGET_RPS || '50', 10);
const DURATION   = __ENV.DURATION   || '60s';
const MAX_VUS    = parseInt(__ENV.MAX_VUS || String(Math.max(64, TARGET_RPS * 3)), 10);
const PRE_VUS    = parseInt(__ENV.PRE_VUS || String(Math.min(MAX_VUS, Math.max(32, TARGET_RPS))), 10);

// Бюджет — РАЗНЫЙ для чтения и записи, поэтому он параметр полосы, а не файла.
const BUDGET_MS  = parseInt(__ENV.BUDGET_MS || (OP.endsWith('_create') ? '200' : '30'), 10);

if (!TOKEN)   throw new Error('TOKEN обязателен: без него край ответит 401 и замер померит скорость отказа');
if (!PROJECT) throw new Error('PROJECT_ID обязателен');

const lat          = new Trend('op_latency_ms', true);
const errRate      = new Rate('op_errors');
const okCount      = new Counter('op_ok');
// ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ. Без него прогон на стенде, куда не доехал посев,
// остался бы зелёным и мерил бы сплошной 403/404, называя это пропускной
// способностью. Считаем ответы, которые пришли с кодом 2xx, но НЕ несут того,
// ради чего операция звалась.
const wrongShape   = new Counter('op_wrong_shape');
const httpDenied   = new Counter('op_denied_4xx');
const httpFailed   = new Counter('op_failed_5xx');

export const options = {
  scenarios: {
    steady: {
      executor: 'constant-arrival-rate',
      rate: TARGET_RPS, timeUnit: '1s', duration: DURATION,
      preAllocatedVUs: PRE_VUS, maxVUs: MAX_VUS, gracefulStop: '15s',
    },
  },
  // Порог — БЮДЖЕТ ЧТЕНИЯ, а не украшение: прогон, вышедший за него, обязан
  // пометиться отказом (k6 rc=99), иначе точку насыщения ищут глазами.
  thresholds: {
    'op_latency_ms':   [{ threshold: `p(99)<${BUDGET_MS}`, abortOnFail: false }],
    'op_errors':       [{ threshold: 'rate<0.01',          abortOnFail: false }],
    'op_wrong_shape':  [{ threshold: 'count<1',            abortOnFail: false }],
    // ПРОГОН, НЕ ОБСЛУЖИВШИЙ НИ ОДНОГО ЗАПРОСА, — НЕ «чисто». Без этой строки
    // пороги считаются на ПУСТОМ наборе и проходят все разом: наблюдалось —
    // полоса, упавшая на разборе фикстуры, отчиталась зелёной, сделав 375
    // итераций и отправив 0 байт.
    'op_ok':           [{ threshold: 'count>0',            abortOnFail: false }],
  },
  // Без этого k6 экспортирует только med/p(90)/p(95): порог считает p(99), но в
  // отчёт он не попадает — то есть единственная величина, по которой выносится
  // вердикт, оказалась бы единственной, которой в таблице нет.
  summaryTrendStats: ['min', 'med', 'avg', 'p(90)', 'p(95)', 'p(99)', 'max'],
  discardResponseBodies: false,
  insecureSkipTLSVerify: true,
};

const H = { headers: { Authorization: `Bearer ${TOKEN}`, 'Content-Type': 'application/json' } };

// uniq — имя, уникальное в пределах прогона. UNIQUE(project,name) иначе даёт
// 409 на повторе, и «пул имён исчерпан» читалось бы как отказ продукта.
function uniq(p) { return `${p}-${RUN_ID}-${__VU}-${__ITER}`; }

function request() {
  switch (OP) {
    case 'net_get': {   // ГОРЯЧАЯ полоса: один объект ⇒ вердикт берётся из кэша
      return { r: http.get(`${BASE}/vpc/v1/networks/${NETWORK}`, H), want: b => b.id === NETWORK };
    }
    case 'net_get_cold': { // ХОЛОДНАЯ полоса: объект на каждый запрос свой
      if (NET_POOL.length === 0) throw new Error('полосе net_get_cold нужен NET_POOL_PATH — без пула она горячая и мерит кэш');
      // Обход пула СКВОЗНОЙ, а не случайный: случайный выбор повторяет объект
      // тем чаще, чем меньше пул, и доля промахов становится величиной, которую
      // замер не контролирует. Сквозной обход даёт ровно один проход за круг.
      const id = NET_POOL[(__VU * 100003 + __ITER) % NET_POOL.length];
      return { r: http.get(`${BASE}/vpc/v1/networks/${id}`, H), want: b => b.id === id };
    }
    case 'net_list':
      return { r: http.get(`${BASE}/vpc/v1/networks?projectId=${PROJECT}&pageSize=${PAGE_SIZE}`, H),
               want: b => Array.isArray(b.networks) && b.networks.length > 0 };
    case 'geo_get':   // контроль: та же дорога, БЕЗ проверки прав на уровне проекта
      return { r: http.get(`${BASE}/geo/v1/regions/${REGION}`, H), want: b => b.id === REGION };
    case 'group_create': {
      // ДЕШЁВОЕ создание: единственный create в дереве БЕЗ квоты и БЕЗ вызова
      // соседей на пути запроса — одна вставка в operations и возврат. Всё
      // остальное уезжает в воркер. Квоты нет ⇒ нет и сериализации на строке
      // учёта, которой связаны все прочие create (см. отчёт).
      const acct = __ENV.ACCOUNT_ID || FIXTURES.accountAId;
      return { r: http.post(`${BASE}/iam/v1/groups`,
                 JSON.stringify({ accountId: acct, name: uniq('ld-grp').toLowerCase() }), H),
               want: b => b.id && b.metadata };
    }
    case 'sg_create': // дешёвое создание: без соседей, работа уезжает в воркер
      return { r: http.post(`${BASE}/vpc/v1/securityGroups`,
                 JSON.stringify({ projectId: PROJECT, networkId: NETWORK, name: uniq('ld-sg') }), H),
               want: b => b.id && b.metadata && b.metadata.securityGroupId };
    case 'net_create': // дорогое создание: сага целиком СИНХРОННО (done=true)
      return { r: http.post(`${BASE}/vpc/v1/networks`,
                 JSON.stringify({ projectId: PROJECT, name: uniq('ld-net'), ipv4CidrBlocks: ['10.96.0.0/16'] }), H),
               want: b => b.done === true && b.metadata && b.metadata.networkId };
    case 'subnet_create': { // дорогое создание С ВЫЗОВОМ СОСЕДА (geo) на пути запроса
      // Адресный блок разводится по VU и итерации, а не случайно: у подсетей
      // сети действует запрет пересечения, и случайный выбор столкнулся бы сам
      // с собой тем вернее, чем выше подача, — отказ выглядел бы как отказ
      // продукта под нагрузкой, хотя это исчерпание пространства имён пробы.
      const n = (__VU * 4096 + __ITER) % 65536;
      const cidr = `10.${64 + (n >> 8)}.${n & 255}.0/24`;
      return { r: http.post(`${BASE}/vpc/v1/subnets`,
                 JSON.stringify({ projectId: PROJECT, networkId: NETWORK, name: uniq('ld-sn'),
                                  zoneId: ZONE, ipv4CidrPrimary: cidr }), H),
               want: b => b.done === true && b.metadata && b.metadata.subnetId };
    }
    default: throw new Error(`неизвестная полоса OP=${OP}`);
  }
}

export default function () {
  const { r, want } = request();
  lat.add(r.timings.duration);
  const ok = r.status >= 200 && r.status < 300;
  errRate.add(!ok);
  if (!ok) {
    if (r.status >= 500 || r.status === 0) httpFailed.add(1); else httpDenied.add(1);
    return;
  }
  okCount.add(1);
  let body = null;
  try { body = r.json(); } catch (e) { body = null; }
  // 2xx с телом не того вида — это НЕ успех: считаем отдельно, а не молча.
  if (!body || !want(body)) wrongShape.add(1);
  check(r, { 'status 2xx': () => ok });
}
