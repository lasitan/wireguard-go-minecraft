package conn

import (
	"net"
	"net/netip"
	"sync/atomic"

	"golang.org/x/net/ipv6"
)

type TransportConfigFile = transportConfigFile

func LoadCamouflageConfig(fileCfg TransportConfigFile) (CamoSharedConfig, MCCamouflageConfig, error) {
	shared, mc, err := loadCamouflageConfig(fileCfg)
	return shared, mc, err
}

type CamoSharedConfig = camoSharedConfig
type MCCamouflageConfig = mcCamouflageConfig

func (b *TCPBind) SetCamoShared(c CamoSharedConfig) {
	b.camoShared = c
}

func (b *TCPBind) SetMCConfig(c MCCamouflageConfig) {
	b.mcConfig = c
}

// KillSessions closes every live TCP connection, as a network blip would.
func (b *TCPBind) KillSessions() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, s := range b.sessions {
		if s != nil && s.conn != nil {
			_ = s.conn.Close()
		}
	}
}

func (b *TCPBind) DialToNATFlag() *atomic.Bool {
	return &b.dialToNAT
}

func (b *TCPBind) PerformCamouflageServer(conn net.Conn) (net.Conn, bool, error) {
	return b.performCamouflageServer(conn)
}

func (b *TCPBind) PerformCamouflageClient(conn net.Conn, dst netip.AddrPort) (net.Conn, error) {
	return b.performCamouflageClient(conn, dst)
}

func WriteAll(conn net.Conn, data []byte) error {
	return writeAll(conn, data)
}

func CoalesceMessages(addr *net.UDPAddr, ep *StdNetEndpoint, bufs [][]byte, msgs []ipv6.Message, setGSO setGSOFunc) int {
	return coalesceMessages(addr, ep, bufs, msgs, setGSO)
}

func SplitCoalescedMessages(msgs []ipv6.Message, firstMsgAt int, getGSO getGSOFunc) (n int, err error) {
	return splitCoalescedMessages(msgs, firstMsgAt, getGSO)
}

func (e *StdNetEndpoint) SetSrcOOB(b []byte) {
	e.src = b
}

func SetSrcControl(control *[]byte, ep *StdNetEndpoint) {
	setSrcControl(control, ep)
}

func StickyControlSize() int {
	return stickyControlSize
}

func GetSrcFromControl(control []byte, ep *StdNetEndpoint) {
	getSrcFromControl(control, ep)
}

func ListenConfig() *net.ListenConfig {
	return listenConfig()
}

const DefaultLoginPluginSecret = defaultLoginPluginSecret

var ErrSecureAuth = errSecureAuth

func (b *TCPBind) GetMCConfig() MCCamouflageConfig {
	return b.getMCConfig()
}

func (c MCCamouflageConfig) PluginSecret() string {
	return c.pluginSecret
}

func (c CamoSharedConfig) SecureEnabled() bool {
	return c.secure
}

func SecurePSK(secret string) []byte {
	return securePSK(secret)
}

func SecureServerHello(psk []byte) ([]byte, any, error) {
	hello, key, err := secureServerHello(psk)
	return hello, key, err
}

func EncodeMCEncryptionRequest(hello, verifyToken []byte) ([]byte, error) {
	return encodeMCEncryptionRequest(hello, verifyToken)
}

func DecodeMCEncryptionRequest(rest []byte) ([]byte, error) {
	return decodeMCEncryptionRequest(rest)
}

func MCPacketHead(payload []byte) (packetID int, rest []byte, err error) {
	return mcPacketHead(payload)
}

func OpenSecureBlob(psk []byte, label string, prior, blob []byte) (pub []byte, flags byte, err error) {
	return openSecureBlob(psk, label, prior, blob)
}
