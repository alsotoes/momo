package crypto

import (
	"bytes"
	"testing"
)

func TestOPRFRoundTripDeterministic(t *testing.T) {
	shares, err := GenerateOPRFShares(3, 5)
	if err != nil {
		t.Fatalf("GenerateOPRFShares failed: %v", err)
	}

	tag := []byte("dedup-tag-for-content")

	blinded, blind, err := OPRFBlind(tag)
	if err != nil {
		t.Fatalf("OPRFBlind failed: %v", err)
	}

	evaluations := make([]OPRFEvaluation, 0, 3)
	for _, s := range shares[:3] {
		eval, err := OPRFEvaluateShare(s.Share, blinded)
		if err != nil {
			t.Fatalf("OPRFEvaluateShare failed: %v", err)
		}
		evaluations = append(evaluations, OPRFEvaluation{ShareIndex: s.ShareIndex, Eval: eval})
	}

	key1, err := OPRFCombineUnblind(evaluations, blind, 3)
	if err != nil {
		t.Fatalf("OPRFCombineUnblind failed: %v", err)
	}
	if len(key1) != KeySize {
		t.Fatalf("content key length = %d, want %d", len(key1), KeySize)
	}

	// A different quorum of the same shares must recover the same key.
	evaluations2 := make([]OPRFEvaluation, 0, 3)
	for _, s := range shares[2:] {
		eval, err := OPRFEvaluateShare(s.Share, blinded)
		if err != nil {
			t.Fatalf("OPRFEvaluateShare failed: %v", err)
		}
		evaluations2 = append(evaluations2, OPRFEvaluation{ShareIndex: s.ShareIndex, Eval: eval})
	}
	key2, err := OPRFCombineUnblind(evaluations2, blind, 3)
	if err != nil {
		t.Fatalf("OPRFCombineUnblind (quorum 2) failed: %v", err)
	}

	if !bytes.Equal(key1, key2) {
		t.Fatal("different quorums recovered different content keys")
	}
}

func TestOPRFFailClosedBelowThreshold(t *testing.T) {
	shares, err := GenerateOPRFShares(3, 5)
	if err != nil {
		t.Fatalf("GenerateOPRFShares failed: %v", err)
	}

	blinded, blind, err := OPRFBlind([]byte("tag"))
	if err != nil {
		t.Fatalf("OPRFBlind failed: %v", err)
	}

	// Only 2 evaluations for a threshold of 3 must fail closed.
	evaluations := make([]OPRFEvaluation, 0, 2)
	for _, s := range shares[:2] {
		eval, _ := OPRFEvaluateShare(s.Share, blinded)
		evaluations = append(evaluations, OPRFEvaluation{ShareIndex: s.ShareIndex, Eval: eval})
	}
	if _, err := OPRFCombineUnblind(evaluations, blind, 3); err == nil {
		t.Fatal("expected error when fewer than threshold evaluations supplied")
	}
}

func TestOPRFTamperedEvaluationFails(t *testing.T) {
	shares, _ := GenerateOPRFShares(2, 3)
	blinded, blind, _ := OPRFBlind([]byte("tag"))

	evaluations := make([]OPRFEvaluation, 0, 2)
	for _, s := range shares[:2] {
		eval, _ := OPRFEvaluateShare(s.Share, blinded)
		evaluations = append(evaluations, OPRFEvaluation{ShareIndex: s.ShareIndex, Eval: eval})
	}

	// Tamper with the first evaluation's payload.
	evaluations[0].Eval = append([]byte{}, evaluations[0].Eval...)
	evaluations[0].Eval[len(evaluations[0].Eval)-1] ^= 0xFF

	if _, err := OPRFCombineUnblind(evaluations, blind, 2); err == nil {
		t.Fatal("expected error when an evaluation is tampered")
	}
}

