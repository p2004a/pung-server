package users

import (
	"crypto/rsa"
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

func (u *User) fullId() string {
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
	lock          sync.RWMutex
	serverName    string
}

func NewUserSet(serverName string) *UserSet {
	return &UserSet{
		idGen:         0,
		usersNameHost: make(map[string]*User),
		usersId:       make(map[int32]*User),
		friends:       make(map[int32][]*User),
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
	if _, ok := s.usersNameHost[user.fullId()]; ok {
		return false
	}
	user.id = s.newId()
	s.usersNameHost[user.fullId()] = user
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
	delete(s.usersNameHost, user.fullId())
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
