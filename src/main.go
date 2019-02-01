package main

import (
	u2t "github.com/manjiewang/udp2tcp/udp2tcp"
)

const (
	ip   = "0.0.0.0"
	port = 10020
)

func main() {
	// go u2t.TCPServer(ip, port)
	// go t2u.UDPServer(ip, 1399)

	// go u2t.UdpToTcpMain(ip, port)
	// t2u.TcpToUdpMain(ip, 8090)
	u2t.UdpToTcpMain(ip, port)

}
