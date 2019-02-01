package tcp2udp

import (
	"bytes"
	"encoding/binary"
	"log"
	"net"
	"strconv"
	"sync"
	"time"
)

// rfc4571 RTP & RTCP over Connection-Oriented Transport

const (
	udp_server      = "127.0.0.1:10014"
	udp_timeout     = 5
	tcp_timeout     = 30
	tcp_check_timer = 10
	rtp_length      = 2
	msg_length      = 4096
)

var sessionMap sync.Map

type TCP2UDP struct {
	session   string
	startFlag bool
	SendStr   chan []byte
	RecvStr   chan []byte
}

// func checkAlive() {
// 	var deleteKey []string
// 	var aliveKey []string
// 	sessionMap.Range(func(key, value interface{}) bool {
// 		session := key.(string)
// 		item := value.(*TCP2UDP)
// 		if time.Now().Unix()-item.aliveTimestamp >= tcp_timeout {
// 			// go item.Stop()
// 			deleteKey = append(deleteKey, session)
// 			log.Println("TCP_2_UDP disconnect timeout tcp connect, session", session, time.Now().Unix()-item.aliveTimestamp)
// 		} else {
// 			aliveKey = append(aliveKey, session)
// 			// log.Println("TCP_2_UDP alive tcp connect, session", session, time.Now().Unix())
// 		}
// 		return true
// 	})
// 	for _, key := range deleteKey {
// 		sessionMap.Delete(key)
// 	}
// 	log.Printf("tcp connect, aliveKey len:%d, %v. delete len:%d, %v.\n", len(aliveKey), aliveKey, len(deleteKey), deleteKey)
// }

func packMsg(msg []byte) (uint16, []byte) {
	var lenBuf = make([]byte, rtp_length)
	binary.BigEndian.PutUint16(lenBuf, uint16(len(msg)))
	var buffer bytes.Buffer
	buffer.Write(lenBuf)
	buffer.Write(msg)
	return uint16(len(msg)), buffer.Bytes()
}
func unpackMsg(msg []byte) (uint16, []byte) {
	if len(msg) <= rtp_length {
		return 0, nil
	}
	n := binary.BigEndian.Uint16(msg[:rtp_length])
	if len(msg) < int(n)+rtp_length {
		return 0, nil
	}
	return n, msg[rtp_length : n+rtp_length]
}

func (tu *TCP2UDP) SendReadUdp(tcpConn *net.TCPConn) {
	for {
		msg, ok := <-tu.RecvStr
		if !ok {
			break
		}
		go func() {
			conn, err := net.Dial("udp", udp_server)
			defer conn.Close()
			if err != nil {
				log.Println("TCP_2_UDP send udp error, ", err)
				return
			}
			n, err := conn.Write(msg)
			if err != nil {
				log.Println("TCP_2_UDP send udp error, ", err)
				return
			}
			// log.Println("TCP_2_UDP send udp success, len", n, msg)
			buf := make([]byte, msg_length)
			conn.SetReadDeadline(time.Now().Add(time.Millisecond * 3))
			n, err = conn.Read(buf)
			if err != nil {
				log.Println("TCP_2_UDP read udp error, ", err)
				return
			}
			// log.Println("TCP_2_UDP read udp success, len", n, buf[:n])
			_, msg := packMsg(buf[:n])
			n, err = tcpConn.Write(msg)
			if err != nil {
				log.Println("TCP_2_UDP send tcp err,", err)
				return
			}
			// log.Println("TCP_2_UDP send tcp success, len", size, msg)
		}()

	}
	log.Println("TCP_2_UDP send read udp end")
}

func handle(conn *net.TCPConn, session string) {
	var tu TCP2UDP
	conn.SetKeepAlive(true)
	defer conn.Close()

	tu.RecvStr = make(chan []byte, 1000)
	defer close(tu.RecvStr)

	tu.startFlag = true
	tu.session = session
	log.Println("TCP_2_UDP udp session start, session:", session)

	go tu.SendReadUdp(conn)

	buf := make([]byte, msg_length)
	var last []byte
	for {
		n, err := conn.Read(buf)
		if err != nil {
			log.Println("TCP_2_UDP tcp read err", err)
			break
		}
		tmpBuf := append(last, buf[:n]...)
		// log.Println("TCP_2_UDP Read from udp_tcp_proxy server: ", udp_server, ", tmpBuf:", tmpBuf)
		for {
			size, msg := unpackMsg(tmpBuf)
			if size == 0 {
				last = tmpBuf
				break
			}
			log.Println("TCP_2_UDP Read from udp_tcp_proxy server: ", udp_server, ", len:", size)
			tu.RecvStr <- msg
			tmpBuf = tmpBuf[rtp_length+size:]
		}

	}
	log.Println("TCP_2_UDP tcp handler end", session)
}

func UDPServer(ip string, port int) {
	addr, err := net.ResolveUDPAddr("udp", ip+":"+strconv.Itoa(port))
	if err != nil {
		log.Fatalln("net.ResolveUDPAddr fail.", err)
		return
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		log.Fatalln("net.ListenUDP fail.", err)
		return
	}
	log.Println("TCP_2_UDP start udp server " + ip + " " + strconv.Itoa(port))
	defer conn.Close()

	// ticker := time.NewTicker(time.Second * tcp_check_timer)
	// go func() {
	// 	for range ticker.C {
	// 		go checkAlive()
	// 	}
	// }()

	for {
		buf := make([]byte, msg_length)
		rlen, remote, err := conn.ReadFromUDP(buf)
		if err == nil {
			log.Println("TCP_2_UDP connected from "+remote.String(), buf[:rlen])
			conn.WriteTo([]byte("rtp packet."), remote)
		} else {
			log.Println("TCP_2_UDP connected from "+remote.String(), ", err:", err)
		}
	}
}

func TcpToUdpMain(ip string, port int) {
	addr, err := net.ResolveTCPAddr("tcp", ip+":"+strconv.Itoa(port))
	if err != nil {
		log.Fatalln("net.ResolveTCPAddr fail.", err)
		return
	}
	conn, err := net.ListenTCP("tcp", addr)
	if err != nil {
		log.Fatalln("net.ListenTCP fail.", err)
		return
	}
	log.Println("TCP_2_UDP start tcp server " + ip + " " + strconv.Itoa(port))
	defer conn.Close()

	for {
		tcpConn, err := conn.AcceptTCP()
		if err == nil {
			go handle(tcpConn, "<"+tcpConn.RemoteAddr().String()+">")
		}
	}
}
