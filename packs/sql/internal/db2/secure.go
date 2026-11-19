package db2

import (
	"crypto/cipher"
	"crypto/des" //nolint:gosec // DRDA's encrypted security mechanisms are DES, as the standard has them
	"crypto/rand"
	"fmt"
	"io"
	"math/big"
)

// DRDA's encrypted security mechanisms (7, a user and an encrypted password,
// and 9, both encrypted) are for a server that takes no password as it is,
// like a Db2 whose AUTHENTICATION is SERVER_ENCRYPT. The two sides agree on a
// key by Diffie-Hellman, with a prime and a base of 256 bits the standard
// gives, and encrypt with DES in CBC mode. Both are of the standard's first
// years and weak by today's measure: what keeps a password from a network
// that is not one's own is TLS, and this is for the servers that ask for it.

// The prime and the base the two sides' keys are made with.
var (
	dhPrime, _ = new(big.Int).SetString("C62112D73EE613F0947AB31F0F6846A1BFF5B3A4CA0D60BC1E4C7A0D8C16B3E3", 16)
	dhBase, _  = new(big.Int).SetString("4690FA1F7B9E1D4442C86C9114603FDECF071EDCEC5F626E21E256AED9EA34E4", 16)
)

// random is where a connection's key comes from: this package's tests give
// one that is the same each time, for a session that replays.
var random io.Reader = rand.Reader

// agreement is one side of a key agreement.
type agreement struct{ private *big.Int }

// newAgreement makes this side's key: a number of 255 bits, as the standard
// has it.
func newAgreement() (*agreement, error) {
	b := make([]byte, 32)
	if _, err := io.ReadFull(random, b); err != nil {
		return nil, err
	}
	b[0] &= 0x7F
	private := new(big.Int).SetBytes(b)
	if private.Sign() == 0 {
		private.SetInt64(1)
	}
	return &agreement{private: private}, nil
}

// public is what this side sends the other: 32 bytes.
func (a *agreement) public() []byte {
	return new(big.Int).Exp(dhBase, a.private, dhPrime).FillBytes(make([]byte, 32))
}

// key is the key the two sides share, from what the other side sent: the
// middle 8 of its 32 bytes.
func (a *agreement) key(theirs []byte) ([]byte, error) {
	if len(theirs) != 32 {
		return nil, fmt.Errorf("%w: a key of %d bytes for an encrypted password, where the standard's is of 32", errProtocol, len(theirs))
	}
	other := new(big.Int).SetBytes(theirs)
	if other.Sign() == 0 || other.Cmp(dhPrime) >= 0 {
		return nil, fmt.Errorf("%w: a key for an encrypted password that is none", errProtocol)
	}
	shared := new(big.Int).Exp(other, a.private, dhPrime).FillBytes(make([]byte, 32))
	return shared[12:20], nil
}

// encrypt is plain as the two sides' key encrypts it, from a token of 8
// bytes: DES in CBC mode, padded as PKCS #5 pads.
func encrypt(key, token, plain []byte) ([]byte, error) {
	block, err := des.NewCipher(key) //nolint:gosec // the standard's cipher
	if err != nil {
		return nil, err
	}
	pad := des.BlockSize - len(plain)%des.BlockSize
	padded := make([]byte, len(plain)+pad)
	copy(padded, plain)
	for i := len(plain); i < len(padded); i++ {
		padded[i] = byte(pad)
	}
	out := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, token).CryptBlocks(out, padded) //nolint:gosec // the standard's mode
	return out, nil
}

// token is what the encryption starts from. With the user encrypted too, it
// is the middle 8 bytes of the server's key; with the user as it is, it is
// the user, in 8 bytes.
func token(mechanism int, user, serverKey []byte) []byte {
	out := make([]byte, 8)
	if mechanism == secEncryptedPassword {
		copy(out, user)
		return out
	}
	copy(out, serverKey[12:20])
	return out
}
