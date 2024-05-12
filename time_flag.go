package main

import (
	"flag"
	"time"
)

type TimeFlagOptionFunc func(*TimeFlag)
type TimeProvider func() time.Time

func WithTimeProvider(now TimeProvider) TimeFlagOptionFunc {
	return func(f *TimeFlag) {
		f.now = now
	}
}

func WithDefault(t time.Time) TimeFlagOptionFunc {
	return func(f *TimeFlag) {
		f.value = t
	}
}

var _ flag.Value = (*TimeFlag)(nil)

type TimeFlag struct {
	now   TimeProvider
	value time.Time
}

func (o *TimeFlag) String() string {
	return o.value.Format(time.RFC3339)
}

func (o *TimeFlag) Set(v string) error {
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return err
	}
	o.value = t
	return nil
}

func (o *TimeFlag) Value() time.Time {
	return o.value
}

func NewTimeFlag(opts ...TimeFlagOptionFunc) *TimeFlag {
	f := &TimeFlag{now: time.Now}
	for _, opt := range opts {
		opt(f)
	}
	return f
}
