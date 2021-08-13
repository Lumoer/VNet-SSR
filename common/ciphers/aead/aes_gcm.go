package aead

import (
	"crypto/aes"
	"crypto/cipher"
	"github.com/ProxyPanel/VNet-SSR/utils/shadowsocksx"
)

func init() {
	registerAEADCiphers("aes-256-gcm", &aesGcm{32, 32, 12, 16})
	registerAEADCiphers("aes-192-gcm", &aesGcm{24, 24, 12, 16})
	registerAEADCiphers("aes-128-gcm", &aesGcm{16, 16, 12, 16})
}

type aesGcm struct {
	keySize   int
	saltSize  int
	nonceSize int
	tagSize   int
}

func (a *aesGcm) KeySize() int {
	return a.keySize
}

func (a *aesGcm) SaltSize() int {
	return a.saltSize
}

func (a *aesGcm) NonceSize() int {
	return a.nonceSize
}

func (a *aesGcm) NewAEAD(key []byte, salt []byte, _ int) (cipher.AEAD, error) {
	subkey := make([]byte, a.KeySize())
	_ = shadowsocksx.HKDF_SHA1(key, salt, []byte("ss-subkey"), subkey)
	blk, err := aes.NewCipher(subkey)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(blk)
}
