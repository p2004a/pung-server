Pung server
===========

Server of the Pung - secure, encrypted chat.

Build
-----

This project was built and tested under go version 1.3.

You need to install go and set up `$GOPATH` variable as described in
[documentation](https://golang.org/doc/code.html#GOPATH).

    go get -u github.com/p2004a/pung-server
    cd $GOPATH/src/github.com/p2004a/pung-server
    go build

Running
-------

When you are in the project directory after building the server you need to
create configuration file `config.json` and TLS certificate.

Example JSON configuration file:

    {
        "ServerAddr": "127.0.0.1",
        "ServerName": "localhost",
        "CertFile": "server.crt",
        "KeyFile": "server.key"
    }

There are many places in the internet describing how to create TLS certificate
that will fulfill your needs for example
[this](http://www.akadia.com/services/ssh_test_certificate.html). To create
self signed certificate for localhost:

    openssl genrsa -out server.key 1024
    # set only Common Name to localhost
    openssl req -new -key server.key -out server.csr
    openssl x509 -req -days 3650 -in server.csr -signkey server.key -out server.crt

Then run binary

    ./pung-server
