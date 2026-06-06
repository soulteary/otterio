/*
 * Minio Cloud Storage, (C) 2016 Minio, Inc.
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

// GOMAXPROCS=10 go test

package dsync_test

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/rpc"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/soulteary/otterio/pkg/dsync"
)

const numberOfNodes = 5

var ds *dsync.Dsync
var rpcPaths []string // list of rpc paths where lock server is serving.

var nodes = make([]string, numberOfNodes) // list of node IP addrs or hostname with ports.
var lockServers []*lockServer

func startRPCServers() {
	for i := range nodes {
		server := rpc.NewServer()
		ls := &lockServer{
			mutex:   sync.Mutex{},
			lockMap: make(map[string]int64),
		}
		server.RegisterName("Dsync", ls)
		// For some reason the registration paths need to be different (even for different server objs)
		server.HandleHTTP(rpcPaths[i], fmt.Sprintf("%s-debug", rpcPaths[i]))
		l, e := net.Listen("tcp", ":"+strconv.Itoa(i+12345))
		if e != nil {
			log.Fatal("listen error:", e)
		}
		go http.Serve(l, nil)

		lockServers = append(lockServers, ls)
	}

	// Let servers start
	time.Sleep(10 * time.Millisecond)
}

// TestMain initializes the testing framework
func TestMain(m *testing.M) {
	const rpcPath = "/dsync"

	rand.Seed(time.Now().UTC().UnixNano())

	for i := range nodes {
		nodes[i] = fmt.Sprintf("127.0.0.1:%d", i+12345)
	}
	for i := range nodes {
		rpcPaths = append(rpcPaths, rpcPath+"-"+strconv.Itoa(i))
	}

	// Initialize net/rpc clients for dsync.
	var clnts []dsync.NetLocker
	for i := 0; i < len(nodes); i++ {
		clnts = append(clnts, newClient(nodes[i], rpcPaths[i]))
	}

	ds = &dsync.Dsync{
		GetLockers: func() ([]dsync.NetLocker, string) { return clnts, uuid.New().String() },
	}

	startRPCServers()

	os.Exit(m.Run())
}

func TestSimpleLock(_ *testing.T) {

	dm := dsync.NewDRWMutex(ds, "test")

	dm.Lock(id, source)

	// Brief hold; the previous 2.5s sleep added nothing because no other
	// goroutine is contending for "test" here.
	time.Sleep(50 * time.Millisecond)

	dm.Unlock()
}

func TestSimpleLockUnlockMultipleTimes(_ *testing.T) {

	dm := dsync.NewDRWMutex(ds, "test")

	dm.Lock(id, source)
	time.Sleep(time.Duration(10+(rand.Float32()*50)) * time.Millisecond)
	dm.Unlock()

	dm.Lock(id, source)
	time.Sleep(time.Duration(10+(rand.Float32()*50)) * time.Millisecond)
	dm.Unlock()

	dm.Lock(id, source)
	time.Sleep(time.Duration(10+(rand.Float32()*50)) * time.Millisecond)
	dm.Unlock()

	dm.Lock(id, source)
	time.Sleep(time.Duration(10+(rand.Float32()*50)) * time.Millisecond)
	dm.Unlock()

	dm.Lock(id, source)
	time.Sleep(time.Duration(10+(rand.Float32()*50)) * time.Millisecond)
	dm.Unlock()
}

// Test two locks for same resource, one succeeds, one fails (after timeout)
func TestTwoSimultaneousLocksForSameResource(_ *testing.T) {

	dm1st := dsync.NewDRWMutex(ds, "aap")
	dm2nd := dsync.NewDRWMutex(ds, "aap")

	dm1st.Lock(id, source)

	// Release the first lock after a short hold. The original test held
	// it for 10s; that is wasteful relative to lockRetryInterval (1s) and
	// is the main reason the package historically blew past `go test ./...
	// -timeout=60s`. 2s is comfortably longer than one retry cycle so the
	// second Lock still races against a held lock at least once before
	// succeeding, preserving the contention being exercised.
	go func() {
		time.Sleep(2 * time.Second)
		dm1st.Unlock()
	}()

	dm2nd.Lock(id, source)

	// Brief hold so we exercise the post-acquire path; the original 2.5s
	// added no signal because dm2nd has no contender at this point.
	time.Sleep(200 * time.Millisecond)

	dm2nd.Unlock()
}

// Test three locks for same resource, one succeeds, one fails (after timeout)
func TestThreeSimultaneousLocksForSameResource(_ *testing.T) {

	dm1st := dsync.NewDRWMutex(ds, "aap")
	dm2nd := dsync.NewDRWMutex(ds, "aap")
	dm3rd := dsync.NewDRWMutex(ds, "aap")

	dm1st.Lock(id, source)

	// Release the first lock after 2s for the same reason as
	// TestTwoSimultaneousLocksForSameResource: the goal is to exercise
	// contention against lockRetryInterval (1s), not to sleep for 10s.
	go func() {
		time.Sleep(2 * time.Second)
		dm1st.Unlock()
	}()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()

		dm2nd.Lock(id, source)

		// Hold long enough for a retry cycle to observe contention,
		// then drop. 500ms == half a retry interval, plenty for the
		// dm3rd Lock below to spin once before succeeding.
		go func() {
			time.Sleep(500 * time.Millisecond)
			dm2nd.Unlock()
		}()

		dm3rd.Lock(id, source)
		time.Sleep(200 * time.Millisecond)
		dm3rd.Unlock()
	}()

	go func() {
		defer wg.Done()

		dm3rd.Lock(id, source)

		go func() {
			time.Sleep(500 * time.Millisecond)
			dm3rd.Unlock()
		}()

		dm2nd.Lock(id, source)
		time.Sleep(200 * time.Millisecond)
		dm2nd.Unlock()
	}()

	wg.Wait()
}

// Test two locks for different resources, both succeed
func TestTwoSimultaneousLocksForDifferentResources(_ *testing.T) {

	dm1 := dsync.NewDRWMutex(ds, "aap")
	dm2 := dsync.NewDRWMutex(ds, "noot")

	dm1.Lock(id, source)
	dm2.Lock(id, source)

	// No contention here (different resource names) -- the original
	// 2.5s sleep was dead time. Hold briefly to keep both locks live
	// across at least a scheduler tick, then release.
	time.Sleep(50 * time.Millisecond)

	dm1.Unlock()
	dm2.Unlock()

	time.Sleep(10 * time.Millisecond)
}

// Test refreshing lock
func TestFailedRefreshLock(t *testing.T) {
	// This test must wait one full drwMutexRefreshInterval (10s, a
	// package-level const) for the refresh failure to be observed and
	// the lock callback to fire. That makes it the slowest test in this
	// package; let `-short` skip it so `go test ./... -short` stays
	// within tight per-package timeouts.
	if testing.Short() {
		t.Skip("skipping: waits one drwMutexRefreshInterval (~10s) by design")
	}

	// Simulate Refresh RPC response to return no locking found
	for i := range lockServers {
		lockServers[i].setRefreshReply(false)
	}

	dm := dsync.NewDRWMutex(ds, "aap")
	wg := sync.WaitGroup{}
	wg.Add(1)

	ctx, cl := context.WithCancel(context.Background())
	cancel := func() {
		cl()
		wg.Done()
	}

	if !dm.GetLock(ctx, cancel, id, source, dsync.Options{Timeout: 5 * time.Minute}) {
		t.Fatal("GetLock() should be successful")
	}

	// Wait until context is canceled
	wg.Wait()
	if ctx.Err() == nil {
		t.Fatal("Unexpected error", ctx.Err())
	}

	// Should be safe operation in all cases
	dm.Unlock()

	// Revert Refresh RPC response to locking found
	for i := range lockServers {
		lockServers[i].setRefreshReply(false)
	}
}

// Borrowed from mutex_test.go
func HammerMutex(m *dsync.DRWMutex, loops int, cdone chan bool) {
	for i := 0; i < loops; i++ {
		m.Lock(id, source)
		m.Unlock()
	}
	cdone <- true
}

// Borrowed from mutex_test.go
func TestMutex(_ *testing.T) {
	loops := 200
	if testing.Short() {
		loops = 5
	}
	c := make(chan bool)
	m := dsync.NewDRWMutex(ds, "test")
	for i := 0; i < 10; i++ {
		go HammerMutex(m, loops, c)
	}
	for i := 0; i < 10; i++ {
		<-c
	}
}

func BenchmarkMutexUncontended(b *testing.B) {
	type PaddedMutex struct {
		*dsync.DRWMutex
	}
	b.RunParallel(func(pb *testing.PB) {
		var mu = PaddedMutex{dsync.NewDRWMutex(ds, "")}
		for pb.Next() {
			mu.Lock(id, source)
			mu.Unlock()
		}
	})
}

func benchmarkMutex(b *testing.B, slack, work bool) {
	mu := dsync.NewDRWMutex(ds, "")
	if slack {
		b.SetParallelism(10)
	}
	b.RunParallel(func(pb *testing.PB) {
		foo := 0
		for pb.Next() {
			mu.Lock(id, source)
			mu.Unlock()
			if work {
				for i := 0; i < 100; i++ {
					foo *= 2
					foo /= 2
				}
			}
		}
		_ = foo
	})
}

func BenchmarkMutex(b *testing.B) {
	benchmarkMutex(b, false, false)
}

func BenchmarkMutexSlack(b *testing.B) {
	benchmarkMutex(b, true, false)
}

func BenchmarkMutexWork(b *testing.B) {
	benchmarkMutex(b, false, true)
}

func BenchmarkMutexWorkSlack(b *testing.B) {
	benchmarkMutex(b, true, true)
}

func BenchmarkMutexNoSpin(b *testing.B) {
	// This benchmark models a situation where spinning in the mutex should be
	// non-profitable and allows to confirm that spinning does not do harm.
	// To achieve this we create excess of goroutines most of which do local work.
	// These goroutines yield during local work, so that switching from
	// a blocked goroutine to other goroutines is profitable.
	// As a matter of fact, this benchmark still triggers some spinning in the mutex.
	m := dsync.NewDRWMutex(ds, "")
	var acc0, acc1 uint64
	b.SetParallelism(4)
	b.RunParallel(func(pb *testing.PB) {
		c := make(chan bool)
		var data [4 << 10]uint64
		for i := 0; pb.Next(); i++ {
			if i%4 == 0 {
				m.Lock(id, source)
				acc0 -= 100
				acc1 += 100
				m.Unlock()
			} else {
				for i := 0; i < len(data); i += 4 {
					data[i]++
				}
				// Elaborate way to say runtime.Gosched
				// that does not put the goroutine onto global runq.
				go func() {
					c <- true
				}()
				<-c
			}
		}
	})
}

func BenchmarkMutexSpin(b *testing.B) {
	// This benchmark models a situation where spinning in the mutex should be
	// profitable. To achieve this we create a goroutine per-proc.
	// These goroutines access considerable amount of local data so that
	// unnecessary rescheduling is penalized by cache misses.
	m := dsync.NewDRWMutex(ds, "")
	var acc0, acc1 uint64
	b.RunParallel(func(pb *testing.PB) {
		var data [16 << 10]uint64
		for i := 0; pb.Next(); i++ {
			m.Lock(id, source)
			acc0 -= 100
			acc1 += 100
			m.Unlock()
			for i := 0; i < len(data); i += 4 {
				data[i]++
			}
		}
	})
}