func TestOPRFRejectsDuplicateShareIndex(t *testing.T) {
	shares, _ := GenerateOPRFShares(2, 3)
	blinded, blind, _ := OPRFBlind([]byte("tag"))

	eval, _ := OPRFEvaluateShare(shares[0].Share, blinded)
	evaluations := []OPRFEvaluation{
		{ShareIndex: shares[0].ShareIndex, Eval: eval},
		{ShareIndex: shares[0].ShareIndex, Eval: eval},
	}
	if _, err := OPRFCombineUnblind(evaluations, blind, 2); err == nil {
		t.Fatal("expected error when the same share index is repeated")
	}
}

func TestOPRFBlindVariability(t *testing.T) {
	tag := []byte("same-tag")
	b1, _, _ := OPRFBlind(tag)
	b2, _, _ := OPRFBlind(tag)
	if bytes.Equal(b1, b2) {
		t.Fatal("OPRFBlind must randomize the blinded element")
	}
}

func TestOPRFThresholdOne(t *testing.T) {
	shares, err := GenerateOPRFShares(1, 3)
	if err != nil {
		t.Fatalf("GenerateOPRFShares failed: %v", err)
	}
	blinded, blind, _ := OPRFBlind([]byte("tag"))
	eval, _ := OPRFEvaluateShare(shares[0].Share, blinded)
	key, err := OPRFCombineUnblind([]OPRFEvaluation{{ShareIndex: shares[0].ShareIndex, Eval: eval}}, blind, 1)
	if err != nil {
		t.Fatalf("threshold 1 combine failed: %v", err)
	}
	if len(key) != KeySize {
		t.Fatalf("content key length = %d, want %d", len(key), KeySize)
	}
}

func TestGenerateOPRFSharesValidation(t *testing.T) {
	if _, err := GenerateOPRFShares(0, 3); err == nil {
		t.Fatal("expected error for threshold 0")
	}
	if _, err := GenerateOPRFShares(4, 3); err == nil {
		t.Fatal("expected error for threshold > n")
	}
}

func TestOPRFScalarLength(t *testing.T) {
	if got := ScalarLength(); got != 32 {
		t.Fatalf("ScalarLength = %d, want 32", got)
	}
}

func TestOPRFFailClosedDeterministicExample(t *testing.T) {
	// Reference smoke: different plaintexts must produce different keys for the
	// same quorum (deterministic per-tag), and identical tags produce identical
	// keys across clients.
	shares, _ := GenerateOPRFShares(3, 4)

	derive := func(tag []byte) []byte {
		blinded, blind, _ := OPRFBlind(tag)
		evals := make([]OPRFEvaluation, 0, 3)
		for _, s := range shares[:3] {
			e, _ := OPRFEvaluateShare(s.Share, blinded)
			evals = append(evals, OPRFEvaluation{ShareIndex: s.ShareIndex, Eval: e})
		}
		k, err := OPRFCombineUnblind(evals, blind, 3)
		if err != nil {
			t.Fatalf("combine failed: %v", err)
		}
		return k
	}

	kA1 := derive([]byte("content-a"))
	kA2 := derive([]byte("content-a"))
	kB := derive([]byte("content-b"))

	if !bytes.Equal(kA1, kA2) {
		t.Fatal("same tag produced different keys across clients")
	}
	if bytes.Equal(kA1, kB) {
		t.Fatal("different tags produced the same key")
	}
}

func BenchmarkOPRFCombineThreshold3(b *testing.B) {
	shares, _ := GenerateOPRFShares(3, 5)
	blinded, blind, _ := OPRFBlind([]byte("tag"))
	evals := make([]OPRFEvaluation, 0, 3)
	for _, s := range shares[:3] {
		e, _ := OPRFEvaluateShare(s.Share, blinded)
		evals = append(evals, OPRFEvaluation{ShareIndex: s.ShareIndex, Eval: e})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := OPRFCombineUnblind(evals, blind, 3); err != nil {
			b.Fatal(err)
		}
	}
}
