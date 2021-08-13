package addrx

import (
	"github.com/ProxyPanel/VNet-SSR/utils/langx"
	"io/ioutil"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func GetIPFromAddr(addr net.Addr) string {
	switch addr.(type) {
	case *net.TCPAddr:
		tcpAddr := addr.(*net.TCPAddr)
		return tcpAddr.IP.String()
	case *net.UDPAddr:
		udpAddr := addr.(*net.UDPAddr)
		return udpAddr.IP.String()
	case nil:
		return ""
	default:
		return ""
	}
}

func GetPortFromAddr(addr net.Addr) int {
	switch addr.(type) {
	case *net.TCPAddr:
		tcpAddr := addr.(*net.TCPAddr)
		return tcpAddr.Port
	case *net.UDPAddr:
		udpAddr := addr.(*net.UDPAddr)
		return udpAddr.Port
	case nil:
		return 0
	default:
		return 0
	}
}

func GetNetworkFromAddr(addr net.Addr) string {
	return addr.Network()
}

func ParseAddrFromString(network, addr string) (net.Addr, error) {
	var addrConvert net.Addr
	var err error
	switch network {
	case "tcp", "tcp4", "tcp6":
		addrConvert, err = net.ResolveTCPAddr(network, addr)
	case "udp", "udp4", "udp6":
		addrConvert, err = net.ResolveUDPAddr(network, addr)
	}
	if err != nil {
		return nil, err
	}
	return addrConvert, nil
}

func SplitIpFromAddr(addr string) string {
	ip, _, err := net.SplitHostPort(addr)
	if err != nil {
		return ""
	}
	return ip
}

func SplitPortFromAddr(addr string) int {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return 0
	}
	return langx.FirstResult(strconv.Atoi, port).(int)
}

func GetPublicIp() (string, error) {
	client := http.Client{
		Timeout: time.Duration(3 * time.Second),
	}
	res, err := client.Get("https://api.ip.sb/ip")
	if err != nil {
		return "", err
	}

	ip, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return "", err
	}

	return strings.Trim(string(ip), "\n"), nil
}

// func GetAddressType(addrx string) string {
// 	var (
// 		ipv4   = `^(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?):?([0-9]{1,4}|[1-5][0-9]{4}|6[0-4][0-9]{3}|65[0-4][0-9]{2}|655[0-2][0-9]|6553[0-5]?)$`
// 		ipv6   = `^(([0-9A-Fa-f]{1,4}:){7}([0-9A-Fa-f]{1,4}|:))|(([0-9A-Fa-f]{1,4}:){6}(:[0-9A-Fa-f]{1,4}|((25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3})|:))|(([0-9A-Fa-f]{1,4}:){5}(((:[0-9A-Fa-f]{1,4}){1,2})|:((25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3})|:))|(([0-9A-Fa-f]{1,4}:){4}(((:[0-9A-Fa-f]{1,4}){1,3})|((:[0-9A-Fa-f]{1,4})?:((25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}))|:))|(([0-9A-Fa-f]{1,4}:){3}(((:[0-9A-Fa-f]{1,4}){1,4})|((:[0-9A-Fa-f]{1,4}){0,2}:((25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}))|:))|(([0-9A-Fa-f]{1,4}:){2}(((:[0-9A-Fa-f]{1,4}){1,5})|((:[0-9A-Fa-f]{1,4}){0,3}:((25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}))|:))|(([0-9A-Fa-f]{1,4}:){1}(((:[0-9A-Fa-f]{1,4}){1,6})|((:[0-9A-Fa-f]{1,4}){0,4}:((25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}))|:))|(:(((:[0-9A-Fa-f]{1,4}){1,7})|((:[0-9A-Fa-f]{1,4}){0,5}:((25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}))|:)):?([0-9]{1,4}|[1-5][0-9]{4}|6[0-4][0-9]{3}|65[0-4][0-9]{2}|655[0-2][0-9]|6553[0-5]?)$`
// 		domain = `^(?=.{1,255}$)[0-9A-Za-z](?:(?:[0-9A-Za-z]|\b-){0,61}[0-9A-Za-z])?(?:\.[0-9A-Za-z](?:(?:[0-9A-Za-z]|\b-){0,61}[0-9A-Za-z])?)*\.?:?([0-9]{1,4}|[1-5][0-9]{4}|6[0-4][0-9]{3}|65[0-4][0-9]{2}|655[0-2][0-9]|6553[0-5]?)$`
// 	)
// }
