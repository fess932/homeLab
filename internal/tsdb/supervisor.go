package tsdb

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

const (
	StateStarting   = "starting"
	StateRunning    = "running"
	StateRestarting = "restarting"
	StateStopped    = "stopped"
	StateFailed     = "failed"
)

const readyTimeout = 90 * time.Second

var ErrGaveUp = errors.New("TSDB не запустилась после 5 попыток подряд")

type Options struct {
	Binary          string
	DataDir         string
	Listen          string
	Retention       string
	ScrapeConfig    string
	MinFreeDisk     int64
	MemoryPercent   int
	MaxScrapeSize   string
	SeriesPerTarget int
}

func (o Options) Args() []string {
	return append([]string{
		"-storageDataPath=" + o.DataDir,
		"-httpListenAddr=" + o.Listen,
		"-retentionPeriod=" + o.Retention,
		"-promscrape.config=" + o.ScrapeConfig,
		"-promscrape.config.strictParse",
		"-promscrape.maxScrapeSize=" + o.MaxScrapeSize,
		fmt.Sprintf("-promscrape.seriesLimitPerTarget=%d", o.SeriesPerTarget),
		fmt.Sprintf("-storage.minFreeDiskSpaceBytes=%d", o.MinFreeDisk),
		fmt.Sprintf("-memory.allowedPercent=%d", o.MemoryPercent),
		"-search.maxQueryDuration=5s",
		"-search.maxConcurrentRequests=8",
		"-search.maxQueueDuration=5s",
		"-search.maxResponseSeries=100",
		"-search.maxPointsPerTimeseries=1100",
		"-search.maxUniqueTimeseries=50000",
		"-search.maxSamplesPerQuery=200000000",
		"-search.maxMemoryPerQuery=128MiB",
		"-search.latencyOffset=0s",
		"-selfScrapeInterval=0",
		"-http.shutdownDelay=0s",
		"-loggerFormat=json",
		"-loggerLevel=WARN",
	}, platformArgs...)
}

type Status struct {
	State    string
	Restarts int
	Error    string
	Since    time.Time
}

type Supervisor struct {
	opts    Options
	log     *slog.Logger
	backoff []time.Duration
	stable  time.Duration
	health  func(ctx context.Context) error

	mu      sync.Mutex
	status  Status
	cmd     *exec.Cmd
	onReady []func()
}

func NewSupervisor(opts Options, log *slog.Logger) *Supervisor {
	s := &Supervisor{
		opts:    opts,
		log:     log,
		backoff: []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second},
		stable:  time.Minute,
		status:  Status{State: StateStarting, Since: time.Now()},
	}
	client := &http.Client{Timeout: 2 * time.Second}
	s.health = func(ctx context.Context) error {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+opts.Listen+"/health", nil)
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("health: %s", resp.Status)
		}
		return nil
	}
	return s
}

func (s *Supervisor) OnReady(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onReady = append(s.onReady, fn)
}

func (s *Supervisor) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

func (s *Supervisor) Ready() bool { return s.Status().State == StateRunning }

func (s *Supervisor) setState(state, errText string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status.State != state {
		s.status.Since = time.Now()
	}
	s.status.State, s.status.Error = state, errText
}

func (s *Supervisor) Run(ctx context.Context) error {
	failures := 0
	for {
		started := time.Now()
		err := s.runOnce(ctx)
		if ctx.Err() != nil {
			s.setState(StateStopped, "")
			return nil
		}
		if time.Since(started) >= s.stable {
			failures = 0
		}
		failures++
		msg := "процесс TSDB завершился"
		if err != nil {
			msg = err.Error()
		}
		s.log.Error("tsdb exited", "err", msg, "failures", failures)
		if failures >= len(s.backoff) {
			s.setState(StateFailed, msg)
			return fmt.Errorf("%w: %s", ErrGaveUp, msg)
		}
		s.mu.Lock()
		s.status.Restarts++
		s.mu.Unlock()
		s.setState(StateRestarting, msg)
		select {
		case <-ctx.Done():
			s.setState(StateStopped, "")
			return nil
		case <-time.After(s.backoff[failures-1]):
		}
	}
}

func (s *Supervisor) runOnce(ctx context.Context) error {
	cmd := exec.Command(s.opts.Binary, s.opts.Args()...)
	cmd.Env = []string{"TZ=UTC"}
	setProcAttr(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("запуск %s: %w", s.opts.Binary, err)
	}
	bindToParent(cmd.Process)
	s.mu.Lock()
	s.cmd = cmd
	s.mu.Unlock()
	logsDone := make(chan struct{})
	go func() {
		defer close(logsDone)
		s.pipeLogs(stdout)
	}()
	exited := make(chan error, 1)
	go func() {
		<-logsDone
		exited <- cmd.Wait()
	}()

	readyCtx, cancelReady := context.WithCancel(ctx)
	defer cancelReady()
	go s.waitReady(readyCtx)

	select {
	case err := <-exited:
		return exitErr(err)
	case <-ctx.Done():
		s.terminate(cmd, exited)
		return nil
	}
}

func (s *Supervisor) waitReady(ctx context.Context) {
	t := time.NewTicker(200 * time.Millisecond)
	defer t.Stop()
	deadline := time.After(readyTimeout)
	for {
		select {
		case <-ctx.Done():
			return
		case <-deadline:
			s.log.Error("tsdb not ready in time, killing", "timeout", readyTimeout)
			_ = s.Signal(os.Kill)
			return
		case <-t.C:
		}
		hctx, cancel := context.WithTimeout(ctx, time.Second)
		err := s.health(hctx)
		cancel()
		if err == nil {
			s.setState(StateRunning, "")
			s.log.Info("tsdb ready")
			s.mu.Lock()
			hooks := append([]func(){}, s.onReady...)
			s.mu.Unlock()
			for _, h := range hooks {
				go h()
			}
			return
		}
	}
}

func (s *Supervisor) terminate(cmd *exec.Cmd, exited <-chan error) {
	stopProcess(cmd.Process)
	select {
	case <-exited:
	case <-time.After(45 * time.Second):
		s.log.Warn("tsdb did not stop in time, killing")
		killProcessGroup(cmd.Process)
		<-exited
	}
}

func (s *Supervisor) Signal(sig os.Signal) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd == nil || s.cmd.Process == nil {
		return errors.New("TSDB не запущена")
	}
	return s.cmd.Process.Signal(sig)
}

func (s *Supervisor) pipeLogs(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var e struct{ Level, Caller, Msg string }
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil || e.Msg == "" {
			s.log.Info(sc.Text())
			continue
		}
		level := slog.LevelInfo
		switch e.Level {
		case "error", "fatal", "panic":
			level = slog.LevelError
		case "warn":
			level = slog.LevelWarn
		}
		s.log.Log(context.Background(), level, e.Msg, "caller", e.Caller)
	}
	if err := sc.Err(); err != nil {
		s.log.Warn("tsdb log stream", "err", err)
		_, _ = io.Copy(io.Discard, r)
	}
}

func exitErr(err error) error {
	if ee, ok := errors.AsType[*exec.ExitError](err); ok {
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return fmt.Errorf("TSDB завершена сигналом %s", ws.Signal())
		}
		return fmt.Errorf("TSDB завершилась с кодом %d", ee.ExitCode())
	}
	return err
}
