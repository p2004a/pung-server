package main

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"github.com/p2004a/pung-server/users"
	"log"
	"os"
	"runtime"
	"time"
)

type ServerConfiguration struct {
	ServerAddr string
	ServerName string
	CertFile   string
	KeyFile    string
}

var userSet *users.UserSet
var serverConfig ServerConfiguration

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

	var printNumGoroutine bool
	flag.BoolVar(&printNumGoroutine, "numgoroutine", false, "prints periodicaly number of running goroutines")

	flag.Parse()

	if printNumGoroutine {
		go func() {
			t := time.Tick(2 * time.Second)
			for {
				<-t
				log.Print(runtime.NumGoroutine())
			}
		}()
	}

	config, err := loadServerConfiguration(configFile)
	if err != nil {
		log.Fatal(err)
		os.Exit(1)
	}
	serverConfig = config

	serverCert, err := tls.LoadX509KeyPair(config.CertFile, config.KeyFile)
	if err != nil {
		log.Fatal(err)
		os.Exit(1)
	}

	userSet = users.NewUserSet(config.ServerName)

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
		handler := NewClientConnHandl(conn)
		go handler.Run()
	}
}
