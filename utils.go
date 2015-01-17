package main

import (
	"crypto/rsa"
	"crypto/x509"
	"errors"
)

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
