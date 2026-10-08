/*
 * MinIO Cloud Storage, (C) 2017-2019 MinIO, Inc.
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

package storageclass

import (
	"errors"
	"reflect"
	"sync"
	"testing"
)

func TestConfigUpdateChangesReceiver(t *testing.T) {
	var cfg Config
	next := Config{Standard: StorageClass{Parity: 6}, RRS: StorageClass{Parity: 3}, DMA: DMAReadWrite}
	cfg.Update(next)
	if got := cfg.GetDMA(); got != next.DMA {
		t.Fatalf("Update did not change DMA: got %q, want %q", got, next.DMA)
	}
	if got := cfg.GetParityForSC(STANDARD); got != next.Standard.Parity {
		t.Fatalf("Update did not change standard parity: got %d, want %d", got, next.Standard.Parity)
	}
	if got := cfg.GetParityForSC(RRS); got != next.RRS.Parity {
		t.Fatalf("Update did not change RRS parity: got %d, want %d", got, next.RRS.Parity)
	}
	cfg.Update(Config{})
	if cfg.GetDMA() != "" || cfg.GetParityForSC(STANDARD) != 0 || cfg.GetParityForSC(RRS) != defaultRRSParity {
		t.Fatal("Update did not restore the default configuration")
	}
}

func TestConfigSnapshotIsIndependent(t *testing.T) {
	cfg := Config{DMA: DMAWrite}
	snapshot := cfg.Snapshot()
	snapshot.DMA = DMAReadWrite
	if got := cfg.GetDMA(); got != DMAWrite {
		t.Fatalf("Changing a snapshot changed the configuration: %q", got)
	}
}

func TestConfigConcurrentReadUpdate(t *testing.T) {
	configs := [2]Config{
		{Standard: StorageClass{Parity: 4}, RRS: StorageClass{Parity: 2}, DMA: DMAWrite},
		{Standard: StorageClass{Parity: 6}, RRS: StorageClass{Parity: 3}, DMA: DMAReadWrite},
	}
	cfg := configs[0]
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < 1000; j++ {
				cfg.Update(configs[j%len(configs)])
			}
		}()
	}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < 1000; j++ {
				if got := cfg.GetDMA(); got != DMAWrite && got != DMAReadWrite {
					t.Errorf("Unexpected DMA: %q", got)
				}
				if got := cfg.GetParityForSC(STANDARD); got != 4 && got != 6 {
					t.Errorf("Unexpected standard parity: %d", got)
				}
				if got := cfg.GetParityForSC(RRS); got != 2 && got != 3 {
					t.Errorf("Unexpected RRS parity: %d", got)
				}
				if got := cfg.Snapshot(); got != configs[0] && got != configs[1] {
					t.Errorf("Snapshot contains a partial update: %+v", got)
				}
			}
		}()
	}
	close(start)
	wg.Wait()
}

func TestParseStorageClass(t *testing.T) {
	tests := []struct {
		storageClassEnv string
		wantSc          StorageClass
		expectedError   error
	}{
		{"EC:3", StorageClass{
			Parity: 3},
			nil},
		{"EC:4", StorageClass{
			Parity: 4},
			nil},
		{"AB:4", StorageClass{
			Parity: 4},
			errors.New("Unsupported scheme AB. Supported scheme is EC")},
		{"EC:4:5", StorageClass{
			Parity: 4},
			errors.New("Too many sections in EC:4:5")},
		{"EC:A", StorageClass{
			Parity: 4},
			errors.New(`strconv.Atoi: parsing "A": invalid syntax`)},
		{"AB", StorageClass{
			Parity: 4},
			errors.New("Too few sections in AB")},
	}
	for i, tt := range tests {
		gotSc, err := parseStorageClass(tt.storageClassEnv)
		if err != nil && tt.expectedError == nil {
			t.Errorf("Test %d, Expected %s, got %s", i+1, tt.expectedError, err)
			return
		}
		if err == nil && tt.expectedError != nil {
			t.Errorf("Test %d, Expected %s, got %s", i+1, tt.expectedError, err)
			return
		}
		if tt.expectedError == nil && !reflect.DeepEqual(gotSc, tt.wantSc) {
			t.Errorf("Test %d, Expected %v, got %v", i+1, tt.wantSc, gotSc)
			return
		}
		if tt.expectedError != nil && err.Error() != tt.expectedError.Error() {
			t.Errorf("Test %d, Expected `%v`, got `%v`", i+1, tt.expectedError, err)
		}
	}
}

func TestValidateParity(t *testing.T) {
	tests := []struct {
		rrsParity     int
		ssParity      int
		success       bool
		setDriveCount int
	}{
		{2, 4, true, 16},
		{3, 3, true, 16},
		{0, 0, true, 16},
		{1, 4, false, 16},
		{7, 6, false, 16},
		{9, 0, false, 16},
		{9, 9, false, 16},
		{2, 9, false, 16},
		{9, 2, false, 16},
	}
	for i, tt := range tests {
		err := validateParity(tt.ssParity, tt.rrsParity, tt.setDriveCount)
		if err != nil && tt.success {
			t.Errorf("Test %d, Expected success, got %s", i+1, err)
		}
		if err == nil && !tt.success {
			t.Errorf("Test %d, Expected failure, got success", i+1)
		}
	}
}

func TestParityCount(t *testing.T) {
	tests := []struct {
		sc             string
		disksCount     int
		expectedData   int
		expectedParity int
	}{
		{RRS, 16, 14, 2},
		{STANDARD, 16, 8, 8},
		{"", 16, 8, 8},
		{RRS, 16, 9, 7},
		{STANDARD, 16, 10, 6},
		{"", 16, 9, 7},
	}
	for i, tt := range tests {
		scfg := Config{
			Standard: StorageClass{
				Parity: 8,
			},
			RRS: StorageClass{
				Parity: 0,
			},
		}
		// Set env var for test case 4
		if i+1 == 4 {
			scfg.RRS.Parity = 7
		}
		// Set env var for test case 5
		if i+1 == 5 {
			scfg.Standard.Parity = 6
		}
		// Set env var for test case 6
		if i+1 == 6 {
			scfg.Standard.Parity = 7
		}
		parity := scfg.GetParityForSC(tt.sc)
		if (tt.disksCount - parity) != tt.expectedData {
			t.Errorf("Test %d, Expected data disks %d, got %d", i+1, tt.expectedData, tt.disksCount-parity)
			continue
		}
		if parity != tt.expectedParity {
			t.Errorf("Test %d, Expected parity disks %d, got %d", i+1, tt.expectedParity, parity)
		}
	}
}

// Test IsValid method with valid and invalid inputs
func TestIsValidStorageClassKind(t *testing.T) {
	tests := []struct {
		sc   string
		want bool
	}{
		{"STANDARD", true},
		{"REDUCED_REDUNDANCY", true},
		{"", false},
		{"INVALID", false},
		{"123", false},
		{"OTTERIO_STORAGE_CLASS_RRS", false},
		{"OTTERIO_STORAGE_CLASS_STANDARD", false},
	}
	for i, tt := range tests {
		if got := IsValid(tt.sc); got != tt.want {
			t.Errorf("Test %d, Expected Storage Class to be %t, got %t", i+1, tt.want, got)
		}
	}
}
