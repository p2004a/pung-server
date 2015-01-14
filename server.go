package main

import (
	"bufio"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type ClientRequest struct {
	cSeq, sSeq int
	message    string
	payload    []string
}

func (cr *ClientRequest) String() string {
	payload := strings.Join(cr.payload, " ")
	if cr.sSeq == -1 {
		return fmt.Sprintf("c%d %s\n%s\n", cr.cSeq, cr.message, payload)
	} else {
		return fmt.Sprintf("c%d s%d %s\n%s\n", cr.cSeq, cr.sSeq, cr.message, payload)
	}
}

type ClientResponse ClientRequest

func (cr *ClientResponse) String() string {
	payload := strings.Join(cr.payload, " ")

	if cr.sSeq == -1 { // simple assertion just for case
		panic("cr.sSeq == -1")
	}

	if cr.cSeq == -1 {
		return fmt.Sprintf("s%d %s\n%s\n", cr.sSeq, cr.message, payload)
	} else {
		return fmt.Sprintf("s%d c%d %s\n%s\n", cr.sSeq, cr.cSeq, cr.message, payload)
	}
}

type ConnState int

const (
	Connected ConnState = iota
	Authenticated
)

type RequestChannels struct {
	ch   chan<- *ClientRequest
	send chan bool
}

type ConnHandler struct {
	state     ConnState
	conn      net.Conn
	scanner   *bufio.Scanner
	resChan   chan<- *ClientResponse
	seqNum    <-chan int
	reqMap    map[int]RequestChannels
	reqMapMux sync.Mutex
}

func NewConnHandler(conn net.Conn) *ConnHandler {
	return &ConnHandler{
		conn:   conn,
		state:  Connected,
		reqMap: make(map[int]RequestChannels),
	}
}

func (c *ConnHandler) responseGenerator(res *ClientResponse, send bool) (<-chan *ClientRequest, chan<- bool) {
	if res.sSeq == -1 {
		panic("res.sSeq == -1")
	}

	c.reqMapMux.Lock()
	defer c.reqMapMux.Unlock()

	if _, ok := c.reqMap[res.sSeq]; ok {
		panic("In reqMap is value that shouldn't be")
	}

	ch := make(chan *ClientRequest, 1)
	sendCh := make(chan bool, 1)
	sendCh <- true
	c.reqMap[res.sSeq] = RequestChannels{
		ch:   ch,
		send: sendCh,
	}

	if send {
		c.resChan <- res
	}
	return ch, sendCh
}

func (c *ConnHandler) removeResGen(res *ClientResponse) {
	c.reqMapMux.Lock()
	if rc, ok := c.reqMap[res.sSeq]; ok {
		delete(c.reqMap, res.sSeq)
		rc.send <- false
	}
	c.reqMapMux.Unlock()
}

func (c *ConnHandler) handleRequestUsingResGen(req *ClientRequest) bool {
	c.reqMapMux.Lock()
	cr, ok := c.reqMap[req.sSeq]
	c.reqMapMux.Unlock()
	if ok {
		if <-cr.send {
			cr.ch <- req
			return true
		}
	}
	return false
}

func (c *ConnHandler) getRequest() (*ClientRequest, error) {
	var lines [2]string
	var i int

	for i = 0; i < 2 && c.scanner.Scan(); i++ {
		lines[i] = c.scanner.Text()
	}

	if err := c.scanner.Err(); err != nil {
		return nil, err
	}
	switch i {
	case 0:
		return nil, nil
	case 1:
		return nil, errors.New("Client closed connection without providing payload for last message")
	}

	req := new(ClientRequest)
	headRE, err := regexp.Compile("^c(\\d{1,9}) (?:s(\\d{1,9}) )?([a-z_]{3,20})$")

	if err != nil {
		panic("Cannot compile regexp")
	}
	if !headRE.MatchString(lines[0]) {
		return nil, errors.New("Client request doesn't contain correct header")
	}

	matches := headRE.FindStringSubmatch(lines[0])

	var res uint64
	res, err = strconv.ParseUint(matches[1], 10, 32)
	if err != nil {
		panic(err)
	}
	req.cSeq = int(res)

	if matches[2] == "" {
		req.sSeq = -1
	} else {
		res, err = strconv.ParseUint(matches[2], 10, 32)
		if err != nil {
			panic(err)
		}
		req.sSeq = int(res)
	}

	req.message = matches[3]
	req.payload = strings.Split(lines[1], " ")

	return req, nil
}

func sequenceGenerator(start int) <-chan int {
	seq := make(chan int, 10)

	go func() {
		for i := start; ; i++ {
			seq <- i
		}
	}()

	return seq
}

func (c *ConnHandler) errorForRequest(req *ClientRequest, msg string) {
	res := new(ClientResponse)
	res.cSeq = req.cSeq
	res.message = "error"
	res.payload = []string{base64.StdEncoding.EncodeToString([]byte(msg))}
	res.sSeq = <-c.seqNum
	c.resChan <- res
}

func (c *ConnHandler) login_procedure(req *ClientRequest) {

}

func (c *ConnHandler) signup_procedure(req *ClientRequest) {

}

func (c *ConnHandler) pong(req *ClientRequest) {
	res := new(ClientResponse)
	res.cSeq = req.cSeq
	res.message = "pong"
	res.payload = []string{}
	res.sSeq = <-c.seqNum
	c.resChan <- res
}

func (c *ConnHandler) Run() {
	defer c.conn.Close()

	c.scanner = bufio.NewScanner(c.conn)

	c.seqNum = sequenceGenerator(1)

	resChan := make(chan *ClientResponse, 100)
	defer close(resChan)
	c.resChan = resChan

	go func() {
		for res := range resChan {
			c.conn.Write([]byte(res.String()))
		}
	}()

	stopPing := make(chan bool)
	go func() {
		t := time.Tick(3 * time.Second)
		for {
			select {
			case <-t:
				res := new(ClientResponse)
				res.cSeq = -1
				res.message = "ping"
				res.payload = []string{}
				res.sSeq = <-c.seqNum
				c.resChan <- res
			case <-stopPing:
				return
			}
		}
	}()
	defer func() { stopPing <- true }()

	for {
		req, err := c.getRequest()
		if err != nil {
			log.Printf("Error while parsing request: %s", err.Error())
			return
		}
		if req == nil {
			return
		}

		if c.handleRequestUsingResGen(req) {
			continue
		}

		if req.message == "ping" {
			go c.pong(req)
		} else {
			switch c.state {
			case Connected:
				switch req.message {
				case "login":
					go c.login_procedure(req)
				case "singup":
					go c.signup_procedure(req)
				default:
					go c.errorForRequest(req, "Unknowne message in Connected state")
				}
			case Authenticated:
				go c.errorForRequest(req, "Impossibru!")
			}
		}
	}
}

type ServerConfiguration struct {
	ServerAddr string
	ServerName string
	CertFile   string
	KeyFile    string
}

func loadServerConfiguration(configFile string) (conf ServerConfiguration, err error) {
	file, err := os.Open(configFile)
	if err != nil {
		return
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	err = decoder.Decode(&conf)
	if err != nil {
		return
	}
	if conf.ServerAddr == "" || conf.KeyFile == "" || conf.CertFile == "" || conf.ServerName == "" {
		err = errors.New("Not all required fields set in config file")
	}
	return
}

func main() {
	var configFile string
	flag.StringVar(&configFile, "config", "config.json", "server configuration file")
	flag.Parse()

	config, err := loadServerConfiguration(configFile)
	if err != nil {
		log.Fatal(err)
		os.Exit(1)
	}

	serverCert, err := tls.LoadX509KeyPair(config.CertFile, config.KeyFile)
	if err != nil {
		log.Fatal(err)
		os.Exit(1)
	}

	tlsConfig := tls.Config{
		Certificates:       []tls.Certificate{serverCert},
		ServerName:         config.ServerName,
		InsecureSkipVerify: false,
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

	listener, err := tls.Listen("tcp", config.ServerAddr, &tlsConfig)
	if err != nil {
		log.Fatal(err)
		os.Exit(1)
	}
	log.Printf("server listening on address: %s\n", listener.Addr().String())
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Fatal(err)
			os.Exit(2)
		}
		log.Printf("Connection from: %s", conn.RemoteAddr().String())
		handler := NewConnHandler(conn)
		go handler.Run()
	}
}
