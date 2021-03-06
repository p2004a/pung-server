package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/p2004a/pung-server/users"
	"io"
	"log"
	"net"
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

type ClientConnState int

const (
	Connected ClientConnState = iota
	Authenticating
	Authenticated
)

type RequestChannels struct {
	ch   chan<- *ClientRequest
	send chan bool
}

type ClientConnHandl struct {
	state       ClientConnState
	conn        net.Conn
	reader      *bufio.Reader
	resChan     chan<- *ClientResponse
	seqNum      <-chan int
	reqMap      map[int]RequestChannels
	reqMapMux   sync.Mutex
	user        *users.User
	loginStruct *users.LoginStruct
}

func NewClientConnHandl(conn net.Conn) *ClientConnHandl {
	return &ClientConnHandl{
		conn:   conn,
		state:  Connected,
		reqMap: make(map[int]RequestChannels),
	}
}

func (c *ClientConnHandl) requestGenerator(res *ClientResponse) (<-chan *ClientRequest, chan<- bool) {
	if res.sSeq == -1 {
		panic("res.sSeq == -1")
	}

	c.reqMapMux.Lock()
	defer c.reqMapMux.Unlock()

	if _, ok := c.reqMap[res.sSeq]; ok {
		panic("In reqMap is value that shouldn't be")
	}

	ch := make(chan *ClientRequest, 1)
	sendCh := make(chan bool, 2)
	c.reqMap[res.sSeq] = RequestChannels{
		ch:   ch,
		send: sendCh,
	}

	return ch, sendCh
}

func (c *ClientConnHandl) removeReqGen(res *ClientResponse) {
	c.reqMapMux.Lock()
	if rc, ok := c.reqMap[res.sSeq]; ok {
		delete(c.reqMap, res.sSeq)
		rc.send <- false
	}
	c.reqMapMux.Unlock()
}

