package aead

import (
	"crypto/rand"
	"io"
	"net"
	"sync"

	"github.com/pkg/errors"

	"github.com/ProxyPanel/VNet-SSR/common/log"
	"github.com/ProxyPanel/VNet-SSR/common/pool"
)

const MAX_PACKET_SIZE = 65507

var _zerononce [128]byte
var ErrShortPacket = errors.New("short packet")

type aeadPacket struct {
	net.PacketConn
	IAEADCipher
	sync.Mutex
	key []byte
	buf []byte
}

func GetAEADPacketCiphers(method string) func(string, net.PacketConn) (net.PacketConn, error) {
	c, ok := aeadCiphers[method]
	if !ok {
		return nil
	}
	return func(password string, packCon net.PacketConn) (net.PacketConn, error) {
		salt := make([]byte, c.SaltSize())
		if _, err := io.ReadFull(rand.Reader, salt); err != nil {
			return nil, err
		}
		ap := &aeadPacket{
			PacketConn:  packCon,
			IAEADCipher: c,
			key:         evpBytesToKey(password, c.KeySize()),
			buf:         pool.GetBufBySize(MAX_PACKET_SIZE),
		}
		return ap, nil
	}
}
func (c *aeadPacket) GetKey() []byte {
	return c.key
}
func (c *aeadPacket) WriteTo(data []byte, addr net.Addr) (int, error) {
	c.Lock()
	defer c.Unlock()
	saltSize := c.SaltSize()
	salt := c.buf[:saltSize]
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return 0, err
	}

	aead, err := c.NewAEAD(c.key, salt, 0)

	if err != nil {
		return 0, err
	}
	if MAX_PACKET_SIZE < c.SaltSize()+len(data)+aead.Overhead() {
		return 0, errors.WithStack(io.ErrShortBuffer)
	}
	b := aead.Seal(c.buf[saltSize:saltSize], _zerononce[:aead.NonceSize()], data, nil)
	_, err = c.PacketConn.WriteTo(c.buf[:saltSize+len(b)], addr)
	return len(b), err
}

func (c *aeadPacket) ReadFrom(b []byte) (int, net.Addr, error) {
	n, addr, err := c.PacketConn.ReadFrom(b)
	if err != nil {
		return n, addr, err
	}
	saltSize := c.SaltSize()
	if len(b) < saltSize {
		return 0, nil, ErrShortPacket
	}
	salt := b[:saltSize]
	aead, err := c.NewAEAD(c.key, salt, 1)
	if err != nil {
		return 0, nil, err
	}

	if len(b) < saltSize+aead.Overhead() {
		return 0, nil, ErrShortPacket
	}

	if saltSize+len(c.buf)+aead.Overhead() < len(b) {
		return 0, nil, io.ErrShortBuffer
	}
	result, err := aead.Open(c.buf[:0], _zerononce[:aead.NonceSize()], b[saltSize:n], nil)
	if err != nil {
		log.Err(err)
		return n, addr, err
	}
	copy(b, result)
	return len(result), addr, err
}
