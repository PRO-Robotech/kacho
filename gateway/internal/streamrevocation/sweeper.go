// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package streamrevocation — ПЕРЕПРОС состояния удостоверения на ОТКРЫТЫХ
// соединениях края (kacho#1410).
//
// # Предмет: контроль на выдаче, но не на предъявлении
//
// Граница отзыва удостоверения объявлена сроком кэша интроспекции и держится
// тем, что КАЖДЫЙ запрос спрашивает состояние заново. Длинное соединение
// запросов больше не делает: проверка была один раз, при открытии, — и дальше
// поток живёт до своего бюджета. Для него объявленная граница не действует
// вовсе, а действует срок соединения, который шире её во столько раз, во
// сколько бюджет больше срока кэша.
//
// Это ровно тот класс, который `security.md` называет «контроль, действующий на
// ВЫДАЧЕ, но не на ПРЕДЪЯВЛЕНИИ»: выданного нового нет, а предъявленное
// проходит до истечения срока. Открытый поток и есть предъявленное.
//
// # Чем это НЕ является: отзыв ПРАВ уже доезжает
//
// Соседняя полоса (kacho#1022, `pkg/subjectchange`) закрывает потоки по отзыву
// ПРАВ: имена приезжают журналом смены субъекта. Отзыв УДОСТОВЕРЕНИЯ строк в
// тот журнал не пишет, поэтому переиспользовать его нельзя — и это разные
// предметы, а не два имени одного.
//
// # Что спрашивается и у кого
//
// Спрашивается НАШ авторитет (`InternalSessionRevocationsService`) — тот, куда
// пишут выход человека и принудительный выход администратора. Полос у вопроса
// две, и они разделены ТАК ЖЕ, как на пути запроса: удостоверение с
// идентификатором спрашивается по нему (`IsRevoked`), браузерная сессия — по
// паре (человек, момент аутентификации) (`SessionCutoffOf`). Расхождение полос
// здесь было бы расхождением одного механизма с самим собой.
//
// # Чего этот перепрос НЕ спрашивает, и это названо, а не подразумевается
//
// Отзывов ПРОВАЙДЕРА он не видит. На пути запроса их спрашивают интроспекцией,
// а она требует предъявленного удостоверения ЦЕЛИКОМ; держать его в памяти края
// весь срок соединения значило бы завести хранилище носителей на девяносто
// секунд ради вопроса, который наш авторитет и так закрывает для выхода и
// принудительного выхода. Истечение срока самого удостоверения тоже не предмет
// этого перепроса: истечение — не отзыв, у него своя величина и свой читатель.
package streamrevocation

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
	"github.com/PRO-Robotech/kacho/gateway/internal/subscriptionstream"
)

// Streams — реестр открытых длинных соединений края.
//
// Реализуется проекцией потока (`subscriptionstream.Handler`). Порт объявлен
// ЗДЕСЬ, у вызывающего: читатель зависит от вопроса, который задаёт, а не от
// того, кто на него отвечает.
type Streams interface {
	// OpenStreams — снимок открытых потоков вместе с удостоверением каждого.
	OpenStreams() []subscriptionstream.OpenStream
	// CloseAll — закрыть ВСЕ открытые потоки; возвращает их число. Путь
	// fail-closed: см. [Sweeper.failClosed].
	CloseAll() int
}

// Authority — НАШ авторитет отзыва, спрошенный про ПРЕДЪЯВЛЕННОЕ.
//
// Оба вопроса объявлены ОДНИМ портом, а не двумя полями: их задаёт один и тот
// же авторитет одному и тому же соседу, и разъехаться они могут только в одну
// сторону — когда провязали половину. Половина, которую можно провязать
// отдельно, есть половина, которую можно ЗАБЫТЬ отдельно, и забытая выглядела
// бы как исполняемый контроль ровно для той полосы, что осталась.
//
// Оба вопроса уже объявлены полосой запроса (`middleware`), и здесь они не
// переобъявляются: третье описание того же вопроса разошлось бы с первыми
// двумя молча.
type Authority interface {
	middleware.SessionRevocationsReader
	middleware.SessionCutoffReader
}

