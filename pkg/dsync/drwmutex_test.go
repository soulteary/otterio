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

package dsync_test

import (
	"context"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	dsync "github.com/soulteary/otterio/pkg/dsync"
)

const (
	id     = "1234-5678"
	source = "main.go"
)

func testSimpleWriteLock(_ *testing.T, duration time.Duration) (locked bool) {

	drwm := dsync.NewDRWMutex(ds, "simplelock")

	ctx1, cancel1 := context.WithCancel(context.Background())
	if !drwm.GetRLock(ctx1, cancel1, id, source, dsync.Options{Timeout: time.Second}) {
		panic("Failed to acquire read lock")
	}

	ctx2, cancel2 := context.WithCancel(context.Background())
	if !drwm.GetRLock(ctx2, cancel2, id, source, dsync.Options{Timeout: time.Second}) {
		panic("Failed to acquire read lock")
	}

	// Stagger the two RUnlocks so there is still contention while the
	// write Lock is being attempted. The longer hold (2nd RUnlock) must
	// stay > the `duration=1s` used by TestSimpleWriteLockTimedOut so
	// that test still observes a timeout.
	go func() {
		time.Sleep(1 * time.Second)
		drwm.RUnlock()
	}()

	go func() {
		time.Sleep(2 * time.Second)
		drwm.RUnlock()
	}()

	ctx3, cancel3 := context.WithCancel(context.Background())
	locked = drwm.GetLock(ctx3, cancel3, id, source, dsync.Options{Timeout: duration})
	if locked {
		time.Sleep(100 * time.Millisecond)
		drwm.Unlock()
	}
	return
}

func TestSimpleWriteLockAcquired(t *testing.T) {
	locked := testSimpleWriteLock(t, 5*time.Second)

	expected := true
	if locked != expected {
		t.Errorf("TestSimpleWriteLockAcquired(): \nexpected %#v\ngot      %#v", expected, locked)
	}
}

func TestSimpleWriteLockTimedOut(t *testing.T) {
	locked := testSimpleWriteLock(t, time.Second)

	expected := false
	if locked != expected {
		t.Errorf("TestSimpleWriteLockTimedOut(): \nexpected %#v\ngot      %#v", expected, locked)
	}
}

func testDualWriteLock(_ *testing.T, duration time.Duration) (locked bool) {

	drwm := dsync.NewDRWMutex(ds, "duallock")

	// fmt.Println("Getting initial write lock")
	ctx1, cancel1 := context.WithCancel(context.Background())
	if !drwm.GetLock(ctx1, cancel1, id, source, dsync.Options{Timeout: time.Second}) {
		panic("Failed to acquire initial write lock")
	}

	// Hold for 1.5s. Must be > the `duration=1s` used by
	// TestDualWriteLockTimedOut so the second GetLock there genuinely
	// times out, but no longer than necessary -- the original 2s just
	// burned wall time.
	go func() {
		time.Sleep(1500 * time.Millisecond)
		drwm.Unlock()
	}()

	// fmt.Println("Trying to acquire 2nd write lock, waiting...")
	ctx2, cancel2 := context.WithCancel(context.Background())
	locked = drwm.GetLock(ctx2, cancel2, id, source, dsync.Options{Timeout: duration})
	if locked {
		// Brief hold; nothing else is contending here.
		time.Sleep(100 * time.Millisecond)
		drwm.Unlock()
	}
	return
}

func TestDualWriteLockAcquired(t *testing.T) {
	locked := testDualWriteLock(t, 5*time.Second)

	expected := true
	if locked != expected {
		t.Errorf("TestDualWriteLockAcquired(): \nexpected %#v\ngot      %#v", expected, locked)
	}

}

func TestDualWriteLockTimedOut(t *testing.T) {
	locked := testDualWriteLock(t, time.Second)

	expected := false
	if locked != expected {
		t.Errorf("TestDualWriteLockTimedOut(): \nexpected %#v\ngot      %#v", expected, locked)
	}

}

// Test cases below are copied 1 to 1 from sync/rwmutex_test.go (adapted to use DRWMutex)

// Borrowed from rwmutex_test.go
func parallelReader(ctx context.Context, m *dsync.DRWMutex, clocked, cunlock, cdone chan bool) {
	if m.GetRLock(ctx, nil, id, source, dsync.Options{Timeout: time.Second}) {
		clocked <- true
		<-cunlock
		m.RUnlock()
		cdone <- true
	}
}

// Borrowed from rwmutex_test.go
func doTestParallelReaders(numReaders, gomaxprocs int) {
	runtime.GOMAXPROCS(gomaxprocs)
	m := dsync.NewDRWMutex(ds, "test-parallel")

	clocked := make(chan bool)
	cunlock := make(chan bool)
	cdone := make(chan bool)
	for i := 0; i < numReaders; i++ {
		go parallelReader(context.Background(), m, clocked, cunlock, cdone)
	}
	// Wait for all parallel RLock()s to succeed.
	for i := 0; i < numReaders; i++ {
		<-clocked
	}
	for i := 0; i < numReaders; i++ {
		cunlock <- true
	}
	// Wait for the goroutines to finish.
	for i := 0; i < numReaders; i++ {
		<-cdone
	}
}

// Borrowed from rwmutex_test.go
func TestParallelReaders(_ *testing.T) {
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(-1))
	doTestParallelReaders(1, 4)
	doTestParallelReaders(3, 4)
	doTestParallelReaders(4, 2)
}

