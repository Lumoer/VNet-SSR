package network

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"github.com/ProxyPanel/VNet-SSR/common"
	"github.com/ProxyPanel/VNet-SSR/common/ciphers"
	"github.com/ProxyPanel/VNet-SSR/common/obfs"
	"github.com/ProxyPanel/VNet-SSR/utils/addrx"
	"github.com/ProxyPanel/VNet-SSR/utils/binaryx"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"net"
	"strings"
	"sync"
	"sync/atomic"
)

type ILimiter interface {
	Wait(int, int) error
	DownLimit(int, int) error
	UpLimit(int, int) error
}

func NewShadowsocksRDecorate(request *Request, obfsMethod, cryptMethod, key, protocolMethod, obfsParam, protocolParam, host string, port int, isLocal bool, single int, users map[string]string) (ssrd *ShadowsocksRDecorate, err error) {
	// init essential parameters
	ssrd = &ShadowsocksRDecorate{
		Request:       request,
		ObfsParam:     obfsParam,
		ProtocolParam: protocolParam,
		Host:          host,
		Port:          port,
		ISLocal:       isLocal,
		Users:         users,
		single:        single,
		recvBuf:       new(bytes.Buffer),
	}

	// init obfs protocol encrypto component
	ssrd.obfs, err = obfs.GetObfs(obfsMethod)
	if err != nil {
		return nil, err
	}

	ssrd.protocol, err = obfs.GetObfs(protocolMethod)
	if err != nil {
		return nil, err
	}

	ssrd.encryptor, err = ciphers.NewEncryptor(cryptMethod, key)
	if err != nil {
		return nil, err
	}

	ssrd.Overhead = ssrd.obfs.GetOverhead(isLocal) + ssrd.protocol.GetOverhead(isLocal)

	// set serverinfo
	ssrd.obfs.SetServerInfo(ssrd.getServerInfo(true))
	ssrd.protocol.SetServerInfo(ssrd.getServerInfo(false))

	if single != 1 {
		ssrd.UID = port
	}
	return ssrd, err
}

type ShadowsocksRDecorate struct {
	*Request
	UID           int
	obfs          obfs.Plain
	protocol      obfs.Plain
	encryptor     *ciphers.Encryptor
	Host          string
	Port          int
	ObfsParam     string
	ProtocolParam string
	Users         map[string]string
	Overhead      int
	ISLocal       bool
	recvBuf       *bytes.Buffer
	upload        int64
	download      int64
	single        int
	common.TrafficReport
	ILimiter
	*sync.Mutex
}

func (ssrd *ShadowsocksRDecorate) SetLimter(limiter ILimiter) {
	ssrd.ILimiter = limiter
}

