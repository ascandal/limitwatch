package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/ascandal/limitwatch/internal/domain"
	"github.com/ascandal/limitwatch/internal/ingest"
)

var ErrQueueFull = errors.New("ingest queue full")

// EvalReport is emitted for each rule evaluation (used by alarm manager).
type EvalReport struct {
	RuleID int64
	Signal string
	Entity string
	At     time.Time
	Value  float64
	Active bool
	Latch  bool
}

// Engine processes points against rules and emits traces and reports.
type Engine struct {
	logger *slog.Logger

	queue   chan ingest.Point
	workers int

	mu sync.RWMutex
	// compiled rules indexed by signal name
	idx map[string][]*CompiledRule
	// per rule+entity state
	state map[stateKey]*ruleState

	trace bool
	// optional internal channel of evaluation events
	traces chan Trace

	// always enabled; consumed by manager
	reports chan EvalReport
}

type stateKey struct {
	RuleID int64
	Entity string
}

type Trace struct {
	RuleID int64
	Signal string
	Entity string
	At     time.Time
	Value  float64
	Active bool   // current state after evaluation
	Detail string // human-readable note (trip/clear/hold)
}

// Rule configs
type ThresholdConfig struct {
	Lo         *float64 `json:"lo,omitempty"`
	Hi         *float64 `json:"hi,omitempty"`
	Hysteresis float64  `json:"hysteresis,omitempty"`
	Deadband   float64  `json:"deadband,omitempty"`
	DurationMs int64    `json:"duration_ms,omitempty"`
}

type DurationConfig struct {
	Op         string  `json:"op"` // ">" or "<"
	Threshold  float64 `json:"threshold"`
	DurationMs int64   `json:"duration_ms"`
	Hysteresis float64 `json:"hysteresis,omitempty"` // for clear
}

type ROCConfig struct {
	WindowMs int64   `json:"window_ms"`
	GT       float64 `json:"gt"`                 // rate greater-than threshold (units per second)
	Abs      bool    `json:"abs"`                // use absolute value of rate
	Deadband float64 `json:"deadband,omitempty"` // ignore tiny deltas
}

type CompiledRule struct {
	Rule domain.Rule
	kind domain.RuleType
	thr  *ThresholdConfig
	dur  *DurationConfig
	roc  *ROCConfig
}

type ruleState struct {
	Active bool
	// Threshold fields
	lastValue float64
	lastTime  time.Time
	mode      string // "lo" or "hi" for threshold
	// Duration tracking
	condStart time.Time
	// ROC window
	win []ingest.Point // sliding window, newest at end
}

func New(rules []domain.Rule, queueSize, workers int, trace bool, logger *slog.Logger) (*Engine, error) {
	if queueSize <= 0 {
		queueSize = 1024
	}

	if workers <= 0 {
		workers = 1
	}

	e := &Engine{
		logger:  logger,
		queue:   make(chan ingest.Point, queueSize),
		workers: workers,
		idx:     map[string][]*CompiledRule{},
		state:   map[stateKey]*ruleState{},
		trace:   trace,
		reports: make(chan EvalReport, 2048),
	}

	if trace {
		e.traces = make(chan Trace, 1024)

		go func() {
			for t := range e.traces {
				logger.Info(
					"trace",
					"rule", t.RuleID,
					"signal", t.Signal,
					"entity", t.Entity,
					"value", t.Value,
					"active", t.Active,
					"detail", t.Detail,
				)
			}
		}()
	}

	for i := range rules {
		if !rules[i].Enabled {
			continue
		}

		cr, err := compileRule(rules[i])
		if err != nil {
			return nil, fmt.Errorf("compile rule %d: %w", rules[i].ID, err)
		}

		e.idx[rules[i].Signal] = append(e.idx[rules[i].Signal], cr)
	}

	return e, nil
}

func (e *Engine) Start(ctx context.Context) {
	for i := 0; i < e.workers; i++ {
		go e.worker(ctx)
	}
}

