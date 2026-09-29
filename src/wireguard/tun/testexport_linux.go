package tun

const (
	VirtioNetHdrLen = virtioNetHdrLen
	UDPHLen         = udphLen
)

// VirtioNetHdr is a test-facing mirror of virtioNetHdr with exported fields.
type VirtioNetHdr struct {
	Flags      uint8
	GSOType    uint8
	HdrLen     uint16
	GSOSize    uint16
	CSumStart  uint16
	CSumOffset uint16
}

func (v VirtioNetHdr) Encode(b []byte) error {
	h := virtioNetHdr{
		flags:      v.Flags,
		gsoType:    v.GSOType,
		hdrLen:     v.HdrLen,
		gsoSize:    v.GSOSize,
		csumStart:  v.CSumStart,
		csumOffset: v.CSumOffset,
	}
	return h.encode(b)
}

func HandleVirtioRead(in []byte, bufs [][]byte, sizes []int, offset int) (int, error) {
	return handleVirtioRead(in, bufs, sizes, offset)
}

func HandleGRO(bufs [][]byte, offset int, tcpTable *TCPGROTable, udpTable *UDPGROTable, canUDPGRO bool, toWrite *[]int) error {
	return handleGRO(bufs, offset, tcpTable, udpTable, canUDPGRO, toWrite)
}

type TCPGROTable = tcpGROTable
type UDPGROTable = udpGROTable

func NewTCPGROTable() *TCPGROTable {
	return newTCPGROTable()
}

func NewUDPGROTable() *UDPGROTable {
	return newUDPGROTable()
}

// UDPGROItemView is a test-facing view of udpGROItem fields used by coalescing tests.
type UDPGROItemView struct {
	GSOSize uint16
	IPHLen  uint8
}

func UDPPacketsCanCoalesceView(pkt []byte, iphLen uint8, gsoSize uint16, item UDPGROItemView, bufs [][]byte, bufsOffset int) CanCoalesce {
	return udpPacketsCanCoalesce(pkt, iphLen, gsoSize, udpGROItem{
		gsoSize: item.GSOSize,
		iphLen:  item.IPHLen,
	}, bufs, bufsOffset)
}

type CanCoalesce = canCoalesce

const (
	CoalescePrepend     = coalescePrepend
	CoalesceUnavailable = coalesceUnavailable
	CoalesceAppend      = coalesceAppend
)

type GROCandidateType = groCandidateType

const (
	NotGROCandidate  = notGROCandidate
	TCP4GROCandidate = tcp4GROCandidate
	TCP6GROCandidate = tcp6GROCandidate
	UDP4GROCandidate = udp4GROCandidate
	UDP6GROCandidate = udp6GROCandidate
)

func PacketIsGROCandidate(b []byte, canUDPGRO bool) GROCandidateType {
	return packetIsGROCandidate(b, canUDPGRO)
}

func (t *tcpGROTable) FlowCount() int {
	return len(t.itemsByFlow)
}

// EachTCPGROItem invokes fn for every item across all flows.
func (t *tcpGROTable) EachTCPGROItem(fn func(sentSeq uint32, numMerged uint16)) {
	for _, items := range t.itemsByFlow {
		for _, item := range items {
			fn(item.sentSeq, item.numMerged)
		}
	}
}

func (t *tcpGROTable) FlowItemCounts() []int {
	out := make([]int, 0, len(t.itemsByFlow))
	for _, items := range t.itemsByFlow {
		out = append(out, len(items))
	}
	return out
}
