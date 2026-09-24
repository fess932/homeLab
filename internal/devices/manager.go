// Package devices держит циклы опроса устройств и публикует их значения на
// внутреннем /metrics. Как опрашивать конкретное устройство, решает драйвер из
// реестра drivers по полю kind.
package devices

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fess932/homeLab/drivers"
	"github.com/fess932/homeLab/internal/model"
	"github.com/fess932/homeLab/internal/secrets"
)

// SecretFunc расшифровывает секрет устройства по id.
type SecretFunc func(ctx context.Context, id string) (secrets.Payload, error)

// У каждого устройства свой мьютекс статуса: цикл опроса не трогает общий мьютекс
// менеджера, поэтому Sync может ждать остановки цикла, держа общий.
type tracker struct {
	dev     model.Device
	mu      sync.Mutex
	status  model.DeviceStatus
	session string // что драйвер запомнил после успешного опроса (например, версию протокола)
	cancel  context.CancelFunc
	done    chan struct{}
}

// Manager держит по циклу опроса на каждое включённое устройство и последние значения в памяти.
type Manager struct {
	Secret SecretFunc
	Log    *slog.Logger

	mu       sync.Mutex
	ctx      context.Context
	cancel   context.CancelFunc
	trackers map[string]*tracker
}

func (m *Manager) Start() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ctx, m.cancel = context.WithCancel(context.Background())
	m.trackers = map[string]*tracker{}
}

// Stop останавливает все циклы опроса и дожидается их завершения.
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel == nil {
		return
	}
	m.cancel()
	for _, t := range m.trackers {
		m.stop(t)
	}
}

// Sync приводит циклы опроса к списку устройств: изменённые перезапускает, удалённые останавливает.
func (m *Manager) Sync(devices []model.Device) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ctx == nil || m.ctx.Err() != nil {
		return
	}
	want := map[string]model.Device{}
	for _, d := range devices {
		want[d.ID] = d
	}
	for id, t := range m.trackers {
		if d, ok := want[id]; ok && d.Revision == t.dev.Revision {
			continue
		}
		m.stop(t)
		delete(m.trackers, id)
	}
	for id, d := range want {
		if _, ok := m.trackers[id]; ok {
			continue
		}
		t := &tracker{dev: d, status: model.DeviceStatus{State: model.StatePending, Readings: []model.Reading{}}}
		if !*d.Enabled {
			t.status.State = model.StateDisabled
		}
		m.trackers[id] = t
		if *d.Enabled {
			m.run(t)
		}
	}
}

func (m *Manager) stop(t *tracker) {
	if t.cancel != nil {
		t.cancel()
		<-t.done
	}
}

func (m *Manager) run(t *tracker) {
	ctx, cancel := context.WithCancel(m.ctx)
	t.cancel, t.done = cancel, make(chan struct{})
	interval := time.Duration(t.dev.IntervalS) * time.Second
	go func() {
		defer close(t.done)
		// Небольшой случайный сдвиг, чтобы устройства не опрашивались одновременно.
		first := time.NewTimer(time.Duration(rand.Int64N(int64(min(interval, 3*time.Second)))))
		defer first.Stop()
		select {
		case <-ctx.Done():
			return
		case <-first.C:
		}
		tick := time.NewTicker(interval)
		defer tick.Stop()
		for {
			m.pollOnce(ctx, t)
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
		}
	}()
}

func (m *Manager) pollOnce(ctx context.Context, t *tracker) {
	t.mu.Lock()
	session := t.session
	t.mu.Unlock()
	dev := t.dev
	st, next := m.poll(ctx, dev, session, nil)
	if ctx.Err() != nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	prev := t.status
	if st.State == model.StateUp {
		t.session = next
	} else {
		// После ошибки драйвер начинает с чистого листа: устройство могли обновить.
		t.session = ""
		st.LastSuccess = prev.LastSuccess
		if prev.State != model.StateDown {
			m.Log.Warn("device poll failed", "device", dev.ID, "name", dev.Name, "err", st.Error)
		}
	}
	t.status = st
}

