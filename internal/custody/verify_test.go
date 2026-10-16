package custody

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"math/big"
	"testing"
)

type fakeChannelReader struct {
	// message, if set, is returned verbatim regardless of amount.
	message []byte
	// messageFor, if set, takes priority over message and lets a test vary
	// the returned bytes by amount (e.g. to prove a signature for one
	// amount does not verify against another).
	messageFor func(amount *big.Int) []byte
	err        error
}

func (f fakeChannelReader) PrepareCommitment(_ context.Context, amount *big.Int) ([]byte, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.messageFor != nil {
		return f.messageFor(amount), nil
	}
	return f.message, nil
}

func TestVerifyCommitment_ValidSignature(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	message := []byte("the exact bytes prepare_commitment would predict")
	sig := ed25519.Sign(priv, message)

	channel := fakeChannelReader{message: message}
	valid, err := VerifyCommitment(context.Background(), channel, pub, big.NewInt(100), sig)
	if err != nil {
		t.Fatalf("VerifyCommitment returned unexpected error: %v", err)
	}
	if !valid {
		t.Error("VerifyCommitment = false for a genuinely valid signature")
	}
}

func TestVerifyCommitment_WrongKeyIsInvalidNotError(t *testing.T) {
	_, funderPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	attackerPub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	message := []byte("the exact bytes prepare_commitment would predict")
	sig := ed25519.Sign(funderPriv, message) // signed by the funder...

	channel := fakeChannelReader{message: message}
	// ...but verified against the attacker's own key, not the funder's —
	// this is exactly the "self-signed, internally-consistent commitment
	// under an attacker's own key" scenario the doc comment calls out.
	valid, err := VerifyCommitment(context.Background(), channel, attackerPub, big.NewInt(100), sig)
	if err != nil {
		t.Fatalf("VerifyCommitment returned unexpected error: %v", err)
	}
	if valid {
		t.Error("VerifyCommitment = true for a signature under the wrong key")
	}
}

func TestVerifyCommitment_SignatureForADifferentAmountIsInvalid(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	channel := fakeChannelReader{messageFor: func(amount *big.Int) []byte {
		return []byte("commitment for amount " + amount.String())
	}}

	sigFor100 := ed25519.Sign(priv, []byte("commitment for amount 100"))

	// The same signature, valid for amount 100, must not verify for a
	// different amount's (different) message.
	valid, err := VerifyCommitment(context.Background(), channel, pub, big.NewInt(200), sigFor100)
	if err != nil {
		t.Fatalf("VerifyCommitment returned unexpected error: %v", err)
	}
	if valid {
		t.Error("VerifyCommitment = true for a signature over a different amount")
	}
}

func TestVerifyCommitment_MalformedSignatureIsInvalidNotError(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	channel := fakeChannelReader{message: []byte("message")}

	valid, err := VerifyCommitment(context.Background(), channel, pub, big.NewInt(1), []byte("too-short"))
	if err != nil {
		t.Fatalf("VerifyCommitment returned unexpected error: %v", err)
	}
	if valid {
		t.Error("VerifyCommitment = true for a malformed (wrong-length) signature")
	}
}

func TestVerifyCommitment_NilAmount(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(nil)
	_, err := VerifyCommitment(context.Background(), fakeChannelReader{}, pub, nil, []byte("sig"))
	if !errors.Is(err, ErrNilAmount) {
		t.Fatalf("error = %v, want ErrNilAmount", err)
	}
}

func TestVerifyCommitment_NonPositiveAmount(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(nil)
	for _, amount := range []int64{0, -1, -1000} {
		_, err := VerifyCommitment(context.Background(), fakeChannelReader{}, pub, big.NewInt(amount), []byte("sig"))
		if !errors.Is(err, ErrNonPositiveAmount) {
			t.Errorf("amount %d: error = %v, want ErrNonPositiveAmount", amount, err)
		}
	}
}

func TestVerifyCommitment_InvalidSignerKeyLength(t *testing.T) {
	_, err := VerifyCommitment(context.Background(), fakeChannelReader{}, []byte("too-short"), big.NewInt(1), []byte("sig"))
	if !errors.Is(err, ErrInvalidSignerKey) {
		t.Fatalf("error = %v, want ErrInvalidSignerKey", err)
	}
}

func TestVerifyCommitment_PrepareCommitmentErrorPropagates(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(nil)
	boom := errors.New("rpc: simulateTransaction failed")
	channel := fakeChannelReader{err: boom}

	_, err := VerifyCommitment(context.Background(), channel, pub, big.NewInt(1), []byte("sig"))
	if !errors.Is(err, boom) {
		t.Fatalf("error = %v, want it to wrap %v", err, boom)
	}
}

// ExampleChannelReader shows how a caller actually uses ChannelReader.
// The interface itself has no fields to set — it is one method, so what
// a caller "constructs" is its implementation (*internal/stellar.Channel
// in production, whose PrepareCommitment runs the live prepare_commitment
// simulation the doc comment describes) and then passes that to
// VerifyCommitment. The interface's own zero value is nil, and that is
// never what a caller passes; the stand-in's caller-supplied field is
// the message, and everything after the call — argument validation, the
// ed25519 check, the (false, nil) versus error distinction — is the
// package filling in its side of the contract.
func ExampleChannelReader() {
	pub, priv, _ := ed25519.GenerateKey(nil)
	message := []byte("the exact bytes prepare_commitment would predict")
	sig := ed25519.Sign(priv, message)

	// Properly constructed: message — the bytes this channel's
	// prepare_commitment predicts for the amount — is the one field the
	// caller's side supplies. The stand-in's other fields (err,
	// messageFor) are left alone; they are failure/variation hooks, not
	// values a caller configures.
	channel := fakeChannelReader{message: message}
	valid, err := VerifyCommitment(context.Background(), channel, pub, big.NewInt(100), sig)
	fmt.Println("properly constructed:", valid, err)

	// Unwired — the implementation equivalent of a zero value — returns
	// no message, so verification completes and finds the signature
	// invalid: (false, nil), a completed outcome, not an error.
	valid, err = VerifyCommitment(context.Background(), fakeChannelReader{}, pub, big.NewInt(100), sig)
	fmt.Println("unwired:", valid, err)

	// Output:
	// properly constructed: true <nil>
	// unwired: false <nil>
}
