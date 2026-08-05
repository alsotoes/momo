package client

import (
	"encoding/hex"
	"fmt"
	"syscall"

	"github.com/alsotoes/momo/src/common"
	momocrypto "github.com/alsotoes/momo/src/crypto"
	"github.com/alsotoes/momo/src/transport"
)

// deriveOPRFContentKey derives the content key for a dedup tag via the
// threshold OPRF. It dials the primary daemon, blinds the tag, requests share
// evaluations from the cluster quorum over the main transport, and combines
// them locally. The tag is never revealed to any daemon's OPRF service; only
// the blinded tag is sent. It fails closed when fewer than threshold distinct
// evaluations are available.
func deriveOPRFContentKey(cfg common.Configuration, tagHex string, serverId int) ([]byte, error) {
	tag, err := hex.DecodeString(tagHex)
	if err != nil {
		return nil, fmt.Errorf("oprf: invalid tag hex: %w", err)
	}

	blinded, blind, err := momocrypto.OPRFBlind(tag)
	if err != nil {
		return nil, fmt.Errorf("oprf: failed to blind tag: %w", err)
	}

	threshold := cfg.Global.OPRFThreshold
	factory := transport.NewProtocolFactory(cfg)
	comm, err := factory.Dial(cfg.Daemons[serverId].Host)
	if err != nil {
		return nil, fmt.Errorf("oprf: failed to connect to daemon: %w", err)
	}
	defer comm.Close()

	results, err := comm.SendOPRFEval(cfg.Global.AuthToken, 0, blinded, threshold)
	if err != nil {
		// Fail closed: no convergent fallback (spec requirement).
		return nil, fmt.Errorf("oprf: evaluation failed: %w", err)
	}

	evaluations := make([]momocrypto.OPRFEvaluation, len(results))
	for i, r := range results {
		evaluations[i] = momocrypto.OPRFEvaluation{ShareIndex: r.ShareIndex, Eval: r.Eval}
	}

	contentKey, err := momocrypto.OPRFCombineUnblind(evaluations, blind, threshold)
	if err != nil {
		return nil, fmt.Errorf("oprf: failed to derive content key: %w", err)
	}
	if len(contentKey) == 0 {
		return nil, fmt.Errorf("oprf: empty content key: %w", syscall.EBADMSG)
	}
	return contentKey, nil
}
