package core

import (
	log "github.com/sirupsen/logrus"
)

type Executor struct {
	Logger *log.Logger
}

func (e *Executor) SetLogger(logger *log.Logger) {
	e.Logger = logger
}

func (e *Executor) GetLogger() *log.Logger {
	return e.Logger
}