// Borrowed from rwmutex_test.go
func reader(rwm *dsync.DRWMutex, numIterations int, activity *int32, cdone chan bool) {
	for i := 0; i < numIterations; i++ {
		if rwm.GetRLock(context.Background(), nil, id, source, dsync.Options{Timeout: time.Second}) {
			n := atomic.AddInt32(activity, 1)
			if n < 1 || n >= 10000 {
				panic(fmt.Sprintf("wlock(%d)\n", n))
			}
			// busy-wait spin to simulate work while holding the lock
			for i := 0; i < 100; i++ {
				_ = i
			}
			atomic.AddInt32(activity, -1)
			rwm.RUnlock()
		}
	}
	cdone <- true
}

// Borrowed from rwmutex_test.go
func writer(rwm *dsync.DRWMutex, numIterations int, activity *int32, cdone chan bool) {
	for i := 0; i < numIterations; i++ {
		if rwm.GetLock(context.Background(), nil, id, source, dsync.Options{Timeout: time.Second}) {
			n := atomic.AddInt32(activity, 10000)
			if n != 10000 {
				panic(fmt.Sprintf("wlock(%d)\n", n))
			}
			// busy-wait spin to simulate work while holding the lock
			for i := 0; i < 100; i++ {
				_ = i
			}
			atomic.AddInt32(activity, -10000)
			rwm.Unlock()
		}
	}
	cdone <- true
}

// Borrowed from rwmutex_test.go
func HammerRWMutex(gomaxprocs, numReaders, numIterations int) {
	runtime.GOMAXPROCS(gomaxprocs)
	// Number of active readers + 10000 * number of active writers.
	var activity int32
	rwm := dsync.NewDRWMutex(ds, "test")
	cdone := make(chan bool)
	go writer(rwm, numIterations, &activity, cdone)
	var i int
	for i = 0; i < numReaders/2; i++ {
		go reader(rwm, numIterations, &activity, cdone)
	}
	go writer(rwm, numIterations, &activity, cdone)
	for ; i < numReaders; i++ {
		go reader(rwm, numIterations, &activity, cdone)
	}
	// Wait for the 2 writers and all readers to finish.
	for i := 0; i < 2+numReaders; i++ {
		<-cdone
	}
}

// Borrowed from rwmutex_test.go
func TestRWMutex(_ *testing.T) {
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(-1))
	n := 100
	if testing.Short() {
		n = 5
	}
	HammerRWMutex(1, 1, n)
	HammerRWMutex(1, 3, n)
	HammerRWMutex(1, 10, n)
	HammerRWMutex(4, 1, n)
	HammerRWMutex(4, 3, n)
	HammerRWMutex(4, 10, n)
	HammerRWMutex(10, 1, n)
	HammerRWMutex(10, 3, n)
	HammerRWMutex(10, 10, n)
	HammerRWMutex(10, 5, n)
}

// Borrowed from rwmutex_test.go
func TestUnlockPanic(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatalf("unlock of unlocked RWMutex did not panic")
		}
	}()
	mu := dsync.NewDRWMutex(ds, "test")
	mu.Unlock()
}

// Borrowed from rwmutex_test.go
func TestUnlockPanic2(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatalf("unlock of unlocked RWMutex did not panic")
		}
	}()
	mu := dsync.NewDRWMutex(ds, "test-unlock-panic-2")
	mu.RLock(id, source)
	mu.Unlock()
}

// Borrowed from rwmutex_test.go
func TestRUnlockPanic(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatalf("read unlock of unlocked RWMutex did not panic")
		}
	}()
	mu := dsync.NewDRWMutex(ds, "test")
	mu.RUnlock()
}

// Borrowed from rwmutex_test.go
func TestRUnlockPanic2(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatalf("read unlock of unlocked RWMutex did not panic")
		}
	}()
	mu := dsync.NewDRWMutex(ds, "test-runlock-panic-2")
	mu.Lock(id, source)
	mu.RUnlock()
}

// Borrowed from rwmutex_test.go
func benchmarkRWMutex(b *testing.B, localWork, writeRatio int) {
	rwm := dsync.NewDRWMutex(ds, "test")
	b.RunParallel(func(pb *testing.PB) {
		foo := 0
		for pb.Next() {
			foo++
			if foo%writeRatio == 0 {
				rwm.Lock(id, source)
				rwm.Unlock()
			} else {
				rwm.RLock(id, source)
				for i := 0; i != localWork; i++ {
					foo *= 2
					foo /= 2
				}
				rwm.RUnlock()
			}
		}
		_ = foo
	})
}

// Borrowed from rwmutex_test.go
func BenchmarkRWMutexWrite100(b *testing.B) {
	benchmarkRWMutex(b, 0, 100)
}

// Borrowed from rwmutex_test.go
func BenchmarkRWMutexWrite10(b *testing.B) {
	benchmarkRWMutex(b, 0, 10)
}

// Borrowed from rwmutex_test.go
func BenchmarkRWMutexWorkWrite100(b *testing.B) {
	benchmarkRWMutex(b, 100, 100)
}

// Borrowed from rwmutex_test.go
func BenchmarkRWMutexWorkWrite10(b *testing.B) {
	benchmarkRWMutex(b, 100, 10)
}
