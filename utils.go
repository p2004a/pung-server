package main

import (
	"crypto/rsa"
	"crypto/x509"
	"errors"
	"regexp"
)

var pungRE *regexp.Regexp

func init() {
	var err error
	pungRE, err = regexp.Compile("^([a-z][a-z_0-9]{2,19})@((?:(?:[a-zA-Z0-9]|[a-zA-Z0-9][a-zA-Z0-9\\-]*[a-zA-Z0-9])\\.)*(?:[A-Za-z0-9]|[A-Za-z0-9][A-Za-z0-9\\-]*[A-Za-z0-9]))$")
	if err != nil {
		panic("incorrect pungRE reqular expresion")
	}
}

func parsePungID(pungid string) (string, string, error) {
	if !pungRE.MatchString(pungid) {
		return "", "", errors.New("given string wasn't valid pung id")
	}

	matches := pungRE.FindStringSubmatch(pungid)
	return matches[1], matches[2], nil
}

func sequenceGenerator(start int, end <-chan bool) <-chan int {
	seq := make(chan int, 10)

	go func() {
		for i := start; ; i++ {
			select {
			case seq <- i:
			case <-end:
				return
			}
		}
	}()

	return seq
}

func rsaPublicKeyFromDER(der []byte) (*rsa.PublicKey, error) {
	genericKey, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, errors.New("Cannot parse public key")
	}
	key, ok := genericKey.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("Key wasn't the rsa public key")
	}
	return key, nil
}

func rsaPublicKeyToDER(pub *rsa.PublicKey) ([]byte, error) {
	buf, err := x509.MarshalPKIXPublicKey(pub)
	return buf, err
}
