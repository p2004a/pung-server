package users

import (
	"container/list"
	"crypto/rsa"
	"errors"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

type User struct {
	id   int32
	data *userData
	Name string
	Host string
	Key  *rsa.PublicKey
}

func (u *User) FullId() string {
	return u.Name + "@" + u.Host
}

// assumes data.lock is locked
func (u *User) isLogged() bool {
	return u.data.logged != nil
}

func NewUser() *User {
	user := &User{
		id: -1,
	}
	user.data = newUserData(user)
	return user
}

type Message struct {
	content string
	from    *User
}

type notify struct{}

type userData struct {
	user                   *User
	friends                []*User
	friendChan             chan<- *User
	friendshipRequests     map[*User]bool
	friendshipRequestsChan chan<- *User
	messages               *list.List
	newMessageChan         chan<- notify
	logged                 chan<- notify
	lock                   sync.RWMutex
}

func newUserData(user *User) *userData {
	data := &userData{
		user:               user,
		friends:            []*User{},
		friendshipRequests: make(map[*User]bool),
		messages:           list.New(),
		logged:             nil,
	}
	return data
}

type UserSet struct {
	idGen         int32
	usersNameHost map[string]*User
	lock          sync.RWMutex
	serverName    string
}

func NewUserSet(serverName string) *UserSet {
	return &UserSet{
		idGen:         0,
		usersNameHost: make(map[string]*User),
		serverName:    serverName,
	}
}

func (s *UserSet) newId() int32 {
	return atomic.AddInt32(&s.idGen, 1)
}

func (s *UserSet) getNameHost(username string) (string, string) {
	parts := strings.Split(username, "@")
	switch len(parts) {
	case 1:
		return parts[0], s.serverName
	case 2:
		return parts[0], parts[1]
	default:
		panic("incorrect username")
	}
}

func (s *UserSet) AddUser(user *User) bool {
	if user.id != -1 {
		return false
	}
	s.lock.Lock()
	defer s.lock.Unlock()
	if _, ok := s.usersNameHost[user.FullId()]; ok {
		return false
	}
	user.data.lock.Lock()
	defer user.data.lock.Unlock()
	user.id = s.newId()
	s.usersNameHost[user.FullId()] = user
	return true
}

// TODO: deep remove
func (s *UserSet) RemoveUser(user *User) bool {
	if user.id == -1 {
		return false
	}
	s.lock.Lock()
	defer s.lock.Unlock()
	user.data.lock.Lock()
	defer user.data.lock.Unlock()
	delete(s.usersNameHost, user.FullId())
	user.id = -1
	return true
}

type userSlice []*User

func (s userSlice) Len() int {
	return len(s)
}

func (s userSlice) Less(i, j int) bool {
	return s[i].id < s[j].id
}

func (s userSlice) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func lockUsers(users ...*User) {
	sort.Sort(userSlice(users))
	for _, user := range users {
		user.data.lock.Lock()
	}
}

func unlockUsers(users ...*User) {
	for _, user := range users {
		user.data.lock.Unlock()
	}
}

func (s *UserSet) GetUser(username string) *User {
	name, host := s.getNameHost(username)
	s.lock.RLock()
	defer s.lock.RUnlock()
	return s.usersNameHost[name+"@"+host]
}

func (s *UserSet) SendFriendshipRequest(from, to *User) {
	to.data.lock.Lock()
	defer to.data.lock.Unlock()

	if ok := to.data.friendshipRequests[from]; !ok {
		to.data.friendshipRequests[from] = true
		if to.isLogged() {
			to.data.friendshipRequestsChan <- from
		}
	}
}

func (s *UserSet) SetFriendship(u1, u2 *User) {
	if u1 == u2 {
		return
	}

	lockUsers(u1, u2)
	defer unlockUsers(u1, u2)

	for _, f := range u1.data.friends {
		if f == u2 {
			return
		}
	}

	u1.data.friends = append(u1.data.friends, u2)
	u2.data.friends = append(u2.data.friends, u1)

	if ch := u1.data.friendChan; ch != nil {
		ch <- u2
	}
	if ch := u2.data.friendChan; ch != nil {
		ch <- u1
	}
}

func (s *UserSet) SendMessage(from, to *User, content string) {
	msg := &Message{from: from, content: content}

	to.data.lock.Lock()
	to.data.messages.PushBack(msg)
	if to.data.messages.Len() == 1 && to.isLogged() {
		to.data.newMessageChan <- notify{}
	}
	to.data.lock.Unlock()
}

func (s *UserSet) messageDeliverer(user *User, msgCh chan<- *Message, conf <-chan bool, newMsg <-chan notify, stop <-chan notify) {
	for {
		select {
		case <-newMsg:
		default:
		}
		user.data.lock.Lock()
		var msg *Message
		if val := user.data.messages.Front(); val != nil {
			var ok bool
			msg, ok = val.Value.(*Message)
			if !ok {
				panic("messages didn't containt *message")
			}
		}
		user.data.lock.Unlock()
		if msg != nil {
			select {
			case msgCh <- msg:
			case <-stop:
				return
			}
			select {
			case ok := <-conf:
				if ok {
					user.data.lock.Lock()
					user.data.messages.Remove(user.data.messages.Front())
					user.data.lock.Unlock()
				}
			case <-stop:
				return
			}
		} else {
			select {
			case <-newMsg:
			case <-stop:
				return
			}
		}
	}
}

func (s *UserSet) friendsRequestDeliverer(user *User, reqOut chan<- *User, reqIn <-chan *User, stop <-chan notify) {
	reqs := list.New()

	user.data.lock.RLock()
	for req := range user.data.friendshipRequests {
		reqs.PushBack(req)
	}
	user.data.lock.RUnlock()

	for {
		if reqs.Front() == nil {
			select {
			case req := <-reqIn:
				reqs.PushBack(req)
			case <-stop:
				return
			}
		} else {
			req, _ := reqs.Front().Value.(*User)
			select {
			case reqOut <- req:
				reqs.Remove(reqs.Front())
			case req := <-reqIn:
				reqs.PushBack(req)
			case <-stop:
				return
			}
		}
	}

	close(reqOut)
}

func (s *UserSet) friendsReporter(user *User, friendOut chan<- *User, friendIn <-chan *User, stop <-chan notify) {
	friendList := list.New()

	user.data.lock.RLock()
	for _, friend := range user.data.friends {
		friendList.PushBack(friend)
	}
	user.data.lock.RUnlock()

	for {
		if friendList.Front() == nil {
			select {
			case newFriend := <-friendIn:
				friendList.PushBack(newFriend)
			case <-stop:
				return
			}
		} else {
			friend, _ := friendList.Front().Value.(*User)
			select {
			case friendOut <- friend:
				friendList.Remove(friendList.Front())
			case newFriend := <-friendIn:
				friendList.PushBack(newFriend)
			case <-stop:
				return
			}
		}
	}

	close(friendOut)
}

type LoginStruct struct {
	messages        <-chan *Message
	messagesConfirm chan<- bool
	friends         <-chan *User
	friendsRequests <-chan *User
}

func (s *UserSet) LogIn(user *User) (*LoginStruct, error) {
	user.data.lock.Lock()
	defer user.data.lock.Unlock()

	if user.isLogged() {
		return nil, errors.New("user is already logged in")
	}

	stopCh := make(chan notify)
	user.data.logged = stopCh

	friendChIn := make(chan *User, 5)
	friendChOut := make(chan *User)
	user.data.friendChan = friendChIn
	go s.friendsReporter(user, friendChOut, friendChIn, stopCh)

	friendReqChIn := make(chan *User, 5)
	friendReqChOut := make(chan *User)
	user.data.friendshipRequestsChan = friendReqChIn
	go s.friendsRequestDeliverer(user, friendReqChOut, friendReqChIn, stopCh)

	msgCh := make(chan *Message)
	msgConfCh := make(chan bool, 1) // must be 1 in case of stop just after sendinf message in messageDeliverer
	msgNew := make(chan notify)
	user.data.newMessageChan = msgNew
	go s.messageDeliverer(user, msgCh, msgConfCh, msgNew, stopCh)

	loginStruct := &LoginStruct{
		messages:        msgCh,
		messagesConfirm: msgConfCh,
		friends:         friendChOut,
		friendsRequests: friendReqChOut,
	}

	return loginStruct, nil
}

func (s *UserSet) LogOut(user *User) {
	user.data.lock.Lock()
	defer user.data.lock.Unlock()

	if !user.isLogged() {
		panic("user was not logged")
	}

	for i := 0; i < 3; i++ {
		user.data.logged <- notify{}
	}
	user.data.logged = nil
}
