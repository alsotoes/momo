package server

import (
	"encoding/hex"
	"fmt"
	"syscall"
	"time"

	"github.com/alsotoes/momo/src/common"
	momocrypto "github.com/alsotoes/momo/src/crypto"
	"github.com/alsotoes/momo/src/p2p"
	"github.com/alsotoes/momo/src/transport"
)

// oprfShareEvaluator evaluates blinded OPRF points with a single daemon's
// Shamir share. It labels evaluations with the daemon's own share index so
// the requesting client can interpolate across a quorum.
type oprfShareEvaluator struct {
	shareIndex  int
	shareScalar []byte
}

// ShareIndex returns this daemon's Shamir evaluation point.
func (e *oprfShareEvaluator) ShareIndex() int { return e.shareIndex }

// EvaluateShare evaluates a blinded OPRF point under this daemon's share.
// The shareIndex argument is informational (each daemon holds one share).
func (e *oprfShareEvaluator) EvaluateShare(shareIndex int, blinded []byte) ([]byte, error) {
	return momocrypto.OPRFEvaluateShare(e.shareScalar, blinded)
}

// newOPRFShareEvaluator builds an evaluator from a daemon's configured share.
func newOPRFShareEvaluator(d *common.Daemon) (*oprfShareEvaluator, error) {
	if d.OPRFShare == "" {
		return nil, fmt.Errorf("daemon is missing 'oprf_share': %w", syscall.EINVAL)
	}
	shareScalar, err := hex.DecodeString(d.OPRFShare)
	if err != nil {
		return nil, fmt.Errorf("invalid 'oprf_share': %w", err)
	}
	return &oprfShareEvaluator{
		shareIndex:  d.OPRFShareIndex,
		shareScalar: shareScalar,
	}, nil
}

// oprfPeerEvaluator gathers peer share evaluations over the P2P transport. It
// is implemented by *p2p.OPRFProvider; the narrow interface keeps the service
// unit-testable without a live transport.
type oprfPeerEvaluator interface {
	Evaluate(blinded []byte, timeout time.Duration) ([]p2p.OPRFEvalResponsePayload, int)
}

// daemonOPRFService answers ModeOPRFEval requests from clients. It combines
// this daemon's own share evaluation with peer evaluations gathered over the
// P2P transport and fails closed when the quorum is not met. No evaluation is
// ever unblinded or logged here.
type daemonOPRFService struct {
	evaluator *oprfShareEvaluator
	provider  oprfPeerEvaluator
	threshold int
}

// EvaluateOPRF returns the distinct share evaluations for a blinded dedup tag,
// failing closed (error, no partial result) when fewer than threshold distinct
// evaluations are available.
func (s *daemonOPRFService) EvaluateOPRF(blinded []byte, timeout time.Duration) ([]transport.OPRFEvalResult, error) {
	ownEval, err := s.evaluator.EvaluateShare(s.evaluator.shareIndex, blinded)
	if err != nil {
		return nil, fmt.Errorf("oprf: local evaluation failed: %w", err)
	}

	seen := map[int]struct{}{s.evaluator.shareIndex: {}}
	results := []transport.OPRFEvalResult{
		{ShareIndex: s.evaluator.shareIndex, Eval: ownEval},
	}

	if s.provider != nil {
		peerResponses, _ := s.provider.Evaluate(blinded, timeout)
		for _, pr := range peerResponses {
			idx := int(pr.ShareIndex)
			if pr.Error != "" || len(pr.Eval) == 0 || idx <= 0 {
				continue
			}
			if _, dup := seen[idx]; dup {
				continue
			}
			seen[idx] = struct{}{}
			results = append(results, transport.OPRFEvalResult{ShareIndex: idx, Eval: pr.Eval})
		}
	}

	if len(results) < s.threshold {
		return nil, fmt.Errorf("oprf: only %d distinct evaluations, need %d (quorum not met)", len(results), s.threshold)
	}
	return results, nil
}

// newDaemonOPRFService builds the client-facing OPRF service for a daemon.
// provider may be nil (threshold == 1 without P2P).
func newDaemonOPRFService(d *common.Daemon, provider oprfPeerEvaluator, threshold int) (transport.OPRFService, error) {
	evaluator, err := newOPRFShareEvaluator(d)
	if err != nil {
		return nil, err
	}
	return &daemonOPRFService{
		evaluator: evaluator,
		provider:  provider,
		threshold: threshold,
	}, nil
}
