package udp2tcp

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
	// tcp_server      = "127.0.0.1:8090"
	tcp_server      = "192.168.26.130:10014"
	udp_timeout     = 5
	tcp_timeout     = 10
	tcp_check_timer = 5
	rtp_length      = 2
	msg_length      = 4096
)

var sessionMap sync.Map

type TcpClienter struct {
	conn    net.Conn
	isAlive bool
	SendStr chan []byte
}

type UDP2TCP struct {
	session        string
	remote         *net.UDPAddr
	client         *TcpClienter
	startFlag      bool
	aliveTimestamp int64
}

func (ut *UDP2TCP) Connect() bool {
	c := ut.client
	if c.isAlive {
		return true
	}
	var err error
	c.conn, err = net.Dial("tcp", tcp_server)
	if err != nil {
		log.Println("UDP_2_TCP Connect tcp err ", err, tcp_server, ut.session)
		return false
	}
	c.isAlive = true
	log.Println("UDP_2_TCP connect to "+tcp_server, ut.session)
	return true
}

func checkAlive() {
	var deleteKey []string
	var aliveKey []string
	sessionMap.Range(func(key, value interface{}) bool {
		session := key.(string)
		item := value.(*UDP2TCP)
		if time.Now().Unix()-item.aliveTimestamp >= tcp_timeout {
			go item.Stop()
			deleteKey = append(deleteKey, session)
			log.Println("UDP_2_TCP disconnect timeout tcp connect, session", session)
		} else {
			aliveKey = append(aliveKey, session)
			// log.Println("UDP_2_TCP alive tcp connect, session", session, time.Now().Unix())
		}
		return true
	})
	for _, key := range deleteKey {
		sessionMap.Delete(key)
	}
	log.Printf("tcp connect, aliveKey len:%d, %v. delete len:%d, %v.\n", len(aliveKey), aliveKey, len(deleteKey), deleteKey)
}

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

func (ut *UDP2TCP) Stop() {
	close(ut.client.SendStr)
}

func (ut *UDP2TCP) ProxySend() {
	c := ut.client
	for {
		if !c.isAlive {
			if !ut.Connect() {
				continue
			}
			time.Sleep(1 * time.Second)
		}
		if c.isAlive {
			reqContent, ok := <-c.SendStr
			if !ok {
				c.conn.Close()
				ut.startFlag = false
				log.Println("UDP_2_TCP SendStr close ", ut.session)
				break
			}

			ut.aliveTimestamp = time.Now().Unix()
			_, msg := packMsg(reqContent)
			_, err := c.conn.Write([]byte(msg))
			if err != nil {
				c.conn.Close()
				c.isAlive = false
				log.Println("UDP_2_TCP disconnect from " + tcp_server)
				continue
			}
			// log.Println("UDP_2_TCP Write to udp_tcp_proxy server: ", tcp_server, ", len:", size)
		}
	}
}

func (ut *UDP2TCP) ProxyRecv(conn *net.UDPConn) {
	c := ut.client
	buf := make([]byte, msg_length)
	var last []byte
	for {
		if !ut.startFlag {
			break
		}
		if !c.isAlive {
			ut.Connect()
			time.Sleep(1 * time.Second)
		}
		if c.isAlive {
			n, err := c.conn.Read(buf)
			if err != nil {
				c.conn.Close()
				c.isAlive = false
				continue
			}

			ut.aliveTimestamp = time.Now().Unix()
			tmpBuf := append(last, buf[:n]...)
			for {
				size, msg := unpackMsg(tmpBuf)
				if size == 0 {
					last = tmpBuf
					break
				}
				// log.Println("UDP_2_TCP Read from udp_tcp_proxy server: ", tcp_server, ", len:", size)
				_, err := conn.WriteToUDP(msg, ut.remote)
				if err != nil {
					log.Println("UDP_2_TCP udp send resp, err ", err, ", session:", ut.session)
				}
				tmpBuf = tmpBuf[rtp_length+size:]
			}
		}
	}
}

func handle(conn *net.UDPConn, remote *net.UDPAddr, session string, data []byte) {
	var ut UDP2TCP

	work, loaded := sessionMap.LoadOrStore(session, &ut)
	if loaded {
		ut = *work.(*UDP2TCP)
		if !ut.startFlag {
			return
		}
	} else {
		ut.session = session
		ut.remote = remote
		var tc TcpClienter
		ut.client = &tc
		if !ut.Connect() {
			sessionMap.Delete(session)
			log.Println("UDP_2_TCP connect tcp failed , session", session, tcp_server)
			return
		}
		tc.SendStr = make(chan []byte, 1000)
		ut.startFlag = true
		go ut.ProxySend()
		go ut.ProxyRecv(conn)
		log.Println("UDP_2_TCP udp session start, session:", session)
	}

	select {
	case ut.client.SendStr <- data:
	case <-time.After(udp_timeout * time.Millisecond):
		log.Println("UDP_2_TCP udp read to SendStr channel timeout.  session:", session)
	}
}

func UdpToTcpMain(ip string, port int) {
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
	log.Println("UDP_2_TCP start udp server " + ip + " " + strconv.Itoa(port))
	defer conn.Close()

	ticker := time.NewTicker(time.Second * tcp_check_timer)
	go func() {
		for range ticker.C {
			go checkAlive()
		}
	}()

	for {
		buf := make([]byte, msg_length)
		rlen, remote, err := conn.ReadFromUDP(buf)
		if err == nil {
			go handle(conn, remote, "<"+remote.String()+">", buf[:rlen])
		} else {
			log.Println("UDP_2_TCP connected from "+remote.String(), ", err:", err)
		}
	}
}

func TCPServer(ip string, port int) {
	addr, err := net.ResolveTCPAddr("tcp", ip+":"+strconv.Itoa(port))
	if err != nil {
		log.Println("net.ResolveTCPAddr fail.", err)
		return
	}
	conn, err := net.ListenTCP("tcp", addr)
	if err != nil {
		log.Println("net.ListenTCP fail.", err)
		return
	}
	log.Println("UDP_2_TCP start tcp server " + ip + " " + strconv.Itoa(port))
	defer conn.Close()

	buf := make([]byte, msg_length)
	for {
		tcpConn, err := conn.Accept()
		if err == nil {
			go func() {
				defer tcpConn.Close()
				for {
					lens, err := tcpConn.Read(buf)
					if err != nil {
						log.Println("UDP_2_TCP tcp server Read error, ", lens, err)
						break
					}
					size, msg := unpackMsg(buf[:lens])
					log.Println("UDP_2_TCP tcp server Read succ, len:", size, msg)

					size, msg = packMsg([]byte("ok!"))
					lens, err = tcpConn.Write(msg)
					if err != nil {
						log.Println("UDP_2_TCP tcp server Write error, ", size, err)
						break
					}
					log.Println("UDP_2_TCP tcp server write succ, len:", size, msg)
				}
				log.Println("UDP_2_TCP tcp server disconnect ")
			}()
		}
	}
}
