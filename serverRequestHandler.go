package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/p2004a/pung-server/users"
)

func serverAddFriendProcedure(user *users.User, friendPungID string, data []string) error {
	if len(data) != 1 {
		return errors.New("incorrect request payload")
	}

	keyBuff, err := base64.StdEncoding.DecodeString(data[0])
	if err != nil {
		return errors.New("Key was not valid base64")
	}
	key, err := rsaPublicKeyFromDER(keyBuff)
	if err != nil {
		return errors.New("Cannot parse key")
	}

	friend := userSet.GetUser(friendPungID)
	if friend == nil {
		friend = users.NewUser()
		friend.Name, friend.Host, _ = parsePungID(friendPungID)
		friend.Key = key
		if !userSet.AddUser(friend) {
			friend = userSet.GetUser(friendPungID)
		}
	}

	userSet.SendFriendshipRequest(friend, user)

	return nil
}

func serverSendMessageProcedure(user *users.User, friendPungID string, data []string) error {
	if len(data) != 4 {
		return errors.New("incorrect request payload")
	}
	return errors.New("not implemented")
}

func serverAcceptFriendshipProcedure(user *users.User, friendPungID string, data []string) error {
	if len(data) != 0 {
		return errors.New("incorrect request payload")
	}
	return errors.New("not implemented")
}

func serverRefuseFriendshipProcedure(user *users.User, friendPungID string, data []string) error {
	if len(data) != 0 {
		return errors.New("incorrect request payload")
	}
	return errors.New("not implemented")
}

func serverRequestHandler(request []string) error {
	if len(request) < 3 {
		return errors.New("incorrect request")
	}
	for i := 1; i <= 2; i++ {
		if _, _, err := parsePungID(request[i]); err != nil {
			return errors.New(fmt.Sprintf("Invalid PungID in %d field", i))
		}
	}
	user := userSet.GetUser(request[1])
	if user == nil {
		return errors.New(fmt.Sprintf("User %s doesn't exist", request[1]))
	}

	switch request[0] {
	case "add_friend":
		return serverAddFriendProcedure(user, request[2], request[3:])
	case "accept_friendship":
		return serverAcceptFriendshipProcedure(user, request[2], request[3:])
	case "refuse_friendship":
		return serverRefuseFriendshipProcedure(user, request[2], request[3:])
	case "send_message":
		return serverSendMessageProcedure(user, request[2], request[3:])
	default:
		return errors.New("unknown request name")
	}
	return nil
}