func (ssrd *ShadowsocksRDecorate) Read(buf []byte) (n int, err error) {
	defer func() {
		if ssrd.ILimiter != nil {
			if err := ssrd.ILimiter.UpLimit(ssrd.UID, n); err != nil {
				logrus.Error(err)
			}
		}
	}()

	// ServerDecode return buffer_to_recv, is_need_decrypt, is_need_to_encode_and_send_back
	if ssrd.recvBuf.Len() > 0 {
		return ssrd.recvBuf.Read(buf)
	}

	bufTmp := make([]byte, 4*1024)
	n, err = ssrd.Conn.Read(bufTmp)
	if err != nil {
		return 0, err
	}
	atomic.AddInt64(&ssrd.upload, int64(n))

	data := bufTmp[:n]
	unobfsData, needDecrypt, needSendBack, err := ssrd.obfs.ServerDecode(data)
	if logrus.GetLevel() == logrus.DebugLevel {
		logrus.WithFields(logrus.Fields{
			"requestId":    ssrd.RequestID,
			"data":         hex.EncodeToString(data),
			"unobfsData":   hex.EncodeToString(unobfsData),
			"needDecrypt":  needDecrypt,
			"needSendBack": needSendBack,
		}).Debug("shadowsocksr obfs ServerDecode")
	}

	if err != nil {
		if logrus.GetLevel() == logrus.DebugLevel {
			logrus.WithFields(logrus.Fields{
				"err": err,
			}).Debugf("ShadowsocksRDecorate obfs decrypt error.")
		}
		return 0, errors.New(fmt.Sprintf("[%s] shadowsocksr obfs decrypt error.", ssrd.RequestID))
	}

	if needSendBack {
		result, err := ssrd.obfs.ServerEncode([]byte{})
		if err != nil {
			return 0, err
		}
		n, err = ssrd.Conn.Write(result)
		if err != nil {
			return 0, errors.Wrap(err, fmt.Sprintf("[%s] ShadowsocksRDecorate obfs sendback error.", ssrd.RequestID))
		}
		atomic.AddInt64(&ssrd.download, int64(n))
		return ssrd.Read(buf)
	}

	if needDecrypt {
		cleartext, err := ssrd.encryptor.Decrypt(unobfsData)
		if ssrd.protocol.GetServerInfo().GetRecvIv() == nil || len(ssrd.protocol.GetServerInfo().GetRecvIv()) == 0 {
			ssrd.protocol.GetServerInfo().SetRecvIv(ssrd.encryptor.IVIn)
		}
		if logrus.GetLevel() == logrus.DebugLevel {
			logrus.WithFields(logrus.Fields{
				"cleartextHexEncode": hex.EncodeToString(cleartext),
				"requestId":          ssrd.RequestID,
			}).Debug("ShadowsocksRDecorate encryptor Decrypt")
		}

		if err != nil && strings.Contains(err.Error(),"buf is too short"){
			return ssrd.Read(buf)
		}

		if err != nil {
			return 0, errors.Wrap(err, fmt.Sprintf("[%s] ShadowsocksRDecorate obfs decrypt error.", ssrd.RequestID))
		}
		data = cleartext
	} else {
		data = unobfsData
	}

	data, sendback, err := ssrd.protocol.ServerPostDecrypt(data)
	if logrus.GetLevel() == logrus.DebugLevel {
		logrus.WithFields(logrus.Fields{
			"serverPostDecryptHex": hex.EncodeToString(data),
			"sendback":             sendback,
			"requestId":            ssrd.RequestID,
		}).Debug("ShadowsocksRDecorate protocol ServerPostDecrypt")
	}
	if err != nil {
		return 0, errors.Wrap(err, fmt.Sprintf("[%s] ShadowsocksRDecorate protocol post decrypt error.", ssrd.RequestID))
	}
	if sendback {
		backdata, err := ssrd.protocol.ServerPreEncrypt([]byte{})
		if logrus.GetLevel() == logrus.DebugLevel {
			logrus.WithFields(logrus.Fields{
				"backdata":  hex.EncodeToString(backdata),
				"requestId": ssrd.RequestID,
				//"LastServerHash":hex.EncodeToString(ssrd.protocol.(*obfs.AuthChainA).LastServerHash),
			}).Debug("shadowoscksr Read ServerPreEncrypt")
		}
		if err != nil {
			return 0, errors.Wrap(err, fmt.Sprintf("[%s] ShadowsocksRDecorate protocol pre encode error.", ssrd.RequestID))
		}
		backdata, err = ssrd.encryptor.Encrypt(backdata)
		if err != nil {
			return 0, errors.Wrap(err, fmt.Sprintf("[%s] ShadowsocksRDecorate encrypter encrypt error.", ssrd.RequestID))
		}
		if logrus.GetLevel() == logrus.DebugLevel {
			logrus.WithFields(logrus.Fields{
				"ReadEncryptData": hex.EncodeToString(backdata),
				"requestId":       ssrd.RequestID,
			}).Debug("shadowoscksr Read Encrypt")
		}
		backdata, err = ssrd.obfs.ServerEncode(backdata)
		if err != nil {
			return 0, errors.Wrap(err, fmt.Sprintf("[%s] ShadowsocksRDecorate obfs service encode error.", ssrd.RequestID))
		}
		if logrus.GetLevel() == logrus.DebugLevel {
			logrus.WithFields(logrus.Fields{
				"ReadServerEncodeData": hex.EncodeToString(backdata),
				"requestId":            ssrd.RequestID,
			}).Debug("shadowoscksr Read ServerEncode")
		}
		n, err = ssrd.Conn.Write(backdata)
		if err != nil {
			return 0, errors.Wrap(err, fmt.Sprintf("[%s] ShadowsocksRDecorate obfs sendback error.", ssrd.RequestID))
		}
		atomic.AddInt64(&ssrd.download, int64(n))
	}
	if ssrd.TrafficReport != nil && ssrd.UID != 0 && ssrd.upload != 0 {
		//TODO add lock
		ssrd.TrafficReport.Upload(ssrd.UID, ssrd.upload)
		ssrd.upload = 0
	}
	if ssrd.recvBuf.Len() == 0 && len(data) == 0 {
		return 0, nil
	}
	ssrd.recvBuf.Write(data)
	n, err = ssrd.recvBuf.Read(buf)
	//log.Debug("n:%d,err:%v",n,err)
	return n, err
}

