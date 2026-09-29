package device

import (
	"crypto/cipher"

	"golang.zx2c4.com/wireguard/src/wireguard/conn"
	"golang.zx2c4.com/wireguard/src/wireguard/tun"
)

type TrieEntry = trieEntry
type ParentIndirection = parentIndirection

func CommonBits(ip1, ip2 []byte) uint8 {
	return commonBits(ip1, ip2)
}

func NewPrivateKey() (sk NoisePrivateKey, err error) {
	return newPrivateKey()
}

func (sk *NoisePrivateKey) PublicKey() (pk NoisePublicKey) {
	return sk.publicKey()
}

func (sk *NoisePrivateKey) SharedSecret(pk NoisePublicKey) ([NoisePublicKeySize]byte, error) {
	return sk.sharedSecret(pk)
}

func (d *Device) NetListenPort() uint16 {
	return d.net.port
}

func (d *Device) SetNetBind(b conn.Bind) {
	d.net.bind = b
}

func (d *Device) SetTunDevice(dev tun.Device) {
	d.tun.device = dev
}

func (d *Device) PeerPublicKeys() []NoisePublicKey {
	d.peers.RLock()
	defer d.peers.RUnlock()
	out := make([]NoisePublicKey, 0, len(d.peers.keyMap))
	for k := range d.peers.keyMap {
		out = append(out, k)
	}
	return out
}

func (d *Device) StaticPrivateKey() NoisePrivateKey {
	d.staticIdentity.RLock()
	defer d.staticIdentity.RUnlock()
	return d.staticIdentity.privateKey
}

func (d *Device) StaticPublicKey() NoisePublicKey {
	d.staticIdentity.RLock()
	defer d.staticIdentity.RUnlock()
	return d.staticIdentity.publicKey
}

func (p *Peer) HandshakeState() *Handshake {
	return &p.handshake
}

func (p *Peer) KeypairsState() *Keypairs {
	return &p.keypairs
}

func (h *Handshake) ChainKeyBytes() []byte {
	return h.chainKey[:]
}

func (h *Handshake) HashBytes() []byte {
	return h.hash[:]
}

func (h *Handshake) PrecomputedStaticStaticBytes() []byte {
	return h.precomputedStaticStatic[:]
}

func (kp *Keypairs) NextKeypair() *Keypair {
	return kp.next.Load()
}

func (k *Keypair) SendAEAD() cipher.AEAD {
	return k.send
}

func (k *Keypair) ReceiveAEAD() cipher.AEAD {
	return k.receive
}

func (p *WaitPool) TestLock() {
	p.lock.Lock()
}

func (p *WaitPool) TestUnlock() {
	p.lock.Unlock()
}

func (p *WaitPool) TestCount() uint32 {
	return p.count
}

func (p *WaitPool) TestMax() uint32 {
	return p.max
}

// TestSetCount adjusts the in-flight Get count for pool tests.
func (p *WaitPool) TestSetCount(n uint32) {
	p.count = n
}

func NewParentIndirection(parent **TrieEntry, bitType uint8) ParentIndirection {
	return parentIndirection{
		parentBit:     (**trieEntry)(parent),
		parentBitType: bitType,
	}
}

func (t ParentIndirection) Insert(ip []byte, cidr uint8, peer *Peer) {
	t.insert(ip, cidr, peer)
}

func (node *TrieEntry) Lookup(ip []byte) *Peer {
	return (*trieEntry)(node).lookup(ip)
}