// verdict — что авторитет ответил про одно удостоверение.
//
// Исходы держатся ВРОЗЬ, потому что читателю журнала они говорят разное и
// лечатся противоположно: слитые в «годен / не годен», они дали бы либо мягкий
// проход на молчащем авторитете, либо закрытие каждого потока в окне раската.
type verdict int

const (
	// verdictLive — спросили, удостоверение действительно.
	verdictLive verdict = iota
	// verdictRevoked — спросили, удостоверение отозвано. Поток закрываем.
	verdictRevoked
	// verdictUnanswered — авторитет не ответил. НЕ «не отозван»: неполученный
	// ответ не есть «да», и такой перепрос не подтверждает чтения отзыва.
	verdictUnanswered
	// verdictUnaskable — спросить нечем: удостоверение себя не назвало.
	verdictUnaskable
	// verdictUnsupported — авторитет ЖИВ и такого вопроса не предлагает (окно
	// раската). Проходим, громко, — той же посадкой, что на пути запроса:
	// считать это отказом значило бы закрывать потоки всего флота на всё окно
	// раската, при том что состояние сходится само.
	verdictUnsupported
)

// Config — что приносит композиционный корень края.
type Config struct {
	// Streams — реестр открытых потоков. ОБЯЗАТЕЛЕН: ноль означал бы читателя,
	// которому нечего закрывать, и несделанная провязка была бы НЕОТЛИЧИМА от
	// сделанной — весь корпус проб остаётся зелёным, код собирается, а отзыв не
	// доезжает вовсе.
	Streams Streams

	// Authority — наш авторитет отзыва. ОБЯЗАТЕЛЕН по тому же доводу.
	Authority Authority

	// Interval — период перепроса. ОБЯЗАТЕЛЕН и умолчания не имеет: это и есть
	// ОБЪЯВЛЕННОЕ ОКНО отзыва для открытого соединения, а величина, которую
	// никто не выбирал, не обсуждаема и не сужаема — она обнаруживается первым
	// пережившим отзыв потоком в бою.
	Interval time.Duration

	// StaleAfter — сколько край вправе ДЕРЖАТЬ потоки, не подтвердив чтения
	// отзыва. Обязателен.
	//
	// Это окно FAIL-CLOSED, и оно есть следствие решения, а не срока жизни
	// соединения: отсчитывается от последнего удачного перепроса. Обязано
	// превосходить [Config.Interval] — иначе один пропущенный перепрос
	// объявляется аварией.
	StaleAfter time.Duration

	// Now — часы. Ноль резолвится в [time.Now]; проба подставляет свои, потому
	// что срок обязан быть свойством решения, а не занятости машины.
	Now func() time.Time

	// Logger — журнал процесса. Обязателен: застрявший отзыв обязан быть виден,
	// а не только не работать.
	Logger *slog.Logger
}

// Sweeper перепрашивает состояние удостоверения на открытых потоках и закрывает
// те, чьё удостоверение отозвано.
type Sweeper struct {
	cfg      Config
	lastGood time.Time
}

