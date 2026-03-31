package logging

import (
	"fmt"
	"io"

	"github.com/sirupsen/logrus"
)

type Logger interface {
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

type Adapter struct {
	base *logrus.Logger
}

func New(base *logrus.Logger) *Adapter {
	return &Adapter{base: base}
}

func NewDiscard(base *logrus.Logger) *Adapter {
	base.SetOutput(io.Discard)
	return New(base)
}

func (a *Adapter) Info(msg string, args ...any) {
	a.log(func(entry *logrus.Entry) {
		entry.Info(msg)
	}, args...)
}

func (a *Adapter) Warn(msg string, args ...any) {
	a.log(func(entry *logrus.Entry) {
		entry.Warn(msg)
	}, args...)
}

func (a *Adapter) Error(msg string, args ...any) {
	a.log(func(entry *logrus.Entry) {
		entry.Error(msg)
	}, args...)
}

func (a *Adapter) log(write func(*logrus.Entry), args ...any) {
	if a == nil || a.base == nil {
		return
	}
	write(a.base.WithFields(toFields(args...)))
}

func toFields(args ...any) logrus.Fields {
	fields := logrus.Fields{}
	for i := 0; i < len(args); i += 2 {
		key := fmt.Sprintf("field_%d", i/2)
		if candidate, ok := args[i].(string); ok && candidate != "" {
			key = candidate
		}

		if i+1 >= len(args) {
			fields[key] = nil
			continue
		}
		fields[key] = args[i+1]
	}
	return fields
}
