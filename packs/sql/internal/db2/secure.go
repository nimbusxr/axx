package db2

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/des" //nolint:gosec // DRDA's first encrypted security mechanisms are DES, as the standard has them
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
// encrypt in CBC mode with one of two ciphers (DRDA, Version 5, Volume 3,
// DHENC):
//
//   - AES with a key of 256 bits, from an agreement of 512 bits: the one a
//     server at level 9 of the security manager takes, and the only one a Db2
//     takes whose ALTERNATE_AUTH_ENC is AES_ONLY.
//   - DES, from an agreement of 256 bits: the standard's first, and weak by
//     today's measure.
//
// Neither side proves who it is to the other in that agreement, so what keeps
// a password from a network that is not one's own is TLS, and this is for the
// servers that ask for it.

// The ciphers, as ENCALG names them.
const (
	cipherDES = 1
	cipherAES = 2
)

// group is what the two sides' keys are made with: a prime, a base, and how
// many bits a side's own number has.
type group struct {
	prime, base *big.Int
	// size is the length of a public key and of the shared one, in bytes.
	size int
	// bits is the length of a side's own number.
	bits int
}

func hexInt(s string) *big.Int {
	n, ok := new(big.Int).SetString(s, 16)
	if !ok {
		panic("db2: a number of the standard's that is no number")
	}
	return n
}

// The standard's agreed values, for a shared key of 256 bits (DES) and of 512
// (AES).
var (
	group256 = group{
		prime: hexInt("C62112D73EE613F0947AB31F0F6846A1BFF5B3A4CA0D60BC1E4C7A0D8C16B3E3"),
		base:  hexInt("4690FA1F7B9E1D4442C86C9114603FDECF071EDCEC5F626E21E256AED9EA34E4"),
		size:  32, bits: 255,
	}
	group512 = group{
		prime: hexInt("F24F63150EAA97CCE78F5710C45FAFBEB71CF6A8724F63140EAA97CCE78F5710" +
			"C45FAFBEB71CF6A8724F631308E32B26EA1594889CBBFC91F6DF7524352EF979"),
		base: hexInt("E8CE9E0844C67A009FB7843CD945A058935DA51B028A49E5A91F831B78364491" +
			"CD0E0A8F72345DF80769549926FD16ECD6F6859481647CA9EFB2BAAC7BC09A92"),
		size: 64, bits: 512,
	}
)

// groupOf is the agreement a cipher's key comes from.
func groupOf(algorithm int) group {
	if algorithm == cipherAES {
		return group512
	}
	return group256
}

// random is where a connection's key comes from: this package's tests give
// one that is the same each time, for a session that replays.
var random io.Reader = rand.Reader

// agreement is one side of a key agreement.
type agreement struct {
	group
	private *big.Int
}

// newAgreement makes this side's number, of as many bits as the standard
// has for the group.
func newAgreement(g group) (*agreement, error) {
	b := make([]byte, (g.bits+7)/8)
	if _, err := io.ReadFull(random, b); err != nil {
		return nil, err
	}
	b[0] &= 0xFF >> (len(b)*8 - g.bits)
	private := new(big.Int).SetBytes(b)
	if private.Sign() == 0 {
		private.SetInt64(1)
	}
	return &agreement{group: g, private: private}, nil
}

// public is what this side sends the other.
func (a *agreement) public() []byte {
	return new(big.Int).Exp(a.base, a.private, a.prime).FillBytes(make([]byte, a.size))
}

// shared is the key the two sides share, from what the other side sent.
func (a *agreement) shared(theirs []byte) ([]byte, error) {
	if len(theirs) != a.size {
		return nil, fmt.Errorf("%w: a key of %d bytes for an encrypted password, where the standard's is of %d", errProtocol, len(theirs), a.size)
	}
	other := new(big.Int).SetBytes(theirs)
	if other.Sign() == 0 || other.Cmp(a.prime) >= 0 {
		return nil, fmt.Errorf("%w: a key for an encrypted password that is none", errProtocol)
	}
	return new(big.Int).Exp(other, a.private, a.prime).FillBytes(make([]byte, a.size)), nil
}

// cipherKey is the cipher's key, from the key the two sides share. DES's is
// the middle 8 of its 32 bytes. AES's is from the two halves of its 64, each
// hashed with SHA-1: the first 12 bytes of the first hash, its last 8 XORed
// with the first 8 of the second, and the last 12 of the second.
func cipherKey(algorithm int, shared []byte) []byte {
	if algorithm != cipherAES {
		return shared[12:20]
	}
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
// vector). It is the middle of the server's public key, 16 bytes for AES and
// 8 for DES, but for DES with the user as it is (mechanism 7): then it is the
// user, in 8 bytes.
func startingBlock(algorithm, mechanism int, user, serverKey []byte) []byte {
	switch {
	case algorithm == cipherAES:
		return serverKey[24:40]
	case mechanism == secEncryptedPassword:
		out := make([]byte, 8)
		copy(out, user)
		return out
	}
	return serverKey[12:20]
}

// encrypt is plain as a cipher encrypts it with a key, from a starting
// block: in CBC mode, padded as PKCS #5 pads, to the cipher's block.
func encrypt(algorithm int, key, start, plain []byte) ([]byte, error) {
	var block cipher.Block
	var err error
	if algorithm == cipherAES {
		block, err = aes.NewCipher(key)
	} else {
		block, err = des.NewCipher(key) //nolint:gosec // the standard's first cipher
	}
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
	cipher.NewCBCEncrypter(block, start).CryptBlocks(out, padded) //nolint:gosec // the standard's mode
	return out, nil
}
