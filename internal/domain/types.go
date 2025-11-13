package domain

import (
    "encoding/json"
    "time"
)

type RuleType string
const (
    RuleThreshold RuleType = "threshold"
    RuleROC       RuleType = "roc"
    RuleDuration  RuleType = "duration"
)

type Severity string
const (
    SevInfo Severity = "info"
    SevWarn Severity = "warn"
    SevCrit Severity = "crit"
)

// Rule describes a single evaluation rule against a signal.
// Config is type-specific JSON
// e.g. threshold: {"lo": 23.5, "hi": null, "hysteresis":0.2, "deadband":0.1, "duration_ms":0}
type Rule struct {
    ID        int64           `json:"id"`
    Name      string          `json:"name"`
    Signal    string          `json:"signal"`
    Type      RuleType        `json:"type"`
    Severity  Severity        `json:"severity"`
    Latch     bool            `json:"latch"`
    Enabled   bool            `json:"enabled"`
    Config    json.RawMessage `json:"config"`
    Version   int             `json:"version"`
    CreatedAt time.Time       `json:"created_at"`
    UpdatedAt time.Time       `json:"updated_at"`
}

type AlarmState string
const (
    Inactive AlarmState = "inactive"
    Active   AlarmState = "active"
    Acked    AlarmState = "acked"
    Silenced AlarmState = "silenced"
)

// Alarm represents the current state for a (rule, entity) pair.
type Alarm struct {
    ID           int64       `json:"id"`
    RuleID       int64       `json:"rule_id"`
    Entity       string      `json:"entity"`
    State        AlarmState  `json:"state"`
    FirstSeen    *time.Time  `json:"first_seen"`
    LastSeen     *time.Time  `json:"last_seen"`
    AckedBy      *string     `json:"acked_by"`
    SilenceUntil *time.Time  `json:"silence_until"`
    CreatedAt    time.Time   `json:"created_at"`
    UpdatedAt    time.Time   `json:"updated_at"`
}

type AlarmEventType string
const (
    EventActivated AlarmEventType = "activated"
    EventAcked     AlarmEventType = "acked"
    EventSilenced  AlarmEventType = "silenced"
    EventCleared   AlarmEventType = "cleared"
    EventNote      AlarmEventType = "note"
)

// AlarmEvent is an immutable audit record of an alarm transition or note.
type AlarmEvent struct {
    ID        int64           `json:"id"`
    AlarmID   int64           `json:"alarm_id"`
    Type      AlarmEventType  `json:"type"`
    Actor     string          `json:"actor"`
    At        time.Time       `json:"at"`
    Note      string          `json:"note"`
    Payload   json.RawMessage `json:"payload"`
}

// Sentinel errors (used across repositories)
var (
    ErrNotFound = Err("not found")
    ErrConflict = Err("conflict") // e.g., unique constraint
)

type Err string
func (e Err) Error() string {
    return string(e)
}