func (m *Manager) poll(ctx context.Context, d model.Device, session string, inline *secrets.Payload) (model.DeviceStatus, string) {
	start := time.Now()
	st := model.DeviceStatus{State: model.StateDown, LastAttempt: model.TimePtr(start), Readings: []model.Reading{}}
	drv, ok := drivers.Get(d.Kind)
	if !ok {
		st.ErrorKind, st.Error = drivers.KindProtocol, "неизвестный тип устройства "+d.Kind
		return st, ""
	}
	var secret secrets.Payload
	if inline != nil {
		secret = *inline
	} else if d.SecretID != nil {
		var err error
		if secret, err = m.Secret(ctx, *d.SecretID); err != nil {
			st.ErrorKind, st.Error = drivers.KindAuth, "учётные данные устройства недоступны: "+err.Error()
			return st, ""
		}
	}
	pctx, cancel := context.WithTimeout(ctx, time.Duration(d.TimeoutS)*time.Second)
	defer cancel()
	res, err := drv.Poll(pctx, drivers.Target{DeviceID: d.ID, Address: d.Address, Config: d.Config, Secret: secret, Session: session})
	st.DurationMS = new(float64(time.Since(start).Microseconds()) / 1000)
	if err != nil {
		st.ErrorKind, st.Error = drivers.Classify(err)
		return st, ""
	}
	if res.Readings != nil {
		st.Readings = res.Readings
	}
	st.State, st.LastSuccess, st.Protocol = model.StateUp, model.TimePtr(time.Now()), res.Protocol
	return st, res.Session
}

// Test опрашивает черновик устройства один раз, ничего не сохраняя. inline — ключ,
// переданный прямо в запросе вместо сохранённого секрета.
func (m *Manager) Test(ctx context.Context, in model.DeviceInput, inline *secrets.Payload) model.DeviceStatus {
	st, _ := m.poll(ctx, model.Device{DeviceInput: in}, "", inline)
	return st
}

func (m *Manager) Status(id string) model.DeviceStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.trackers[id]; ok {
		t.mu.Lock()
		defer t.mu.Unlock()
		return t.status
	}
	return model.DeviceStatus{State: model.StatePending, Readings: []model.Reading{}}
}

// WritePrometheus дописывает значения устройств к внутреннему /metrics HomeDeck, откуда
// их забирает VictoriaMetrics. Значения есть только у отвечающих устройств: пропуск
// опроса виден на графике разрывом, а не последним известным числом.
func (m *Manager) WritePrometheus(w io.Writer) {
	m.mu.Lock()
	ids := make([]string, 0, len(m.trackers))
	for id, t := range m.trackers {
		if *t.dev.Enabled {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	type snap struct {
		dev model.Device
		st  model.DeviceStatus
	}
	list := make([]snap, len(ids))
	for i, id := range ids {
		t := m.trackers[id]
		t.mu.Lock()
		list[i] = snap{t.dev, t.status}
		t.mu.Unlock()
	}
	m.mu.Unlock()

	var b strings.Builder
	for _, s := range list {
		if s.st.State == model.StatePending {
			continue
		}
		base := deviceLabels(s.dev)
		up := 0
		if s.st.State == model.StateUp {
			up = 1
		}
		fmt.Fprintf(&b, "homedeck_device_up{%s} %d\n", base, up)
		if s.st.DurationMS != nil {
			fmt.Fprintf(&b, "homedeck_device_poll_duration_seconds{%s} %s\n", base, formatFloat(*s.st.DurationMS/1000))
		}
		if up == 0 {
			continue
		}
		for _, r := range s.st.Readings {
			if r.State != "" {
				fmt.Fprintf(&b, "homedeck_device_state{%s,key=%s,value=%s} 1\n", base, quote(r.Key), quote(r.State))
				if r.Value == 0 {
					continue
				}
			}
			fmt.Fprintf(&b, "homedeck_device_value{%s,key=%s,unit=%s} %s\n", base, quote(r.Key), quote(r.Unit), formatFloat(r.Value))
		}
	}
	_, _ = io.WriteString(w, b.String())
}

func deviceLabels(d model.Device) string {
	keys := make([]string, 0, len(d.Labels))
	for k := range d.Labels {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var b strings.Builder
	b.WriteString("device_id=" + quote(d.ID) + ",device=" + quote(d.Name))
	for _, k := range keys {
		b.WriteString("," + k + "=" + quote(d.Labels[k]))
	}
	return b.String()
}

func quote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)
	return `"` + r.Replace(s) + `"`
}

func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'g', -1, 64)
}
