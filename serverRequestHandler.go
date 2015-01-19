package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/p2004a/pung-server/users"
)

func serverAddFriendProcedure(user *users.User, friendPungID string, data []string) ([]string, error) {
	if len(data) != 1 {
		return []string{}, errors.New("incorrect request payload")
	}

	keyBuff, err := base64.StdEncoding.DecodeString(data[0])
	if err != nil {
		return []string{}, errors.New("Key was not valid base64")
	}
	key, err := rsaPublicKeyFromDER(keyBuff)
	if err != nil {
		return []string{}, errors.New("Cannot parse key")
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

	buf, err := rsaPublicKeyToDER(user.Key)
	if err != nil {
		panic("cannot create der from pubkey")
	}
	keyStr := base64.StdEncoding.EncodeToString(buf)

	return []string{keyStr}, nil
}

func serverSendMessageProcedure(user *users.User, friendPungID string, data []string) ([]string, error) {
	if len(data) != 4 {
		return []string{}, errors.New("incorrect request payload")
	}

	friend := userSet.GetUser(friendPungID)
	if friend == nil {
		return []string{}, errors.New(fmt.Sprintf("User %s doesn't exist", friendPungID))
	}

	if !userSet.AreFriends(user, friend) {
		return []string{}, errors.New(fmt.Sprintf("User %s and %s aren't friends", friend.FullId(), user.FullId()))
	}

	msg := &users.Message{
		From:      friend,
		Content:   data[0],
		Signature: data[1],
		Key:       data[2],
		Iv:        data[3],
	}
	userSet.SendMessage(user, msg)

	return []string{}, nil
}

func serverAcceptFriendshipProcedure(accept bool, user *users.User, friendPungID string, data []string) ([]string, error) {
	if len(data) != 0 {
		return []string{}, errors.New("incorrect request payload")
	}

	friend := userSet.GetUser(friendPungID)
	if friend == nil {
		return []string{}, errors.New("there wasn't any friendship request")
	}

	if accept {
		userSet.SetFriendship(user, friend)
	} else {
		userSet.RefuseFriendship(friend, user)
	}

	return []string{}, nil
}

func serverRequestHandler(request []string) ([]string, error) {
	if len(request) < 3 {
		return []string{}, errors.New("incorrect request")
	}
	for i := 1; i <= 2; i++ {
		if _, _, err := parsePungID(request[i]); err != nil {
			return []string{}, errors.New(fmt.Sprintf("Invalid PungID in %d field", i))
		}
	}
	user := userSet.GetUser(request[1])
	if user == nil {
		return []string{}, errors.New(fmt.Sprintf("User %s doesn't exist", request[1]))
	}

	switch request[0] {
	case "add_friend":
		return serverAddFriendProcedure(user, request[2], request[3:])
	case "accept_friendship":
		return serverAcceptFriendshipProcedure(true, user, request[2], request[3:])
	case "refuse_friendship":
		return serverAcceptFriendshipProcedure(false, user, request[2], request[3:])
	case "send_message":
		return serverSendMessageProcedure(user, request[2], request[3:])
	default:
		return []string{}, errors.New("unknown request name")
	}
}
