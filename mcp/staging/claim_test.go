package staging

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestTakeIsNonDestructiveUntilDone pins the claim/Done contract. Take is
// not the commit point — the store is — so Take must not delete anything.
// If it did, a memory the store then refuses (bad scope, disk error, git
// error) would be gone with no compensating re-add anywhere.
func TestTakeIsNonDestructiveUntilDone(t *testing.T) {
	a, l := newArea(t)
	id := "1111222233334444"
	if err := a.Add(mem(id, "Approved.")); err != nil {
		t.Fatalf("Add: %v", err)
	}

	m, err := a.Take(id)
	if err != nil {
		t.Fatalf("Take: %v", err)
	}
	if m.Body != "Approved." {
		t.Errorf("Body = %q", m.Body)
	}

	file := filepath.Join(l.Pending, id+".md")
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("Take must leave the pending file in place until Done; stat err = %v", err)
	}
	claim := file + ".taking"
	if _, err := os.Stat(claim); err != nil {
		t.Fatalf("Take must hold its claim marker until Done/Abort; stat err = %v", err)
	}

	// A claimed item is mid-approval, not reviewable: it must not show up in
	// the review queue while someone is committing it.
	pending, err := a.Pending()
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("a claimed item must not be listed as pending, got %d", len(pending))
	}

	if err := a.Done(id); err != nil {
		t.Fatalf("Done: %v", err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Errorf("Done must remove the pending file; stat err = %v", err)
	}
	if _, err := os.Stat(claim); !os.IsNotExist(err) {
		t.Errorf("Done must remove the claim marker; stat err = %v", err)
	}
	if pending, _ := a.Pending(); len(pending) != 0 {
		t.Errorf("pending should be empty after Done, got %d", len(pending))
	}
}

// TestAbortReturnsItemToTheQueue is the other half: whoever claimed a memory
// and then could not commit it must be able to hand it back for a retry
// rather than destroying it.
func TestAbortReturnsItemToTheQueue(t *testing.T) {
	a, l := newArea(t)
	id := "2222333344445555"
	if err := a.Add(mem(id, "Retry me.")); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := a.Take(id); err != nil {
		t.Fatalf("Take: %v", err)
	}
	if err := a.Abort(id); err != nil {
		t.Fatalf("Abort: %v", err)
	}

	if _, err := os.Stat(filepath.Join(l.Pending, id+".md.taking")); !os.IsNotExist(err) {
		t.Errorf("Abort must remove the claim marker; stat err = %v", err)
	}
	pending, err := a.Pending()
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if len(pending) != 1 || pending[0].ID != id {
		t.Fatalf("aborted memory must be back in the queue, got %v", pending)
	}
	// And it must be takeable again.
	if _, err := a.Take(id); err != nil {
		t.Fatalf("Take after Abort: %v", err)
	}
}

// TestPendingListsItemWithStaleClaim proves the claim filter in Pending
// cannot wedge an item out of the review queue forever: a marker left by a
// crashed process is stale, and a stale claim must not hide the memory.
func TestPendingListsItemWithStaleClaim(t *testing.T) {
	a, l := newArea(t)
	id := "3333444455556666"
	if err := a.Add(mem(id, "body")); err != nil {
		t.Fatalf("Add: %v", err)
	}
	claim := filepath.Join(l.Pending, id+".md.taking")
	if err := os.WriteFile(claim, nil, 0o600); err != nil {
		t.Fatalf("plant claim: %v", err)
	}
	old := time.Now().Add(-2 * staleClaimAge)
	if err := os.Chtimes(claim, old, old); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	pending, err := a.Pending()
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("a stale claim must not hide a pending memory, got %d", len(pending))
	}
}

// TestClaimFreshnessThresholdIsPinned pins the actual VALUE of
// staleClaimAge, not merely that some threshold exists. The ages are
// absolute, deliberately: a test phrased in multiples of staleClaimAge is
// scale-invariant and passes for any value at all, including a nanosecond,
// which is exactly how the previous version of this test let a
// nanosecond-threshold mutant survive. These two cases bracket the constant
// into (5s, 10m]: shrink it below five seconds and the first case fails,
// grow it past ten minutes and the second does.
//
// The mtimes are set with Chtimes rather than by sleeping, so the assertion
// does not depend on filesystem timestamp granularity either.
func TestClaimFreshnessThresholdIsPinned(t *testing.T) {
	cases := []struct {
		name       string
		age        time.Duration
		wantRefuse bool
	}{
		{"five seconds old is a live claim", 5 * time.Second, true},
		{"ten minutes old is a stale claim", 10 * time.Minute, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, l := newArea(t)
			id := "4444555566667777"
			if err := a.Add(mem(id, "body")); err != nil {
				t.Fatalf("Add: %v", err)
			}
			claim := filepath.Join(l.Pending, id+".md.taking")
			if err := os.WriteFile(claim, nil, 0o600); err != nil {
				t.Fatalf("plant claim: %v", err)
			}
			when := time.Now().Add(-tc.age)
			if err := os.Chtimes(claim, when, when); err != nil {
				t.Fatalf("Chtimes: %v", err)
			}

			_, err := a.Take(id)
			if tc.wantRefuse && err == nil {
				t.Fatalf("Take stole a claim only %s old; staleClaimAge is %s and must be longer than that", tc.age, staleClaimAge)
			}
			if !tc.wantRefuse && err != nil {
				t.Fatalf("Take refused a claim %s old; staleClaimAge is %s and must be shorter than that: %v", tc.age, staleClaimAge, err)
			}
		})
	}
}

// TestFutureDatedClaimIsStale closes the mirror image of the crashed-process
// case. A marker whose mtime is in the future (clock skew, a restored
// backup, a hand-edited file) is never "older than staleClaimAge" under a
// naive time.Since check, so it wedges the id until wall-clock catches up —
// the same permanently-un-takeable state the stale-claim recovery exists to
// prevent, reached from the other direction.
func TestFutureDatedClaimIsStale(t *testing.T) {
	a, l := newArea(t)
	id := "5555666677778888"
	if err := a.Add(mem(id, "body")); err != nil {
		t.Fatalf("Add: %v", err)
	}
	claim := filepath.Join(l.Pending, id+".md.taking")
	if err := os.WriteFile(claim, nil, 0o600); err != nil {
		t.Fatalf("plant claim: %v", err)
	}
	future := time.Now().Add(2 * staleClaimAge)
	if err := os.Chtimes(claim, future, future); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	if _, err := a.Take(id); err != nil {
		t.Fatalf("a future-dated claim must be treated as stale, not as a live claim: %v", err)
	}
	if pending, _ := a.Pending(); len(pending) != 0 {
		t.Fatalf("after a successful Take the item is claimed and must not be listed, got %d", len(pending))
	}
}