func (c *ClientConnHandl) handleRequestUsingReqGen(req *ClientRequest) bool {
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

func (c *ClientConnHandl) closeAllReqGen() {
	c.reqMapMux.Lock()
	for key := range c.reqMap {
		cr := c.reqMap[key]
		close(cr.ch)
	}
	c.reqMapMux.Unlock()
}

func (c *ClientConnHandl) sendResponse(res *ClientResponse) (err error) {
	defer func() {
		if x := recover(); x != nil {
			err = errors.New("Unable to send to resChan")
		}
	}()
	c.resChan <- res
	return
}

func (c *ClientConnHandl) singleRequest(res *ClientResponse, timeout time.Duration) (*ClientRequest, error) {
	check, send := c.requestGenerator(res)
	defer c.removeReqGen(res)
	send <- true
	if err := c.sendResponse(res); err != nil {
		return nil, err
	}
	if timeout > 0 {
		select {
		case req, ok := <-check:
			if ok {
				return req, nil
			} else {
				return nil, nil
			}
		case <-time.After(timeout):
			return nil, errors.New("timeouted waiting for request")
		}
	} else {
		req, ok := <-check
		if ok {
			return req, nil
		} else {
			return nil, nil
		}
	}
}

func (c *ClientConnHandl) simpleResponse(req *ClientRequest, message string, payload ...string) error {
	res := new(ClientResponse)
	res.cSeq = req.cSeq
	res.message = message
	res.payload = payload
	res.sSeq = <-c.seqNum
	return c.sendResponse(res)
}

func (c *ClientConnHandl) getRequest() (*ClientRequest, error) {
	var lines [2]string
	var i int

	for i = 0; i < 2; i++ {
		buf, err := c.reader.ReadSlice(byte('\n'))
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		lines[i] = string(buf[0 : len(buf)-1])
	}

	switch i {
	case 0:
		return nil, nil
	case 1:
		return nil, errors.New("Client closed connection without providing payload for last message")
	}

	req := new(ClientRequest)
	headRE, err := regexp.Compile("^c(\\d{1,9}) (?:s(\\d{1,9}) )?([a-z_]{2,20})$")

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
	if lines[1] == "" {
		req.payload = []string{}
	} else {
		req.payload = strings.Split(lines[1], " ")
	}

	return req, nil
}

func (c *ClientConnHandl) errorForRequest(req *ClientRequest, msg string) {
	c.simpleResponse(req, "error", base64.StdEncoding.EncodeToString([]byte(msg)))
}

func (c *ClientConnHandl) verifyKey(req *ClientRequest, key *rsa.PublicKey) (*ClientRequest, error) {
	hash := sha256.New()
	secret := make([]byte, 32)
	_, err := rand.Reader.Read(secret)
	if err != nil {
		panic("Cannot get numers form random generator")
	}
	encrypted, err := rsa.EncryptOAEP(hash, rand.Reader, key, secret, []byte("verification"))
	if err != nil {
		return req, errors.New("Failed to encrypt secret")
	}

	res := new(ClientResponse)
	res.cSeq = req.cSeq
	res.message = "decrypt"
	res.payload = []string{base64.StdEncoding.EncodeToString(encrypted)}
	res.sSeq = <-c.seqNum

	req, err = c.singleRequest(res, 1*time.Second)
	if err != nil || req == nil {
		return nil, nil
	}
	if req.message != "check" || len(req.payload) != 1 {
		return req, errors.New("Wrong request for decrypt response")
	}
	secretToCheck, err := base64.StdEncoding.DecodeString(req.payload[0])
	if err != nil {
		return req, errors.New("Decoded secret payload was not valid base64")
	}

	if !bytes.Equal(secretToCheck, secret) {
		return req, errors.New("Wrong answer for check")
	}

	return req, nil
}

func (c *ClientConnHandl) loginProcedure(req *ClientRequest) {
	if len(req.payload) != 1 {
		c.state = Connected
		c.errorForRequest(req, "Incorrect login payload")
		return
	}

	user := userSet.GetUser(req.payload[0])
	if user == nil {
		c.state = Connected
		c.errorForRequest(req, "There is no user with requested login")
		return
	}

	if user.Key == nil {
		c.state = Connected
		c.errorForRequest(req, "Cannot login to account without verified key")
		return
	}

	req, err := c.verifyKey(req, user.Key)
	if err != nil {
		c.state = Connected
		c.errorForRequest(req, err.Error())
		return
	}

	if req == nil {
		return
	}

	c.loginStruct, err = userSet.LogIn(user)
	if err != nil {
		c.state = Connected
		c.errorForRequest(req, err.Error())
		return
	}

	c.simpleResponse(req, "ok")

	c.user = user
	c.state = Authenticated
}

func (c *ClientConnHandl) signupProcedure(req *ClientRequest) {
	if len(req.payload) != 2 {
		c.state = Connected
		c.errorForRequest(req, "Incorrect singnup payload")
		return
	}

	keyBuff, err := base64.StdEncoding.DecodeString(req.payload[1])
	if err != nil {
		c.state = Connected
		c.errorForRequest(req, "Key was not valid base64")
		return
	}
	key, err := rsaPublicKeyFromDER(keyBuff)
	if err != nil {
		c.state = Connected
		c.errorForRequest(req, "Cannot parse key")
		return
	}
	login := req.payload[0]
	if len(login) < 3 || len(login) > 20 {
		c.state = Connected
		c.errorForRequest(req, "Login must be between 3 and 20 characters long")
		return
	}

	loginRE, err := regexp.Compile("^[a-z][a-z_0-9]{2,19}$")
	if err != nil {
		panic("Cannot compile regexp")
	}
	if !loginRE.MatchString(login) {
		c.state = Connected
		c.errorForRequest(req, "Login can consist only of alphanumerics and underscores")
		return
	}

	user := users.NewUser()
	user.Name = login
	user.Host = serverConfig.ServerName
	user.Key = nil
	if !userSet.AddUser(user) {
		c.state = Connected
		c.errorForRequest(req, "There exists user with this name")
		return
	}

	req, err = c.verifyKey(req, key)
	if err != nil {
		c.state = Connected
		userSet.RemoveUser(user)
		c.errorForRequest(req, err.Error())
		return
	}
	if req == nil {
		userSet.RemoveUser(user)
		return
	}

	c.loginStruct, err = userSet.LogIn(user)
	if err != nil {
		panic(err.Error())
	}

	user.Key = key

	c.simpleResponse(req, "ok")

	c.user = user
	c.state = Authenticated
}

func (c *ClientConnHandl) logoutProcedure() {
	userSet.LogOut(c.user)
	c.user = nil
	c.state = Connected
	// for security reasons
	c.closeAllReqGen()
}

func (c *ClientConnHandl) addFriendProcedure(req *ClientRequest) {
	if len(req.payload) != 1 {
		c.errorForRequest(req, "Incorrect payload")
		return
	}

	friendPungID := req.payload[0]
	_, friendHost, err := parsePungID(friendPungID)
	if err != nil {
		c.errorForRequest(req, "friend id doesn't match valid PungID")
		return
	}

	if friendHost != serverConfig.ServerName {
		buf, err := rsaPublicKeyToDER(c.user.Key)
		if err != nil {
			panic("cannot create der from pubkey")
		}
		keyStr := base64.StdEncoding.EncodeToString(buf)

		friend := userSet.GetUser(friendPungID)
		if friend == nil {
			friend = users.NewUser()
			friend.Name, friend.Host, _ = parsePungID(friendPungID)
			friend.Key = c.user.Key // we can't do anything smarter
			if !userSet.AddUser(friend) {
				friend = userSet.GetUser(friendPungID)
			}
		}

		userSet.SendFriendshipRequest(c.user, friend)

		data, err := serverManager.SendMessage(friendHost, "add_friend", friendPungID, c.user.FullId(), keyStr)
		if err != nil {
			c.errorForRequest(req, "Cannot add friend from other server: "+err.Error())
			return
		}

		if len(data) != 1 {
			c.errorForRequest(req, "Server returned ok but not returned key, sending messages will fail")
			return
		}
		keyBuff, err := base64.StdEncoding.DecodeString(data[0])
		if err != nil {
			c.errorForRequest(req, "Server returned ok but key wasn't encoded base64 key, sending messages will fail")
			return
		}
		key, err := rsaPublicKeyFromDER(keyBuff)
		if err != nil {
			c.errorForRequest(req, "Server returned ok but cannot parse returned key, sending messages will fail")
			return
		}

		friend.Key = key
	} else {
		friend := userSet.GetUser(friendPungID)
		if friend == nil {
			c.errorForRequest(req, "User with requested id doesn't exist")
			return
		}

		userSet.SendFriendshipRequest(c.user, friend)
	}

	c.simpleResponse(req, "ok")
}

func (c *ClientConnHandl) getFriendsProcedure(req *ClientRequest) {
	if len(req.payload) != 0 {
		c.errorForRequest(req, "Incorrect payload")
		return
	}

	for friend := range c.loginStruct.Friends {
		buf, err := rsaPublicKeyToDER(friend.Key)
		if err != nil {
			panic("cannot create der from pubkey")
		}
		keyStr := base64.StdEncoding.EncodeToString(buf)
		err = c.simpleResponse(req, "friend", friend.FullId(), keyStr)
		if err != nil {
			return
		}
	}
}

func (c *ClientConnHandl) getFriendRequestsProcedure(req *ClientRequest) {
	if len(req.payload) != 0 {
		c.errorForRequest(req, "Incorrect payload")
		return
	}

	for friend := range c.loginStruct.FriendsRequests {
		go func(user, friend *users.User, req *ClientRequest) {
			res := &ClientResponse{
				cSeq:    req.cSeq,
				sSeq:    <-c.seqNum,
				message: "friend_request",
				payload: []string{friend.FullId()},
			}
			req, _ = c.singleRequest(res, 0)
			if req == nil {
				return
			}
			switch req.message {
			case "accept":
				if friend.Host != serverConfig.ServerName {
					_, err := serverManager.SendMessage(friend.Host, "accept_friendship", friend.FullId(), user.FullId())
					if err != nil {
						break
					}
				}
				userSet.SetFriendship(user, friend)
			case "refuse":
				if friend.Host != serverConfig.ServerName {
					_, err := serverManager.SendMessage(friend.Host, "refuse_friendship", friend.FullId(), user.FullId())
					if err != nil {
						break
					}
				}
				userSet.RefuseFriendship(user, friend)
			}
		}(c.user, friend, req)
	}
}

func (c *ClientConnHandl) getMessagesProcedure(req *ClientRequest) {
	if len(req.payload) != 0 {
		c.errorForRequest(req, "Incorrect payload")
		return
	}

	for {
		msg, ok := <-c.loginStruct.Messages
		if !ok {
			return
		}
		res := &ClientResponse{
			cSeq:    req.cSeq,
			sSeq:    <-c.seqNum,
			message: "message",
			payload: []string{
				msg.From.FullId(),
				msg.Content,
				msg.Signature,
				msg.Key,
				msg.Iv,
			},
		}
		req2, err := c.singleRequest(res, 3*time.Second)
		if err == nil && req2 == nil {
			c.loginStruct.MessagesConfirm <- false
			return
		}
		if err != nil || req2.message != "ok" {
			log.Print("Failed to deliver message")
			c.loginStruct.MessagesConfirm <- false
			continue
		}
		c.loginStruct.MessagesConfirm <- true
	}
}

func (c *ClientConnHandl) sendMessageProcedure(req *ClientRequest) {
	if len(req.payload) != 5 {
		c.errorForRequest(req, "Incorrect payload")
		return
	}

	friendPungID := req.payload[0]
	if !pungRE.MatchString(friendPungID) {
		c.errorForRequest(req, "Friend id doesn't match valid PungID")
		return
	}

	for i := 1; i <= 4; i++ {
		_, err := base64.StdEncoding.DecodeString(req.payload[i])
		if err != nil {
			c.errorForRequest(req, fmt.Sprintf("payload part %d wasn't encoded in valid base64", i))
			return
		}
	}

	friend := userSet.GetUser(friendPungID)
	if friend == nil {
		c.errorForRequest(req, "User with requested id doesn't exist")
		return
	}

	if !userSet.AreFriends(c.user, friend) {
		c.errorForRequest(req, "You can send message only to your friends")
		return
	}

	if friend.Host != serverConfig.ServerName {
		_, err := serverManager.SendMessage(friend.Host, "send_message", friend.FullId(), c.user.FullId(), req.payload[1], req.payload[2], req.payload[3], req.payload[4])
		if err != nil {
			c.errorForRequest(req, "Cannot send message to other server: "+err.Error())
			return
		}
	} else {
		msg := &users.Message{
			From:      c.user,
			Content:   req.payload[1],
			Signature: req.payload[2],
			Key:       req.payload[3],
			Iv:        req.payload[4],
		}

		userSet.SendMessage(friend, msg)
	}

	c.simpleResponse(req, "ok")
}

func (c *ClientConnHandl) ping() {
	res := new(ClientResponse)
	res.cSeq = -1
	res.message = "ping"
	res.payload = []string{}
	res.sSeq = <-c.seqNum
	pong, send := c.requestGenerator(res)
	send <- true
	if c.sendResponse(res) != nil {
		return
	}
	select {
	case req, ok := <-pong:
		if ok && req.message != "pong" {
			c.errorForRequest(req, "Wrong response for ping request")
		}
	case <-time.After(5 * time.Second):
		log.Print("pong timeouted")
	}
	c.removeReqGen(res)
}

func (c *ClientConnHandl) resWriter(resChan <-chan *ClientResponse) {
	for res := range resChan {
		if _, err := c.conn.Write([]byte(res.String())); err != nil {
			c.conn.Close()
			log.Printf("Error while sending data: %s", err.Error())
			return
		}
	}
}

func (c *ClientConnHandl) Run() {
	defer c.conn.Close()
	defer log.Printf("Connection closed")
	defer c.closeAllReqGen()

	// buffer with 100KB -> max size of single message from user = 100KB
	c.reader = bufio.NewReaderSize(c.conn, 100*1024)

	seqEnd := make(chan bool)
	c.seqNum = sequenceGenerator(1, seqEnd)
	defer func() { seqEnd <- true }()

	resChan := make(chan *ClientResponse, 100)
	defer close(resChan)
	c.resChan = resChan
	go c.resWriter(resChan)

	stopPing := make(chan bool)
	go func() {
		t := time.Tick(3 * time.Second)
		for {
			select {
			case <-t:
				go c.ping()
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
			if c.user != nil {
				userSet.LogOut(c.user)
			}
			return
		}

		if c.handleRequestUsingReqGen(req) {
			continue
		}

		if req.message == "ping" {
			go c.simpleResponse(req, "pong")
		} else {
			switch c.state {
			case Connected:
				switch req.message {
				case "login":
					c.state = Authenticating
					go c.loginProcedure(req)
				case "signup":
					c.state = Authenticating
					go c.signupProcedure(req)
				default:
					go c.errorForRequest(req, "Unknowne message in Connected state")
				}
			case Authenticating:
				go c.errorForRequest(req, "Unknowne message in Authenticating state")
			case Authenticated:
				switch req.message {
				case "logout":
					c.logoutProcedure() // to make logout atomic
				case "add_friend":
					go c.addFriendProcedure(req)
				case "get_friends":
					go c.getFriendsProcedure(req)
				case "get_friend_requests":
					go c.getFriendRequestsProcedure(req)
				case "get_messages":
					go c.getMessagesProcedure(req)
				case "send_message":
					go c.sendMessageProcedure(req)
				default:
					go c.errorForRequest(req, "Unknowne message in Authenticated state")
				}
			}
		}
	}
}
