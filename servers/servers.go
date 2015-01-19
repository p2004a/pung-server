package servers

import (
	"bufio"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"
)

type ServerRequestHandler func(request []string) error

type serverConn struct {
	req chan<- []string
	res <-chan error
}

type ServerManager struct {
	serverName     string
	connPort       int
	serverMap      map[string]*serverConn
	serverMapLock  sync.RWMutex
	requestHandler ServerRequestHandler
}

func (s *ServerManager) handleConnection(conn net.Conn) {
	reader := bufio.NewReaderSize(conn, 101*1024)

	log.Printf("S2S connection from: %s", conn.RemoteAddr().String())

	for {
		buf, err := reader.ReadSlice(byte('\n'))
		if err != nil {
			break
		}
		request := strings.Split(string(buf[0:len(buf)-1]), " ")

		err = s.requestHandler(request)
		var response string
		if err == nil {
			response = "ok\n"
		} else {
			response = "error " + err.Error() + "\n"
		}

		if _, err = conn.Write([]byte(response)); err != nil {
			break
		}
	}
}

func (s *ServerManager) run(listener net.Listener) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Fatal(err)
		}
		go s.handleConnection(conn)
	}
}

func (s *ServerManager) connect(host string, req <-chan []string, res chan<- error) {
	log.Printf("connecting to %s", host)

	dialer := &net.Dialer{
		Timeout:   time.Second * 1,
		KeepAlive: time.Second * 10,
	}
	addr := fmt.Sprintf("%s:%d", host, s.connPort)

	conn, err := tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{
		InsecureSkipVerify: true, // insecure, not suitable for production
	})
	if err != nil {
		log.Printf("cannot connect to %s: %s", host, err.Error())
		return
	}

	reader := bufio.NewReaderSize(conn, 101*1024)

	sendRes := func(e error) {
		select {
		case res <- e:
		case <-time.After(time.Millisecond * 10):
		}
	}

Outer:
	for {
		var request []string
		select {
		case request = <-req:
		case <-time.After(time.Minute * 40):
			break Outer
		}

		data := strings.Join(request, " ") + "\n"

		if _, err := conn.Write([]byte(data)); err != nil {
			sendRes(errors.New("error while sending request"))
			break
		}

		buf, err := reader.ReadSlice(byte('\n'))
		if err != nil {
			sendRes(errors.New("error while receiving response"))
			break
		}

		line := string(buf[0 : len(buf)-1])
		if line == "ok" {
			sendRes(nil)
		} else if len(line) > 6 && line[:6] == "error " {
			sendRes(errors.New(line[6:]))
		} else {
			sendRes(errors.New("Cannot parse error from other server"))
			break
		}
	}

	conn.Close()

	s.serverMapLock.Lock()
	delete(s.serverMap, host)
	s.serverMapLock.Unlock()
}

func (s *ServerManager) SendMessage(host string, data ...string) error {
	s.serverMapLock.RLock()
	serv, ok := s.serverMap[host]
	s.serverMapLock.RUnlock()
	if !ok {
		s.serverMapLock.Lock()
		serv, ok = s.serverMap[host]
		if !ok {
			req := make(chan []string)
			res := make(chan error)
			serv = &serverConn{
				req: req,
				res: res,
			}
			s.serverMap[host] = serv
			go s.connect(host, req, res)
		}
		s.serverMapLock.Unlock()
	}
	select {
	case serv.req <- data:
		return <-serv.res
	case <-time.After(time.Second):
		return errors.New(fmt.Sprintf("sending request to %s timeouted", host))
	}
}

func NewServerManager(serverCert tls.Certificate, serverAddr, serverName string, connPort int, handl ServerRequestHandler) (*ServerManager, error) {
	tlsConfig := tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ServerName:   serverName,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA,
			tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
		},
		PreferServerCipherSuites: false,
		SessionTicketsDisabled:   true,
		MinVersion:               tls.VersionTLS12,
		MaxVersion:               0,
	}
	tlsConfig.BuildNameToCertificate()

	listener, err := tls.Listen("tcp", serverAddr, &tlsConfig)
	if err != nil {
		return nil, err
	}
	log.Printf("server manager listening on address: %s\n", listener.Addr().String())

	sm := &ServerManager{
		serverName:     serverName,
		connPort:       connPort,
		requestHandler: handl,
		serverMap:      make(map[string]*serverConn),
	}
	go sm.run(listener)
	return sm, nil
}
