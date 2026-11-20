package db2

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // the standard derives its AES key with SHA-1
	"fmt"
	"io"
	"math/big"
)

// DRDA's encrypted security mechanisms (7, a user and an encrypted password,
// and 9, both encrypted) are for a server that takes no password as it is,
// like a Db2 whose AUTHENTICATION is SERVER_ENCRYPT. The two sides agree on a
// key by Diffie-Hellman, with a prime and a base the standard gives, and
// encrypt with AES in CBC mode, with a key of 256 bits from an agreement of
// 512 (DRDA, Version 5, Volume 3, DHENC). It is what a server at level 9 of
// the security manager takes, and the only cipher a Db2 takes whose
// ALTERNATE_AUTH_ENC is AES_ONLY.
//
// The standard's first cipher, DES from an agreement of 256 bits, is not
// here: its key of 56 bits is one a recorded login gives up in hours.
//
// Neither side proves who it is to the other in that agreement, so what keeps
// a password from a network that is not one's own is TLS, and this is for the
// servers that ask for it.

// cipherAES is AES, as ENCALG names it.
const cipherAES = 2

// The length of a public key and of the shared one, in bytes, and of a
// side's own number, in bits.
const (
	keySize     = 64
	privateBits = 512
)

func hexInt(s string) *big.Int {
	n, ok := new(big.Int).SetString(s, 16)
	if !ok {
		panic("db2: a number of the standard's that is no number")
	}
	return n
}

// The standard's agreed values for a shared key of 512 bits.
var (
	dhPrime = hexInt("F24F63150EAA97CCE78F5710C45FAFBEB71CF6A8724F63140EAA97CCE78F5710" +
		"C45FAFBEB71CF6A8724F631308E32B26EA1594889CBBFC91F6DF7524352EF979")
	dhBase = hexInt("E8CE9E0844C67A009FB7843CD945A058935DA51B028A49E5A91F831B78364491" +
		"CD0E0A8F72345DF80769549926FD16ECD6F6859481647CA9EFB2BAAC7BC09A92")
)

// random is where a connection's key comes from: this package's tests give
// one that is the same each time, for a session that replays.
var random io.Reader = rand.Reader

// agreement is one side of a key agreement.
type agreement struct {
	private *big.Int
}

// newAgreement makes this side's number, of as many bits as the standard
// has.
func newAgreement() (*agreement, error) {
	b := make([]byte, privateBits/8)
	if _, err := io.ReadFull(random, b); err != nil {
		return nil, err
	}
	private := new(big.Int).SetBytes(b)
	if private.Sign() == 0 {
		private.SetInt64(1)
	}
	return &agreement{private: private}, nil
}

// public is what this side sends the other.
func (a *agreement) public() []byte {
	return new(big.Int).Exp(dhBase, a.private, dhPrime).FillBytes(make([]byte, keySize))
}

// shared is the key the two sides share, from what the other side sent.
func (a *agreement) shared(theirs []byte) ([]byte, error) {
	if len(theirs) != keySize {
		return nil, fmt.Errorf("%w: a key of %d bytes for an encrypted password, where the standard's is of %d", errProtocol, len(theirs), keySize)
	}
	other := new(big.Int).SetBytes(theirs)
	if other.Sign() == 0 || other.Cmp(dhPrime) >= 0 {
		return nil, fmt.Errorf("%w: a key for an encrypted password that is none", errProtocol)
	}
	return new(big.Int).Exp(other, a.private, dhPrime).FillBytes(make([]byte, keySize)), nil
}

// cipherKey is AES's key, from the key the two sides share: of the two
// halves of its 64 bytes, each hashed with SHA-1, the first 12 bytes of the
// first hash, its last 8 XORed with the first 8 of the second, and the last
// 12 of the second.
func cipherKey(shared []byte) []byte {
	first := sha1.Sum(shared[:32]) //nolint:gosec // the standard's derivation
	second := sha1.Sum(shared[32:])
	key := make([]byte, 0, 32)
	key = append(key, first[:12]...)
	for i := range 8 {
		key = append(key, first[12+i]^second[i])
	}
	return append(key, second[8:]...)
}

// startingBlock is what the encryption starts from (CBC's initialization
// vector): the middle 16 bytes of the server's public key.
func startingBlock(serverKey []byte) []byte { return serverKey[24:40] }

// encrypt is plain as AES encrypts it with a key, from a starting block: in
// CBC mode, padded as PKCS #5 pads, to the cipher's block.
func encrypt(key, start, plain []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	size := block.BlockSize()
	pad := size - len(plain)%size
	padded := make([]byte, len(plain)+pad)
	copy(padded, plain)
	for i := len(plain); i < len(padded); i++ {
		padded[i] = byte(pad)
	}
	out := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, start).CryptBlocks(out, padded)
	return out, nil
}
