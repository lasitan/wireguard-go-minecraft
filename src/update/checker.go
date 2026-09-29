package update

import (
	"context"
	"sync"
	"time"
)

// Status is what the Master exposes at /api/version.
type Status struct {
	Current   string    `json:"current"`
	Latest    string    `json:"latest,omitempty"`
	HasUpdate bool      `json:"hasUpdate"`
	Release   *Release  `json:"release,omitempty"`
	CheckedAt time.Time `json:"checkedAt,omitempty"`
	Error     string    `json:"error,omitempty"`
	Disabled  bool      `json:"disabled,omitempty"`
}

// Checker polls GitHub on an interval and caches the last result.
type Checker struct {
	Interval time.Duration
	Disabled bool

	mu     sync.Mutex
	status Status
	Fetch  func(context.Context) (*Release, error)
	kick   chan struct{}

	// OnChange runs after every completed check (set before Run).
	OnChange func()
}

func NewChecker(interval time.Duration, disabled bool) *Checker {
	return &Checker{Interval: interval, Disabled: disabled, Fetch: Latest, kick: make(chan struct{}, 1)}
}

func (c *Checker) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.status
	s.Current = Current()
	s.Disabled = c.Disabled
	if s.Latest != "" {
		s.HasUpdate = Newer(s.Latest, s.Current)
	}
	return s
}

// Refresh asks the background loop to re-check now (non-blocking).
func (c *Checker) Refresh() {
	select {
	case c.kick <- struct{}{}:
	default:
	}
}

func (c *Checker) Run(stop <-chan struct{}) {
	if c.Disabled {
		return
	}
	c.CheckNow()
	t := time.NewTicker(c.Interval)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		case <-c.kick:
		}
		c.CheckNow()
	}
}

// CheckNow queries GitHub synchronously and updates the cached status.
func (c *Checker) CheckNow() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rel, err := c.Fetch(ctx)
	c.store(rel, err)
	if c.OnChange != nil {
		c.OnChange()
	}
}

func (c *Checker) store(rel *Release, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.status.CheckedAt = time.Now().UTC()
	if err != nil {
		c.status.Error = err.Error()
		return
	}
	c.status.Error = ""
	c.status.Latest = rel.Version
	c.status.Release = rel
}