func (e *Engine) QueueLen() int {
	return len(e.queue)
}

func (e *Engine) Enqueue(p ingest.Point) error {
	select {
	case e.queue <- p:
		return nil
	default:
		return ErrQueueFull
	}
}

// Reports returns a receive-only channel of evaluation results.
func (e *Engine) Reports() <-chan EvalReport {
	return e.reports
}

func (e *Engine) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case p := <-e.queue:
			e.evaluatePoint(p)
		}
	}
}

func compileRule(r domain.Rule) (*CompiledRule, error) {
	cr := &CompiledRule{Rule: r, kind: r.Type}

	switch r.Type {
	case domain.RuleThreshold:
		var cfg ThresholdConfig

		if err := json.Unmarshal(r.Config, &cfg); err != nil {
			return nil, err
		}

		cr.thr = &cfg
	case domain.RuleDuration:
		var cfg DurationConfig

		if err := json.Unmarshal(r.Config, &cfg); err != nil {
			return nil, err
		}

		cr.dur = &cfg
	case domain.RuleROC:
		var cfg ROCConfig

		if err := json.Unmarshal(r.Config, &cfg); err != nil {
			return nil, err
		}

		cr.roc = &cfg
	default:
		return nil, fmt.Errorf("unknown rule type: %s", r.Type)
	}
	return cr, nil
}