// New собирает читателя и судит объявление.
//
// Отказ на сборке, а не первым отказом в бою: величина, при которой перепрос не
// наступает или fail-closed не наступает никогда, — ошибка посадки, и
// обнаруживать её тогда, когда она уже кому-то стоила доступа, поздно.
func New(cfg Config) (*Sweeper, error) {
	if cfg.Streams == nil {
		return nil, fmt.Errorf("streamrevocation: реестр открытых потоков не назван — " +
			"перепросу нечего закрывать, и несделанная провязка была бы неотличима от сделанной")
	}
	if cfg.Authority == nil {
		return nil, fmt.Errorf("streamrevocation: авторитет отзыва не назван — " +
			"перепрос без него молчит ровно так же, как перепрос, которому нечего сообщить")
	}
	if cfg.Logger == nil {
		return nil, fmt.Errorf("streamrevocation: журнал процесса не назван — " +
			"молчащий авторитет обязан быть виден, а не только не работать")
	}
	if cfg.Interval <= 0 {
		return nil, fmt.Errorf("streamrevocation: период перепроса %v — это и есть объявленное "+
			"окно отзыва для открытого соединения, и умолчания у него нет", cfg.Interval)
	}
	if cfg.StaleAfter <= cfg.Interval {
		return nil, fmt.Errorf("streamrevocation: StaleAfter %v не превосходит Interval %v — "+
			"срок, исчерпываемый одним пропущенным перепросом, объявляет аварией всякую заминку",
			cfg.StaleAfter, cfg.Interval)
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	// Срок неподтверждённого чтения отсчитывается от СТАРТА, а не от нуля
	// времени: процесс, чей перепрос не заработал ни разу, обязан закрывать
	// потоки — но по истечении того же объявленного срока, а не немедленно.
	return &Sweeper{cfg: cfg, lastGood: cfg.Now()}, nil
}

// Interval — объявленное окно отзыва для открытого соединения. Для самоотчёта.
func (s *Sweeper) Interval() time.Duration { return s.cfg.Interval }

// StaleAfter — объявленный срок неподтверждённого чтения. Для самоотчёта.
func (s *Sweeper) StaleAfter() time.Duration { return s.cfg.StaleAfter }

// Run блокирует до отмены контекста. Зовётся в своей горутине.
//
// РЕПЛИКИ: на-реплику — реплика держит СВОИ потоки и никаких чужих, а закрыть
// поток можно только там, где он открыт. Разведи петлю выбором одной реплики —
// и потоки невыбранных пережили бы отзыв, причём молча: у остальных наблюдение
// зелёное. Дубль невозможен by construction (чужих потоков в реестре нет), а
// повторный вопрос авторитету стоит одного чтения и ничего не меняет.
func (s *Sweeper) Run(ctx context.Context) {
	t := time.NewTicker(s.cfg.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.Sweep(ctx)
		}
	}
}

// Sweep исполняет РОВНО ОДИН перепрос.
//
// Метод публичный по тем же двум доводам, что и у читателя смены субъекта. Он и
// есть единица работы, которую [Sweeper.Run] повторяет по расписанию; и им
// ставится сквозной вопрос — проба, спрашивающая «отзыв доехал ли до открытого
// потока», обязана читать наблюдаемое ПОСЛЕ того, как перепрос закончился, а не
// пока он идёт.
func (s *Sweeper) Sweep(ctx context.Context) {
	open := s.cfg.Streams.OpenStreams()

	// Потоки группируются ПО УДОСТОВЕРЕНИЮ: у человека столько потоков, сколько
	// вкладок, и спрашивать авторитет по разу на вкладку значило бы умножать
	// нагрузку на соседа на число вкладок, ничего не узнавая сверх первого
	// ответа.
	byCred := make(map[principalmeta.Credential][]subscriptionstream.OpenStream, len(open))
	for _, st := range open {
		byCred[st.Credential] = append(byCred[st.Credential], st)
	}

	var closed, asked, unanswered, unaskable, unsupported int
	for cred, streams := range byCred {
		switch s.ask(ctx, cred) {
		case verdictRevoked:
			for _, st := range streams {
				st.Close()
			}
			closed += len(streams)
			asked++
		case verdictLive:
			asked++
		case verdictUnsupported:
			asked++
			unsupported += len(streams)
		case verdictUnanswered:
			unanswered++
		case verdictUnaskable:
			unaskable += len(streams)
		}
	}

	if unanswered == 0 {
		// Подтверждением считается перепрос, на котором авторитет не отказал НИ
		// РАЗУ. Пустой реестр тоже подтверждение: неподтверждённым остаётся
		// чтение, а не наличие потоков, — и не будь этого, первый же поток
		// после тихого часа закрывался бы сроком, накопленным без единого
		// вопроса.
		s.lastGood = s.cfg.Now()
	}

	if closed > 0 || unaskable > 0 || unsupported > 0 {
		s.cfg.Logger.Info("subscription credential recheck",
			"streams", len(open), "credentials", len(byCred),
			"streams_closed", closed,
			// Три величины держатся ВРОЗЬ: слитые в одну, они смешали бы
			// исполненный контроль с двумя разными способами его не исполнить,
			// и по смешанной величине нельзя принять ни одного решения.
			"streams_unaskable", unaskable,
			"credentials_unanswered", unanswered,
			"streams_cutoff_unsupported", unsupported)
	}

	if unaskable > 0 {
		// ДЕФЕКТ, а не норма: поток, чьё удостоверение себя не назвало, отзывом
		// удостоверения закрыть нельзя ни при каких условиях. Тревога вешается
		// только на него — норма её не поднимает никогда, иначе жалоба звучала
		// бы в штатной работе и её перестали бы читать вместе с настоящей
		// находкой.
		s.cfg.Logger.Warn("open subscription stream carries no askable credential",
			"streams_unaskable", unaskable,
			"predicate", "исчезает, когда полоса аутентификации назовёт удостоверение потока")
	}
	if unsupported > 0 {
		s.cfg.Logger.Error("session revocation not enforced on open subscription streams: "+
			"the authority does not offer this question (image skew)",
			"streams_cutoff_unsupported", unsupported,
			"predicate", "исчезает, когда служба прав докатится до того же дерева")
	}
	if unanswered > 0 {
		s.cfg.Logger.Warn("subscription credential recheck unanswered",
			"credentials_unanswered", unanswered,
			"stale_for", s.cfg.Now().Sub(s.lastGood).String(),
			"stale_after", s.cfg.StaleAfter.String())
		s.failClosed()
	}
}

