package core_test

import (
	"strings"
	"sync"
	"testing"

	"github.com/juancavallotti/octo/core"
)

func TestNewSpiesRejectsBadAddresses(t *testing.T) {
	tests := []struct {
		name      string
		addresses []string
		want      string
	}{
		{name: "empty address", addresses: []string{"orders.charge", ""}, want: "empty"},
		{name: "duplicate address", addresses: []string{"orders.charge", "orders.charge"}, want: "twice"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := core.NewSpies(tc.addresses)
			if err == nil {
				t.Fatalf("NewSpies(%q) succeeded, want an error", tc.addresses)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to mention %q", err, tc.want)
			}
		})
	}
}

// TestSpiesAddressesKeepTheOrderGiven: a trace reads back in the order the user
// asked for it, not in map order.
func TestSpiesAddressesKeepTheOrderGiven(t *testing.T) {
	want := []string{"orders.charge", "orders.validate", "other.step"}
	spies, err := core.NewSpies(want)
	if err != nil {
		t.Fatalf("NewSpies: %v", err)
	}
	got := spies.Addresses()
	if len(got) != len(want) {
		t.Fatalf("got %d addresses, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("address %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestSpiesRecordIsConcurrencySafe: a fork runs its branches at once and a flow runs
// its blocks across several workers, so a spied block is recorded from many
// goroutines. Every crossing must survive, with a sequence number that is unique
// across every spy — that shared counter is what puts a multi-spy trace in order.
func TestSpiesRecordIsConcurrencySafe(t *testing.T) {
	const perAddress = 50
	addresses := []string{"orders.a", "orders.b"}

	spies, err := core.NewSpies(addresses)
	if err != nil {
		t.Fatalf("NewSpies: %v", err)
	}

	var wg sync.WaitGroup
	for _, address := range addresses {
		for range perAddress {
			wg.Add(1)
			go func() {
				defer wg.Done()
				spies.Record(address, core.SpyRecord{})
			}()
		}
	}
	wg.Wait()

	seen := make(map[int64]bool, len(addresses)*perAddress)
	for _, address := range addresses {
		records := spies.Records(address)
		if len(records) != perAddress {
			t.Fatalf("%s: got %d records, want %d — a crossing was lost", address, len(records), perAddress)
		}
		for i, rec := range records {
			if seen[rec.Seq] {
				t.Fatalf("seq %d was handed out twice", rec.Seq)
			}
			seen[rec.Seq] = true
			// Within one address the records are in the order they were taken.
			if i > 0 && rec.Seq <= records[i-1].Seq {
				t.Errorf("%s: record %d has seq %d, not after %d", address, i, rec.Seq, records[i-1].Seq)
			}
		}
	}
}

// TestSpiesRecordsForUnwatchedAddress: only the injected spy blocks call Record, and
// each was built for an address the collector knows, so an unknown one is ignored
// rather than panicking. Records for it are empty, not nil-dereferencing.
func TestSpiesRecordsForUnwatchedAddress(t *testing.T) {
	spies, err := core.NewSpies([]string{"orders.charge"})
	if err != nil {
		t.Fatalf("NewSpies: %v", err)
	}
	spies.Record("orders.nope", core.SpyRecord{})
	if got := spies.Records("orders.nope"); got != nil {
		t.Errorf("Records for an unwatched address = %v, want nil", got)
	}
	if got := spies.Records("orders.charge"); len(got) != 0 {
		t.Errorf("a record filed under an unwatched address leaked into %v", got)
	}
}
