/*
 * MinIO Cloud Storage, (C) 2018 MinIO, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package logger

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"
)

// Holds a map of recently logged errors.
type logOnceType struct {
	IDMap map[interface{}]error
	sync.Mutex
	stopCh chan struct{}
	once   sync.Once
}

// One log message per error.
func (l *logOnceType) logOnceIf(ctx context.Context, err error, id interface{}, errKind ...interface{}) {
	if err == nil {
		return
	}
	l.Lock()
	shouldLog := false
	prevErr := l.IDMap[id]
	if prevErr == nil {
		l.IDMap[id] = err
		shouldLog = true
	} else {
		if prevErr.Error() != err.Error() {
			l.IDMap[id] = err
			shouldLog = true
		}
	}
	l.Unlock()

	if shouldLog {
		LogIf(ctx, err, errKind...)
	}
}

// Cleanup the map every 30 minutes so that the log message is printed again for the user to notice.
func (l *logOnceType) cleanupRoutine() {
	timer := time.NewTimer(30 * time.Minute)
	defer timer.Stop()
	for {
		l.Lock()
		l.IDMap = make(map[interface{}]error)
		l.Unlock()

		select {
		case <-l.stopCh:
			return
		case <-timer.C:
			timer.Reset(30 * time.Minute)
		}
	}
}

// Stop terminates the background cleanup goroutine. Idempotent.
func (l *logOnceType) Stop() {
	l.once.Do(func() {
		close(l.stopCh)
	})
}

// Returns logOnceType
func newLogOnceType() *logOnceType {
	l := &logOnceType{IDMap: make(map[interface{}]error), stopCh: make(chan struct{})}
	go l.cleanupRoutine()
	return l
}

var logOnce = newLogOnceType()

// StopLogOnce stops the background cleanup goroutine for the package-level
// logOnce singleton. Intended for graceful shutdown in tests / TestMain.
func StopLogOnce() {
	logOnce.Stop()
}

// LogOnceIf - Logs notification errors - once per error.
// id is a unique identifier for related log messages, refer to cmd/notification.go
// on how it is used.
func LogOnceIf(ctx context.Context, err error, id interface{}, errKind ...interface{}) {
	if err == nil {
		return
	}

	if errors.Is(err, context.Canceled) {
		return
	}

	if err.Error() == http.ErrServerClosed.Error() || err.Error() == "disk not found" {
		return
	}

	logOnce.logOnceIf(ctx, err, id, errKind...)
}
