package valueobjects

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// ============================================================================
// TRANSACTION HASH — SHA-256 wrapper for the ledger hash chain
// ============================================================================

// GenesisPrevHash is the prev_hash used by the first transaction in a user's
// chain, per Phase D Step 3: "First tx per user has prev_hash = '0'".
const GenesisPrevHash = "0"

var hex64Re = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ErrInvalidHash is returned when a string is not a valid chain hash.
var ErrInvalidHash = errors.New("ledger: invalid transaction hash")

// TransactionHash is an immutable wrapper around a 64-char lowercase hex
// SHA-256 digest, or the special genesis sentinel "0".
type TransactionHash struct {
	value string // "0" (genesis) or 64-hex chars
}

// NewTransactionHash validates and wraps an existing hex digest or "0".
func NewTransactionHash(s string) (TransactionHash, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == GenesisPrevHash || hex64Re.MatchString(s) {
		return TransactionHash{value: s}, nil
	}
	return TransactionHash{}, fmt.Errorf("%w: %q", ErrInvalidHash, s)
}

// MustTransactionHash panics on invalid input; use in tests/consts only.
func MustTransactionHash(s string) TransactionHash {
	h, err := NewTransactionHash(s)
	if err != nil {
		panic(err)
	}
	return h
}

// HashFromDigest wraps a raw 32-byte SHA-256 digest.
func HashFromDigest(d [sha256.Size]byte) TransactionHash {
	return TransactionHash{value: hex.EncodeToString(d[:])}
}

// IsGenesis reports whether this is the chain's sentinel predecessor.
func (h TransactionHash) IsGenesis() bool { return h.value == GenesisPrevHash }

// Hex returns the lowercase hex representation ("0" for genesis).
func (h TransactionHash) Hex() string { return h.value }

// String implements fmt.Stringer.
func (h TransactionHash) String() string { return h.value }

// Equal reports hash equality.
func (h TransactionHash) Equal(other TransactionHash) bool { return h.value == other.value }

// ComputeTxHash builds the chain hash per Phase D Step 3:
//
//	hash = SHA256(tx_id + prev_hash + user_id + amount + balance_after + timestamp)
//
// Fields are concatenated with '|' separators over their canonical string
// forms (amount/balance as integer ETB cents, timestamp as Unix nanoseconds)
// so recomputation is deterministic across languages/services.
func ComputeTxHash(txID, userID string, prevHash TransactionHash, amountCents, balanceAfterCents int64, timestampUnixNano int64) TransactionHash {
	var b strings.Builder
	b.WriteString(strings.ToLower(txID))
	b.WriteByte('|')
	b.WriteString(prevHash.Hex())
	b.WriteByte('|')
	b.WriteString(strings.ToLower(userID))
	b.WriteByte('|')
	fmt.Fprintf(&b, "%d", amountCents)
	b.WriteByte('|')
	fmt.Fprintf(&b, "%d", balanceAfterCents)
	b.WriteByte('|')
	fmt.Fprintf(&b, "%d", timestampUnixNano)
	sum := sha256.Sum256([]byte(b.String()))
	return HashFromDigest(sum)
}
