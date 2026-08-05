package server

import (
	"bytes"
	"encoding/hex"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/crypto"
	"github.com/alsotoes/momo/src/p2p"
	"github.com/alsotoes/momo/src/transport"
)

type fakeOPRFProvider struct {
	responses []p2p.OPRFEvalResponsePayload
}

func (f *fakeOPRFProvider) Evaluate(blinded []byte, timeout time.Duration) ([]p2p.OPRFEvalResponsePayload, int) {
	return f.responses, len(f.responses)
}

// realDaemons generates a genuine (t,n) Shamir share split and returns daemon
// configs holding one distinct share each.
func realDaemons(t *testing.T, thr, n int) []common.Daemon {
	t.Helper()
	shares, err := crypto.GenerateOPRFShares(thr, n)
	if err != nil {
		t.Fatal(err)
	}
	daemons := make([]common.Daemon, n)
	for i, pair := range shares {
		daemons[i] = common.Daemon{
			OPRFShare:      hex.EncodeToString(pair.Share),
			OPRFShareIndex: pair.ShareIndex,
		}
	}
	return daemons
}

func evalAll(t *testing.T, daemons []common.Daemon, blinded []byte, threshold int) []transport.OPRFEvalResult {
	t.Helper()
	var all []transport.OPRFEvalResult
	for i := range daemons {
		evaluator, err := newOPRFShareEvaluator(&daemons[i])
		if err != nil {
			t.Fatal(err)
		}
		eval, err := evaluator.EvaluateShare(evaluator.shareIndex, blinded)
		if err != nil {
			t.Fatalf("daemon %d eval failed: %v", i, err)
		}
		all = append(all, transport.OPRFEvalResult{ShareIndex: evaluator.shareIndex, Eval: eval})
	}
	return all
}

func combineResult(t *testing.T, results []transport.OPRFEvalResult, blind []byte, threshold int) []byte {
	t.Helper()
	seen := map[int]struct{}{}
	evals := make([]crypto.OPRFEvaluation, 0, len(results))
	for _, r := range results {
		if _, dup := seen[r.ShareIndex]; dup {
			continue
		}
		seen[r.ShareIndex] = struct{}{}
		evals = append(evals, crypto.OPRFEvaluation{ShareIndex: r.ShareIndex, Eval: r.Eval})
	}
	key, err := crypto.OPRFCombineUnblind(evals, blind, threshold)
	if err != nil {
		t.Fatalf("combine failed: %v", err)
	}
	return key
}

const testTag = "a shared plaintext dedup tag"

func TestDaemonOPRFService_ThresholdOneOwnShare(t *testing.T) {
	daemons := realDaemons(t, 1, 1)
	svc, err := newDaemonOPRFService(&daemons[0], nil, 1)
	if err != nil {
		t.Fatalf("newDaemonOPRFService failed: %v", err)
	}
	blinded, blind, err := crypto.OPRFBlind([]byte(testTag))
	if err != nil {
		t.Fatal(err)
	}
	results, err := svc.EvaluateOPRF(blinded, 2*time.Second)
	if err != nil {
		t.Fatalf("threshold-1 eval should succeed with the own share: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected exactly 1 evaluation, got %d", len(results))
	}
	if key := combineResult(t, results, blind, 1); len(key) == 0 {
		t.Fatal("derived empty content key")
	}
}

func TestDaemonOPRFService_FailClosedBelowQuorum(t *testing.T) {
	// Three daemons share-split with threshold 3, but this service is isolated
	// (no provider), so only its own share is available -> fail closed.
	daemons := realDaemons(t, 3, 3)
	svc, err := newDaemonOPRFService(&daemons[0], nil, 3)
	if err != nil {
		t.Fatal(err)
	}
	blinded, _, err := crypto.OPRFBlind([]byte(testTag))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.EvaluateOPRF(blinded, 2*time.Second); err == nil {
		t.Fatal("expected fail-closed when fewer than threshold evaluations are available")
	}

	// Adding peer evaluations from the provider reaches quorum.
	peers := evalAll(t, daemons[1:], blinded, 3)
	svc2, _ := newDaemonOPRFService(&daemons[0], &fakeOPRFProvider{responses: asPeerPayloads(peers)}, 3)
	results, err := svc2.EvaluateOPRF(blinded, 2*time.Second)
	if err != nil {
		t.Fatalf("quorum-satisfied eval should succeed: %v", err)
	}
	if len(results) < 3 {
		t.Fatalf("expected at least 3 distinct evaluations, got %d", len(results))
	}
}

func TestDaemonOPRFService_CrossClientDeterministicKey(t *testing.T) {
	daemons := realDaemons(t, 3, 3)

	// Client 1: blind, evaluate across all three daemons, combine.
	blinded1, blind1, err := crypto.OPRFBlind([]byte(testTag))
	if err != nil {
		t.Fatal(err)
	}
	key1 := combineResult(t, evalAll(t, daemons, blinded1, 3), blind1, 3)

	// Client 2: fresh blinding of the SAME tag must yield the SAME content key.
	blinded2, blind2, err := crypto.OPRFBlind([]byte(testTag))
	if err != nil {
		t.Fatal(err)
	}
	key2 := combineResult(t, evalAll(t, daemons, blinded2, 3), blind2, 3)

	if !bytes.Equal(key1, key2) {
		t.Fatalf("content keys differ across clients: %x != %x", key1, key2)
	}
}

func TestDaemonOPRFService_IgnoresErroredPeer(t *testing.T) {
	daemons := realDaemons(t, 2, 2)
	blinded, _, err := crypto.OPRFBlind([]byte(testTag))
	if err != nil {
		t.Fatal(err)
	}
	// Peer reports an error and share index 0 -> must be ignored; with only the
	// own share available and threshold 2, the eval must fail closed.
	failed := &fakeOPRFProvider{responses: []p2p.OPRFEvalResponsePayload{
		{ShareIndex: 0, Error: "boom"},
	}}
	svc, _ := newDaemonOPRFService(&daemons[0], failed, 2)
	if _, err := svc.EvaluateOPRF(blinded, time.Second); err == nil {
		t.Fatal("expected fail-closed when the only peer errored")
	}
}

func asPeerPayloads(results []transport.OPRFEvalResult) []p2p.OPRFEvalResponsePayload {
	out := make([]p2p.OPRFEvalResponsePayload, len(results))
	for i, r := range results {
		out[i] = p2p.OPRFEvalResponsePayload{ShareIndex: byte(r.ShareIndex), Eval: r.Eval}
	}
	return out
}
