/*
 * MinIO Cloud Storage, (C) 2016 MinIO, Inc.
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

package cmd

import (
	"context"
	"os"
	"os/exec"
	"syscall"
)

// Type of service signals currently supported.
type serviceSignal int

const (
	serviceRestart       serviceSignal = iota // Restarts the server.
	serviceStop                               // Stops the server.
	serviceReloadDynamic                      // Reload dynamic config values.
	// Add new service requests here.
)

// Global service signal channel.
var globalServiceSignalCh chan serviceSignal

// GlobalServiceDoneCh - Global service done channel.
var GlobalServiceDoneCh <-chan struct{}

// GlobalContext context that is canceled when server is requested to shut down.
var GlobalContext context.Context

// cancelGlobalContext can be used to indicate server shutdown.
var cancelGlobalContext context.CancelFunc

func initGlobalContext() {
	GlobalContext, cancelGlobalContext = context.WithCancel(context.Background())
	GlobalServiceDoneCh = GlobalContext.Done()
	globalServiceSignalCh = make(chan serviceSignal)
}

// restartProcess replaces the active server after listeners and storage close.
// Unix exec preserves the PID. Darwin requests a new worker from its stable
// public supervisor, avoiding exec in the busy server runtime.
func restartProcess() error {
	// Use the original binary location. This works with symlinks such that if
	// the file it points to has been changed we will use the updated symlink.
	argv0, err := exec.LookPath(os.Args[0])
	if err != nil {
		return err
	}

	if handled, err := restartSupervisedProcess(); handled {
		return err
	}

	// Invokes the execve system call.
	// Re-uses the same pid. This preserves the pid over multiple server-respawns.
	return syscall.Exec(argv0, os.Args, os.Environ())
}
