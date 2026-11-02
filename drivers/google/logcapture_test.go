// Copyright 2018 Drone.IO Inc
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package google

import (
	"io/ioutil"
	"sync"

	"github.com/sirupsen/logrus"
)

// logHook records every entry logged through it, so tests can assert on what
// the driver logged and at what level.
type logHook struct {
	mu      sync.Mutex
	entries []*logrus.Entry
}

func (h *logHook) Levels() []logrus.Level { return logrus.AllLevels }

func (h *logHook) Fire(entry *logrus.Entry) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	e := *entry
	h.entries = append(h.entries, &e)
	return nil
}

// AllEntries returns a copy of everything logged so far.
func (h *logHook) AllEntries() []*logrus.Entry {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]*logrus.Entry(nil), h.entries...)
}

// newNullLogger returns a logger that discards its output and a hook that
// records what was logged to it.
func newNullLogger() (*logrus.Logger, *logHook) {
	l := logrus.New()
	l.Out = ioutil.Discard
	h := &logHook{}
	l.AddHook(h)
	return l, h
}