func (ssrd *ShadowsocksRDecorate) Write(buf []byte) (n int, err error) {
	defer func() {
		if ssrd.ILimiter != nil {
			if err := ssrd.ILimiter.DownLimit(ssrd.UID, n); err != nil {
				logrus.Error(err)
			}
		}
	}()

	data, err := ssrd.protocol.ServerPreEncrypt(buf)
	if err != nil {
		return 0, errors.Wrap(err, fmt.Sprintf("[%s] ShadowsocksRDecorate protocol service encode error.", ssrd.RequestID))
	}
	//logrus.WithFields(logrus.Fields{
	//	"ServerPreEncryptWriteData": hex.EncodeToString(data),
	//	//"LastServerHash":hex.EncodeToString(ssrd.protocol.(*obfs.AuthChainA).LastServerHash),
	//}).Debug("shadowoscksr Write ServerPreEncrypt")
	data, err = ssrd.encryptor.Encrypt(data)
	if err != nil {
		return 0, errors.Wrap(err, fmt.Sprintf("[%s] ShadowsocksRDecorate encryptor encrypt error.", ssrd.RequestID))
	}
	//logrus.WithFields(logrus.Fields{
	//	"EncryptWriteData": hex.EncodeToString(data),
	//}).Debug("shadowoscksr Write Encrypt")
	data, err = ssrd.obfs.ServerEncode(data)
	if err != nil {
		return 0, errors.Wrap(err, fmt.Sprintf("[%s] ShadowsocksRDecorate obfs service encode error.", ssrd.RequestID))
	}
	//logrus.WithFields(logrus.Fields{
	//	"ServerEncodeWriteData": hex.EncodeToString(data),
	//}).Debug("shadowoscksr Write ServerEncode")
	n, err = ssrd.Conn.Write(data)
	if err != nil {
		return 0, err
	}
	atomic.AddInt64(&ssrd.download, int64(n))
	if ssrd.TrafficReport != nil && ssrd.download != 0 && ssrd.UID != 0 {
		//TODO add lock
		ssrd.TrafficReport.Download(ssrd.UID, ssrd.download)
		ssrd.download = 0
	}

	return len(buf), nil
}

func (ssrd *ShadowsocksRDecorate) ReadFrom() (data, uid []byte, addr net.Addr, err error) {
	p := make([]byte, 2048)
	n, addr, err := ssrd.PacketConn.ReadFrom(p)
	if err != nil {
		return nil, nil, nil, err
	}
	data, iv, err := ssrd.encryptor.DecryptAll(p[:n])
	if err != nil {
		return nil, nil, nil, err
	}
	ssrd.protocol.GetServerInfo().SetIv(iv)
	result, uidPack, err := ssrd.protocol.ServerUDPPostDecrypt(data)
	if err != nil {
		return nil, nil, nil, err
	}
	// update upload traffic
	if ssrd.single == 1 && ssrd.TrafficReport != nil {
		ssrd.TrafficReport.Upload(int(binaryx.LEBytesToUInt32([]byte(uidPack))), int64(n))
	}
	if ssrd.single != 1 && ssrd.TrafficReport != nil {
		ssrd.TrafficReport.Upload(ssrd.UID, int64(n))
		uidPack = string(binaryx.LEUint32ToBytes(uint32(ssrd.UID)))
	}
	return result, []byte(uidPack), addr, err

}

func (ssrd *ShadowsocksRDecorate) WriteTo(p, uid []byte, addr net.Addr) error {
	data, err := ssrd.protocol.ServerUDPPreEncrypt(p, uid)
	if err != nil {
		return err
	}
	data, err = ssrd.encryptor.EncryptAll(data, ssrd.encryptor.MustNewIV())
	if err != nil {
		return err
	}
	n, err := ssrd.Request.WriteTo(data, addr)
	if ssrd.TrafficReport != nil {
		ssrd.TrafficReport.Download(int(binaryx.LEBytesToUInt32([]byte(uid))), int64(n))
	}
	return err
}

func (ssrd *ShadowsocksRDecorate) getServerInfo(isObfs bool) obfs.ServerInfo {
	serverInfo := obfs.NewServerInfo()
	serverInfo.SetHost(ssrd.Host)
	serverInfo.SetPort(ssrd.Port)
	if ssrd.Conn != nil {
		serverInfo.SetClient(net.ParseIP(addrx.GetIPFromAddr(ssrd.Conn.RemoteAddr())))
		serverInfo.SetPort(addrx.GetPortFromAddr(ssrd.Conn.RemoteAddr()))
	}
	if isObfs {
		serverInfo.SetObfsParam(ssrd.ObfsParam)
		serverInfo.SetProtocolParam("")
	} else {
		serverInfo.SetObfsParam("")
		serverInfo.SetProtocolParam(ssrd.ProtocolParam)
	}
	serverInfo.SetIv(ssrd.encryptor.IVOut)
	serverInfo.SetRecvIv([]byte{})
	serverInfo.SetKeyStr(ssrd.encryptor.KeyStr)
	serverInfo.SetKey(ssrd.encryptor.Key)
	serverInfo.SetHeadLen(obfs.DEFAULT_HEAD_LEN)
	// TODO: need calculate,for now, I don't know how to implement it on windows
	serverInfo.SetTCPMss(obfs.TCP_MSS)
	serverInfo.SetBufferSize(obfs.BUF_SIZE - ssrd.Overhead)
	serverInfo.SetOverhead(ssrd.Overhead)
	serverInfo.SetUpdateUserFunc(ssrd.UpdateUser)
	serverInfo.SetUsers(ssrd.Users)
	return serverInfo
}

func (ssrd *ShadowsocksRDecorate) UpdateUser(uid []byte) {
	if ssrd.single == 1 {
		uidInt := binaryx.LEBytesToUInt32(uid)
		ssrd.UID = int(uidInt)
		logrus.Infof("ShadowsocksRDecorate update uid: %v", uidInt)
	}
}
