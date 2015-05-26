package main

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/p2004a/pung-server/servers"
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

var serverManager *servers.ServerManager
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
	}
	serverConfig = config

	serverCert, err := tls.LoadX509KeyPair(config.CertFile, config.KeyFile)
	if err != nil {
		log.Fatal(err)
	}

	userSet = users.NewUserSet(config.ServerName)
	serverManager, err = servers.NewServerManager(serverCert, fmt.Sprintf("%s:%d", config.ServerAddr, 24949), config.ServerName, 24949, serverRequestHandler)
	if err != nil {
		log.Fatal(err)
	}

	tlsConfig := tls.Config{
		Certificates:       []tls.Certificate{serverCert},
		ServerName:         config.ServerName,
		InsecureSkipVerify: false,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
		},
		PreferServerCipherSuites: false,
		SessionTicketsDisabled:   true,
		MinVersion:               tls.VersionTLS12,
		MaxVersion:               0,
	}
	tlsConfig.BuildNameToCertificate()

	listener, err := tls.Listen("tcp", fmt.Sprintf("%s:%d", config.ServerAddr, 24948), &tlsConfig)
	if err != nil {
		log.Fatal(err)
		os.Exit(1)
	}
	log.Printf("server listening on address: %s\n", listener.Addr().String())
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("Connection from: %s", conn.RemoteAddr().String())
		handler := NewClientConnHandl(conn)
		go handler.Run()
	}
}