func (e *Engine) getState(ruleID int64, entity string) *ruleState {
	key := stateKey{RuleID: ruleID, Entity: entity}
	e.mu.RLock()
	s, ok := e.state[key]
	e.mu.RUnlock()

	if ok {
		return s
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if s, ok := e.state[key]; ok {
		return s
	}

	s = &ruleState{}
	e.state[key] = s

	return s
}

func (e *Engine) evaluatePoint(p ingest.Point) {
	rules := e.idx[p.Name]

	if len(rules) == 0 {
		return
	}

	for _, cr := range rules {
		s := e.getState(cr.Rule.ID, p.Entity)
		activeBefore := s.Active
		var activeAfter bool
		var detail string

		switch cr.kind {
		case domain.RuleThreshold:
			activeAfter, detail = evalThreshold(cr.thr, s, p)
		case domain.RuleDuration:
			activeAfter, detail = evalDuration(cr.dur, s, p)
		case domain.RuleROC:
			activeAfter, detail = evalROC(cr.roc, s, p)
		default:
			continue
		}

		s.Active = activeAfter

		// emit trace (optional)
		if e.trace {
			d := "hold"
			if !activeBefore && activeAfter {
				d = "trip"
			}

			if activeBefore && !activeAfter {
				d = "clear"
			}

			e.traces <- Trace{
				RuleID: cr.Rule.ID,
				Signal: p.Name,
				Entity: p.Entity,
				At:     p.TS,
				Value:  p.Value,
				Active: activeAfter,
				Detail: d + ":" + detail,
			}
		}

		// emit report (always)
		select {
		case e.reports <- EvalReport{
			RuleID: cr.Rule.ID,
			Signal: p.Name,
			Entity: p.Entity,
			At:     p.TS,
			Value:  p.Value,
			Active: activeAfter,
			Latch:  cr.Rule.Latch,
		}:
		default:
			// drop if manager is too slow; we prefer liveness
		}
	}
}

// --- Evaluation functions ---

func evalThreshold(cfg *ThresholdConfig, s *ruleState, p ingest.Point) (bool, string) {
	// Apply deadband to incoming value for minor noise
	v := p.Value

	switch {
	case s.lastTime.IsZero():
		s.lastValue = v
		s.lastTime = p.TS
	case cfg.Deadband > 0 && math.Abs(v-s.lastValue) < cfg.Deadband:
		// ignore minor changes
		v = s.lastValue
	default:
		s.lastValue = v
		s.lastTime = p.TS
	}

	// Determine violation and clear edges with hysteresis
	now := p.TS
	var violated bool
	var mode string

	if cfg.Lo != nil && v < *cfg.Lo {
		violated = true
		mode = "lo"
	}

	if cfg.Hi != nil && v > *cfg.Hi {
		violated = true
		mode = "hi"
	}

	// Hysteresis for clear condition
	clear := false

	if s.Active {
		switch s.mode {
		case "lo":
			if cfg.Lo != nil {
				thr := *cfg.Lo + cfg.Hysteresis
				if v >= thr {
					clear = true
				}
			}
		case "hi":
			if cfg.Hi != nil {
				thr := *cfg.Hi - cfg.Hysteresis
				if v <= thr {
					clear = true
				}
			}
		}
	}

	// Duration filter
	if violated && !s.Active {
		if s.condStart.IsZero() {
			s.condStart = now
		}

		dur := time.Duration(cfg.DurationMs) * time.Millisecond

		if dur > 0 && now.Sub(s.condStart) < dur {
			return false, "arming"
		}

		s.mode = mode

		return true, "trip"
	}

	if violated && s.Active {
		// still violated: keep active
		return true, "hold"
	}

	if !violated && s.Active {
		// condition false; check hysteresis-clear
		if clear {
			s.condStart = time.Time{}
			s.mode = ""

			return false, "clear"
		}

		return true, "hold-hyst"
	}

	// No violation
	s.condStart = time.Time{}

	return false, "idle"
}

func evalDuration(cfg *DurationConfig, s *ruleState, p ingest.Point) (bool, string) {
	v := p.Value
	cond := false

	switch cfg.Op {
	case ">":
		cond = v > cfg.Threshold
	case "<":
		cond = v < cfg.Threshold
	default:
		// defensively allow typo by treating as >
		cond = v > cfg.Threshold
	}

	now := p.TS

	if cond {
		if s.condStart.IsZero() {
			s.condStart = now
		}

		dur := time.Duration(cfg.DurationMs) * time.Millisecond

		if dur == 0 || now.Sub(s.condStart) >= dur {
			return true, "trip"
		}

		return false, "arming"
	}

	// cond false: check hysteresis-like clearing for >
	if s.Active {
		switch cfg.Op {
		case ">":
			if v <= cfg.Threshold-cfg.Hysteresis {
				s.condStart = time.Time{}
				return false, "clear"
			}

			return true, "hold-hyst"
		case "<":
			if v >= cfg.Threshold+cfg.Hysteresis {
				s.condStart = time.Time{}
				return false, "clear"
			}

			return true, "hold-hyst"
		}
	}

	s.condStart = time.Time{}

	return false, "idle"
}

func evalROC(cfg *ROCConfig, s *ruleState, p ingest.Point) (bool, string) {
	// Append new point and drop old points outside window
	s.win = append(s.win, p)

	if cfg.WindowMs <= 0 {
		cfg.WindowMs = 10000
	}

	cutoff := p.TS.Add(-time.Duration(cfg.WindowMs) * time.Millisecond)
	i := 0

	for ; i < len(s.win); i++ {
		if s.win[i].TS.After(cutoff) || s.win[i].TS.Equal(cutoff) {
			break
		}
	}

	if i > 0 {
		s.win = s.win[i:]
	}

	if len(s.win) < 2 {
		return false, "arming"
	}

	first := s.win[0]
	last := s.win[len(s.win)-1]
	dv := last.Value - first.Value
	dt := last.TS.Sub(first.TS).Seconds()

	if dt <= 0 {
		return false, "idle"
	}

	if cfg.Deadband > 0 && math.Abs(dv) < cfg.Deadband {
		return false, "deadband"
	}

	// units per second
	rate := dv / dt
	val := rate

	if cfg.Abs && rate < 0 {
		val = -rate
	}

	if val > cfg.GT {
		return true, fmt.Sprintf("trip rate=%.3f/s", rate)
	}

	// clear when not exceeded; no hysteresis for ROC at this phase
	return false, fmt.Sprintf("rate=%.3f/s", rate)
}
