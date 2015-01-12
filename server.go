package main

import (
	"bufio"
	"crypto/tls"
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
)

type ConnHandler struct {
	conn    net.Conn
	scanner *bufio.Scanner
}

func NewConnHandler(conn net.Conn) *ConnHandler {
	return &ConnHandler{conn: conn}
}

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

func (c *ConnHandler) Run() {
	defer c.conn.Close()

	c.scanner = bufio.NewScanner(c.conn)

	for {
		req, err := c.getRequest()
		if err != nil {
			log.Printf("Error while parsing request: %s", err.Error())
			return
		}
		if req == nil {
			return
		}
		fmt.Print(req)
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
