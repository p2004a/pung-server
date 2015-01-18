package users

import (
	"container/list"
	"crypto/rsa"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
)

type User struct {
	id   int32
	Name string
	Host string
	Key  *rsa.PublicKey
}

func (u *User) FullId() string {
	return u.Name + "@" + u.Host
}

func NewUser() *User {
	return &User{
		id: -1,
	}
}

type UserSet struct {
	idGen         int32
	usersNameHost map[string]*User
	usersId       map[int32]*User
	friends       map[int32][]*User
	logged        map[int32]chan<- struct{}
	friendChan    map[int32]chan<- *User
	lock          sync.RWMutex
	serverName    string
}

func NewUserSet(serverName string) *UserSet {
	return &UserSet{
		idGen:         0,
		usersNameHost: make(map[string]*User),
		usersId:       make(map[int32]*User),
		friends:       make(map[int32][]*User),
		logged:        make(map[int32]chan<- struct{}),
		friendChan:    make(map[int32]chan<- *User),
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
	user.id = s.newId()
	s.usersNameHost[user.FullId()] = user
	s.usersId[user.id] = user
	s.friends[user.id] = []*User{}
	return true
}

func (s *UserSet) RemoveUser(user *User) bool {
	if user.id == -1 {
		return false
	}
	s.lock.Lock()
	defer s.lock.Unlock()
	if _, ok := s.usersId[user.id]; !ok {
		return false
	}
	delete(s.usersId, user.id)
	delete(s.friends, user.id)
	delete(s.usersNameHost, user.FullId())
	user.id = -1
	return true
}

func (s *UserSet) GetUser(username string) *User {
	name, host := s.getNameHost(username)
	s.lock.RLock()
	defer s.lock.RUnlock()
	return s.usersNameHost[name+"@"+host]
}

func (s *UserSet) SendFriendshipRequest(from, to *User) {

}

func (s *UserSet) SetFriendship(u1, u2 *User) {
	s.lock.Lock()
	defer s.lock.Unlock()

	if u1 == u2 {
		return
	}

	for _, f := range s.friends[u1.id] {
		if f == u2 {
			return
		}
	}

	s.friends[u1.id] = append(s.friends[u1.id], u2)
	s.friends[u2.id] = append(s.friends[u2.id], u1)

	if ch, ok := s.friendChan[u1.id]; ok {
		ch <- u2
	}
	if ch, ok := s.friendChan[u2.id]; ok {
		ch <- u1
	}
}

func (s *UserSet) messageDeliverer(user *User, msg chan<- string, conf <-chan bool, stop <-chan struct{}) {

}

func (s *UserSet) friendsReporter(user *User, friendOut chan<- *User, friendIn <-chan *User, stop <-chan struct{}) {
	friendList := list.New()

	s.lock.RLock()
	for _, friend := range s.friends[user.id] {
		friendList.PushBack(friend)
	}
	s.lock.RUnlock()

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
}

func (s *UserSet) LogIn(user *User) (<-chan string, chan<- bool, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	if _, ok := s.logged[user.id]; ok {
		return nil, nil, errors.New("user is already logged in")
	}

	stopCh := make(chan struct{}, 2)
	s.logged[user.id] = stopCh

	friendChIn := make(chan *User, 5)
	friendChOut := make(chan *User)
	s.friendChan[user.id] = friendChIn
	go s.friendsReporter(user, friendChOut, friendChIn, stopCh)

	msgCh := make(chan string)
	mshConfCh := make(chan bool)
	go s.messageDeliverer(user, msgCh, mshConfCh, stopCh)

	return msgCh, mshConfCh, nil
}

func (s *UserSet) LogOut(user *User) {
	s.lock.Lock()
	defer s.lock.Unlock()

	if _, ok := s.logged[user.id]; !ok {
		panic("user was not logged")
	}

	stopCh := s.logged[user.id]
	for i := 0; i < 2; i++ {
		stopCh <- struct{}{}
	}
}
