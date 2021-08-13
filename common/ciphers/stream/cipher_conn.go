package stream

import (
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"io"
	"net"

	"github.com/ProxyPanel/VNet-SSR/common/log"
	"github.com/ProxyPanel/VNet-SSR/common/pool"
)

func GetStreamConnCiphers(method string) func(string, net.Conn) (net.Conn, error) {
	c, ok := streamCiphers[method]
	if !ok {
		return nil
	}
	return func(password string, conn net.Conn) (net.Conn, error) {
		iv := make([]byte, c.IVLen())
		if _, err := io.ReadFull(rand.Reader, iv); err != nil {
			return nil, err
		}
		sc := &streamConn{
			Conn:         conn,
			IStreamCipher: c,
			key:           evpBytesToKey(password, c.KeyLen()),
		}
		var err error
		sc.Encrypter, err = sc.NewStream(sc.key, iv, 0)
		_, err = conn.Write(iv)
		return sc, err
	}
}

type streamConn struct {
	net.Conn
	IStreamCipher
	key       []byte
	Encrypter cipher.Stream
	Decrypter cipher.Stream
}

func (s *streamConn) GetKey() []byte {
	return s.key
}

func (s *streamConn) Read(b []byte) (n int, err error) {
	if s.Decrypter == nil {
		iv := make([]byte, s.IVLen())
		if _, err = s.Conn.Read(iv); err != nil {
			return
		}
		s.Decrypter, err = s.NewStream(s.key, iv, 1)
		if err != nil {
			log.Error("[Stream Conn] init decrypter failed: %v", err)
			return 0, err
		}
	}
	buf := pool.GetBuf()
	if len(buf) < len(b) {
		pool.PutBuf(buf)
		buf = make([]byte, len(b))
	} else {
		defer pool.PutBuf(buf)
	}

	buf = buf[:len(b)]
	n, err = s.Conn.Read(buf)
	if err != nil {
		return
	}
	s.Decrypter.XORKeyStream(b[:n], buf[:n])
	return
}

func (s *streamConn) Write(b []byte) (n int, err error) {
	buf := pool.GetBuf()
	if len(buf) < len(b) {
		pool.PutBuf(buf)
		buf = make([]byte, len(b))
	} else {
		buf = buf[:len(b)]
		defer pool.PutBuf(buf)
	}
	s.Encrypter.XORKeyStream(buf, b)
	return s.Conn.Write(buf)
}

func evpBytesToKey(password string, keyLen int) (key []byte) {
	const md5Len = 16

	cnt := (keyLen-1)/md5Len + 1
	m := make([]byte, cnt*md5Len)
	copy(m, MD5([]byte(password)))
	d := make([]byte, md5Len+len(password))
	start := 0
	for i := 1; i < cnt; i++ {
		start += md5Len
		copy(d, m[start-md5Len:start])
		copy(d[md5Len:], password)
		copy(m[start:], MD5(d))
	}
	return m[:keyLen]
}

func MD5(data []byte) []byte {
	hash := md5.New()
	hash.Write(data)
	return hash.Sum(nil)
}
