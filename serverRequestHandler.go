package main

import (
	"errors"
	"fmt"
	"github.com/p2004a/pung-server/users"
)

func serverAddFriendProcedure(user *users.User, friendPundID string, data []string) error {
	return errors.New("not implemented")
}

func serverSendMessageProcedure(user *users.User, friendPundID string, data []string) error {
	return errors.New("not implemented")
}

func serverAcceptFriendshipProcedure(user *users.User, friendPundID string, data []string) error {
	return errors.New("not implemented")
}

func serverRefuseFriendshipProcedure(user *users.User, friendPundID string, data []string) error {
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
