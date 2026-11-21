package relay

import "net"

// PeerDeviceOf 返回该来源设备在本会话中的对端设备。
func (f *Forwarder) PeerDeviceOf(sid string, from *net.UDPAddr) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := f.pairs[sid]
	if p == nil {
		return ""
	}
	fromStr := from.String()
	for dev, pr := range p.Peers {
		if pr.addr.String() == fromStr {
			return p.PeerOf[dev]
		}
	}
	return ""
}

// LocalAddrOf 返回本节点上某设备的地址；不在本节点返回 nil。
func (f *Forwarder) LocalAddrOf(sid, deviceID string) *net.UDPAddr {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := f.pairs[sid]
	if p == nil {
		return nil
	}
	if pr, ok := p.Peers[deviceID]; ok {
		return pr.addr
	}
	return nil
}