// ask задаёт авторитету ТОТ вопрос, который про это удостоверение вообще есть.
//
// Разделение полос — то же, что на пути запроса, и это не совпадение: полосы
// одного механизма, объявляющие разное, расходятся молча, а решал бы это
// расхождение никто.
func (s *Sweeper) ask(ctx context.Context, c principalmeta.Credential) verdict {
	switch {
	case c.JTI != "":
		revoked, err := s.cfg.Authority.IsSessionRevoked(ctx, c.JTI)
		if err != nil {
			return verdictUnanswered
		}
		if revoked {
			return verdictRevoked
		}
		return verdictLive

	case c.UserID != "":
		cutoff, found, err := s.cfg.Authority.SessionCutoffOf(ctx, c.UserID)
		switch {
		case errors.Is(err, middleware.ErrSessionCutoffUnsupported):
			return verdictUnsupported
		case err != nil:
			return verdictUnanswered
		case !found:
			// Пустой ответ означает ПУСТО. Человек, которого никто не отзывал,
			// продолжает читать свой поток.
			return verdictLive
		case c.AuthenticatedAt.IsZero():
			// Отсечка есть, а сравнивать не с чем: провайдер момента не назвал.
			// Пропустить — значит дать обходить отзыв ОТСУТСТВИЕМ поля в чужом
			// ответе. Та же посадка принята полосой запроса.
			return verdictRevoked
		case c.AuthenticatedAt.After(cutoff):
			// Отсечка действует ВПЕРЁД и включает свой момент: сессия,
			// аутентифицировавшаяся РОВНО в него, недействительна. Иначе
			// принудительный выход, совпавший по метке со входом, не подействовал
			// бы — а совпадают они тем чаще, чем грубее разрешение метки.
			return verdictLive
		default:
			return verdictRevoked
		}

	default:
		return verdictUnaskable
	}
}

// failClosed закрывает ВСЕ потоки, когда перепрос не подтверждался дольше
// объявленного срока.
//
// Неполученный ответ авторитета не есть «удостоверений никто не отзывал». Кого
// именно закрывать, реплика в этом состоянии знать не может: ответ приезжал как
// раз тем перепросом, которого нет, — поэтому радиус широкий, и он назван.
//
// Закрывает на КАЖДОМ перепросе за сроком, а не однажды: клиент,
// переоткрывшийся в середине аварии, обязан закрыться тоже. На пустом реестре
// это стоит нуля.
func (s *Sweeper) failClosed() {
	stale := s.cfg.Now().Sub(s.lastGood)
	if stale < s.cfg.StaleAfter {
		return
	}
	closed := s.cfg.Streams.CloseAll()
	if closed == 0 {
		// Ноль закрытых — не событие: держать было нечего. Печатать это на
		// каждом перепросе значило бы утопить настоящую находку в шуме.
		return
	}
	s.cfg.Logger.Error("subscription credential recheck is stale: closing every open stream (fail-closed)",
		"stale_for", stale.String(), "stale_after", s.cfg.StaleAfter.String(), "streams_closed", closed)
}
